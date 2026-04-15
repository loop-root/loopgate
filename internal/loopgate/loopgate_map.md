# Loopgate Map

This file maps the main Loopgate package files. **Loopgate** is the authority and enforcement runtime in this repository. **Operator clients** attach over **HTTP on the local control-plane socket** (and may use **out-of-tree** MCP→HTTP forwarders). **In-tree MCP is deprecated and removed** (ADR 0010 — reduced attack surface; **reserved** for a possible future thin forwarder via new ADR). Any remaining legacy client shells in the repo are deletion candidates, not active product surfaces.

Use it when changing:

- capability inventory and status surfaces
- capability-specific execution paths
- **local HTTP client** surfaces (`internal/loopgate/client.go`, neutral `/v1/...` routes)
- mirrored host-folder grants and sync behavior
- the next permissioned host plan/apply model for real user-folder actions

## Core Role

`internal/loopgate/` is the control plane and authority boundary.

**Planning:** phased roadmap in repo-root `sprints/` (latest dated `*.md`); durable decisions in `docs/adr/`. Update this map when you add or rename primary files here.

For integrators it matters in four ways:

- it defines which capabilities actually exist
- it produces the status data clients render from authoritative projections
- it owns the authoritative bridge between explicit host-folder grants and **client-visible** mirrored folders
- it centralizes approvals, policy, audit, MCP governance, and host-action state so **every client** shares one auditable substrate

## Key Files

### Authority and Capability Inventory

- `server.go`
  - server construction
  - tool registry wiring
  - capability summaries derived from the registry
  - dispatch point for capability-specific execution paths such as host-folder plan/apply helpers
  - now also contains some legacy actor-scoped branches that should continue shrinking rather than becoming product surface
  - handler panics and operator-relevant errors should log via the diagnostic **`slog`** loggers (`internal/loopdiag`, levels from `config/runtime.yaml` → `logging.diagnostic`) with **`tenant_id` / `user_id`** on the log record when a control session is bound, so admins can troubleshoot without a debugger and filter by tenant in multi-tenant deployments
- `folder_access.go`
  - authoritative folder-grant storage
  - compare-before-sync mirror logic
  - current place where host-folder changes become **audited, client-visible** updates
  - likely starting point for the future granted-folder resource model that separates read, plan, and apply scopes
- `server_connection_handlers.go`
  - `/v1/status`
  - current global capability inventory surface
- `server_audit_runtime.go`
  - append-only audit chain state, persisted audit-event recording, and operator diagnostic log helpers
- `server_response_runtime.go`
  - JSON response writing, audit-unavailable responses, and control-plane denial-to-HTTP status mapping
- `approval_flow.go`
  - approval token authentication, approval state transitions, approval manifest verification, and approval metadata / operator-facing reason shaping
- `capability_result_runtime.go`
  - result classification, structured result shaping, and per-field metadata derivation for capability execution and configured remote capabilities
- `capability_execution_runtime.go`
  - capability-risk classification, trusted-Haven session helpers, execution-token derivation, capability request normalization, and capability-set helpers
- `request_body_runtime.go`
  - strict JSON body decode and signed-body verification helpers shared across HTTP handlers
- `types.go`
  - core control-plane request/response structs, including:
    - `CapabilitySummary`
    - `OpenSessionRequest`
    - `CapabilityRequest`
    - `CapabilityResponse`
- `types_connections.go`
  - connection status, PKCE, model-connection store, and site-inspection/trust wire contracts plus validators
- `types_sandbox.go`
  - sandbox import/export/list/metadata wire contracts plus request validation helpers

### Retired Haven surface

The old Haven chat, UI projection, and helper-route implementation files have
been removed from the active package. Remaining Haven-named internals in
Loopgate are cleanup debt, not part of the current product surface.

### Request handlers (split from `server.go`)

Loopgate splits HTTP-style handlers across `server_*_handlers.go` files. Examples:

- `server_sandbox_handlers.go` — sandbox import/export/list/stage; `redactSandboxError` returns stable sentinel strings only (no wrapped host paths in client-visible errors)
- `server_sandbox_handlers_test.go` — `TestRedactSandboxError_DoesNotExposeAbsolutePaths`
- `server_capability_handlers.go` — capability execution
- `server_model_handlers.go` — model connection APIs; **session open** stamps `TenantID` / `UserID` from `config/runtime.yaml` → `tenancy` (reserved deployment fields in the current local-first build; see ADR 0004)
- `server_config_handlers.go` — configuration
- `server_connection_handlers.go` — `/v1/status` and connection surface
- `server_quarantine_handlers.go` — quarantine flows
- `server_host_access_handlers.go` — explicit host-access / folder-grant operations beyond simple mirror

### Local client status and UI projection

- `ui_server.go`
  - UI status and approvals
- `ui_types.go`
  - client-facing UI summaries and event envelopes
  - includes folder-access sync/status response types used by **local HTTP clients**
- `client.go`
  - public Go client surface core (`Client`, constructors, model/connections/site wrappers) over the Unix socket — **wire reference** for non-Go integrators; see `docs/setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md`
- `client_session.go`
  - control-session bootstrap, delegated-session refresh state, approval-token flows, and capability-execution wrappers
- `client_sandbox.go`
  - sandbox import/export/list/metadata wrappers over the signed local control plane
- `client_transport.go`
  - signed HTTP transport, retry-on-token-refresh behavior, and request-signature helpers
- `configured_capability_runtime.go`
  - configured remote capability execution, access-token issuance/cache, provenance metadata, and registry registration
- `configured_capability_extract.go`
  - configured response extraction, HTML/Markdown/JSON selectors, and result-field classification helpers

### Retired In-Tree Memory Layer

The old in-tree continuity and memory subsystem has been removed from the
active Loopgate runtime. Remaining memory or continuity references elsewhere in
the repo should be treated as extraction, archival, or documentation cleanup
debt rather than active operator surface.

## Current Sprint Focus

The current working set in this directory is:

- `client.go`
- `folder_access.go`
- `server.go`
- `server_connection_handlers.go`
- `ui_types.go`

These files matter because:

- **Clients** must not depend on vague product claims; Loopgate exposes the governance surfaces that actually exist
- the capability inventory should stay authoritative even if a **UI** renders friendlier names
  - actor-scoped low-friction execution (including the current `haven` compatibility actor) must stay inside Loopgate policy, not leak into generic evaluation for other actors
- host-folder mirroring should stay explicit, audited, and compare-before-sync instead of becoming a noisy or implicit watcher path
- the next host-help slice should build as a plan/apply system on top of Loopgate, not as raw writable host filesystem authority

## Important Watchouts

- Do not weaken Loopgate's internal identifiers just to get friendlier UI.
- If a capability appears in a **client**, it should still come from Loopgate's registered tool inventory.
- Compare-before-sync exists to preserve observability without flooding the audit trail. Do not turn routine folder polling into fake security activity.
