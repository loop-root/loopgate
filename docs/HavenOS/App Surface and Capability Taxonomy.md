**Last updated:** 2026-03-24

# Haven OS — App Surface and Capability Taxonomy

## Purpose

This document defines the intended Haven app model after the product pivot from "desktop demo" to "assistant operating environment."

See also: [MVP Experience Spec](./MVP%20Experience%20Spec.md)

The goal is to make two things explicit:

1. which apps and tools are native to Haven and should feel low-friction inside Morph's own environment
2. which tools remain external, untrusted, or boundary-crossing and must stay tightly governed by Loopgate

This is a product model, not a permission bypass.

Loopgate remains the sole authority boundary.

---

## Core Principle

Morph should be able to do a great deal inside Haven without repeatedly asking for permission.

That does **not** mean Morph or a Haven app gets direct authority.

It means:

- Loopgate pre-registers approved Haven-native capabilities
- Loopgate grants those capabilities to the active Haven shell or actor
- the active Haven actor may use sandbox-local Haven-native capabilities without per-action approval when the request stays entirely inside Morph's own world
- Haven should only expose tool surfaces to the model for capabilities Loopgate actually granted, not merely for tools that happen to be registered locally
- proactive or scheduled task execution should still default to approval unless Loopgate has a standing-approved execution class for that sandbox-local task family
- Loopgate still validates every request
- Loopgate still audits every request
- Loopgate still denies execution when the request leaves the approved envelope

Low-friction use inside Haven is still mediated use.

---

## UI Shells

Haven should support multiple shells over the same underlying app/runtime model.

### Desktop Shell

This is the current primary surface.

It presents Haven as a visible mini desktop where Morph appears to live.

It is responsible for:

- window management
- spatial file placement
- launchable Haven apps
- Morph avatar and presence
- ambient life and environment feel

### Sidebar Shell

This is a later surface, not the current primary focus.

It should expose the same core Haven apps without requiring the user to enter the desktop every time.

Expected sidebar apps:

- Messenger
- Workspace
- Activity Monitor
- Loopgate
- Desktop launcher

The sidebar is not a second product. It is another shell over the same Haven apps and Loopgate-backed state.

---

## Core Haven Apps

These are the baseline app surfaces Haven should treat as first-class.

### Messenger

Primary conversational surface for Morph.

Responsibilities:

- threads
- approvals
- task progress
- artifact references
- task follow-up and review prompts

### Workspace

Morph's visible filesystem browser inside Haven.

Responsibilities:

- browse sandbox-resident files
- import copies into Haven
- inspect artifacts
- review diffs
- export approved results

### Activity Monitor

Human-readable execution and audit projection.

Responsibilities:

- plain-English capability use
- denials
- approvals
- worker activity
- artifact creation

### Loopgate

Security/status surface for the operator.

Responsibilities:

- capability posture
- pending approvals
- connection state
- policy summary
- trust and security alerts

### Settings

User preferences and non-governance product settings.

Responsibilities:

- appearance
- ambient behavior preferences
- setup/runtime preferences

### Desktop

A Haven app in its own right, even when it is the current main shell.

Responsibilities:

- spatial desktop environment
- icon and artifact placement
- Morph presence projection
- ambient UI state

---

## Future Haven-Native Apps

These should be modeled as native Haven apps, not ad hoc tool bundles.

### Journal

Morph's private journaling and note space inside Haven.

### Paint

Creative canvas and gallery inside Haven.

### Program Studio

A bounded environment where Morph can write and run small local programs inside Haven.

This should prefer typed runtime tools over generic shell authority.

### Research Browser

A Haven app for web research, downloads, and capture.

Because it crosses the network boundary, it should be treated as an external-access app even if the UX feels native.

---

## Capability Classes

Capabilities should be grouped by trust and boundary class, not only by implementation detail.

### Class A: Haven-Native Local Capabilities

These operate entirely inside Morph's Haven environment.

They should usually require no per-action user approval, but they must still be executed through Loopgate.

Examples:

- `workspace.read`
- `workspace.list`
- `workspace.write`
- `workspace.move`
- `workspace.rename`
- `workspace.delete`
- `artifact.stage`
- `artifact.inspect`
- `journal.entry.write`
- `journal.entry.read`
- `paint.canvas.stroke`
- `paint.canvas.clear`
- `paint.image.save`
- `desktop.layout.read`
- `desktop.layout.write`
- `program.file.write`
- `program.run.local`
- `program.stop.local`

Expected controls:

- sandbox-only paths
- bounded file size and output limits
- interpreter allowlists
- time budgets
- CPU and memory limits where applicable
- audit logging for every execution

### Class B: Boundary-Crossing Capabilities

These leave Morph's private Haven environment or interact with untrusted systems.

They should remain policy-governed and often approval-gated.

Examples:

- `host.import`
- `host.export`
- `browser.fetch`
- `browser.search`
- `mcp.invoke`
- `connection.pkce.start`
- `connection.pkce.complete`
- `provider.request.external`

Expected controls:

- explicit policy binding
- approval rules by action class
- connection and secret scoping
- result redaction and quarantine where needed
- stronger audit requirements

### Class C: Governance Capabilities

These are not ordinary Morph capabilities.

They belong to the governance plane and should remain unavailable to normal Haven apps and actors.

Examples:

- `policy.edit`
- `capability.registry.edit`
- `connection.provider.register`
- `secret.backend.override`
- `admin.auth.configure`
- `loopgate.transport.expose`

Morph must not acquire these through prompts, summaries, plans, or app manifests.

---

## App Registration Model

Apps should be registered with Loopgate as explicit app manifests.

At minimum, an app manifest should define:

- app identity
- app version
- signer identity
- manifest hash
- declared capabilities
- runtime class
- allowed path classes
- network class
- whether helper delegation is allowed

Loopgate should treat the manifest as input to validation, not as authority by itself.

The actual authority comes from:

- Loopgate's registered app record
- Loopgate's trust decision for the signer or manifest
- policy binding for that app class

---

## Tool and App Integrity

Haven should not automatically trust tools just because they are "inside Haven."

Tool and app integrity should be explicit.

Required direction:

- every installable app or external tool bundle has a signed manifest
- Loopgate stores the signer identity, manifest hash, and tool digest metadata
- Loopgate checks the presented binary/script/package digest before launch
- execution is denied when the digest or signature does not match the registered record

This preserves the security invariant that trust comes from Loopgate's validation path, not from naming or placement.

For native built-in Haven apps, the same idea still applies:

- built-in tool definitions are registered by Loopgate
- capability envelopes are explicit
- runtime envelopes are explicit
- tampered tool definitions must fail closed

---

## MCP and Third-Party Tools

MCP should be treated as an external connector layer, not as a trusted native extension system.

An MCP server is:

- third-party unless proven otherwise
- capable of hostile or malformed output
- able to expand Morph's reach outside Haven

Therefore:

- MCP servers must be registered through Loopgate
- secrets must be stored as Loopgate-managed secret refs
- tool schemas and server metadata are descriptive, not authoritative
- each MCP tool must bind to a capability class before it can be invoked
- result filtering, redaction, and audit rules remain in Loopgate

MCP is powerful, but it is not a bypass around Haven-native capability design.

---

## Transport and UI Boundary

Loopgate should not expose a separate browser-admin plane by default.

The intended product surfaces are Haven shells and Haven apps talking to Loopgate over the local control-plane transport.

Implications:

- no standalone localhost admin UI by default
- no convenience browser surface that quietly becomes a second authority path
- UI-facing status and approval endpoints remain internal Loopgate APIs for approved local clients

This keeps the transport model aligned with the "Loopgate is the kernel" design.

---

## Immediate Build Direction

The next product step is not to make generic execution broader.

The next step is to make Haven-native capabilities richer and more explicit so Morph can genuinely live and work inside Haven without blurring the security boundary.

Near-term focus:

- finish the review/apply artifact flow
- replace generic shell dependence with typed Haven-native tools where practical
- define the first native app set: Messenger, Workspace, Activity, Loopgate, Journal, Paint, Program Studio
- introduce MCP later as a separate external-tool layer
