# React hooks (reference Wails shell)

Hooks used by `App.tsx` to keep the shell readable. Naming: `useHaven*` for shell/runtime integration, `useDesktop*` for desktop icon/file drag.

| File | Summary |
|------|---------|
| `useHavenWindowManager.ts` | Window stack, geometry persistence, z-order |
| `useHavenBootSetup.ts` | Boot animation + `CheckSetup` |
| `useHavenToast.ts` | Toasts + `pushToast` |
| `useDesktopIconDrag.ts` | Icon positions + drag; exposes `setIconPositions` for backend sync |
| `useDesktopFileDrag.ts` | Desktop file list + drag |
| `useHavenClock.ts` | Clock string for menu bar |
| `useActivityStatusPoll.ts` | Activity status polling |
| `useLoopgateSecurityPoll.ts` | Security overview polling when Loopgate is open |
| `useHavenPresence.ts` | Presence fetch + `haven:presence_changed` |
| `useHavenMemorySync.ts` | Memory status + `haven:memory_updated` |
| `useDeskNotesSync.ts` | Desk notes list + `haven:desk_notes_changed` |
| `useHavenBackendToastBridge.ts` | `haven:toast` → UI toasts |
| `useRemoteIconPositionsSync.ts` | `haven:icon_positions_changed` |
| `useGlobalMenuDismiss.ts` | Click-away for menus |
| `useWailsFileDrop.ts` | Native file drop → pending import |
| `useHavenMorphSessionEvents.ts` | Chat/thread runtime events + security alert window |
| `useWorkspaceWindowEffects.ts` | Workspace list load + file-changed refresh |
| `useHavenThreadBootstrap.ts` | Initial thread list + persisted active thread selection |
| `useJournalWindowState.ts` | Journal entries, active entry, journal `haven:file_changed` |
| `useWorkingNotesWindowState.ts` | Working notes, save, notes `haven:file_changed` |
| `useHavenShellLayout.ts` | `workstation` (default) vs `classic` desktop; `localStorage` key `haven-shell-layout` |
| `useHavenDockEdge.ts` | Dock position `bottom` (default) / `left` / `right`; `haven-dock-edge` |
| `useWorkstationWorkspaceOpen.ts` | Embedded workspace column visibility; `haven-workstation-workspace-open` |
