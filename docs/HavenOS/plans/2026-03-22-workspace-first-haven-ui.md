**Last updated:** 2026-03-24

# Plan: Haven workstation shell (OS desktop + proactive desk)

**Status:** Phase 0 shipped in code (dock, dock edge preference, classic icons → dock, workstation workspace collapse); later phases sequenced below.  
**North star:** Aligns with `HavenOS_Northstar.md` and a **real desktop metaphor**: wallpaper, dock, optional floating windows — not a chat-first product.  
**Implementation map:** `docs/HavenOS/Haven_Frontend_Source_Map.md` (update when shell components change).

## Visual identity (futuristic retro)

- **Direction:** Warm **beige / paper / stone** chrome, **soft spectrum accents** (muted rainbow: think early Apple strip and System-era badges), **modern spacing and type** so it does not read as pastiche.
- **Dock icons:** App icons live on the **dock** (unified tile treatment: rounded plate, subtle inner shadow, optional thin rainbow accent on hover/active). Full icon redraw is **ongoing**; Phase 0 establishes the **chrome wrapper** and layout so new art drops in consistently.
- **Wallpaper / theme:** User-owned; `WallpaperTheme` continues to drive CSS variables.

## Interaction model (coworker / proactive space)

- **Primary story is observation, not chat.** Desk **sticky notes** are a main Morph → you channel; the room feels **proactive**.
- **Morph chat is a right rail in workstation mode** — minimal, semi-opaque **HUD-style** panel so the **center stays visually primary** (wallpaper + notes + files).
- **Workspace is not always on screen.** The embedded workspace column is **hidden by default**; it opens when the user toggles **Workspace** on the dock, opens a desktop file into the workspace, or completes an import/drop that adds files (Phase 0). Further triggers (e.g. Morph-run-driven) can be added later with explicit product rules.
- **Dock = OS launcher.** **Default dock edge: bottom** (familiar desktop). Users can move the dock to **left** or **right** via **View** menu (persisted `localStorage`). Desktop **app** icons are **removed** in favor of **dock launchers** (same `AppID` set); **desktop file** icons and desk notes stay on the wallpaper in classic mode.
- **Workstation dock launchers** omit **Morph** (Morph is always the right rail). **Classic** dock includes **Morph** plus the other apps.

## Product principle (preserve)

- **Loopgate** permission model, approvals, and audit semantics are **invariants** — UI may surface them differently, not weaken them.
- **Theme / wallpaper** remains user-owned; workstation shell keeps wallpaper on the stage background.
- **Desk notes + presence** stay; over time make them **Morph-driven** (content/events from backend), not static props only.

## Phase 0 — Shell dock + conditional workspace (**current**)

**Goals:**

1. **HavenShellBar / TaskBar:** Launcher strip (dock tiles) + status tray; optional **vertical** layout when dock edge is left/right.
2. **`useHavenDockEdge`:** `haven-dock-edge`: `bottom` | `left` | `right` (default `bottom`).
3. **`useWorkstationWorkspaceOpen`:** Persist `haven-workstation-workspace-open`; default **false** so first paint is desk-forward.
4. **`HavenWorkstationStage`:** Render embedded **Workspace** column only when `workspaceVisible`; **Morph** stays on the **right**; assistant panel uses HUD-oriented styling.
5. **`DesktopSurface`:** Remove desktop **app** icon grid; avatar uses **desk** anchoring when icons are absent.
6. **`MenuBar`:** **View → Dock on bottom / left / right**.
7. **Wire opens:** Import, drop-import, and **open desktop file** show workspace surface in workstation mode.

**Code (primary):**

- `src/hooks/useHavenDockEdge.ts`, `src/hooks/useWorkstationWorkspaceOpen.ts`
- `src/lib/haven.tsx` — `DOCK_LAUNCHER_APPS_CLASSIC`, `DOCK_LAUNCHER_APPS_WORKSTATION`, `ICON_MAP` includes **settings**
- `src/components/TaskBar.tsx` — dock launchers + tray (replaces separate “open window” buttons for listed apps)
- `src/components/HavenWorkstationStage.tsx` — conditional workspace column
- `src/components/DesktopSurface.tsx` — no app icons; desk-only avatar positioning
- `src/components/MenuBar.tsx` — dock edge actions
- `src/App.tsx` — `haven-shell-body` wrapper, dock handlers, workstation workspace state
- `src/App.css` — `.haven-shell-body`, `.haven-dock-*`, `.haven-taskbar--edge-*`, workstation HUD

**Out of scope for Phase 0:** Multi-pane IDE, new backend APIs, boot removal, avatar speech bubbles, full icon asset redesign.

## Phase 1 — True workspace surface (lightweight IDE feel)

- **Multi-pane layout** inside the workspace surface when shown (e.g. file tree + editor/preview split).
- **Shared visibility:** workspace selection / active path into Morph context (Go + prompt contract).
- **Refine dock triggers** for when workspace auto-opens vs stays collapsed.

## Phase 2 — Morph-owned journal (narrative)

- **Backend:** generation pipeline for automatic journal entries; append-only store under Loopgate policy.
- **Frontend:** timeline UX; migrate user-only defaults when reliable.

## Phase 3 — Activity → audit log **(Option A, preferred)**

- Filterable audit / security log with Loopgate decisions, capability traces, export.

## Phase 4 — Reading + Bookshelf

- Loopgate-mediated sandboxed read; bookshelf panel backed by durable store.

## Phase 5 — Shared terminal / runner

- Split pane wired to **`shell_exec`**; shared stdout; Wails events.

## Phase 6 — Projects (first-class)

- Project entity, switching project reloads workspace + thread + memory scope.

## Phase 7 — Outbox / deliveries

- Artifacts handoff UX; Loopgate audit where relevant.

## Phase 8 — Simplify boot + instant feel

- Skippable boot; instant second launch; dock remains primary nav.

## Testing / safety

- Any new capability surface: **deny-by-default** in Loopgate; UI is projection only.
- Add frontend tests for layout mode (`excludeWindowIds`, no duplicate morph/workspace).
- E2E or manual checklist: approval flow, workspace import, thread send in both layouts; dock toggles workspace visibility.

## References

- `docs/HavenOS/Desktop Blueprint.md`
- `docs/HavenOS/HavenOS_Frontend_Structure.mmd`
- `AGENTS.md` — Haven desktop UI section
