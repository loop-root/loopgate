**Last updated:** 2026-04-10

# Next Agent Handoff

This is the current-state engineering handoff for the next coding agent.
It is not a target-state RFC. It should answer three things:

- what is already landed and trustworthy
- what changed recently and why it matters
- what the next bounded slice should be

For target-state design, prefer:

- [Loopgate design overview](/Users/adalaide/Dev/loopgate/docs/design_overview/loopgate.md)
- [Roadmap](/Users/adalaide/Dev/loopgate/docs/roadmap/roadmap.md)
- [Memory v1 handoff](/Users/adalaide/Dev/loopgate/docs/reports/memory-v1-handoff-2026-04-10.md)
- [Current security review](/Users/adalaide/Dev/loopgate/docs/reports/security-review-current-state-2026-04-10.md)

## Current repo state

As of `2026-04-10`:

- the worktree is clean
- `go test ./...` is green
- the current control plane remains local HTTP over the Unix domain socket
- in-tree MCP server support remains removed; do not reintroduce alternate authority paths casually

## Current product shape

The repo is now strongest as:

1. Loopgate as the authority kernel
   - policy
   - approvals
   - secrets
   - sandbox mediation
   - continuity memory
   - append-only audit
2. governed assistant behavior on top of Loopgate
3. UI / operator surfaces as projections, not authority

The project is no longer blocked on basic memory plumbing. The main open work is now read-side policy breadth, stale compatibility cleanup, and deeper security review.

## Memory v1 status

Memory is in a truthful `v1` state for prototype integration.

Landed and important:

- backend-owned memory authority path for `remember`, `inspect`, `discover`, `recall`, review, tombstone, and purge
- wake-state split between `authoritative_state` and `derived_context`
- exact-first retrieval for stable profile slots
- runtime `hybrid` backend:
  - continuity remains authoritative for durable state
  - bounded RAG evidence is advisory only
- bounded artifact APIs:
  - `POST /v1/memory/artifacts/lookup`
  - `POST /v1/memory/artifacts/get`

Do not regress this boundary:

- wake state should stay small and current-state focused
- recalled evidence is not authority
- model inference is not persisted memory
- hybrid evidence must remain bounded and non-authoritative

See:

- [Memory v1 handoff](/Users/adalaide/Dev/loopgate/docs/reports/memory-v1-handoff-2026-04-10.md)
- [Memorybench running results](/Users/adalaide/Dev/loopgate/docs/memorybench_running_results.md)

## Current benchmark truth

Current honest headline on the `70`-fixture scored matrix:

- continuity product path: `70/70`
- governed RAG baseline: `42/70`
- governed stronger RAG: `38/70`

Important targeted slices:

- long-horizon state continuity:
  - continuity `8/8`
  - hybrid `8/8`
  - governed RAG baseline `2/8`
  - governed stronger RAG `2/8`
- hybrid recall bucket:
  - continuity `0/7`
  - RAG baseline `0/7`
  - stronger RAG `0/7`
  - hybrid `7/7`

Interpretation:

- continuity is materially better than RAG-only on durable state continuity
- stronger RAG still helps on broad evidence retrieval
- bounded hybrid composition is the current honest MVP path

Do not oversell this as universal semantic-memory victory. It is a real state-memory win plus a targeted hybrid-retrieval win.

## Recent security hardening now landed

The latest hardening passes closed several real loopholes and should be treated as the new baseline:

- explicit scoped control capabilities across drifting control-plane routes:
  - memory
  - connection
  - model
  - diagnostic
  - site trust / inspect
  - sandbox
  - UI projection and UI write routes
- startup socket path validation and safe stale-socket removal
- Haven preferences now fail closed on corrupt state and write `0600`
- UI presence projection now normalizes bounded state instead of replaying raw file text
- sandbox import/export are now bound to operator mounts, and export additionally requires an active write grant
- `session.open` now restores the replaced session if audit append for the replacement fails
- secret rollback cleanup failures are now surfaced instead of silently swallowed

These are no longer speculative review items. They are landed behavior and test-covered.

## Highest-value next slice

The next review slice should be conservative and security-oriented:

1. delegated-session / tenancy drift audit
2. stale compatibility ballast sweep
3. route-to-capability parity spot-check for any newly added surfaces

Specific suspicion to review next:

- stale or misleading compatibility fields around delegated session config
- tenant/user propagation assumptions across delegated worker or task-plan flows
- dead or semi-dead compatibility code that still appears security-relevant even if it no longer carries authority

This is a review / cleanup slice first, not a large feature slice.

## What not to do next

Do not:

- widen wake-state payloads for convenience
- make hybrid evidence authoritative
- reintroduce a parallel authority path around Loopgate
- treat UI-visible state as audit truth
- add background lifecycle daemons or autonomous workers without explicit design review
- claim TCL generalized semantic memory is "solved"

## Local runtime artifacts

Runtime artifacts may appear locally, including under:

- `runtime/`
- `loopgate/`
- `.cache/`

Treat them as local runtime state unless the user explicitly asks to inspect or clean them.
