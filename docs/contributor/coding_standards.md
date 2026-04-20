**Last updated:** 2026-04-20

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
