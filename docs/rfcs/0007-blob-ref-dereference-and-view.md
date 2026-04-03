**Last updated:** 2026-03-24

# RFC 0007: Blob-Ref Dereference and View Semantics

- Status: Draft
- Applies to: Loopgate blob-backed content, quarantine storage, future
  quarantine viewers, and oversized extracted-field handling

## 1. Purpose

This RFC defines how `blob_ref` content is represented, viewed, and
dereferenced.

The goal is to keep large or unsafe content off the hot path without letting
blob references become a hidden trust or rendering side door.

## 2. Core invariants

- `blob_ref` is storage indirection, not trust escalation.
- A `blob_ref` MUST NOT become prompt-eligible or memory-eligible by virtue of
  being referenced.
- A `blob_ref` MUST NOT be auto-dereferenced in prompt, memory, or normal UI
  rendering paths.
- Viewing a `blob_ref` is observation, not promotion.
- Dereference policy MUST remain explicit, auditable, and fail closed.
- Metadata-only retention preserves lineage, not promotability.
- Pruning MAY remove content bytes, but MUST NOT erase evidence.

Short form:

Storage indirection is not trust escalation.

## 3. What a `blob_ref` points to

In v1, a `blob_ref` SHOULD point to a quarantined artifact identity plus enough
metadata to preserve lineage.

Minimum required properties:

- stable artifact reference
- content hash
- content type if known
- stored size
- origin and provenance linkage

`blob_ref` MUST NOT imply that the referenced bytes are safe, current, or
eligible for downstream use.

Recommended v1 inline shape:

```json
{
  "kind": "blob_ref",
  "quarantine_ref": "quarantine://payloads/abc123",
  "content_sha256": "deadbeef...",
  "content_type": "text/plain",
  "size_bytes": 183492,
  "storage_state": "blob_present"
}
```

Allowed `storage_state` values:

- `blob_present`
- `blob_pruned`

## 4. Inline behavior

When a structured result contains a `blob_ref`, the inline result MUST carry
metadata only.

Allowed inline data:

- artifact reference
- content hash
- content size
- content type
- provenance summary

Disallowed inline data by default:

- full referenced content
- prompt-ready preview text
- memory-ready text
- implicit rendered expansion of the referenced bytes

## 5. Viewing and dereference actions

The following actions are distinct:

- `inspect_metadata`
  - operator sees blob metadata only
- `view_blob`
  - operator explicitly requests content bytes or a controlled preview
- `promote_from_blob`
  - operator initiates explicit promotion flow from blob-backed source content

Rules:

- `inspect_metadata` MUST NOT imply `view_blob`
- `view_blob` MUST NOT imply `promote_from_blob`
- `view_blob` MUST NOT imply prompt or memory eligibility
- `promote_from_blob` MUST follow the same explicit promotion rules as any
  other quarantined source

`view_blob` MUST NOT reserve the underlying bytes against future pruning.
Viewing grants no trust change and no storage lease by default.

## 6. Auto-dereference policy

The following MUST NOT auto-dereference `blob_ref`:

- prompt compilation
- memory ingestion
- standard tool-result rendering
- ordinary UI event feed rendering
- summary generation paths

Any dereference path MUST require explicit operator intent or explicit future
policy designed for that exact use case.

## 7. Preview policy

V1 SHOULD avoid semantic inline previews entirely.

If previews are added later, they MUST be:

- explicitly bounded
- separately classified
- treated as tainted content by default
- clearly distinguished from the underlying blob

Previewing MUST NOT change the trust state of the blob-backed source.

## 8. Relationship to promotion

Promotion from blob-backed source content MAY be supported later, but only
through explicit promotion semantics.

Rules:

- the source blob remains quarantined forever
- promotion MUST create a new derived artifact
- the derived artifact MUST carry explicit provenance and classification
- viewing a blob MUST NOT create a promotable derivative automatically

V1 conservative rule:

- fresh promotion from blob-backed free-text content SHOULD remain out of scope
  until deterministic extraction policy is clearer
- future promotion from blob-backed source content MUST operate on the source
  artifact, not on the `blob_ref` object as if it were trusted content

## 9. Metadata-only retention

If blob bytes have been pruned and only metadata remains:

- metadata inspection MAY still succeed
- lineage queries MAY still succeed
- content dereference MUST fail clearly
- fresh promotion that depends on source-byte verification MUST fail closed

Metadata-only retention preserves evidence, not content availability.

Metadata-only retained artifacts MUST remain:

- quarantined in trust state
- lineage-valid as evidence
- ineligible for fresh promotion that requires source-byte verification

Metadata-only retained artifacts MUST NOT be treated as if pruning resolved or
relaxed their trust state.

## 10. State model and lifecycle events

Trust/classification state and storage state are orthogonal.

Trust/classification state:

- `quarantined` forever

Storage state:

- `blob_present`
- `blob_pruned`

Recommended append-only lifecycle events for v1:

- `artifact.quarantined`
- `artifact.viewed`
- `artifact.promoted`
- `artifact.blob_pruned`

`artifact.blob_pruned` means:

- blob bytes removed from storage
- metadata, hash, and lineage retained
- dereference prohibited
- fresh promotion from source prohibited if verification requires source bytes

## 11. Race and failure behavior

The following cases MUST behave explicitly and fail closed where verification is
required.

### Case A: viewed -> blob pruned -> promote

Result:

- promotion fails closed if source-byte verification is required

Viewing grants no reservation and no trust change.

### Case B: metadata inspected -> blob pruned -> dereference attempt

Result:

- metadata inspection may still succeed
- content dereference fails clearly
- no fallback preview or cached content may be silently substituted unless that
  cache is itself a separately governed artifact

### Case C: promote request races with pruning

Result:

- promotion MUST verify blob presence and source hash inside the same critical
  section or version-check window
- if the blob disappears before verification completes, promotion MUST be
  denied

### Case D: fresh promotion after metadata-only retention

Result:

- deny with a specific reason such as `source_bytes_unavailable`

Metadata-only retention preserves history, not new trust transitions.

## 12. Pruning model

The first pruning implementation SHOULD be conservative.

Suggested model:

- metadata file or ledger entry retained
- blob file removed
- append-only `artifact.blob_pruned` event recorded

Pruning MUST:

- never modify classification
- never delete lineage metadata
- never remove source hash
- never remove links to derived artifacts
- never enable dereference or promotion

Pruning eligibility MAY later consider age, size, or reference state, but the
first implementation SHOULD prefer blob pruning over any deeper deletion.

## 13. Operator-facing safety

Future UI implementations SHOULD treat blob-backed content as hostile display
material.

Recommended constraints:

- metadata and content view are separate states
- blob view actions require explicit operator action
- tainted blob content is rendered in an isolated visual region
- approval or promotion controls are not colocated with tainted content

## 14. Initial implementation guidance

V1 implementation SHOULD:

1. represent oversized content as metadata-bearing refs, not inline text
2. keep `blob_ref` non-prompt-eligible and non-memory-eligible
3. avoid automatic previews
4. require explicit future view/promotion paths for content access
5. preserve lineage when pruning blob bytes
6. deny fresh promotion from pruned sources
7. fail closed on dereference from pruned sources

## 15. Open questions

- whether blob refs should use the quarantine namespace directly or a separate
  blob namespace with linked provenance
- whether later UI flows should allow bounded previews and under what policy
- how blob refs should interact with future quotas and pruning rules
