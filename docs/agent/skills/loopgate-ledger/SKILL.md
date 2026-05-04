---
name: loopgate-ledger
description: Use when verifying or inspecting Loopgate's local audit ledger with loopgate-ledger, including chain verification, HMAC checkpoints, recent event tails, event summaries, or explicitly confirmed demo resets.
---

# Loopgate Ledger

Use `loopgate-ledger` when the question is about local audit history and audit
integrity. The command inspects Loopgate's local control-plane audit ledger; it
does not make agents authority sources.

## Guardrails

- Run `verify` before treating ledger history as trusted evidence.
- Do not treat `summary` or `tail` as integrity verification. They are
  convenience reads of the active JSONL file.
- Do not edit, truncate, rotate, rewrite, or delete audit files.
- Do not run `demo-reset` unless the user explicitly asks for a local demo
  reset and understands it is destructive.
- Prefer `./bin/loopgate-ledger` over `go run ./cmd/loopgate-ledger` on macOS
  because keychain-backed verification can depend on executable identity.
- Never suggest disabling HMAC checkpoints, bypassing policy, or weakening
  audit retention as a quick fix.

## Command choice

Use the smallest command that answers the question:

- Integrity check: `./bin/loopgate-ledger verify`
- Event counts in the active JSONL only: `./bin/loopgate-ledger summary`
- Recent event view: `./bin/loopgate-ledger tail -verbose`
- Plain recent event metadata: `./bin/loopgate-ledger tail -n <count>`
- Confirmed local demo cleanup only: `./bin/loopgate-ledger demo-reset -yes`

Add `-repo <dir>` when diagnosing a repo that is not the current working
directory. Add `-socket <path>` to `demo-reset` when Loopgate uses a non-default
Unix socket.

## Recommended workflow

1. Read `docs/agent/agent_surfaces.yaml` for the command's authority posture.
2. Run `./bin/loopgate-ledger verify` if the user needs trust or incident
   evidence.
3. Use `summary` or `tail -verbose` only after explaining that they are
   unverified convenience views.
4. If verification fails, stop and report the failure. Do not infer history from
   unverified output.
5. Use `loopgate-doctor explain-denial` when the user has an approval id,
   request id, or Claude hook session id and needs a plain-language reason.

## Interpreting results

- `verify ok` means the active chain and configured closed segments verified,
  and HMAC checkpoint verification reached the reported status.
- A missing active ledger file during `summary` or `tail` can be normal for a
  fresh setup, but it is not proof that no events ever existed.
- `summary` counts event types in the active JSONL only. It does not inspect
  sealed rotation segments.
- `tail -verbose` prints readable redacted recent events. It should not be used
  as the only source for a compliance or incident answer.
- `demo-reset` removes local demo ledger/log state after checking that Loopgate
  is not running at the configured socket.

## Failure posture

If verification fails, say the ledger is not currently trusted and include the
error class or command stderr. The next safe step is diagnosis, not repair.

If `summary` or `tail` prints malformed entries, say that the active ledger view
contains malformed content and run `verify` before drawing conclusions.
