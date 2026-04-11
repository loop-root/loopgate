# Loopgate Context Map

This file is the **master index** for the repository: a quick orientation guide for agents and contributors, and the **table of contents** for every `*_map.md` navigation file (those files are gitignored like this one so they stay out of review noise but remain on disk for local use).

Its goals:

- explain what the project is now
- show where the important code lives **without pasting large trees into prompts** — follow the map for the area you are changing
- call out the non-negotiable invariants
- separate real source from generated, runtime, local-only, or legacy paths

If this file disagrees with code, the code wins. If this file disagrees with the security constitution in `AGENTS.md` (repo root) or `AGENTS/AGENTS.md` (local-only), the agent guidance wins on safety and authority rules.

## Project in One Paragraph

Loopgate is a policy-governed AI governance engine.

This repo implements the enforcement runtime (`cmd/loopgate`, `internal/loopgate`) and shared backend libraries. It is the authority boundary for AI capability execution — locally on developer machines, and in enterprise deployment as a distributed enforcement network with centralized governance.

Product surfaces:

- **Primary integration surface:** **HTTP-on-UDS** control plane for native local clients and **out-of-tree** bridges. **In-tree MCP is deprecated and removed** (ADR 0010 — reduced attack surface); **reserved** for a possible future thin forwarder via new ADR.
- **Current operator MVP:** the Haven TUI/CLI shell in the separate `haven_cli` repo.

This repository centers on **Loopgate** as the governance engine.

## Current Product Shape

- `Loopgate` is the enforcement and governance kernel: policy evaluation, approvals, secrets, sandboxing, audit, memory continuity, morphling lifecycle.
- The **current product surface** is a direct local client model: **HTTP control plane** for native clients, plus the Haven TUI/CLI MVP and optional **out-of-tree** MCP→HTTP forwarders where operators want MCP-shaped hosts.
- **Transport:** local clients connect via HTTP over Unix domain socket (signed requests, same authority model as RFC 0001 / AMP local profile). Admin node connects via mTLS over TCP. Apple XPC is optional post-launch hardening (no committed date).
- `Morphlings` are bounded subordinate workers governed by Loopgate, not free agents.
- **Multi-tenancy:** `tenant_id` namespace isolation is the foundation for enterprise deployment — being added now. Every resource, audit event, and capability grant will carry a `tenant_id`.

Useful mental model (enterprise):

```text
Developer IDE or local HTTP client
  -> Loopgate **HTTP on UDS** (local node)
  -> Policy evaluation, audit, memory, approvals
  -> Admin node (governance, IDP, audit aggregation)
```

## Non-Negotiable Invariants

**Recent control-plane hardening (2026-04):** Pending capability approvals store a deep-copied `CapabilityRequest`; post-approval execution verifies the body hash against `ExecutionBodySHA256` when present. Secret-export blocking consults the tool registry (optional interfaces) plus the legacy name heuristic. In-memory caps bound pending approvals per session and replay tables (fail closed when saturated). See `docs/adr/0009-macos-scope-and-approval-hardening.md` and `docs/reports/security-hardening-plan-2026-04.md`.

These are the rules to keep in your head before editing anything:

- Loopgate is the authority boundary.
- Natural language never creates authority.
- Model output is untrusted input.
- Unprivileged clients (IDE, MCP host, CLI, TUI, or other local client) are not privileged just because they initiated a request.
- Local privileged transport stays local-only by default.
- The sandbox boundary matters. Governed workspace lives under `/morph/home`, not arbitrary host paths.
- Host access must stay explicit, mediated, and reviewable.
- Audit history is append-only and security-relevant actions must remain observable.
- User-visible summaries are derived views, not source-of-truth state.
- Secrets must stay out of logs, ledgers, plain config, and UI surfaces.
- Fail closed is preferred over fail open.

For the full safety constitution, read `AGENTS.md` at the repo root (and `AGENTS/AGENTS.md` if you maintain a local `AGENTS/` directory).

## Read This First

If you are new to the repo, read in this order:

1. `README.md`
2. `AGENTS.md` (repo root; tracked) — security constitution and system model
3. `AGENTS/AGENTS.md` (optional local copy under ignored `AGENTS/`)
4. `AGENTS/ARCHITECTURE.md` — enterprise architecture, deployment models, component boundaries
5. `AGENTS/BUILD_NOW.md` — current implementation slice and priorities
6. `internal/loopgate/server.go` — central server object, session/token state, mux registration
7. `internal/loopgate/continuity_memory.go` — memory and wake-state
8. `internal/tcl/` — Thought Compression Language: memory normalization, anchors, policy
9. `docs/reviews/memory_reviewGaps.md` — documented memory system gaps and fixes
10. `docs/plans/` — session handoff docs (if present)

Notes:

- `AGENTS.md` at the repo root is the tracked security constitution; `AGENTS/` is a local-only directory (gitignored). It may not exist on every clone.
- The current direction is **HTTP-native integrations**, the Haven TUI/CLI MVP, and multi-tenancy groundwork; **proxy mode is dropped** and **in-tree MCP remains deprecated**.

## Top-Level Map

```text
/Users/adalaide/Dev/loopgate
├── AGENTS/                Local agent guidance (ignored by git in normal workflows)
├── cmd/                   Executable entrypoints
├── config/                Checked-in runtime and alias config
├── core/                  Checked-in policy and some historical memory artifacts
├── docs/                  Architecture, setup, RFCs, threat model
├── internal/              Real implementation packages
├── persona/               Default operator persona (`default.yaml`) and values
├── runtime/               Local runtime state, socket, logs, sandbox, caches
├── README.md              Top-level project summary
├── go.mod / go.sum        Go module definition
├── morph                  Local build output binary
└── loopgate-admin         Stale leftover binary from a removed admin-UI path; should not be present
```

Important interpretation:

- `cmd/` and `internal/` are the main source trees.
- `docs/` is authoritative for design intent and product shape.
- `runtime/` is local machine state, not source.
- top-level binaries like `morph` are local build outputs and usually not meaningful source artifacts.
- `loopgate-admin` comes from a removed admin-UI path and should be treated as stale local output if it appears at all.

## Where the Agent Files Live

Local agent guidance may live in:

- `AGENTS.md` (repo root)
- `AGENTS/AGENTS.md`, `AGENTS/ARCHITECTURE.md`, `AGENTS/BUILD_NOW.md` (when `AGENTS/` exists locally)

These are useful because they capture:

- the security constitution
- enterprise architecture notes (when present)
- the current implementation slice

They are local/ignored rather than core product docs, so do not assume every clone or CI environment has them.

## Entrypoints

### Primary: HTTP control plane / IDE bridges

- **Docs:** `docs/setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md` (normative); `docs/setup/LOOPGATE_MCP.md` (**deprecated in-tree MCP**, out-of-tree / future-ADR note).

### `cmd/loopgate/`

The local control-plane server.

- `main.go`: starts Loopgate on the repo-local Unix socket under `runtime/state/loopgate.sock`

### `cmd/morphling-runner/`

Task-plan runner interface binary for the older lease-driven runner seam.

It is a thin execution wrapper, not an isolation boundary. Under current peer
binding, a distinct subprocess reusing another process's delegated session
credentials is expected to be denied.

## Internal Package Map

### `internal/loopgate/`

The most important backend package in the repo.

What lives here:

- control-plane server and handlers
- session and token integrity
- approval workflows
- sandbox import / export / stage / list / metadata
- memory continuity and wake-state
- model connection storage and validation
- morphling lifecycle and worker governance
- site trust and outbound integration hooks
- UI status queries used by unprivileged clients (e.g. IDE integrations)
- shared-folder mediation
- folder-grant mirroring and compare-before-sync refresh

Key files:

- `server.go`: central server object, session/token state, audit chain
- `server_*_handlers.go`: split handlers by concern
- `ui_client.go` and `ui_types.go`: display-safe control-plane queries for local clients
- `client.go`: Go HTTP client for Loopgate (**HTTP on Unix domain socket** for v1; used by tests and tooling — see `docs/setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md`; optional future XPC adapter TBD — see `docs/rfcs/0001-loopgate-token-policy.md`)
- `shared_folder.go`: default shared-space mediation
- `folder_access.go`: explicit host-folder grant storage and compare-before-sync mirror logic
- `continuity_memory.go`: Loopgate-owned durable memory selection and task-board wake-state reconstruction
- `continuity_runtime.go`: wake-state/runtime projection helpers; now part of the task-metadata and future scheduler groundwork
- `todo_capability.go`: Loopgate-owned task-board authority; persists task metadata into continuity state
- `morphlings.go`, `morphling_workers.go`: bounded worker lifecycle

Important note:

- The old standalone Loopgate HTTP admin UI and its embedded frontend tree have been **removed** from the active server path. Do not hunt for `internal/loopgate/web/admin/` — it is gone. Future **admin console** work is the enterprise admin-mode surface, not that path.

### `internal/sandbox/`

Sandbox path and copy primitives.

This package matters whenever host files move into or out of the governed sandbox.

Responsibilities:

- path normalization
- root enforcement
- symlink rejection
- atomic copy / mirror
- virtual path mapping helpers

### `internal/tools/`

Typed tool registry and core tool implementations.

Important distinction:

- `NewDefaultRegistry(...)` is repo-root oriented and used by Loopgate and non-sandbox runtime paths
- `NewSandboxRegistry(...)` is sandbox-home oriented and used for sandbox-scoped actors in Loopgate and related runtimes

Current core tools:

- `fs_list`
- `fs_read`
- `fs_write`
- `shell_exec`
- `path_open`
- `journal.*` (sandbox workspace)
- `paint.*` (sandbox workspace)
- `note.create` (sandbox workspace)
- `memory.remember` (explicit durable memory lane)
- `todo.*` (Task Board operating-memory tools)
- `notes.*` (private working-notes tools)

### `internal/policy/`

Policy checker and policy decision types.

The checked-in YAML policy lives under `core/policy/`, but this package enforces it.

### `internal/secrets/`

Secret storage and redaction.

Important here:

- macOS Keychain support
- secure-store selection
- redaction helpers
- audit-safe secret summaries

### `internal/memory/`

Memory and continuity primitives.

This is lower-level memory logic shared by unprivileged clients and Loopgate.

### `internal/threadstore/`

Append-only thread/event storage (`internal/threadstore`).

Use this when working on Messenger persistence or rebuilding UI state from thread history.

### `internal/model/` and `internal/modelruntime/`

Model-provider adapters and runtime configuration.

Current provider work includes:

- Anthropic
- OpenAI-compatible endpoints

### `internal/orchestrator/`

Task planning / structured orchestration helpers used by the runtime.

### `internal/audit/` and `internal/ledger/`

Append-only event persistence and related file-state helpers.

If your change affects auditability, denial paths, or write ordering, read these packages.

## Config and Policy

### `core/policy/policy.yaml`

Checked-in default policy:

- filesystem roots and denials
- read/write enablement
- shell / HTTP policy
- morphling spawn defaults
- memory thresholds
- safety toggles

### `core/policy/morphling_classes.yaml`

Morphling class definitions and resource envelopes.

### `persona/default.yaml`

Default operator persona for unprivileged clients.

### `config/runtime.yaml`

Runtime model/provider configuration.

### `config/goal_aliases.yaml`

Goal alias mapping used by the system.

## Docs Map

For a **folder-by-folder overview** of `docs/`, see `docs/docs_map.md`. Agent phase reports for the master implementation plan live under `docs/superpowers/reports/` (see `docs_map.md`).

**Execution roadmap:** `sprints/` (repo root) — timestamped phased plans with exit criteria; start with `sprints/README.md`.

**Architecture Decision Records:** `docs/adr/` — short, dated decisions (tradeoffs + escape hatches); index in `docs/adr/README.md`.

### Architecture and security docs

- `docs/design_overview/architecture.md`
- `docs/design_overview/loopgate.md`
- `docs/design_overview/systems_contract.md`
- `docs/loopgate-threat-model.md`
- `docs/roadmap/roadmap.md`

### Setup docs

- `docs/setup/SETUP.md`
- `docs/setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md` — Unix-socket HTTP, signing, routes
- `docs/setup/LEDGER_AND_AUDIT_INTEGRITY.md` — append-only JSONL hash-chain semantics (macOS operator expectations)
- `docs/setup/LOOPGATE_MCP.md` — **deprecated in-tree MCP** (removed); future reservation (ADR 0010)
- `docs/setup/SECRETS.md`
- `docs/setup/TOOL_USAGE.md`

### Repository map index (master TOC)

These `*_map.md` files (gitignored like this file) are the **navigation layer**: open the map for the directory you are working in, then open only the source files that map lists.

**Entrypoints and top-level trees**

- `cmd/cmd_map.md` — `cmd/loopgate/`, `cmd/morphling-runner/`, other binaries
- `core/core_map.md` — checked-in policy YAML and `core/memory/` interpretation
- `config/config_map.md` — tracked `config/*.yaml` (not the Go package)
- `persona/persona_map.md` — `default.yaml` and values
- `docs/docs_map.md` — documentation tree overview

**`internal/` — every package has a map**

- `internal/audit/audit_map.md` — audit severity over ledger append
- `internal/config/config_map.md` — Go loaders for policy, persona, JSON state (`Policy` struct, store helpers)
- `internal/threadstore/threadstore_map.md` — thread JSONL persistence
- `internal/identifiers/identifiers_map.md` — safe identifier validation
- `internal/integration/integration_map.md` — Loopgate integration tests (`*_test.go` only)
- `internal/ledger/ledger_map.md` — append-only hash-chained ledger
- `internal/loopgate/loopgate_map.md` — control plane (main package)
- `internal/loopgateresult/loopgateresult_map.md` — Loopgate result formatting for display/prompts
- `internal/memory/memory_map.md` — continuity/memory primitives
- `internal/model/model_map.md` — provider adapters and tool schema
- `internal/modelruntime/modelruntime_map.md` — `model.Client` construction from repo/env
- `internal/orchestrator/orchestrator_map.md` — tool orchestration and structured parsing
- `internal/policy/policy_map.md` — tool policy checker
- `internal/prompt/prompt_map.md` — system prompt compilation
- `internal/safety/safety_map.md` — strict path resolution (`resolvePathStrict`)
- `internal/sandbox/sandbox_map.md` — sandbox paths and safe copy
- `internal/secrets/secrets_map.md` — secret refs, backends, redaction
- `internal/setup/setup_map.md` — interactive model setup wizard
- `internal/shell/shell_map.md` — slash-commands and terminal integration
- `internal/signal/signal_map.md` — SIGINT/SIGTERM handling
- `internal/state/state_map.md` — compact client runtime state file
- `internal/tcl/tcl_map.md` — typed continuity language (normalize, anchors, validation)
- `internal/tools/tools_map.md` — typed tool registry and implementations
- `internal/ui/ui_map.md` — terminal UI primitives

**When to update maps**

- Loopgate and its client/integration surfaces: control-plane surfaces, HTTP-on-UDS transport (v1), future XPC TBD — see `docs/rfcs/0001-loopgate-token-policy.md` and `docs/loopgate-threat-model.md`
- Tools, prompt, model, modelruntime: capability or prompt/model/runtime contracts
- Sandbox, policy, secrets, safety, memory, TCL, audit, ledger, threadstore: boundaries or persistence semantics
- Config (Go), core/policy, checked-in `config/`: governance YAML or loader shape
- `docs/docs_map.md`: new major doc folders or renamed doc entrypoints
- Any map: when you add or rename primary files in that directory so the map stays a reliable shortcut

**Convention**

- One `_map.md` per logical parent directory; this file links them all
- Maps summarize roles and boundaries — they are not a substitute for reading AGENTS invariants before security-sensitive edits

## Runtime, Generated, and Local-Only Paths

These paths are easy to misread if you are new to the repo.

### Treat as local runtime state

- `runtime/`
- `output/`
- `tmp/`
- `.claude/`

### Treat as local build outputs

- `morph`
- `loopgate-admin`

### Treat carefully

- `core/memory/`

The repo currently contains memory-related files under `core/memory/`. Many of those paths are also ignored by `.gitignore` for normal runtime output. Do not assume everything under `core/memory/` is stable source code; some of it is historical or runtime-like data.

### Treat as local scratch unless a user says otherwise

- `HelloWorld.py`
- `body/`

## If You Are Changing X, Start Here

### Messenger / chat behavior (HTTP API + threadstore)

Start in:

- `internal/threadstore/` (append-only thread/event storage)
- `internal/loopgate/server_haven_chat.go` and related handlers (legacy route prefix `/v1/haven/...`)

### Sandbox / file import / export / shared-space behavior

Start in:

- `internal/sandbox/`
- `internal/loopgate/server_sandbox_handlers.go`
- `internal/loopgate/shared_folder.go`
- `internal/loopgate/folder_access.go`

### New native tool or capability

Start in:

- `internal/tools/`
- `internal/loopgate/server.go`
- `internal/loopgate/server_*_handlers.go`
- `internal/loopgate/types.go`
- `internal/loopgate/ui_client.go`
- tests in the same package

### Policy semantics

Start in:

- `core/policy/policy.yaml`
- `core/policy/morphling_classes.yaml`
- `internal/policy/`
- `internal/loopgate/`

### Secrets or model-provider setup

Start in:

- `internal/secrets/`
- `internal/loopgate/model_connections.go`
- `internal/modelruntime/`

### Morphlings

Start in:

- `internal/loopgate/morphlings.go`
- `internal/loopgate/morphling_workers.go`
- `internal/loopgate/morphling_classes.go`
- `cmd/morphling-runner/main.go`

### Continuity / memory / wake-state

Start in:

- `internal/memory/`
- `internal/loopgate/continuity_memory.go`
- `internal/loopgate/server_memory_handlers.go`
- `internal/loopgate/client.go`

Important note:

- explicit "remember this" requests should eventually use Loopgate's explicit remember path rather than relying only on thread distillation
- if memory feels broken, inspect threshold behavior, remembered-fact normalization, wake-state projection, and client-side prompt claims before inventing a new algorithm
- explicit remember lane exists through `memory.remember`; next work is diagnostics, resident continuity, and clearer operator-facing language

## Useful Commands

Primary flows:

```bash
go test ./...
go run ./cmd/loopgate
```

## Current Direction

Loopgate is pivoting from a personal AI workstation backend to an enterprise AI governance engine.

The enforcement runtime, policy evaluation, audit system, memory continuity, and morphling lifecycle are solid — that work is done. The current mission is adding the integration surface and proxy path that make Loopgate viable as enterprise infrastructure.

**Primary work right now:**

1. **HTTP control plane (v1)** — Integrations use **HTTP on the Unix socket** (session open, signed requests). **In-tree MCP removed** (ADR 0010); **out-of-tree** forwarders or a **future ADR** may add a thin MCP layer without a weaker trust boundary.
2. **Multi-tenancy foundation** — `tenant_id` isolation across all resources. Prerequisite for everything multi-tenant.
3. **Memory system fixes** — key registry expansion (`goal.*`, `work.*`), preference facet coverage. Silent data-loss bugs that must be fixed before memory is relied on.
4. **Chat path hardening** — panic recovery, audit log coverage, typing indicator for legacy HTTP chat handlers (`handleHavenChat` and related; `haven` prefix is a wire/handler name, not a product).
5. **Proxy v0** — automatic memory injection/capture path with strict auditability and bounded latency.

**Engineering invariants that don't change:** HTTP on local Unix socket for local client transport. mTLS over TCP for admin node. XPC optional post-launch. Policy-aligned routes over ad-hoc sprawl.

## Active Product Gaps

These are the known gaps to fix before the next milestone. See `docs/reviews/memory_reviewGaps.md` for detailed analysis.

**Blocking enterprise readiness:**

- No proxy v0. The seamless default developer experience does not exist yet.
- No `tenant_id` on resources. Multi-tenancy is not implementable without this foundation.
- Legacy `/v1/haven/...` route and threadstore dependency still need Loopgate-agnostic cleanup.

**Memory system (silent data-loss bugs):**

- `goal.*` and `work.*` key prefixes are not in the registry — `memory.remember` fails silently for these keys.
- Preference facet coverage is too narrow — repeated preferences on the same topic accumulate as separate distillates instead of superseding.
- 4 benchmark fixtures fail: same-entity preview-label confusion in timezone/locale contradiction cases.
- Slot-only contradiction: RAG baseline (10/12) beats continuity (8/12) — hints are the load-bearing retrieval mechanism, not anchors alone.

**Legacy HTTP chat / reference client (blocking some workflows):**

- Chat regression: Anthropic provider returns "I can't reach home base" for tool-heavy requests. Likely panic in the legacy HTTP chat handler (`handleHavenChat`) that closes the connection before a response is written.
- Chat regression: local model hangs silently for 120s with no typing indicator.
- Attachments may crash the reference client (nil dereference or JSON decode failure in message handler).

**Lower priority:**

- Capability hint references key patterns that don't pass canonicalization (`goal.*`, `work.*`).
- Continuity inspection threshold is opaque — not surfaced in settings or benchmark.
- Graduated policy rules (`DispositionReview`, `DispositionDrop`) not implemented — current policy is one motif, two outcomes.

## Useful Working Assumptions

- The underlying thread store and audit history should remain durable even if a given client UI is ephemeral.
- "Feels alive" should come from causality, continuity, and visible traces of work, not fake rituals or unexplained automation.
- Sandbox-local tools should map to clear operator workflows; if a capability has no obvious surface, the product model may not be ready.
- Explicit remember requests should be treated as a deterministic contract (`memory.remember`), not best-effort inference.

If you are unsure whether a change fits, check whether it moves the repo toward that product shape without weakening the Loopgate boundary.
