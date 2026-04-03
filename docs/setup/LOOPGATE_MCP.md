**Last updated:** 2026-04-03

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
| `LOOPGATE_MCP_TENANT_ID` | No | Copied into MCP diagnostic context; use same values as the control session / `config/runtime.yaml` tenancy when set. **Empty** for personal / default deployment (matches `docs/setup/TENANCY.md`). |
| `LOOPGATE_MCP_USER_ID` | No | Same as tenant id row — optional; empty in personal mode. |

Treat these values as **secrets**; do not commit them to `.mcp.json`. Prefer a wrapper script or OS keychain-fed env.

## Tools (Dynamic)

| Tool | Purpose |
|------|---------|
| `loopgate.status` | Same inventory as `GET /v1/status`. |
| `<Capability Name>` | Each allowed Loopgate capability (e.g., `fs_list`, `memory.remember`) is automatically registered as a native MCP tool, mapped dynamically to `POST /v1/capabilities/execute`. |

## Example IDE config shape (illustrative)

Exact schema depends on the IDE. The **command** is the `loopgate` binary with first argument `mcp-serve`; **env** carries delegated credentials (injected by your launcher, not checked into git).

## Limitations (v0)

- Requires a **separate** long-running `loopgate` process; MCP does not start the control plane.
- Stdout is reserved for MCP; errors use stderr.
