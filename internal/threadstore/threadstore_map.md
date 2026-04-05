# Threadstore Map

This file maps `internal/threadstore/`, append-only thread persistence for Loopgate-backed chat clients.

Use it when changing:

- thread JSONL append semantics
- index rebuild behavior
- redaction before persist (secrets, tool output)
- workspace-scoped thread listing

## Core Role

`internal/threadstore/` is the local thread/event store for chat-style operator clients: per-thread append-only JSONL, a rebuildable index, and centralized redaction on append so raw secrets and raw tool output are not written to disk.

It is **client-side** durability, not Loopgate’s authoritative audit ledger (see `internal/ledger/` for that boundary).

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

- Active Loopgate callers: `internal/loopgate/server_haven_chat.go`, `internal/loopgate/server_haven_continuity.go`
- Loopgate audit: separate system; do not confuse thread JSONL with control-plane audit

## Important Watchouts

- Never disable redaction “for debugging” in paths that persist to disk.
- Preserve append-only semantics; repairing history belongs in migration tools, not silent rewrite.
