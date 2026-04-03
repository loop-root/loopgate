# Threadstore Map

This file maps `internal/haven/threadstore/`, Haven’s append-only Messenger thread persistence.

Use it when changing:

- thread JSONL append semantics
- index rebuild behavior
- redaction before persist (secrets, tool output)
- workspace-scoped thread listing

## Core Role

`internal/haven/threadstore/` is the **local thread/event store** for Haven Messenger: per-thread append-only JSONL, a rebuildable index, and **centralized redaction** on append so raw secrets and raw tool output are not written to disk.

It is Morph/Haven-side durability, not Loopgate’s authoritative audit ledger (see `internal/ledger/` for that boundary).

## Key Files

- `store.go`
  - `Store`, `NewStore`, append/list/thread lifecycle
  - invariants documented in package comment (append-only, index rebuildable)

- `types.go`
  - thread summaries and event types used by the store

- `redact.go` / `redact_test.go`
  - redaction applied before persistence

- `store_test.go`
  - store behavior tests

## Relationship Notes

- Haven backend callers: `cmd/haven/threads.go`, `cmd/haven/chat.go` (and related)
- Loopgate audit: separate system; do not confuse thread JSONL with control-plane audit

## Important Watchouts

- Never disable redaction “for debugging” in paths that persist to disk.
- Preserve append-only semantics; repairing history belongs in migration tools, not silent rewrite.
