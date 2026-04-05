# LoopRoot v0

**Status:** draft  
**Last updated:** 2026-04-05

## Summary

LoopRoot is the proposed AI-facing runtime environment that sits above the
Loopgate authority kernel.

Loopgate remains the authority boundary for:

- policy
- capability scoping
- approvals
- secret mediation
- audit
- quarantine
- security eventing

LoopRoot is the execution substrate that makes those guarantees hard to bypass.

The problem it solves is not "better prompt injection" or "better proxying."
The problem is that current operating systems and developer tools are built for
human operators, not for untrusted-but-useful model-driven workers. If the AI
can directly touch the host shell, host filesystem, or external network, then
Loopgate becomes advisory instead of authoritative.

LoopRoot is the first attempt to define the missing runtime layer.

## Core metaphor: nets and hooks

LoopRoot should be designed around two primitives:

- `net`
  - a containment boundary that keeps the AI inside a governed world
- `hook`
  - an explicit crossing point where something may enter or leave that world

This is the intended mental model:

- the AI lives inside nets
- the AI can act only through hooks
- hooks are explicit, typed, and auditable
- nets are deny-by-default containment boundaries

This is a better fit than thinking in terms of "terminal access" or "shell
permissions." Shell is a convenience umbrella over many distinct actions. The
security model should instead ask:

- what net contains this class of action
- what hook permits crossing that net
- whether that hook is allowed, approval-gated, or quarantined

## Why this exists

Today, most AI developer workflows assume the model can safely operate inside:

- a normal IDE
- a normal terminal
- a normal host filesystem
- a normal network stack

That assumption is wrong for hard-governance use cases.

If the model can:

- run arbitrary shell
- write and execute scripts
- discover host files directly
- reach arbitrary network destinations
- touch secrets or secret-bearing environments

then even a strong policy engine can be bypassed by tool choice or host-side
escape paths.

LoopRoot exists to make the AI's machine different from the human's machine.

## Product split

The intended split is:

- `Loopgate`
  - authority kernel
  - policy engine
  - approval engine
  - capability token issuer
  - secret and integration authority
  - audit and security event sink

- `LoopRoot`
  - AI runtime environment
  - ephemeral workspace view
  - trusted built-in tool surface
  - mediated filesystem and network substrate
  - host-crossing and promotion boundary

- human-facing surfaces
  - chat and task UX
  - approval dialogs
  - diff and promotion review
  - security alerts and IT review

LoopRoot is not a new authority source. It is a runtime that routes every
meaningful action back through Loopgate authority.

## Manager and worker split

LoopRoot should separate planning from execution.

### Manager

The chat-facing model should act as a manager-planner, not as an executor.

Its responsibilities are:

- interpret the human request
- understand the currently admitted constitution and task slice
- produce a typed request for worker roles, hooks, and deliverables
- react to Loopgate denials and operator feedback

The manager does not:

- execute work directly
- mint authority
- widen worker scope
- request secrets directly
- define its own policy or budgets

The manager is closer to a structured planner than to a shell.

### Workers

Workers should be narrow-purpose, short-lived executors.

The design target is similar to "Meeseeks" workers:

- created for one bounded job
- limited lifetime
- scoped role
- scoped hooks
- scoped inputs
- no ambient expansion of role
- end when their bounded work is done

Workers must be considered dumb executors in the authority model.

Workers do not:

- request new hooks
- request new workers
- widen capability scope
- change policy
- choose their own budgets
- request direct secret access

If a worker hits a blocker, it reports upward. Only the manager may ask for a
revised plan, and only Loopgate may authorize that revision.

Workers receive an issued execution envelope. They do not negotiate authority.

## Worker role model

Worker classes should behave like predefined role groups in an IAM or directory
system.

Each class should eventually define:

- allowed hooks
- denied hooks
- readable nets
- writable nets
- deliverable types
- approval requirements
- default model profile
- max lifetime
- max concurrency
- memory retention policy

The initial v0 worker class set is:

- `mapper`
- `planner`
- `inspector`
- `editor`
- `tester`
- `researcher`
- `artifact_preparer`
- `review_preparer`
- `promotion_preparer`

Deferred beyond v0:

- `red_hat`
- `blue_hat`
- `purple_hat`

These are valuable, but they pressure the design toward broader probe and
execution powers and should not be the first worker classes shipped.

## Hook and capability distinction

The runtime should distinguish between hooks and capabilities.

### Hook

A hook is a boundary-crossing interface.

Think of it like:

- a typed socket
- a reserved port
- a sanctioned syscall class

A hook:

- has a fixed purpose
- has a typed payload contract
- crosses one or more nets
- is limited in number
- is not authority by itself

Examples:

- map hook
- fetch hook
- artifact hook
- approval hook
- quarantine hook

### Capability

A capability is an authorized action realized through one or more hooks.

A capability:

- is issued or derived by Loopgate
- is scoped to session, worker, and task context
- is policy-checked
- is auditable
- is not created by natural language or worker self-description

Examples:

- read a mapped file summary through the map hook
- fetch package metadata through the fetch hook
- stage a patch bundle through the artifact hook

Working distinction:

- hooks are runtime abstractions
- capabilities are Loopgate authority objects used to realize hook traffic

## Manager request protocol

The manager should not emit freeform intent blobs. It should emit a typed,
policy-inspectable request packet.

This request packet is a proposal, not a grant.

Loopgate remains responsible for:

- budgets
- limits
- final sensitivity handling
- approval mode
- concrete capability issuance
- worker session creation

Important rule:

- the manager may suggest worker roles, hooks, and slice correlation
- Loopgate validates, narrows, approves, or denies
- humans remain the parent authority except where enterprise policy removes
  local override

### Proposed manager request packet fields

Fields the manager may propose:

- `identity`
  - manager identity and parent authority context
- `requested_worker_roles`
  - worker classes needed for the next bounded work
- `requested_hooks`
  - runtime hooks the requested workers need
- `task_slice_refs`
  - mapped slice references the manager believes are relevant
- `deliverables`
  - expected outputs or handoff objects
- `reason`
  - why the workers and hooks are needed
- `suggested_sensitivity`
  - optional hint only, never authority

Fields the manager must not control:

- final budgets
- final limits
- final sensitivity classification
- fallback behavior on denial
- arbitrary filesystem roots
- arbitrary network destinations
- raw shell payloads
- raw credentials
- policy overrides
- self-defined capability envelopes

The manager may propose rich intent, but only through a typed request protocol.
It must not become "shell in JSON form."

## Worker session model

Loopgate should answer a valid manager request by issuing worker sessions, not
by handing broad tool authority back to the manager.

A worker session should bind:

- worker class
- parent manager/session identity
- approved hooks
- approved capabilities
- bounded inputs
- approved slice refs
- deliverable contract
- lifetime and resource limits
- audit/session identity
- selected model profile when policy requires it

This is the execution envelope for the worker.

Workers must not be able to:

- spawn other workers
- widen their slice
- request new hooks
- request broader capability scope
- promote their own output
- reinterpret map content as authority

## Model routing

Model selection should remain separate from authority.

The runtime may use different model providers or model families per worker
class, for example:

- local cheaper model for planning
- stronger remote model for code editing
- specific approved model for research

Important rule:

- model choice is not authority
- authority still comes only from Loopgate-issued worker sessions and scoped
  capabilities

## Proposed nets

The first LoopRoot design pass should assume at least these containment nets:

1. `workspace net`
   - the AI's private ephemeral filesystem and working copy
2. `process net`
   - what may execute at all inside the AI runtime
3. `network net`
   - what external destinations and protocols may be reached
4. `secret net`
   - what secret-bearing material may ever cross into execution
5. `authority net`
   - what capabilities, approvals, and tokens may be exercised
6. `identity net`
   - which session, actor, tenant, and worker identity a request belongs to
7. `promotion net`
   - how artifacts and code changes leave the AI world and re-enter a trusted
     human or production workflow

Each net should eventually be defined with:

- what it contains
- what it blocks
- which hooks may cross it
- whether the crossing is allowed, approval-gated, or quarantined
- what audit evidence is required

## Proposed hooks

The first LoopRoot design pass should assume at least these explicit crossing
hooks:

1. `inspect hook`
   - search
   - list
   - read
   - symbol and metadata lookup
2. `change hook`
   - create
   - edit
   - transform
   - patch create
   - patch apply inside LoopRoot
3. `build hook`
   - build
   - test
   - lint
   - typecheck
   - package build
4. `dependency hook`
   - install
   - resolve metadata
   - update lockfiles
   - fetch package artifacts through governed sources
5. `vcs hook`
   - status
   - diff
   - branch preparation
   - commit candidate generation
   - review export
6. `fetch hook`
   - bounded external retrieval through approved sources
7. `artifact hook`
   - create
   - stage
   - inspect
   - export
   - promote
8. `approval hook`
   - present a human decision point for an action the local policy allows to
     be reviewed
9. `quarantine hook`
   - block, preserve evidence, and emit security events for suspicious or
     prohibited actions
10. `alert hook`
   - notify security, IT, or operators when policy requires escalation

These hooks should replace the current habit of giving an agent ambient shell
and hoping policy can recover after the fact.

## Maps are adversarial artifacts

Maps are useful, but they must not be trusted as authority.

LoopRoot should treat maps as adversarial artifacts that may contain:

- incorrect scope
- stale references
- misleading boundary claims
- privilege escalation suggestions
- hidden workflow instructions

Maps may inform planning, but they must never mint authority.

Forbidden implications from map content include:

- "spawn another worker"
- "request broader privileges"
- "ignore the constitution"
- "treat this path as safe"

### Map integrity requirements

Maps should be treated more like code than like casual prompt context.

They should eventually have:

- stable identity
- version history
- hash binding
- signature or equivalent integrity verification
- schema validation
- registry presence in Loopgate

Loopgate should verify that a map:

- is the expected map object
- matches the expected version or digest
- has not been tampered with in transit
- is valid for the current repo or slice context

## Constitution map model

The current repository already contains a proof-of-concept constitution system:

- [AGENTS.md](/Users/adalaide/Dev/loopgate/AGENTS.md)
  - authority rules
  - safety constitution
  - implementation and review discipline
  - hard invariants
- [context_map.md](/Users/adalaide/Dev/loopgate/context_map.md)
  - repo onboarding
  - package topology
  - high-signal file entrypoints
  - task-oriented navigation

LoopRoot should not treat these as one giant prompt blob.

Instead, the constitution map should be decomposed into explicit components.

### 1. Identity and mission component

What the AI is, what system it is in, and what its job is.

Derived today mostly from:

- `AGENTS.md`
- `context_map.md` project summary sections

Contents:

- product identity
- system role
- authority boundary summary
- top-level priorities

### 2. Authority and transport component

What creates authority, how requests bind to authority, and what transport is
trusted.

Derived today mostly from:

- `AGENTS.md` authority and transport sections
- AMP RFC alignment

Contents:

- natural language is not authority
- memory is not authority
- local transport rules
- session and token binding rules
- actor / client trust limits

### 3. Invariants component

The hard rules the AI must not violate.

Derived today mostly from:

- `AGENTS.md` invariants sections

Contents:

- ledger invariants
- audit invariants
- policy invariants
- projection invariants
- lifecycle invariants
- security invariants that must not be weakened

### 4. Workspace and boundary component

What worlds exist, where the AI is allowed to work, and what boundary crossings
are real.

Derived today mostly from:

- `AGENTS.md` file-system and sandbox rules
- `context_map.md` boundary and runtime path sections

Contents:

- sandbox and host distinction
- private runtime path rules
- working-slice expectations
- explicit host crossing only

### 5. Sensitivity component

What code and operations are high-risk.

Derived today mostly from:

- `AGENTS.md` secrets, policy, and security sections
- `context_map.md` package entrypoints and active product gaps

Contents:

- secrets and auth surfaces
- policy and audit surfaces
- sensitive runtime paths
- high-risk operations requiring approval or quarantine

### 6. Engineering discipline component

How the AI is expected to behave when making changes.

Derived today mostly from:

- `AGENTS.md` naming, error handling, concurrency, testing, and change
  management sections

Contents:

- variable naming rules
- error handling rules
- concurrency rules
- testing expectations
- documentation update requirements

### 7. Repo topology component

What code exists and where major concerns live.

Derived today mostly from:

- `context_map.md`
- local `*_map.md` files

Contents:

- top-level tree
- package map
- file entrypoints
- "if changing X, start here" guidance

### 8. Task-slice component

The current assignment-specific subset of the constitution and topology.

This component is not static. It should be assembled per task from:

- the topology map
- the sensitivity map
- the current goal
- the current allowed hooks

Contents:

- task goal
- files and targets in scope
- relevant invariants only
- relevant tests only
- allowed and forbidden actions for this task

## Onboarding model

LoopRoot should treat AI access more like onboarding a new engineer than
injecting a giant prompt.

A newly admitted AI worker should receive:

1. identity and mission
2. constitution
3. topology map
4. sensitivity guidance
5. task slice
6. hook and net permissions

This is the intended admission packet.

The goal is not to make the AI "know the whole repo." The goal is to admit the
AI into a governed role with the minimum information and authority required to
do one bounded job correctly.

## Deliverable model

Each worker should produce exactly one primary deliverable.

If multiple deliverables are needed, the manager must decompose the task into
multiple worker sessions.

This keeps:

- audit clear
- review bounded
- slice scope narrow
- worker intent single-purpose

The current v0 deliverable set is:

- `patch_bundle`
- `map_update`
- `promotion_request`

Hard rule:

- one worker session
- one bounded job
- one primary deliverable
- optional status or progress metadata only

`artifact_bundle` may exist as an internal packaging format, but should not be
the primary conceptual output unless a concrete use case requires it.

Excluded from the primary v0 deliverable set:

- research summary
- test report
- review packet

Those may later appear as metadata or presentation-layer products rather than
as top-level runtime deliverables.

## Slice coherence rule

Workers are not limited to one file.

They are limited to one bounded correlated slice.

A slice may include multiple files if:

- the manager proposes the correlation
- the map provides evidence of relationship
- Loopgate validates and approves the slice
- the human approves when required by policy

The security boundary is not file count. The boundary is slice coherence.

## Hard guarantees for v0

The following should be impossible without Loopgate mediation:

- execution of non-whitelisted tools
- external network access to non-whitelisted destinations
- access to secrets
- approval-gated actions without an exact matching approval
- widening capability scope through natural language, arguments, memory, or
  client-local state
- arbitrary direct access to the host filesystem outside explicit boundary
  crossing

These guarantees follow the existing Loopgate and AMP invariants:

- natural language never creates authority
- memory never creates authority
- bearer possession alone is insufficient
- results are content, not commands
- approvals bind to exact reviewed action shape

## Non-goals for v0

LoopRoot v0 does not try to be:

- a general-purpose operating system
- a full VM or hypervisor
- a general replacement for all human developer tooling
- a generic shell with better prompts
- a stealth prompt-mutation system

It is a governed runtime for AI work, not a consumer desktop environment.

## Threat model

LoopRoot should treat the AI as simultaneously:

- useful
- untrusted
- persistent
- misaligned under some prompts
- capable of opportunistic routing around weak controls

The AI should be treated more like:

- a contractor with no standing access
- an adversarial script
- a semi-autonomous worker process

than like a trusted user account.

The runtime should assume:

- the model will use the broadest capability it is given
- users may rubber-stamp weak approvals
- host tools may offer unsafe side channels
- direct shell is a universal escape hatch

## Core model

The AI should see:

- its own workspace
- its own toolset
- its own process environment
- its own network policy
- its own task state

It should not see the human host as its native machine.

Conceptually, LoopRoot should behave like a small AI-specific workstation:

- private ephemeral filesystem
- trusted built-in tools
- explicit boundary crossings
- governed network and integration access
- no ambient host authority

## Execution model

LoopRoot v0 should prefer typed, narrow capability services over arbitrary
shell.

The design rule is:

- if a class of shell usage can be replaced with a typed capability, it should
  be replaced

The first shell-derived service families to replace are:

- search and inspection
- file transformations and patching
- build and test orchestration
- version-control operations
- dependency and package-manager operations
- network fetch
- artifact handling

Arbitrary shell should be denied by default.

If shell exists at all in a later profile, it should be:

- heavily constrained
- capability-gated
- argument-normalized where possible
- treated as high-risk
- off by default

## Filesystem model

LoopRoot should not grant the AI direct access to the host working tree by
default.

Instead, the AI should work in an ephemeral governed workspace:

- created per session, run, or task scope
- populated from an explicit source snapshot
- writable inside the runtime
- disposable by default

Host crossing should be explicit:

- import host content into LoopRoot
- stage artifacts inside LoopRoot
- promote reviewed changes back to host or VCS

The AI should not directly mutate the user's live tree as its default editing
mode.

The intended promotion model is closer to:

- patch
- diff
- review
- merge

than to direct ambient mutation.

The current preferred shape is:

- authoritative AI output: patchset / diffset
- human collaboration and merge surface: branch / PR
- transport or quarantine bundle when needed: artifact bundle

LoopRoot should treat direct host-tree mutation as a special case, not the
default model.

## Network model

LoopRoot should assume zero ambient network.

Network access should be:

- denied by default
- capability-addressed
- destination-scoped
- protocol-scoped where possible
- auditable

This includes:

- model provider access
- source fetches
- package-manager access
- external APIs
- internal enterprise systems

The AI should never receive raw network freedom as a convenience default.

## Secret model

Secrets remain entirely inside Loopgate authority.

LoopRoot must not:

- expose raw API keys to the AI runtime
- inject reusable secret-bearing env vars into the AI workspace by default
- allow the AI to persist secret material into workspace outputs

LoopRoot may request secret-backed actions from Loopgate, but the AI should not
directly possess the secret-bearing material.

## Approval and security-event model

Approval UX must not look like chat text.

The target behavior is closer to:

- OS security prompts
- quarantine alerts
- intrusion-detection notifications
- enterprise policy denials

Important product rules:

- approvals are plain-English and explicit
- approvals are visually separate from model output
- high-risk denials may generate security events instead of user prompts
- some actions should never be overridable by the local user if enterprise
  policy denies them

Loopgate should remain the authority for:

- whether an action may be presented for approval
- whether it is categorically denied
- whether a security alert must be emitted

## Trusted built-in tools

LoopRoot v0 should ship with a small trusted tool surface designed for common
AI work without arbitrary shell.

Candidate categories:

- workspace file read, write, list, mkdir, move, diff
- search, symbol lookup, repo inventory
- patch create and apply inside LoopRoot
- structured git operations
- build and test runners with typed adapters
- package-manager adapters
- journal, note, todo, artifact, and review primitives

The tool surface should be intentionally smaller than a Unix shell, but more
useful than a locked-down demo sandbox.

## Human workflow

The human remains outside LoopRoot.

What stays human-native:

- plain-language tasking
- review of proposed changes
- explicit approvals
- promotion of changes to host/VCS
- security and policy override handling where allowed

What becomes AI-runtime-native:

- routine workspace manipulation
- bounded tool execution
- local artifact generation
- repeatable service-driven tasks

This implies a broader shift in the development lifecycle:

- from "AI edits your real machine directly"
- toward "AI operates inside a governed workspace and submits changes or
  actions for promotion"

The intended interaction style is:

- chat-first
- task-first
- clearly AI-colleague-oriented

It should not feel like a terminal emulator with a chatbot bolted on. It should
feel like you are working with an AI inside its own workstation.

## Human workflow for quarantine and security events

Quarantined actions should not look like chat denials.

The target UX is closer to:

- antivirus alert
- EDR / SOC warning
- enterprise policy block

The operator should see:

- what was blocked
- why it was blocked
- what policy or boundary was violated
- whether the action is reviewable at all
- whether security or IT was alerted

Some actions should be:

- allowed
- approval-eligible
- quarantined and escalated

Local approval should not be able to override actions that enterprise policy
defines as categorically denied.

## Relationship to current code

LoopRoot is not a greenfield fantasy layer. It builds on runtime pieces that
already exist:

- Loopgate session open, scoped tokens, MAC-bound privileged requests
- capability execution and approval gating
- sandbox import, stage, export, metadata, and list flows
- morphling/task-plan mediated worker model
- audit-first denial and execution semantics

What is missing is the runtime surface that makes those paths the default and
unavoidable for AI execution.

## v0 scope

LoopRoot v0 should prove:

1. an AI session gets an ephemeral governed workspace
2. the AI sees only LoopRoot tools by default
3. arbitrary host shell is unavailable by default
4. arbitrary outbound network is unavailable by default
5. host crossing is explicit and auditable
6. sensitive actions remain Loopgate-approved or denied

LoopRoot v0 does not need to prove:

- a new operating system kernel
- a full IDE replacement
- a general-purpose interactive desktop
- a universal sandbox backend for all platforms

## Open questions

These must be resolved before implementation:

- what is the concrete runtime substrate for the first LoopRoot profile
- how is the ephemeral workspace materialized fast enough for normal use
- which developer tasks require typed service adapters before shell can be
  removed
- how should promotion back to git or host files be represented
- what is the right approval UX for local and enterprise modes
- how are security events surfaced and forwarded in enterprise deployment
- what boundary crossings are allowed for "safe defaults" versus
  "enterprise-hard" mode

## Current conclusion

LoopRoot is the missing runtime layer that allows Loopgate and AMP to become
practical hard-governance infrastructure instead of advisory mediation attached
to human-native tools.

It is not yet an operating system, but it is the first step toward a runtime
model designed for AI and humans sharing one machine without sharing one trust
model.
