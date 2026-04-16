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

- `candidate_input.go`
  - raw TCL analysis input shape (`MemoryCandidateInput`) plus the temporary `MemoryCandidate` alias for older callers
  - analysis input is intentionally separate from the validated write contract

- `types.go`
  - actions, objects, qualifiers, trust, `ConflictAnchor`, node types

- `validated_candidate.go` / `validated_candidate_test.go`
  - raw-text-free validated memory write contract built on top of the existing TCL analysis pipeline
  - Phase 1 only supports `explicit_fact` as a validated write source even though lower-level analysis supports more candidate kinds
  - overwrite/persistence paths are expected to consume this contract in later phases instead of reinterpreting raw analysis input
  - contract validation now checks anchor-to-slot consistency, so a candidate cannot carry a syntactically valid but semantically wrong anchor tuple

- `normalize.go` / `normalize_test.go`
  - normalization of candidates into stable TCL nodes and anchor derivation
  - `CanonicalizeExplicitMemoryFactKey` — narrow compiled registry applied **before** anchor derivation
  - registry currently allows explicit profile/preference/routine/project plus conservative `goal.*` and `work.*` namespaces; unknown or bare namespace keys fail closed
  - explicit profile/settings support now includes canonical `profile.timezone` and `profile.locale` with exact aliases for bare `timezone` / `locale`
  - `deriveExplicitPreferenceFacet` is still a narrow secondary fallback for explicit preference writes, not the target TCL-first preference path

- `dangerous_candidate.go`
  - isolated risk classifier for explicit/continuity candidates that look like memory poisoning, authority spoofing, or secret-exfiltration instructions
  - normalization still consumes that classifier, but keeping it separate makes review-heuristic changes easier to test without mixing them into anchor/key shaping

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
- Lower-level persistence/runtime extraction work now lives in the sibling `continuity` repo

## Important Watchouts

- Model output must still pass through validation; TCL types are not trust by themselves.
- Keep analysis input and validated write contracts distinct. `MemoryCandidateInput` is untrusted pipeline input; `ValidatedMemoryCandidate` is the later trust boundary.
- Keep canonical keys stable when changing normalization—migration implications for stored facts.
- Do not widen the explicit-memory registry casually; see the continuity repo ADR `docs/adr/0006-explicit-memory-key-registry-compiled-until-signed-admin-distribution.md`.
- Do not broaden fallback preference anchoring casually; see the continuity repo ADR `docs/adr/0007-preference-anchor-fallback-stays-secondary.md`.
- Do not widen validated write sources casually; the contract is intentionally narrower than the analysis pipeline until persistence semantics are updated to consume it directly.
- Grow conformance fixtures when RFC semantics change; do not let code silently drift from the TCL RFCs.
