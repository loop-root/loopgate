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
- Morph as a separate product
- Haven as the main user-facing product
- morphlings as a near-term core feature
- multi-tenant deployment
- admin-node deployment
- automatic memory as part of Claude v1

## Phase 1: Repo-facing truth and operator docs

Status: **in progress**

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

Status: in progress

Goals:
- remove multi-tenant/admin-node language from active docs unless clearly marked future or archived
- archive Haven/Morph/morphling-heavy docs that are no longer central
- reduce doc clutter so new readers can find the real product path quickly

Targets:
- docs index and setup references
- roadmap and design overview docs
- product RFCs and legacy folders
- trust-rotation and tenancy docs that are currently too prominent
- stale historical docs moved into the separate `ARCHIVED` repo under `docs/`

## Phase 3: Runtime/code slimming

Status: in progress

Goals:
- remove or isolate stale product code paths
- keep Loopgate core thinner, more legible, and easier to audit
- prepare memory/continuity for extraction behind a narrower interface and
  eventual standalone repo boundary

Mapped buckets:
- Safe now:
  - tracked Finder/editor junk and empty legacy directories
  - stale repo-facing references that still make legacy surfaces look active
- Coupled, needs a replacement or extraction plan first:
  - memory backend and continuity subsystems that are no longer part of Claude v1 but still exist as first-class server code
  - Haven-specific route and UI projection leftovers still wired into server/runtime types
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

## Phase 4: Repo hygiene and sanitization

Status: in progress

Goals:
- remove tracked runtime artifacts and stale local state
- strip hardcoded local paths from active docs where practical
- verify no secrets or sensitive data are committed

Known current issues:
- hardcoded local paths in active docs
- tracked legacy memory artifacts under `core/memory/`
- local absolute paths in benchmark/report docs

Completed in this phase so far:
- removed tracked legacy memory ledger history from `core/memory/ledger/`
- tightened ignore coverage for memory ledger/distillate backup artifacts
- moved tracked runtime sandbox/state artifacts out of source control and made `runtime/` fully gitignored
- sanitized remaining hardcoded local paths in active docs and tests

## Phase 5: Security hardening pass on the local-first core

Status: pending

Goals:
- focus on real remaining local-first security gaps after cleanup reduces noise

Known hardening items:
- ledger replacement / rollback / tamper gap
- clearer operator recovery docs when policy, hook, or audit paths break
- final review of fail-closed behavior after stale code removal

## Tracking notes

Cleanup should stay incremental:
- one truthful slice at a time
- docs first where they currently mislead
- code removal only after stable replacement boundaries exist

The immediate next cleanup slices after Phase 1 should be:
1. de-emphasize multi-tenant/admin-node docs
2. identify and quarantine tracked runtime artifacts and hardcoded path leaks
3. start mapping morphling/Haven/Morph deletion candidates
