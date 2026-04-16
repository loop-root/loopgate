# Loopgate Result Map

This file maps `internal/loopgateresult/`, **formatting and sanitization** of Loopgate capability results for display and prompts.

Use it when changing:

- how structured tool results render in the terminal or chat
- redaction of approval metadata
- text shown when results are audit-only or quarantined

## Core Role

`internal/loopgateresult/` turns `loopgate.CapabilityResponse` and structured maps into **operator-safe strings**, without leaking nonces or quarantined source material. It bridges Loopgate’s classification (`AuditOnly`, `Quarantined`) to user-facing copy.

## Key Files

- `render.go`
  - `FormatDisplayResponse`, `StructuredDisplayText`, `StructuredPromptText`
  - `SanitizedApprovalMetadata`

- `render_test.go`
  - formatting regressions

## Relationship Notes

- Types: `internal/loopgate` (`CapabilityResponse`, status enums)
- Used by: `internal/shell` (`HandleCommand` and related), and similar call paths that surface capability output

## Important Watchouts

- Treat structured results as untrusted content; keep quarantine and audit-only semantics explicit.
- Strip or avoid echoing sensitive approval nonces (`SanitizedApprovalMetadata`).
