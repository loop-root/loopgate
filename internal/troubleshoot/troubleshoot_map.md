# Troubleshoot Package Map

This file maps `internal/troubleshoot/`, the offline operator diagnostics
package used by `cmd/loopgate-doctor`.

Use it when changing:

- doctor reports
- diagnostic bundle generation
- ledger-derived explanation output
- audit-export trust summaries
- nonce replay diagnostics

## Core Role

`troubleshoot/` builds derived operator views from local state. It is not an
authority surface and must not mutate policy, approvals, or audit history.

The package exists so operators can answer "what happened?" and "what should I
check next?" without reading raw JSONL or diagnostic logs directly.

## Key Files

- `report.go`
  - `Report`
  - `BuildReport`
  - active ledger summary, event type histograms, diagnostic config summary,
    audit-export summary, and nonce replay summary

- `audit_integrity.go`
  - HMAC checkpoint and ledger integrity verification helpers

- `audit_logs.go`
  - verified audit event traversal and compact rendering helpers

- `approval_explanation.go`
  - approval timeline explanation from verified audit events

- `request_explanation.go`
  - capability request timeline explanation from verified audit events

- `hook_block_explanation.go`
  - blocked Claude hook explanation from verified audit events

- `bundle.go`
  - support bundle writer and diagnostic log tail extraction
  - enforces repo-relative diagnostic directory handling

- `demo_reset.go`
  - explicit demo reset helper for local demo state

- `*_test.go`
  - verification for ledger integrity, explanations, bundle path safety, and
    rendering behavior

## Relationship Notes

- CLI entrypoint: `cmd/loopgate-doctor/`.
- Ledger verification primitives: `internal/ledger/`.
- Runtime config types: `internal/config/`.
- Diagnostic log writer: `internal/loopdiag/`.

## Important Watchouts

- Always verify the audit ledger before presenting recent audit-derived events
  as trusted operator history.
- Diagnostic bundles must not read outside the repo or include secrets.
- Explanations are derived views; do not let them become authoritative approval
  or policy state.
- Keep redaction and bounded output in mind when adding new report fields.
