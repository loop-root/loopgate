# Ledger and audit log integrity (operator guide)

**Last updated:** 2026-04-08  
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

## What the hash chain does **not** give you

The chain uses **public SHA-256**, not a **secret HMAC** or **asymmetric signature** held outside the log.

**Implication:** A **same-user** attacker (or any process with write access to the log files under your macOS account) can **delete the file** or **replace it entirely** with a **new, internally consistent chain** from a synthetic genesis. Verification would succeed on that forged file because nothing in the file proves **Loopgate authorship**—only **internal consistency**.

So:

- Treat these logs as **strong evidence of ordering and integrity while the file remains under Loopgate’s control**, not as **unforgeable proof** against a compromised local user or offline disk editing.
- **File permissions** (`0600` on sensitive paths), **full-disk encryption**, and **least-privilege** on the Mac account remain part of the real-world boundary.
- For **forensic-grade non-repudiation**, you need an **out-of-band** control: e.g. periodic **append-only export** to a system the workstation user cannot rewrite, or **signed checkpoints** with a key in **secure storage** (not colocated with a mutable JSONL file). Those are **not** implemented in-tree today; see `docs/reports/security-hardening-plan-2026-04.md`.

## Operational practices (macOS)

- **Backups:** Copy JSONL and segment manifests together; restoring a **partial** set can break sequence expectations.
- **Do not edit lines in place:** Append-only is an invariant; in-place edits break the chain and may cause append failures until operators repair or rotate logs per runbook.
- **Diagnostics:** `GET /v1/diagnostic/report` (authenticated, signed) can summarize ledger verification; it does not change the trust model above.

## See also

- Threat model row **TM-05** — [loopgate-threat-model.md](../loopgate-threat-model.md)
- Implementation — [internal/ledger/ledger.go](../../internal/ledger/ledger.go)
- Hardening backlog — [security-hardening-plan-2026-04.md](../reports/security-hardening-plan-2026-04.md)
