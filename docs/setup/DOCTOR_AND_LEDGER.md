**Last updated:** 2026-04-15

# Doctor And Ledger

Loopgate ships two different local troubleshooting tools:

- `loopgate-ledger`
- `loopgate-doctor`

They overlap a little, but they answer different questions.

## Use `loopgate-ledger` when you need to trust or inspect the audit history

Primary uses:
- verify the append-only audit chain
- verify HMAC checkpoints in the shipped macOS-first default
- inspect recent events
- reset local demo state intentionally

Most useful commands:

```bash
go run ./cmd/loopgate-ledger verify
go run ./cmd/loopgate-ledger summary
go run ./cmd/loopgate-ledger tail -verbose
```

What each one is for:
- `verify`
  - trust check for the active audit chain and sealed segments
- `summary`
  - quick count of event types in the active JSONL only
- `tail -verbose`
  - readable last-N event view for operator review
- `demo-reset -yes`
  - destructive local cleanup for demo state only

Use `loopgate-ledger` first when the question is:
- "Can I trust this local audit history?"
- "What just happened in the last few events?"
- "Did this approval or denial really get recorded?"

## Use `loopgate-doctor` when you need a broader local diagnostic snapshot

Primary uses:
- build an offline diagnostic report from repo state
- write a troubleshooting bundle with log tails
- ask a running Loopgate instance for audit-export trust status

Most useful commands:

```bash
go run ./cmd/loopgate-doctor report
go run ./cmd/loopgate-doctor bundle -out ./tmp/loopgate-bundle
go run ./cmd/loopgate-doctor trust-check
```

What each one is for:
- `report`
  - offline JSON summary of runtime config, diagnostics, and ledger verification state
- `bundle`
  - `report.json` plus diagnostic log tails for sharing or later inspection
- `trust-check`
  - live query against a running Loopgate instance for audit-export trust preflight

Use `loopgate-doctor` first when the question is:
- "What is this repo/runtime configured to do right now?"
- "Can I package a local troubleshooting bundle?"
- "Is audit export trust configured and healthy?"

## Short rule of thumb

- `loopgate-ledger` = audit history and integrity
- `loopgate-doctor` = broader local diagnostics and operator bundle output

## Typical operator flow

After a denial, approval surprise, or suspicious local behavior:

1. run `go run ./cmd/loopgate-ledger tail -verbose`
2. run `go run ./cmd/loopgate-ledger verify`
3. run `go run ./cmd/loopgate-doctor report`
4. if needed, write a bundle with `go run ./cmd/loopgate-doctor bundle -out ...`

## Read next

- [Getting started](./GETTING_STARTED.md)
- [Operator guide](./OPERATOR_GUIDE.md)
- [Ledger and audit integrity](./LEDGER_AND_AUDIT_INTEGRITY.md)
