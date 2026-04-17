# Loopgate — Senior Engineer Production Readiness Review

**Reviewer posture:** 30-year senior Go engineer and security researcher  
**Codebase snapshot:** `v0.1.0` + 2 commits, 274 Go files, ~69K lines, 104 test files (~27.5K test lines)  
**Date:** 2026-04-16  

---

## Executive Summary

This is an *unusually disciplined* codebase for its stage. The security invariants are not afterthoughts — they are structural. The dependency diet is exemplary (4 direct deps). The audit ledger is genuine append-only with hash-chain integrity. The auth model uses layered defense (peer creds → bearer token → session binding → HMAC-signed requests → nonce replay protection) that most production systems never reach.

That said, there are real findings that would bite you under load, at 2 AM, or during a security incident. I've organized them by severity.

---

## Table of Contents

1. [Security Posture — Strong](#1-security-posture--strong)
2. [What Will Break Under Load](#2-what-will-break-under-load)
3. [What Will Confuse You at 2 AM](#3-what-will-confuse-you-at-2-am)
4. [Dead and Redundant Code](#4-dead-and-redundant-code)
5. [Architecture Observations](#5-architecture-observations)
6. [Dependency Audit](#6-dependency-audit)
7. [Documentation Quality](#7-documentation-quality)
8. [New User Setup Experience](#8-new-user-setup-experience)
9. [Where This Could Be More Viable](#9-where-this-could-be-more-viable)
10. [Overall Verdict](#10-overall-verdict)

---

## 1. Security Posture — Strong

### What's correct and impressive

**Layered authentication** — The auth stack in [request_auth.go](file:///Users/adalaide/dev/loopgate/internal/loopgate/request_auth.go) is textbook defense-in-depth:

1. Unix socket peer credential binding (UID/PID/EPID)
2. Bearer token with peer-identity pinning
3. Control session binding via `X-Loopgate-Control-Session`
4. HMAC-SHA256 request signatures covering `method + path + session + timestamp + nonce + body_hash`
5. Nonce replay detection with fail-closed saturation behavior
6. Timestamp skew enforcement (±2 minutes)

The TOCTOU fix at [request_auth.go:88-109](file:///Users/adalaide/dev/loopgate/internal/loopgate/request_auth.go#L88-L109) — taking a `now()` snapshot under the lock and performing all expiry checks inside a single critical section — is exactly correct.

**Path safety** — [safepath.go](file:///Users/adalaide/dev/loopgate/internal/safety/safepath.go) resolves symlinks before allow/deny checks, rejects null bytes, handles macOS NFC/NFD normalization and case-insensitive comparisons, and fails closed on unresolvable deny rules. The `resolvePathStrict` function correctly refuses to walk above the immediate parent.

**Sandbox `O_NOFOLLOW` / `openat(2)` walking** — [sandbox.go:546-584](file:///Users/adalaide/dev/loopgate/internal/sandbox/sandbox.go#L546-L584) does component-by-component directory descent using `unix.Openat` with `O_NOFOLLOW | O_CLOEXEC`, which is the correct POSIX way to prevent symlink-following TOCTOU races. This is significantly more careful than most Go codebases.

**Audit ledger integrity** — The [ledger](file:///Users/adalaide/dev/loopgate/internal/ledger/ledger.go) is append-only with hash-chain linking (`SHA-256(event sans event_hash)`), monotonic sequence enforcement, and file-level `flock` serialization. The segmented rotation in [segmented.go](file:///Users/adalaide/dev/loopgate/internal/ledger/segmented.go) maintains manifest-level hash chains across segment boundaries.

**Secret redaction** — [redact.go](file:///Users/adalaide/dev/loopgate/internal/secrets/redact.go) covers bearer tokens, basic auth, JWT patterns, URL query parameters, and structured field keys. The `CapabilityRequest.MarshalJSON` defense-in-depth at [types.go:278-295](file:///Users/adalaide/dev/loopgate/internal/loopgate/types.go#L278-L295) strips provider-echo metadata on serialization — nice touch.

### Security findings to address

#### Finding S-1: `verifySignedRequest` duplicates header parsing (Medium)

[request_auth.go:234-287](file:///Users/adalaide/dev/loopgate/internal/loopgate/request_auth.go#L234-L287) re-parses all four signed headers, re-checks timestamp skew, and re-validates control session binding — all of which `parseSignedControlPlaneHeaders` already does. Then `verifySignedRequestWithMACKey` (line 289) does it a **third time**. Three separate timestamp parses of the same string in one call chain is a maintenance hazard. A future editor will fix a bug in one copy and miss the other two.

#### Finding S-2: `secretExportCapabilityNameHeuristic` is a brittle heuristic (Low)

[secret_export.go:10-33](file:///Users/adalaide/dev/loopgate/internal/loopgate/secret_export.go#L10-L33) uses prefix/substring matching like `"secret."`, `"token."`, `"key."`. The AGENTS.md correctly notes this is a defense-in-depth guard, not the primary boundary. But `"key."` is overly broad — any capability named `key.view`, `keyboard.send`, or `monkey.wrench` would be caught. Consider migrating to an explicit allowlist against the registered capability set as the AGENTS.md suggests.

#### Finding S-3: `hmac.Equal` vs `subtle.ConstantTimeCompare` inconsistency (Low)

[request_auth.go:343](file:///Users/adalaide/dev/loopgate/internal/loopgate/request_auth.go#L343) uses `hmac.Equal`. [session_mac_rotation.go:227](file:///Users/adalaide/dev/loopgate/internal/loopgate/session_mac_rotation.go#L227) uses `subtle.ConstantTimeCompare`. Both are constant-time, but `hmac.Equal` internally calls `subtle.ConstantTimeCompare` and also checks lengths. Pick one pattern.

---

## 2. What Will Break Under Load

### Finding L-1: `pruneExpiredLocked` is O(n) over all maps on every request (High)

[control_plane_state.go:27-130](file:///Users/adalaide/dev/loopgate/internal/loopgate/control_plane_state.go#L27-L130) sweeps through `tokens`, `sessions`, `approvals`, `mcpGatewayApprovalRequests`, `seenRequests`, `seenAuthNonces`, and `usedTokens` on every auth check. With `maxSeenRequestReplayEntries = 65536` and `maxAuthNonceReplayEntries = 65536`, a saturated server will iterate ~131K entries while holding `server.mu`. The 250ms sweep interval throttle helps, but under burst load, the first request after the interval pays the full O(n) cost.

**Impact:** At high request volume, every authenticated request that touches `server.mu` will compete for lock time with expiry sweeps iterating 100K+ map entries.

**Fix:** Use a time-ordered data structure (min-heap or ordered list) so you only sweep entries that are actually expired, rather than visiting every entry.

### Finding L-2: `countPendingApprovalsForSessionLocked` is O(total approvals) (Medium)

[control_plane_state.go:422-433](file:///Users/adalaide/dev/loopgate/internal/loopgate/control_plane_state.go#L422-L433) linearly scans all approvals to count those belonging to one session. With `maxTotalApprovalRecords = 4096`, this is fine. But it's called from the approval-creation path, which already holds `server.mu`. If approval limits are ever raised, this becomes a hot path.

### Finding L-3: Audit ledger `flock` serialization is a throughput bottleneck (Medium)

Every event append acquires an exclusive file lock ([ledger.go:94](file:///Users/adalaide/dev/loopgate/internal/ledger/ledger.go#L94), [segmented.go:627-638](file:///Users/adalaide/dev/loopgate/internal/ledger/segmented.go#L627-L638)), reads the entire file to verify chain state (if cache misses), seeks to end, writes, and fsyncs. Under high event volume:

- Cache misses (after rotation or restart) require a full file scan
- `fsync` on every event is durable but expensive

For a local single-user system this is perfectly acceptable. For enterprise multi-session load, batch writes or buffered appending would be needed.

### Finding L-4: `sessionReadCounts` slice grows unbounded within the rate window (Low)

[server.go:1168-1189](file:///Users/adalaide/dev/loopgate/internal/loopgate/server.go#L1168-L1189) stores timestamps as a slice and prunes in-place. In the worst case, 60 timestamps per minute per session — fine. But if `defaultFsReadRateLimit` were ever raised significantly, this would allocate more than necessary since pruning only happens at check time.

---

## 3. What Will Confuse You at 2 AM

### Finding D-1: The `Server` struct is 146 lines of fields (High)

[server.go:40-146](file:///Users/adalaide/dev/loopgate/internal/loopgate/server.go#L40-L146) is a 106-field struct with 11 separate mutexes (`mu`, `auditMu`, `auditExportMu`, `promotionMu`, `uiMu`, `claudeHookSessionsMu`, `connectionsMu`, `modelConnectionsMu`, `hostAccessPlansMu`, `policyRuntimeMu`, `pkceMu`, `providerTokenMu`). At 2 AM, the question "which mutex protects which field?" requires cross-referencing dozens of files.

**What would help:** Document the mutex-to-field mapping with comments directly on the struct, or extract sub-structs with their own locks (e.g., `type sessionState struct { mu sync.Mutex; sessions map[...]; tokens map[...]; ... }`).

### Finding D-2: `executeCapabilityRequest` is a 580-line function (High)

[server.go:544-1125](file:///Users/adalaide/dev/loopgate/internal/loopgate/server.go#L544-L1125) handles validation, replay detection, secret-export prohibition, capability-token scope, tool lookup, schema validation, policy evaluation, operator-mount grant override, low-risk auto-allow, approval creation, execution token consumption, rate limiting, host-folder dispatching, capability execution, quarantine, result classification, and audit logging — all in one method. This is the single hardest function to debug in the codebase.

### Finding D-3: Approval state machine is implicit (Medium)

Approval states (`pending`, `expired`, `granted`, `denied`, `consumed`) are string constants scattered across files without a formal state machine definition or transition validation. At 2 AM, answering "can a `denied` approval transition to `consumed`?" requires reading every function that writes to the `State` field.

### Finding D-4: `normalizeCapabilityRequest` silently strips echoed fields (Low)

[types.go:267-273](file:///Users/adalaide/dev/loopgate/internal/loopgate/types.go#L267-L273) accepts optional echoed provider-native fields (`ToolName`, `tool_name`, `toolName`, `ToolUseID`, etc.) and silently strips them. This is documented as defense-in-depth, which is correct. But if a bug ever causes the wrong field to be used as the capability name, the silent stripping means you'll never see it in the audit trail.

---

## 4. Dead and Redundant Code

### Finding R-1: `loopgate-admin` binary is committed to the repo (High)

The root directory contains an 8.3MB compiled Mach-O binary (`loopgate-admin`). While `.gitignore` has `/loopgate-admin`, the file exists in the repo. Binary artifacts should not be committed — they balloon `.git` history and won't run on other architectures.

### Finding R-2: `runtime/sandbox/root/home/workspace/haven_cli/affirmation_tui/stats.go` (Medium)

This file sits inside the sandbox root directory and appears to be a remnant from a prior project (`haven_cli`). It's a working Go file with its own package declaration. It shouldn't be in the repo — it gets included in `find` results and potentially confuses tooling.

### Finding R-3: Duplicated header validation across `verifySignedRequest` functions (Medium)

As noted in S-1, the control-session ID check, timestamp parse, and skew validation are performed in:
1. `parseSignedControlPlaneHeaders` (L194-232)
2. `verifySignedRequest` (L234-287)
3. `verifySignedRequestWithMACKey` (L289-356)

These should be factored into a single parse-and-validate step with the MAC verification as the final step.

### Finding R-4: `snapshotNonceReplayStore` is legacy but still reachable (Low)

[control_plane_state.go:164-216](file:///Users/adalaide/dev/loopgate/internal/loopgate/control_plane_state.go#L164-L216) — The `snapshotNonceReplayStore` exists as a fallback for the legacy JSON snapshot format. The `appendOnlyNonceReplayStore` (the current default) falls back to it when the JSONL file doesn't exist. Consider whether this migration path is still needed or can be removed.

### Finding R-5: The `output/` and `bin/` directories are present but gitignored and empty (Low)

These appear to be build artifact directories that shouldn't be in the repo tree at all.

---

## 5. Architecture Observations

### What's done well

**Dependency injection throughout** — The `Server` struct injects `now()`, `appendAuditEvent`, `resolveSecretStore`, `reportResponseWriteError`, `resolvePeerIdentity`, `resolveExePath`, `processExists`, and `newModelClientFromConfig`. This makes the server fully testable without filesystem or process dependencies.

**Policy-as-code with signed verification** — Policy is loaded from YAML with Ed25519 detached signatures. The README explicitly warns against loading stale `policy.json` from runtime state. This is production-correct thinking.

**Clear package boundaries** — Internal packages are well-separated: `safety`, `sandbox`, `secrets`, `ledger`, `policy`, `tools`, `model`, `config`, `troubleshoot`. No circular dependencies visible.

**Fail-closed everywhere** — Saturation of replay maps → denial, not eviction. Audit append failure → request rejection, not log-and-continue. Unresolvable path → denial, not fallback. This is the correct posture for a governance system.

### What could be better

**Global mutable state in `ledger` package** — The `appendChainStateCache` at [ledger.go:33-38](file:///Users/adalaide/dev/loopgate/internal/ledger/ledger.go#L33-L38) and `syncLedgerFileHandle` at [ledger.go:42-44](file:///Users/adalaide/dev/loopgate/internal/ledger/ledger.go#L42-L44) are package-level mutable variables. The cache is fine for performance, but the `syncLedgerFileHandle` variable used as a test seam means any test that overrides it affects all tests in the same process if not properly restored. The cleanup function (`useLedgerFileSyncForTest`) mitigates this, but it's fragile under `t.Parallel()`.

**`internal/loopgate/` is a God package** — 106 files, ~40K lines in one package. This makes code navigation hard and compilation slow. The handler files are well-named (`server_*_handlers.go`, `server_*_runtime_test.go`), but they all share the same namespace and all access the `Server` struct directly.

**No structured logging** — Error reporting uses `fmt.Fprintf(os.Stderr, ...)` in several places. The `loopdiag` package provides structured diagnostic logging, but not all code paths use it. For a governance system, every denial should be emittable as structured telemetry.

---

## 6. Dependency Audit

```
go.mod dependencies:
  github.com/chzyer/readline v1.5.1   — Interactive CLI
  golang.org/x/sys v0.42.0            — Unix syscalls (openat, flock, peercred)
  golang.org/x/text v0.23.0           — Unicode normalization (NFC/NFD)
  gopkg.in/yaml.v3 v3.0.1             — YAML config parsing

Indirect:
  github.com/kr/text v0.2.0           — Via check.v1
  github.com/niemeyer/pretty v0.0.0   — Via check.v1
  gopkg.in/check.v1 v1.0.0            — Test dependency
```

**Verdict: Excellent.** Four direct dependencies, all justified:
- `readline` for the interactive shell
- `x/sys` for low-level Unix primitives unavailable in stdlib (`Openat`, `Flock`, `SO_PEERCRED`)
- `x/text` for Unicode normalization (macOS APFS stores filenames in NFD)
- `yaml.v3` for policy/config parsing

No web frameworks, no ORM, no logging libraries, no dependency injection frameworks. This is exactly correct for a security-sensitive system.

> [!TIP]
> `gopkg.in/check.v1` is a test-only dependency — consider removing it in favor of stdlib `testing` patterns to eliminate the `kr/text` and `niemeyer/pretty` indirect deps.

---

## 7. Documentation Quality

### Strengths

- **38KB HTTP API reference** at [LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md](file:///Users/adalaide/dev/loopgate/docs/setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md) — comprehensive
- **26KB threat model** at [loopgate-threat-model.md](file:///Users/adalaide/dev/loopgate/docs/loopgate-threat-model.md) — real threat model, not marketing
- **ADR trail** — 5 ADRs documenting key decisions
- **GETTING_STARTED.md** — 5-step quick path with a Mermaid sequence diagram
- **Policy signing docs** with rotation procedures
- **Ledger integrity docs** explaining what the hash chain does and doesn't guarantee

### Gaps

1. **No architecture overview diagram** — The `context_map.md` exists at the root but there's no visual diagram showing how the server, ledger, sandbox, policy checker, and tools registry interact.

2. **No runbook for incident response** — When the audit chain fails verification, what does the operator do? The `loopgate-doctor` tool exists but isn't documented for "my audit chain is broken" scenarios.

3. **Missing inline documentation on the Server struct** — 11 mutexes and 100+ fields with no doc comments explaining which mutex protects which subset.

4. **CHANGELOG is minimal** — Only 3 entries. For a `v0.1.0` release, the changelog should capture the key decisions and hardening steps.

---

## 8. New User Setup Experience

### Tested flow

```bash
go run ./cmd/loopgate init
go run ./cmd/loopgate-policy-admin validate
go run ./cmd/loopgate
go run ./cmd/loopgate install-hooks
```

### Assessment: Good, with friction

**What works:**
- 4-step setup is genuinely quick
- `loopgate init` handles key generation, trust anchor installation, and policy signing in one command
- Default socket path (`runtime/state/loopgate.sock`) is sensible
- The `install-hooks` command auto-configures Claude Code integration

**Friction points:**

1. **No `Makefile` or build script** — A new contributor must know to use `go run ./cmd/loopgate`. A `make run`, `make test`, `make build` would lower the bar.

2. **macOS-only default** — The secret store defaults to macOS Keychain (`BackendMacOSKeychain`). On Linux, you get `StubSecureStore` which provides no actual secure storage. The code handles this, but the docs don't warn about it.

3. **`MORPH_REPO_ROOT` vs working directory** — The server infers `repoRoot` from `MORPH_REPO_ROOT` or `os.Getwd()`. If you run from the wrong directory, you get a confusing error about missing policy files.

4. **`go 1.25.0` requirement** — `go.mod` specifies Go 1.25.0, which is bleeding-edge. Many developers won't have this installed. The docs don't mention the Go version requirement.

5. **No containerized setup** — For evaluation purposes, a `Dockerfile` or `docker-compose.yaml` would let someone try it without installing Go.

---

## 9. Where This Could Be More Viable

### Extract the God package

The `internal/loopgate` package has 106 files. Split it:
- `internal/loopgate/auth` — auth middleware, session management, MAC rotation
- `internal/loopgate/approval` — approval state machine, approval handlers
- `internal/loopgate/audit` — audit event emission, export handlers
- `internal/loopgate/sandbox` — sandbox handlers (already partly separate)
- `internal/loopgate/mcpgateway` — MCP gateway handlers

### Add graceful degradation metrics

For enterprise use, you need to know when you're approaching saturation limits (`maxSeenRequestReplayEntries`, `maxTotalControlSessions`, `maxTotalApprovalRecords`). Expose gauge metrics so operators can set alerts before hitting fail-closed walls.

### Consider a `context.Context`-based timeout for `pruneExpiredLocked`

If the sweep takes too long under extreme load, it blocks all authenticated requests. A budget-based approach (sweep at most N entries per invocation) would bound worst-case latency.

### Build a formal state machine for approvals

Define valid transitions in a lookup table and validate transitions at the point of mutation. This eliminates an entire class of "can state X transition to state Y?" bugs.

### Add integration test coverage for the audit chain under crash recovery

The ledger handles partial writes (last line without trailing newline), but I don't see a test that kills the process mid-write and verifies recovery. This is the kind of test that prevents 2 AM incidents.

---

## 10. Overall Verdict

### Scores

| Dimension | Score | Notes |
|---|---|---|
| **Security posture** | 9/10 | Layered auth, fail-closed, TOCTOU-aware path handling, proper MAC verification. Minor heuristic-based weaknesses. |
| **Code quality** | 7.5/10 | Clean, explicit naming, zero TODOs/FIXMEs. God package and 580-line function weigh it down. |
| **Test coverage** | 8/10 | 27.5K lines of tests for 69K lines of code (~40% by line). Tests cover security boundaries, denial paths, and edge cases. Missing crash-recovery tests. |
| **Dependency hygiene** | 10/10 | 4 justified direct deps. No waste. |
| **Documentation** | 7/10 | Threat model, API reference, and setup docs are strong. Missing architecture diagrams and incident runbooks. |
| **Operability** | 6.5/10 | Good CLI tooling (`doctor`, `ledger`). Needs metrics, structured logging on all paths, and saturation alerting. |
| **Production readiness** | 7/10 | Ready for single-user local deployment. Needs work for enterprise multi-session load. |
| **New user experience** | 6.5/10 | Quick setup but friction from missing Makefile, Go version requirement, and macOS-centric defaults. |

### Bottom line

This is **genuinely good security-engineering work**. The invariants described in AGENTS.md are actually enforced in the code — that's rare. The dependency discipline is something most senior engineers don't achieve. The audit ledger with hash-chain integrity is a real differentiator.

The main risks are:
1. **Scaling** — The linear-scan expiry sweep and file-lock-per-event ledger will become bottlenecks under enterprise load
2. **Maintainability** — The God package and 580-line main function will accumulate bugs faster than they should
3. **Onboarding** — The gap between "read the README" and "understand the auth flow" is too wide without architecture diagrams

For a `v0.1.0` from a small team, this is significantly above average. The security posture is where I'd expect a `v1.0` product to be. The code quality and operability are where I'd expect a `v0.3` to be. That's the right prioritization order for a governance system.

> [!IMPORTANT]
> **If I were advising on one thing to do before enterprise deployment:** Factor the `executeCapabilityRequest` function into composable middleware stages. Every bug in that function is a bug in the entire decision pipeline, and right now it's a single 580-line code path with 12 possible exit points. A staged pipeline (validate → authorize → rate-limit → execute → classify → audit) would make each stage independently testable and debuggable.
