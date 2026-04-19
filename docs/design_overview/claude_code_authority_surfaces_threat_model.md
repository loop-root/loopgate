**Last updated:** 2026-04-12

# Claude Code Authority Surfaces Threat Model

This is a narrow threat model for the current **Claude Code + Loopgate hooks**
MVP and the planned **single Loopgate MCP gateway**.

It does not replace the repo-wide [Loopgate Threat Model](../loopgate-threat-model.md).
It exists to answer one specific question:

Which Claude-facing surfaces can actually widen authority, and which ones are
only harness or observability events?

## Scope

In scope:

- Claude Code `PreToolUse` hook handling via `POST /v1/hook/pre-validate`
- Claude built-in tools currently mapped in [`internal/loopgate/server_hook_handlers.go`](../../internal/loopgate/server_hook_handlers.go)
- tool-specific policy in [`internal/config/policy.go`](../../internal/config/policy.go)
- current hook enforcement in [`internal/loopgate/claude_code_hook_policy.go`](../../internal/loopgate/claude_code_hook_policy.go)
- future `Claude -> one Loopgate MCP gateway -> governed MCP servers` path from [RFC 0016](../rfcs/0016-claude-tool-policy-and-mcp-gateway.md)

Out of scope:

- Claude model behavior in general
- provider-specific prompt injection beyond how it reaches tool calls
- repo-wide filesystem, secrets, or multi-tenant threats except where they are directly affected by Claude authority surfaces

## Assumptions

- Same-user local processes are realistic attackers.
- Claude Code is an operator harness, not an authority source.
- Hook policy is only meaningful if unknown tools are denied and Loopgate is reachable.
- A future MCP surface is only in scope if Claude talks to exactly one Loopgate-owned gateway rather than directly to arbitrary MCP servers.

## Assets

- Loopgate policy integrity
- approval integrity
- filesystem and shell reach
- outbound network reach
- audit truth for hook decisions
- future MCP server launch and invocation authority

## Trust boundaries

1. `Claude Code -> hook command`
   The hook subprocess receives pending tool intent from Claude Code. This is a policy decision point, not a trusted authority source.

2. `hook command -> Loopgate local control plane`
   The hook sends a bounded JSON request over HTTP on the Unix socket. Peer binding and strict request decoding are the hard controls here.

3. `Loopgate policy engine -> built-in tool execution path`
   For Claude built-ins, Loopgate currently pre-validates rather than directly executing the tool. That means the hook decision must fail closed or governance becomes theater.

4. `Claude Code -> Loopgate MCP gateway` (future)
   This is the only acceptable MCP trust boundary for the planned design. Direct Claude access to arbitrary MCP servers is out of model.

## Authority surface classification

### Primary authority surfaces

These can directly mutate state, widen data reach, or execute external effects.

- `Bash`
- `Write`
- `Edit`
- `MultiEdit`
- `Read`
- `Glob`
- `Grep`
- `WebFetch`
- `WebSearch`
- future Loopgate-governed MCP tool invocations

Why they matter:

- they touch files, shell, or network
- they can expand the model-visible context
- they create real side effects or widen what later side effects are possible

### Secondary governance surfaces

These do not directly perform the main side effect, but they can widen activity or hide it.

- `PermissionRequest`
- subagent or task-start/task-stop style events
- future delegation or teammate events

Why they matter:

- they can create parallel work the operator may not notice
- they can expand the set of pending actions
- they can turn one approved workflow into many concurrent workflows

### Observability and harness-internal surfaces

These matter for audit and hygiene, but they should not be the primary authorization anchor.

- `SessionStart`
- `SessionEnd`
- `InstructionsLoaded`
- `ConfigChange`
- `FileChanged`
- `Notification`
- `Stop`
- `StopFailure`

Why they matter less:

- they report context rather than directly widening capability
- they are useful for audit trails and tamper detection
- treating them as first-class authority checks would add noise before securing the real side-effect surfaces

Special note for `SessionStart`:

- it may inject bounded remembered context
- that injected text is still content, not authority
- it must reuse Loopgate's existing remembered-context contract rather than
  inventing a second history-summary format

Special note for `UserPromptSubmit`:

- it may inject bounded historical recall context based on the submitted prompt
- it remains a content-only lane, not an approval or policy lane
- retrieval must stay server-side and use Loopgate's bounded remembered-context
  and local session-history paths rather than a client-managed history source
- the continuity target is durable, high-signal facts rather than transcript
  retention or arbitrary prose replay

Special note for `SessionEnd`:

- it is a lifecycle/audit boundary, not a durable memory write path
- the exit reason is useful for local continuity hygiene, but it must not be
  treated as a model-authored authority input
- any future thread rollover or promotion tied to SessionEnd should reuse the
  existing append-only local session-history model rather than inventing a
  second parallel history store
- Claude `session_id` binding should stay Loopgate-owned and local-only so the
  hook harness does not become the source of truth for local history state

## Abuse paths

### TM-CC-01: Unknown Claude tool bypass

Attack:

1. Claude emits a tool name not present in Loopgate's governance map.
2. The hook path allows it through by default.
3. Claude executes a tool outside reviewed policy semantics.

Impact:

- silent authority widening
- governance theater

Current control:

- unknown tools are denied by default in [`internal/loopgate/server_hook_handlers.go`](../../internal/loopgate/server_hook_handlers.go)
- `tools.claude_code.deny_unknown_tools` defaults true in [`internal/config/policy.go`](../../internal/config/policy.go)
- hook audit now records `hook_surface_class` and whether the event was handled
  as `enforced` or `audit_only`

Residual risk:

- future Claude tool additions still require explicit map updates and tests

### TM-CC-02: Hook unavailable but built-in tools still execute

Attack:

1. Loopgate is unreachable or hook transport fails.
2. Claude continues executing built-in tools anyway.
3. The apparent governed path is not the real execution path.

Impact:

- fail-open execution
- audit gaps

Current control:

- the tracked hook bundle is fail-closed by default in [`claude/hooks/scripts/loopgate_pretool.py`](../../claude/hooks/scripts/loopgate_pretool.py)

Residual risk:

- built-in tools remain Claude-executed, not Loopgate-executed
- this is acceptable only as an MVP pre-execution gate, not the end-state authority model

### TM-CC-02B: SessionStart becomes a remembered-context side channel

Attack:

1. A local caller triggers the SessionStart hook path repeatedly.
2. Loopgate returns unbounded or raw historical context instead of the bounded remembered-context projection.
3. Historical context leaks through a hook path that was supposed to stay narrow.

Impact:

- unnecessary historical-context disclosure
- drift between prompt context and authoritative remembered-context policy

Current control:

- SessionStart uses the same bounded remembered-context formatter as the
  existing remembered-context prompt path
- the injected text is explicitly marked historical context and not fresh verification

Residual risk:

- same-user local callers remain in scope, so the hook path must stay bounded
  and avoid raw historical-context artifacts

### TM-CC-02C: UserPromptSubmit turns prompt text into an unbounded remembered-context oracle

Attack:

1. A user or local process submits arbitrary prompts crafted to retrieve historical context.
2. Loopgate injects broad or raw historical artifacts rather than bounded recall.
3. The prompt hook becomes a hidden historical-context exfiltration lane.

Impact:

- unnecessary historical-context disclosure
- repeated injection of irrelevant or oversized historical context
- drift between prompt-context recall and Loopgate's bounded recall semantics

Current control:

- `UserPromptSubmit` uses server-side `discover -> recall -> format`
- the recall path remains bounded by `max_items` and `max_tokens`
- injected output is explicitly labeled historical context rather than fresh
  verification

Residual risk:

- same-user local callers can still probe their own local remembered-context
  surface, so the hook path must stay local-node-only and bounded

### TM-CC-02D: Hook approval becomes fake authority theater

Attack:

1. Loopgate says a built-in Claude tool needs approval.
2. The harness shows a local prompt, but Loopgate never records the request or
   outcome.
3. The apparent approval flow cannot be audited or tied back to the governed
   session lifecycle.

Impact:

- approval theater
- weak operator traceability
- unresolved local approval state across retries or session end

Current control:

- `PreToolUse` now records a local hook approval keyed by Claude `tool_use_id`
- `approval.created` is appended when the inline Claude approval is surfaced
- `PostToolUse` and `PostToolUseFailure` resolve that local approval state and
  append `approval.granted`
- `SessionEnd` abandons unresolved local hook approvals for the ended session
  and appends `approval.cancelled`

Residual risk:

- this is a local Claude-built-in approval bridge, not yet the final
  org-admin approval distribution model
- manual user rejection in Claude does not currently emit a first-class
  resolution event, so abandonment still happens at session boundary rather
  than immediately
- a future Loopgate approval window is possible only if it is designed around
  the actual `PermissionRequest` and notification hook contract, not around an
  assumed indefinite external wait primitive in interactive Claude Code

### TM-CC-03: Constraint parsing mismatch

Attack:

1. A constrained tool call omits or reshapes a field such as `command`, `file_path`, `path`, or `url`.
2. Policy code mis-parses the payload and accidentally allows it.

Impact:

- path, command, or domain restrictions become incomplete

Current control:

- constrained tools deny on missing required fields in [`internal/loopgate/claude_code_hook_policy.go`](../../internal/loopgate/claude_code_hook_policy.go)

Residual risk:

- any new Claude tool schema or payload shape change needs regression tests before it is trusted

### TM-CC-04: Secondary event abuse hides authority expansion

Attack:

1. Claude starts using delegation, permission, or teammate-style events that are not governed.
2. Those events create additional work or widen concurrency.
3. Operators see only the primary tool event and miss the expansion.

Impact:

- approval fatigue
- hidden parallelism
- incomplete audit story

Current control:

- RFC 0016 classifies these as secondary governance surfaces rather than ignoring them
- current handler blocks known secondary governance events until they are
  explicitly implemented, rather than treating them as observability noise

Residual risk:

- they are not yet governed in the current hook policy implementation

### TM-CC-05: Direct MCP access bypasses Loopgate

Attack:

1. Claude is configured with arbitrary MCP servers directly.
2. Loopgate governs only built-ins or only one partial tool subset.
3. MCP tools execute outside Loopgate policy and audit.

Impact:

- full authority bypass
- unmanaged secrets, network reach, and tool surface

Required control:

- Claude must talk to one Loopgate-owned MCP gateway only
- unknown MCP servers and tools must deny by default

Residual risk:

- Phase 2 is not implemented yet, so this remains an architectural requirement rather than a completed control

## Priorities

### High

- keep unknown built-in tools denied by default
- keep hook unavailability fail-closed
- expand policy coverage only with explicit map entries and regression tests

### Medium

- classify and govern secondary delegation-style surfaces before enabling them broadly
- add clearer audit fields that distinguish base-category policy from tool-specific policy

### High for Phase 2

- never allow direct ambient Claude -> arbitrary MCP access
- make Loopgate the only MCP gateway
- deny unknown servers, unknown tools, malformed args, and server-launch mismatches

## Recommended next controls

1. Add `loopgate-policy-admin diff` to review normalized policy changes before signing.
2. Keep RFC 0016's authority-surface classification updated as Claude adds or changes event types.
3. When Phase 2 begins, write the MCP gateway threat model before implementing passthrough or server launch code.
