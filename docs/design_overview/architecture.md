**Last updated:** 2026-04-03

# Loopgate architecture overview

This repository is centered on **Loopgate**: the policy-governed **AI governance engine** and local control plane (`cmd/loopgate`, `internal/loopgate`).

**Operator clients** connect over **HTTP on a Unix domain socket** (v1). **Primary direction:** MCP hosts and proxy clients (**Claude Code**, **Cursor**, **VS Code**, **Anti‑Gravity**, **OpenAI Codex**, …) — see `docs/setup/LOOPGATE_MCP.md`. Any remaining in-repo UI shells are legacy code, not current product surfaces.

**Morphlings** are Loopgate-governed bounded workers (naming unchanged).

## 1) Current system classification

As of **2026-04-03**, the implemented deployment is:

- **local** control plane (typical socket: `runtime/state/loopgate.sock`)
- **single-tenant** in code today; **multi-tenant `tenant_id`** is an explicit enterprise direction (see root `AGENTS.md`)
- **HTTP over Unix domain socket** between local clients and Loopgate (v1; see RFC 0001 and `docs/setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md`)
- append-only audit logging
- deny-by-default capability execution

**Enterprise surfaces** (MCP server, proxy mode, mTLS admin transport) are **in progress** — not fully described by this “local single-node” snapshot alone.

## 2) High-level execution model

Typical **IDE / MCP** flow (target primary):

`developer tool → Loopgate (MCP or proxy) → validation / policy / approval / tool execution → structured result → Loopgate durable memory / audit`

Supporting subsystems include: `internal/state`, `internal/prompt`, `internal/model`, `internal/modelruntime`, `internal/memory`, `internal/loopgate`, `internal/shell`, `internal/setup`, and policy/tools/safety packages.

## 3) Component ownership

### Unprivileged operator clients

- **Shipped integrations:** MCP- and proxy-capable IDEs (see `docs/setup/LOOPGATE_MCP.md`).
- Persona loading, prompt compilation, model runtime configuration (non-secret), local session state, continuity thread projection, local ledger, approval UX — on the **unprivileged** side of the boundary (same pattern any client must follow).

### Loopgate

- Authoritative policy, capability orchestration, approval state machine, token minting and validation.
- Model inference for configured providers, filesystem capabilities, gateway audit, OS-backed secrets, integration auth (e.g. client_credentials, PKCE).
- Task plan validation, morphling lifecycle, sandbox mediation, continuity inspection, distillates, wake-state projection, governed recall.

## 4) Trust boundaries

**Trusted:** Loopgate binary, policy enforcement inside Loopgate, any local client **binary** (IDE bridge, MCP host, proxy client) as a transport — but **not** model output routed through it.

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

**Remaining gaps** (non-exhaustive): authorization code without PKCE, full refresh-token rotation story, generic external HTTP capability, externally anchored audit signatures, and explicit proxy/admin-node implementation as product priorities land.

## 7) Planned expansion

### Loopgate (product)

- Enterprise: **MCP server**, **transparent proxy**, **`tenant_id` isolation**, **mTLS** to governance authority — with the same policy and audit invariants as today’s HTTP handlers.
- OAuth and integration expansion, additional secret backends, typed integrations, deny-by-default secret export.

### Skills / manifests

- Explicit manifests, typed schemas, declared capability bindings, approval requirements — no permissions from prompt text alone.

### APIs

- Loopgate APIs for capability execution, connection flows, denial introspection, and future proxy mediation.
