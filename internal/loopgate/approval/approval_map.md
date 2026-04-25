# Approval Package Map

This file maps `internal/loopgate/approval/`, the pure approval lifecycle
package used by the Loopgate authority runtime.

Use it when changing:

- pending approval state transitions
- approval decision validation
- approval manifest hashing and request binding
- approval-token hashing helpers

## Core Role

`approval/` owns approval rules that do not need `Server` state. It is the
small, testable core behind Loopgate's operator approval flow.

The package exists so lifecycle invariants stay pure and reusable:

- pending approvals have a canonical state machine
- approval decisions are bound to owner, nonce, and manifest
- manifest hashes bind the displayed approval to the request body that may run
- validation errors have stable codes that the server maps to denial codes

## Key Files

- `approval.go`
  - approval states: `pending`, `granted`, `denied`, `expired`, `cancelled`,
    `consumed`, and `execution_failed`
  - `ValidateStateTransition`
  - approval token hashing
  - canonical approval manifest hashing
  - request-body hashing for approval-bound actions

- `model.go`
  - `PendingApproval`
  - `ExecutionContext`
  - `BuildCapabilityApprovalManifest`
  - `BackfillPendingApprovalManifest` for older pending records

- `decision.go`
  - `DecisionActor`
  - `ValidateDecisionRequest`
  - owner/tenant checks, nonce checks, state checks, and manifest checks

- `*_test.go`
  - state-machine coverage
  - manifest binding coverage
  - decision validation coverage

## Relationship Notes

- Server integration lives in `internal/loopgate/approval_flow.go`.
- Wire request shapes live in `internal/loopgate/protocol/` and
  `internal/loopgate/controlapi/`.
- Stable denial-code strings live in `internal/loopgate/controlapi/core.go`.

## Important Watchouts

- Do not relax state transitions without checking replay, double-execution, and
  audit consequences.
- Actor binding must be checked before state so non-owners do not learn the
  lifecycle state of another session's approval.
- Approval manifest mismatches must fail closed.
- Approval validation is authority logic, not UI logic. Display strings may be
  friendly, but this package should stay deterministic and small.
