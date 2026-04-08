# ADR 0010: macOS as supported target; remove in-repo MCP server

**Date:** 2026-04-07  
**Status:** accepted

## Context + decision

Loopgate v1 is shipped and operated as a **macOS-first** local control plane (Unix socket + `LOCAL_PEERCRED`). The stdio **MCP** bridge (`loopgate mcp-serve`, package `internal/loopgate/mcpserve`) added supply-chain surface (`mcp-go`) and duplicated the authority story already enforced over HTTP on the UDS. We **remove the MCP server from this repository** and document **macOS as the only supported production OS**; non-macOS execution requires an explicit `LOOPGATE_ALLOW_NON_DARWIN=1` escape hatch for development and CI.

## Deprecation stance (MCP)

**In-tree MCP is deprecated and removed.** That server was an additional **attack and integration surface**: extra dependencies, stdio subprocess bootstrap, delegated-session env paths, and protocol parsing alongside the authoritative **HTTP-on-UDS** control plane. Dropping it **does not** weaken enforcement — privileged work still goes only through the existing HTTP handlers, session binding, signing, and audit paths (RFC 0001).

**Reserved for potential later resurfacing:** A **future ADR** may reintroduce MCP (or another IDE protocol) **only** as a **thin forwarder** to the same HTTP API, with the same invariants as today: **no trust bypass**, same policy evaluation, approvals, and audit as every other client (`AGENTS.md`).

## Tradeoff

Operators lose a bundled MCP stdio adapter; integrations must use the **local HTTP API on the Unix socket** (or an external adapter maintained outside this repo). Linux and other OS users are **unsupported** as production targets until peer-credential and platform stories are revisited.

## Escape hatch

- **Development / CI:** set `LOOPGATE_ALLOW_NON_DARWIN=1` to start the daemon or run tests on non-Darwin hosts.
- **Future MCP:** only via a new ADR, as above.

## Supersedes

- **ADR 0005** (MCP stdio + mcp-go) — superseded by this ADR for the in-tree implementation (removed).
