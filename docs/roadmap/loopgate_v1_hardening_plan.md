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
- wrong denial code for `fs_read` rate limiting
- misleading signed-request parser naming
- missing authoritative audit records for auth-token failures
- unnecessary epoch key material exposure on the wire

### Hardening after blocker closure

These are important, but not in the same “fix before V1” bucket:

- HMAC checkpoint default posture and operator surfacing
- replay-window and replay-store sizing review
- policy-sign trust-path cleanup
- stronger `loopgate-policy-sign` test coverage

## Working rules

- Preserve Loopgate authority and audit invariants.
- Prefer smaller, isolated slices over combined refactors.
- Do not widen trust to make a fix easier.
- Make rollback possible for each phase.
- Update docs and tests in the same slice when the contract changes.

## Phase A: Baseline And Blocker Classification

Status: **pending**

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

## Phase B: Low-Risk Blocker Fixes

Status: **pending**

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

## Phase C: Auth Failure Audit Contract

Status: **pending**

Focus:
- make auth-token failures durably visible in the authoritative audit path

### Phase C1: Define the audit contract

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

### Phase C2: Implement auth-failure audit logging

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

## Phase D: Session MAC Response Minimization

Status: **pending**

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

## Phase E: Nonce Replay Persistence Redesign

Status: **pending**

Focus:
- remove the per-request full-map fsync bottleneck without weakening replay protection

### Phase E1: Introduce a persistence seam

Planned work:
- isolate nonce persistence behind a narrow internal abstraction
- keep current behavior temporarily behind that seam
- add contract tests around replay persistence behavior

Acceptance criteria:
- auth flow no longer depends directly on one storage format
- persistence behavior is testable in isolation

### Phase E2: Replace snapshot-per-request persistence

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

## Phase F: Replay-Window And Saturation Review

Status: **pending**

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

## Phase G: Audit Integrity Posture Surfacing

Status: **pending**

Focus:
- make the current integrity posture legible before changing defaults

Planned work:
- surface HMAC checkpoint status in:
  - `loopgate-doctor`
  - setup docs
  - operator guide
  - startup/operator messaging if useful
- clearly distinguish:
  - hash-chain-only protection
  - keyed HMAC checkpoint protection

Only after surfacing is clear:
- decide whether HMAC checkpoints should become default-on

Acceptance criteria:
- operators can tell which integrity mode they are running
- docs, diagnostics, and runtime messaging agree

Rollback:
- surfacing changes are low-risk; any default-on decision should be separate

## Phase H: Policy-Sign Trust Path Cleanup

Status: **pending**

Focus:
- remove ambiguous test-only trust behavior from the signer path

Planned work:
- delete dead `verifyPolicySignatureFile(...)`
- replace `runningUnderGoTestBinary()` with a real test-only mechanism
- expand `cmd/loopgate-policy-sign` and `internal/config` tests

Acceptance criteria:
- production binaries cannot enable test trust by argv naming
- signer/operator flow has stronger test coverage
- docs still match the real trust path

Rollback:
- dead-code removal is trivial
- trust-path change should stay isolated enough to revert independently

## Phase I: Readiness Closure

Status: **pending**

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

## Current execution order

1. Phase A: baseline and blocker classification
2. Phase B: low-risk blocker fixes
3. Phase C: auth failure audit contract and implementation
4. Phase D: session MAC response minimization
5. Phase E: nonce replay persistence redesign
6. Phase F: replay-window and saturation review
7. Phase G: audit integrity posture surfacing
8. Phase H: policy-sign trust-path cleanup
9. Phase I: readiness closure

## Immediate next step

Start with:

1. Phase A baseline
2. Phase B code slice

That gives the project one safe correctness checkpoint before touching auth
audit behavior or the nonce persistence redesign.
