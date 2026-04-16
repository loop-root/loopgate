**Last updated:** 2026-04-13

# Loopgate Cleanup Plan

This plan tracks the repository transition to a single clear product:

**Loopgate** — a local-first governance layer that tracks and constrains what your AI is doing.

## Product truth we are cleaning toward

Near-term product scope:
- Loopgate only
- local-first
- single-user / local operator
- Claude Code hooks as the active harness
- signed policy
- approvals
- local audit
- governed local MCP/runtime work

Not the current product story:
- alternate operator UIs as the main harness
- assistant-persona product layers inside this repo
- worker subsystems as a near-term core feature
- remote or multi-node deployment as the default
- automatic memory as part of Claude v1

## Phase 1: Repo-facing truth and operator docs

Status: **completed**

Goals:
- make top-level docs describe one product
- add operator-facing setup and usage docs
- stop presenting future enterprise or legacy UI paths as the default story

This slice includes:
- `README.md`
- `docs/README.md`
- `docs/setup/SETUP.md`
- `docs/setup/OPERATOR_GUIDE.md`

## Phase 2: Archive or de-emphasize stale docs

Status: **mostly completed**

Goals:
- remove multi-tenant/admin-node language from active docs unless clearly marked future or archived
- archive legacy product and subsystem docs that are no longer central
- reduce doc clutter so new readers can find the real product path quickly

Targets:
- docs index and setup references
- roadmap and design overview docs
- product RFCs and legacy folders
- trust-rotation and tenancy docs that are currently too prominent
- stale historical docs moved into the separate `ARCHIVED` repo under `docs/`

## Phase 3: Runtime/code slimming

Status: **mostly completed**

Goals:
- remove stale product code paths
- keep Loopgate core thinner, more legible, and easier to audit
- keep continuity out of the active Loopgate repo boundary

Mapped buckets:
- Safe now:
  - tracked Finder/editor junk and empty legacy directories
  - stale repo-facing references that still make legacy surfaces look active
- Coupled, needed staged extraction:
  - continuity and memory code that is now being moved into the sibling `continuity` repo
  - legacy actor and UI naming that still needed cleanup inside active helpers and tests
- Likely archive before delete:
  - legacy design docs, product RFCs, and benchmark/report material with old product framing

Constraint:
- only remove code after proving it is not required by the active Loopgate path

Completed in this phase so far:
- removed tracked `.DS_Store` residue from legacy `cmd/` and `morphlings/` directories
- removed the legacy `cmd/morphling-runner` binary while keeping the narrower in-process task-plan helper
- removed the task-plan prototype surface from the active server/runtime and its direct integration tests/docs
- removed Haven-only helper routes for resident journal ticks and agent work-item creation/completion, plus their direct client/test shims
- retired the larger Haven compatibility route surface instead of keeping it behind a runtime switch
- removed the retired Haven chat, UI projection, continuity-inspect, model-catalog, and settings implementation files from `internal/loopgate/`
- removed the morphling runtime/state-machine/worker file cluster from `internal/loopgate/`
- removed active morphling route, shell, config, status, and policy/template surfaces
- renamed the Go module from `morph` to `loopgate` and rewrote internal package imports
- renamed the lingering `soft_morphling_concurrency` runtime/profile field to `soft_worker_concurrency`
- removed `/goal` and `/todo` from the active shell command/catalog/man-page surface
- removed the external `goal_aliases` config path so continuity classification
  no longer depends on checked-in Loopgate tuning files
- moved stale `docs/superpowers/` planning material into the separate `ARCHIVED` repo
- extracted the in-tree memory/continuity subsystem from the active Loopgate runtime
- rehomed continuity-owned docs into the sibling `continuity` repo
- switched the active operator actor label to `operator` while keeping `haven` as a narrow compatibility alias
- dropped the unused sqlite dependency stack after the continuity extraction

## Phase 4: Repo hygiene and sanitization

Status: **in progress**

Goals:
- remove tracked runtime artifacts and stale local state
- strip hardcoded local paths from active docs where practical
- verify no secrets or sensitive data are committed

Known current issues:
- some active maps and comments still carry historical naming
- historical docs outside the active set still contain old product framing
- final publishability review is still needed for sensitive or stale tracked artifacts

Completed in this phase so far:
- removed tracked legacy memory ledger history from `core/memory/ledger/`
- preserved the benchmark harness by seeding a sibling `memBench` repo from the extracted continuity snapshot instead of forcing a later rebuild
- copied the remaining local `core/memory` residue into the sibling `continuity` repo under a clearly labeled runtime-residue snapshot
- copied the leftover local `cmd/memorybench` residue into the sibling `memBench` repo under a clearly labeled runtime-residue snapshot
- copied the current live `internal/tcl` and `internal/relationhints` source into the sibling `continuity` repo so extraction work can continue there without rebuilding from stale snapshots
- removed the in-tree `internal/tcl` and `internal/relationhints` packages from Loopgate after confirming they were self-contained islands and not imported by the active governance runtime
- preserved the remaining Loopgate-side continuity/memory contract residue in the sibling `continuity` repo and removed the dead `memory` promotion target plus unused continuity/memory denial codes from the main Loopgate runtime
- confirmed the remaining local `core/memory/` residue is preserved in the sibling `continuity` repo snapshot and kept out of Loopgate source control
- removed the leftover tracked `cmd/memorybench` cache residue from Loopgate after preserving the same local artifact in the sibling `memBench` repo
- preserved the post-split `memory_eligible` result-classification contract in the sibling `continuity` repo and removed that continuity-specific metadata axis from active Loopgate responses and UI projections
- preserved the unused persona `memory_promotion` / persona-memory prompt contract in the sibling `continuity` repo and removed those continuity-specific knobs from the active Loopgate persona defaults and prompt summary
- tightened ignore coverage for memory ledger/distillate backup artifacts
- moved tracked runtime sandbox/state artifacts out of source control and made `runtime/` fully gitignored
- sanitized remaining hardcoded local paths in active docs and tests

## Phase 5: Security hardening pass on the local-first core

Status: **in progress**

Goals:
- focus on real remaining local-first security gaps after cleanup reduces noise

Known hardening items:
- ledger replacement / rollback / tamper gap
- clearer operator recovery docs when policy, hook, or audit paths break
- final review of fail-closed behavior after repo slimming

## Tracking notes

Cleanup should stay incremental:
- one truthful slice at a time
- active docs should describe the current product, not the migration history
- code removal should preserve the Loopgate authority and audit invariants

The immediate next cleanup slices are:
1. finish the active-doc and map sanitization pass
2. remove remaining historical naming from active tests/comments where it no longer helps
3. keep hardening the local audit and demo surfaces without widening authority
