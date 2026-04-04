# Wails frontend map (reference shell)

This file maps the main frontend surfaces under `cmd/haven/frontend/`.

Use it when changing:

- desktop shell design
- Security window labels
- primary conversation surface
- presence rendering
- continuity status surfaces
- app/window composition
- onboarding flow
- settings behavior toggles

## Core Role

This directory contains the React frontend embedded by Wails.

It is responsible for:

- rendering the reference desktop shell
- subscribing to Wails events from the Go backend
- showing windows, notes, activity, and presence
- translating backend state into operator-facing language

It is not the source of truth for security or memory.

## Key Files

### App Shell

- `src/App.tsx`
  - top-level frontend orchestration
  - window open/close state
  - Wails event subscriptions
- `src/App.css`
  - current visual system, materials, spacing, and desktop styling

### Shell Components

- `src/components/DesktopSurface.tsx`
  - desktop icons, sticky notes, and on-screen assistant presence (reference shell)
  - spatial orb/glyph rendering for that presence on the desktop
- `src/components/MenuBar.tsx`
  - top bar and current status indicators
  - shell wordmark and calmer menu/status treatment
- `src/components/TaskBar.tsx`
  - dock / task switching surface
  - now uses icon-bearing pills rather than plain text window buttons
- `src/components/HavenWindow.tsx`
  - shared window chrome
  - structural shell for the softer rounded window treatment
- `src/components/SetupWizard.tsx`
  - first-run onboarding flow
  - now includes folder grants, model choice, and `Focused Help` vs `Full Presence`
- `src/components/SettingsPanel.tsx`
  - in-product configuration surface
  - mirrors the resident behavior mode and folder access state after setup

### App Windows

- `src/components/windows/MorphWindow.tsx`
  - primary ambient conversation window (historical filename; not the operator-client product surface)
  - ambient conversation surface with visible recency-faded turns
  - quiet Recall drawer for durable thread access
  - distinguishes ephemeral in-room chat from durable thread access via Recall
- `src/components/windows/LoopgateWindow.tsx`
  - Security window
  - capability label mapping and built-in vs external grouping
  - surfaces standing-approved task classes and lets the operator revoke or restore them
- `src/components/windows/ActivityWindow.tsx`
  - activity monitor surface for durable evidence outside the ambient in-room conversation
  - now intentionally styled as a calmer monitoring room rather than a raw event list
- `src/components/windows/TodoWindow.tsx`
  - operational continuity surface for open items and active goals
  - now behaves as a Task Board rather than a plain carry-over list
  - renders task kind, source, next-step, and scheduled-time metadata from Loopgate wake-state
  - shows whether a task is always allowed in this shell or still asks first
  - supports add and complete actions against Loopgate-backed continuity
  - can now create future-scheduled tasks, while the resident loop only treats them as actionable once due
- `src/components/windows/NotesWindow.tsx`
  - private working-notes room for plans, scratchpads, and notebook-style externalized working memory
  - lists notes under `scratch/notes` and edits them through Loopgate-mediated `notes.*` tools
- `src/components/windows/JournalWindow.tsx`
  - journal reader UI
  - shares the same hero/material language as the rest of the shell
- `src/components/windows/PaintWindow.tsx`
  - paint canvas and gallery
  - shares the same hero/material language as the rest of the shell
- `src/components/windows/WorkspaceWindow.tsx`
  - files, previews, review flow
  - current home of the Finder-like surface that still needs to stay usable while matching the newer shell language

### Shared UI Helpers

- `src/components/ConfirmDialog.tsx`
- `src/components/TextInputDialog.tsx`
- `src/components/ToastStack.tsx`
- `src/components/ApprovalDialog.tsx`
- `src/components/DropApprovalDialog.tsx`
- `src/components/ContextMenu.tsx`

### Shared Frontend Types

- `src/lib/haven.tsx`
  - frontend-only types, icon metadata, wallpaper helpers, shared constants, reusable glyph assets, and wordmark (historical module path)

## Current Sprint Focus

The current working set in this directory is:

- `src/components/windows/LoopgateWindow.tsx`
- `src/components/windows/MorphWindow.tsx`
- `src/components/windows/TodoWindow.tsx`
- `src/components/DesktopSurface.tsx`
- `src/components/SetupWizard.tsx`
- `src/components/SettingsPanel.tsx`
- `src/App.tsx`
- `src/lib/haven.tsx`
- `src/App.css`

These files matter because they control:

- how capabilities are explained to the user
- how assistant presence reads on the desktop
- how continuity is made legible without dumping raw logs
- how startup opens into a lived-in state instead of an empty room
- how the Task Board behaves as a true operating-memory surface instead of a read-only wake-state card
- whether standing-approved task classes are legible and easy to revoke
- whether task metadata is legible enough for future scheduling and execution work
- whether working memory can be externalized into notes instead of being trapped in the conversation context
- how cheap or polished the UI feels
- how the ambient conversation room behaves and what remains durable underneath
- whether assistant presence reads consistently across setup, desktop, icons, and the conversation window
- whether the shell reads as one coherent desktop rather than unrelated windows
- whether utility windows still feel like internal tools or like first-class shell surfaces
- whether the small interaction scaffolding still feels prototype-like even after the main rooms look polished
- whether onboarding and settings express the same product truth about resident behavior and granted access

## Important Watchouts

- Avoid raw internal language in the UI when a product-facing label exists.
- Keep transcript storage durable even if the visible conversation becomes ephemeral.
- The activity/security surfaces can be repurposed, but they should still reflect authoritative backend state.
- The conversation window can feel softer and more ambient, but continuity facts and approvals still need a durable way back to the operator.
- In this reference shell, conversation is intentionally not a full IM transcript. Do not regress the room into a sidebar-plus-feed layout when adding features.
- Setup and Settings must stay aligned with backend defaults. If cloud models default to calmer behavior in Go, the frontend copy and toggles should say the same thing.
