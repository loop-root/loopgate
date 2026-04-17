**Last updated:** 2026-04-17

# Loopgate Ship-Readiness Hardening Follow-Up

This document is the follow-up hardening plan after the 2026-04-16 review
closure pass.

It does not reopen everything ever mentioned in a review. It captures the
remaining security, determinism, recovery, and stability findings that are:

- still present in current code, or
- intentionally deferred but worth landing before a public tag if time allows

Use this plan together with:

- [`../reports/reviews/2026-04-16/review_status.md`](../reports/reviews/2026-04-16/review_status.md)
- [`loopgate_v1_hardening_plan.md`](./loopgate_v1_hardening_plan.md)
- [`release_candidate_checklist.md`](./release_candidate_checklist.md)

## Scope

This plan stays inside the current Loopgate product boundary:

- local HTTP control plane on a Unix domain socket
- signed policy
- approvals and denials
- append-only local audit
- Claude Code hook governance
- governed MCP broker execution

It does not reopen:

- removed model/runtime surfaces
- future remote or enterprise transport work
- continuity or memory features that now live outside this repo

## Working rules

- Preserve Loopgate authority and audit invariants first.
- Prefer small PR-sized slices over ambitious cleanup bundles.
- Do not reopen findings marked closed or stale unless current code proves they
  are still present.
- Do not trade away determinism or observability to reduce code size.
- Update tests and operator docs in the same slice when behavior changes.

## Current code-verified open findings

These findings were re-verified against the current tree before writing this
plan:

- `internal/loopgate/control_plane_state.go`
  - `appendOnlyNonceReplayStore.Compact(...)` is still a no-op

The release-closure doc also still intentionally defers:

- fuzzing for JSON / YAML parsers
- build-tagged end-to-end integration testing
- policy-sign coverage gate in CI
- metrics / saturation surfacing
- full `internal/loopgate` package breakup

## Release triage

### Must land before a public tag

- local packaging hygiene:
  - remove ignored root `loopgate-admin`
  - remove ignored local `output/` clutter if unused

### Strongly preferred before release candidate

- nonce replay log growth handling or operator-visible saturation signal
- audit-chain crash-recovery integration coverage
- parser fuzzing for JSON / YAML entry paths
- policy-sign coverage gate in CI

### Defer unless a real workflow is blocked

- full `internal/loopgate` package breakup
- audit throughput improvements
- metrics / saturation surfaces for enterprise-style load
- replacing string-based fs-read dispatch with registry metadata
- removing the legacy nonce snapshot fallback
- Pi adapter and broader remote / enterprise follow-through

## Phase 0: Canonical Tracking

Status: **completed**

Focus:
- keep one authoritative follow-up list so we do not reopen stale review noise

Planned work:
- record each remaining item as:
  - `pre-tag must land`
  - `pre-RC preferred`
  - `defer`
- map each item to:
  - owner
  - file set
  - tests to add
  - docs to update

Acceptance criteria:
- every remaining review finding has one status
- no duplicate work streams are open for the same issue
- stale findings are explicitly marked stale instead of silently ignored

Rollback:
- not applicable; planning-only phase

## Phase 1: Determinism And Persistence Correctness

Status: **completed**

Focus:
- close correctness bugs that can weaken audit trust or produce unstable
  behavior under normal use

Planned work:
- make audit-event hashing deterministic in:
  - `internal/ledger/ledger.go`
  - `internal/loopgate/server_audit_runtime.go`
- remove or narrow the JSON round-trip in
  `canonicalizeAuditData(...)` so the hot audit path no longer depends on a
  lossy marshal/unmarshal cycle
- replace the fixed temp path in `saveConnectionRecords(...)` with a unique temp
  file plus atomic rename
- replace the aliasing prune pattern in `checkFsReadRateLimit(...)` with a fresh
  destination slice

Recommended implementation direction:
- keep the on-disk event schema stable if possible
- make deterministic hashing explicit rather than relying on Go map behavior
- reuse the repo's existing atomic-write pattern where it already exists

Security impact:
- strengthens the audit-integrity story by making hash behavior explicit
- closes a local state-write race in connection persistence

Concurrency impact:
- removes an avoidable aliasing hazard in the fs-read throttle path
- reduces the chance of conflicting writers on connection-state temp files

Recovery impact:
- keeps connection-state writes crash-safe and less ambiguous

Tests to add:
- deterministic hash regression tests for both ledger and audit hashing paths
- connection persistence test covering concurrent or repeated saves without temp
  path collision
- fs-read rate-limit regression test covering prune + append behavior

Acceptance criteria:
- the audit hash is stable for semantically identical events
- connection-state writes no longer share a fixed temp filename
- the fs-read limiter no longer mutates through a shared backing slice

Completed in this phase:
- centralized chain-event canonicalization in `internal/ledger` so hash
  computation and stored ledger bytes use the same normalized event shape
- replaced the audit-specific `canonicalizeAuditData(...)` round-trip with the
  shared ledger canonicalization path
- added regression coverage proving event hashes stay stable across different
  map insertion orders for both ledger and audit events
- connection-state temp-file race fix
- fs-read rate-limit slice-alias cleanup

## Phase 2: Secret And Hook-Surface Hardening

Status: **completed**

Focus:
- close obvious secret-redaction gaps and reduce easy control-plane saturation

Planned work:
- teach `internal/secrets/redact.go` to redact `[]byte` values before they can
  leak into audit-visible structures
- add a per-UID rate limiter for `/v1/hook/pre-validate` in:
  - `internal/loopgate/server_hook_handlers.go`
  - supporting control-plane state under `internal/loopgate/`

Recommended implementation direction:
- treat `[]byte` as sensitive-by-default content and redact it before any JSON
  encoding path
- keep the hook rate limiter simple, synchronous, and in-memory
- prefer an explicit denial with a stable code over silent slowdown

Security impact:
- reduces the chance that secret-bearing byte payloads survive redaction
- reduces easy local hammering of the unauthenticated hook validation surface

Concurrency impact:
- introduces bounded shared state for hook counters; keep it under existing lock
  discipline

Recovery impact:
- none beyond normal in-memory limiter reset on process restart

Tests to add:
- redaction tests proving `[]byte` never reaches persisted audit structures
- hook rate-limit tests:
  - below threshold allowed
  - above threshold denied
  - counts isolated by peer UID

Acceptance criteria:
- no raw `[]byte` values survive the redaction path
- hook storms fail closed with a clear denial instead of saturating the server

Completed in this phase:
- redacted raw `[]byte` and `json.RawMessage` values before structured audit
  persistence paths can encode them
- added a per-peer-UID `PreToolUse` hook limiter that returns
  `hook_rate_limit_exceeded` through the existing JSON hook contract
- added regression coverage proving the limiter blocks repeated `PreToolUse`
  requests while leaving audit-only hook events like `SessionStart` untouched

## Phase 3: Replay, Recovery, And Operator Visibility

Status: **completed**

Focus:
- make replay persistence and startup hardening more supportable without hiding
  limits

Planned work:
- decide the nonce replay log strategy:
  - real compaction, or
  - explicit operator-visible size / age warning if compaction remains deferred
- add doctor/report surfacing for nonce replay growth or saturation risk
- harden socket creation posture with umask tightening before listen
- add audit-chain crash-recovery integration coverage beyond unit-level tests

Recommended implementation direction:
- do not add background daemons or hidden cleanup workers
- if compaction remains deferred, surface that honestly in operator tooling
- keep any new startup hardening explicit and documented

Security impact:
- strengthens local socket hygiene
- reduces the chance of replay-state surprises after long-running use

Recovery impact:
- improves restart confidence for audit and replay persistence

Tests to add:
- replay-store load/compact or warning-path tests
- socket-creation tests for hardened file-mode expectations where practical
- crash-recovery integration test for audit verification after interrupted write
  scenarios

Acceptance criteria:
- replay-store growth is either bounded or clearly visible to the operator
- socket creation follows a stricter local-only posture
- crash-recovery behavior is verified, not merely described

Completed in this phase:
- tightened Unix socket creation with a temporary `077` umask during listen so
  the socket does not briefly land with broader ambient create permissions
  before the final `0600` mode is enforced
- added `nonce_replay` doctor/report diagnostics exposing active retained
  entries, utilization against the current fail-closed cap, persisted log size,
  and warning states for legacy snapshot fallback, high retained-entry
  utilization, or visible append-only log growth
- added integration coverage proving a crash-shaped truncated audit tail is
  surfaced by doctor/report as a ledger-integrity failure and blocks server
  restart fail-closed on the same workspace

## Phase 4: Verification Gates

Status: **completed**

Focus:
- increase confidence that the current trust model holds under malformed input
  and full-path exercise

Planned work:
- add fuzz tests for JSON / YAML parser entry paths
- add a build-tagged end-to-end integration test for one governed workflow
- add a policy-sign coverage gate in CI
- make sure the CI path exercises the same `ship-check` expectations we use
  locally

Recommended implementation direction:
- keep fuzz targets narrow and deterministic enough for CI budgets
- choose one real governed workflow for end-to-end coverage rather than trying
  to simulate the whole product

Security impact:
- better malformed-input coverage on trust-boundary parsers

Recovery impact:
- higher confidence that a supported operator flow still works after hardening

Tests to add:
- parser fuzz targets
- one end-to-end signed-policy + approval + audit workflow
- CI assertion that policy-sign coverage remains above the chosen floor

Acceptance criteria:
- CI catches the main parser regressions and one real governed-flow regression
- policy-sign test depth is enforced instead of aspirational

Completed in this phase so far:
- re-ran `go test -race -count=1 ./...` on the current tree and confirmed it is
  green before adding more verification gates
- expanded `cmd/loopgate-policy-sign` tests to cover real sign, `-verify-setup`,
  and operator-guidance error paths instead of leaving the CLI largely
  unexercised
- added a `policy-sign-coverage-check` gate to local `ship-check` and GitHub
  Actions, enforcing a first-pass 60% combined coverage floor across
  `cmd/loopgate-policy-sign` and `internal/config`
- added narrow fuzz targets for one signed-policy YAML parser and one
  capability-request JSON parser so malformed input coverage starts at real
  trust boundaries instead of helper-only code
- added an opt-in `e2e` build-tagged integration test for one signed-policy +
  approval + audit workflow, with `make test-e2e` as the stable rerun command
- expanded CI automation so tracked PR/push CI now runs the tagged e2e flow,
  while a scheduled/manual nightly workflow runs fuzz smoke and a macOS
  ship-readiness smoke pass

## Phase 5: Post-Tag Structural Follow-Through

Status: **not started**

Focus:
- pay down real architecture debt without confusing it with ship blockers

Planned work:
- continue `internal/loopgate` decomposition in small concern-based slices
- revisit metrics / saturation surfacing
- revisit audit throughput if real operator use shows pressure
- remove legacy fallback paths only after replacement coverage is strong

Acceptance criteria:
- each structural PR preserves behavior and keeps authority boundaries explicit

## Recommended execution order

Open these as separate slices, in order:

1. deterministic audit hashing + canonicalization cleanup
2. `[]byte` redaction + fs-read slice alias fix
3. connection-state temp-file race fix
4. hook endpoint rate limiting
5. replay-store visibility / compaction decision
6. socket umask hardening
7. fuzz + end-to-end + coverage gates

Do not start with package breakup. It is real work, but it is not the highest
security return before ship.
