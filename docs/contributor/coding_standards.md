**Last updated:** 2026-05-03

# Coding Standards

## Naming

Name things so a tired engineer can still understand them during an incident.

Prefer:

- explicit names over clever names
- names that reveal trust level and lifecycle
- project-native nouns like `capability`, `approval`, `audit`, `sandbox`,
  `control session`, `quarantine`, and `promotion`

Useful trust-revealing prefixes:

- `raw...` for unvalidated input
- `parsed...` for decoded but still untrusted values
- `validated...` for fully checked data
- `resolved...` for canonicalized filesystem targets
- `projected...` for derived UI-facing views
- `authoritative...` for control-plane-owned state

Avoid:

- vague names like `data`, `value`, `thing`, or `result`
- `ok` when the condition needs a semantic name
- implying stronger guarantees than the code actually provides

## Error handling

Do not hide errors.

Required behavior:

- return errors with context
- use wrapping where appropriate
- distinguish validation failure from system failure
- distinguish expected denials from unexpected runtime errors
- distinguish audit-unavailable failures from ordinary execution failures

Forbidden behavior:

- swallowing errors
- logging and continuing when the operation should fail
- silently downgrading transport, storage, or audit guarantees
- panicking inside locks
- returning `nil` on partial failure unless the contract explicitly allows it

Error messages should help operators debug without leaking secrets.

## Concurrency

Assume concurrency bugs exist until proven otherwise.

Required behavior:

- prefer simple ownership models
- use explicit synchronization
- keep lock scope small
- avoid holding locks across network I/O, model calls, or disk I/O when the
  invariants allow it
- keep state transitions atomic
- document shared mutable state

Review hazards:

- panic paths inside locks
- check-then-act races
- TOCTOU around filesystem validation and use
- mutating shared maps or slices without synchronization
- hidden goroutine side effects
- non-atomic multi-step state updates

Prefer deterministic, boring synchronization over clever concurrency.

## Implementation style

Prefer:

- small, testable functions
- explicit validation steps
- typed structures over loose maps
- deterministic parsing
- explicit allowlists
- narrow interfaces
- minimal side effects
- derived views that are clearly separate from authoritative state

Avoid:

- magic fallback behavior
- silent coercion
- broad implicit defaults
- hidden global state
- speculative abstractions before the lifecycle and authority model are clear

## Package boundaries

Use package boundaries to make ownership and authority explicit.

`internal/loopgate` should remain the HTTP/control-plane adapter and server
wiring layer. It may coordinate runtime packages, decode/encode HTTP payloads,
and bind runtime decisions to audit, diagnostics, and responses. It should not
grow indefinitely as the home for every authority-owning subsystem.

When extracting a cohesive runtime domain, prefer a sibling internal package
over another deep subpackage under `internal/loopgate/`. Good target shapes are:

- `internal/controlruntime` for control sessions, tokens, replay, and nonce
  state
- `internal/approvalruntime` for approval lifecycle, decision, and rollback
  orchestration
- `internal/auditruntime` for Loopgate audit sequencing/checkpoint runtime
- `internal/connections` for provider connection records, credentials, PKCE,
  and provider-token cache behavior
- `internal/mcpgateway` for MCP gateway manifests, launch state, approvals,
  execution, and policy decisions
- `internal/hostaccess` for host-folder grants and plan/apply behavior

Dependency direction matters more than the package name: `internal/loopgate`
may import these runtime packages, but these runtime packages must not import
`internal/loopgate`. If a candidate extraction needs to import `loopgate`, the
boundary is not ready; pass dependencies through small interfaces or callbacks
instead.

Do not create vague packages like `internal/common`, `internal/util`, or
`internal/runtime`. A package should have one obvious owner and one obvious
reason to change.

## Documentation expectations

Update docs when you change:

- policy semantics
- trust boundaries
- transport boundaries
- file handling behavior
- secret handling behavior
- ledger or audit guarantees
- operator-visible failure modes
- CLI or API behavior

Document the reason for the change, not just the mechanics.
