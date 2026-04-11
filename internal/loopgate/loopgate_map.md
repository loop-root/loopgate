# Loopgate Map

This file maps the main Loopgate package files. **Loopgate** is the authority and enforcement runtime in this repository. **Operator clients** attach over **HTTP on the local control-plane socket** (and may use **out-of-tree** MCP→HTTP forwarders). **In-tree MCP is deprecated and removed** (ADR 0010 — reduced attack surface; **reserved** for a possible future thin forwarder via new ADR). Any remaining legacy client shells in the repo are deletion candidates, not active product surfaces.

Use it when changing:

- capability inventory and status surfaces
- explicit memory persistence
- wake-state and memory diagnostics
- capability-specific execution paths
- **local HTTP client** surfaces (`internal/loopgate/client.go`, legacy `/v1/haven/...` routes where present)
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
- `todo_execution.go`
  - capability-entry wrappers for `todo.add`, `todo.complete`, `todo.list`, `goal.set`, and `goal.close`
  - owns result shaping, audit/error surfacing, and UI tool-event emission for task/goal capability execution
- `todo_contract.go`
  - shared explicit-todo constants, workflow/status vocabulary, and active-item record shape used across mutation, projection, and continuity paths
- `todo_request.go`
  - todo request normalization and workflow/task validation helpers shared by handlers and projections
- `todo_mutation.go`
  - continuity-backed todo mutation path and status updates
- `todo_task_facts.go`
  - explicit todo task-fact construction and semantic projection helpers for continuity records
- `goal_mutation.go`
  - continuity-backed goal open/close mutation path
- `todo_projection.go`
  - continuity-backed task board projection, recent completion shaping, and explicit todo state discovery
- `todo_render.go`
  - bounded todo success text and prompt/JSON rendering helpers for `todo.list`
- `task_standing_grants.go`
  - Loopgate-owned standing-approval catalog for safe **actor-scoped** task execution classes (e.g. low-friction paths gated to the `haven` actor)
  - persists operator-visible “always allowed” class sets and audits changes
- `server_connection_handlers.go`
  - `/v1/status`
  - current global capability inventory surface
- `types.go`
  - core control-plane request/response structs, including:
    - `CapabilitySummary`
    - `OpenSessionRequest`
    - `CapabilityRequest`
    - `CapabilityResponse`
    - sandbox, connection, and site-inspection envelopes
- `types_haven_ui.go`
  - Haven UI, task-board projection, desk-note, journal, working-note, workspace, and presence wire contracts
- `types_memory.go`
  - continuity inspection, wake-state, memory lookup/recall/artifact, and todo wire contracts plus request validation helpers
- `types_morphling.go`
  - morphling lifecycle wire contracts and morphling request validation helpers

### Haven Chat (agentic tool execution)

- `server_haven_chat.go`
  - Haven chat HTTP handler and SSE stream lifecycle
  - now mostly holds only the request lifecycle, stream error handling, and turn completion bookkeeping
- `server_haven_chat_runtime.go`
  - `havenChatRuntime` — internal runtime object that owns the supervised agent loop and explicit runtime dependencies (`policy`, `registry`, capability execution hook)
  - `runToolLoop` — the agent loop; now delegates to `executeToolCallsConcurrent`
- `server_haven_chat_request.go`
  - request-method gate, trusted Haven session enforcement, signed-body verification, and chat request decode
- `server_haven_chat_thread.go`
  - thread bootstrap, workspace binding handoff, and user-message persistence into threadstore
- `server_haven_chat_runtime_setup.go`
  - model/runtime bootstrap: persona, model runtime config, wake summary, attachment shaping, and timeout setup
- `server_haven_chat_tool_setup.go`
  - tool bootstrap for Haven chat: allowed capability filtering, native tool definitions, and runtime-fact assembly inputs
- `server_haven_chat_loop_state.go`
  - per-turn loop state (conversation growth, follow-up nudges, pending approval outcome shaping)
- `server_haven_chat_results.go`
  - tool-result shaping, approval wait UX, prompt-eligible result filtering, and SSE previews
- `server_haven_chat_tool_defs.go`
  - capability-summary filtering and model-facing tool definition shaping
- `server_haven_chat_runtime_facts.go`
  - Haven chat runtime-fact assembly
- `server_haven_chat_context_facts.go`
  - stable session/runtime facts plus project and granted-path context facts
- `server_haven_chat_capability_facts.go`
  - capability-specific guidance facts, grouped by product/tool family
- `server_haven_chat_heuristics.go`
  - host-folder and follow-up intent heuristics
- `server_haven_chat_conversation.go`
  - threadstore conversation reconstruction and model windowing
- `server_haven_chat_transport.go`
  - SSE transport types and emitter
- `server_haven_chat_tools.go`
  - `executeToolCallsConcurrent` — fans out read-only tool calls in parallel (Phase 1: reads), then runs write/execute/unknown calls serially (Phase 2: writes). Each goroutine emits its own `tool_result` SSE event inline as it finishes, giving the operator live feedback.
  - `executeToolCalls` — retained as the simple serial reference implementation; used by the runtime for direct serial dispatch and plan auto-apply
- `server_haven_model_catalog.go`
  - Haven-facing model catalog discovery
- `server_haven_wake_context.go`
  - renders continuity wake state into the compact Haven-facing memory summary used by chat and resident flows
- `workspace_binding.go`
  - authoritative workspace ID derivation from the repo root
- `havenSSEEmitter`
  - now carries a `sync.Mutex` on the struct; `emit()` is goroutine-safe so concurrent read goroutines can stream events without corrupting SSE frames
- `tool_classification.go` (**new**)
  - `capabilityClass` struct: `readOnly bool` — derived from Loopgate's own `OpRead` / `OpWrite` / `OpExecute` taxonomy
  - `classifyCapability(registry, capabilityName)` — pure, fail-closed: unregistered → serial (readOnly=false); OpRead → readOnly=true; OpWrite/OpExecute → readOnly=false
  - Canonical dispatch decision for `havenChatRuntime.executeToolCallsConcurrent`. If you add a new `Op*` constant to `internal/tools/tool.go`, add a corresponding test case here.
- `tool_classification_test.go` (**new**)
  - Unit tests that pin the fail-closed contract and the OpRead / non-OpRead split
  - Also spot-checks real capability names (fs_read, notes.list, notes.write) to guard against unintentional operation-type changes
- `server_haven_chat_concurrent_test.go` (**new**)
  - Integration tests for `havenChatRuntime.executeToolCallsConcurrent`:
    - `TestHavenChat_ConcurrentReadOnlyToolsRunFasterThanSerial` — wall-clock timing proves reads overlap
    - `TestHavenChat_SerialWriteToolsRunInOrder` — start-time tracking proves writes do not overlap
    - `TestHavenChat_ToolResultsRetainInputOrder` — result slice order matches input call order despite parallel execution

### Request handlers (split from `server.go`)

Loopgate splits HTTP-style handlers across `server_*_handlers.go` files. Examples:

- `server_sandbox_handlers.go` — sandbox import/export/list/stage; `redactSandboxError` returns stable sentinel strings only (no wrapped host paths in client-visible errors)
- `server_sandbox_handlers_test.go` — `TestRedactSandboxError_DoesNotExposeAbsolutePaths`
- `server_memory_handlers.go` — memory endpoints (see Memory section below)
- `server_haven_memory_handlers.go` — UI memory inventory/reset handlers; projects redacted manageable objects and owns demo reset paths
- `server_capability_handlers.go` — capability execution
- `morphling_state.go`
  - morphling record schema, validation, signed on-disk persistence, tenant checks, and operator-facing summary projection helpers
- `morphling_transition.go`
  - atomic morphling record transition, rollback, and restore helpers around on-disk state persistence
- `morphling_termination.go`
  - morphling termination, approval expiry, failure cleanup, and restart recovery
- `morphling_spawn.go`
  - morphling spawn admission, approval creation/finalization, and approval resolution
- `morphling_status.go`
  - morphling status projection for the authenticated control session
- `morphling_workers.go`
  - morphling worker IPC, execution updates, and staged artifact handling
- `server_morphling_handlers.go` / `server_morphling_worker_handlers.go` — morphling lifecycle and workers
- `server_model_handlers.go` — model connection APIs; **session open** stamps `TenantID` / `UserID` from `config/runtime.yaml` → `tenancy` (see `docs/setup/TENANCY.md`, ADR 0004)
- `server_config_handlers.go` — configuration
- `server_connection_handlers.go` — `/v1/status` and connection surface
- `server_taskplan_contract.go`
  - task-plan request/response envelopes and audit-unavailable response helpers
- `server_taskplan_handlers.go`
  - task-plan submission, lease issuance, execution, completion, and result handlers
- `server_quarantine_handlers.go` — quarantine flows
- `server_host_access_handlers.go` — explicit host-access / folder-grant operations beyond simple mirror

### Local client status and UI projection

- `ui_server.go`
  - UI status and approvals
- `server_haven_memory_handlers.go`
  - `GET /v1/ui/memory`
  - `POST /v1/ui/memory/reset`
  - display-safe memory inventory for **operator-facing** memory controls
  - auditable archive-and-fresh-start reset for demo prep
- `server_haven_desk_notes.go`
  - desk-note handlers and persistence for the operator desk surface
- `server_haven_presence.go`
  - normalized Haven presence and morph-sleep projections
- `server_haven_journal.go`
  - Haven journal listing, entry loading, and journal preview/title helpers
- `server_haven_working_notes.go`
  - working-note list/load/save handlers and title/preview/path derivation helpers
- `server_haven_workspace.go`
  - workspace listing, host-layout projection, preview handlers, and Haven workspace path mapping
- `server_haven_file_access.go`
  - shared file-read capability bridge for Haven UI projections
- `ui_types.go`
  - client-facing UI summaries and event envelopes
  - includes folder-access sync/status response types used by **local HTTP clients**
  - now also includes standing task grant summaries for the Security room
- `client.go`
  - public Go client surface over the Unix socket (`NewClient` plus endpoint wrappers) — **wire reference** for non-Go integrators; see `docs/setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md`
  - methods used by local clients and test harnesses (not authority)
- `client_session.go`
  - control-session bootstrap, delegated-session refresh state, approval-token flows, and capability-execution wrappers
- `client_transport.go`
  - signed HTTP transport, retry-on-token-refresh behavior, SSE chat reader, and request-signature helpers

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
- `server_haven_memory_handlers.go`
  - display-safe memory inventory projection over continuity state **for the session tenant’s partition**
  - archive-and-reset path that closes and reopens the backend for **that partition only** (multi-tenant: other partitions untouched)
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
  - derived SQLite store for projected continuity classes
  - current benchmark-friendly projected-node search surface

- `memorybench_bridge.go`
  - narrow internal bridge that lets `cmd/memorybench` read the `continuity_tcl`
    projected discovery surface without widening Loopgate’s public API
  - benchmark governance now uses the same validated-candidate write path as production explicit memory writes

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
