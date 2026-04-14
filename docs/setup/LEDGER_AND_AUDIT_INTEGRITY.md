# Ledger and audit log integrity (operator guide)

**Last updated:** 2026-04-07  
**Scope:** **macOS**, **single Loopgate instance** per machine (see `docs/adr/0009-macos-scope-and-approval-hardening.md`). This page explains what append-only JSONL logs **do and do not** prove so operators set expectations for forensics and compliance.

## What these files are

| Artifact | Typical path (repo layout) | Role |
| --- | --- | --- |
| **Loopgate control-plane audit** | `runtime/state/loopgate_events.jsonl` (+ rotated segments / manifest under `runtime/state/loopgate_event_segments/`) | Authoritative append-only record of security-relevant control-plane actions Loopgate chose to persist |
| **Client / orchestrator ledger** | Often `core/memory/ledger/ledger.jsonl` (layout may vary by client) | Session-scoped tool and lifecycle events on the **unprivileged** client side |

Both use the shared **`internal/ledger`** machinery: monotonic sequence fields, `previous_event_hash`, and `event_hash` per line (SHA-256 over canonical JSON **excluding** the stored `event_hash`).

## What the hash chain gives you

- **Ordering and linkage:** Each event references the previous hash; accidental truncation, single-line corruption, or reordering is detected when the chain is verified on read/append.
- **Tamper-evidence inside the file:** You cannot change one historical line without breaking the chain **unless** you recompute hashes for all following lines.

Loopgate verifies the chain when appending and on diagnostic paths that scan the active log (see `internal/ledger`).

## What the hash chain does **not** give you (by itself)

The per-line digest is **public SHA-256** over canonical event bytes. Without a **separate secret**, the chain alone does not prove **Loopgate authorship** to a verifier who assumes the whole file might have been rewritten.

**Implication:** A **same-user** attacker (or any process with write access to the log files under your macOS account) can **delete the file** or **replace it entirely** with a **new, internally consistent chain** from a synthetic genesis. Hash-chain verification would succeed on that forged file because it checks **internal consistency**, not possession of a signing key.

So:

- Treat these logs as **strong evidence of ordering and integrity while the file remains under Loopgate’s control**, not as **unforgeable proof** against a compromised local user or offline disk editing.
- **File permissions** (`0600` on sensitive paths), **full-disk encryption**, and **least-privilege** on the Mac account remain part of the real-world boundary.

## Optional HMAC checkpoints

When `logging.audit_ledger.hmac_checkpoint` is **enabled** in `config/runtime.yaml`, Loopgate appends `audit.ledger.hmac_checkpoint` after every **N** ordinary audit events. Each line carries **HMAC-SHA256** over a canonical v1 message that includes the **through** `audit_sequence`, **through** prior `event_hash`, and a **checkpoint timestamp**; the **signing key** is loaded via **`secret_ref`** (for example macOS Keychain), not embedded in the JSONL.

Checkpoint lines still participate in the same **append-only hash chain** as other events. Verification helpers live in **`internal/ledger`** (`VerifyAuditLedgerHMACCheckpointEvent`). This improves **detectability of tampering** for parties that hold the key; it is **not** a substitute for **out-of-band** retention (append-only export, central aggregation) where the operator needs evidence off the workstation. See `docs/reports/security-hardening-plan-2026-04.md` for follow-on work.

## Recommended topology

For the current Loopgate model, prefer **one authoritative append-only control-plane audit ledger per local enforcement node** rather than multiple authoritative ledgers for hooks, approvals, tools, and policy separately.

Why:

- a single monotonic sequence keeps approval, denial, request, execution, and lifecycle ordering explainable
- multiple authoritative ledgers create cross-log reconstruction problems during incident review
- a single signed or HMAC-checkpointed chain is easier to verify than several partially ordered chains

If the event stream becomes noisy, solve that with **classification and derived views**, not with multiple competing sources of truth. In practice that means:

- keep one authoritative local audit ledger
- tag events with classes such as `policy`, `approval`, `hook`, `capability`, `memory`, `session`
- rotate segments for size management
- export or aggregate filtered views for downstream tooling or retention backends
- allow configurable detail levels for some low-risk projections, but keep security-relevant minimum events mandatory

Current conservative example:

- `logging.audit_detail.hook_projection_level: full` keeps redacted previews for some Claude hook events
- `logging.audit_detail.hook_projection_level: minimal` drops those preview strings but still keeps hashes, verbs, resolved targets, approval state, and session linkage

For downstream shipping, keep a separate local export cursor such as
`runtime/state/audit_export_state.json`. That cursor is **not authoritative
history**. It exists only to track what has already been streamed to a remote
sink. If it becomes corrupt or is reset, the consequence should be
duplicate export, not loss of the authoritative local ledger.

Current product note:

- downstream export exists in the codebase, but it is **not the center of the current Loopgate product**
- the active product story is still local-first governance and local authoritative audit
- treat export as optional implementation surface, not the primary way to understand Loopgate

Current implementation note:

- the export path is intentionally local-first and request-driven
- there is no autonomous background shipping daemon in this phase
- a later shipper may read the authoritative ledger by cursor and deliver
  batches elsewhere, but only after local append succeeds
- a control-plane operator can run a read-only preflight with
  `GET /v1/audit/export/trust-check` when the session has the
  `diagnostic.read` control capability; this evaluates local trust material
  and last-known export error state without moving the export cursor or
  contacting the downstream sink
- a control-plane operator can trigger one signed flush with
  `POST /v1/audit/export/flush` when the session has the `audit.export`
  control capability; this route appends its own request/outcome audit events
  locally before relying on downstream export state

If a downstream sink is used, the export unit should be a typed batch
envelope, not an ad hoc stream of lines. The envelope should include:

- source node correlation metadata (hostname, deployment tenant/user, transport profile)
- destination label and sink kind
- the exact exported audit-sequence range
- the through `event_hash`
- the original audit events without rewriting local history

Current conservative delivery rules:

- export uses the configured `logging.audit_export.endpoint_url`
- export to a configured remote sink also uses `logging.audit_export.authorization.secret_ref`
  to load a bearer token from the configured secret backend; raw credentials are
  not stored in `runtime.yaml`
- remote export should also use `logging.audit_export.tls.*`
  secret refs for mTLS client auth and remote certificate verification;
  bearer auth remains defense in depth, not the primary node identity proof
- operators may also pin the expected remote server public key with
  `logging.audit_export.tls.pinned_server_public_key_sha256` when they want a
  certificate rotation to require an explicit trust update rather than only CA
  validation
- `logging.audit_export.tls.minimum_remaining_validity_seconds` can fail closed
  before the local client cert, configured root CA, or observed remote leaf
  certificate is too close to expiry
- embedded credentials in the URL are rejected
- remote hosts must use `https` (loopback `http` is allowed for local tests/dev only)
- non-loopback remote export fails closed unless `logging.audit_export.tls.enabled`
  is set and the root CA, client certificate, and client private key secret refs
  are all present
- the local export cursor advances only after the sink confirms the same
  `through_audit_sequence` and `through_event_hash`

The following event families should remain **must-persist** even when operators reduce detail:

- session open, bind, and close
- policy deny and policy override decisions
- approval created, granted, denied, cancelled, or abandoned
- tool or capability request intent
- observed execution completion or failure
- audit integrity checkpoint events when enabled

This preserves one authoritative forensic timeline while still allowing downstream systems to consume narrower views such as a hook-focused export or an approval-only report.

## Operational practices (macOS)

- **Backups:** Copy JSONL and segment manifests together; restoring a **partial** set can break sequence expectations.
- **Do not edit lines in place:** Append-only is an invariant; in-place edits break the chain and may cause append failures until operators repair or rotate logs per runbook.
- **Diagnostics:** `GET /v1/diagnostic/report` (authenticated, signed) can summarize ledger verification; it does not change the trust model above.
  If you are intentionally testing the optional downstream export path, also see [AUDIT_EXPORT_TRUST_ROTATION.md](./AUDIT_EXPORT_TRUST_ROTATION.md).

## See also

- Threat model row **TM-05** — [loopgate-threat-model.md](../loopgate-threat-model.md)
- Implementation — [internal/ledger/ledger.go](../../internal/ledger/ledger.go), [internal/ledger/hmac_checkpoint.go](../../internal/ledger/hmac_checkpoint.go)
- Hardening backlog — [security-hardening-plan-2026-04.md](../reports/security-hardening-plan-2026-04.md)
