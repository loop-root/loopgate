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

## Operational practices (macOS)

- **Backups:** Copy JSONL and segment manifests together; restoring a **partial** set can break sequence expectations.
- **Do not edit lines in place:** Append-only is an invariant; in-place edits break the chain and may cause append failures until operators repair or rotate logs per runbook.
- **Diagnostics:** `GET /v1/diagnostic/report` (authenticated, signed) can summarize ledger verification; it does not change the trust model above.

## See also

- Threat model row **TM-05** — [loopgate-threat-model.md](../loopgate-threat-model.md)
- Implementation — [internal/ledger/ledger.go](../../internal/ledger/ledger.go), [internal/ledger/hmac_checkpoint.go](../../internal/ledger/hmac_checkpoint.go)
- Hardening backlog — [security-hardening-plan-2026-04.md](../reports/security-hardening-plan-2026-04.md)
