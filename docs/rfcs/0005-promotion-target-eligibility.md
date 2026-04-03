**Last updated:** 2026-03-24

# RFC 0005: Promotion Target Eligibility

- Status: Draft
- Applies to: Loopgate promotion policy, derived artifact creation, future
  quarantine review and blob-ref flows

## 1. Purpose

This RFC defines which source-field classes may be promoted to which targets in
v1.

The goal is to prevent promotion from becoming a silent trust-expansion path
for tainted or oversized content.

This RFC is intentionally narrow. It does not define browser UX, blob-pruning
implementation, or future recursive promotion of nested data.

## 2. Core invariants

- Promotion targets are trust targets, not UI labels.
- Display, memory, and prompt are distinct targets with distinct risk levels.
- Tainted text MUST NOT become prompt-eligible or memory-eligible by default.
- Blob-like storage representation MUST NOT imply trust.
- Metadata-only retention preserves evidence, not new promotability.
- Trust must never be inferred from field name, shape, or size alone.

## 3. Promotion targets

Allowed promotion targets remain:

- `display`
- `memory`
- `prompt`

Rules:

- promotion to `display` MUST NOT imply `memory`
- promotion to `memory` MUST NOT imply `prompt`
- promotion to `prompt` is the most restrictive target in v1

## 4. Source field classes

For v1, source fields are classified into the following coarse classes:

- `bounded_scalar`
  - booleans, validated numbers, enums, timestamps, strict identifiers
- `tainted_scalar_text`
  - remote or otherwise tainted scalar text
- `blob_ref`
  - oversized or externally stored content represented by reference
- `non_scalar`
  - arrays, objects, recursive structures

These classes are policy concepts, not trusted authority on their own.

## 5. V1 promotion matrix

### `bounded_scalar`

May be promoted to:

- `display`
- `memory`
- `prompt`

Only if:

- the field is already fully materialized
- the field metadata is present and valid
- policy allows the requested target

### `tainted_scalar_text`

May be promoted to:

- `display` only

Must be denied for:

- `memory`
- `prompt`

Rationale:

Display is an operator-facing visibility target.
Memory and prompt targets amplify downstream influence and MUST remain more
restrictive in v1.

### `blob_ref`

MUST NOT be promoted directly to:

- `memory`
- `prompt`

May participate in:

- future explicit review/promotion flows that produce a new smaller derived
  artifact

`blob_ref` MAY be displayable as metadata only, but MUST NOT auto-dereference
into prompt, memory, or normal rendering paths.

### `non_scalar`

MUST NOT be promoted directly in v1.

Nested objects and arrays require recursive metadata and policy that do not yet
exist.

## 6. Field-name semantics

Field names are labels, not capabilities.

Names such as:

- `approved`
- `policy`
- `tool_call`
- `instructions`
- `memory_candidate`

MUST NOT gain control semantics simply by appearing in promoted content.

Control semantics must come only from trusted Loopgate envelopes, policy, and
approval state.

## 7. Derived artifact requirements

When promotion succeeds, the derived artifact MUST:

- contain only explicitly selected source fields
- carry fully materialized field metadata for every included field
- carry fully materialized derived classification at creation time
- preserve provenance back to the source artifact

Promotion MUST NOT imply trust in unselected source content.

## 8. Metadata-only retention

If a source artifact has been reduced to metadata-only retention:

- lineage queries MAY still succeed
- existing derived artifacts remain valid lineage objects
- fresh promotion that requires source-byte verification MUST be denied

Metadata-only retention preserves evidence, not new trust transitions.

## 9. Relationship to `blob_ref`

Future `blob_ref` support is a storage representation for oversized content.

Rules:

- `blob_ref` is not a trust upgrade
- `blob_ref` does not make content prompt-safe
- `blob_ref` does not make content memory-safe
- promotion from blob-backed source content MUST still produce a new derived
  artifact with explicit provenance and classification

## 10. Initial implementation guidance

V1 implementation SHOULD enforce:

1. `bounded_scalar` may be promoted to `display`, `memory`, or `prompt`
   subject to policy
2. `tainted_scalar_text` may be promoted to `display` only
3. `non_scalar` and `blob_ref` may not be promoted directly
4. metadata-only retained source artifacts may not participate in fresh
   promotion

## 11. Open questions

- how future sanitization or normalization flows should be modeled without
  laundering trust
- whether a future operator-reviewed path may promote some text into memory or
  prompt targets after separate transformation policy exists
- how suspicious field-name policy hooks should be surfaced in config and
  validation
