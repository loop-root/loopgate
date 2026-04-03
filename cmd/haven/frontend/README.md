# Haven frontend (Wails + React) — **reference / deprecated**

> **Not the product UI.** The maintained Haven desktop app is **native Swift** in a separate repository (typical dev path: `~/Dev/Haven`). This TypeScript tree exists in the Morph repo as a **reference implementation** for Loopgate contracts, Wails bindings, and automated tests—not for operator-facing ship.

TypeScript/React UI for the legacy in-repo Haven shell. The Go backend lives in `cmd/haven/`; bindings are generated into `src/wailsjs/`.

## Commands

```bash
npm install
npm run build    # tsc && vite build
npm run dev      # Vite dev (typically used together with Wails)
```

## Layout

| Directory | Purpose |
|-----------|---------|
| `src/App.tsx` | Composes hooks, handlers, and shell layout (boot → setup → desktop) |
| `src/app/` | Shared app-level types/constants (e.g. `havenTypes.ts`) |
| `src/hooks/` | `useHaven*` and desktop drag hooks — see `hooks/README.md` |
| `src/components/` | Shell UI, `HavenFloatingWindows`, `windows/*` feature panes |
| `src/lib/haven.tsx` | IDs, wallpapers, window defaults, shared TS types |

**Agent-oriented map:** `docs/HavenOS/Haven_Frontend_Source_Map.md` in the repo root.

## Shell layout

- **Default:** `workstation` — center **Workspace**, right **Morph** rail; Loopgate/Notes/Journal/etc. stay as floating windows.
- **Classic:** `localStorage.setItem("haven-shell-layout", "classic")` or **View → Classic desktop layout** for the old full desktop + floating Morph/Workspace.
- Roadmap: `docs/HavenOS/plans/2026-03-22-workspace-first-haven-ui.md`.

## Conventions

- New Wails event subscriptions or polls: add a focused hook under `src/hooks/` rather than inline effects in `App.tsx`.
- New floating app window: extend `AppID` in `lib/haven.tsx`, add `components/windows/YourWindow.tsx`, and wire `HavenFloatingWindows.tsx` + props from `App.tsx`.
