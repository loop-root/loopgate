# MCP integration (deprecated in-tree)

**Status:** **Deprecated and removed** from this repository. This page documents **what changed**, **why** (attack surface), and **what is reserved** for a possible future design.

The **`loopgate mcp-serve`** stdio MCP server and package `internal/loopgate/mcpserve` were **deleted** (see [`docs/adr/0010-macos-supported-target-and-mcp-removal.md`](../adr/0010-macos-supported-target-and-mcp-removal.md)).

## Why it was removed

The in-tree MCP server added **supply-chain and protocol surface** (e.g. `mcp-go`), **stdio subprocess bootstrap**, and **parallel session/credential paths** next to the authoritative **HTTP-on-Unix-socket** control plane. Removing it **does not** relax policy: it eliminates a whole class of integration bugs and trust-boundary confusion while keeping a **single** wire path for privileged calls.

## Reserved for later (explicit ADR only)

**MCP is not permanently rejected as a product idea.** A **future ADR** may reintroduce MCP (or another IDE protocol) **only** as a **thin forwarder** to the existing HTTP API, with the same invariants as all other clients: same policy evaluation, approvals, audit, and signing — **never** a bypass. Until then, treat MCP as **out of scope for in-tree shipping**.

## What to use instead

Integrations should attach to Loopgate over **HTTP on the local Unix socket** using the normal **session open** and **signed request** flow (`/v1/session/open`, `/v1/capabilities/execute`, etc.), identical to other local control-plane clients. See [LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md](./LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md).

If you need an MCP-shaped adapter for an IDE host today, treat it as an **out-of-tree forwarder** that speaks MCP on one side and Loopgate HTTP on the other, without weakening policy, audit, or signing requirements.
