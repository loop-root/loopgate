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
- Client-side thread history persistence is no longer implemented in-tree.

## Important Watchouts

- Never mutate past events; ordering and hashes must stay consistent.
- Partial writes and integrity errors must surface explicitly to callers.
- **Security semantics:** `event_hash` / `previous_event_hash` are **SHA-256 over canonical JSON** (not a secret-keyed MAC). They detect accidental corruption and intra-file tampering that breaks the chain; they do **not** prove Loopgate authorship against a same-user attacker who replaces the whole file with a new valid chain. Operators: `docs/setup/LEDGER_AND_AUDIT_INTEGRITY.md`.
