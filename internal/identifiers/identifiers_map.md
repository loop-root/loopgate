# Identifiers Map

This file maps `internal/identifiers/`, validation for **safe identifiers** used in metadata (not raw paths or shell).

Use it when changing:

- rules for secret ref IDs, backends, scopes, account names
- any new user-supplied or model-adjacent label that must stay inert

## Core Role

`internal/identifiers/` provides `ValidateSafeIdentifier` — a **deny-by-default character set** and length limit so identifiers cannot carry path traversal, shell metacharacters, or obvious injection fragments.

## Key Files

- `identifiers.go`
  - `ValidateSafeIdentifier` and pattern

- `identifiers_test.go`
  - regression tests for accepted and rejected inputs

## Relationship Notes

- Used by `internal/secrets` (`SecretRef.Validate`), and other packages needing stable safe labels.

## Important Watchouts

- This is not a substitute for path canonicalization; use `internal/safety` / `internal/sandbox` for filesystem targets.
