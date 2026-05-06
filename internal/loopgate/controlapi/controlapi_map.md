# Control API Package Map

This file maps `internal/loopgate/controlapi/`, the local control-plane wire
contract package.

Use it when changing:

- JSON request and response shapes
- denial codes or response status values
- hook validation payloads
- UI/event projection structs
- MCP gateway, sandbox, audit-export, or connection API contracts

## Core Role

`controlapi/` defines typed contracts for Loopgate's HTTP-over-Unix-socket
control plane. It intentionally does not own server state or policy decisions.

The package exists so server code, the Go client, operator CLIs, and tests share
one set of validated wire types without importing the full `Server`.

## Key Files

- `doc.go`
  - package boundary statement

- `core.go`
  - common response statuses
  - stable denial codes
  - health/status/session/capability/approval wire types
  - Claude Code hook request/response shapes
  - result classification metadata

- `ui.go`
  - display-safe UI status, approval summaries, event summaries, and
    operator-mount status shapes

- `connections.go`
  - model/provider connection and OAuth-style connection contract shapes

- `sandbox.go`
  - sandbox import/export/list metadata contracts

- `mcp_gateway.go`
  - governed MCP gateway launch/status/call request and response shapes

- `audit_export.go`
  - audit-export trust preflight operator response shapes

- `validation_test.go`
  - validation coverage for request contracts and safe identifiers

## Relationship Notes

- Server enforcement lives in `internal/loopgate/`.
- Capability request canonicalization lives in `internal/protocol/`.
- Public operator docs for HTTP clients live in
  `docs/setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md`.

## Important Watchouts

- Do not put authority decisions or mutable server state in this package.
- Denial codes are operator-facing and audit-relevant; treat renames as
  compatibility changes.
- Wire types must not include raw secrets, tokens beyond explicit control-plane
  credentials, or unbounded payload previews.
- Adding fields is usually safer than changing meanings of existing fields.
