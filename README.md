# Loopgate

**Last updated:** 2026-04-08

**Loopgate** is a policy-governed AI governance engine and enforcement runtime. It is the control plane between AI models and your infrastructure: capabilities, approvals, audit, secrets, sandboxing, continuity memory, and bounded worker (morphling) lifecycle.

## What problem Loopgate solves

Most AI agent frameworks give the model ambient authority: the model calls tools, the tools run, things happen. The safety story is "we trust the model and hope the prompt is enough."

Loopgate takes the opposite position: **natural language is never authority**. The model proposes; Loopgate decides. Every capability execution goes through an enforcement choke point — policy check, approval gate, audit write — before anything touches your system.

Specific problems Loopgate addresses:

- **Uncontrolled tool execution.** Without a governance layer, an agent that's been jailbroken, confused, or prompt-injected can call any tool it knows about. Loopgate has deny-by-default capability execution. Every capability must be registered, policy-gated, and optionally approval-gated before it runs.
- **Invisible audit trails.** Most frameworks have no tamper-evident record of what the model actually did. Loopgate writes an append-only, hash-linked audit log. You can verify nothing was silently deleted or altered.
- **Secret leakage.** Agents that can read `~/.ssh` or `.env` files have access to secrets the model has no business seeing. Loopgate owns secrets; they never leave Loopgate through the public API. The model cannot read them because there is no code path that surfaces them.
- **Ephemeral agent memory.** Most frameworks start each session fresh. Loopgate keeps durable continuity memory: facts the assistant has remembered, tasks in progress, goals, journal entries — all survive across sessions and are explicitly governed.
- **Unbounded subagents.** Spawning a worker that can do anything is just recursion of the original problem. Loopgate morphlings are bounded by a declared class, run inside Loopgate's execution envelope, and require operator approval to spawn.

## How Loopgate differs from other agent frameworks

| Property | LangChain / CrewAI / AutoGen | Loopgate |
|---|---|---|
| Capability authority | Model decides which tools to call; framework executes | Model proposes; Loopgate evaluates policy and enforces approval gates before execution |
| Secret access | Tools can read any env var or file the process can access | Secrets are Loopgate-owned; never surfaced to the model or through the public API |
| Audit | Application logs, if configured | Append-only, hash-linked audit log; rollover segments with manifest |
| Memory | Stateless per-session, or ad-hoc vector stores | Durable, Loopgate-owned continuity memory with governed recall and explicit remember API |
| Subagents | Nested agents with the same (or wider) authority | Morphlings bounded by declared class; spawn requires approval; isolated execution envelope |
| Trust model | Model output is trusted as instruction | Model output is untrusted input; language is never authority |
| Multi-tenant isolation | Not native | `tenant_id` isolation across memory, audit, capability grants, secrets (in progress) |
| IDE integration | Python/JS SDKs, custom tool definitions | **HTTP on Unix socket** (v1); **in-tree MCP deprecated/removed** (ADR 0010 — smaller attack surface); **out-of-tree** MCP→HTTP forwarders possible |

## Core capabilities

Loopgate ships a registered capability inventory. The Haven TUI client uses these through the governed chat surface; other clients use them through **HTTP on the local socket** (and **out-of-tree** bridges where operators use MCP-shaped hosts).

**Filesystem**
- `fs_read` — read a file within granted paths
- `fs_write` — write a file (approval-gated by default)
- `fs_list` — list directory contents
- `fs_mkdir` — create a directory
- `fs_search` — search files by pattern or content

**Notes and Journal**
- `notes.list`, `notes.read`, `notes.write`, `notes.delete` — working notes in the sandbox
- `journal.write`, `journal.read`, `journal.search` — session journal: the assistant's record of what it did and found

**Memory**
- `memory.remember` — store an explicit fact into durable continuity memory
- `memory.wake` — surface the current wake state (recalled context for this session)
- `memory.discover` — search explicit memory

**Tasks and Goals**
- `todo.add`, `todo.complete`, `todo.list` — task board backed by Loopgate continuity; survives restart
- `goal.set`, `goal.close` — higher-level objectives that tasks roll up to

**Shell** (off by default; approval-gated per invocation)
- `shell_exec` — run a command in the sandbox shell

**Host Folder Access**
- `host.folder.grant`, `host.folder.list`, `host.folder.revoke` — explicit grants for mirrored host directories; compare-before-sync; audited

**Host Actions** (approval-gated)
- `host.organize.plan` — propose host filesystem organization changes
- `host.plan.apply` — execute a reviewed host plan

**Sandbox**
- `note.create`, `desktop.organize` — sandbox-scoped content creation
- `invoke_capability` — meta-capability for morphling delegation

**Paint / Canvas** (experimental)
- `paint.canvas.create`, `paint.canvas.list`, `paint.canvas.view`, `paint.canvas.patch`, `paint.canvas.export`

## Architecture

```
Developer tool (Claude Code, Cursor, VS Code, etc.)
        │
        │  Out-of-tree MCP→HTTP  │  Unix socket (local, normative)
        ▼
   ┌──────────────────────────────────────────────────────────┐
   │                       Loopgate                          │
   │                                                          │
   │  Session open → capability token → signed requests       │
   │                                                          │
   │  Policy check → Approval gate → Capability execute       │
   │                                                          │
   │  Audit   — append-only, hash-linked event log            │
   │  Memory  — continuity, wake state, governed recall       │
   │  Morphlings — 9-state lifecycle, Loopgate-owned          │
   │  Sandbox — /morph/home, import → stage → export          │
   │  Secrets — macOS Keychain on Darwin                      │
   └──────────────────────────────────────────────────────────┘
        │
        ▼
   Model provider (Anthropic, etc. — credentials in Loopgate)
```

**Transport:** Unix domain socket at `runtime/state/loopgate.sock`. **Normative v1:** HTTP clients attach directly. **In-tree MCP subprocess removed** (ADR 0010). No public TCP listener by default.

**Auth model** — layered:
1. `POST /v1/session/open` → control session + MAC key
2. Every privileged request carries: bearer token, control session ID, timestamp, single-use nonce, and a request HMAC
3. Bearer possession alone is not sufficient — peer binding enforced at the socket level

**Audit:** `runtime/state/loopgate_events.jsonl` — each event carries `audit_sequence`, `previous_event_hash`, and `event_hash`. Segments roll over and seal. The segment manifest at `runtime/state/loopgate_event_segments/manifest.jsonl` is append-only.

**Memory model:** Durable continuity event log under `runtime/state/memory/continuity_events.jsonl`. Separate goal and profile event streams. Wake state is rebuilt from the event log at session open. Explicit facts are written only through `/v1/memory/remember` — no implicit side channels.

**Sandbox model:** The assistant's workspace is `/morph/home`. Host content enters through explicit `POST /v1/sandbox/import`. Output is staged (`POST /v1/sandbox/stage`), reviewed by the operator, then exported (`POST /v1/sandbox/export`) to the host. No ambient host write access.

**Morphling lifecycle:** Nine states — `requested → authorizing → pending_spawn_approval → spawned → running → completing → pending_review → terminating → terminated`. Spawn requires approval. Goal text is stored as an HMAC in audit rather than raw text. Output is treated as untrusted artifacts until the operator reviews and promotes.

## Quick start

```bash
# Run tests and start the control plane
go test ./...
go run ./cmd/loopgate
```

**IDE integration:** **HTTP API** — `docs/setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md`. **Deprecated in-tree MCP** — `docs/setup/LOOPGATE_MCP.md` (removal rationale, **reserved** future thin forwarder via ADR).

**Haven TUI / CLI MVP:** See the `haven_cli` repo — the terminal workstation for governed AI work and the current operator-facing shell.

**Configuration:**
- `config/runtime.yaml` — server config, logging levels, tenant config
- `connections/*.yaml` — provider connection definitions (Anthropic, etc.)
- `core/policy/` — capability policy and morphling class definitions
- `persona/default.yaml` — default operator persona

**Secrets:** On macOS, credentials are stored in Keychain. See `docs/setup/SECRETS.md`.

## Key endpoints

| Endpoint | Description |
|---|---|
| `GET /v1/health` | Liveness probe (unauthenticated) |
| `GET /v1/status` | Full capability inventory (signed) |
| `POST /v1/session/open` | Open a control session |
| `POST /v1/capabilities/execute` | Execute a capability (policy-gated) |
| `POST /v1/approvals/{id}/decision` | Approve or deny a pending action |
| `GET /v1/ui/approvals` | List pending approvals |
| `GET /v1/memory/wake-state` | Current wake state |
| `POST /v1/memory/remember` | Store an explicit fact |
| `POST /v1/memory/discover` | Search memory |
| `POST /v1/morphlings/spawn` | Spawn a bounded worker |
| `POST /v1/sandbox/import` | Import host content into sandbox |
| `POST /v1/sandbox/export` | Export reviewed sandbox output to host |
| `GET /v1/ui/journal/entries` | List journal entries |
| `GET /v1/ui/desk-notes` | Desk notifications |
| `POST /v1/chat` | Governed Haven chat turn (SSE stream) |

Full endpoint list: `docs/design_overview/loopgate.md`.

## Repository layout

```
cmd/loopgate/          control-plane service
cmd/morphling-runner/  legacy task-plan runner interface binary
internal/loopgate/     server implementation
core/policy/           capability policy and morphling class definitions
persona/               default operator persona
connections/           provider connection definitions
config/                runtime.yaml
docs/                  architecture, RFCs, setup, threat model
runtime/               local state and logs (gitignored)
```

## Documentation

- [Architecture](./docs/design_overview/architecture.md)
- [Loopgate design](./docs/design_overview/loopgate.md)
- [MCP (deprecated in-tree)](./docs/setup/LOOPGATE_MCP.md)
- [HTTP API for local clients](./docs/setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md)
- [Threat model](./docs/loopgate-threat-model.md)
- [Setup](./docs/setup/SETUP.md)
- [Secrets](./docs/setup/SECRETS.md)
- [Tenancy](./docs/setup/TENANCY.md)
- [Roadmap](./docs/roadmap/roadmap.md)
- [Docs index](./docs/README.md)

## Status

Experimental; active security hardening. Not production-ready security software without your own review. See the [threat model](./docs/loopgate-threat-model.md) for an honest account of the current security posture and known gaps.

Runtime state, generated artifacts, and local editor config are excluded from source control. Examples: `runtime/state/`, `runtime/logs/`, local memory artifacts under `core/memory/`, `output/`, `tmp/`.

Proprietary — see [LICENSE](./LICENSE).
