# Memory Map

This file maps `internal/memory/`, shared continuity and memory-selection primitives below Loopgate’s authoritative wake-state.

Use it when changing:

- distillates, recall, or wake-state shaping
- thread-linked memory hygiene
- continuity event annotations for the ledger
- budget or discovery helpers

## Core Role

`internal/memory/` holds **lower-level memory logic** that unprivileged client code and Loopgate-side code both build on: candidates, continuity events, distillates, recall, and wake snapshots.

Authoritative “what is remembered” and wake reconstruction for production paths is still coordinated with `internal/loopgate/continuity_memory.go` and related handlers.

## Key Files

- `continuity.go`
  - constants and helpers for memory candidates and continuity event annotations on ledger payloads

- `continuity_state.go`
  - continuity state structures and transitions used when rebuilding or projecting state

- `distillate.go` / `distillate_test.go`
  - distillate handling and tests

- `recall.go` / `recall_test.go`
  - recall paths and thresholds

- `wake_state.go` / `wake_state_test.go`
  - wake-state construction and tests

- `threads.go` / `threads_test.go`
  - thread-linked memory threading

- `hygiene.go` / `hygiene_test.go`
  - hygiene rules for memory content

- `discovery.go` / `discovery_test.go`
  - discovery of memory-relevant material from threads or ledger

- `keys.go` / `keys_test.go`
  - keying and identifiers for memory records

- `budget.go`
  - budget limits for memory operations

- `verified_ledger.go`
  - ledger verification helpers tied to memory continuity

- `distillate_policy.yaml`
  - policy data for distillate behavior (checked in where present)

## Relationship Notes

- Loopgate wake-state and diagnostics: `internal/loopgate/continuity_memory.go`, `server_memory_handlers.go`
- Reference client projection: `cmd/haven/memory.go`
- TCL normalization (explicit facts, anchors): `internal/tcl/`

## Important Watchouts

- Treat model-originated strings as untrusted; projection and redaction rules from AGENTS still apply.
- Do not bypass Loopgate for durable “remember this” semantics.
