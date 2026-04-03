**Last updated:** 2026-04-01

# Loopgate MCP server (`loopgate mcp-serve`)

The **Model Context Protocol** (stdio JSON-RPC) lets IDEs (Cursor, Claude Code, etc.) call tools implemented by a subprocess. Loopgate exposes a minimal MCP server that **forwards** tool calls to a **running** Loopgate daemon over the **local Unix socket** — the same HTTP API and AMP-style session integrity as native clients (see [RFC 0001](../rfcs/0001-loopgate-token-policy.md), [AMP](../AMP/README.md)).

**Product note:** Any client (Swift Haven, a future SDK, or a custom “Haven-shaped” app) can open a control session and export credentials; MCP is one consumer of that contract.

## Prerequisites

1. **Loopgate daemon** listening (default: `loopgate` with `runtime/state/loopgate.sock`, or `LOOPGATE_SOCKET`).
2. A **valid open session**: capability token, approval token, session MAC key, and expiry — normally obtained from `POST /v1/session/open` (or delegated session export from an existing client).

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

Treat these values as **secrets**; do not commit them to `.mcp.json`. Prefer a wrapper script or OS keychain-fed env.

## Tools (initial)

| Tool | Purpose |
|------|---------|
| `loopgate.status` | Same inventory as `GET /v1/status`. |
| `loopgate.execute_capability` | `capability` + optional `arguments_json` (JSON object of string keys/values) → same path as `POST /v1/capabilities/execute`. |

## Example IDE config shape (illustrative)

Exact schema depends on the IDE. The **command** is the `loopgate` binary with first argument `mcp-serve`; **env** carries delegated credentials (injected by your launcher, not checked into git).

## Limitations (v0)

- No dedicated `memory.remember` tool name yet — use `loopgate.execute_capability` with `capability` set to `memory.remember` and appropriate `arguments_json`.
- Requires a **separate** long-running `loopgate` process; MCP does not start the control plane.
- Stdout is reserved for MCP; errors use stderr.
