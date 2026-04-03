# Haven Map (reference shell)

> **Product note:** The **shipped** Haven app is **native Swift** (`~/Dev/Haven`). This directory is the **in-repo Wails + Go reference** — contracts, tests, and parity experiments only; do not treat it as the canonical operator UI.

This file maps the main **`cmd/haven/`** backend files.

Use it when changing:

- capability truth
- prompt/runtime grounding
- memory persistence from Haven
- explicit memory writes and continuity diagnostics
- presence behavior
- resident utility scheduling
- task approval posture and standing Haven-local task grants
- mirrored-folder refresh behavior
- future permissioned host-access UX and the shift away from mirror-only help
- the dashboard-first shell pivot and the move away from desktop-first product thinking
- setup and settings behavior modes
- desktop-facing security and activity views

## Core Role

`cmd/haven/` is the **reference** desktop application layer (Wails).

It is responsible for:

- booting the Wails app
- holding local desktop state
- talking to Loopgate as an unprivileged client
- projecting security, activity, memory, and workspace state into the UI
- making Morph feel resident inside Haven

The desktop-first shell still exists, but the next product direction is now dashboard-first: Haven as mission control over Morph's tasks, plans, notes, tools, approvals, and work trace, with the desktop treated as a secondary ambient shell.

It is not the authority boundary.
Loopgate remains the control plane and source of truth for capability execution.

## Key Files

### Boot and Lifecycle

- `main.go`
  - app bootstrap
  - Loopgate session configuration
  - Haven capability allowlist
  - sandbox tool registry creation
  - the `haven` actor label matters because Loopgate now scopes trusted sandbox-local low-friction behavior to that actor
- `app.go`
  - `HavenApp` construction
  - shutdown coordination
  - background manager teardown

### Conversation and Tool Use

- `capabilities.go`
  - Haven capability catalog
  - session capability filtering
  - runtime-facing product-language summaries for the model
- `chat.go`
  - main Haven model loop
  - runtime facts injected into the prompt
  - tool execution through Loopgate
  - assistant message emission
  - current place where Haven tells Morph what it can do
  - now also tells Morph that Haven-native in-world actions should stay low-friction while shell and boundary-crossing work remain governed
  - durable conversation substrate underneath the ambient in-room Morph surface
- `threads.go`
  - thread metadata helpers
- `types.go`
  - thread execution state and UI response structs

### Security and System Projection

- `desktop.go`
  - Security overview and activity monitor response shaping
  - capability data projected to the frontend
  - good place for friendly capability labels and grouping
  - now also projects Loopgate-owned standing task grants and exposes the toggle path back to Loopgate
- `shared.go`
  - shared-folder status and sync bridge
  - Wails-facing shared-space entrypoint used by the frontend
  - likely future landing point for distinguishing mirror-only intake from live granted-folder help
- `folder_sync.go`
  - Haven-side background poller for granted mirrored folders
  - refreshes workspace state and leaves calm offer notes when mirrored inputs change
- `settings.go`
  - Haven settings surface
  - now owns the `Focused Help` vs `Full Presence` behavior split

### Memory and Continuity

- `memory.go`
  - wake-state loading and refresh
  - diagnostic-wake loading and caching
  - thread distillation requests to Loopgate
  - continuity summary shaping for the frontend
  - current wake snapshot access used by resident behavior
  - now carries richer task-board metadata such as task kind, source, next step, and scheduled time
  - first place to look when Haven memory feels stale or absent
  - continuity board data used by the Task Board / Todo window surfaces

### Presence and Ambient Life

- `presence.go`
  - Morph presence model
  - active anchor and status text
  - current startup/idle personality projection
  - startup continuity restore path so Morph can settle in from persisted memory
- `idle.go`
  - Haven-local idle detection
  - resident utility scheduler
  - now prefers open carry-over work before ambient behavior
- `idle_behaviors.go`
  - planning notes for carry-over work
  - ambient journaling and creation as the fallback lane only
  - now distinguishes approval-required carry-over work from standing-approved local task classes

### Visible Resident Surfaces

- `desknotes.go`
  - sticky note persistence and dismissal
- `handoff.go`
  - post-task handoff notes
- `todo.go`
  - Haven-side add/complete actions for the Task Board
  - uses `ExecuteCapability(...)` so the desktop stays a projection of Loopgate continuity
  - forwards optional execution-class metadata so Loopgate can enforce approval-vs-standing-grant behavior later
- `journal.go`
  - Journal app listing and reading
- `notes.go`
  - working-notebook listing, reading, and saving
  - private operating notes that are distinct from the Journal and Desk Notes surfaces
- `paint.go`
  - Paint app storage and gallery

### Files, Setup, and Workspace

- `workspace.go`
  - file browsing and editing
  - import/export path mapping
- `diff.go`
  - review/export/discard flow
- `setup.go`
  - first-run setup and model-provider bootstrap
  - onboarding path for folder access and presence mode
- `model_settings.go`
  - model connection and runtime tuning surfaced in Settings
- `folder_access_timeout.go`
  - timeouts and UX around folder-access / grant flows
- `memory_intent.go`
  - user-intent classification for memory-related actions (feeds prompt/runtime behavior)
- `toast.go`
  - backend-to-frontend toast events

## Current Sprint Focus

The current working set in this directory is:

- `app.go`
- `capabilities.go`
- `chat.go`
- `desktop.go`
- `folder_sync.go`
- `idle.go`
- `idle_behaviors.go`
- `main.go`
- `memory.go`
- `presence.go`
- `settings.go`
- `setup.go`
- `shared.go`
- `todo.go`

These files drive:

- what capabilities Haven asks for
- how those capabilities are described to the model
- how they are rendered to the user
- how explicit memory writes refresh continuity
- how memory and presence feel in practice
- what the frontend sees as resident continuity on startup
- how the Task Board is projected as an operational continuity surface
- how the user can explicitly add or complete durable tasks without bypassing Loopgate
- how richer task metadata survives wake-state and startup restore
- how execution-class metadata survives wake-state and keeps approval posture explicit
- how scheduled tasks stay dormant until they are actually due
- how Morph can externalize plans and scratch work into a private notebook instead of holding everything in context
- how the ambient conversation room stays presentation-only while thread durability remains authoritative underneath
- how granted mirrored folders stay fresh after launch
- how the current mirror-first model will likely evolve into permissioned host-help flows after the `Host Access and Action Model` design doc
- how resident behavior prefers useful carry-over work before journaling or art
- how onboarding and settings choose between `Focused Help` and `Full Presence`

## Relationship Notes

High-level flow:

```text
frontend action
  -> HavenApp method
  -> Loopgate client request
  -> Loopgate authority / tool execution / memory operation
  -> Haven event emission
  -> frontend state update
```

Prompt flow:

```text
chat.go runtime facts
  -> native tool defs filtered to granted Haven capabilities
  -> internal/prompt/compiler.go
  -> provider system prompt
  -> model reply
  -> tool execution through Loopgate
```

Continuity flow:

```text
user says "remember this"
  -> chat.go tool call
  -> Loopgate ExecuteCapability(memory.remember)
  -> server-side explicit remember path
  -> Haven RefreshWakeState()
  -> haven:memory_updated
  -> frontend continuity card / presence update
```

Todo flow:

```text
user adds or completes a task in Haven
  -> todo.go Wails method
  -> Loopgate ExecuteCapability(todo.add / todo.complete)
  -> server-side explicit continuity mutation
  -> Haven RefreshWakeState()
  -> haven:memory_updated
  -> Task Board / presence / Morph continuity card update
```

Standing-approval flow:

```text
user toggles an "always allowed in Haven" task class
  -> desktop.go UpdateTaskStandingGrant()
  -> Loopgate UI endpoint
  -> Loopgate standing-grant config + audit event
  -> Security room refresh
  -> Task Board tags reflect the new approval posture
```

Folder flow:

```text
host folder changes
  -> Loopgate compare-before-sync path
  -> Haven folder_sync poll
  -> changed mirror emits haven:file_changed
  -> workspace area refresh + calm desk note offer
```

## Important Watchouts

- Do not let Haven invent authority. Capability execution must still go through Loopgate.
- If capability names shown to the user are friendlier than internal IDs, keep the internal IDs intact underneath.
- Do not overclaim memory reliability in prompt/runtime facts.
- Explicit remembered facts and inferred continuity are different lanes. Do not blur them for convenience.
- Presence should be tied to real work, not fake rituals.
- Mirrored-folder refresh should stay Loopgate-mediated; Haven should not invent direct host watchers or bypass the audited sync path.
- Ambient behavior is now secondary. If carry-over work exists, do not let decorative behavior outrank it.
- Haven’s visible conversation can be ephemeral, but thread history, approvals, and activity evidence still need to survive underneath.
- The standalone Morph CLI has been removed; Haven + Loopgate are the active product surfaces.
