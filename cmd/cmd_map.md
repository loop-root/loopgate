# Cmd Map

This file maps **`cmd/`** Go **main** entrypoints. **`cmd/loopgate/`** is the primary server binary. Any remaining legacy shells under `cmd/` are deletion candidates, not active product surfaces.

Use it when changing:

- how Loopgate binds its socket or accepts policy
- morphling out-of-process runner contracts
- flags and startup diagnostics for the active binaries

## Core Role

`cmd/` contains small binaries. **Loopgate** (`cmd/loopgate/`) is the primary shipped server from this repository. The active product-facing surfaces are Loopgate itself, the **HTTP-on-UDS** control plane, and typed local APIs used by direct clients such as Haven. **In-tree MCP removed** (ADR 0010).

## `cmd/loopgate/`

- `main.go`
  - constructs socket path `runtime/state/loopgate.sock` under cwd-as-repo-root
  - optional `-accept-policy` for policy hash acknowledgment
  - runs `memory.InspectUnsupportedRawMemoryArtifacts` with warnings to stderr
  - starts `loopgate.NewServerWithOptions` and runs until signal

## `cmd/morphling-runner/`

- `main.go`
  - reads JSON `TaskPlanRunnerConfig` from **stdin**, calls `loopgate.RunMorphlingRunnerProcess`, writes JSON result to **stdout**
  - separate process for lease-bound morphling execution; **not** a sandbox boundary by itself (see file comment)

## Relationship Notes

- Control plane implementation: `internal/loopgate/loopgate_map.md`

## Important Watchouts

- Loopgate must stay on local Unix socket transport by default (see AGENTS).
- Runner stdin/stdout JSON is a trust boundary — callers must validate.
