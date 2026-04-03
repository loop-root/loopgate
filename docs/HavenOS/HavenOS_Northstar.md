**Last updated:** 2026-03-24

# Haven OS — North Star

## What We Are Building

Haven OS is a safer, calmer, more legible alternative to OpenClaw.

The short version:

- OpenClaw is the Linux-shaped version of agentic AI.
- Haven should be the macOS-shaped version.
- Loopgate stays as the control plane and safety layer.
- Morph is the assistant the user actually experiences.

The product is not "an AI desktop."

The product is:

**a supervised personal AI that can do useful work on a user's machine with clear permissions, visible plans, and clean review/apply moments.**

## The One Job

The first product loop is:

**Morph reads, plans, and organizes the user's Downloads folder with permission.**

That one loop is enough to prove:

- Morph is useful
- Morph has real access to user files
- Loopgate is real but not oppressive
- plans and approvals are understandable
- Haven provides persistent working memory, tasks, notes, and visibility

If this loop does not feel magical, the rest of the product will not matter.

## Product Promise

Haven should feel like:

- Steve Jobs made OpenClaw
- calm, opinionated, and simple
- secure by default, but not neutered
- personable, but not cloying
- powerful, but not chaotic

Morph should feel:

- quirky and warm, like Weebo
- useful before cute
- proactive without being invasive
- capable of doing nearly anything the user would normally do, once permission is granted

Loopgate should feel like:

- guardrails
- a failsafe against drift, prompt injection, secret leakage, and bad boundaries
- not a bouncer for every harmless action inside Morph's own world

## Core Mental Model

The user should understand four things:

### 1. Morph

Morph is the assistant.

### 2. Haven

Haven is Morph's operating space: tasks, notes, memory, plans, tools, approvals, and work trace.

### 3. Loopgate

Loopgate is the governing layer that decides what real-world actions are allowed.

### 4. Permissions

The user can give Morph more power deliberately, revoke it, and review what happened.

## Product Shape

The primary shell should be a Desktop.

**Workspace-first UI:** The default Haven shell is a **workstation layout** (center workspace + Morph assistant rail). The classic icon-grid + floating windows layout remains available (**View → Classic desktop layout**). Roadmap: `docs/HavenOS/plans/2026-03-22-workspace-first-haven-ui.md`.

The desktop exists to show:

- what Morph is doing now
- what it plans to do next
- what files it is working with
- what approvals are pending
- what tasks and goals are active
- what notes and working memory it has externalized
- what changed

The dashboard can remain as a secondary emotional shell, but it is not the primary MVP.

## MVP Definition

The MVP is successful when a user can:

1. install Haven on macOS
2. grant Morph access to Downloads
3. ask Morph to help clean it up
4. see Morph inspect the real folder
5. review a proposed organization plan
6. approve the plan
7. watch Loopgate apply the real file operations
8. reopen later and see that Morph remembers the task, notes, and state

## What We Are Not Doing First

Not first:

- a broad "general AI OS"
- browser-first research workflows
- morphlings as a core MVP dependency
- art / garden / decorative autonomy as a headline feature
- dozens of apps
- generic shell power as the center of the product

Those may come later, but they do not define the first useful product.

## Design Standard

Every feature should answer one question:

**Does this make Morph better at safely helping with real user work, starting with Downloads?**

If not, it is probably roadmap, not MVP.

## Reference Positioning

OpenClaw remains a useful reference for:

- agent capability
- local-first ambition
- modular tool ideas

Haven should differentiate on:

- calmer UX
- clearer permissions
- more legible plans and approvals
- stronger product coherence
- more personable assistant behavior
- safer defaults without making the assistant weak

## Current Focus

Point-in-time Downloads MVP execution brief and implementation roadmap narratives are archived for maintainers under **`~/Dev/projectDocs/morph/haven-archives/`** (not shipped in a Morph clone). This repo keeps security, capability, and MVP spec docs in `docs/HavenOS/`.
