# Integration Tests Map

This file maps `internal/integration/`, **black-box integration tests** for Loopgate and cross-cutting security behavior.

Use it when changing:

- end-to-end flows over the real Loopgate server and Unix socket
- policy denial, sandbox escape attempts, audit chain, quarantine, task-plan runners
- harness setup for temporary repos

## Core Role

This directory contains **only `*_test.go` files** (package `integration_test`). It is not a library imported by production code.

Tests spin up `loopgate.Server` with temp policy, exercise HTTP-over-UDS or client paths, and assert invariants (denials, audit, lifecycle).

## Key Files (representative)

- `harness_test.go`
  - shared harness for starting Loopgate in a temp repo

- `policy_denial_test.go`, `sandbox_escape_test.go`, `audit_chain_test.go`
  - security regression suites

- `taskplan_golden_path_test.go`, `taskplan_runner_test.go`
  - morphling / task-plan execution paths

- `session_socket_test.go`, `quarantine_lifecycle_test.go`
  - session and quarantine behavior

## Relationship Notes

- Implementation under test: `internal/loopgate/`
- Ledger assertions: `internal/ledger/`

## Important Watchouts

- Tests should remain hermetic (temp dirs, no real secrets).
- Failing tests often indicate boundary regressions — fix production code, not by weakening assertions.
