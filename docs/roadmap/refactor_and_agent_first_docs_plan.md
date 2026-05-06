---
status: active
owner_area: loopgate-core
tags:
  - refactor
  - agent-first-docs
  - maintainability
  - performance
related_code:
  - ../../internal/loopgate/server.go
  - ../../internal/loopgate/server_audit_runtime.go
  - ../../internal/loopgate/control_plane_state.go
  - ../../internal/loopgate/server_benchmark_test.go
related_docs:
  - ../design_overview/loopgate_locking.md
  - ../../context_map.md
  - ../../AGENTS.md
---

# Refactor and agent-first docs plan

**Last updated:** 2026-05-03

## Problem

Loopgate is reaching the point where adding enterprise, HIPAA-readiness,
audit-integrity, TUI, and performance work without tightening the core will
make future changes slower and riskier.

The immediate symptoms are:

- `internal/loopgate/server.go` is again too large and owns too many domains
- several handler/test files are large enough that local reasoning is costly
- important invariants live across docs, maps, comments, and tests instead of
  being easy for a new contributor or agent to load in one path
- performance and overload behavior now needs benchmark-backed decisions

## Design stance

Do not do a broad package rewrite.

Use small vertical slices that preserve behavior and tests while moving one
clear ownership domain at a time. The goal is not prettier directories; the
goal is easier authority reasoning.

Prefer sibling `internal/...` runtime packages for cohesive extracted domains
instead of adding more deep packages below `internal/loopgate/`. Keep
`internal/loopgate` as the HTTP/control-plane adapter and wiring layer.
Dependency direction is one-way: `internal/loopgate` may import extracted
runtime packages, but those packages must not import `internal/loopgate`.

## Refactor boundaries

Start with domains that already have separate concepts, locks, or maps:

1. **Audit runtime**
   - candidate code: audit sequence state, HMAC checkpoints, diagnostic audit
     emission, audit export coordination where it touches local append state
   - invariant: audit append failure for security-relevant actions remains a
     hard failure
   - package direction: `internal/auditruntime` owns the extracted runtime
     state; keep `internal/loopgate` as the HTTP/control-plane adapter

2. **Protocol and approval runtime**
   - candidate code: canonical capability/approval request envelopes, approval
     state machine, approval manifest hashing, and decision validation
   - invariant: approval decisions remain bound to owner, nonce, manifest, and
     single-use lifecycle state
   - package direction: `internal/protocol` and `internal/approvalruntime` own
     the pure request/approval logic; `internal/loopgate` keeps audit,
     transport, and handler wiring

3. **Control session and replay state**
   - candidate code: session open/close, session MAC rotation, request replay,
     nonce replay, expiry pruning
   - invariant: do not split logical auth/expiry transitions across lock
   windows
   - package direction: `internal/controlruntime`

4. **Claude hook runtime**
   - candidate code: hook pre-validation, hook lifecycle cache, hook approvals,
     hook audit projection
   - invariant: hook path remains fail closed and model/tool input stays
     untrusted
   - package direction: keep handler glue in `internal/loopgate`; move pure
     runtime/policy state only if a cohesive sibling package emerges

4. **Provider connections**
   - candidate code: connection records, PKCE, configured capabilities, token
     issuance/cache
   - invariant: clients never receive long-lived provider secrets
   - package direction: `internal/connections`

5. **MCP gateway**
   - candidate code: manifests, launch state, approval requests, execution
   - invariant: execution remains request-driven and auditable
   - package direction: `internal/mcpgateway`

## Agent-first documentation shape

Docs that are intended for agents should include:

- frontmatter with `status`, `owner_area`, `tags`, `related_code`, and
  `related_docs`
- a "start here" section for the specific task type
- concrete invariants, not just high-level descriptions
- short runnable commands with expected result shape
- links to code owners by path, not vague package names
- examples that show the canonical way to make a safe change

Do not turn every doc into an encyclopedia. Prefer small, linked docs with
clear ownership.

## First vertical slices

### Slice 1: benchmark baseline

Status: expanded baseline in progress.

Add benchmark coverage for:

- unauthenticated health route baseline
- audited hook pre-validation path
- direct audit append path
- direct ledger append path
- capability execution path
- approval creation path
- audit-ledger startup path

Success criteria:

- `make bench` prints p50/p95/p99 latency and throughput metrics
- benchmark results make the audit/fsync bottleneck visible
- no benchmark requires a running daemon or external service

### Slice 2: audit runtime extraction plan

Status: first extraction landed; continuing.

Write a local map for the future audit-runtime boundary, then move audit
sequencing/checkpoint ownership behind a narrow runtime API.

Success criteria:

- identifies exact functions and state fields to move
- names the package boundary and dependency direction
- includes tests that must continue passing
- `Server.logEvent` remains a compatibility facade while the main package
  shrinks

Planning artifact:

- `internal/loopgate/audit_runtime_extraction_map.md`

Implementation artifact:

- `internal/auditruntime/runtime.go`

### Slice 3: test decomposition

Status: started; MCP gateway, hook handler, configured-capability, config,
audit-export, quarantine, shared loopgate helper, and integration config tests
split. The original general `server_test.go` and request-auth runtime tests
have also been decomposed. The hook approval split has been tightened into
harness-owned approval and operator-override files.
Connection and control-capability scope tests have been decomposed as well.
Audit replay/hash-chain runtime tests have been split by concern.
Ledger append tests have been decomposed by append basics, chain integrity,
runtime cache behavior, and concurrency/large-event stress.
MCP gateway approval tests have been split by preparation and decision flow.
Config handler tests have been split by auth/scope, runtime updates,
connection config replacement, retired routes, and signed policy reload.
Host access handler tests have been split by plan parsing/recovery, symlink
escape denial, plan approval-risk classification, and shared setup helpers.
MCP gateway execution tests have been split by successful audited execution
and execution failure/cleanup behavior.

Largest remaining test files by line count:

- `internal/loopgate/server_ui_runtime_test.go`
- `internal/loopgate/server_test_connection_fixtures_test.go`
- `internal/loopgate/server_configured_capability_pkce_test.go`
- `internal/loopgate/shared_folder_test.go`
- `internal/loopgate/server_configured_capability_markdown_html_test.go`
- `internal/loopgate/server_hook_operator_override_test.go`

Decompose by behavior family, not by arbitrary size:

- MCP gateway: inventory, launch lifecycle, approvals, execution, policy
  denial paths
- Hook handlers: peer binding, pre-validation, approval cache, audit
  projection, failure modes
- Configured capabilities: config parsing, runtime binding, provider execution,
  response filtering
- Runtime config: audit integrity, control-plane overload, diagnostics,
  tenancy, export TLS/auth
- Audit export: trust diagnostics, cursor recovery, batch shape, sender
  responses
- Quarantine: path safety, duplicate detection, promotion, audit behavior

Success criteria:

- helpers stay shared, but scenario tests live near the behavior they verify
- each extracted test file has one obvious reason to change
- no behavior is changed only to make tests easier to split

Completed first pass:

- replaced `internal/loopgate/server_mcp_gateway_handlers_test.go` with focused
  MCP gateway test files for helper process behavior, inventory/status,
  lifecycle, execution, invocation validation, approval, and execution
  validation
- replaced `internal/loopgate/server_hook_handlers_test.go` with focused hook
  test files for shared helpers, basic policy decisions, path/symlink safety,
  harness/operator approvals, audit projection, rate limiting, and lifecycle
  audit-only events
- replaced `internal/loopgate/server_configured_capability_runtime_test.go`
  with focused tests for sandbox/secret safety, client-credentials token flow,
  redirect guards, JSON response filtering, markdown/HTML extraction,
  public-read extraction, site trust inspection, and PKCE
- replaced `internal/config/config_test.go` with focused config tests for
  policy helpers/loading/signing, persona defaults, runtime core validation,
  audit export validation, and audit-integrity/HMAC validation
- replaced `internal/loopgate/server_audit_export_runtime_test.go` with focused
  audit-export tests for state/batch construction, HTTP flush/cursor behavior,
  mTLS/pinning behavior, and certificate helpers
- replaced `internal/loopgate/server_quarantine_runtime_test.go` with focused
  quarantine tests for classification, storage, promotion, duplicate detection,
  prune/view behavior, and promotion guardrails
- replaced `internal/loopgate/server_test_helpers_test.go` with focused helper
  files for test setup/signing, policy fixtures, capability assertions,
  configured-connection fixtures, quarantine timestamp helpers, and UI event
  readers
- replaced `internal/loopgate/integration_config_test.go` with focused
  integration config tests for redirect validation, field validation,
  public-read examples/selectors, markdown extractor validation, HTML metadata
  validation, and shared repo-root helpers
- replaced `internal/loopgate/server_test.go` with focused server tests for
  filesystem capability execution, capability rate limits, shell policy
  denials, approval ledger ordering, operator-mount write grants, and sandbox
  import/stage/export flow
- replaced `internal/loopgate/server_request_auth_runtime_test.go` with focused
  request-auth tests for capability token refresh/signature/nonce checks,
  auth-denial audit persistence/suppression, approval-token authorization,
  approval creation rollback, response JSON secrecy, and shared audit readers
- tightened the earlier hook approval split by replacing
  `internal/loopgate/server_hook_approval_test.go` with separate harness-owned
  approval and operator-override tests
- replaced `internal/loopgate/connections_test.go` with focused connection
  tests for record persistence/keying/status, secret lifecycle/validation, and
  credential rotation rollback/cache invalidation
- replaced `internal/loopgate/server_control_capabilities_test.go` with focused
  control-scope tests for connection/site routes, diagnostics/MCP routes,
  folder/quarantine/sandbox/UI routes, and socket path validation
- replaced `internal/loopgate/server_audit_replay_runtime_test.go` with focused
  tests for request replay/execution-token guardrails and audit hash-chain
  metadata/determinism
- replaced `internal/ledger/ledger_test.go` with focused ledger tests for
  append basics/file creation, chain integrity/tamper failure, append runtime
  cache isolation, and concurrent/large-event append behavior
- replaced `internal/loopgate/server_mcp_gateway_approval_test.go` with focused
  MCP gateway approval tests for approval preparation/reuse/rollback and
  approval decision grant/deny/manifest/audit rollback behavior
- replaced `internal/loopgate/server_config_handlers_test.go` with focused
  config handler tests for config route auth/scope, runtime update validation
  and derived state, connection config replacement/rollback, retired routes,
  and signed policy reload/tamper rejection
- replaced `internal/loopgate/server_host_access_handlers_test.go` with focused
  host access tests for host plan parsing/recovery messages, symlink escape
  denial for read/list, host plan approval-risk classification, and shared
  host access setup helpers
- replaced `internal/loopgate/server_mcp_gateway_execution_test.go` with
  focused MCP gateway execution tests for successful audited execution and
  execution failure cleanup/denial behavior

### Slice 4: agent-readable setup path

Add frontmatter and "agent start here" sections to the setup docs most likely
to be read by agents:

- `docs/setup/GETTING_STARTED.md`
- `docs/setup/AGENT_ASSISTED_SETUP.md`
- `docs/setup/LEDGER_AND_AUDIT_INTEGRITY.md`
- `docs/setup/POLICY_REFERENCE.md`

Success criteria:

- an agent can identify setup mode, authority source, files changed, and
  verification commands without scanning the entire docs tree
- operator-facing prose remains readable for humans

## Current benchmark signal

The first local in-process benchmark shows:

- `/v1/health` wrapper/handler overhead is tiny
- audited hook pre-validation and direct audit append have similar latency
- audit append/fsync is the dominant cost in the audited hook path
- capability execution and approval creation add visible overhead above the
  audit/fsync floor and should be the next throughput inspection targets
- server startup with a small active audit ledger is currently fast enough to
  treat as watchlist, not the first bottleneck

That is expected and mostly healthy. We should optimize around bounded overload
and batching/export design, not by weakening audit durability.

## Non-goals

- no UI authority move
- no remote enterprise control-plane implementation in this cleanup pass
- no collapse of all locks into one lock
- no generated-doc sprawl without code links and ownership
