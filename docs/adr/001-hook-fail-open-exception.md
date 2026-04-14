**Last updated:** 2026-04-12

# ADR 001: Historical Hook Fail-Open Exception

**Status:** superseded
**Context:** Claude Code hook integration (v0)

---

## Decision

This ADR is now historical.

It recorded an earlier phase where the Claude Code PreToolUse hook was treated
as a convenience overlay and could allow through when Loopgate was unreachable.

That is **not** the current repo-default posture anymore.

For the current MVP direction, where **Claude Code hooks are the active
operator harness**, the repo-default hook integration should **fail closed**.
Any fail-open behavior must be an explicit operator override.

---

## Context

Loopgate's core invariant is **fail-closed**: when policy evaluation, audit
append, or integrity checks fail, the system must deny rather than allow.

This ADR documented a narrow, explicitly scoped exception to that invariant
for the earliest Claude Code PreToolUse hook experiments.

---

## Why fail-open is acceptable here

### 1. Why the exception existed

The hook is a convenience governance layer over an existing tool (Claude Code).
Claude Code is not a hostile runtime — it is an operator-controlled tool
running as the same user, on the same machine, for the same person who started
Loopgate.

In that earlier phase, the hook added governance where none existed before.
If Loopgate was unreachable, the pre-hook state was restored: Claude Code
operated exactly as it did without the hook.

### 2. Why the exception no longer fits the repo default

The Loopgate server is started manually or via a launch script. If it is not
running, the most likely explanation is:

- The operator has not started Loopgate yet in this session.
- Loopgate crashed and has not been restarted.
- The operator is developing or testing without the control plane running.

Those assumptions stop being good defaults once the hook path becomes the
primary operator harness we are asking people to use.

If the product story is "use Claude Code under Loopgate governance," a default
allow on unreachability weakens the very boundary we are trying to ship.

---

## Trigger that superseded this ADR

This exception had to be revisited if:

1. The hook becomes the **primary** governance layer for Claude Code (i.e., no
   other mechanism limits what Claude Code can do, and the hook is the only
   control).
2. The hook is deployed in an environment where "operator did not start
   Loopgate" is not a valid expected state (e.g., managed enterprise deployments
   where Loopgate is guaranteed to be up).
3. The hook is extended to govern actions that have non-reversible side effects
   on shared infrastructure (external API calls, production deployments, etc.)
   where fail-open would be genuinely dangerous.

The first trigger now applies to the repository MVP direction.

---

## Current guidance

- Repo-default Claude hook behavior should fail closed.
- Operators who want fail-open must opt into it explicitly.
- Primary governance should use a **command hook** talking to Loopgate over the
  Unix socket, not a raw Claude HTTP hook, because Claude HTTP hook transport
  failures are non-blocking.

---

## Alternatives considered

**Always fail-closed:** This is now the correct default for the repo MVP, even
though it was not the preferred default during the earliest optional-overlay
phase.

**Timeout with retry:** Adds latency on every tool call when Loopgate is
down. Degrades Claude Code responsiveness. Unreachability is typically
persistent (Loopgate not started), not transient — retrying does not help.

**Subprocess restart check:** Check whether Loopgate is intentionally
stopped before deciding. Too complex, relies on process state that may not
be accessible in all environments.

---

## Related

- `internal/loopgate/server_hook_handlers.go` — handler implementation
- `.claude/hooks/loopgate_pretool.py` — hook script
- `.claude/settings.json` — hook registration
- `docs/design_overview/claude_code_hooks_mvp.md` — current MVP direction
