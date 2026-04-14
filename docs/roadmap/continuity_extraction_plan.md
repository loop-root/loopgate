**Last updated:** 2026-04-14

# Continuity Split Note

Loopgate no longer carries continuity and memory as part of its active runtime.
That subsystem is being re-homed into a separate sibling repository named
`continuity`.

## Objective

Keep **Loopgate** focused on:

- policy
- approvals
- audit
- local control sessions
- Claude hook governance
- governed MCP/runtime mediation

Move continuity and memory into a separate product boundary that owns:

- durable memory state
- continuity inspection and distillation
- wake-state derivation
- discovery / recall ranking
- TCL-backed semantic normalization
- benchmark and comparison harnesses

## Current status

The active in-tree continuity surface has already been retired from Loopgate.
The remaining work is repo-shape and provenance:

- rewrite Loopgate docs so they stop advertising extracted continuity work
- re-home the extracted continuity code into its own repo
- leave Loopgate as the thinner governance kernel

## Non-goals

This plan does **not** assume:

- a stable public API for the continuity product yet
- weakening Loopgate’s authority boundary to make extraction easier

## Boundary after the split

Loopgate keeps:

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

## Immediate handoff

The practical next step is simple:

1. create the sibling `continuity` repo
2. move the extracted continuity source and docs there
3. keep Loopgate docs and setup material focused on governance, policy, audit,
   Claude hooks, and MCP
