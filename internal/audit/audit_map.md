# Audit Map

This file maps `internal/audit/`, the thin layer over append-only ledger writes with severity classes.

Use it when changing:

- whether a failed ledger append is fatal (`must_persist`) vs warning-only
- how Loopgate or other packages record security-relevant events

## Core Role

`internal/audit/` provides `LedgerWriter` and helpers that wrap `ledger.Append` with **explicit failure policy**: some paths must hard-fail if append fails; others may warn and continue.

This supports the project invariant that **security-relevant audit failures must not be silently swallowed** where the design requires persistence.

## Key Files

- `ledger.go`
  - `ClassMustPersist`, `ClassWarnOnly`
  - `LedgerWriter.Record`, `RecordMustPersist`

- `ledger_test.go`
  - behavior tests for writer semantics

## Relationship Notes

- Append-only storage: `internal/ledger/ledger.go`
- Loopgate audit paths: various `server_*` handlers in `internal/loopgate/`
  - Example: `capability.haven_trusted_sandbox_auto_allow` (must-persist) fires when Haven **trusted-sandbox auto-allow** upgrades `NeedsApproval` → `Allow` so operators can grep the ledger for that bypass path.

## Important Watchouts

- Do not convert must-persist failures into warn-only without an explicit design change and tests.
