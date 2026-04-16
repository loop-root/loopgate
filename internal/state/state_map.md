# State Map

This file maps `internal/state/`, **small persistent operator-runtime state** (session counters, distill cursor, activity timestamps).

Use it when changing:

- what gets stored in the compact runtime state file vs the ledger
- corruption recovery behavior (rename to `.corrupt.*`, reinit)

## Core Role

`internal/state/` defines `RuntimeState` — a **minimal JSON snapshot** for session identity and cursors. Detailed history remains in the append-only ledger; this file is not a second source of truth for audit.

## Key Files

- `state.go`
  - `LoadOrInit`, `Save`, `New`, fields for session and distill cursor

- `state_test.go`
  - load/save and corruption path tests

## Relationship Notes

- Ledger: `internal/ledger/`
- Derived maintenance flows may read cursor fields — keep semantics documented when bumping schema.

## Important Watchouts

- Corrupt state is preserved with a timestamp suffix for forensics before reinit — do not silently delete.
