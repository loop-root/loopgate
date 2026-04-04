**Last updated:** 2026-03-24

# Next Agent Handoff

This document is a current-state handoff for the next coding agent. It is not a north-star RFC. It should describe:

- what is already implemented
- what is partially in progress
- what the next recommended slice is

For target-state design, prefer the RFC tracks:

- [Implementation RFCs](../rfcs/)
- [AMP Docs](../AMP/README.md)
- [Product RFCs](../product-rfcs/)

Protocol note:

- AMP is vendor-neutral
- Operator clients and Loopgate may keep historical names in code where the RFC does not
  require exact wire/object names
- RFC `MUST` and `MUST NOT` requirements are still binding even when local
  naming differs

## Current Product Shape

The project now has three clear layers:

1. Operator client (unprivileged runtime)
   - persistent operator shell
   - planning/orchestration
   - answer shaping
   - bounded continuity presentation
2. `Loopgate`
   - privileged control plane
   - policy, approvals, execution, secrets, model proxying, quarantine, promotion, sandbox crossing
3. `AMP`
   - neutral protocol/object model layer
   - sessions, capability tokens, approvals, artifacts, denials, references

The current MVP is strongest as a secure kernel plus narrow workflows. It is weaker as a broad assistant.

## Ship plan and engineering status (2026-03-24)

- **Public engineering snapshot:** `docs/roadmap/roadmap.md` and `docs/design_overview/loopgate.md`. Dated execution plans (if you keep them) live under a local-only `docs/superpowers/` tree — see `docs/DOCUMENTATION_SCOPE.md`.
- **v1 transport:** Local control plane is **HTTP over the Unix socket** (not Apple XPC). Native/Swift clients: `docs/setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md`.
- Several items the plan lists as “post-launch Tier 1” (e.g. sandbox `EvalSymlinks` fail-closed, deny-list fail-closed, ledger `fsync`, morphling summary projection hygiene) are **already implemented**; treat the plan’s Post-Launch table as the checklist, not “all future.”

## Current Working Features

Implemented and working:

- local control-plane auth, signed requests, and opaque scoped tokens
- Loopgate-owned model inference and model-secret handling
- quarantine, promotion, and blob-ref handling
- bounded memory continuity:
  - explicit typed continuity threads:
    - `current`
    - `next`
    - `previous`
  - continuity-tagged ledger events with explicit `thread_id`
  - Loopgate-owned sealed-thread inspection
  - Loopgate-owned distillates, resonate keys, wake-state projection, discovery, and recall
  - split memory runtime artifacts under `runtime/state/memory/`
  - deterministic goal-family normalization from `config/goal_aliases.yaml`
  - centralized memory scoring/configuration from `config/runtime.yaml`
  - Client-local prompt projection of live thread context
- narrow extractor contracts:
  - JSON allowlists
  - markdown frontmatter
  - markdown section selector
  - narrow HTML metadata extraction
- sandbox-first mediated file crossing:
  - `/sandbox import`
  - `/sandbox stage`
  - `/sandbox metadata`
  - `/sandbox export`
- RFC-MORPH-0008 morphling convergence in code:
  - authoritative class policy loaded from `core/policy/morphling_classes.yaml`
  - Loopgate-owned lifecycle records with formal monotonic states
  - request-level denial distinct from instantiated morphling termination
  - raw goal text kept out of append-only audit via `goal_hmac`
  - restart recovery resolves nonterminal records before accepting new work
- lifecycle-aware morphling pool management:
  - `/morphling spawn`
  - `/morphling status`
  - `/morphling terminate`
  - sandbox working dirs under `/morph/home/agents`
  - operator-facing morphling **summaries** use projection (lifecycle-oriented `status_text`, `memory_string_count`; raw worker/model strings are not exposed in summaries)
  - append-only hash-linked audit for `morphling.spawn_requested` through `morphling.terminated`
- narrow live workflows:
  - status check
  - repo/issues summary

## Current Pivot

The project has pivoted away from adding more kernel primitives first.

Current priority order is:

1. sandbox-first operator model
2. explicit approval classes
3. socket-bound morphling execution handshake
4. workflow usefulness and orchestration quality

## Approval Class Slice

This slice is partially implemented and should be treated as the current immediate boundary.

Already landed in code:

- approval classes exist in [approval_classes.go](../../internal/loopgate/approval_classes.go)
- capability approval metadata now carries `approval_class`
- approval audit events now carry `approval_class`
- generic Loopgate capability approval cards show the approval class

This handoff also completed the remaining local approval UX alignment:

- `/site trust-draft` now uses the typed approval card
- `/sandbox export` now uses the typed approval card

## Next Recommended Slice

The RFC-MORPH-0008 class/lifecycle convergence work and the first socket-bound
worker handshake are now in place. The next slice should stay inside the same
trust boundary and make the memory path converge with the newer continuity
design.

Recommended order:

1. keep v1 distillation field-first and deterministic; do not let model prose
   become durable memory by default
2. extend restart and replay tests around sealed-but-uninspected and
   inspected-but-not-yet-acknowledged threads
3. add client-side projection for Loopgate continuity review and lineage status
   without moving durable-memory authority out of Loopgate
4. avoid widening public surface area while continuity semantics are still
   converging

## Morphling MVP Constraints

The first morphling slice should be intentionally narrow:

- single explicit goal
- sandbox-scoped working directory
- class-based capability envelope
- explicit lifecycle states
- explicit audit events
- socket-only Loopgate control surface
- no policy mutation
- no direct persistent state mutation
- no direct host filesystem access outside sandbox boundaries

Suggested classes:

- reviewer
- editor
- tester
- researcher
- builder

## Current Known Drift / Follow-Up

These are still worth revisiting after the RFC-MORPH-0008 convergence work:

- continuity review, tombstone, and purge flows now exist on the local Loopgate control plane
- current durable wake-state projection is global-scope only; thread/task scope
  activation is still missing
- the local transport path is conceptually AMP-aligned but not yet RFC 0001/0004
  conformant on exact version/profile negotiation and canonical request envelope
- the approval subsystem is still product-specific rather than fully RFC 0005
  manifest-bound and consumed-state-driven
- artifact/reference types are still implementation-specific rather than unified AMP envelopes
- morphling state is materialized in a state file for fast lookup, while the
  cryptographically verifiable audit source of truth remains the hash-linked
  Loopgate event ledger
- the first local worker handshake now drives `running`, `completing`, and
  `pending_review`, but there is still no broader worker engine or chaining
- broader HTML extraction should remain frozen unless a real workflow requires it
- workflow usefulness is still more important than adding more extraction or protocol surface

## Local Runtime Artifacts

There are often local untracked runtime artifacts in developer worktrees such as:

- `core/memory/`
- `loopgate/`
- `test.txt`

Treat these as local runtime artifacts unless the user explicitly asks you to inspect, clean, or commit them.
