# Ledger Map

This file maps `internal/ledger/`, append-only JSONL event logs with hash chaining.

Use it when changing:

- event schema, sequencing, or integrity checks
- atomic append behavior or crash safety
- cross-platform file state helpers

## Core Role

`internal/ledger/` implements the **append-only ledger**: `Event` records, monotonic sequence, and hash-linked integrity checks. It is the canonical persistence layer for session/tool history where the design requires tamper-evident ordering.

## Key Files

- `ledger.go`
  - core append, chain validation, `Event` structure, schema version

- `ledger_test.go`, `segmented_test.go`
  - integrity and append tests

- `segmented.go`
  - segmented ledger layout where used

- `file_state.go`, `file_state_darwin.go`, `file_state_linux.go`
  - platform-specific safe file operations for ledger files

## Relationship Notes

- Audit wrapper: `internal/audit/ledger.go`
- Morph-side thread store (separate concern): `internal/haven/threadstore/`

## Important Watchouts

- Never mutate past events; ordering and hashes must stay consistent.
- Partial writes and integrity errors must surface explicitly to callers.
