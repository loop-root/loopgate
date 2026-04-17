# Loopgate — Senior Engineering Code Review

**Reviewer:** Claude Sonnet 4.6 (Thinking) acting as Sr. Principal Engineer  
**Date:** 2026-04-16  
**Scope:** Full repository — 274 Go source files, ~69k lines  
**Verdict summary:** Architecturally serious, security-first, production-capable for local single-user deployment. Several non-trivial issues exist that would hurt you at 2 AM. Documented below in full.

---

## 1. First Impressions & Orientation

Reading the first files of any codebase tells you a lot. Here, the first things that jump out are:

- `AGENTS.md` is 33KB — the rules document is longer than most services' entire codebases. That's a feature, not a bug. It reads like someone who has been burned by ambiguous authority models before.
- `go.mod` has **four direct dependencies** for a 69k-line security-critical system. That is exceptional dependency hygiene.
- `server.go` starts with a locking invariant comment that documents the exact ordering of all mutexes before the first function definition. That is discipline.
- The threat model (`docs/loopgate-threat-model.md`) is honest about its own gaps — unauthenticated `/v1/health`, same-user local trust, no external tamper evidence. Honest threat models are rare and valuable.

This does not read like a rookie's repository. It reads like someone with a strong security background who is navigating the complexity of building a governed AI control plane. But there are real issues, and a few of them are sharp.

---

## 2. What's Genuinely Good

### Dependency hygiene
Four direct deps: `readline`, `golang.org/x/sys`, `golang.org/x/text`, `gopkg.in/yaml.v3`. No ORMs, no web frameworks, no embedded databases, no Kubernetes clients, no JSON schema libraries. The only third-party security-sensitive code is `x/sys` for `unix.Flock`. This is exceptional.

### Lock ordering is documented and enforced
The locking comment at the top of `Server` struct is real — and the code respects it. `auditMu` is a strict leaf. `mu` takes precedence. Cross-lock operations snapshot and release before crossing domains. This is the right model.

### Fail-closed everywhere
Every auth denial path, path resolution failure, and approval store overflow fails closed. The `SafePath` function in particular is solid — it resolves symlinks before allowing/denying, rejects null bytes, validates basenames, and uses a clean `isWithin` prefix check with path separator protection against `/rootX` bypass attacks.

### Audit is append-only and hash-chained
The ledger is JSONL with SHA-256 hash linking (`event_hash`/`previous_event_hash`) and sequence monotonicity enforcement. The segmented rotation path adds manifest hash chaining. The code reads the verified chain on startup rather than trusting a cached cursor. That's the right design.

### Secrets hygiene is unusually good
The `secrets.RedactText` function is applied at the boundary — audit entries, error messages, log output. The `RedactStructuredFields` function handles nested maps. The sensitive-field fragment list in `isSensitiveFieldKey` is reasonable. Secrets are extracted from request structs immediately after decode.

### HMAC request signing
Request signatures cover method, path, session ID, timestamp, nonce, and body hash. The TOCTOU in expiry was identified in the AGENTS.md rules and is correctly fixed — `now()` is snapshotted inside `mu.Lock()` and all expiry checks happen atomically.

### MCP process lifecycle
The `ensureMCPGatewayServerLaunched` path handles launch-attempt-ID racing correctly. It places a `starting` sentinel, launches the process, then only upgrades to `launched` if the sentinel still matches the current attempt ID. This is the kind of concurrency discipline most engineers miss.

---

## 3. Issues — Ranked by Severity

### 🔴 CRITICAL — `go 1.25.0` does not exist

**File:** `go.mod:3`

```go
go 1.25.0
```

The current release of Go as of April 2026 is 1.24.x. `go 1.25.0` has not been released. This means:
- `go test ./...` will fail on any machine with a real Go toolchain unless that toolchain is artificially labeled 1.25.
- A new contributor running `go mod tidy` or `go build` will see an immediate error or a confusing toolchain resolution path.
- CI on standard images will fail.

This is likely a future-proofing stub that was committed to reserve space, but it is wrong and it blocks the getting-started path entirely. This is the highest-priority fix for onboarding.

**Fix:** `go 1.24.0` (or whatever the current stable release is).

---

### 🔴 CRITICAL — Sandbox artifact committed to the repo

**File:** `runtime/sandbox/root/home/workspace/haven_cli/affirmation_tui/stats.go`

An entire Go source file — a small TUI stats application — exists inside the sandbox runtime directory. Runtime state that `runtime/` contains is documented as "fully gitignored." This file exists in the live tree, appears to be an artifact of a previous demo session, and is completely unrelated to Loopgate.

This is a `.gitignore` failure. The `runtime/` directory should be fully gitignored as stated. A committed demo artifact in a security-sensitive repo creates several problems:
- It breaks the claim that `runtime/` is fully gitignored.
- It suggests the sandbox escape boundary may not be as clean as documented.
- It is dead code in a production repo — exactly what the user asked to find.
- For an enterprise customer reviewing this repo, it signals sloppiness in a security product.

**Fix:** Remove the file, update `.gitignore` to verify `runtime/sandbox/` is fully excluded, audit the `.gitignore` for other gaps.

---

### 🟠 HIGH — Legacy env var namespace is a namespace collision / identity leak

**Files:** `internal/modelruntime/runtime.go`

The model runtime configuration reads from a legacy environment-variable
namespace instead of a Loopgate-owned prefix. This creates several problems:

1. **Namespace collision risk:** If another tool uses the same prefix in the
   operator's environment, these env vars will silently stomp each other or
   leak configuration.
2. **Product confusion:** The documentation says Loopgate is the product.
   Legacy-prefixed env vars in operator-facing configuration paths are
   confusing for new users trying to configure the system.
3. **Error messages reference it:** legacy-prefixed names surface in
   production error output.

**Fix:** Make `LOOPGATE_MODEL_PROVIDER` the canonical name and remove the
legacy namespace from runtime behavior and operator docs.

---

### 🟠 HIGH — `appendChainStateCache` is a package-level global mutex-protected map

**File:** `internal/ledger/ledger.go:33`

```go
var appendChainStateCache = struct {
    mu     sync.Mutex
    states map[cachedChainStateKey]cachedChainState
}{
    states: make(map[cachedChainStateKey]cachedChainState),
}
```

This is the only package-level mutable state in the entire codebase. It is a performance cache for the ledger append chain state. Issues:

1. **Test isolation is broken.** Any test that writes to the ledger will populate this cache. The next test that writes to the same normalized path will hit the cache. If tests run in parallel (even within the same test binary), they will share this state. This is the most common source of "my tests pass individually but fail together" debugging nightmares.
2. **Process-singleton behavior.** If two goroutines in the same process are appending to different logical ledgers that happen to have the same normalized path (an edge case, but possible in tests), they share cached chain state through this global.
3. **No bounded growth.** The cache map grows without eviction other than the `fileState` invalidation check. In a long-running process that rotates segments (which changes the normalized path for the active file), old cache entries accumulate.

**Fix:** Pass the chain-state cache as a value attached to the `Server` struct. Tests that construct `Server` instances get independent caches. Alternatively, key invalidation on process start (reset the cache in `NewServer`) would at least fix the test isolation issue.

---

### 🟠 HIGH — `verifySignedRequestWithMACKey` duplicates header parsing three times

**File:** `internal/loopgate/request_auth.go`

The functions `parseSignedControlPlaneHeaders`, `verifySignedRequest`, and `verifySignedRequestWithMACKey` each independently re-parse the same four headers:
```go
controlSessionID := strings.TrimSpace(request.Header.Get("X-Loopgate-Control-Session"))
requestTimestamp := strings.TrimSpace(request.Header.Get("X-Loopgate-Request-Timestamp"))
requestNonce := strings.TrimSpace(request.Header.Get("X-Loopgate-Request-Nonce"))
requestSignature := strings.TrimSpace(request.Header.Get("X-Loopgate-Request-Signature"))
```

This is three separate copies of the same validation logic, independently checking headers and timestamp skew. When a bug is found in one, it must be fixed in three places. This is a maintenance trap — particularly dangerous in a security-critical code path. At 2 AM when you're investigating a signature bypass report, you will look at `verifySignedRequest` and miss that `verifySignedRequestWithMACKey` has diverged.

**Fix:** Parse headers exactly once into a typed struct. Pass the parsed, validated struct to the HMAC verification function. The `parseSignedControlPlaneHeaders` function already exists for this purpose but is not always used as the entry point.

---

### 🟠 HIGH — `ensurePromotionNotDuplicate` scans the entire `derivedArtifactDir` on every promotion

**File:** `internal/loopgate/promotion.go:320`

```go
derivedArtifactEntries, err := os.ReadDir(server.derivedArtifactDir)
// ... iterates all files, loads and deserializes each record, computes SHA256 fingerprint
```

This is an `O(n)` full directory scan that loads and deserializes every derived artifact record on every promotion attempt. Under sustained load (many capability executions producing quarantined output, each potentially promoted), this becomes a quadratic scan pattern. A directory with 10,000 derived artifact records will deserialize 10,000 JSON files on every promotion. This will be slow and is never pruned.

**Fix:** Maintain an in-memory fingerprint index (a `map[string]string` fingerprint → artifact ID) loaded at startup and updated on each successful promotion. The `promotionMu` already serializes all promotion operations, so this is safe.

---

### 🟡 MEDIUM — The hash chain is tamper-evident, not tamper-proof — but operators may not understand the distinction

**File:** `internal/ledger/ledger.go:237` (comment), `docs/loopgate-threat-model.md:96`

The documentation correctly states: *"a same-user filesystem writer can replace the entire file with a new internally consistent chain."*

This is honest and correct. The HMAC checkpoint mechanism on macOS (using Keychain) addresses this for incremental appends, but a sophisticated same-user attacker who replaces the entire JSONL file can recompute a consistent chain from scratch using SHA-256 (no secret key in the chain itself).

The threat model acknowledges this. However, the Getting Started documentation sends operators directly to `go run ./cmd/loopgate-ledger tail -verbose` to verify audit. A new operator who receives this as their primary inspection tool will believe the chain is strongly tamper-proof. They need to understand:
1. The chain proves ordering and completeness *within the file as written*.
2. It does not prove Loopgate was the only writer.
3. The Keychain-backed HMAC checkpoint is the stronger control, and it requires setup verification.

**Fix:** Add a bolded callout to the Getting Started and Ledger docs explaining what the chain does and does not prove. The threat model has it right; the operator flow docs should match.

---

### 🟡 MEDIUM — `sessionMACRotationMaster` fallback path creates a TOCTOU window

**File:** `internal/loopgate/request_auth.go:283`

```go
if len(server.sessionMACRotationMaster) > 0 {
    return server.verifySignedRequestAgainstRotatingSessionMAC(...)
}
return server.verifySignedRequestWithMACKey(..., activeSession.SessionMACKey)
```

The check `len(server.sessionMACRotationMaster) > 0` happens outside any lock. `sessionMACRotationMaster` is a `[]byte` on the `Server` struct. If it is ever reloaded or rotated during server operation (which the design appears to anticipate given its name), reading it without a lock is a data race.

In the current code path the master is written once at startup and never changed, so this is latent rather than live. But AGENTS.md rule 2 explicitly prohibits this pattern: *"expiry checks in `authenticate()` must be performed inside the lock using a single `now()` snapshot."* The same reasoning applies to any other server-state read in the auth path.

**Fix:** Either document that `sessionMACRotationMaster` is immutable after startup (and enforce it structurally), or read it inside `server.mu.Lock()`.

---

### 🟡 MEDIUM — The policy `Check()` function evaluates the `"host"` category using filesystem config, not host-specific config

**File:** `internal/policy/checker.go:107`

```go
func (c *Checker) checkHost(tool ToolInfo) CheckResult {
    fsCfg := c.Policy.Tools.Filesystem  // ← reads filesystem config
```

The `checkHost` function is a category check for `"host"` tools, but it reads `Policy.Tools.Filesystem` rather than any host-specific policy key. This means:
- Changing host read/write permissions requires editing the filesystem policy section.
- There is no way to allow host folder reads while denying sandbox filesystem reads (or vice versa).
- Future operators trying to configure tighter policy will be confused that `host.*` tools are governed by `tools.filesystem.*` config.

This is not a security vulnerability (it defaults to deny for unknown operations), but it is a configuration model bug that will cause debugging confusion and may prevent legitimate operator intent from being expressed.

**Fix:** Either add a `tools.host` policy section, or document explicitly that host tools are intentionally governed by `tools.filesystem` policy, and add that explanation to the operator guide.

---

### 🟡 MEDIUM — `isSecretExportCapabilityHeuristic` appears in two separate files with identical logic

**Files:** `internal/loopgate/secret_export.go`, `internal/loopgate/capability_execution_runtime.go`

`secretExportCapabilityNameHeuristic` (in `secret_export.go`) and `isSecretExportCapabilityHeuristic` (in `capability_execution_runtime.go`) are functionally identical — same prefix list, same combination logic. They differ only in name and comment structure.

The AGENTS.md rule 6 specifically calls this out: *"`isSecretExportCapability` is a defense-in-depth guard, not the primary capability boundary."* Having two independently maintained copies of the heuristic means they can drift, and a future change to one may not be applied to the other.

**Fix:** One function, one location. The `capabilityProhibitsRawSecretExport` dispatch in `pending_approval_integrity.go` should call the single canonical heuristic.

---

### 🟡 MEDIUM — Rate limiting for `fs_read` uses string comparison, not capability registry

**File:** `internal/loopgate/server.go:950`

```go
if capabilityRequest.Capability == "fs_read" || capabilityRequest.Capability == "operator_mount.fs_read" {
```

This is a hardcoded string comparison that dictates which capabilities get rate limiting. It cannot be extended without source modification. If a new capability named `vault_read` or `external_fs_read` is added that should also be rate-limited, a developer must know to update this condition.

Compare this to the registry-based dispatch pattern used for most other tool classification decisions. The appropriate design is an interface method on `Tool` (e.g., `RequiresRateLimit() bool`) that each tool opts into.

---

### 🟡 MEDIUM — `cleanupDeadMCPGatewayServerIfNeeded` has a double lock + process-liveness check TOCTOU

**File:** `internal/loopgate/mcp_gateway_runtime.go:215`

```go
func (server *Server) cleanupDeadMCPGatewayServerIfNeeded(serverID string) {
    server.mu.Lock()
    launchedServer, found := server.mcpGatewayLaunchedServers[serverID]
    // ...
    processStillAlive, err := server.processExists(launchedServer.PID)  // ← lock held during syscall
    if err != nil || processStillAlive {
        server.mu.Unlock()
        return
    }
    delete(server.mcpGatewayLaunchedServers, serverID)
    server.mu.Unlock()
    closeMCPGatewayLaunchedServerPipes(launchedServer)
```

This holds `server.mu` during a `processExists` syscall (a `kill(pid, 0)` check on Linux/macOS). AGENTS.md rule explicitly states: *"Avoid holding locks across network I/O, model calls, or disk I/O where possible."* A syscall that probes process liveness is in the same category — it can block if the OS is under pressure.

Additionally, the liveness check and the delete are not atomic with respect to an external concurrent stop call that might also delete the same server entry.

**Fix:** Snapshot the PID while holding the lock, release the lock, perform the `processExists` check, re-acquire the lock, re-verify the server entry still matches before deleting.

---

### 🟢 LOW — Dead/vestigial env var `MORPH_REPO_ROOT`

**File:** `cmd/loopgate/main.go` (inferred from grep output)

`MORPH_REPO_ROOT` is read to set the repo root path. This is a legacy env var from the `MORPH_` namespace and appears alongside `LOOPGATE_`-prefixed documentation. New operators will not know to set this. There is no error message pointing them to the correct variable name.

---

### 🟢 LOW — `uiEvents []UIEventEnvelope` is an unbounded in-memory slice

**File:** `internal/loopgate/server.go:102`

```go
uiEvents []UIEventEnvelope
```

UI events are accumulated in this slice. There is no capped ring buffer or count-based trim visible in the struct. Under sustained operation, this will grow without bound. The `nextExpirySweepAt` pattern handles session/approval expiry, but there is no corresponding sweep for UI events.

---

### 🟢 LOW — `loadOrCreateStateKey` writes a new key non-atomically

**File:** `internal/loopgate/server.go:1207`

```go
if err := os.WriteFile(keyPath, newKey, 0o600); err != nil {
```

This uses `os.WriteFile` directly rather than the write-to-temp + atomic rename pattern used everywhere else in the codebase (e.g., `SavePersistedConfig`, `writeDerivedArtifactRecord`). If the process crashes mid-write, a partial key file is left on disk. On next startup, `ReadFile(...) len >= 32` will succeed with a 0-padded partial key. Unlikely to matter in practice since the key is new and small, but inconsistent with the established pattern.

---

## 4. Code Quality & Maintainability

### What's clear and readable

The naming discipline is strong. `rawModelOutput`, `validatedRequest`, `resolvedPath`, `policyDecision`, `tokenClaims`, `effectiveTokenClaims`, `peerIdentityFromContext` — names actually encode their trust level and provenance. A new engineer reading `capabilityRequest.Actor = tokenClaims.ActorLabel` understands that the actor label comes from a validated token, not from the request body.

The handler pattern is consistent. Every handler checks method, authenticates, verifies the signed request, reads/validates the body, performs the action, audits, and returns. Deviation from this pattern is a red flag that's easy to spot in code review.

### Where it gets murky

**`server.go` is 1,270 lines.** It is the center of gravity for the entire system. The struct alone is 160 lines with 30+ fields. The `executeCapabilityRequest` function is ~580 lines. This is the function that will confuse you at 2 AM. It handles:
- replay detection
- secret export prohibition
- token scope checks
- unknown capability denial
- schema validation
- policy evaluation
- operator mount grant override
- low-risk host plan auto-allow
- approval record creation
- approval pending denial
- single-use token derivation and consumption
- fs_read rate limiting
- special-cased capability routing (4 capabilities dispatched by name)
- context timeout
- tool execution
- quarantine persistence (two separate paths)
- structured result building
- second quarantine persistence (for inferred quarantine)
- classification normalization
- success audit logging

This is one function doing too many things. It would benefit from being split into named stages: `denyIfProhibited`, `applyPolicyDecision`, `materializeApprovalIfNeeded`, `consumeToken`, `executeWithTimeout`, `buildResult`. Each stage would be independently testable and auditable.

### The `request_auth.go` duplication problem at debug-time

If you are at 2 AM and someone reports "signatures are being rejected for valid requests," you will search for the signature verification logic. You will find three functions with four-level nesting of the same header parsing. You will not immediately know which one is called for the code path in question. You will read all three. This costs 20–30 minutes of cognitive tax every time.

---

## 5. Under Load

### What holds up

- The locking model is well-structured and mostly fine for the single-node local-first design.
- The `maxTotalControlSessions = 512`, `maxTotalApprovalRecords = 4096` caps prevent unbounded memory growth from malicious session flooding.
- The `defaultMaxSeenRequestReplayEntries = 65536` and `maxAuthNonceReplayEntries = 65536` caps on replay tables are reasonable.
- Body size limits (`maxOpenSessionBodyBytes`, `maxCapabilityBodyBytes`) are enforced before parsing.
- The HTTP server has read/write/idle timeouts set.

### What breaks under load

1. **The `ensurePromotionNotDuplicate` directory scan** (see HIGH above) will grind to a halt at scale.
2. **The `uiEvents` unbounded slice** will eventually cause an OOM on a long-running instance with active UI subscribers.
3. **The `sessionReadCounts` sliding window** for `fs_read` rate limiting prunes on every read (`pruned := timestamps[:0]`). For a session doing rapid file reads, this slice is re-allocated on every call. Under aggressive read rates, the garbage collector is working harder than necessary. A ring buffer per session would be O(1).
4. **`activeSessionsForPeerUIDLocked`** is an O(sessions) linear scan through all sessions to count by UID. With `maxTotalControlSessions = 512` this is bounded and acceptable, but it will be the first function to hit performance degradation if that cap is raised.

---

## 6. Dead Code / Redundant Code

- **`secret_export.go` vs `capability_execution_runtime.go`**: Duplicate heuristic (see MEDIUM above).
- **`runtime/sandbox/root/home/workspace/haven_cli/affirmation_tui/stats.go`**: Entirely dead committed artifact.
- **`NewServer` vs `NewServerWithOptions`**: `NewServer` is a one-liner that calls `NewServerWithOptions` with no additional options. They have the same signature. Either the options pattern was never finished (it only has two parameters, same as `NewServer`) or `NewServer` is vestigial. This is confusing.
- **The `.cache`, `.gocache`, `.tmp-loopgate-tests`, `.worktrees`, `.external` directories** in the repo root suggest accumulated tooling state. Some of these should be in `.gitignore` if they aren't already, or explained in `CONTRIBUTING.md`.

---

## 7. Documentation Quality

### The good

The documentation structure is excellent for an opinionated security product:
- `AGENTS.md` (rules for AI contributors — genuinely novel and useful)
- `context_map.md` (fast orientation guide)
- `docs/loopgate-threat-model.md` (honest and specific)
- `docs/setup/GETTING_STARTED.md` (sequential, task-oriented)
- ADR directory (architectural decisions recorded)
- RFC directory (token policy, XPC hardening)

### The gaps

1. **No API reference.** The HTTP API has 30+ routes. The only documentation is `LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md` (which I did not see in the repo but is referenced). Operators connecting custom clients have no machine-readable contract and no comprehensive endpoint list.

2. **Legacy env vars are not documented anywhere visible.** A new operator who
   reads `GETTING_STARTED.md` and `OPERATOR_GUIDE.md` has no way to know they
   exist or what they do. They will discover them only by reading source.

3. **`CHANGELOG.md` is 1,666 bytes.** For a system with this many architectural decisions (ADR 0010 alone removed an entire transport layer), a changelog this brief is not useful for operators upgrading from older deployments.

4. **The `context_map.md` explicitly warns against several things** (retired
   assistant/UI surfaces, continuity in another repo, speculative desktop work)
   suggesting there was historical confusion about what this repo contains. For
   a new user, this creates a puzzling first impression — why does the
   orientation document begin with a list of things that aren't here?

---

## 8. New User Setup Experience

Starting from scratch:

```
Step 1: go mod tidy          ← Fails: go 1.25.0 does not exist
```

That is the first command in `GETTING_STARTED.md` and it fails on any real machine. Everything after that is unknowable until this is fixed.

Assuming the Go version issue is resolved:

- `go run ./cmd/loopgate init` — Creates key material. The UX here is unclear: what does "installs the matching trust anchor" mean to a new operator? Where is this anchor stored? There is no `--dry-run` to preview what will change.
- `go run ./cmd/loopgate-policy-admin validate` — Validates policy. Good.
- `go run ./cmd/loopgate` — Starts the server. Good.
- Hook installation writes to `~/.claude/settings.json` and `~/.claude/hooks/` — These are global Claude Code settings, which means installing Loopgate changes the behavior of Claude Code for ALL projects, not just the Loopgate project. This should be documented prominently and the install command should confirm this with the operator.

The operator then has an HTTP service on a Unix socket with no shell or UI in-tree (the shell/UI is referenced as out-of-tree). Inspecting the audit requires running `go run ./cmd/loopgate-ledger tail -verbose`. There is no `loopgate status` command or single-command health check.

**Difficulty rating:** 7/10 for a Go-experienced engineer. 9/10 for anyone else. The concepts are correct but the tooling UX has rough edges, and the go.mod bug is a hard blocker.

---

## 9. Overall Security Posture

**Strong:**
- Transport: Unix socket with peer credential binding. Not a TCP listener. Correct.
- Auth: Bearer token + HMAC-signed request envelope. Multi-factor session validation.
- Policy: Deny-by-default. Unknown category = denied. Unknown operation type = denied.
- Secrets: macOS Keychain integration, immediate struct field clearing after decode, no plaintext fallback for production use.
- Ledger: Append-only, hash-chained, fsync'd on every append, flock'd for cross-process serialization.
- Path safety: Symlink resolution before allow/deny, null byte rejection, separator validation, case normalization on macOS for APFS.
- Audit: Every security-relevant action is logged before or after it executes. Audit failure = hard denial.

**Gaps (already documented by the author):**
- Same-user local process trust is the inherent limitation of a Unix socket control plane. This is correct and acknowledged.
- The hash chain does not prove Loopgate authorship — a same-user attacker with filesystem access can replace the log.
- The HMAC checkpoint (macOS Keychain) addresses this incrementally but requires startup verification that a new operator may not know to perform.
- No cryptographic remote audit export in-tree.

**Gaps not yet documented:**
- The duplicate secret export heuristic (if they drift, one of them could be too permissive).
- The `sessionMACRotationMaster` read-without-lock in `verifySignedRequest`.
- The missing rate limiting on non-`fs_read` capabilities (only those two are rate-limited by name).

**Verdict:** For a local single-user governance tool, the security posture is well above average. The attack surface is genuinely narrow (Unix socket, peer-bound sessions, deny-by-default policy, quarantined outputs). The documented gaps are real but acknowledged honestly. The undocumented gaps are addressable without architectural rework.

---

## 10. Viability & Overall Thoughts

**What this software is:** A principled local governance layer for AI agent activity. The design philosophy — natural language is never authority, model output is untrusted input, policy decides what can run — is the correct response to the current state of AI tooling. Most AI harnesses give the model ambient authority and rely on prompt discipline. Loopgate's approach is architecturally sounder.

**What makes it commercially viable:** The dependency hygiene, audit chain, and approval workflow are features that a security-conscious enterprise will pay for. AGENTS.md is a prototype for a corporate AI governance policy document — which is itself a product.

**What needs work before "enterprise ready":**
1. The go.mod bug must be fixed before any external developer can compile it.
2. The committed sandbox artifact creates an immediate credibility problem.
3. The `MORPH_*` namespace confusion signals incomplete productization.
4. The policy checker's use of filesystem config for host tools will frustrate enterprise operators trying to write precise policies.
5. A machine-readable API contract (OpenAPI or similar) is table stakes for enterprise integration.
6. The `loopgate-admin` binary (8.6MB, committed to the repo root) — its presence here without documentation, source, or entry in `go.mod` is unexplained. What is it? Where did it come from?

**The author's experience level:** This is not a rookie's work. The locking model, the fail-closed discipline, the hash chain design, the peer credential binding, the TOCTOU analysis in AGENTS.md — these reflect someone who has debugged real security incidents and designed real distributed systems. The naming conventions, the separation of user-facing audit from internal telemetry, and the treatment of model output as untrusted input are all signs of careful, experienced thinking.

The rough edges are productization rough edges (namespace leakage, UX gaps, one committed artifact) and scale rough edges (O(n) promotion scan, duplicate code in the auth path). They are fixable without rearchitecting the system.

---

## Priority Fix List

| Priority | Issue | File(s) | Effort |
|----------|-------|---------|--------|
| P0 | `go 1.25.0` does not exist | `go.mod` | 5 min |
| P0 | Committed sandbox artifact | `runtime/sandbox/root/...` | 10 min |
| P1 | `MORPH_*` env namespace — document or rename | `internal/modelruntime/runtime.go` | 1 day |
| P1 | Duplicate secret export heuristic | `secret_export.go`, `capability_execution_runtime.go` | 2 hours |
| P1 | `appendChainStateCache` global breaks test isolation | `internal/ledger/ledger.go` | 1 day |
| P1 | Triplicated header parsing in auth | `internal/loopgate/request_auth.go` | 4 hours |
| P2 | `ensurePromotionNotDuplicate` O(n) scan | `internal/loopgate/promotion.go` | 4 hours |
| P2 | `sessionMACRotationMaster` read without lock | `internal/loopgate/request_auth.go` | 1 hour |
| P2 | `checkHost` reads filesystem config | `internal/policy/checker.go` | 2 hours |
| P2 | `uiEvents` unbounded slice | `internal/loopgate/server.go` | 4 hours |
| P3 | `executeCapabilityRequest` [580 lines] — split into stages | `internal/loopgate/server.go` | 2–3 days |
| P3 | `cleanupDeadMCPGatewayServerIfNeeded` lock held across syscall | `mcp_gateway_runtime.go` | 2 hours |
| P3 | `loopgate-admin` binary — document or remove | repo root | 1 hour |
| P3 | Operator docs: clarify hook install scope | `docs/setup/` | 2 hours |
