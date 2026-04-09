**Last updated:** 2026-04-08

# Loopgate architecture overview

This repository is centered on **Loopgate**: the policy-governed **AI governance engine** and local control plane (`cmd/loopgate`, `internal/loopgate`).

**Operator clients** connect over **HTTP on a Unix domain socket** (v1). **Primary direction:** native HTTP clients, the Haven TUI/CLI shell, and optional external forwarders for MCP-shaped hosts (in-tree stdio MCP removed — see `docs/adr/0010-macos-supported-target-and-mcp-removal.md`, `docs/setup/LOOPGATE_MCP.md`). Any remaining in-repo UI shells are legacy code, not current product surfaces.

**Morphlings** are Loopgate-governed bounded workers (naming unchanged).

## 1) Current system classification

As of **2026-04-03**, the implemented deployment is:

- **local** control plane (typical socket: `runtime/state/loopgate.sock`)
- **single-tenant** in code today; **multi-tenant `tenant_id`** is an explicit enterprise direction (see root `AGENTS.md`)
- **HTTP over Unix domain socket** between local clients and Loopgate (v1; see RFC 0001 and `docs/setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md`)
- append-only audit logging
- deny-by-default capability execution

**Enterprise surfaces** (**in-tree MCP deprecated/removed** — ADR 0010; mTLS admin transport) are **in progress** — not fully described by this “local single-node” snapshot alone.

## 2) High-level execution model

Typical **IDE / operator client** flow (v1):

`developer tool → Loopgate (HTTP on UDS) → validation / policy / approval / tool execution → structured result → Loopgate durable memory / audit`

**MCP:** deprecated and removed in-tree; **reserved** for a possible future thin forwarder (new ADR). External MCP hosts use **out-of-tree** forwarders to this HTTP API today.

Supporting subsystems include: `internal/state`, `internal/prompt`, `internal/model`, `internal/modelruntime`, `internal/memory`, `internal/loopgate`, `internal/shell`, `internal/setup`, and policy/tools/safety packages.

## 3) Component ownership

### Unprivileged operator clients

- **Shipped integrations:** **HTTP-on-UDS** local clients and the Haven TUI/CLI shell. **In-tree MCP removed** — see `docs/setup/LOOPGATE_MCP.md` (deprecation) and `docs/setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md` (normative wire).
- Persona loading, prompt compilation, model runtime configuration (non-secret), local session state, continuity thread projection, local ledger, approval UX — on the **unprivileged** side of the boundary (same pattern any client must follow).

### Loopgate

- Authoritative policy, capability orchestration, approval state machine, token minting and validation.
- Model inference for configured providers, filesystem capabilities, gateway audit, OS-backed secrets, integration auth (e.g. client_credentials, PKCE).
- Task plan validation, morphling lifecycle, sandbox mediation, continuity inspection, distillates, wake-state projection, governed recall.

## 4) Trust boundaries

**Trusted:** Loopgate binary, policy enforcement inside Loopgate, any local client **binary** (IDE bridge, CLI/TUI shell, MCP host) as a transport — but **not** model output routed through it.

**Untrusted:** model output, user input, tool arguments/output, config until validated, external integration responses.

Model output is content, not authority. The client presents capability intent; Loopgate decides whether anything executes.

## 5) Invariants currently enforced

- Ledger append-only semantics where applicable.
- Loopgate is the execution choke point for governed capabilities and task-plan mediation.
- Approvals are created and enforced in Loopgate.
- Capability tokens are short-lived and scoped.
- Unprivileged operator clients do not receive raw secret material from Loopgate through the implemented contracts.
- Startup fails closed if Loopgate is unavailable (client paths that require it).
- Continuity thread transitions monotonic where designed; wake-state projection is derived and rebuilt from Loopgate authority.

## 6) Current implementation state

Loopgate supports provider-auth paths (`client_credentials`, `pkce`), YAML connection definitions, quarantine for raw remote bodies, morphling state machine, sandbox import/export/stage flows, and hash-linked audit where implemented.

See `docs/design_overview/loopgate.md` and `docs/roadmap/roadmap.md` for a feature-level list.

**Remaining gaps** (non-exhaustive): authorization code without PKCE, full refresh-token rotation story, generic external HTTP capability, externally anchored audit signatures, and explicit admin-node implementation as product priorities land.

## 7) Planned expansion

### Loopgate (product)

- Enterprise: **`tenant_id` isolation**, **mTLS** to governance authority, and direct governed clients with the same policy and audit invariants as today’s HTTP handlers. (**In-tree MCP** removed; any future IDE protocol adapter is **out of scope** until a new ADR — see ADR 0010.)
- OAuth and integration expansion, additional secret backends, typed integrations, deny-by-default secret export.

### Skills / manifests

- Explicit manifests, typed schemas, declared capability bindings, approval requirements — no permissions from prompt text alone.

### APIs

- Loopgate APIs for capability execution, connection flows, and denial introspection.
