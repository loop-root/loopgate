**Last updated:** 2026-04-11

# Haven Single-Workspace SwiftUI MVP

This document defines the recommended MVP shape for a native Haven operator
client if we move forward with SwiftUI.

It intentionally replaces the idea of "five screens" with one governed
workspace.

## 1. Product decision

Build Haven as a **single macOS workspace window** backed by Loopgate's typed
local HTTP API.

Do not build the MVP as:

- a browser-first shell
- a set of disconnected primary screens
- a UI that reads Loopgate runtime files directly
- a UI that talks directly to Ollama, Anthropic, or OpenAI for settings

The right shape is:

- one native macOS window
- one operator workspace
- multiple panels and drawers inside that workspace
- Loopgate remains the only authority boundary

## 2. Why SwiftUI is the right MVP choice

SwiftUI is the conservative choice for a macOS-first operator surface because:

- the UI contract is already local and typed
- the control plane is already local HTTP over the Unix socket
- the browser bootstrap path still has a documented same-user launch-token race
- a native client can remain a presentation layer without inventing a second
  transport or secret model

Relevant contract:

- [UI Surface Contract](./ui_surface_contract.md)
- [Loopgate HTTP API for local clients](../setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md)

## 3. Single-workspace interaction model

The workspace should feel like one governed desk, not a tab jungle.

### 3.1 Layout

Use one main window with three stable regions:

1. **Left dock rail**
   - launch/focus controls
   - compact mode toggles
   - connection / health badge

2. **Center desk**
   - the main surface
   - notes-first and activity-first
   - proactive Morph presence
   - conditional workspace content when work is active

3. **Right context panel**
   - chat transcript and composer
   - approvals
   - memory / task board / goals
   - settings
   - file preview when needed

This is still a single workspace because the center desk remains the primary
surface and the side panels are contextual tools, not separate app destinations.

### 3.2 Default view

Default state should be:

- desk visible
- chat de-emphasized but reachable
- approvals visible when pending
- workspace panel hidden unless:
  - the user explicitly opens it, or
  - Morph is doing workspace-relevant work

This matches the earlier "coworker in the room" direction better than a
Messenger-style product shell.

### 3.3 What is not a separate screen

For MVP, these should stay inside the same workspace window:

- chat
- approvals
- settings
- memory inventory
- task board / goals
- file preview

If something needs focus, it should open in:

- the right context panel, or
- a bounded sheet / popover

not as a separate primary navigation mode.

## 4. SwiftUI shell recommendation

Use a single `WindowGroup` with one root workspace scene.

Recommended root shape:

```swift
@main
struct HavenApp: App {
    @State private var workspaceModel = HavenWorkspaceModel()

    var body: some Scene {
        WindowGroup {
            HavenWorkspaceView(model: workspaceModel)
                .environment(LoopgateClient.live)
                .environment(HavenEventFeed.live)
        }
    }
}
```

Recommended root view composition:

- `HavenWorkspaceView`
  - owns window-level layout only
- `WorkspaceDockRail`
- `WorkspaceDeskView`
- `WorkspaceContextPanel`

Recommended state ownership:

- one root `@Observable` workspace model for cross-workspace state
- feature-local state stays inside feature views
- services go through `@Environment`

Do not start with:

- a `TabView`
- multiple routers for multiple primary screens
- a browser-like route tree
- a giant view model that owns both UI state and transport logic

## 5. MVP workspace modes

The dock and context panel should switch between a small number of modes, but
they are still modes of one workspace, not distinct screens.

Recommended modes:

- `desk`
- `chat`
- `approvals`
- `memory`
- `settings`

Recommended center-desk content:

- current activity cards from `GET /v1/ui/events`
- pending approval summary from `GET /v1/ui/approvals`
- recent notes / journal
- task board summary
- workspace panel only when active

## 6. Backend contract to use

The MVP should use only existing typed Loopgate routes.

### 6.1 Core session and health

- `GET /v1/health`
- `POST /v1/session/open`
- `GET /v1/status`
- `GET /v1/ui/status`
- `GET /v1/ui/events`

### 6.2 Chat and approvals

- `POST /v1/chat`
- `GET /v1/ui/approvals`
- `POST /v1/ui/approvals/{id}/decision`

### 6.3 Workspace and file preview

- `POST /v1/ui/workspace/list`
- `GET /v1/ui/workspace/host-layout`
- `POST /v1/ui/workspace/preview`

### 6.4 Notes and journal

- `GET /v1/ui/working-notes`
- `GET /v1/ui/working-notes/entry`
- `POST /v1/ui/working-notes/save`
- `GET /v1/ui/journal/entries`
- `GET /v1/ui/journal/entry`
- `GET /v1/ui/desk-notes`
- `POST /v1/ui/desk-notes/dismiss`

### 6.5 Memory, tasks, and goals

- `GET /v1/ui/memory`
- `POST /v1/ui/memory/reset`
- `GET /v1/tasks`
- `PUT /v1/tasks/{id}/status`
- `POST /v1/agent/work-item/ensure`
- `POST /v1/agent/work-item/complete`

`GET /v1/tasks` already returns goals plus task items, so the MVP should not
invent a separate goals transport.

### 6.6 Settings

- `GET /v1/model/settings`
- `POST /v1/model/settings`
- `GET /v1/settings/idle`
- `POST /v1/settings/idle`
- `GET /v1/settings/shell-dev`
- `POST /v1/settings/shell-dev`

## 7. What the UI must not do

The MVP must not:

- read `runtime/state/*` directly
- tail audit logs directly
- parse command output as UI state
- call Ollama or provider APIs directly for settings
- hold approval decision nonces
- hold Loopgate MAC material in browser-exposed code
- become a fallback execution path if Loopgate is unavailable

That is not just implementation preference; it is required by
[UI Surface Contract](./ui_surface_contract.md).

## 8. Suggested MVP build order

### Phase 0 — contract and hardening

Already in good shape:

- typed UI contract exists
- Haven-only route trust model exists
- model/settings and chat runtime error redaction now fail closed without
  leaking backend detail

Still worth doing before polish:

- continue sweeping operator-facing routes for raw internal error reflection

### Phase 1 — native shell

Build one macOS app shell with:

- one workspace window
- one dock rail
- one desk surface
- one context panel
- live `ui.status` and `ui.events`

No tabs. No secondary primary windows.

### Phase 2 — governed chat + approvals

Add:

- `POST /v1/chat` SSE client
- approval inbox
- approval decision submission
- minimal transcript persistence in UI state only

### Phase 3 — desk productivity surface

Add:

- working notes
- desk notes
- journal entries
- task board
- goals projection through `/v1/tasks`

### Phase 4 — conditional workspace

Add:

- workspace list
- preview
- conditional workspace visibility rules

Do not make the workspace surface permanent by default.

## 9. First implementation slice

If we start immediately, the first real slice should be:

1. a native SwiftUI app shell with one window
2. UDS HTTP client + session open
3. `ui.status` + `ui.events`
4. right-side chat / approvals panel
5. center desk with placeholder notes/activity cards

That slice proves:

- the transport
- the trusted-session shape
- the single-workspace layout
- the product direction

without forcing us to solve every tool and every panel before we can use it.

## 10. Recommendation

Proceed with SwiftUI, but keep the implementation discipline strict:

- one workspace
- one authority boundary
- typed Loopgate routes only
- no browser bootstrap in the MVP path
- no five-screen architecture

If we keep those constraints, the MVP stays aligned with Loopgate instead of
becoming a second system with its own hidden trust model.
