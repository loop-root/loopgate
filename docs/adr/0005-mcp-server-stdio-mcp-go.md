**Date:** 2026-04-01  
**Status:** superseded by [0010](0010-macos-supported-target-and-mcp-removal.md) — the in-tree MCP server was **removed** (smaller attack surface). A future MCP-shaped surface is **reserved** for a new ADR only, as a thin forwarder to the HTTP API.

## Context + decision (historical — implementation since removed)

Phase 2 originally added **`loopgate mcp-serve`**: a **stdio MCP** server using **`github.com/mark3labs/mcp-go`**, forwarding tool calls to an **already-running** Loopgate over **HTTP on the Unix socket** via **`loopgate.Client`** and **delegated session** env vars (`LOOPGATE_MCP_*`). This avoids two writers to `runtime/state` and reuses the same capability execution path as HTTP.

## Tradeoff

Operators must **bootstrap credentials** (tokens + MAC key + expiry) into the MCP process environment; we do not yet ship a turnkey “IDE logs in” flow. Tool coverage starts narrow (`status`, generic `execute_capability`).

## Escape hatch

Swap the MCP library while keeping the same **tool semantics** and HTTP forwarding, or replace env bootstrap with a **short-lived credential file** or socket-based session helper without changing Loopgate’s core handlers.
