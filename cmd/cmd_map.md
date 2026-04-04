# Cmd Map

This file maps **`cmd/`** Go **main** entrypoints. **`cmd/loopgate/`** is the primary server binary. **`cmd/haven/`** is a **Wails reference** shell (contracts/tests only)—see `haven_map.md` and `frontend_map.md` when working on that tree.

Use it when changing:

- how Loopgate binds its socket or accepts policy
- morphling out-of-process runner contracts
- flags and startup diagnostics shared with `start.sh`

## Core Role

`cmd/` contains small binaries. **Loopgate** (`cmd/loopgate/`) is the primary shipped server from this repository. **`cmd/haven/`** is reference/frozen for product UX per root `AGENTS.md`.

## `cmd/loopgate/`

- `main.go`
  - constructs socket path `runtime/state/loopgate.sock` under cwd-as-repo-root
  - optional `-accept-policy` for policy hash acknowledgment
  - optional `-admin` to start the **loopback admin console** TCP listener when `admin_console.enabled` is true in `config/runtime.yaml` (requires `LOOPGATE_ADMIN_TOKEN`); see `docs/setup/ADMIN_CONSOLE.md`
  - runs `memory.InspectUnsupportedRawMemoryArtifacts` with warnings to stderr
  - starts `loopgate.NewServerWithOptions` and runs until signal

## `cmd/morphling-runner/`

- `main.go`
  - reads JSON `TaskPlanRunnerConfig` from **stdin**, calls `loopgate.RunMorphlingRunnerProcess`, writes JSON result to **stdout**
  - separate process for lease-bound morphling execution; **not** a sandbox boundary by itself (see file comment)

## Relationship Notes

- Control plane implementation: `internal/loopgate/loopgate_map.md`
- Launcher script: `start.sh` (repo root)

## Important Watchouts

- Loopgate must stay on local Unix socket transport by default (see AGENTS).
- Runner stdin/stdout JSON is a trust boundary — callers must validate.
