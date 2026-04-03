**Last updated:** 2026-03-24

# Haven OS — Host Access and Action Model

## Purpose

This document defines the next major product and architecture slice for Haven:

- how Morph should interact with the user's real Mac
- how that interaction stays useful instead of toy-like
- how Loopgate keeps authority, auditability, and approval intact

This is the design pass for moving Haven from:

- "Morph can work in its own sandbox and on mirrored copies"

to:

- "Morph can help on the user's actual machine with explicit permission"

This is a product-purpose document as much as a technical design.

---

## The Product Problem

Right now Haven has atmosphere, presence, continuity, and a credible desktop shell.

What it still lacks is purpose.

Morph feels like it lives somewhere, but not yet like it can truly help.

The biggest reason is the current host-file model:

- `~/Shared with Morph` works as an intake tray
- optional mirrored folders make Haven aware of `Downloads`, `Desktop`, and `Documents`
- but Morph still mostly works on mirrored copies inside Haven rather than the user's real folders

That is good for safety, but too weak as the primary model.

It makes the system feel like:

- a contained demo
- a stylized sandbox
- a resident that can observe, but not really act

For Haven to feel like the secure, opinionated, more personal "macOS version" of an agentic system, Morph needs to be able to do real work on the user's machine with clear permission boundaries.

---

## Product Goal

Haven should feel like Morph's computer.

But Morph should also be able to help on the user's real Mac when the user allows it.

The correct model is:

1. Morph has a rich native world of its own inside Haven
2. Morph can see selected parts of the user's Mac through explicit grants
3. Morph can plan and then perform real host actions through Loopgate
4. The user can understand, approve, and revoke that power easily

This should make Haven feel:

- more useful
- more trustworthy
- more legible
- less like a clever UI around an isolated toy environment

---

## Design Principles

### 1. Loopgate remains the authority boundary

Natural language never creates authority.

Morph does not get host power because it "sounds trustworthy."

Loopgate remains responsible for:

- validating grants
- resolving host paths
- normalizing operations
- enforcing approvals
- issuing standing grants
- auditing execution

### 2. Mirrors are not the main model

Mirrors still have value, but only for:

- the shared intake tray
- safe import / handoff workflows
- boundary-reduction in especially sensitive contexts

Mirrors should not remain the main way Morph helps with the user's files.

### 3. Read, plan, and apply are separate phases

Host access should not be one undifferentiated permission.

The basic operating pattern should be:

1. inspect
2. prepare a plan
3. review / approve
4. apply

This keeps Morph useful without turning it into an ambient host-power process.

### 4. Standing grants must be narrow and visible

If a host action becomes low-friction, it should be because the user granted a narrow recurring authority class, not because Haven silently widened trust.

Standing grants must be:

- scoped
- human-readable
- revocable
- auditable

### 5. The user remains in control, but can choose stronger modes

Haven should be safe by default, not weak by design.

That means:

- the default profile should stay conservative
- the user should be able to widen Morph's authority deliberately
- Loopgate should keep that widening explicit, reviewable, and revocable

Some users will want:

- inspect-only help
- plan-and-approve help
- narrow recurring standing grants

Other users will want something much stronger.

That should be allowed.

If a user wants Morph to have broad or even near-total access to the system within the privileges the app can obtain, the product should support that as an advanced mode with clear warnings and strong visibility.

The system should not artificially cripple the agent just to preserve a tidy default.

### 5. The UX should feel like macOS, not DevOps

The user should not need to understand tokens, manifests, or raw capability IDs.

The product language should be:

- "Morph can view Downloads"
- "Morph can prepare cleanup plans for Downloads"
- "Morph can apply approved organization changes in Downloads"

not:

- `host_fs_write`
- `capability token`
- `approval manifest SHA256`

Those internals remain Loopgate internals.

---

## The Four Access Modes

Haven should operate with four clear access modes.

### Mode A — Haven-native local

This is Morph's own world.

Examples:

- notes
- journal
- paint
- task board
- internal workspace files
- desktop notes and internal layout

Properties:

- low-friction by default
- no per-action user approval
- still mediated and audited by Loopgate

### Mode B — Shared intake mirror

This is the intentional bridge into Haven.

Examples:

- `~/Shared with Morph`
- explicit file or folder import

Properties:

- copy-based
- safe default
- good for deliberate handoff
- not the primary long-term host-help model

### Mode C — Granted live host folders

This is the missing MVP layer.

Examples:

- `~/Downloads`
- `~/Desktop`
- `~/Documents`
- later: user-selected custom folders

Properties:

- real host paths
- explicit folder grants
- read/plan/apply scope separation
- approval or standing grant required for mutation

### Mode C2 — Advanced host authority

This is the optional power-user mode.

Examples:

- broad access to the user's home directory
- access to arbitrary user-selected folders
- later: user-approved whole-system working profiles within the app's real OS-level reach

Properties:

- explicitly enabled
- strongly warned
- highly visible in Security
- still mediated by Loopgate
- still audited
- still subject to macOS and app-process permission limits

This mode should never be the default, but it should exist.

### Mode D — External / untrusted systems

Examples:

- web access
- cloud connectors
- third-party tools
- future MCP servers

Properties:

- more heavily governed
- explicit trust boundary
- clearly separate from local file help

---

## Proposed Grant Model

Each host folder should be represented in Loopgate as a first-class granted resource.

Illustrative model:

```text
GrantedFolder
  - id
  - display_name
  - resolved_host_path
  - scopes
  - created_at
  - last_used_at
  - status
```

Where `scopes` are not generic booleans, but explicit phases:

- `read`
- `plan`
- `apply`

Meaning:

- `read`: Morph may inspect and summarize the real folder
- `plan`: Morph may prepare a concrete change plan for that folder
- `apply`: Loopgate may execute approved operations in that folder

The user-facing setup flow can still simplify this into:

- View only
- Help organize with approval
- Allow recurring organization in this folder

And later, for advanced users:

- Advanced access
- Broad access with approval
- Broad recurring authority

But the internal model should stay phase-based.

---

## Proposed Host Capability Shape

Do not expose host power as a generic writable filesystem surface.

Prefer typed host capabilities.

### Read / inspect capabilities

- `host.folder.list`
- `host.file.read`
- `host.folder.summarize`

These should be scoped to granted folders only.

### Planning capabilities

- `host.organize.plan`
- `host.rename.plan`
- `host.archive.plan`

These should return structured plans, not execute directly.

### Apply capabilities

- `host.plan.apply`
- `host.plan.cancel`
- `host.plan.preview`

The key move is: apply should run against a Loopgate-created plan object, not raw natural-language instructions from the model.

For advanced authority profiles, additional capabilities may exist, but they should still be typed and mediated rather than collapsing into "the model can do anything because it asked."

---

## Plan / Apply Workflow

This is the core safety and UX pattern.

### Phase 1 — Inspect

Morph reads the granted folder through Loopgate.

Example:

- list Downloads
- identify file types, duplicates, old items, obvious clutter

### Phase 2 — Build a plan

Morph proposes a structured plan.

Example:

- create `Downloads/Archives/`
- move older `.zip` files there
- group screenshots into `Downloads/Screenshots/`
- rename two generic files for clarity

This is not yet execution.

Loopgate should persist the plan as a first-class object.

### Phase 3 — Review

The user sees:

- what will change
- which folder it affects
- how many operations are involved
- whether this is one-time or recurring

The user can:

- approve all
- approve selected items later
- reject
- convert into a standing recurring grant if the class is narrow and safe

### Phase 4 — Apply

Loopgate executes the plan against real host paths.

Important:

- path resolution must happen at execution time
- final targets must still be validated against the granted folder root
- symlink and traversal protections still apply
- audit entries must preserve what was planned and what actually executed

---

## Supported MVP Operations

The first host-action MVP should be conservative.

Include:

- move file
- rename file
- create folder
- group files into subfolders
- archive into a user-visible archive folder

Avoid for the first slice:

- destructive delete
- recursive broad cleanup
- arbitrary shell execution against host paths
- changing file permissions
- opaque mass operations

If "delete" is needed later, it should probably be:

- move to Trash

not:

- permanent delete

---

## Standing Grants

Standing grants should exist, but only for narrow recurring families.

Good examples:

- organize Downloads inside the granted Downloads folder
- rename screenshots in Desktop
- archive older files into a specific subfolder

Bad examples:

- full write access to Documents
- arbitrary shell execution in a granted folder
- blanket permission to mutate any granted folder in any way

Important nuance:

Those are bad defaults, not forbidden advanced modes.

If the user explicitly wants broad power, the system should support it through a clearly marked advanced profile rather than pretending it is impossible.

Standing grants should bind to:

- a task family
- a folder resource
- an operation class

Illustrative shape:

```text
StandingGrant
  - id
  - granted_folder_id
  - action_class
  - scope
  - created_at
  - last_used_at
  - actor
  - status
```

The Security room should render these as:

- "Always allowed in Downloads"
- "Ask first in Documents"
- "No live access to Desktop"

And for stronger modes:

- "Broad access with approval"
- "Broad recurring authority"
- "Full Haven host authority"

Those labels should feel serious, not casual.

The point is not to encourage them.
The point is to make them possible and legible.

---

## Access Profiles

The user should not be forced to build authority one checkbox at a time unless they want to.

Haven should expose a few clear profiles.

### Profile 1 — Safe Default

- Haven-native local: low-friction
- shared intake tray
- selected live folders in read or plan mode
- host mutation requires approval

This should be the default onboarding path.

### Profile 2 — Trusted Helper

- selected folders may be inspected, planned, and applied with approval
- narrow recurring standing grants allowed
- still folder-scoped

This is the likely main power-user mode for the MVP.

### Profile 3 — Advanced Operator

- user may grant broad home-directory or custom-folder authority
- host mutation still mediated by Loopgate
- approvals can be broad or recurring if the user insists
- Security must make this unmistakably visible

### Profile 4 — Full Host Authority

This is the explicit "I want Morph to really act like my computer operator" mode.

Properties:

- highly warned
- hidden behind advanced settings
- never recommended
- still audited through Loopgate
- still bounded by the actual OS-level permissions the app has

This should not bypass Loopgate.
It should simply give Loopgate a much broader envelope to govern.

---

## Wizard and Settings UX

The onboarding and settings model should change from "mirror some folders" to "choose how Morph may help."

### Setup should ask

- Which folders may Morph see?
- Should Morph only inspect them, or also prepare organization plans?
- Should Morph ever be allowed to apply approved changes there?

And for advanced users:

- Do you want the safe default, trusted helper, or advanced operator model?

Suggested defaults:

- `Downloads`: help organize with approval
- `Desktop`: inspect only
- `Documents`: inspect only
- custom folders: inspect only until explicitly changed
- advanced profiles should be tucked behind an "Advanced Access" affordance with clear warnings

### Settings should allow

- changing scopes later
- revoking folder access
- converting a folder from mirror-only to live host help
- reviewing standing grants

The UI should make it obvious that:

- `Shared with Morph` is a drop zone
- folder grants are real host-access permissions

Those two ideas should not blur together.

---

## Example MVP Flow

### Downloads cleanup

1. User grants Downloads during setup
2. Morph notices clutter in the real Downloads folder
3. Morph leaves a calm note:
   - "Downloads looks crowded. Want me to prepare a cleanup plan?"
4. User clicks yes
5. Morph inspects real Downloads through Loopgate
6. Morph creates a structured plan
7. Haven opens a review surface
8. User approves
9. Loopgate applies the operations on the real folder
10. Morph leaves a handoff note summarizing what changed

This is a much stronger product story than:

- "I sorted a mirrored copy in Haven"

---

## Why This Matters for Product Purpose

Right now Haven has identity and atmosphere, but not enough consequence.

This host-access model is what turns Haven from:

- "a beautiful space where an AI lives"

into:

- "a secure, legible, personable agent workstation that can actually help me"

That is the closer analogue to the intended "macOS version" of an agentic system:

- more opinionated
- more visible
- more secure
- more humane
- still capable

If OpenClaw feels like:

- broad Linux-like agent power

then Haven should feel like:

- a premium local assistant workstation with stronger defaults and better boundaries

And importantly:

- strong defaults should not become weak ceilings

If the user wants Morph to have more power, the system should let them choose that deliberately.

---

## Out of Scope for This Slice

Not part of the first host-action MVP:

- MCP trust model
- signed tool manifests
- full Browser implementation
- remote multi-user transport
- morphling host action autonomy
- broad unattended automation over host folders

Those can build on top of this model later.

---

## Next Implementation Sequence

### Step 1

Define Loopgate's granted-folder resource model and scope system.

### Step 2

Add host read/list capabilities for granted folders.

### Step 3

Add structured plan objects for host organization actions.

### Step 4

Build the approval/review UI for host file plans.

### Step 5

Add controlled apply execution against real host paths.

### Step 6

Add narrow recurring standing grants for safe host task families.

---

## Decision Summary

The current mirror-first model is a good safety bridge, but the wrong primary product model.

The next MVP leap should be:

- permissioned real host access
- plan-before-apply execution
- narrow standing grants
- Loopgate-owned folder resources

That is the path to making Haven feel genuinely useful without abandoning the security story.
