# ADR 0015: MCP Library Choice and Architectural Escape Hatch

**Status:** Superseded — in-tree MCP server **removed** (deprecated — **attack surface reduced**); see `docs/adr/0010-macos-supported-target-and-mcp-removal.md`. **Reserved:** a future ADR may reintroduce MCP only as a **thin HTTP forwarder** with identical policy/audit invariants.  
**Date:** 2026-04-03

## Context (historical)
As part of the Loopgate enterprise pivot (Phase 2), we planned to integrate the Model Context Protocol (MCP) in-tree. The objective is to expose governed Loopgate capabilities to connected developer tools (like Claude Code, Cursor, VS Code) without compromising our strict policy and audit boundaries.

Loopgate is fundamentally a Go application. We need an MCP implementation that supports stdio transport (and eventually HTTP/SSE) while integrating seamlessly with our existing control plane.

## Decision
We will use the [`mark3labs/mcp-go`](https://github.com/mark3labs/mcp-go) library for our initial MCP server integration.

### Why this library?
1. **Language alignment**: It is native Go, meaning we don't need a sidecar process just to speak the MCP protocol, minimizing attack surface.
2. **Minimal abstractions**: The `mcp.Tool` and `server.ToolHandlerFunc` definitions easily map to our existing `CapabilitySummary` and `lgClient.ExecuteCapability` semantics.
3. **Transport support**: It supports stdio out of the box, which solves our immediate Phase 2 goals of subprocess integration.

### Security and Boundary Guardrails
- **No authority via MCP:** The MCP server is purely a proxy. `handleTypedCapabilityTool` translates an MCP tool call to an `lgClient.ExecuteCapability` call. The actual enforcement, denial logic, and ledger appending remain strictly inside the Loopgate daemon over UDS.
- **Dynamic mapping:** Instead of building a parallel permission model inside the MCP layer, the MCP server automatically probes `/v1/status` to determine which capabilities to present to the user. 
- **Tenancy:** Environment variables (`LOOPGATE_MCP_TENANT_ID`, `LOOPGATE_MCP_USER_ID`) configure the local `loopdiag` integration natively to ensure all MCP-side diagnostics correctly carry the tenant identifier.

### Escape Hatch
If `mcp-go` becomes unmaintained or limits our capabilities (e.g., if we require complex streaming mechanisms or native resources endpoints that don't fit its interfaces), our escape hatch is trivial:
Because the handler logic is incredibly thin (as seen in `mcpserve/server.go`), we can quickly wrap the standard `mcp.io` protocol definition ourselves using Go's `encoding/json` and standard I/O streams without impacting any underlying Loopgate execution patterns.

## Consequences
- **Positive:** Phase 2 delivery is expedited without sacrificing invariants. Capabilities natively translate to MCP Tool prompts, dramatically increasing integration ease for end-users.
- **Negative/Risk:** If the library parses untrusted JSON poorly, it could lead to crashes. **Mitigation:** We wrap the handler processing in strict validations before `executeCapabilityWithArgs` triggers, mitigating injection into the Loopgate core.
