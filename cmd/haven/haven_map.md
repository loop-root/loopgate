# `cmd/haven/` map (reference shell)

> **Not the primary integration surface.** This tree is the **in-repo Wails + Go reference** for HTTP contracts, bindings, and tests. **Production-style operator workflows** should use **MCP or proxy** (`docs/setup/LOOPGATE_MCP.md`). Loopgate remains the authority for policy, capabilities, and audit.

This file maps the main **`cmd/haven/`** Go files so engineers can find behavior quickly without product-specific framing.

## When to touch this directory

- Wails lifecycle, desktop state, or frontend event wiring
- Reference client paths that call Loopgate (chat, memory, continuity, folder sync)
- Parity tests or contract checks against the local HTTP API
- **Not** for changing core policy — that lives under `internal/loopgate` and related packages

## Core role

- Boot the Wails application and hold **local UI state only**
- Call Loopgate over the **local control-plane binding** as an unprivileged client
- Project Loopgate-backed security, activity, memory, and workspace data into the UI

Authority stays in Loopgate; this shell must not invent capabilities or bypass audited paths.

## Key files (backend)

| File | Role |
|------|------|
| `main.go` | App bootstrap, Loopgate session wiring, capability allowlist for this client; **`haven` actor** label scopes Loopgate sandbox behavior for this reference UI |
| `app.go` | App struct construction, shutdown, background task teardown |
| `capabilities.go` | Capability catalog and session filtering for the model-facing layer |
| `chat.go` | Main chat loop, runtime facts, tool execution via Loopgate |
| `threads.go`, `types.go` | Thread metadata and execution/UI response shapes |
| `desktop.go` | Security / activity projection; standing task-grant toggles forwarded to Loopgate |
| `shared.go` | Shared-folder status and Wails bridge |
| `folder_sync.go` | Background polling for granted mirrored folders (Loopgate-mediated) |
| `settings.go` | Settings surface (e.g. presence / help modes) |
| `memory.go` | Wake state, diagnostics, distillation requests to Loopgate |
| `presence.go`, `idle.go`, `idle_behaviors.go` | Presence and idle / resident scheduling behavior |
| `desknotes.go`, `handoff.go`, `todo.go`, `journal.go`, `notes.go`, `paint.go` | Feature surfaces backed by Loopgate capabilities where applicable |
| `workspace.go`, `diff.go` | File browsing, editing, review flows |
| `setup.go`, `model_settings.go` | First-run and model connection setup |
| `folder_access_timeout.go`, `memory_intent.go`, `toast.go` | UX helpers around grants, intent, and notifications |

## Typical request flow

```text
frontend action
  -> Wails app method (Go)
  -> Loopgate HTTP client
  -> Loopgate policy / tools / memory
  -> Wails event emission
  -> frontend state update
```

## Watchouts

- **No authority invention:** every privileged action must go through Loopgate with the same policy and audit guarantees as other clients.
- **Actor label:** the `haven` actor is a **compatibility label** for this reference client, not a trust grant by itself.
- **Mirrors and folders:** refresh and sync stay **Loopgate-mediated**; do not add unaudited host watchers here.
- **Memory lanes:** explicit remembers vs inferred continuity remain distinct; do not blur them in prompts or UI copy.
- **Prefer MCP for new work:** extend Loopgate and MCP handlers rather than growing this shell as a product surface.

## Related maps

- Frontend: `cmd/haven/frontend/README.md`
- Loopgate server: `internal/loopgate/loopgate_map.md`, repo `context_map.md`
