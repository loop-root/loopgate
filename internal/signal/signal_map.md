# Signal Map

This file maps `internal/signal/`, graceful **SIGINT/SIGTERM** handling for long-running processes.

Use it when changing:

- shutdown semantics for servers or CLI loops
- contexts canceled on interrupt

## Core Role

`internal/signal/` provides a small `Handler` that registers for interrupt signals and exposes `Wait` and `Context()` for coordinated shutdown.

## Key Files

- `signal.go`
  - `NewHandler`, `Wait`, `Context`, cleanup

- `signal_test.go`
  - behavior tests where applicable

## Relationship Notes

- `cmd/loopgate/main.go` may use `signal.NotifyContext` directly; this package is available for shared patterns.

## Important Watchouts

- Ensure goroutines exit cleanly; avoid holding locks across signal handling.
