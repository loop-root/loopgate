# Loopgate Map

This file maps the main Loopgate package files. **Loopgate** is the authority and enforcement runtime in this repository. **Operator clients** attach over **HTTP on the local control-plane socket** (and may use **out-of-tree** MCP→HTTP forwarders). **In-tree MCP is deprecated and removed** (ADR 0010 — reduced attack surface; **reserved** for a possible future thin forwarder via new ADR). Any remaining legacy client shells in the repo are deletion candidates, not active product surfaces.

Use it when changing:

- capability inventory and status surfaces
- explicit memory persistence
- wake-state and memory diagnostics
- capability-specific execution paths
- **local HTTP client** surfaces (`internal/loopgate/client.go`, neutral `/v1/...` routes)
- task execution classes and standing approvals
- mirrored host-folder grants and sync behavior
- the next permissioned host plan/apply model for real user-folder actions
- continuity-backed task and approval substrates (Loopgate-owned, not UI-owned)

## Core Role

`internal/loopgate/` is the control plane and authority boundary.

**Planning:** phased roadmap in repo-root `sprints/` (latest dated `*.md`); durable decisions in `docs/adr/`. Update this map when you add or rename primary files here.

For integrators it matters in four ways:

- it defines which capabilities actually exist
- it exposes the memory APIs **local clients** must call (not optional side channels)
- it produces the status data **Security / Activity / continuity UIs** render from projections
- it owns the authoritative bridge between explicit host-folder grants and **client-visible** mirrored folders
- it centralizes tasks, approvals, memory, and host-action state so **every client** (IDE, CLI, TUI, or other local client) shares one auditable substrate

## Key Files

### Authority and Capability Inventory

- `server.go`
  - server construction
  - tool registry wiring
  - capability summaries derived from the registry
  - dispatch point for capability-specific execution paths such as `memory.remember`
  - now also contains some legacy actor-scoped branches that should continue shrinking rather than becoming product surface
  - handler panics and operator-relevant errors should log via the diagnostic **`slog`** loggers (`internal/loopdiag`, levels from `config/runtime.yaml` → `logging.diagnostic`) with **`tenant_id` / `user_id`** on the log record when a control session is bound, so admins can troubleshoot without a debugger and filter by tenant in multi-tenant deployments
- `folder_access.go`
  - authoritative folder-grant storage
  - compare-before-sync mirror logic
  - current place where host-folder changes become **audited, client-visible** updates
  - likely starting point for the future granted-folder resource model that separates read, plan, and apply scopes
- `memory_capability.go`
  - authoritative execution for `memory.remember`
  - bridges native tool calls onto the explicit remember-memory API
- `todo_contract.go`
  - legacy explicit-task continuity constants retained only so older persisted continuity records still parse deterministically after task-board retirement
- `todo_legacy_helpers.go`
  - legacy read-only task/goal continuity helpers retained for replay and memory-state loading
  - does not expose a live task-board API, standing-grant control surface, or `todo.*` capability execution path
- `task_standing_grants.go`
  - reduced to legacy task execution-class metadata helpers needed for continuity replay
  - no longer exposes a standing-grant API or persisted operator toggle state
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
- `types_memory.go`
  - continuity inspection, wake-state, memory lookup/recall/artifact, and legacy todo replay wire contracts plus request validation helpers
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
- `server_memory_handlers.go` — memory endpoints (see Memory section below)
- `server_capability_handlers.go` — capability execution
- `server_model_handlers.go` — model connection APIs; **session open** stamps `TenantID` / `UserID` from `config/runtime.yaml` → `tenancy` (see `docs/setup/TENANCY.md`, ADR 0004)
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
  - now also includes standing task grant summaries for the Security room
- `client.go`
  - public Go client surface core (`Client`, constructors, model/connections/site wrappers) over the Unix socket — **wire reference** for non-Go integrators; see `docs/setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md`
- `client_session.go`
  - control-session bootstrap, delegated-session refresh state, approval-token flows, and capability-execution wrappers
- `client_memory.go`
  - continuity, wake, and memory governance wrappers used by local clients and test harnesses
- `client_sandbox.go`
  - sandbox import/export/list/metadata wrappers over the signed local control plane
- `client_transport.go`
  - signed HTTP transport, retry-on-token-refresh behavior, and request-signature helpers
- `configured_capability_runtime.go`
  - configured remote capability execution, access-token issuance/cache, provenance metadata, and registry registration
- `configured_capability_extract.go`
  - configured response extraction, HTML/Markdown/JSON selectors, and result-field classification helpers

### Memory and Continuity

- `memory_partition.go`
  - **`memoryBasePath`** + per-tenant **`memoryPartitions`** map: each tenant gets its own continuity root under `memory/partitions/<key>/` (`default` for empty deployment tenant; hashed directory name for non-empty — no raw tenant string in paths)
  - **`maybeMigrateMemoryToPartitionedLayout`**: one-time move of legacy top-level artifacts into `partitions/default/` when `partitions/` is absent; idempotent when already migrated
  - **`ensureMemoryPartitionLocked`**, wake rebuild and SQLite sync iterate partitions so each namespace stays consistent with its own authoritative JSON/JSONL
- `memory_tcl.go`
  - bridges explicit remember requests into `internal/tcl` through `buildValidatedMemoryRememberCandidate`
  - legacy request parsing now stops at a `tcl.ValidatedMemoryCandidate`; persistence and benchmark governance consume that contract instead of loose analysis fields
  - explicit-memory denials for unsupported key families are audited with stable `memory_candidate_invalid` fields instead of falling through as silent persistence misses
  - current preference supersession still depends on a narrow secondary fallback facet table for `preference.stated_preference`; see ADR 0007
  - explicit profile/settings writes now feed `profile.timezone` and `profile.locale` through the same validated contract instead of synthetic retrieval-only anchors
- `memory_backend_continuity_tcl_candidate.go`
  - continuity backend now owns the explicit remember candidate-builder seam, so targeted TCL failure injection no longer depends on a `Server` hook
  - explicit remember normalization, denial audit, and continuity fact candidate analysis live together on the backend side of the memory authority boundary

- `memory_conflict_anchor_test.go`
  - Phase 1 anchor persistence and fail-closed TCL failures: `TestRememberMemoryFact_SupersedesOnlyWhenAnchorTupleMatches`, `TestRememberMemoryFact_CoexistsWhenTCLReturnsNoAnchor`, `TestRememberMemoryFact_FailsClosedWhenTCLValidationFails`
  - additional wake/replay coverage for anchor tuples lives in `continuity_memory_test.go` (see implementation plan Task 4 test names)

- `server_memory_handlers.go`
  - memory endpoints, including explicit remember and diagnostic wake
- `continuity_memory_access.go`
  - continuity request normalization, inspect/discover/recall adapters, and backend lookup
- `continuity_memory_mutation.go`
  - continuity mutation ordering and explicit memory fact write-budget enforcement
- `continuity_memory_records.go`
  - continuity inspect / lineage record types, schema constants, and distillate-record JSON compatibility helpers
- `continuity_memory_wake.go`
  - core Loopgate wake-state builder and diagnostic-wake assembly
- `continuity_memory_wake_selection.go`
  - wake fact-candidate precedence, anchor tuple resolution, and authoritative-vs-derived state classification
- `continuity_memory_wake_projection.go`
  - wake-state projection helpers and token-budget trimming
- `continuity_mutation_ordering_test.go`
  - `TestMutateContinuityMemory_*`, `TestContinuityInspectRequest_*`, corrupt-replay coverage per Phase 1 Tasks 1 and 5
- `continuity_runtime_contract.go`
  - continuity runtime constants, replay/event contract structs, and partition artifact-path layout
- `continuity_runtime.go`
  - continuity runtime derivation helpers: goal normalization, resolved profile snapshots, ranking cache, and task/review snapshot reconstruction
  - important when new continuity event payloads or projection fields are introduced
- `continuity_runtime_storage.go`
  - continuity artifact persistence, JSONL append/replay, and derived snapshot materialization
  - the fail-closed path for replaying authoritative continuity events back into memory state

- `memory_backend.go` / `memory_backend_continuity_tcl.go`
  - swappable backend boundary
  - current `continuity_tcl` wrapper
  - projected-node discovery path used by the benchmark harness

- `memory_sqlite_store.go`
  - derived SQLite store open/init path and shared schema/types
- `memory_sqlite_store_benchmark.go`
  - benchmark and fixture seeding path for projected-node SQLite tests and memorybench scenarios
- `memory_sqlite_store_projection.go`
  - authoritative projected-node sync from continuity state into SQLite-backed search classes
- `memory_sqlite_store_search.go`
  - projected-node list/search/debug logic, including slot-preference ranking for exact state queries

- `memorybench_bridge.go`
  - narrow internal bridge that lets `cmd/memorybench` read the `continuity_tcl`
    projected discovery surface without widening Loopgate’s public API
  - projected-node backend openers plus production-parity seeding helpers for benchmark scenarios
- `memorybench_bridge_control_plane.go`
  - product-valid control-plane scenario seeding and discover/recall replay for benchmark parity runs
- `memorybench_bridge_candidate.go`
  - benchmark-only memory candidate governance adapter that reuses the production TCL validation path without pretending benchmark inputs are product API shapes

## Current Sprint Focus

The current working set in this directory is:

- `client.go`
- `server_memory_handlers.go`
- `continuity_memory.go`
- `folder_access.go`
- `memory_capability.go`
- `todo_execution.go`
- `todo_mutation.go`
- `goal_mutation.go`
- `server.go`
- `server_connection_handlers.go`
- `ui_types.go`

These files matter because:

- **Clients** must not depend on vague memory claims; Loopgate exposes explicit APIs and diagnostics
- explicit "remember this" should route through `RememberMemoryFact`
- explicit Todo mutations should route through the same continuity authority model instead of a **client-local** parallel store
- open tasks should be durable operational objects, not only UI strings
- native `memory.remember` tool calls should not bypass the explicit remember pipeline
- the capability inventory should stay authoritative even if a **UI** renders friendlier names
  - actor-scoped low-friction execution (including the current `haven` compatibility actor) must stay inside Loopgate policy, not leak into generic evaluation for other actors
- standing approvals are still Loopgate authority, not UI preference state
- host-folder mirroring should stay explicit, audited, and compare-before-sync instead of becoming a noisy or implicit watcher path
- the next host-help slice should build as a plan/apply system on top of Loopgate, not as raw writable host filesystem authority

## Existing Strengths To Reuse

The memory system already has more structure than some **client summaries** currently surface.

Notable existing pieces:

- explicit remembered-fact API
- wake-state generation
- diagnostic wake report with included and excluded entries
- inspection outcomes such as `skipped_under_threshold`

That means the first diagnostics pass should expose existing truth before inventing a new algorithm.

## Important Watchouts

- Do not weaken Loopgate's internal identifiers just to get friendlier UI.
- Keep explicit memory writes deterministic and auditable.
- If a capability appears in a **client**, it should still come from Loopgate's registered tool inventory.
- Keep explicit memory denials and diagnostic state visible. Silent success-looking memory failures will make the **operator experience** feel unreliable very quickly.
- Compare-before-sync exists to preserve observability without flooding the audit trail. Do not turn routine folder polling into fake security activity.
- Keep retrieval tuning bounded and inspectable. Do not turn discover-memory ranking into a hidden heuristic stack that can bypass current eligibility rules.
- Benchmark bridges must stay read-only and internal. Do not turn them into a public convenience API.
