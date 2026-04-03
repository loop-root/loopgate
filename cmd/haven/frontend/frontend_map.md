# Haven Frontend Map

This file maps the main frontend surfaces under `cmd/haven/frontend/`.

Use it when changing:

- desktop shell design
- Security window labels
- Morph conversation surface
- presence rendering
- continuity status surfaces
- app/window composition
- onboarding flow
- settings behavior toggles

## Core Role

This directory contains the React frontend embedded by Wails.

It is responsible for:

- rendering Haven's desktop shell
- subscribing to Wails events from the Go backend
- showing windows, notes, activity, and presence
- translating backend state into product language

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
  - desktop icons, sticky notes, and Morph's on-screen presence
  - current home of Morph's spatial orb/glyph rendering on the desktop
- `src/components/MenuBar.tsx`
  - top bar and current status indicators
  - current home of the Haven wordmark and calmer menu/status treatment
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
  - in-Haven Morph room
  - ambient conversation surface with visible recency-faded turns
  - quiet Recall drawer for durable thread access
  - current place where the product distinguishes “conversation in Haven” from future full Messenger history outside Haven
- `src/components/windows/LoopgateWindow.tsx`
  - Security window
  - capability label mapping and built-in vs external grouping
  - now surfaces standing-approved Haven task classes and lets the user revoke or restore them
- `src/components/windows/ActivityWindow.tsx`
  - activity monitor surface for durable evidence outside the ambient in-room conversation
  - now intentionally styled as a calmer monitoring room rather than a raw event list
- `src/components/windows/TodoWindow.tsx`
  - operational continuity surface for open items and active goals
  - now behaves as a Task Board rather than a plain carry-over list
  - renders task kind, source, next-step, and scheduled-time metadata from Loopgate wake-state
  - now also shows whether a task is always allowed inside Haven or still asks first
  - supports add and complete actions against Loopgate-backed continuity
  - can now create future-scheduled tasks, while the resident loop only treats them as actionable once due
- `src/components/windows/NotesWindow.tsx`
  - private working-notes room for plans, scratchpads, and notebook-style externalized working memory
  - lists notes under `scratch/notes` and edits them through Loopgate-mediated `notes.*` tools
- `src/components/windows/JournalWindow.tsx`
  - journal reader UI
  - now shares the same hero/material language as the rest of Haven
- `src/components/windows/PaintWindow.tsx`
  - paint canvas and gallery
  - now shares the same hero/material language as the rest of Haven
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
  - frontend-only types, icon metadata, wallpaper helpers, shared constants, the reusable Morph glyph, and the Haven wordmark

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
- how Morph feels present on the desktop
- how continuity is made legible without dumping raw logs
- how startup opens into a lived-in state instead of an empty room
- how the Task Board behaves as a true operating-memory surface instead of a read-only wake-state card
- whether standing-approved Haven task classes are legible and easy to revoke
- whether task metadata is legible enough for future scheduling and execution work
- whether working memory can be externalized into notes instead of being trapped in the conversation context
- how cheap or polished the UI feels
- how the ambient conversation room behaves and what remains durable underneath
- whether Morph feels like the same entity across setup, desktop presence, icons, and the in-room conversation surface
- whether Haven itself feels like a coherent branded OS rather than a set of loosely related windows
- whether the remaining utility apps still feel like old internal tools or like first-class Haven rooms
- whether the small interaction scaffolding still feels prototype-like even after the main rooms look polished
- whether onboarding and settings express the same product truth about resident behavior and granted access

## Important Watchouts

- Avoid raw internal language in the UI when a product-facing label exists.
- Keep transcript storage durable even if the visible conversation becomes ephemeral.
- The activity/security surfaces can be repurposed, but they should still reflect authoritative backend state.
- The Morph window can feel softer and more ambient, but continuity facts and approvals still need a durable way back to the user.
- Inside Haven, conversation is intentionally not a full IM transcript anymore. Do not accidentally regress the room back into a sidebar-plus-feed layout when adding features.
- Setup and Settings must stay aligned with backend defaults. If cloud models default to calmer behavior in Go, the frontend copy and toggles should say the same thing.
