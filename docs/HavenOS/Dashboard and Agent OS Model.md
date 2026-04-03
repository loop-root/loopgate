**Last updated:** 2026-03-24

# Haven OS — Dashboard and Agent OS Model

## Purpose

This document reframes Haven around what the product actually needs to be.

Haven should not primarily be a fake miniature desktop that an AI "uses."

Haven should be:

- an operating substrate for Morph
- a dashboard-first supervision and collaboration surface for the user
- a place where Morph externalizes memory, plans, tasks, notes, tools, and work

This document captures the product pivot from:

- "desktop environment for an AI"

to:

- "agent operating system with a human-facing mission control"

---

## Why This Pivot Is Necessary

The desktop metaphor helped establish:

- presence
- atmosphere
- personality
- emotional coherence

That work was valuable.

But a literal desktop is not the core thing an AI model needs.

AI models do not need:

- windows for their own sake
- icons for their own sake
- a fake GUI loop as the main working modality

They need:

- tasks
- notes
- plans
- files
- durable memory
- tools
- schedules
- configuration
- resource awareness
- safe access to the outside world

In other words:

- they need the function of an operating system

The dashboard should therefore become primary.
The desktop should become secondary.

---

## Core Product Statement

Haven is Morph's operating environment.

The user should not feel like they are opening:

- a chatbot
- a developer console
- a toy desktop

They should feel like they are opening:

- the command center for a local AI agent that lives, works, remembers, plans, and acts on their behalf

This is the closest expression of the original intent:

- more secure than OpenClaw
- easier to use than OpenClaw
- more personable than OpenClaw
- more visibly governed than OpenClaw
- more "macOS-like" in polish, defaults, and human clarity

---

## New Primary Shell

The primary Haven experience should be a mission-control dashboard.

This is the main surface the user opens and returns to.

### The dashboard should show

- what Morph is doing now
- current objective
- active plan
- open goals
- task board
- pending approvals
- recent activity
- working notes
- remembered facts
- available tools/apps
- resource usage
- current blockers
- a conversation input surface

This should feel closer to:

- Mission Control
- Activity Monitor
- a project operations console
- a living assistant dashboard

and less like:

- Finder with a chatbot layered on top

---

## Desktop Shell Becomes Secondary

The desktop is still valuable.

But it should no longer be the primary product.

It becomes:

- an optional "Morph's room" mode
- an ambient and emotional shell
- a secondary spatial surface

Good uses for the desktop shell:

- sticky notes
- artifacts laid out spatially
- journal traces
- ambient presence
- a more whimsical or intimate mode

But the desktop should not be the main place the user must go to understand or supervise Morph.

---

## What the OS Is For

The purpose of Haven is to let Morph stop holding everything in the context window.

That means Morph must be able to externalize work into:

- files
- notes
- tasks
- plans
- schedules
- artifacts
- config
- code
- memory records

This is the real operating-system idea:

- not visual chrome
- not fake windows
- but durable externalized cognitive structure

The system should work more like how humans work:

- write things down
- create todo items
- keep project notes
- save drafts
- schedule reminders
- split work into subtasks
- revisit work later

---

## Dashboard Modules

The primary dashboard should be made of modules, not just windows.

### 1. Current Work

Shows:

- current objective
- current step
- current tool/app in use
- whether Morph is thinking, waiting, acting, or blocked

### 2. Work Trace

This is not raw chain of thought.

It should show:

- recent tool actions
- decisions made
- why an approval is needed
- what changed
- important observations

This gives transparency without exposing unsafe or noisy raw reasoning.

### 3. Conversation

The user should still be able to talk to Morph easily.

But in the dashboard shell, this should be:

- a calm input surface
- optionally voice later
- tied to current work, not isolated as a pure IM feed

### 4. Tasks and Goals

Shows:

- open tasks
- scheduled tasks
- recurring tasks
- goals
- due work
- waiting items

### 5. Working Notes

Shows:

- Morph's scratchpads
- project notes
- temporary reasoning notes
- reminders to self

This is one of the most important OS layers.

### 6. Memory

Shows:

- remembered user facts
- preferences
- routines
- project continuity

This should be inspectable and editable in a user-legible way.

### 7. Approvals

Shows:

- pending approvals
- recent approvals
- standing grants
- what actions are always allowed

### 8. Tools / Apps

Shows:

- available native tools
- external connectors
- which tools are local vs boundary-crossing

### 9. Resource Monitor

Shows:

- active model/provider
- token usage
- background activity
- worker/morphling count later
- maybe local CPU/memory for heavier local workflows

---

## Personality Direction

Morph should feel:

- quirky
- useful
- personable
- present
- not overbearing

The right reference here is not a robotic enterprise assistant.

It is closer to:

- Weebo from *Flubber*

But with less chaos and more grounded usefulness.

### Target tone

- warm
- quick
- observant
- encouraging without being syrupy
- slightly playful
- never smug
- never passive-aggressive
- never bureaucratic

### Product behavior implications

Morph should:

- offer to help
- notice useful things
- leave calm, relevant notes
- speak first sometimes
- feel alive

Morph should not:

- spam
- overexplain every action
- ask for permission for trivial in-world work
- feel like a rule engine wearing a smile
- become theatrical or fake

---

## Loopgate's Role

Loopgate remains essential.

But Loopgate should feel like:

- guardrails
- control plane
- failsafe
- anti-drift layer
- prompt-injection resistance
- secret storage
- permission boundary

It should not feel like:

- a bouncer blocking every useful action

The product rule should be:

- Haven-native work inside Morph's own world should be easy
- real host actions should be permissioned and governed
- external and untrusted systems should be more tightly controlled

Loopgate exists to prevent:

- unsafe drift
- prompt injection wins
- secret leaks
- unreviewable host changes
- accidental authority expansion

Loopgate should not be the reason the product feels incapable.

---

## Tool Model

Haven-native tools should move toward an MCP-like shape.

That means:

- explicit tool names
- schema-based inputs
- structured outputs
- typed resources
- Loopgate-governed execution

The product does not need to adopt MCP branding internally first.

But the tools should behave like good MCP tools.

### Desired characteristics

- discoverable
- typed
- composable
- auditable
- local-first
- permission-aware

### Examples

- `task.create`
- `task.update`
- `task.schedule`
- `task.complete`
- `note.write`
- `note.read`
- `plan.create`
- `plan.preview`
- `plan.apply`
- `workspace.read`
- `workspace.patch`
- `resource.inspect`
- `approval.request`
- `memory.remember`

The current ad hoc mix of sandbox tools, mirrored folders, and UI-specific helper paths should gradually converge toward that shape.

---

## Relationship to OpenClaw

OpenClaw is useful as a reference point, but not as the product we are trying to copy.

Based on the current public repo and README, OpenClaw frames itself as a personal assistant running across many channels, with a gateway as control plane and the assistant itself as the product. It also has a very large multi-package structure with apps, extensions, packages, skills, source, and UI layers in one repo. Source: [openclaw/openclaw](https://github.com/openclaw/openclaw).

That suggests a few useful lessons:

- keep the assistant as the product, not the control plane
- treat tools/skills as first-class modular surfaces
- support channels and shells separately from the core agent runtime
- invest in onboarding because it reduces activation friction

What Haven should not copy:

- sprawling generality too early
- channel sprawl as the core identity
- a product shape that feels more like infrastructure assembly than a crafted user experience

What Haven *can* borrow:

- plugin/tool modularity ideas
- MCP-like tool surface discipline
- onboarding clarity
- separation between gateway/control plane and user-facing assistant

The key difference should stay:

- OpenClaw as the Linux-like, broad, hackable, agentic layer
- Haven as the macOS-like, opinionated, safer, calmer, more human-facing layer

---

## What Makes the MVP Demonstrable

The MVP does not need to be everything.

It needs to tell a clear story quickly.

A strong demo should show:

1. first-run onboarding is simple
2. Morph feels present immediately
3. the dashboard clearly shows what Morph is doing
4. Morph remembers the user
5. Morph can create tasks, notes, and plans
6. Morph can help with real user files through Loopgate-governed permissions
7. approvals are legible and not annoying
8. the product feels calmer, safer, and easier than OpenClaw

If that loop works, the pitch works.

---

## Updated Product Hierarchy

### Primary

- dashboard shell
- task substrate
- notes / working memory
- plans
- approvals
- real host-help workflows

### Secondary

- desktop shell
- ambient room feel
- journal traces
- paint
- spatial artifacts

### Later

- browser
- morphlings
- sidebar shell
- more connectors
- richer installable tool ecosystem

---

## Design Consequence

This means Haven is not primarily:

- a desktop simulator

It is:

- an agent operating system with a dashboard-first UX

That is the version of the idea most likely to feel:

- useful
- coherent
- fundable
- demonstrable

---

## Next Steps

### Step 1

Adopt this dashboard-first model in the roadmap and product docs.

### Step 2

Define the dashboard information architecture and module layout.

### Step 3

Align Haven-native tools to a typed MCP-like model under Loopgate.

### Step 4

Build the host access / plan / apply layer so Morph can actually help on the user's machine.

### Step 5

Make the desktop shell optional and secondary instead of primary.

---

## Decision Summary

Haven should become:

- Morph's operating substrate
- a mission-control dashboard first
- a desktop/room second
- a place where Morph externalizes cognition into durable structures
- a safer and easier-to-use agent system with real power under Loopgate governance

That is the clearest version of the product so far.
## Current Role

This document remains directionally correct, but it is no longer the immediate build target by itself.

The dashboard-first model should support the first real workflow:

- Morph inspects Downloads
- Morph builds a plan
- the user reviews and approves it
- Loopgate applies it

So this document should be read as the future shell model around the narrower Downloads-organizer MVP; the point-in-time execution brief for that slice lives in **`~/Dev/projectDocs/morph/haven-archives/`** for maintainers.
