**Last updated:** 2026-04-12

# Claude Code Hooks MVP

This document defines the current Loopgate MVP direction.

The active operator harness is **Claude Code**.
Loopgate remains the authority boundary.

## 1. Product decision

Use **Claude Code + project hooks + Loopgate** as the MVP harness.

That means:

- Claude Code is the operator-facing shell
- Loopgate is the sole authority for governance decisions
- project-local `.claude/` configuration is part of the active product surface
- Loopgate owns the governed path for hook validation, approvals, policy, and
  audit

## 2. Why this is the conservative path

This direction keeps the trust model clear:

- we do not need to invent a second operator UI
- we do not need to compete on general-purpose harness UX
- we can focus on governed behavior, approvals, policy, and audit
- we can ship governance value on top of a tool people already use

The harness is replaceable.
The authority boundary is not.

## 3. MVP shape

The current MVP stack is:

`Claude Code -> project hook command -> Loopgate hook validation -> policy / audit / denial`

Minimum repo-owned pieces:

- tracked hook source under `claude/hooks/scripts/`
- operator-installed Claude settings entries under `~/.claude/settings.json` or project-local `.claude/settings.json`
- `claude/hooks/scripts/loopgate_pretool.py`
- `claude/hooks/scripts/loopgate_posttool.py`
- `claude/hooks/scripts/loopgate_posttoolfailure.py`
- `claude/hooks/scripts/loopgate_sessionstart.py`
- `claude/hooks/scripts/loopgate_sessionend.py`
- `claude/hooks/scripts/loopgate_userpromptsubmit.py`
- `claude/hooks/scripts/loopgate_permissionrequest.py`
- `POST /v1/hook/pre-validate`
- `runtime/state/claude_hook_sessions.json`
- repo docs that explain the trust model and expected workflow

The hook path is intentionally narrow.
It begins with **tool pre-validation**, not a sprawling background automation
system.

## 4. Hook design constraints

### 4.1 Loopgate stays authoritative

Claude Code is the harness.
It is not the policy engine.

The hook may:

- inspect the pending tool call
- ask Loopgate whether the tool call should proceed
- block the tool call with a clear reason

The hook must not:

- invent permissions locally
- treat prompt text as authority
- bypass Loopgate when Loopgate is required for governance

### 4.2 Fail closed by default

For this MVP, the hook path is part of the primary governance story.

That means:

- unreachable Loopgate should block governed tool execution by default
- any opt-out to fail-open is an explicit operator override, not the repo default

### 4.3 Use command hooks for primary enforcement

Claude Code supports command hooks, HTTP hooks, prompt hooks, and agent hooks.

For primary enforcement, prefer a **command hook** that talks to Loopgate over
the local Unix socket.

Reason:

- command hooks can block deterministically in-process
- Claude's HTTP hooks treat connection failures and non-2xx responses as
  non-blocking errors

That makes raw HTTP hooks the wrong primitive for primary deny behavior.

### 4.4 Start narrow

The MVP should gate only the tools we actually understand and classify.

Current first-pass tool surface:

- `Bash`
- `Write`
- `Edit`
- `MultiEdit`
- `Read`
- `Glob`
- `Grep`
- `WebFetch`
- `WebSearch`

Additional hook events can come later:

- `PermissionRequest`
- `PostToolUse`
- `ConfigChange`
- `SessionStart`
- `SessionEnd`
- `UserPromptSubmit`

But they should be added only with a clear governance reason.

Current handler posture:

- `PreToolUse` is an **enforced** authority surface
- `SessionStart` remains **audit-only** for local lifecycle binding
- `UserPromptSubmit` remains **audit-only** and does not inject additional
  remembered context on each turn
- `SessionEnd` records a **local session lifecycle boundary** for audit and
  future local session-history rollover work, but does not itself create
  durable continuity state
- other known observability events such as `ConfigChange` may be accepted as
  **audit-only**
- known governance-relevant non-tool events such as `PermissionRequest` are
  **blocked until explicitly implemented**
- unknown hook events are **blocked by default**

`UserPromptSubmit` should remain audit-only in the Claude Code harness path.
Any future remembered-context or continuity access should happen through
explicit governed retrieval paths rather than hook-time prompt injection.

Local remembered-context lifecycle in the current Claude Code MVP is:

1. `SessionStart` records a local lifecycle boundary and binds Claude
   `session_id` to a local Loopgate-owned session-history record
2. `UserPromptSubmit` records prompt submission for audit and session-history
   correlation, but does not add remembered-context recall
3. `SessionEnd` records the session boundary and exit reason for audit and
   attempts a safe local session-history rollover
4. durable state, if introduced later, should still come from explicit
   governed write paths rather than hook text alone

Local approval lifecycle in the current Claude Code MVP is:

1. `PreToolUse` returns `ask` when policy says the built-in tool needs approval
2. the engineer sees the approval inline in Claude Code rather than switching to a separate Loopgate approval UI or CLI
3. Loopgate records a local hook approval keyed by Claude `tool_use_id`
4. Loopgate appends `approval.created` to the append-only ledger for that inline approval
5. `PostToolUse` or `PostToolUseFailure` resolves that local approval state and appends `approval.granted`
6. `SessionEnd` abandons unresolved local hook approvals for the session and appends `approval.cancelled`

This hook approval bridge is local and session-scoped.
It is not a separate operator CLI approval surface.

Hard policy denials remain outside the approval lane:

- IT policy denials block immediately
- the denial is appended to the ledger
- org log aggregation can surface or alert on those denials to SOC or admin teams
- admins are not expected to manually approve routine engineer tool asks

Hook audit detail may be reduced, but not disabled.
The conservative control is `logging.audit_detail.hook_projection_level`:

- `full` keeps redacted previews for shell commands and web requests where applicable
- `minimal` drops those previews to reduce noise

Both modes still persist the must-have evidence:

- tool name and target kind
- request fingerprint or content hash
- file target or request host where applicable
- approval state and denial reason
- Claude session linkage and local actor/session hint

For a stricter class of operations, Loopgate can eventually require a stronger
approval surface than Claude's inline prompt.
The realistic bridge for interactive Claude Code is:

1. `PreToolUse` creates a Loopgate-tracked pending approval
2. `PermissionRequest` matches that pending approval by request fingerprint
3. a Loopgate-owned approval surface may approve or deny it
4. the `PermissionRequest` hook may then allow or deny on behalf of the user

Important limitation:
interactive Claude Code does not provide a clean "pause and wait forever on an
external window" primitive. The non-interactive `defer` path is SDK-oriented.
So any stronger approval surface has to be designed around the actual hook
contract rather than assumed as a transparent replacement for the inline Claude
prompt.

## 5. Out of scope for this MVP

Out of scope:

- building a second primary operator UI inside this repo
- turning Loopgate into a general-purpose agent harness
- background automation systems that bypass explicit governed requests
- governance behavior that depends on prompt-time memory injection

## 6. Naming note

Some runtime identifiers still carry historical names. Treat them as cleanup
debt, not as product surface.
