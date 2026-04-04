**Last updated:** 2026-04-03

# Loopgate MCP server (`loopgate mcp-serve`)

The **Model Context Protocol** (stdio JSON-RPC) lets IDEs (**Claude Code**, **Cursor**, **VS Code**, **Google Anti‑Gravity**, **OpenAI Codex**, and other MCP hosts) call tools implemented by a subprocess. Loopgate exposes a minimal MCP server that **forwards** tool calls to a **running** Loopgate daemon over the **local Unix socket** — the same HTTP API and AMP-style session integrity as HTTP-native clients (see [RFC 0001](../rfcs/0001-loopgate-token-policy.md), [AMP](../AMP/README.md)).

**Product note:** MCP is the **primary** developer integration path. Any HTTP-native client (custom app, test harness, or the in-repo Wails reference) can open a control session and export credentials using the same contract.

## Prerequisites

1. **Loopgate daemon** listening (default: `loopgate` with `runtime/state/loopgate.sock`, or `LOOPGATE_SOCKET`).
2. A **valid open session**: capability token, approval token, session MAC key, and expiry — normally obtained from `POST /v1/session/open` (or delegated session export from an existing client).

## Local / dev IDE mode

`loopgate mcp-serve -local-open-session ...` is a **local/dev convenience mode only**.

- It is intended for a local IDE such as **Claude Code** or **Cursor** running on the **same machine** as Loopgate.
- It does **not** introduce a new auth model.
- It does **not** create a remote bootstrap path.
- It still opens a normal Loopgate control session over the **local Unix socket** using the existing request-signing and policy flow.

Use this mode only for local IDE integration. Do not treat it as a production or remote deployment pattern.

Example:

```bash
loopgate mcp-serve \
  -local-open-session \
  -actor claude_code \
  -client-session cursor_demo \
  -requested-capabilities loopgate.status,memory.remember,memory.discover
```

## Environment variables

| Variable | Required | Description |
|----------|----------|-------------|
| `MORPH_REPO_ROOT` | No | Repo root (default: cwd). Used for default socket path. |
| `LOOPGATE_SOCKET` | No | Override Unix socket path. |
| `LOOPGATE_MCP_CONTROL_SESSION_ID` | Yes | Control session id (must match the minted session). |
| `LOOPGATE_MCP_CAPABILITY_TOKEN` | Yes | Bearer capability token. |
| `LOOPGATE_MCP_APPROVAL_TOKEN` | Yes | Approval token from session open. |
| `LOOPGATE_MCP_SESSION_MAC_KEY` | Yes | Hex session MAC key for signed POST bodies. |
| `LOOPGATE_MCP_EXPIRES_AT` | Yes | Token expiry, RFC3339 or RFC3339Nano. |
| `LOOPGATE_MCP_ACTOR` | No | Client actor label for signing (default: `mcp`). **Effective actor is still the token’s session actor** — use a session opened with the actor/capabilities you need. |
| `LOOPGATE_MCP_CLIENT_SESSION` | No | Client session label for signing (default: `mcp-stdio`). |
| `LOOPGATE_MCP_TENANT_ID` | No | Copied into MCP diagnostic context; use same values as the control session / `config/runtime.yaml` tenancy when set. **Empty** for personal / default deployment (matches `docs/setup/TENANCY.md`). |
| `LOOPGATE_MCP_USER_ID` | No | Same as tenant id row — optional; empty in personal mode. |

Treat these values as **secrets**; do not commit them to `.mcp.json`. Prefer a wrapper script or OS keychain-fed env.

## Tools

| Tool | Purpose |
|------|---------|
| `loopgate.status` | Same inventory as `GET /v1/status`. |
| `loopgate.memory_wake_state` | Loads the current wake-state summary via `GET /v1/memory/wake-state`. |
| `loopgate.memory_discover` | Typed memory discovery wrapper over `POST /v1/memory/discover`. |
| `loopgate.memory_remember` | Typed explicit-memory write wrapper over `POST /v1/memory/remember`. |
| `<Capability Name>` | Each allowed Loopgate capability (for example `fs_list`, `memory.remember`) is also registered dynamically as a native MCP tool mapped to `POST /v1/capabilities/execute`. This remains the fallback surface when a typed wrapper does not exist yet. |

## Example IDE config shape (illustrative)

Exact schema depends on the IDE. The **command** is the `loopgate` binary with first argument `mcp-serve`; **env** carries delegated credentials (injected by your launcher, not checked into git).

## Limitations (v0)

- Requires a **separate** long-running `loopgate` process; MCP does not start the control plane.
- Stdout is reserved for MCP; errors use stderr.
- `-local-open-session` is for **local/dev IDE integration only**, not a general auth surface.
