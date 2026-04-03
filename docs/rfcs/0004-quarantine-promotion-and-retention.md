**Last updated:** 2026-03-24

# RFC 0004: Quarantine Promotion and Retention

- Status: Draft
- Applies to: Loopgate quarantined artifacts, derived artifacts, and future
  review/promotion flows

## 1. Purpose

This RFC defines the normative rules for:

- quarantined artifact lifecycle
- operator review and promotion semantics
- derived artifact provenance
- stale reference handling
- storage-retention and blob-pruning behavior

The goal is to ensure that future quarantine review and promotion features do
not silently increase trust, erase evidence, or weaken Loopgate's control-plane
invariants.

## 2. Core invariants

- Quarantine is a trust state, not a temporary inconvenience.
- Cleanup is a storage-lifecycle action, not a trust-lifecycle action.
- Viewing quarantined content MUST NOT increase trust.
- Promotion MUST create a new derived artifact or append-only promotion record.
- Promotion MUST NOT mutate the source quarantined artifact in place.
- Cleanup MAY remove blob bytes, but MUST NOT erase provenance or audit
  lineage.
- Cleanup MUST NOT increase prompt, memory, or display eligibility.
- Cleanup MUST NOT decrease recorded sensitivity.
- Source artifacts remain quarantined forever, even after promotion.

Short form:

Quarantine can expire as storage, but never disappear as evidence.

## 3. Scope

This RFC covers:

- review vocabulary
- promotion targets
- append-only promotion events
- source/derived artifact relationships
- stale source behavior
- retention and blob-pruning semantics

This RFC does not cover:

- browser UI layout details
- concrete API endpoints for review or promotion
- model-side interpretation of promoted artifacts

## 4. Review vocabulary

The following actions are distinct and MUST NOT be conflated:

- `view`
  - operator inspects quarantined content
- `acknowledge`
  - operator records awareness without changing trust or eligibility
- `promote`
  - operator authorizes creation of a new derived artifact for a specific use
- `transform`
  - system or operator creates a reduced, selected, normalized, or otherwise
    transformed derivative from source content

Rules:

- `view` MUST NOT imply `acknowledge`
- `acknowledge` MUST NOT imply `promote`
- `promote` MUST NOT imply unrestricted downstream use
- `transform` MUST NOT occur implicitly because content was viewed

V1 narrowing:

- promotion MUST be limited to explicit field selection plus materialization of
  a new derivative
- selected field paths MUST be top-level field paths only
- `transformation_type` MUST be `identity_copy` only
- arbitrary rewriting, summarizing, sanitizing, or normalizing MUST NOT be
  implemented in v1

## 5. Promotion targets

Promotion MUST be target-specific.

Allowed promotion targets:

- `display`
- `memory`
- `prompt`

Rules:

- promotion for `display` MUST NOT imply `memory`
- promotion for `memory` MUST NOT imply `prompt`
- promotion for `prompt` MUST be the most restrictive target
- future implementations MAY require stronger approval or review for more
  sensitive targets

There MUST NOT be one generic "approved" trust state.

## 6. Source artifact rules

A quarantined source artifact is immutable evidence.

Source artifact properties MUST remain true after promotion:

- source artifact id is stable
- source provenance is stable
- source classification history is stable
- source quarantine status remains true
- source sensitivity history remains preserved

Promotion MUST NOT add a field such as `promoted=true` to the source artifact if
that changes the meaning of the source artifact's trust state.

## 7. Derived artifact rules

Promotion MUST produce a new derived artifact or append-only promotion record.

Derived artifacts MUST carry provenance linking back to the source.

Minimum required provenance for a derived artifact:

- `source_quarantine_ref`
- `source_content_sha256`
- `promotion_target`
- `promoted_by`
- `promoted_at_utc`
- `derived_artifact_ref`
- `derived_classification`

`derived_classification` MUST be fully materialized at creation time.
It MUST NOT be inferred later only from the promotion target.

Recommended additional fields:

- `selected_field_paths`
- `transformation_type`
- `source_content_type`
- `source_content_class`
- `source_blob_present` at time of promotion

V1 rules:

- derived artifacts MUST contain only the explicitly selected top-level source
  fields
- every included top-level field MUST have fully materialized metadata at
  creation time
- selected-field promotion MUST NOT imply trust in unselected source content

## 8. Event model

Implementations SHOULD model review and promotion as append-only lifecycle
events.

Recommended event types:

- `artifact.quarantined`
- `artifact.viewed`
- `artifact.acknowledged`
- `artifact.promoted`
- `artifact.derived_created`
- `artifact.blob_pruned`
- `artifact.expired`

Rules:

- events MUST be append-only
- event ordering MUST remain monotonic and explainable
- promotion history MUST remain reconstructible after blob pruning

Compact implementations MAY collapse `artifact.promoted` and
`artifact.derived_created` into one event if provenance remains explicit.

V1 implementation guidance:

- use one append-only promotion event that includes the derived artifact
  metadata explicitly
- do not split promotion into multiple persisted event types unless a later
  workflow requires that extra granularity

## 9. Legal state transitions

Conceptual source-artifact states:

- `quarantined_blob_present`
- `quarantined_blob_pruned`

Conceptual derived-artifact states:

- `derived_display`
- `derived_memory`
- `derived_prompt`

Legal transitions:

- `quarantined_blob_present -> viewed`
- `quarantined_blob_present -> acknowledged`
- `quarantined_blob_present -> promoted -> derived_*`
- `quarantined_blob_present -> blob_pruned -> quarantined_blob_pruned`
- `quarantined_blob_pruned -> retained_as_metadata_only`

Illegal transitions:

- `quarantined -> trusted_in_place`
- `viewed -> prompt_eligible` without explicit promotion
- `blob_pruned -> source_trust_increased`
- `cleanup -> classification_relaxed`

## 10. Stale and race behavior

Promotion MUST bind to an immutable source identity.

Minimum binding inputs:

- source artifact id
- source content hash or source version id

Rules:

- promotion MUST fail closed if the referenced source identity does not match
  the expected hash/version
- promotion MUST fail closed if required source bytes are no longer available
  and the requested promotion operation depends on verifying them
- promotion of selected fields or transformed derivatives MUST require
  source-byte verification unless the promoted content was already
  materialized and hash-bound in prior trusted state
- exact duplicate promotions in v1 MUST be denied with an explicit reason
  rather than silently deduplicated
- concurrent view/promotion/cleanup operations MUST NOT create ambiguous
  lineage

Implementations SHOULD use a lease, lock, or explicit version check to avoid
cleanup races during active review.

V1 duplicate semantics:

An exact duplicate promotion MUST be defined by the same:

- `source_quarantine_ref`
- `source_content_sha256`
- `promotion_target`
- canonical sorted `selected_field_paths`
- `transformation_type`
- `promoted_by`
- derived artifact payload digest
- derived field-metadata digest
- fully materialized `derived_classification`

If any of the above differ, the promotion attempt is distinct and MUST NOT be
collapsed into the prior event.

## 11. Retention and cleanup model

Retention is a storage policy, not a trust policy.

Recommended storage layers:

- append-only metadata and audit lineage
- blob payload storage
- derived artifact metadata and payloads

Retention rules:

- metadata SHOULD outlive blob payloads
- large quarantined blobs MAY be pruned earlier than small blobs
- artifacts with active lineage SHOULD retain enough metadata to preserve
  explainability after blob pruning
- implementations SHOULD consider reference-aware pruning instead of only
  age-based deletion

Cleanup MUST NOT:

- erase evidence that an artifact existed
- erase provenance needed to explain a derived artifact
- silently remove audit linkage
- increase eligibility or trust

## 12. Blob pruning

The first cleanup implementation SHOULD be blob pruning only.

Blob pruning means:

- raw blob bytes are removed from payload storage
- metadata, hash, provenance, and lifecycle events remain

Blob pruning SHOULD produce an append-only event containing at least:

- `artifact_ref`
- `content_sha256`
- `pruned_at_utc`
- `blob_size_bytes`
- `reason`

Viewing or promoting a blob-pruned artifact MUST behave explicitly:

- viewing raw bytes MUST fail clearly if bytes are no longer retained
- lineage queries MUST still succeed
- promotion that requires source verification MUST fail closed if verification
  is impossible

Metadata-only retention preserves lineage only. It MUST NOT authorize new
promotion that depends on source-byte verification.

## 13. Relationship to oversized content and future `blob_ref`

Oversized inline extracted fields SHOULD eventually use a `blob_ref`-style
representation rather than truncation.

Rules:

- `blob_ref` is a storage representation, not a trust upgrade
- `blob_ref` content remains subject to quarantine and promotion rules
- promoting a `blob_ref` source MUST still create a new derived artifact with
  explicit provenance and classification
- `blob_ref` content MUST NOT be auto-dereferenced into prompt, memory, or
  normal rendering paths

## 14. UI and operator safety constraints

Future UI implementations SHOULD treat quarantined content as hostile display
material.

Recommended constraints:

- tainted/quarantined content rendered in a separate visual region
- approval or promotion controls separated from the tainted-content view
- viewing and promoting as separate operator actions
- promotion controls SHOULD derive their context summary from trusted metadata,
  not from tainted display content
- no implicit trust upgrade because content was opened or previewed

This RFC does not define exact UI layout, but these constraints are normative
security goals.

## 15. Initial implementation guidance

The next implementation work SHOULD proceed in this order:

1. define append-only promotion event schema with explicit target-specific
   derived classification
2. implement exact-duplicate denial and top-level selected-field-only
   promotion for v1
3. add source-binding and stale-reference checks
4. add metadata-only retention and blob-pruning semantics
5. add `blob_ref` support for oversized extracted content
6. only then add richer quarantine review UI flows

## 16. Open questions

- which promotion targets require separate approvals
- whether future implementations need idempotent promotion for tightly matched
  retries beyond the v1 explicit-denial rule
- how browser/session binding should be enforced for future UI-driven promotion
- when a derived artifact may itself become input to further promotion steps
