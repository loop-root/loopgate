# Tools Map

This file maps the typed tool layer under `internal/tools/`.

Use it when changing:

- the real capability surface available to operator clients (via Loopgate)
- native structured tool definitions
- legacy actor-scoped tool registration labels that are still being cleaned up
- explicit remembered-fact tools
- sandbox-local execution behavior

## Core Role

`internal/tools/` defines the executable tool objects Loopgate registers and runs.

This layer is important because:

- capability names originate here
- Loopgate status surfaces registered tools from here
- model-native structured tool definitions are derived from schemas here
- policy checks use tool category and operation metadata from here

## Key Files

- `tool.go`
  - tool interface
  - schema model
  - important constraint: current schemas are flat scalar args only
- `registry.go`
  - tool registration and lookup
- `defaults.go`
  - default registry builders
  - currently the place where sandbox and default tool bundles are assembled
- **Operator mount tools** (`operator_mount.fs_*`) are registered from `internal/loopgate/operator_mount.go` (not this package): session-scoped host directory access from pinned operator mount paths on `POST /v1/session/open`; optional `primary_operator_mount_path` selects the default root for relative paths without widening allowed roots
- `memory_tools.go`
  - explicit remembered-fact tool definition
  - capability contract only; real execution is routed through Loopgate's dedicated memory path
- `fs_read.go`
- `fs_list.go`
- `fs_write.go`
  - core filesystem tools
- `shell_exec.go`
  - shell execution tool
- `path_open.go`
  - path-opening helper tool

## Current Constraint

The current native tool schema path only supports scalar arguments:

- `string`
- `path`
- `int`
- `bool`

That means new sandbox-local tools should initially use flat APIs like:

- `fs_write(path, content)`
- `memory.remember(kind, value, source)`

and not nested object graphs like a full paint stroke array.

## Current Sprint Focus

The current working set in this directory is:

- `defaults.go`
- `memory_tools.go`
- filesystem tools
- host folder tools
- shell execution tools
- capability dispatch helpers

The next capability pass should keep this directory narrow and aligned with the
active Loopgate product surface rather than reintroducing retired Haven or
task-board tools.

## Important Watchouts

- Keep new tools sandbox-local unless the design explicitly crosses a trust boundary.
- Do not add tool categories that bypass the policy checker unless the checker is updated deliberately.
- Tool descriptions become part of the model contract. Keep them precise and product-aligned.
- If a tool needs an authority path outside normal `tool.Execute(...)`, document that clearly and keep tests aligned with the real server dispatch path.
