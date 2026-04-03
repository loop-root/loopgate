**Last updated:** 2026-03-24

# Integration Test Plan

Status: proposed next implementation slice  
Authority: implementation-facing

## Purpose

The current test suite is strong at the package and handler level. The next gap is
boundary coverage across the real local control-plane path:

- Morph-side request construction
- Unix-socket transport
- Loopgate auth and policy enforcement
- filesystem side effects
- append-only audit persistence

The goal is not to replace unit tests. The goal is to prove that the real
boundary holds when the pieces are connected the way a real operator or attacker
would encounter them.

## Invariants To Prove

The integration suite should prove all of the following:

- local-only Loopgate socket transport remains the privileged boundary
- deny-by-default policy still blocks writes and approvals end-to-end
- sandbox and host filesystem paths never cross implicitly
- append-only audit and ledger chaining remain intact under full-session flows
- secret redaction still holds after real request, response, and audit handling
- replayed signed requests are rejected over the real socket
- quarantined content preserves hash integrity across store, view, and prune

## Harness Shape

Build the first harness around a temporary repo fixture plus a real Loopgate
server and client.

- repo fixture:
  - temp repo root
  - minimal `persona/`, `core/`, `runtime/`, and `core/policy/` tree
  - isolated ledger, state, quarantine, sandbox, and socket paths
- server:
  - real `loopgate.NewServer(...)`
  - real Unix domain socket listener
  - injected `server.now` test clock where lifecycle or retention matters
- client:
  - real `loopgate.Client`
  - real session open and signed request flow
- dependencies:
  - use `httptest.Server` for any provider-facing HTTP behavior
  - do not use real providers or real public network calls
- Morph side:
  - first wave can drive parser/tool-call and command flows directly while using
    the real Loopgate socket
  - full PTY/process tests come later if needed

## Proposed Test Package Layout

Start with a dedicated integration package rather than burying these in the
already-large package test files.

- `internal/integration/harness_test.go`
- `internal/integration/session_socket_test.go`
- `internal/integration/policy_denial_test.go`
- `internal/integration/audit_chain_test.go`
- `internal/integration/quarantine_lifecycle_test.go`
- `internal/integration/sandbox_escape_test.go`
- `internal/integration/model_adversarial_test.go`

## First Tests To Implement

### 1. Session Auth Replay Over Real Socket

Purpose:

- prove nonce replay rejection over the real Unix socket
- prove modified signatures fail for a different reason than exact replays

Flow:

1. open a real Loopgate session
2. send one valid signed request
3. replay the exact same request bytes
4. mutate one byte of the signature and resend
5. assert distinct denial paths and audit outcomes

Expected files:

- `internal/integration/session_socket_test.go`

### 2. Policy Denial Over Real Socket Does Not Write

Purpose:

- prove that denied filesystem writes stay denied after parser, request, socket,
  policy, and response handling are all connected

Flow:

1. open a real session
2. issue a write request targeting a denied path such as `core/policy/policy.yaml`
3. assert Loopgate returns a typed denial
4. assert the target file is unchanged on disk
5. assert the denial is present in audit with redacted/operator-safe fields

Expected files:

- `internal/integration/policy_denial_test.go`

### 3. Audit Chain And Redaction Round Trip

Purpose:

- prove that a full session still produces contiguous, chained, redacted events

Flow:

1. start a real session
2. perform one allowed read-like action
3. perform one denied action
4. emit one malformed tool call or parse error path from the Morph side
5. shutdown cleanly
6. read the ledger back and verify:
   - contiguous sequence
   - unbroken prior-hash chain
   - single session id across expected events
   - no raw secrets in event data

Expected files:

- `internal/integration/audit_chain_test.go`

### 4. TaskPlan Golden Path Over Real Socket

Purpose:

- prove the full plan → validation → lease → mediated execution → staged result → completion flow over the real Unix socket
- prove that lease-bound execution prevents caller-supplied capability/arguments
- prove that Loopgate stages provider output (morphling output is untrusted)

Implemented in:

- `internal/integration/taskplan_golden_path_test.go`
- `internal/integration/taskplan_runner_test.go`

### 5. Morphling Runner Lifecycle Over Real Socket

Purpose:

- prove a separate-process morphling runner can consume a lease through Loopgate mediation
- prove lease expiry is enforced at execute time
- prove duplicate completion is rejected
- prove crash recovery semantics: crash after /execute but before /complete leaves plan in executing state, and recovery /complete succeeds
- prove concurrent execution of the same lease results in exactly one success

Implemented in:

- `internal/integration/taskplan_runner_test.go`

## Second-Wave Tests

- quarantine lifecycle with fake clock:
  - store
  - view
  - prune eligible
  - metadata retained after blob prune
- sandbox escape attempts:
  - symlink-to-host
  - traversal forms
  - path normalization edge cases
  - operator-visible virtual path does not expose runtime path
- adversarial model output:
  - malformed tool blocks
  - unknown capability names
  - denied capability arguments
  - normal text mixed with malformed tool syntax

## Execution Policy

Keep the suite deterministic and conservative.

- no real external network
- no background cleanup goroutines added just for tests
- no public TCP transport
- no shelling out to Morph or Loopgate binaries for the first wave
- prefer injected clocks and temp dirs over sleeps where possible

## Recommended Implementation Order

1. create shared temp-repo and Loopgate socket harness
2. add session replay test
3. add policy-denial/no-write test
4. add audit chain/redaction round-trip test
5. add fake-clock quarantine lifecycle test
