# Claude Hooks Map

This file maps `claude/`, the checked-in Claude Code hook bundle that routes
Claude hook events through Loopgate.

Use it when changing:

- hook script install layout
- supported Claude Code hook events
- hook request filtering
- hook output translation for Claude Code
- fail-closed behavior when Loopgate is unavailable

## Core Role

`claude/` contains the Python hook scripts that are copied into an operator's
Claude config by `loopgate install-hooks` or `loopgate setup`.

The hook bundle exists to make Claude Code ask Loopgate before tool use and to
send lifecycle/audit context into the local control plane. The hooks are a
client surface, not an authority source.

## Key Files

- `hooks/scripts/loopgate_hook_common.py`
  - JSON input loading and field allowlist
  - Loopgate socket resolution
  - HTTP-over-Unix-socket hook request
  - Loopgate `allow` / `ask` / `block` translation to Claude hook output
  - fail-closed handling via exit code `2`

- Event wrappers:
  - `loopgate_pretool.py`
  - `loopgate_permissionrequest.py`
  - `loopgate_posttool.py`
  - `loopgate_posttoolfailure.py`
  - `loopgate_userpromptsubmit.py`
  - `loopgate_sessionstart.py`
  - `loopgate_sessionend.py`

## Relationship Notes

- Installer and settings mutation live in `cmd/loopgate/hooks.go`.
- Hook server handling lives in `internal/loopgate/server_hook_handlers.go`.
- Hook policy evaluation lives in `internal/loopgate/claude_code_hook_policy.go`.
- Hook audit projection lives in `internal/loopgate/hook_audit_projection.go`.

## Important Watchouts

- Hooks must fail closed when Loopgate cannot be reached or returns invalid
  output.
- Hook input is untrusted. Keep request fields allowlisted and bounded.
- Do not add authority to the Python scripts; policy and approvals stay in
  Loopgate.
- Keep Claude hook output compatible with Claude Code's documented hook event
  semantics.
