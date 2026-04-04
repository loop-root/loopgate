**Last updated:** 2026-03-24

# RFC-MORPH-0006: Approval & promotion flow (Loopgate-orchestrated)

- **Status:** Draft — aligned to derivative-based promotion in Loopgate
- **Primary authority:** **Loopgate** (clients render approvals; Loopgate decides)
- **Normative revision:** 2026-03-09

---

# Summary

This RFC defines how **artifacts created inside the sandbox become real changes**. The approval and promotion system ensures that the **operator client** (presenting operator intent) and **morphlings** (bounded workers) cannot silently modify the host system or long‑term memory without **Loopgate** authorization.

All work produced by Morphlings must follow the flow:

```
Sandbox → Artifact → Approval → Promotion
```

Loopgate acts as the **final authority** for all promotions.

This RFC should be read together with the current Loopgate derivative-based
promotion model: promotion must create a new derived artifact and must not
mutate or "bless" the source artifact in place.

---

# Design Goals

The approval and promotion system must provide:

- explicit user visibility
- narrow approval scopes
- deterministic promotion behavior
- full audit traceability

No artifact may modify host state or continuity storage without passing through the promotion pipeline.

---

# Artifact Creation

Morphlings produce artifacts inside the sandbox.

Typical artifact types include:

- code patches
- generated files
- analysis reports
- summaries
- proposed configuration changes

Artifacts must be written to:

```
/morph/home/outputs/
```

Artifacts must not directly modify imported files.

---

# Artifact Metadata

All artifacts must include metadata describing their origin.

Example:

```json
{
  "artifact_id": "uuid",
  "task_id": "task-uuid",
  "creator": "morphling",
  "timestamp": "2026-03-09T22:30:00Z",
  "type": "patch",
  "source_paths": [
    "/morph/home/imports/project-x/file.go"
  ],
  "output_path": "/morph/home/outputs/patch.diff"
}
```

Artifacts without metadata must be rejected.

---

# Promotion Stages

Promotion occurs in several stages.

```
Morphling
   ↓
Artifact creation
   ↓
Client review & summary
   ↓
User approval
   ↓
Loopgate validation
   ↓
Promotion or rejection
```

Each stage must be recorded in the audit ledger.

---

# Operator / client review

The unprivileged client (IDE, native UI, or reference shell) is responsible for presenting artifact summaries to the user.

The client must:

- describe the proposed change
- identify affected files
- explain the task context

The client must not promote artifacts automatically.

---

# User Approval

Users must explicitly approve promotion.

Example approval actions:

- approve artifact
- reject artifact
- request revision

Approval must apply only to the specific artifact and the specific promotion
target.

Approvals must never grant broad filesystem permissions.

---

# Loopgate Validation

Before promotion, Loopgate must verify:

1. artifact metadata integrity
2. sandbox path validity
3. capability token scope
4. approval presence
5. policy compliance

If validation fails, promotion must be denied.

Promotion must also verify that the source artifact remains available and valid
for any operation that depends on source-byte verification.

---

# Promotion Types

Several promotion targets exist.

## Host Filesystem Export

Artifacts may be applied to host files after approval.

Example:

```
/morph/home/outputs/patch.diff
→ apply to ~/repo/project-x
```

Loopgate performs the modification.

---

## Continuity Promotion

Artifacts may become durable continuity records.

Example:

- task summary
- project decision
- system insight

Continuity promotion must follow the memory rules in RFC‑0005.

Continuity promotion creates a new durable memory artifact. It must not mutate
the trust/classification state of the source artifact.

---

## Trust Draft Promotion

Artifacts may propose trust drafts for external sources.

Example:

- trusted documentation site
- known API endpoint

Trust drafts must always require explicit approval.

---

# Rejection Handling

If promotion is rejected:

- artifact remains in sandbox
- no host changes occur
- rejection is logged

Rejected artifacts may be revised or deleted.

---

# Artifact Expiration

Artifacts should not persist indefinitely.

Loopgate may clean up artifacts that are:

- stale
- rejected
- superseded

Cleanup must not remove approved or promoted artifacts.
Cleanup must not erase lineage or audit history for promoted or rejected artifacts.

---

# Audit Requirements

Loopgate must record:

- artifact creation
- artifact review
- approval decisions
- promotion actions
- rejection events

These events must append to the audit ledger.

---

# Security Invariants

The promotion system must guarantee:

1. Morphlings cannot modify host files directly.
2. All state changes pass through Loopgate.
3. User approval is required for promotion.
4. Artifacts remain sandboxed until approved.
5. Promotion actions are fully auditable.
6. Promotion creates derived artifacts; it does not silently increase trust in place.

---

# Future Work

Possible improvements:

- multi-artifact promotion batches
- visual diff inspection
- artifact dependency graphs
- automated test validation before promotion

These features are **out of scope for v1**.

---

# Conclusion

The approval and promotion pipeline ensures that the operator client and morphlings remain powerful but controlled. By forcing all meaningful changes to pass through artifact review and Loopgate validation, the system preserves safety, transparency, and user authority.
