**Last updated:** 2026-04-15

# Loopgate V1 Hardening Plan

This document tracks the concrete hardening work that follows the 2026-04-15
review findings.

It is not a general wishlist. It is the implementation roadmap for closing the
remaining V1 security and correctness gaps in the local-first Loopgate core.

## Scope

This plan focuses on the reviewed local-first Loopgate path:

- signed policy
- Unix-socket control plane
- token and request-integrity enforcement
- append-only local audit
- Claude Code hook governance

It does not reopen extracted continuity work, alternate operator surfaces, or
future enterprise transport design.

## Release triage

### Ship blockers

These need to be fixed before a credible V1 / OSS release:

- nonce replay persistence on the authenticated hot path
- missing authoritative audit records for auth-token failures
- unnecessary epoch key material exposure on the wire

Resolved ship-blocker items:

- user-configurable policy signing trust anchor for open-source operators
- wrong denial code for `fs_read` rate limiting
- misleading signed-request parser naming

### Hardening after blocker closure

These are important, but not in the same “fix before V1” bucket:

- HMAC checkpoint default posture and operator surfacing
- replay-window and replay-store sizing review
- policy-sign trust-path cleanup beyond the OSS trust-anchor blocker
- stronger `loopgate-policy-sign` test coverage
- `go test` execution in CI

## Working rules

- Preserve Loopgate authority and audit invariants.
- Prefer smaller, isolated slices over combined refactors.
- Do not widen trust to make a fix easier.
- Make rollback possible for each phase.
- Update docs and tests in the same slice when the contract changes.

## Phase A: Baseline And Blocker Classification

Status: **completed**

Focus:
- capture the current behavior before changing it
- confirm exact blocker scope and affected tests

Planned work:
- record current behavior for:
  - token-auth failure paths
  - nonce persistence across restart
  - `/v1/session/mac-keys` response shape
  - current audit visibility for auth failures
- map each issue to:
  - code files
  - existing tests
  - docs that may need updates

Acceptance criteria:
- each blocker has a concrete file/test/doc map
- compatibility-sensitive behavior is identified before edits begin

Rollback:
- not applicable; read-only phase

Completed in this phase:
- mapped the Phase B slice to:
  - `internal/loopgate/controlapi/core.go`
  - `internal/protocol/capability.go`
  - `internal/loopgate/server.go`
  - `internal/loopgate/request_auth.go`
  - `internal/loopgate/approval_flow.go`
  - `internal/loopgate/server_test.go`
- confirmed the Phase B approval-flow expectations were already covered in `server_test.go`
- added a dedicated regression test for the `fs_read` throttle denial code

## Phase B: Low-Risk Blocker Fixes

Status: **completed**

Focus:
- close the smallest blocker items first

Planned work:
- add `DenialCodeFsReadRateLimitExceeded`
- use it in the `fs_read` and `operator_mount.fs_read` throttle path
- rename `parseAndValidateSignedControlPlaneRequest(...)` to reflect that it does not verify the HMAC
- replace the hardcoded `"8 hours"` approval reason text with the real TTL-derived string

Acceptance criteria:
- denial output is less misleading
- auth helper naming no longer overstates validation
- approval reason text matches the real grant TTL
- no authority-path or persistence behavior changes

Rollback:
- trivial revert; no stored-state migration

Completed in this phase:
- added `DenialCodeFsReadRateLimitExceeded`
- switched the `fs_read` throttle path to the dedicated denial code
- renamed `parseAndValidateSignedControlPlaneRequest(...)` to `parseSignedControlPlaneHeaders(...)`
- replaced the hardcoded `"8 hours"` operator-mount approval text with the real TTL-derived string

## Phase C: Policy Signing Trust Anchor Configuration

Status: **completed**

Focus:
- remove the open-source launch blocker where the binary trusts only the compiled-in key

Problem to solve:
- the current docs tell operators to generate their own Ed25519 signer
- the current binary will not trust that signer unless source is modified and rebuilt

Recommended design direction:
- support an operator-local trust-anchor source outside the repo
- keep the compiled-in trust anchor as a fallback, not the only production path
- avoid repo-local trust anchors that would weaken the detached-signature boundary back into ordinary repo editability

Candidate implementation shape:
- add an operator trust-anchor file under the Loopgate config directory
- allow explicit file override for advanced operators
- load:
  - compiled default trust anchor
  - plus optional operator-local trust anchors
- require explicit `key_id` matching, not just “first key wins”

Files likely involved:
- `internal/config/policy_signing.go`
- `internal/config/policy_signing_verify.go`
- `cmd/loopgate-policy-sign/main.go`
- `cmd/loopgate-policy-sign/main_test.go`
- `docs/setup/POLICY_SIGNING.md`
- `docs/setup/GETTING_STARTED.md`

Acceptance criteria:
- a fresh open-source user can generate a signer and a trust anchor without editing source
- the trust anchor does not need to live in the repo checkout
- existing built-in trust behavior still works unless intentionally overridden
- signer/setup verification reflects the new trust-anchor source honestly

Rollback:
- keep the built-in trust anchor path intact while landing the operator-local trust source so the new source can be backed out independently

Completed in this phase:
- added operator-local trust-anchor loading from `os.UserConfigDir()/Loopgate/policy-signing/trusted/`
- added `LOOPGATE_POLICY_SIGNING_TRUST_DIR` as an explicit override for advanced and test use
- kept the compiled fallback trust anchor in place while allowing operator-installed public keys outside the repo
- added config and CLI tests covering operator trust-anchor loading and `verify-setup`
- updated policy-signing and setup docs so open-source users can generate their own signer and install the matching public key without editing source

## Phase D: Auth Failure Audit Contract

Status: **completed**

Focus:
- make auth-token failures durably visible in the authoritative audit path

### Phase D1: Define the audit contract

Decide explicitly:
- which failures are audited
- what metadata is safe to write
- what happens when audit append fails during auth denial handling

Recommended scope:
- missing peer identity
- missing token
- invalid token
- expired token
- peer binding mismatch

Recommended event shape:
- event type like `auth.denied`
- denial code
- redacted reason
- request method/path
- control session identifier if available
- safe peer summary only if already accepted elsewhere

### Phase D2: Implement auth-failure audit logging

Planned work:
- add a narrow helper for auth denial audit emission
- keep JSON denial responses stable unless a stronger contract change is needed
- ensure no raw token/header material reaches audit

Acceptance criteria:
- targeted auth failures produce durable audit records
- no audit recursion or auth-loop behavior
- audit append semantics are explicit and test-covered

Rollback:
- helper-isolated revert possible if behavior proves too coupled

Completed in this phase:
- added a shared fail-closed `auth.denied` audit path for capability-token and approval-token authentication failures
- audited:
  - missing peer identity
  - missing token
  - invalid token
  - expired token
  - peer binding mismatch
- kept the JSON denial contract stable unless audit append itself fails, in which case the route now returns `DenialCodeAuditUnavailable`
- added regression tests covering capability-token and approval-token auth denials plus audit-unavailable behavior

## Phase E: Session MAC Response Minimization

Status: **completed**

Focus:
- stop sending epoch derivation material to clients

Planned work:
- remove `EpochKeyMaterialHex` from the wire response
- preserve:
  - epoch index
  - validity window
  - derived per-session MAC key
- verify whether any real client depends on the removed field before changing the response

Acceptance criteria:
- clients still have what they need to sign requests
- epoch derivation material no longer leaves the server
- response-shape tests prove the field is absent

Rollback:
- response field can be restored temporarily if an unexpected client dependency appears

Completed in this phase:
- removed `EpochKeyMaterialHex` from `SessionMACKeysResponse`
- verified the live client refresh path only depends on `current.derived_session_mac_key`
- added route and response-shape regression tests to keep epoch derivation material off the wire
- updated the local HTTP API doc to match the narrower response contract

## Phase F: Nonce Replay Persistence Redesign

Status: **completed**

Focus:
- remove the per-request full-map fsync bottleneck without weakening replay protection

### Phase E1: Introduce a persistence seam

Status: **completed**

Planned work:
- isolate nonce persistence behind a narrow internal abstraction
- keep current behavior temporarily behind that seam
- add contract tests around replay persistence behavior

Acceptance criteria:
- auth flow no longer depends directly on one storage format
- persistence behavior is testable in isolation

Completed in this subphase:
- introduced a narrow nonce replay store abstraction for load/save behavior
- kept the existing snapshot-on-write persistence semantics behind the new seam
- rewired startup, shutdown, and `recordAuthNonce(...)` through the store instead of direct file marshaling
- added store round-trip, pruning, and rollback-on-save-failure tests

### Phase E2: Replace snapshot-per-request persistence

Status: **completed**

Preferred design:
- append-only nonce log
- replay log on startup
- keep only active-window entries in memory
- add compaction later only if needed

Required explicit decisions:
- partial last-record handling
- malformed log entry handling
- duplicate nonce record handling
- behavior when corruption occurs in the tail vs the middle of the log

Recommended stance:
- tolerate truncated tail records
- fail closed on structurally corrupt middle sections unless a clearly safe recovery path is defined

Acceptance criteria:
- replay protection survives restart
- hot path no longer serializes and fsyncs the entire nonce map
- append failure remains explicit and fail-closed
- no background daemon or hidden async lifecycle is required

Rollback:
- switch back to the old persistence implementation behind the seam if needed

Completed in this subphase:
- switched the default nonce replay store to an append-only JSONL log
- made startup load the append log first, with legacy snapshot loading as a compatibility fallback when the new log is absent
- made the loader tolerate a truncated malformed tail record while failing closed on malformed middle records
- kept append failures explicit and fail-closed with in-memory rollback of the just-recorded nonce
- left shutdown compaction as a no-op for the append-only store so the hot path no longer rewrites the full nonce map

## Phase G: Replay-Window And Saturation Review

Status: **completed**

Focus:
- make replay retention and saturation behavior match real session lifetime and operator expectations

Planned work:
- review:
  - `requestReplayWindow`
  - `maxAuthNonceReplayEntries`
  - `seenRequests` capacity
  - relation to 1-hour session TTL
- decide whether current 24-hour replay retention is intentional or simply inherited

Acceptance criteria:
- replay-store sizing is explainable
- saturation remains fail-closed
- docs and operator behavior match actual retention semantics

Rollback:
- constant/config rollback if tuning is too aggressive

Completed in this phase:
- reduced replay-state retention from 24 hours to the 1-hour control-session TTL
- applied the tighter window consistently to:
  - request replay entries
  - auth nonce replay entries
  - used single-use tokens
  - terminal approval rows retained for replay/conflict visibility
- left fail-closed replay-store saturation caps unchanged because the tighter retention window already reduces steady-state pressure substantially
- added prune regressions for replay state and terminal approvals so the shorter retention remains intentional

## Phase H: Audit Integrity Posture Surfacing

Status: **completed**

Focus:
- make the current integrity posture legible and safe by default for the macOS-first product

Completed in this phase:
- enabled HMAC checkpoints in the shipped `config/runtime.yaml` using the existing macOS Keychain-backed secret-ref convention:
  - `id: audit_ledger_hmac`
  - `backend: macos_keychain`
  - `account_name: loopgate.audit_ledger_hmac`
  - `scope: local`
- added a first-start bootstrap path so the default checkpoint key is created in Keychain automatically when the default ref is enabled but missing
- taught offline diagnostics to report `bootstrap_pending` instead of a vague secret-load failure before the first successful server start
- updated getting-started, operator, ledger, and doctor docs so the stronger default posture is explicit
- aligned the tracked GitHub Actions workflow with the macOS-first supported target
- added `Server.AuditIntegrityModeMessage()` to surface active mode at startup (printed alongside socket path)
- added "Know your audit integrity mode" section to `OPERATOR_GUIDE.md` explaining both modes, how to check via `loopgate-doctor report`, and a config snippet to enable HMAC checkpoints
- 4 unit tests added in `internal/loopgate/server_audit_integrity_mode_test.go` (2026-04-16)

Acceptance criteria:
- operators can tell which integrity mode they are running
- docs, diagnostics, and runtime messaging agree

Rollback:
- config and bootstrap logic can be reverted independently if first-start behavior proves too surprising

## Phase I: Policy-Sign Trust Path Cleanup And CI Baseline

Status: **completed**

Focus:
- remove remaining trust ambiguity from the signer path and make regressions harder to miss

Planned work:
- delete dead `verifyPolicySignatureFile(...)`
- replace `runningUnderGoTestBinary()` with a real test-only mechanism
- expand `cmd/loopgate-policy-sign` and `internal/config` tests
- add `go test ./...` to CI, then evaluate `-race` viability separately if runner cost is acceptable

Acceptance criteria:
- production binaries cannot enable test trust by argv naming
- signer/operator flow has stronger test coverage
- PRs and pushes run the test suite automatically
- docs still match the real trust path

Rollback:
- dead-code removal is trivial
- trust-path change should stay isolated enough to revert independently

Completed in this phase so far:
- added GitHub Actions CI coverage with `go vet ./...` and `go test -race -count=1 ./...` on pushes to `main` and all pull requests
- removed the dead `verifyPolicySignatureFile(...)` wrapper
- replaced argv-name-based `runningUnderGoTestBinary()` trust extension with `testing.Testing()` plus the existing explicit `LOOPGATE_TEST_POLICY_SIGNING_*` test env
- kept operator-local trust anchors and explicit test-only trust separate, so production binaries no longer accept test trust merely because of executable naming
- kept signer verification coverage green across `internal/config`, `cmd/loopgate-policy-sign`, and `cmd/loopgate-policy-admin`

## Phase J: Readiness Closure

Status: **completed**

Focus:
- close the loop between code, tests, docs, and release posture

Planned work:
- update docs touched by each phase
- update risk notes only after fixes are merged and verified
- run full suite plus targeted regressions
- sanity-check operator flows:
  - first setup
  - denied-request diagnosis
  - `loopgate-ledger verify`
  - `loopgate-doctor report`

Acceptance criteria:
- blocker items are closed or explicitly deferred with rationale
- docs match runtime behavior
- operator-facing recovery paths are understandable without reading source

Rollback:
- not a single rollback point; this phase validates that earlier slices are safe to keep

Completed in this phase:
- updated top-level and setup docs so operator-facing text matches the current shipped posture:
  - HMAC checkpoints are default-on in the macOS-first runtime config
  - first server start bootstraps the default Keychain-backed checkpoint key
  - `bootstrap_pending` is documented as a first-run state, not a generic failure
- re-ran the full suite and targeted regressions after the Phase H and Phase I changes:
  - `go test ./...`
  - `go vet ./...`
  - signer-path regressions
  - audit-integrity and checkpoint bootstrap regressions
- confirmed the original ship-blocker list is closed in the active codebase:
  - nonce replay no longer rewrites the full snapshot on every authenticated request
  - `fs_read` throttling has its own denial code
  - auth denials enter the authoritative audit path
  - `EpochKeyMaterialHex` is gone from the session MAC response
  - OSS operators can use local trust anchors for policy signing

Deferred with rationale:
- stronger launcher-bound bootstrap identity remains a future hardening item, but it is not one of the original V1 ship blockers for the current macOS-first local product
- broader product and UX improvements remain tracked separately in `loopgate_v1_product_gaps.md`

## Current execution order

1. Phase A: baseline and blocker classification
2. Phase B: low-risk blocker fixes
3. Phase C: policy signing trust anchor configuration
4. Phase D: auth failure audit contract and implementation
5. Phase E: session MAC response minimization
6. Phase F: nonce replay persistence redesign
7. Phase G: replay-window and saturation review
8. Phase H: audit integrity posture surfacing
9. Phase I: policy-sign trust-path cleanup and CI baseline
10. Phase J: readiness closure

## Immediate next step

Start with:

1. decide whether to cut a release candidate / tag from the now-closed hardening roadmap
2. or take one final publishability / docs polish pass if you want a stricter OSS launch checklist first

That leaves roadmap execution itself complete; the next work is release judgment, not another blocker phase.
