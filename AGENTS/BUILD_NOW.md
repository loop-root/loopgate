# BUILD_NOW.md

## Purpose

This document defines the **current implementation slice** for Loopgate.

**Phased execution order and exit criteria:** `sprints/` (latest dated plan). **Durable decisions:** `docs/adr/`.

It exists to prevent scope creep and keep development focused.

If a proposed change does not help this slice, defer it.

This file is intentionally narrower than `ARCHITECTURE.md`.

---

## Current Mission

Make Loopgate enterprise-ready.

The security kernel and enforcement runtime already exist. The current work is adding the integration surface, multi-tenancy foundation, and administrative interface that make Loopgate viable as enterprise infrastructure — without building another UI that developers have to learn.

---

## The Slice We Are Building

### 1. MCP server

Loopgate exposes itself as an MCP server. Claude Code, Cursor, and any MCP-compatible IDE connects with a single config entry and gets governed capability execution, memory recall, and policy enforcement. The developer's existing tool is the UI. We don't build one.

### 2. Multi-tenancy foundation

Add `tenant_id` and `user_id` to the runtime context. Every resource — memory distillate, audit event, capability grant, secret — acquires tenant identity. Cross-tenant access is a hard denial. This is the prerequisite for every enterprise feature that follows.

### 3. Memory system fixes

The key registry silently rejects `goal.*` and `work.*` keys. Preference facet coverage is too narrow for real use. Both are silent data-loss bugs that must be fixed before memory is relied on by more users.

### 4. Chat regression fixes

Panic recovery in `handleHavenChat`, audit log coverage for error paths, and UX polish for **legacy HTTP chat** consumers. These block reliable **local demos** that still use `/v1/haven/chat` (handler and route names are historical).

### 5. Admin console v0

Minimal web UI served by an admin-mode Loopgate node: policy viewer, audit log, user list. Server-rendered HTML is acceptable. This is the first enterprise customer-facing surface.

---

## In Scope Right Now

### 1. MCP Server

Implement `loopgate mcp-serve` — an MCP protocol handler that delegates all requests to the local Loopgate enforcement node.

**Minimum goals:**

- register Loopgate's typed capability set as MCP tools
- validate all requests through the same policy evaluation path as HTTP handlers
- emit audit events for every capability execution — identical to HTTP paths
- surface approval workflows to the connected IDE (tool result with approval ID)
- expose `memory.remember` and memory recall as MCP tools
- one config entry (`.mcp.json`) connects any MCP-compatible IDE

**Non-negotiable:** MCP is not a trust bypass. A capability denied over HTTP is denied over MCP. Same policy evaluation, same audit logging, same approval workflow.

**Recommended library:** `github.com/mark3labs/mcp-go` (Go MCP implementation). Review before adopting.

---

### 2. Multi-Tenancy Foundation

Add tenant identity to the runtime context before any other enterprise work.

**Minimum goals:**

- `TenantID` and `UserID` fields on `ControlSession` and downstream request contexts
- `tenant_id` namespace isolation for: memory distillates, audit events, capability grants, secrets, morphling contexts
- Cross-tenant access returns a hard denial with an explicit error — not empty results, not a fallback
- Node initialization flow that sets tenant identity (can be single-tenant/empty string for personal mode — no behavior change for existing single-node deployments)
- `tenant_id` appears in all audit events
- Session-scoped **diagnostic** logs (`logging.diagnostic` / `internal/loopdiag`) include **`tenant_id` and `user_id`** on each line where a control session is known, aligned with audit semantics so operators can filter by tenant

**Do not build yet:** multi-node sync protocol, admin node networking, IDP integration. Get the data model right in single-node mode first. Everything else is built on top of this.

**Files to touch:** `internal/loopgate/server.go` (session/context struct), `internal/loopgate/types.go`, audit event types, memory distillate schema.

---

### 3. Memory System Fixes

**File: `internal/tcl/memory_registry.go`**

- Add `goal.*` and `work.*` as recognized prefix rules in `CanonicalizeExplicitMemoryFactKey()`

```go
{rawPrefix: "goal.", canonicalPrefix: "goal."},
{rawPrefix: "work.", canonicalPrefix: "work."},
```

- Expand `deriveExplicitPreferenceFacet()` to cover common preference categories:

```go
case "bullet" / "list" / "numbered" → "response_format"
case "concise" / "brief" / "short" / "terse" → "verbosity"
case "verbose" / "detailed" / "thorough" → "verbosity"
case "formal" / "professional" → "tone"
case "casual" / "informal" → "tone"
case "one question" / "single question" → "question_style"
```

**File: `cmd/haven/capabilities.go`**

- Update capability hints to only reference key patterns that actually pass canonicalization. If the hint says "goals" are a valid category, `goal.*` must be in the registry.

**Why this matters:** The model attempts these keys based on the capability hint, they fail silently (no error surfaced), and the user's fact is lost. This is a silent data-loss bug, not a feature gap.

---

### 4. Chat Regression Fixes

**File: `internal/loopgate/server_haven_chat.go`**

Add panic recovery at the top of `handleHavenChat`. Use the **diagnostic `slog` path** already wired from `config/runtime.yaml` (`logging.diagnostic`) so operators see **ERROR** lines in `server.log` (or the appropriate channel) when they raise verbosity — not only stderr. Example shape:

```go
defer func() {
    if r := recover(); r != nil {
        server.log.Error("haven.chat panic", "panic", r)
        http.Error(writer, "internal error", http.StatusInternalServerError)
    }
}()
```

Ensure other chat error paths (timeout, provider failure) also log at **WARN**/**ERROR** with redaction rules from `AGENTS.md`. Move `server.logEvent("haven.chat", ...)` before error-path early returns. Currently, sessions that fail (panic, timeout, tool loop error) are never audited because the log call is after the early return. Every chat attempt must be recorded.

Optional: lower `modelCtx` timeout in the tool loop from 120s to 60s so the fallback text appears sooner on slow local models.

**Native client (out of tree):** If you maintain a separate macOS chat UI, mirror the same behavior there: expose a **thinking / typing** state tied to the in-flight HTTP request, and show a typing indicator in the conversation surface. (Historical notes referred to Swift sources under a `Haven/` tree; this repository does not vendor that app.)

---

### 5. Admin Console v0

A minimal web UI served by a Loopgate node running in `--admin` mode.

**Minimum goals:**

- policy viewer: display active policy rules in human-readable form
- audit log viewer: filter by tenant, user, capability, time range; export to CSV
- user list: provisioned users and their last-seen timestamp

**Implementation notes:**

- server-rendered HTML is acceptable for v0 — this is an admin tool, not a real-time surface
- must require authentication (Loopgate session model — do not ship unauthenticated)
- HTTP over TCP (not Unix socket) for admin node — secured with mTLS in production
- do not reuse `internal/loopgate/web/admin/` (stale legacy frontend); start fresh or use a minimal server-rendered template approach

**Not in v0:** policy editor, IDP integration, canvas agent builder, real-time event streaming.

---

## Explicitly Out of Scope Right Now

These may matter later. They do not define the current slice.

- **Organizational IdP integration (OIDC/OAuth; SAML where required)** — after multi-tenancy and session model are correct; parallel **integration layer** to provider OAuth/PKCE already used for model connections. Roadmap: `sprints/2026-04-01-loopgate-enterprise-phased-plan.md` § *Future enterprise integration layers*.
- **Enterprise secret backends (e.g. HashiCorp Vault, cloud KMS, HSM-backed stores; TPM/platform identity for bootstrap)** — extend `SecretStore` / `SecretRef`; not a reason to weaken local keychain defaults. Roadmap: same sprint section; `docs/setup/SECRETS.md` § *Enterprise integration layer*.
- Policy decision tree editor UI
- Zero-code canvas agent builder
- Multi-node admin ↔ local sync protocol
- Proxy mode implementation (MCP is higher priority)
- Non-Loopgate desktop UI features (paint animation, uninstaller, man page) — defer
- Full production installer / distribution improvements
- Distributed morphling execution across nodes
- Webhook/event streaming API
- Rate limiting and quota management
- Model routing

---

## Preferred Reuse

Do not replace what already works:

- existing policy evaluation — extend, do not replace
- existing audit/ledger system — extend, do not replace
- existing session and token integrity — extend, do not replace
- existing memory and TCL system — fix and extend, do not replace
- existing morphling lifecycle — extend for multi-tenant, do not replace
- existing Unix socket HTTP transport — keep for local; add TCP mTLS for admin node separately

---

## Success Criteria

### MCP server

- a developer adds one `.mcp.json` config entry and uses Loopgate from Claude Code
- capability execution goes through policy evaluation (verify in audit log)
- memory recall surfaces facts across sessions
- approval-gated capabilities surface an approval request to the IDE, not a silent denial

### Multi-tenancy foundation

- every audit event has a `tenant_id` field
- querying memory for tenant A does not return tenant B facts
- cross-tenant access returns a hard denial with an explicit error code
- single-node personal mode behavior is unchanged (empty `tenant_id` degrades gracefully)

### Memory fixes

- `goal.current_sprint` as a memory key succeeds where it previously failed silently
- "I prefer concise answers" said four times across four sessions produces one distillate, not four
- the capability hint only references key patterns that actually canonicalize

### Chat regression

- "organize my downloads folder" with Anthropic provider no longer returns "I can't reach home base"
- `runtime/logs/` shows `haven.chat` audit events for every request including failed ones
- slow local model shows typing indicator immediately after message send

---

## Decision Heuristics

### Does it help a developer use their existing IDE with Loopgate?

If yes, it's probably in scope.

### Does it require redesigning the enforcement runtime?

If yes, strongly prefer not to do it now.

### Does it add developer-invisible governance to an existing workflow?

If yes, high priority.

### Does it require building a new UI that developers use every day?

If yes, question whether MCP or proxy integration can solve it instead.

### Does it increase backend complexity without improving the enterprise story?

If yes, defer.

---

## Notes for Agents

- The security constitution in `AGENTS.md` does not change because the product direction changed. Every invariant, policy rule, and security constraint remains in force.
- Multi-tenancy is a **data model change first**. Do not introduce sync protocols, network topology, or admin node networking until `tenant_id` isolation is correct in the single-node data model.
- MCP handlers must go through the **same policy evaluation path** as HTTP handlers. There is no MCP fast path that skips policy. This is a hard requirement, not a guideline.
- Do not ship the admin console without authentication. Even v0 must require Loopgate session authentication before serving any admin routes.
- `cmd/haven/` (Wails prototype) is frozen. Do not add product features there.
