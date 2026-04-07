# Loopgate Map

This file maps the main Loopgate package files. **Loopgate** is the authority and enforcement runtime in this repository. **Operator clients** attach over **MCP**, **proxy**, or **HTTP on the local control-plane socket**. Any remaining legacy client shells in the repo are deletion candidates, not active product surfaces.

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
- it centralizes tasks, approvals, memory, and host-action state so **every client** (IDE, CLI, proxy-integrated client) shares one auditable substrate

## Key Files

### MCP (enterprise IDE integration)

- `mcpserve/` — **`loopgate mcp-serve`**: stdio MCP (`mcp-go`), tools forward to **`loopgate.Client`** + delegated `LOOPGATE_MCP_*` env against the Unix socket (no second control-plane writer). See `docs/setup/LOOPGATE_MCP.md`, ADR 0005.

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
- `todo_capability.go`
  - authoritative execution for `todo.add`, `todo.complete`, and `todo.list`
  - bridges Todo tool calls onto explicit continuity mutations and wake-state reads
  - now persists task metadata into continuity distillates so wake-state can reconstruct a real task substrate
  - now also persists task execution classes so approval posture survives restart and wake-state rebuild
- `task_standing_grants.go`
  - Loopgate-owned standing-approval catalog for safe **actor-scoped** task execution classes (e.g. low-friction paths gated to the `haven` actor)
  - persists operator-visible “always allowed” class sets and audits changes
- `server_connection_handlers.go`
  - `/v1/status`
  - current global capability inventory surface
- `types.go`
  - request/response structs, including:
    - `CapabilitySummary`
    - `MemoryRememberRequest`
    - `MemoryRememberResponse`
    - `TodoAddRequest`
    - `TodoCompleteRequest`
    - `TodoListResponse`
    - `MemoryWakeStateResponse`
    - `MemoryDiagnosticWakeResponse`
  - now carries richer task-board metadata in `TodoAddRequest`, `TodoAddResponse`, and `MemoryWakeStateOpenItem`

### Haven Chat (agentic tool execution)

- `server_haven_chat.go`
  - Haven chat HTTP handler and SSE streaming loop
  - `runHavenChatToolLoop` — the agent loop; now delegates to `executeHavenToolCallsConcurrent`
  - `executeHavenToolCallsConcurrent` — fans out read-only tool calls in parallel (Phase 1: reads), then runs write/execute/unknown calls serially (Phase 2: writes). Each goroutine emits its own `tool_result` SSE event inline as it finishes, giving the operator live feedback.
  - `executeHavenToolCalls` — retained as the simple serial reference implementation; no longer called by the chat loop but kept for direct-call tests and future fallback use
  - `havenSSEEmitter` — now carries a `sync.Mutex` on the struct; `emit()` is goroutine-safe so concurrent read goroutines can stream events without corrupting SSE frames
- `tool_classification.go` (**new**)
  - `capabilityClass` struct: `readOnly bool` — derived from Loopgate's own `OpRead` / `OpWrite` / `OpExecute` taxonomy
  - `classifyCapability(registry, capabilityName)` — pure, fail-closed: unregistered → serial (readOnly=false); OpRead → readOnly=true; OpWrite/OpExecute → readOnly=false
  - Canonical dispatch decision for `executeHavenToolCallsConcurrent`. If you add a new `Op*` constant to `internal/tools/tool.go`, add a corresponding test case here.
- `tool_classification_test.go` (**new**)
  - Unit tests that pin the fail-closed contract and the OpRead / non-OpRead split
  - Also spot-checks real capability names (fs_read, notes.list, notes.write) to guard against unintentional operation-type changes
- `server_haven_chat_concurrent_test.go` (**new**)
  - Integration tests for `executeHavenToolCallsConcurrent`:
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
- `morphlings.go` / `morphling_workers.go` — lifecycle, worker IPC; `morphlingSummaryFromRecord` + `morphlingProjectionStatusText` project operator-facing summaries without raw model memory strings or termination prose
- `server_morphling_handlers.go` / `server_morphling_worker_handlers.go` — morphling lifecycle and workers
- `server_model_handlers.go` — model connection APIs; **session open** stamps `TenantID` / `UserID` from `config/runtime.yaml` → `tenancy` (see `docs/setup/TENANCY.md`, ADR 0004)
- `server_config_handlers.go` — configuration
- `server_connection_handlers.go` — `/v1/status` and connection surface
- `server_taskplan_handlers.go` — task plan execution
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
- `ui_types.go`
  - client-facing UI summaries and event envelopes
  - includes folder-access sync/status response types used by **local HTTP clients**
  - now also includes standing task grant summaries for the Security room
- `client.go`
  - local HTTP client over the Unix socket (`NewClient`, `doJSON`, `attachRequestSignature`, `computeRequestSignature`) — **wire reference** for non-Go integrators; see `docs/setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md`
  - methods used by local clients and test harnesses (not authority)
  - includes explicit diagnostic-wake loading for continuity status
  - includes granted-folder sync for the **resident folder bridge**
  - now includes standing task grant read/update methods for the Security room
  - includes memory inventory/reset client calls for **HTTP-native** operator UIs

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

- `memory_conflict_anchor_test.go`
  - Phase 1 anchor persistence and fail-closed TCL failures: `TestRememberMemoryFact_SupersedesOnlyWhenAnchorTupleMatches`, `TestRememberMemoryFact_CoexistsWhenTCLReturnsNoAnchor`, `TestRememberMemoryFact_FailsClosedWhenTCLValidationFails`
  - additional wake/replay coverage for anchor tuples lives in `continuity_memory_test.go` (see implementation plan Task 4 test names)

- `server_memory_handlers.go`
  - memory endpoints, including explicit remember and diagnostic wake
- `server_haven_memory_handlers.go`
  - display-safe memory inventory projection over continuity state **for the session tenant’s partition**
  - archive-and-reset path that closes and reopens the backend for **that partition only** (multi-tenant: other partitions untouched)
- `continuity_memory.go`
  - thread distillation
  - explicit remembered facts
  - wake-state generation
  - wake-state diagnostics
  - explicit remember persistence now revalidates and consumes `tcl.ValidatedMemoryCandidate` directly; request normalization is preflight only
  - timezone/locale alias handling ends at the adapter boundary, so persistence only sees canonical `profile.timezone` / `profile.locale`
  - discover-memory retrieval remains tag-overlap first, with a narrow slot-preference tie-break for a tiny allowlist of stable profile slots (`name`, `preferred_name`, `timezone`, `locale`)
  - that tie-break only reorders already-eligible discover results; it is not an admission path and must not bypass lineage or review filters
  - now rehydrates task metadata for unresolved items from continuity facts
  - durable mutation ordering: audit before continuity JSONL append; `saveContinuityMemoryState(..., nowUTC)` for testable / consistent artifact timestamps
- `continuity_mutation_ordering_test.go`
  - `TestMutateContinuityMemory_*`, `TestContinuityInspectRequest_*`, corrupt-replay coverage per Phase 1 Tasks 1 and 5
- `continuity_runtime.go`
  - event-log replay and wake reconstruction
  - important when new continuity event types are introduced
  - now projects structured task snapshots instead of only item IDs and text

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
- `todo_capability.go`
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
