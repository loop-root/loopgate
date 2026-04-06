**Last updated:** 2026-04-06

# ADR 001: Hook Fail-Open Exception

**Status:** accepted
**Context:** Claude Code hook integration (v0)

---

## Decision

The `loopgate_pretool.py` PreToolUse hook exits 0 (allow) when Loopgate is
unreachable, rather than exiting 2 (block).

Operators who want fail-closed behavior can set:

```
LOOPGATE_HOOK_FAIL_CLOSED=true
```

---

## Context

Loopgate's core invariant is **fail-closed**: when policy evaluation, audit
append, or integrity checks fail, the system must deny rather than allow.

This ADR documents a narrow, explicitly scoped exception to that invariant
for the Claude Code PreToolUse hook integration.

---

## Why fail-open is acceptable here

### 1. Defense-in-depth, not primary enforcement

The hook is a convenience governance layer over an existing tool (Claude Code).
Claude Code is not a hostile runtime — it is an operator-controlled tool
running as the same user, on the same machine, for the same person who started
Loopgate.

The hook adds governance where none existed before. If Loopgate is unreachable,
the pre-hook state is restored: Claude Code operates exactly as it did without
the hook. No new attack surface is opened.

### 2. Loopgate is a local process started by the same user

The Loopgate server is started manually or via a launch script. If it is not
running, the most likely explanation is:

- The operator has not started Loopgate yet in this session.
- Loopgate crashed and has not been restarted.
- The operator is developing or testing without the control plane running.

Blocking Claude Code entirely when Loopgate is not up would make the tool
unusable during normal development workflows. This is the wrong tradeoff for
a v0 integration.

### 3. The threat model difference

Loopgate's fail-closed invariant is designed for cases where the control plane
IS the primary enforcement layer — i.e., a morphling worker that has no other
way to act. In that context, allowing through on Loopgate failure means the
action executes without governance.

The hook path is different: Loopgate is an overlay on top of Claude Code's
native execution model. If the overlay is absent, Claude Code still runs.
The invariant applies to Loopgate-native surfaces, not to this overlay.

---

## When this exception becomes unacceptable

This exception MUST be revisited — and the default MUST flip to fail-closed — if:

1. The hook becomes the **primary** governance layer for Claude Code (i.e., no
   other mechanism limits what Claude Code can do, and the hook is the only
   control).
2. The hook is deployed in an environment where "operator did not start
   Loopgate" is not a valid expected state (e.g., managed enterprise deployments
   where Loopgate is guaranteed to be up).
3. The hook is extended to govern actions that have non-reversible side effects
   on shared infrastructure (external API calls, production deployments, etc.)
   where fail-open would be genuinely dangerous.

---

## Migration path

Operators can opt into fail-closed today:

```sh
export LOOPGATE_HOOK_FAIL_CLOSED=true
```

When the conditions above apply, the default will flip:
- `LOOPGATE_HOOK_FAIL_CLOSED` will default to `true`
- Operators who want fail-open must explicitly set `LOOPGATE_HOOK_FAIL_OPEN=true`

---

## Alternatives considered

**Always fail-closed:** Breaks Claude Code when Loopgate is not running.
Unacceptable for the v0 workflow where Loopgate is optional and started
manually. Would cause operator friction that actively discourages adoption.

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
- `docs/design_overview/looproot_v0.md` — LoopRoot design context
