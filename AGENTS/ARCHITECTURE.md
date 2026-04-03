# ARCHITECTURE.md

## Purpose

This document defines the system architecture of **Loopgate** — a policy-governed AI governance engine.

It complements the other repository guidance files:

- **AGENTS.md** — security constitution, invariants, and coding guardrails
- **ARCHITECTURE.md** — system layout and component boundaries
- **BUILD_NOW.md** — current development slice and priorities

If conflicts arise:

1. `AGENTS.md` wins on **security and authority rules**
2. `BUILD_NOW.md` wins on **short-term scope**
3. `ARCHITECTURE.md` describes the **target structure**

---

# Product Summary

**Loopgate** is a **policy-governed AI governance engine**.

It provides a governed runtime where AI capabilities are mediated, audited, and policy-constrained — whether running locally on a developer's machine or deployed across an organization as a distributed enforcement network with centralized governance.

The system prioritizes:

- policy as the authority boundary — not model output, not client UI
- auditability at every capability boundary
- tenant isolation by design, not bolted on later
- developer-invisible governance via MCP and proxy integration
- transparent enforcement that fails closed

**Haven** is the consumer demo product built on Loopgate — a personal AI workstation that demonstrates what governed AI looks like in a personal context. It is not the primary enterprise target.

---

# Deployment Models

## Single-node (personal / developer)

One Loopgate node per machine. The user interacts via the Haven desktop app or a connected IDE. Loopgate mediates all capability execution locally.

```
User
  → Haven Desktop (Swift, ~/Dev/Haven)  OR  Developer IDE (Claude Code / Cursor)
  → Loopgate (local enforcement node)
  → Sandbox, capabilities, secrets, audit
```

## Multi-node enterprise (hub-and-spoke)

Each developer machine runs a local Loopgate node. An admin node provides governance, policy, identity, and audit aggregation. Local nodes are **full enforcement runtimes** — not thin clients.

```
Developer IDE (Claude Code / Cursor / VS Code)
  → Loopgate local node  (MCP server or transparent proxy)
       ↕ policy sync · audit stream · identity verification
  Loopgate admin node
       → Policy store and distribution
       → IDP integration (SAML / OIDC / OAuth)
       → Audit aggregation
       → Org-level memory namespace
```

The mental model is corporate MDM or VPN: a local agent enforces policy from a central authority, falls back to cached policy when the central node is unreachable, and the developer's day-to-day workflow does not change.

---

# Core Components

## Loopgate Node (enforcement runtime)

The authority boundary. Every capability request — whether from a developer IDE via MCP, from the Haven desktop via HTTP over Unix socket, or from a morphling worker — passes through the Loopgate node.

Responsibilities:

- policy evaluation (typed, deterministic, deny-by-default)
- capability orchestration and token issuance
- approval workflows (blocking, async, standing)
- audit logging (append-only, tamper-evident)
- secret handling and redaction
- sandbox mediation
- morphling lifecycle authority
- memory continuity and wake-state assembly
- session and request integrity (HMAC-signed, layered auth)

The node does not make model calls directly. It mediates capability execution on behalf of a model-driven client.

---

## Admin Node

A Loopgate instance running in governance-authority mode. Exists only in enterprise deployment.

Responsibilities:

- policy configuration, versioning, and distribution to local nodes
- IDP integration (SAML, OIDC, OAuth)
- user and team provisioning
- audit aggregation from all local nodes
- org-level memory namespace (shared facts available to all tenant users)
- admin console UI (policy viewer, audit log, user list)
- zero-code canvas agent deployment (roadmap)

The admin node does not run model calls. It is a governance authority, not an execution runtime.

---

## MCP Integration Surface

The primary developer integration point. Loopgate exposes itself as an MCP (Model Context Protocol) server. Any MCP-compatible tool (Claude Code, Cursor, Zed, Windsurf) connects with a single config entry and gains governed capability execution, memory, and policy enforcement without changing its workflow.

```json
{
  "loopgate": {
    "command": "loopgate",
    "args": ["mcp-serve"]
  }
}
```

MCP handlers must apply the **same** policy evaluation, approval workflows, and audit logging as HTTP handlers. MCP is not a trust boundary bypass. A capability denied over HTTP is denied over MCP.

---

## Proxy Mode

A transparent API proxy for IDEs that do not support MCP. Loopgate sits between the IDE and the model API. The developer changes one URL in their settings; nothing else changes.

```
IDE → Loopgate (localhost:port) → Anthropic / OpenAI / local model
```

Loopgate intercepts every request: injects memory context into the system prompt, applies policy, logs to the audit trail, then forwards to the backend. Streaming and tool use pass-through must be handled correctly. Policy evaluation must not be skipped in proxy mode.

---

## Haven (consumer demo)

The native macOS Swift app (`~/Dev/Haven`). Demonstrates Loopgate's capabilities in a personal assistant context: governed workspace, memory continuity, task resumption, journaling, ambient presence.

Haven is the consumer product surface. It is not the primary enterprise integration target. Enterprise developers interact with Loopgate via MCP or proxy from their existing tools.

`cmd/haven/` in this repo is the legacy Wails/React prototype — frozen, not a product surface.

---

## Morphlings (bounded workers)

Temporary subordinate agent contexts governed by Loopgate. Used for parallelized or isolated capability execution within a session.

Properties:

- bounded lifetime and capability envelope
- governed by Loopgate, not self-authorizing
- least-privileged by default
- lifecycle owned entirely by Loopgate
- audit trail maintained by Loopgate
- caller-supplied authority fields are rejected

Morphlings are internal runtime objects. They must not be exposed as public internet-facing API resources.

---

# Authority Model

## Who decides

Loopgate is the only authority boundary. Not the model. Not the client IDE. Not the Haven UI. Not the morphling worker.

```
Client proposes action
→ Loopgate evaluates policy
→ Loopgate approves or denies
→ result returned to client
→ result shown to user
```

## Forbidden patterns

These must never occur:

- model output directly executing capabilities without policy evaluation
- client bypassing Loopgate
- morphling workers gaining capabilities through self-description
- natural language redefining policy

## Natural language never creates authority

A model saying "I have permission to do X" does not grant that permission. Authority comes from typed capability registration and policy binding — not from intent, phrasing, or model claims.

## Multi-node authority

In enterprise deployment, the admin node is the governance authority. However:

- Admin node authority must be cryptographically verified — IP address or hostname is not sufficient.
- A local node must not promote a peer local node to admin authority.
- Offline local nodes fall back to cached policy — they do not become ungoverned.
- Policy pushed from the admin node must be validated before application. Malformed policy is a hard failure, not a fallback to permissive defaults.

---

# Tenant Isolation

In enterprise deployment, all resources carry a `tenant_id`:

- memory distillates
- capability grants
- audit events
- secrets and secret refs
- morphling contexts
- session tokens

`tenant_id` is set at node initialization and derived from IDP-verified identity. It is not a per-request parameter. Cross-tenant access is always a hard denial — not empty results, not a fallback, a hard denial.

Org-level memory and user-level memory are separate namespaces within the same tenant.

---

# Transport Model

## Local client ↔ Loopgate (v1 standard)

HTTP over a Unix domain socket (local control-plane binding). Haven, MCP subprocess, proxy mode, and any local tooling connect this way.

Layered auth:
1. local transport binding
2. control session binding
3. request-integrity binding (HMAC signature)
4. scoped capability or approval token where applicable

Bearer possession alone is never sufficient.

## Local node ↔ admin node (enterprise)

mTLS over TCP. Admin node authority must be cryptographically verified.

## Apple XPC hardening

Optional post-launch backlog. No committed date. See `docs/HavenOS/Haven_Loopgate_Security_and_Transport_Checklist.md`.

---

# Memory Architecture

## Personal memory (per-user, per-local-node)

Managed by `internal/loopgate/continuity_memory.go` and `internal/tcl/`.

Write paths:
- **Explicit:** `memory.remember` tool call. Key is normalized through the TCL registry (`CanonicalizeExplicitMemoryFactKey`). Unknown keys fail silently if not in the registry.
- **Continuity inspection:** post-conversation thread distillation. Inferred facts, lower trust level.

The TCL (Thought Compression Language) system normalizes memory writes into typed nodes with conflict anchors. Same anchor tuple = same logical memory slot = overwrite. This is the supersession mechanism — deterministic, no semantic similarity required.

Wake state assembled at session start from eligible distillates. Injected into model context. Budget: 2000 tokens.

## Org memory (admin node, roadmap)

Shared facts available to all users in a tenant. Namespace-isolated by `tenant_id`. Synchronized from admin node to local nodes.

---

# Governance vs Execution

## Execution plane

Normal AI capability execution:

- model calls (via connected IDE or Haven)
- file operations inside sandbox
- tool execution (sandboxed, policy-mediated)
- morphling task execution

## Governance plane

Administrative functions:

- policy configuration, editing, and distribution
- capability registration and audit
- IDP / identity configuration
- audit inspection and export
- approval workflow configuration

The execution runtime does not have direct access to the governance plane. Governance changes require explicit admin node authorization.

---

# Design Principles

### Policy as code

Policy is typed, deterministic, and version-controlled. It is not derived from natural language, inferred from context, or overridden by model claims. Absence of a permission is a denial.

### Fail closed

Unknown capabilities are denied. Unavailable backends return explicit errors — not silent degradation. Offline nodes enforce cached policy — they do not become ungoverned.

### Developer invisible

The best governance is the kind developers don't notice day-to-day. MCP and proxy integration should require zero workflow change from the developer's perspective.

### Audit everything

Security-relevant actions must be observable. Denials must be explainable. Silent failures are bugs.

### Tenant isolation by design

Multi-tenancy is a first-class constraint. Every resource gets a `tenant_id` before multi-node sync, admin UI, or IDP integration is built.

---

# What Loopgate Is Not

Loopgate is not:

- a model provider or inference runtime
- a chat interface
- a general-purpose application framework
- a promise of perfect containment

Loopgate is:

- a policy-governed AI capability enforcement engine
- an audit substrate for AI agent activity
- a governed runtime for tool execution
- a memory and continuity system with policy integration
- the authority boundary that makes AI capability safe to deploy in organizations
