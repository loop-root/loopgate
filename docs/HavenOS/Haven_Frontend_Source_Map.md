**Last updated:** 2026-03-29

# Haven frontend source map (Wails reference shell)

> **Scope:** This map describes **`cmd/haven/frontend/`** in **this (Loopgate) repository**—a **reference / deprecated** Wails+React shell used for contracts and tests. The **canonical Haven UI** is the **Swift/macOS app** in a separate checkout (e.g. `~/Dev/Haven`). Product UX and Loopgate client work should land there first; keep this tree updated only when you intentionally mirror or validate behavior in the reference shell.

For agents working on the in-repo Wails/React desktop shell (`cmd/haven/frontend/`). This doc stays aligned with the **modular `App.tsx` refactor**: orchestration is split into hooks and focused components so behavior stays testable and boundaries stay obvious.

## Build and entry

| Path | Role |
|------|------|
| `cmd/haven/frontend/package.json` | `npm run build` → `tsc && vite build`; `npm run dev` for Vite dev server (with Wails as usual) |
| `cmd/haven/frontend/src/main.tsx` | React root |
| `cmd/haven/frontend/src/App.tsx` | **Shell orchestrator**: Wails bindings, thread/chat handlers, workspace + dialogs, composes hooks + `HavenFloatingWindows`; **`HavenWorkstationStage`** when `haven-shell-layout` is `workstation` (default), else **`DesktopSurface`** (classic floating-desktop) |
| `cmd/haven/frontend/src/App.css` | Global Haven chrome + window themes |

## Shared types and constants

| Path | Role |
|------|------|
| `src/app/havenTypes.ts` | Dialog/drag-related TypeScript types, `DRAG_THRESHOLD`, `NEW_WORKING_NOTE_PATH`, `finiteWindowValue` (window frame restore) |
| `src/lib/haven.tsx` | `AppID`, wallpaper registry, `WinState`, icons, `ICON_MAP` (includes settings), `DOCK_LAUNCHER_APPS_*`, `WIN_DEFAULTS`, `resolveWindowFrame`, toast/context types |

## Hooks (`src/hooks/`)

| Hook | Responsibility |
|------|------------------|
| `useHavenWindowManager` | Floating window map, z-order, focus, open/close/collapse/drag/resize, persisted frame geometry |
| `useHavenBootSetup` | Boot sequence lines, fade, `CheckSetup()` → `needsSetup` |
| `useHavenToast` | Toast list + `pushToast` (auto-dismiss) |
| `useDesktopIconDrag` | Desktop icon positions (localStorage), drag threshold + listeners; exposes `setIconPositions` for `haven:icon_positions_changed` |
| `useDesktopFileDrag` | Desktop file icons: list + drag + persistence |
| `useHavenClock` | Menu bar clock string, 30s tick |
| `useActivityStatusPoll` | Polls `ActivityStatus` when not booting (8s) |
| `useLoopgateSecurityPoll` | When Loopgate window is open, refresh security overview on interval |
| `useHavenPresence` | Initial `GetPresence` + `haven:presence_changed` |
| `useHavenMemorySync` | `GetMemoryStatus` + `haven:memory_updated`; exposes `memoryLoaded` for startup layout gating |
| `useDeskNotesSync` | `ListDeskNotes` + `haven:desk_notes_changed` |
| `useHavenBackendToastBridge` | `haven:toast` → `pushToast` |
| `useRemoteIconPositionsSync` | `haven:icon_positions_changed` merge + localStorage |
| `useGlobalMenuDismiss` | Document click closes context menu + menu bar |
| `useWailsFileDrop` | `OnFileDrop` / `OnFileDropOff` → pending import drop |
| `useHavenMorphSessionEvents` | Thread-scoped runtime events: assistant messages, execution state, approvals, tool start/result, `haven:security_alert` (auto-open Loopgate) |
| `useWorkspaceWindowEffects` | Empty workspace load when window opens; `haven:file_changed` refresh for workspace |
| `useHavenThreadBootstrap` | Initial `ListThreads`, restore `haven-active-thread-id`, calls `selectThread` from `App` (`selectThread` is a `useCallback` so the hook can subscribe on mount) |
| `useJournalWindowState` | Journal list + active entry + loaders; refresh when Journal window opens; `haven:file_changed` for journal paths |
| `useWorkingNotesWindowState` | Working notes list + active note + save; refresh when Notes window opens; `haven:file_changed` / `notes_write` |
| `useHavenShellLayout` | `workstation` (default) vs `classic`; persists `haven-shell-layout` in `localStorage` |
| `useHavenDockEdge` | Dock on `bottom` (default), `left`, or `right`; persists `haven-dock-edge` |
| `useWorkstationWorkspaceOpen` | Workstation embedded workspace rail visible or collapsed; persists `haven-workstation-workspace-open` |

See `src/hooks/README.md` for a short index.

## Major components

| Path | Role |
|------|------|
| `src/components/HavenFloatingWindows.tsx` | Maps open windows to `HavenWindow` + per-app bodies; supports `excludeWindowIds` for docked apps in workstation mode |
| `src/components/HavenWorkstationStage.tsx` | Workstation: env strip + collapsible center `WorkspaceWindow` (CSS collapse, stays mounted) + right Morph HUD rail + float layer |
| `src/components/DesktopSurface.tsx` | Classic: wallpaper, avatar, desk notes, desktop **file** icons only (app launchers live on dock) |
| `src/components/HavenWindow.tsx` | Window chrome, drag/resize |
| `src/components/MenuBar.tsx` | Top bar; View includes layout + dock edge |
| `src/components/TaskBar.tsx` | OS-style dock launchers + status tray; vertical layout when dock edge is left/right |
| `src/components/SetupWizard.tsx` | First-run: welcome → model (Ollama local or Anthropic cloud) → folder grants → **Finish**; defaults for name/wallpaper/presence/background (change in **Settings**) |
| `src/components/windows/*.tsx` | Feature windows (Morph chat, Loopgate security, workspace, notes, journal, paint, todo, settings) |
| `src/components/SettingsPanel.tsx` | Haven settings: identity, **model provider** (local Ollama vs Anthropic cloud), wallpaper, behavior, shared folder |
| `src/components/settings/ModelProviderSection.tsx` | Model section UX: `GetModelSettings` / `SaveModelProviderSettings` / `SaveModelSelection`; cloud wizard (provider → key) and local endpoint flow |

## Wails bindings

Generated under `frontend/wailsjs/` (see `cmd/haven/wails.json` `wailsjsdir`). Regenerate with `wails generate module` from `cmd/haven` when Go API changes; if a new `HavenApp` method is added, keep `wailsjs/go/main/HavenApp.{js,d.ts}` in sync until generate is run.

## Related design docs

- `docs/HavenOS/plans/2026-03-22-workspace-first-haven-ui.md` — **Haven shell** roadmap (dock, optional workspace column, right Morph rail, visual direction; later phases for journal, audit, projects, terminal, outbox)
- `docs/HavenOS/Desktop Blueprint.md` — UX layout (links here for implementation map)
- `docs/HavenOS/HavenOS_Architecture.mmd` — product-level architecture
- `docs/HavenOS/HavenOS_Frontend_Structure.mmd` — React/Wails layering diagram

## Maintenance

When you add a **new cross-cutting subscription** (EventsOn, timers), prefer a **`useHaven*`** hook in `src/hooks/` over growing `App.tsx`. When you add a **new window**, extend `AppID` / `haven.tsx`, add a `components/windows/*` module, and wire a branch in `HavenFloatingWindows.tsx` plus props from `App.tsx`. For a window with **substantial local state and file watchers** (like Journal or Notes), follow `useJournalWindowState` / `useWorkingNotesWindowState`: a dedicated hook that takes `pushToast` and `*WindowOpen` booleans derived from `windows.*`.
