# Tools Map

This file maps the typed tool layer under `internal/tools/`.

Use it when changing:

- the real capability surface available to operator clients (via Loopgate)
- native structured tool definitions
- `haven`-actor-scoped tool registration (legacy actor label; see policy)
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
  - sandbox tools now carry an explicit trusted-sandbox-local wrapper so Loopgate can treat **actor-scoped** in-world actions differently from host-rooted/default registries
- `trusted_sandbox_tool.go`
  - marker wrapper for trusted sandbox-local tools (typically `haven` actor)
  - used so Loopgate can reduce approval friction for the `haven` actor without trusting the same tool names globally
- `journal_tools.go`
  - journal list/read/write tools
- `haven_operator_context.go`
  - read-only `haven.operator_context` tool: Loopgate-maintained operator guide for Haven (mounts, TUI, troubleshooting); trusted-sandbox-local registration
- **Operator mount tools** (`operator_mount.fs_*`) are registered from `internal/loopgate/operator_mount.go` (not this package): session-scoped host directory access from Haven `operator_mount_paths` on `POST /v1/session/open`, accepted only when Loopgate is pinning the expected Haven client executable; optional `primary_operator_mount_path` selects the default root for relative paths without widening allowed roots
- `paint_tools.go`
  - flat prompt-oriented paint save/list tools
- `note_create.go`
  - sticky-note creation tool that writes desk-note state (`haven_desk_notes.json`)
- `memory_tools.go`
  - explicit remembered-fact tool definition
  - capability contract only; real execution is routed through Loopgate's dedicated memory path
- `todo_tools.go`
  - Todo add/complete/list capability definitions
  - contract only; real execution is routed through Loopgate's dedicated continuity path
  - add now carries task-board metadata such as task kind, source, next step, scheduled time, and execution class
- `notes_tools.go`
  - working-notes list/read/write tools for private notebook-style scratch space under `scratch/notes`
  - intentionally separate from Journal because this is operational working memory, not reflective writing
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

That means new **trusted sandbox-local** tools should initially use flat APIs like:

- `journal.write(title, body)`
- `note.create(kind, title, body)`

and not nested object graphs like a full paint stroke array.

## Current Sprint Focus

The current working set in this directory is:

- `defaults.go`
- `journal_tools.go`
- `paint_tools.go`
- `note_create.go`
- `memory_tools.go`
- `todo_tools.go`
- `notes_tools.go`

The next capability pass should make this directory the source of truth for:

- journal tools
- paint/save tools with flat arguments
- sticky note tools
- explicit remember tools
- todo tools
- working-notes tools

## Important Watchouts

- Keep new tools sandbox-local unless the design explicitly crosses a trust boundary.
- Do not add tool categories that bypass the policy checker unless the checker is updated deliberately.
- Tool descriptions become part of the model contract. Keep them precise and product-aligned.
- If a tool needs an authority path outside normal `tool.Execute(...)`, document that clearly and keep tests aligned with the real server dispatch path.
