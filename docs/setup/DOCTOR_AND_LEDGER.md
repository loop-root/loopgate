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
./bin/loopgate-ledger verify
./bin/loopgate-ledger summary
./bin/loopgate-ledger tail -verbose
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
- explain one approval or capability-request outcome directly from the verified audit ledger
- ask a running Loopgate instance for audit-export trust status

Most useful commands:

```bash
./bin/loopgate-doctor report
./bin/loopgate-doctor bundle -out ./tmp/loopgate-bundle
./bin/loopgate-doctor explain-denial -approval-id <approval-id>
./bin/loopgate-doctor explain-denial -request-id <request-id>
./bin/loopgate-doctor trust-check
```

For keychain-backed diagnostics, prefer the stable `./bin/...` binaries over
`go run`; a fresh `go run` build changes the executable identity and can cause
repeated macOS approval prompts.

What each one is for:
- `report`
  - offline JSON summary of runtime config, diagnostics, ledger verification
    state, and nonce replay persistence/utilization warnings
- `bundle`
  - `report.json` plus diagnostic log tails for sharing or later inspection
- `explain-denial`
  - walks the verified audit ledger for one `approval_request_id` or `request_id`
    and prints the current status, denial code/reason or execution-failure class
    when present, plus a short related-event timeline
- `trust-check`
  - live query against a running Loopgate instance for audit-export trust preflight

Use `loopgate-doctor` first when the question is:
- "What is this repo/runtime configured to do right now?"
- "Can I package a local troubleshooting bundle?"
- "Why did approval `X` get denied?"
- "Why did request `Y` get denied or fail?"
- "Is audit export trust configured and healthy?"

## Short rule of thumb

- `loopgate-ledger` = audit history and integrity
- `loopgate-doctor` = broader local diagnostics and operator bundle output

## Typical operator flow

After a denial, approval surprise, or suspicious local behavior:

1. run `./bin/loopgate-ledger tail -verbose`
2. run `./bin/loopgate-ledger verify`
3. if you have an approval id, run `./bin/loopgate-doctor explain-denial -approval-id <approval-id>`
4. if you have a direct request id instead, run `./bin/loopgate-doctor explain-denial -request-id <request-id>`
5. run `./bin/loopgate-doctor report`
6. if needed, write a bundle with `./bin/loopgate-doctor bundle -out ...`

## Read next

- [Getting started](./GETTING_STARTED.md)
- [Operator guide](./OPERATOR_GUIDE.md)
- [Ledger and audit integrity](./LEDGER_AND_AUDIT_INTEGRITY.md)
