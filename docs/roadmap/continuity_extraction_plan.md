**Last updated:** 2026-04-13

# Continuity Extraction Plan

This plan tracks how to separate memory and continuity from Loopgate so they can
become a thinner, more publishable standalone product and PoC later.

## Objective

Keep **Loopgate** focused on:

- policy
- approvals
- audit
- local control sessions
- Claude hook governance
- governed MCP/runtime mediation

Move continuity and memory toward a separate product boundary that owns:

- durable memory state
- continuity inspection and distillation
- wake-state derivation
- discovery / recall ranking
- TCL-backed semantic normalization
- benchmark and comparison harnesses

## Current reality

Today the continuity system is split across three places:

- `internal/memory`
- `internal/tcl`
- continuity-heavy files still living under `internal/loopgate`

That means Loopgate still directly owns too much memory behavior:

- continuity state loading and persistence
- continuity inspection/distillate generation
- wake-state formatting and ranking
- memory API request/response contracts
- benchmark bridge code

## Non-goals

This plan does **not** assume:

- a full extraction in one PR
- a stable public API for the continuity product yet
- preserving retired task-board, goal, or todo workflows as product features
- weakening Loopgate’s authority boundary to make extraction easier

## Target boundary

After extraction, the Loopgate kernel should depend on a narrow continuity
adapter interface rather than continuity implementation files.

Loopgate should keep:

- authenticated memory route handlers
- capability gating and approval decisions
- audit append ordering
- secret resolution and config loading
- any Loopgate-owned request signing / session binding

Continuity should own:

- internal memory records and state machines
- TCL normalization and semantic projection
- wake-state assembly
- discovery / recall ranking
- memory storage backends
- benchmark harnesses and continuity fixtures

## Proposed phases

### Phase 1: Remove product leakage and config coupling

Status: **in progress**

Goals:

- remove task/goal workflow surfaces from active Loopgate product UX
- remove checked-in continuity tuning that leaks through Loopgate config
- stop advertising continuity internals as Loopgate product features

Completed in this phase:

- retired `/goal` and `/todo` shell commands
- removed `config/goal_aliases.yaml`
- removed the `goal_aliases` config loader/writer path
- made continuity classification self-contained instead of depending on a
  checked-in alias table

### Phase 2: Define a narrow continuity service interface

Status: pending

Goals:

- introduce a small Loopgate-owned interface for:
  - `WakeState`
  - `Discover`
  - `Recall`
  - `Remember`
  - continuity inspection / distillation entrypoints
- make Loopgate route handlers depend on that interface rather than concrete
  continuity files

Expected file movement:

- pull memory-facing orchestration out of `internal/loopgate/server_memory_handlers.go`
  dependencies and into an adapter seam
- identify which request/response types stay Loopgate-facing versus which move
  to the continuity implementation

### Phase 3: Consolidate implementation under one continuity tree

Status: pending

Goals:

- move continuity-heavy implementation out of `internal/loopgate/` and into a
  dedicated package tree
- stop treating continuity implementation files as part of the Loopgate kernel

Primary candidates:

- `internal/loopgate/continuity_*`
- `internal/loopgate/memory_backend_*`
- `internal/loopgate/memory_sqlite_*`
- `internal/loopgate/memory_wake_format.go`
- `internal/loopgate/memory_artifact_refs.go`
- `internal/loopgate/memory_hybrid_evidence.go`

### Phase 4: Pull benchmarks and TCL under the same boundary

Status: pending

Goals:

- keep benchmark and TCL work with the continuity product instead of the
  Loopgate kernel
- make the “memory PoC” publishable without dragging the governance kernel
  along with it

Primary candidates:

- `internal/memory`
- `internal/tcl`
- `internal/memorybench`
- continuity benchmark bridges currently under `internal/loopgate`

### Phase 5: Externalize into a separate module/repo

Status: pending

Goals:

- create a standalone continuity repo/module with its own tests and docs
- leave Loopgate consuming it through a narrow adapter
- preserve Loopgate’s authority model while letting continuity evolve
  independently

Deliverables:

- standalone module path
- Loopgate adapter package
- fixture/test migration plan
- docs describing which product owns which responsibilities

## Immediate next slices

1. retire the remaining task/goal naming in continuity-facing maps and comments
2. define the first narrow continuity service interface inside Loopgate
3. move one small continuity implementation cluster behind that interface
