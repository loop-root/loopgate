# TCL Map

This file maps `internal/tcl/` (typed continuity language): normalized memory nodes, validation, signatures, and conflict anchors.

Use it when changing:

- how explicit memory facts normalize to stable keys
- `ConflictAnchor` and canonical slot identity
- TCL policy or validation rules
- parser behavior for TCL-shaped structures
- conformance fixtures that should match the TCL RFC set

## Core Role

`internal/tcl/` defines **types**, **normalization**, **validation**, and **signing** for structured memory/TCL content so Loopgate and memory pipelines can agree on stable identifiers and safe shapes.

Conflict anchors let the system detect supersession and conflicts for the same logical slot without treating free text as authority.

## Key Files

- `types.go`
  - actions, objects, qualifiers, trust, `ConflictAnchor`, node types

- `normalize.go` / `normalize_test.go`
  - normalization of candidates into stable TCL nodes and anchor derivation
  - `canonicalizeExplicitFactKey` — narrow alias table applied **before** `deriveExplicitFactConflictAnchor` (identity and preference keys only; no fuzzy matching)
  - dangerous-candidate shaping also lives here; continuity and explicit candidates that look like memory poisoning, authority spoofing, or secret-exfiltration instructions are forced into a review/quarantine semantic family before policy evaluation

- `key_normalization_test.go`
  - explicit fact key aliases collapse to the same TCL conflict anchor (`TestNormalizeExplicitFactKey_*`)

- `validator.go`
  - validation of nodes and conflict anchors

- `parser.go` / `parser_test.go`
  - parsing TCL-like input into validated structures

- `conformance_fixtures.go` / `conformance_test.go`
  - checked-in conformance corpus for compact syntax and anchor derivation
  - should track the canonical TCL RFC set rather than ad hoc implementation guesses

- `policy.go` / `policy_test.go`
  - policy hooks for TCL acceptance

- `signatures.go` / `signatures_test.go`
  - signing and verification helpers for TCL payloads

- `conflict_anchor_test.go`
  - canonical key behavior for `ConflictAnchor`

## Relationship Notes

- Loopgate memory/TCL integration: `internal/loopgate/memory_tcl.go` and related tests
- Lower-level memory primitives: `internal/memory/`

## Important Watchouts

- Model output must still pass through validation; TCL types are not trust by themselves.
- Keep canonical keys stable when changing normalization—migration implications for stored facts.
- Grow conformance fixtures when RFC semantics change; do not let code silently drift from the TCL RFCs.
