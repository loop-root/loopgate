# Wails + React frontend — **reference shell only**

> **Not a product UI.** This TypeScript tree exists in the Loopgate repo as a **reference implementation** for Loopgate contracts, Wails bindings, and automated tests — not for operator-facing ship. Prefer **MCP**-connected IDEs for real workflows (`docs/setup/LOOPGATE_MCP.md`).

TypeScript/React UI for the in-repo Wails shell. The Go backend lives in `cmd/haven/`; bindings are generated into `src/wailsjs/`.

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
| `src/app/` | Shared app-level types/constants |
| `src/hooks/` | Desktop hooks — see `hooks/README.md` |
| `src/components/` | Shell UI, floating windows, feature panes |
| `src/lib/haven.tsx` | IDs, wallpapers, window defaults, shared TS types |

## Shell layout

- **Default:** `workstation` — center workspace + right assistant rail; other tools as floating windows.
- **Classic:** `localStorage.setItem("haven-shell-layout", "classic")` or **View → Classic desktop layout** for the older full-desktop arrangement.

## Conventions

- New Wails event subscriptions or polls: add a focused hook under `src/hooks/` rather than inline effects in `App.tsx`.
- New floating app window: extend `AppID` in `lib/haven.tsx`, add `components/windows/YourWindow.tsx`, and wire `HavenFloatingWindows.tsx` + props from `App.tsx`.
