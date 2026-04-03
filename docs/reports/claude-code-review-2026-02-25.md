**Last updated:** 2026-03-24

# Loopgate code review & architecture discussion (historical)
**Date**: 2026-02-25
**Reviewer**: Claude (Opus 4.5)
**Project**: Loopgate — capability-governed AI control plane (body below may still say “Morph” for the historical client name)

---

## Table of Contents
1. [Initial Assessment](#initial-assessment)
2. [Architecture Review](#architecture-review)
3. [Concurrency & Safety Analysis](#concurrency--safety-analysis)
4. [Threat Modeling (Malicious Model)](#threat-modeling-malicious-model)
5. [Detailed Concurrency Checks](#detailed-concurrency-checks)
6. [QA Engineering Recommendations](#qa-engineering-recommendations)
7. [Product Viability Assessment](#product-viability-assessment)
8. [Enterprise Documentation Synthesis MVP](#enterprise-documentation-synthesis-mvp)
9. [Summary & Next Steps](#summary--next-steps)

---

## Initial Assessment

### What Morph Is
A **capability-governed AI orchestrator** focused on safety and auditability. Core philosophy:
- Consent-first operations
- Append-only audit logs
- Policy-driven capability restrictions

### What's Working Well

**Security-first architecture** — SafePath enforcement, deny-list overrides, write approval gates, and protected memory paths.

**Clean, minimal codebase** — 1,223 lines of Go with only 3 dependencies (readline, yaml, sys). Modular structure: `internal/` for core logic, `core/` for policy/memory, `cmd/` for CLI.

**Immutable audit trail** — JSONL ledger approach is sound. Policy enforcement working (denying access to protected paths).

**Multi-tiered memory system** — Ledger → distillates → resonate keys is a good design for long-term agent memory.

### Where It's Incomplete

1. `body/` is empty — Tools and skills not implemented
2. `morphlings/` unused — Registry exists but no sub-agent management
3. Model interface is a stub — Just echoes input, no real AI backend
4. Distillation under-utilized — Heuristics minimal, distillates file empty
5. No tests — `tests/` directory exists but is empty

---

## Architecture Review

### Constraints Reviewed Against
1. Ledger is canonical source of truth (append-only)
2. Distillates are derived data (never modify ledger history)
3. Policy enforcement sits between model output and tool execution
4. Memory compaction is deterministic and cursor-based (no overlap, no duplication)

### Concurrency Risks in Scheduler Design

**Issue in `/reset` command** (`main.go:244-247`):
```go
st = statepkg.New()
st.DistillCursorLine = persisted.DistillCursorLine  // BUG: uses startup cursor
```
If distillation advanced the cursor since startup, `/reset` reverts it, causing double-processing.

**Fix**: Capture current cursor before reset:
```go
oldCursor := st.DistillCursorLine
st = statepkg.New()
st.DistillCursorLine = oldCursor
```

### Race Conditions in State Persistence

**State file writes not atomic** (`state/state.go:65`):
```go
return os.WriteFile(path, b, 0644)  // Truncates then writes
```
Use write-to-temp-then-rename:
```go
tmp := path + ".tmp"
os.WriteFile(tmp, b, 0644)
os.Rename(tmp, path)  // Atomic on POSIX
```

**No file locking** — Two Morph instances would create duplicate distillates.

### Distillation: Skip/Double-Processing Risks

**Distillate write failure** (`distillate.go:174`): Returns old cursor on error, could cause re-processing if file partially written.

**Distillate ID collision** (`distillate.go:150`): Two distillations in same second get identical IDs.

### Policy/Tool Boundary

**CLI boundary is clean** — Every command checks policy flags then calls `SafePath()`.

**Bypass vectors**:
1. **Symlink traversal** — `SafePath` validates path string, not resolved target
2. **TOCTOU window** — Between SafePath check and file operation

### Architectural Weaknesses for Model Backend

1. Model interface doesn't support tool calls (returns `string` only)
2. No policy enforcement layer between model and tools
3. Chat history grows unbounded
4. No tool call rate limiting
5. Synchronous execution blocks main loop

---

## Concurrency & Safety Analysis

### Threat Model: Malicious Model Integration

Assume a malicious model attempts to:
- Call tools repeatedly
- Trigger distillation storms
- Bypass policy by crafting tool-like output
- Cause resource exhaustion via ledger growth

### Attack Vector 1: Repeated Tool Calls

**Vulnerabilities**:
- No per-turn tool call limit
- No session-wide tool budget
- No cooldown between tool calls

**Mitigations**:
```go
// Add to RuntimeState
ToolCallsThisTurn    int
ToolCallsThisSession int

// Add to Policy
tools:
  max_calls_per_turn: 10
  max_calls_per_session: 500
  cooldown_between_calls_ms: 100
```

### Attack Vector 2: Distillation Storms

**Vulnerabilities**:
- No distillation cost limit
- Distillation I/O unbounded
- No concurrent distillation guard

**Mitigations**:
```go
var lastDistillUTC time.Time
var distillMu sync.Mutex

runDistill := func(trigger string) {
    distillMu.Lock()
    defer distillMu.Unlock()
    if time.Since(lastDistillUTC) < 30*time.Second {
        return
    }
    // ... existing logic ...
    lastDistillUTC = time.Now().UTC()
}
```

### Attack Vector 3: Policy Bypass via Crafted Output

**Current protection**: Model output just printed, never parsed as commands.

**Future vulnerabilities**:
1. Tool call injection if parsing is loose
2. Prompt injection via ledger (if distillates fed back to model)
3. History poisoning

**Mitigations**:
- Structured tool call protocol (don't parse text for tool calls)
- Tool whitelist validation
- Sanitize before context injection
- Role enforcement in history

### Attack Vector 4: Resource Exhaustion

**Vulnerabilities**:
- No ledger size cap
- No event size limit
- No event rate limit
- Chat history grows unbounded

**Mitigations**:
- Event size limit (64KB)
- Model output truncation (32KB)
- Ledger rotation
- History window (keep first 10 + last 90 turns)

### Defense-in-Depth Summary

| Layer | Control | Policy Field |
|-------|---------|--------------|
| Model output | Size limit, truncation | `model.max_output_bytes` |
| Tool dispatch | Per-turn limit, cooldown | `tools.max_calls_per_turn`, `tools.cooldown_ms` |
| Tool dispatch | Session budget | `tools.max_calls_per_session` |
| Ledger | Event size limit | `logging.max_event_bytes` |
| Ledger | Rotation threshold | `logging.rotate_at_bytes` |
| Distillation | Minimum interval | `memory.min_distill_interval_seconds` |
| Distillation | Hourly cap | `memory.max_distills_per_hour` |
| History | Turn window | `memory.max_history_turns` |

---

## Detailed Concurrency Checks

### 1. `stMu` Analysis

**Verdict**: Mutex usage is correct but not optimal.

**Issue 1**: Read-modify-write gap in `runDistill`:
```go
stMu.Lock()
cursor := st.DistillCursorLine  // READ
stMu.Unlock()

newCursor, err := memory.DistillFromLedger(...)  // WORK (outside lock)

stMu.Lock()
st.DistillCursorLine = newCursor  // WRITE
```

If `/reset` executes between unlock and re-lock, cursor values conflict.

**Issue 2**: `must()` vs `_` inconsistency in error handling.

### 2. `toolEventCh` Debouncing

**Verdict**: Correctly implemented. Proper coalescing and timer reset pattern.

### 3. Panic in Distillation

**Risk**: If `Save` panics, `stMu` is held — deadlock on next access.

**Scenario**: Turn increment completes, `IncrementTurn` modifies `st`, then `Save` panics:
```go
stMu.Lock()
statepkg.IncrementTurn(&st)  // st modified
must(statepkg.Save(...))     // PANIC here
stMu.Unlock()                // Never reached
```

### 4. Ledger Read/Write Race

**Verdict**: Safe due to:
1. POSIX append atomicity for reasonably-sized writes
2. Scanner reads complete lines (waits for `\n`)
3. Malformed lines skipped without advancing cursor

### 5. FinalizeSession vs Distillation Ordering

**Verdict**: Safe but sloppy. No explicit synchronization on shutdown.

**Recommended fix**: Add `schedulerDone` channel for clean shutdown:
```go
close(doneCh)
<-schedulerDone  // Wait for scheduler to fully exit
_ = memory.FinalizeSession(...)
```

### Summary Table

| Concern | Status | Issue |
|---------|--------|-------|
| `stMu` usage | ⚠️ Mostly safe | Read-modify-write gap; `/reset` cursor bug |
| Cursor persistence | ⚠️ Safe but fragile | Silent error discard; crash can cause duplicates |
| `toolEventCh` debounce | ✅ Correct | Proper coalescing and timer reset |
| Panic in distillation | ⚠️ Risk | Mutex held during panic |
| Ledger read/write race | ✅ Safe | POSIX append semantics |
| Finalize vs distill ordering | ⚠️ Sloppy | No explicit synchronization |

---

## QA Engineering Recommendations

### Testing Strategy

**Tier 1 — Safety-critical**:
- `SafePath` — path traversal, symlinks, Unicode normalization
- `ledger.Append` — concurrent appends, large events, invalid UTF-8

**Tier 2 — Correctness-critical**:
- `DistillFromLedger` cursor math (empty ledger, EOF, boundaries, malformed lines)

**Tier 3 — Behavioral**:
- Scheduler timing (use fake clocks)
- `/reset` preserves correct cursor

### Observability Gaps

Add structured internal logging:
```go
type Logger interface {
    Debug(msg string, fields ...Field)
    Info(msg string, fields ...Field)
    Warn(msg string, fields ...Field)
    Error(msg string, err error, fields ...Field)
}
```

Critical events to log:
- Distillation started/completed/failed (with duration)
- State save succeeded/failed
- Policy denials
- Scheduler triggers

### Missing Signal Handling

Add SIGTERM/SIGINT handling:
```go
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

go func() {
    <-sigCh
    close(doneCh)  // Trigger graceful shutdown
}()
```

### Error Recovery / Corruption Handling

- Corrupt state file: Backup and reinitialize
- Corrupt ledger line: Log warning, continue
- Disk full: Enter degraded mode with warning

### Time-Related Edge Cases

- Clock skew: Use monotonic sequence numbers for ordering
- Long sessions: Add history trimming

### Configuration Hardcoding

Move magic numbers to policy or constants:
```yaml
scheduler:
  idle_threshold_minutes: 5
  periodic_threshold_minutes: 10
  debounce_delay_seconds: 2
```

### Ledger Schema Versioning

Add version field now:
```json
{"v":1,"ts":"...","type":"chat.user","session":"...","data":{"text":"hello"}}
```

### Operational Runbook

Add `/debug` commands:
```
/debug state     — dump RuntimeState
/debug cursor    — show distill cursor vs ledger length
/debug distill   — force immediate distillation
/debug ledger    — show last N ledger events
```

### Architectural Suggestion

Extract a `Kernel` struct from `main.go`:
```go
type Kernel struct {
    paths     Paths
    persona   config.Persona
    policy    config.Policy
    state     *RuntimeState
    stateMu   sync.Mutex
    ledger    *Ledger
    scheduler *Scheduler
    model     Model
}
```

### Priority Checklist

| Category | Item | Priority |
|----------|------|----------|
| Testing | SafePath property tests | High |
| Testing | Cursor math unit tests | High |
| Observability | Internal structured logging | High |
| Reliability | SIGTERM/SIGINT handling | High |
| Robustness | Ledger schema version field | High |
| Reliability | Corrupt state recovery | Medium |
| Operations | `/debug` commands | Medium |
| Code quality | Extract Kernel struct | Low |

---

## Product Viability Assessment

### What's Novel

1. **Event-sourced memory** — Ledger → distillate → resonate key pipeline is more principled than vector DB approaches
2. **Policy as first-class citizen** — Tools check policy before execution
3. **Deterministic compaction** — Cursor-based distillation with no overlap
4. **Minimal dependencies** — Three Go libraries, no framework bloat

### What's Missing for Shippable

**Tier 1 — Without these, it's a demo:**
- Real model backend
- 3-5 useful tools beyond filesystem
- One compelling end-to-end workflow

**Tier 2 — Without these, it's fragile:**
- Concurrency/persistence fixes
- Signal handling
- Basic observability

**Tier 3 — Without these, it's hard to adopt:**
- Documentation
- Installation story
- Example policies

### Target Market

**Strong fit:**
- Regulated industries (finance, healthcare, legal)
- Enterprise contexts needing audit trails
- Research requiring reproducibility

**Weak fit:**
- Hobbyists
- Developers wanting fastest path to "working agent"

### Verdict

The architecture is sound. The vision is coherent. The bet is that governed, auditable AI agents will matter more over time. That's a reasonable bet.

---

## Enterprise Documentation Synthesis MVP

### Use Case

Morph safely integrates with internal systems to synthesize and compile accurate documentation with full auditability.

### Deployment: Docker vs Local

**Docker-first** for enterprise:
- Predictable environment
- Fits existing infrastructure (K8s, ECS)
- Easier secrets management
- Network isolation

**Also provide static binary** for:
- Developer evaluation
- CI/CD integration
- Air-gapped environments

### Integration Priority

| Integration | Priority | Why |
|-------------|----------|-----|
| Confluence | 1 | Where enterprise docs live |
| Slack | 2 | Where tribal knowledge hides |
| Notion | 3 | Popular but less enterprise-dominant |
| Obsidian | 4 | Niche, local-first |
| Custom DB | Later | Don't build until necessary |

### Policy Extension for Integrations

```yaml
tools:
  confluence:
    enabled: true
    base_url: "https://yourcompany.atlassian.net"
    allowed_spaces: ["ENG", "PRODUCT"]
    denied_pages: ["HR-*", "LEGAL-*"]
    write_enabled: false

  slack:
    enabled: true
    allowed_channels: ["#engineering", "#incidents"]
    denied_channels: ["#exec-*", "#hr-*"]
    read_threads: true
    read_dms: false
```

### Documentation Synthesis Workflow

1. **Ingest** — Read from sources (each read logged with metadata)
2. **Synthesize** — Model processes content
3. **Review** — Human approval with source visibility
4. **Write** — Output to target with provenance

**Killer feature**: Every generated document traces back to sources via ledger.

### Enterprise Readiness Checklist

| Concern | What You Need |
|---------|---------------|
| Auth | OIDC/SAML integration |
| Permissions | Role-based access |
| Data residency | Self-hosted option |
| Audit | Ledger export (SIEM-compatible) |
| Compliance | Document controls |
| Support | SLA, runbooks |

### Note on Obsidian

Obsidian is **not open source** (proprietary freeware). The vault format is open. Open-source alternatives: Logseq, Foam, Dendron.

**Recommendation**: Don't build a document database. Output to standard formats (Markdown, Confluence). The value is synthesis and governance, not storage.

---

## Summary & Next Steps

### Required Fixes (Priority Order)

1. Fix `/reset` to use current cursor, not startup cursor
2. Add `schedulerDone` channel for clean shutdown
3. Replace `must()` with error returns, or use `defer` to release mutex before panic
4. Don't silently discard `Save()` errors
5. Add symlink resolution to `SafePath`

### Recommended Next Steps

1. **Finish model backend** — Even simple OpenAI/Anthropic API integration
2. **Build Confluence reader first** — Highest enterprise value
3. **Build "doc audit" view** — Show sources for generated documents
4. **Docker image with sane defaults** — Single `docker run` to start
5. **One demo workflow** — Confluence + Slack → synthesized doc with provenance

### Final Assessment

**The concept is valid. The architecture is appropriate. The market need is real and growing.**

The core insight — that AI agents need governance, auditability, and consent-based operation — is sound and increasingly important. Most agent frameworks treat these as afterthoughts. Morph makes them foundational.

The question isn't "is this a good idea" — it is. The question is execution: build the integrations, find the customers, survive long enough for the market to catch up.

The foundation is solid. Go build it.

---

*Review conducted by Claude (Opus 4.5) on 2026-02-25*
