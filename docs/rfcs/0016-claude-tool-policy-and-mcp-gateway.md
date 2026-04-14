# RFC 0016: Claude Tool Policy Surface and Governed MCP Gateway

**Status:** proposed  
**Date:** 2026-04-12

## 1. Summary

Loopgate will treat **Claude Code** as the current operator harness and will
add a first-class **admin-managed policy surface** for Claude built-in tools and
for a future **governed MCP gateway**.

The design goal is simple:

- admins define what tools are allowed
- admins define what tools require approval
- admins define path, domain, and command constraints where relevant
- unknown tools and unknown MCP servers are denied by default
- Loopgate remains the authority boundary for governance decisions

This RFC covers two phases:

1. **Phase 1:** Claude built-in tool governance through the existing hook path
2. **Phase 2:** a single governed MCP gateway that brokers all MCP servers

## 2. Problem

The current hook integration is too narrow:

- only a small subset of Claude tools is mapped
- unknown tools are currently allowed through
- policy is mostly category-level rather than tool-specific
- there is no admin-friendly template system for tool governance
- there is no Loopgate-owned MCP gateway surface

That is inconsistent with the project invariants:

- fail closed by default
- no ambient authority
- no hidden bypasses
- operator-visible policy must reflect the real execution path

## 3. Non-goals

This RFC does **not** propose:

- an admin GUI
- a generic passthrough MCP proxy that merely observes traffic
- trusting Claude permission rules as the primary authority
- making every Claude internal event a governed authority surface

## 4. Product and trust model

### 4.1 Claude Code

Claude Code is the active operator harness.
It is not the policy engine.

Loopgate uses Claude hooks to inspect pending tool calls and decide:

- allow
- block
- require approval

For Claude built-in tools, the immediate enforcement path is:

`Claude Code -> command hook -> Loopgate pre-validation -> allow/block`

That is a real governance surface, but it is still a **pre-execution gate** over
Claude's own built-in tools rather than a fully Loopgate-executed capability
path.

For the current MVP, approval-required Claude built-ins use a local hook bridge:

- `PreToolUse` returns Claude `ask`
- the engineer handles that approval inline in Claude Code
- Loopgate records a local hook approval keyed by Claude `tool_use_id`
- Loopgate appends `approval.created` for the inline approval
- `PostToolUse` / `PostToolUseFailure` resolve that local approval state and append `approval.granted`
- `SessionEnd` abandons unresolved local approvals and appends `approval.cancelled`

This preserves a real local audit trail without pretending that Claude built-in
execution is already a full Loopgate-executed capability path.

This does **not** imply that admins should manually approve routine engineer
tool asks through a separate CLI. Hard IT denials remain immediate deny events
for audit and aggregation; ordinary inline asks stay with the engineer in the
Claude harness.

For a smaller subset of higher-friction actions, a stronger Loopgate approval
surface may still be appropriate. The conservative interactive pattern is:

- `PreToolUse` creates the pending approval
- `PermissionRequest` correlates to that pending approval by request fingerprint
- a Loopgate-owned surface resolves the approval
- the hook then allows or denies on behalf of the user

This is possible, but interactive Claude Code does not offer a general
"suspend and wait on an external window forever" primitive. Designs that assume
transparent external waiting should be treated skeptically unless they are
built around the actual hook contract.

### 4.2 Governed MCP gateway

The future MCP model is:

`Claude Code -> one Loopgate MCP gateway -> approved MCP servers/tools`

Loopgate must own:

- which MCP servers exist
- how they are launched
- which tools are exposed
- which secrets or env vars they get
- which network or filesystem boundaries apply
- audit and approval for invocation

Loopgate must **not** merely watch Claude talk directly to arbitrary MCP
servers.

The current broker work is intentionally narrower than launch or execution:

- Loopgate loads only policy-declared MCP servers and tools at startup
- Loopgate exposes read-only inventory and decision checks over the local control plane
- Loopgate validates invocation envelopes before any launch path exists
- Loopgate can now prepare a typed pending MCP approval object for `needs_approval` invocations
- Loopgate can now resolve that prepared MCP approval object with nonce + manifest binding and append `approval.granted` / `approval.denied`
- Loopgate can now validate the exact future execution envelope against a granted approval and append `mcp_gateway.execution_checked`
- Loopgate can now request-drive one declared `stdio` server launch and execute one exact approved `tools/call` against that launched server
- approval preparation, launch, and execution are local and audited; there is still no autonomous broker worker or restart loop

## 5. Authority surface classification

Not every Claude action needs the same governance treatment.

See also: [Claude Code authority surfaces threat model](../design_overview/claude_code_authority_surfaces_threat_model.md).

### 5.1 Primary authority surfaces

These can directly change state or widen reach and should be governed first:

- `Bash`
- `Write`
- `Edit`
- `MultiEdit`
- `Read`
- `Glob`
- `Grep`
- `WebFetch`
- `WebSearch`
- any future Loopgate MCP gateway tool invocation

### 5.2 Secondary governance surfaces

These influence behavior but are not themselves the primary mutation path:

- `PermissionRequest`
- `TaskCreated`
- `TaskCompleted`
- `SubagentStart`
- `SubagentStop`

These matter because they can widen activity or hide it in parallel work, but
they should be added deliberately after the primary tool surface is stable.

### 5.3 Harness internals / observability surfaces

These are mostly for audit and diagnostics rather than direct authorization:

- `SessionStart`
- `SessionEnd`
- `UserPromptSubmit`
- `InstructionsLoaded`
- `ConfigChange`
- `FileChanged`
- `Notification`
- `Stop`
- `StopFailure`

They can still matter for policy hygiene, but they are not the first place to
anchor authority.

`SessionStart` and `UserPromptSubmit` should remain audit-only in the current
Claude Code harness path. Follow-on memory access, if reintroduced later,
should go through explicit governed memory operations rather than hook-time
re-injection.

The continuity target remains durable, high-signal facts and bounded recall,
not transcript retention.

`SessionEnd` is lifecycle-only in the current MVP. It may record exit reasons
and local session boundaries for future continuity rollover work, but it is not
itself a durable memory write path.

Claude hook `session_id` continuity binding is owned by Loopgate local state,
not by Claude itself. The session label is an input hint; the authoritative
binding and thread IDs are persisted by Loopgate under local runtime state.

### 5.4 Threat-model takeaway

The key threat-model conclusion for this RFC is:

- unknown built-in tools must deny by default
- hook unavailability must deny by default
- constrained tools must deny on malformed or missing input fields
- secondary delegation-style events matter, but they are not the first
  authorization anchor
- known governance-relevant non-tool hook events should block until explicitly
  implemented rather than being treated as audit noise
- known observability hook events may be accepted as audit-only, but that must
  be explicit in audit records
- a future MCP surface only fits the Loopgate model if Claude reaches exactly
  one Loopgate-owned MCP gateway rather than arbitrary MCP servers

## 6. Admin policy surface

### 6.1 Storage and distribution

Admin policy remains **signed YAML**.

Primary operator workflow:

- edit YAML policy templates
- validate locally with an admin CLI
- sign policy
- distribute through normal config rollout

Future distribution sources may include:

- local checked-in policy
- GitHub repository
- GitLab repository
- a custom HTTPS config repository

Remote retrieval must not weaken integrity:

- the fetched policy must still be signature-verified
- fetch failure must not silently widen permissions
- stale policy behavior must be explicit

### 6.2 Admin CLI

The first admin management surface should be a CLI, not a GUI.

Expected commands:

- validate policy
- explain effective tool policy
- render starter templates
- diff two policies
- fetch and verify remote policy bundle

The admin `diff` command should be treated as an **effective-policy diff**,
not a literal YAML source diff. Operators still need ordinary VCS review for
comments, ordering, and textual provenance.

### 6.3 Policy model

Phase 1 adds a `tools.claude_code.tool_policies` surface with per-tool policy
entries.

Initial fields:

- `enabled`
- `requires_approval`
- `allowed_roots`
- `denied_paths`
- `allowed_domains`
- `allowed_command_prefixes`
- `denied_command_prefixes`

Unknown Claude tool names in policy are a load-time error.

## 7. Phase 1 design

### 7.1 Scope

Phase 1 governs Claude built-in tools through the existing
`POST /v1/hook/pre-validate` path.

### 7.2 Default behavior

- unknown tools: **deny**
- malformed inputs for constrained tools: **deny**
- Loopgate unavailable: **deny** by default
- tool not mentioned in `tool_policies`: fall back to category policy

### 7.3 Constraint model

- `Bash`: command prefix allow/deny lists
- `Read` / `Write` / `Edit` / `MultiEdit`: path root / deny checks
- `Glob` / `Grep`: search root path checks
- `WebFetch`: domain checks
- `WebSearch`: category and tool enable/approval only for the first slice unless
  a deterministic domain restriction path is added

### 7.4 Audit

Every hook decision must append audit before returning allow or block.

Audit fields should include:

- tool name
- effective decision
- denial reason
- peer binding facts
- whether a tool-specific Claude policy was involved

## 8. Phase 2 design: governed MCP gateway

### 8.1 Core model

Loopgate exposes one MCP gateway surface.

That gateway can broker only policy-declared MCP servers.

Each declared server has:

- stable server id
- launch contract
- working directory policy
- allowed environment variables and secret refs
- transport expectations
- per-tool exposure rules
- approval requirements

The initial typed policy surface is:

- `tools.mcp_gateway.deny_unknown_servers`
- `tools.mcp_gateway.servers.<server_id>.enabled`
- `tools.mcp_gateway.servers.<server_id>.requires_approval`
- `tools.mcp_gateway.servers.<server_id>.transport`
- `tools.mcp_gateway.servers.<server_id>.launch.command`
- `tools.mcp_gateway.servers.<server_id>.launch.args`
- `tools.mcp_gateway.servers.<server_id>.working_directory`
- `tools.mcp_gateway.servers.<server_id>.allowed_environment`
- `tools.mcp_gateway.servers.<server_id>.secret_environment`
- `tools.mcp_gateway.servers.<server_id>.tool_policies.<tool_name>.enabled`
- `tools.mcp_gateway.servers.<server_id>.tool_policies.<tool_name>.requires_approval`
- `tools.mcp_gateway.servers.<server_id>.tool_policies.<tool_name>.required_arguments`
- `tools.mcp_gateway.servers.<server_id>.tool_policies.<tool_name>.allowed_arguments`
- `tools.mcp_gateway.servers.<server_id>.tool_policies.<tool_name>.denied_arguments`
- `tools.mcp_gateway.servers.<server_id>.tool_policies.<tool_name>.argument_value_kinds`

The current local control-plane MCP routes are:

- `GET /v1/mcp-gateway/inventory`
- `GET /v1/mcp-gateway/server/status`
- `POST /v1/mcp-gateway/decision`
- `POST /v1/mcp-gateway/server/ensure-launched`
- `POST /v1/mcp-gateway/server/stop`
- `POST /v1/mcp-gateway/invocation/validate`
- `POST /v1/mcp-gateway/invocation/request-approval`
- `POST /v1/mcp-gateway/invocation/decide-approval`
- `POST /v1/mcp-gateway/invocation/validate-execution`
- `POST /v1/mcp-gateway/invocation/execute`

The current launch-and-execute slice is intentionally narrow:

- Loopgate can request-drive launch ownership for one declared `stdio` server
- Loopgate can request-drive explicit stop/reset of one launched declared server
- Loopgate can expose a read-only launched-server runtime projection for declared MCP servers
- Loopgate reuses a live declared server inside the same runtime when possible
- Loopgate injects only policy-declared env vars and secret refs
- Loopgate appends `mcp_gateway.server_launched` before committing launched state
- Loopgate appends `mcp_gateway.server_stopped` after a real stop and does not resurrect state if that audit append later fails
- Loopgate executes one exact approved `tools/call` synchronously over the retained stdio transport
- Loopgate does **not** introduce a restart daemon or autonomous broker worker

The validation and approval routes still create or reuse typed MCP approval
objects for validated invocations and do **not** reuse the generic
capability-approval store. The first execution path is deliberately narrow and
request-driven rather than a generic multiplexed broker.

The first transport is intentionally narrow:

- `stdio`

This is a policy contract plus a minimal request-driven launch/runtime broker,
not yet a generic passthrough broker. The first runtime slices load
policy-declared server and tool manifests at startup, deny unknown or disabled
entries, keep launch state explicit and server-owned, and allow one exact
approved tool execution path without background lifecycle workers.

Before subprocess launch exists, the broker should still expose a typed
invocation-envelope validation path for:

- `server_id`
- `tool_name`
- top-level `arguments` object shape
- optional policy-declared top-level argument constraints

That keeps malformed or obviously out-of-model MCP calls out of the future
execution path without pretending that generic per-tool schemas are already
known.

### 8.2 Deny-by-default behavior

- unknown MCP server: deny
- unknown MCP tool on known server: deny
- malformed tool args: deny
- server launch policy mismatch: deny
- secret resolution failure: deny

### 8.3 Why this fits Loopgate

This keeps the governed execution path real:

- Claude does not get direct ambient MCP authority
- Loopgate remains the gateway and the audit point
- policy and approval remain server-side

## 9. Risks

### 9.1 Built-in Claude tool governance is pre-execution, not full mediation

Phase 1 is still a hook gate over Claude's built-in tools.
That is useful, but it is not the same as Loopgate executing the tool itself.

We should describe it honestly as:

- strong deny gate
- not full tool mediation

### 9.2 Policy complexity

Path, domain, and command policy can become unreadable if allowed to drift into
regex soup.

Mitigation:

- templates
- typed fields
- validation CLI
- narrow first-class semantics

### 9.3 MCP gateway breadth

“Any MCP server” is too broad if taken literally.

Loopgate should support **any policy-declared MCP server**, not arbitrary
ambient passthrough.

## 10. Implementation plan

### Phase 1

1. deny unknown Claude tools in the hook handler
2. add `tools.claude_code` policy parsing and validation
3. enforce Bash prefix and file-path constraints in pre-validation
4. update tests and policy examples
5. update the historical fail-open ADR and hook docs

### Phase 2

1. define MCP server manifest schema
2. add Loopgate-owned MCP broker lifecycle
3. add per-server and per-tool policy enforcement
4. add audit and approval for brokered tool use
5. add admin CLI support for template rendering and remote policy retrieval

## 11. Open questions

- Whether some Claude secondary surfaces should use hook-only audit first before
  full policy controls
- Whether WebSearch should be governed only as on/off plus approval, or whether
  a deterministic domain-restriction layer is worth the complexity
- Whether the MCP gateway should be exposed only over the local Loopgate socket
  or also through a managed enterprise attachment later
