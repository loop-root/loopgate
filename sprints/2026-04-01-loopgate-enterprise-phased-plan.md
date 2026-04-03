# Loopgate enterprise phased plan

**Created:** 2026-04-01  
**Status:** active roadmap (update this file when scope or order changes)  
**Companion docs:** `AGENTS/BUILD_NOW.md`, `AGENTS/ARCHITECTURE.md`, `context_map.md`, `docs/adr/`

This plan is the **execution roadmap** for the enterprise pivot: MCP, tenant isolation, memory fixes, Haven chat stability, and admin console v0. It assumes the security constitution in root `AGENTS.md` is non-negotiable.

---

## Engineering discipline (applies to every phase)

These are not optional polish; they are how we keep the system legible under stress.

### Comments (why, not only what)

After any **non-obvious** design decision, add roughly **two sentences**: why this structure exists, and what would break or confuse a maintainer if we did it differently. Obvious one-liners do not need essays.

Ask while writing: *How does this change over time? What breaks this in six months? What is the maintenance cost? What would an engineer in two years complain about?*

### Architecture Decision Records

Record durable choices in `docs/adr/` (see `docs/adr/README.md`). Target **~three sentences** per ADR: what we chose, the tradeoff, and what we would migrate to if the tradeoff hurts.

### Maps

When you add or rename **primary** files in a mapped package, update the corresponding `*_map.md` (and `docs/docs_map.md` if doc layout changes). This keeps agents and humans from spelunking blind.

### Operational logging (product requirement)

Loopgate already ships **human-legible diagnostic logs** via `log/slog`, configured in **`config/runtime.yaml`** under `logging.diagnostic` (`default_level`, per-channel `levels`, and file names). Load/merge semantics live in **`internal/config/runtime.go`**; the multi-channel logger is **`internal/loopdiag/manager.go`** (channels: `audit`, `server`, `client`, `socket`, `memory`, `ledger`, `model`). Levels include **error, warn, info, debug, trace**.

**Requirement:** Panic recovery, unexpected errors, and other operator-relevant failure paths **must** emit structured log lines on the appropriate channel (usually `server` for control-plane handlers; use the channel that matches how similar paths already log). Messages should be **troubleshooting-oriented**: what failed, with enough context to grep and correlate, without secrets or raw tokens (same redaction rules as `AGENTS.md`). Silence on failure is a bug — admins and support rely on these files to cut MTTR.

**Tenant correlation:** Whenever a control session or request context carries **`tenant_id`** (and **`user_id`** when available), diagnostic lines for that path **must** include those fields as structured `slog` attributes so multi-tenant operators can filter and support can avoid cross-tenant noise. Use the same sentinel or empty-string semantics as audit (see Phase 1 ADR for personal mode); for code paths **before** any session exists (early listen/bind failures), omit tenant/user or log `tenant_id=none` consistently — document which in code comments once.

New surfaces (**MCP**, **admin**, major handler changes) should log **start/finish or error** at minimum at INFO; DEBUG/TRACE reserved for deeper diagnosis when YAML turns verbosity up.

---

## Dependency overview

```text
Phase 1 (tenant_id) ──┬──► Phase 2 (MCP) ──► same policy/audit paths must carry tenant
                      │
                      └──► Phase 5 (admin v0) ──► audit/user views need tenant namespace

Phase 3 (memory registry) ──► can overlap Phase 1–2 if staffing allows; fix silent data loss before marketing memory-heavy flows

Phase 4 (chat regressions) ──► parallel; unblocks Haven demo but must not weaken audit/policy
```

**Intentional sequencing:** Multi-tenancy **data model on single-node** comes before betting heavily on MCP and admin UI, so new surfaces do not paint us into a corner. MCP and admin work should **thread `tenant_id` through** from the first merged PR, even if personal mode uses an explicit empty or default sentinel (see ADR when we lock the sentinel semantics).

---

## Phase 1 — Multi-tenancy foundation (single node)

**Goal:** Every durable resource and audit event carries tenant (and user where applicable) identity; cross-tenant access is a **hard, explicit denial** — never empty results or silent fallback.

**Primary touchpoints:** `internal/loopgate/server.go`, session/context types, audit event types, memory distillate paths, capability grants, secrets — as listed in `AGENTS/BUILD_NOW.md`.

**Exit criteria**

- [x] `ControlSession` (or equivalent) has `TenantID` / `UserID` (names per code review) populated from a single initialization path.
- [x] Audit events include `tenant_id` consistently.
- [x] **Diagnostic logs** on session-scoped handlers (HTTP, and later MCP) include **`tenant_id`** and **`user_id`** as structured attributes whenever the active `ControlSession` (or equivalent) is known — same semantics as audit so grep and log aggregators stay aligned. *(Audit-derived control-plane/model lines use `diagAppendTenantAttrs`; Haven chat panic/SSE lines use `diagnosticSlogTenantUser`. Pre-session paths — listen, socket bind, per-request HTTP middleware — omit tenant by design.)*
- [x] Tests: cross-tenant denial for at least one representative resource class (memory, audit read, or grant).
- [x] ADR: default tenant for personal/single-user mode and why.

**Also shipped for Phase 1 scope:** on-disk **memory partitions** per deployment tenant (`memory/partitions/…`), one-time legacy migration, partition-scoped Haven reset — see `internal/loopgate/memory_partition.go`, `docs/setup/TENANCY.md`. **Not** required for v1 single-org installs: tenant-suffixed **secret** storage keys (see § *Future enterprise integration layers*).

**Explicitly out of scope for this phase**

- Multi-node sync, admin node networking, IDP integration (`BUILD_NOW.md`).

---

## Phase 2 — MCP server

**Goal:** `loopgate mcp-serve` (or agreed command) exposes Loopgate as an MCP server; **same policy evaluation, approvals, and audit** as HTTP handlers.

**Exit criteria**

- [x] Tool registration mirrors typed capabilities without inventing a parallel permission model. *(v0: generic `loopgate.execute_capability` + `loopgate.status`; expand to typed tool names.)*
- [x] Denials and approvals behave like HTTP paths (tests that compare or share fixtures where practical).
- [x] Memory tools (`memory.remember`, recall) go through the same enforcement as existing paths. *(Achievable today via `execute_capability` with `capability=memory.remember`; dedicated MCP tool aliases TBD.)*
- [x] MCP lifecycle and request failures are **logged** on the appropriate diagnostic channel (see Operational logging), including **`tenant_id` / `user_id`** on each tool or session-scoped line when the MCP connection is bound to a control session; operators can raise verbosity via `logging.diagnostic` without code changes.
- [x] ADR: library choice (e.g. mcp-go) and why, plus escape hatch if we replace it. *(ADR 0005 + `docs/setup/LOOPGATE_MCP.md`.)*

**MCP vs proxy (what Phase 2 does *not* replace):** In a typical IDE setup, **chat** traffic flows **IDE ↔ model provider** (or local model); the MCP server receives **tool calls** only. Loopgate then sits **between the model and governed actions** (capabilities, memory via tools like `memory.remember` / recall, approvals) — **not** automatically between the model and every user token. **Automatic memory-in-context** for the whole prompt requires **transparent proxy mode** (IDE → Loopgate → provider) or an equivalent client-side injection strategy. **Proxy mode** is already a **documented enterprise target** (same policy/audit parity as HTTP) but **deferred behind MCP** in `AGENTS/BUILD_NOW.md` and tracked alongside MCP in `docs/roadmap/roadmap.md` / `context_map.md`. The two surfaces **compose**: MCP for tools + proxy for chat when both ship.

**Risk to watch:** MCP becoming a “fast path” that skips redaction or audit — treat as a **security bug**, not a performance feature.

---

## Control-plane transport (Unix socket vs TCP + mTLS)

**Today:** `Server.Serve` listens only on a **Unix domain socket** (`internal/loopgate/server.go`). That matches v1 local IPC (Swift Haven, same-host tooling) but is awkward for some **enterprise** layouts: containers without a shared UDS mount, integrators that expect a **TCP** port, cross-host callers, or ops standards that mandate **TLS on the wire**.

**Not the same link as admin governance:** `AGENTS.md` already distinguishes **local client ↔ Loopgate** (v1: HTTP over UDS) from **local node ↔ admin node** (enterprise: **mTLS over TCP**). This section is about the **local control-plane listen surface** that Haven, future MCP HTTP clients, and other integrations attach to — not replacing the admin-node protocol design.

**MCP vs transport:** MCP’s IDE-facing leg is usually **stdio** to a subprocess. That subprocess can call Loopgate over **UDS on the same machine** without TCP. You need **TCP (+ TLS/mTLS)** when a caller **cannot** use UDS or must cross a network hop to reach Loopgate.

### Do we need to settle TCP + mTLS *before* Phase 2?

| Situation | Sequencing |
|-----------|------------|
| Phase 2 = **same-host** MCP (subprocess → Loopgate via UDS or in-process server) | **No** — ship MCP against `executeCapabilityRequest` / existing HTTP paths on UDS first. |
| Integrations **require** TCP (K8s sidecar, remote agent, no socket file) | **Spike transport in parallel** or **immediately before** “remote MCP”; extend `internal/loopgate/client.go` (or equivalent) for `https://` + client certs when listen profile exists. |
| Product promise is “**enterprise = TLS port only**” even on laptop | **Yes** — define listen profile + ADR **early** so MCP and Haven configs target one URL scheme (e.g. loopback mTLS) rather than hard-coding UDS everywhere. |

**Recommendation:** Add a small **listen-profile** abstraction (`unix` default; optional `tcp` with **TLS or mTLS**, **127.0.0.1-only** by default for local encrypted loopback) + **ADR** (cert lifecycle, bind rules, fail-closed defaults). Implement **in parallel** with MCP once the spike confirms `mcp-go` — MCP tools then use the **same HTTP API** regardless of UDS vs TLS. **Do not** duplicate policy handlers per transport.

**Invariant:** Identical policy, audit, and session semantics for every bind mode; only the **listener** and **client transport** change.

---

## Phase 3 — Memory system fixes (silent data loss)

**Goal:** Registry and facet rules match product promises; no silent drop of `goal.*` / `work.*`; broader preference facet coverage per `BUILD_NOW.md`.

**Primary touchpoints:** `internal/tcl/memory_registry.go`, capability hints (including legacy `cmd/haven/` only if still required for builds — prefer Swift Haven + Loopgate as product path).

**Exit criteria**

- [x] Canonicalization accepts documented key families; tests for regression.
- [x] ADR: why the registry stays compiled-in for now vs external config (tradeoff: deploy velocity vs operator tunability).

---

## Phase 4 — Haven chat / demo regressions

**Goal:** Unblock reliable demo: panic safety in `handleHavenChat`, audit on error paths, typing indicator / timeout behavior for local models, attachment handling crashes (Loopgate + Swift Haven as applicable).

**Exit criteria**

- [x] Panic recovery **logs** at **ERROR** (or **WARN** only if the panic is fully benign and the response is still correct); include a stable message prefix, **`tenant_id` / `user_id`** when the handler has bound a session, request correlation where available, and stack or `panic` value **without** leaking secrets. Recovery must not swallow failures that must fail closed for security — document the distinction in code comments.
- [x] Other error paths in the same handlers (timeouts, provider errors, partial write failures) log at **WARN** or **ERROR** with the same redaction rules and **tenant/user** attributes when known so support can follow `server.log` / `model.log` without reproducing in a debugger.
- [x] Audit coverage on failure paths where security-relevant work occurred (`haven.chat`, `haven.chat.error`, `haven.chat.denied` in `server_haven_chat.go`).
- [x] Linked issues or ADR snippet if behavior is intentionally product-shaped (e.g. streaming contracts). *(Streaming remains product-shaped; no separate ADR — behavior documented in handler comments.)*

---

## Phase 5 — Admin console v0

**Goal:** Minimal authenticated surface: policy viewer, audit log, user list — server-rendered HTML acceptable.

**Exit criteria**

- [x] No unauthenticated exposure of policy or audit contents.
- [x] Respects `tenant_id` (admin sees only authoritative scope for that deployment mode).
- [x] Admin-relevant actions and auth failures are **logged** (diagnostic channels) with **`tenant_id`** (and admin identity fields as appropriate, redacted) so operators can trace “why can’t this admin log in?” and “who loaded policy?” per tenant without enabling a separate debug build.
- [x] ADR: auth mechanism for v0 and known limitations.

**Note:** Greenfield implementation in `internal/loopgate/admin_console.go` (no `internal/loopgate/web/admin/` tree in repo). ADR 0016 + `docs/setup/ADMIN_CONSOLE.md`.

---

## Future enterprise integration layers (post–current phases, RFC-first)

Phases 1–5 deliberately defer **customer-chosen identity and secret infrastructure**. Those are still **first-class product requirements** for self-hosted and hosted deployments; they land as **explicit integration surfaces** (RFC + ADR + `SecretStore` / auth wiring), not as ad hoc one-offs.

### Identity — IdP (OIDC / OAuth)

- **Use case:** Bind **admin console** login and/or **local node bootstrap** to the customer’s **IdP** (OIDC/OAuth first; SAML where the market requires it).
- **Today:** Phase 1 leaves **IDP out of scope**; `tenant_id` / `user_id` on control sessions come from **`config/runtime.yaml`** until IdP exists (`docs/setup/TENANCY.md`, `docs/adr/0004-deployment-tenant-from-runtime-config.md`). PKCE and provider OAuth for **model connections** already exist in Loopgate; **organizational IdP** for operators is a separate track.
- **Direction:** IdP **verifies** identity and **feeds** governance fields; Loopgate remains the **authority** for policy, audit, and capability tokens. Fail closed on token validation and session binding.

### Secrets — Vault / KMS / HSM / TPM

- **Use case:** Resolve model and integration credentials from **non-local** enterprise systems: **HashiCorp Vault**, **cloud KMS** (envelope encryption), **HSM**-backed vaults, and **TPM** / platform secure element for **machine identity** or sealing bootstrap material — while keeping resolution inside Loopgate’s trust model.
- **Today:** `SecretRef` + `SecretStore` in `internal/secrets/` with OS keyring as the primary desktop path (`docs/setup/SECRETS.md`). Single-org-per-local-install does **not** require tenant-suffixed secret keys on disk; **multi-tenant SaaS or shared runtimes** may — document per backend.
- **Direction:** Add backends or adapters behind the existing interface; operator docs for Vault paths, least-privilege policies, and rotation; no silent fallback from secure to env.

**Companion:** `AGENTS/BUILD_NOW.md` (“Explicitly out of scope”) and `docs/setup/SECRETS.md` (enterprise integration roadmap).

---

## Repository alignment (audit checklist)

Use this when the sprint doc and code drift. **Last verified against tree:** 2026-04-01.

| Phase / claim | Where to verify in repo |
|---------------|-------------------------|
| **1** Tenancy + memory partitions | `docs/setup/TENANCY.md`, `memory_partition.go`, ADR 0004, `tenancy_phase1_test.go` |
| **2** MCP stdio + dynamic tools | `cmd/loopgate/main.go` (`mcp-serve`), `internal/loopgate/mcpserve/`, `docs/setup/LOOPGATE_MCP.md`, ADR 0005 |
| **3** `goal.*` / `work.*` registry | `internal/tcl/memory_registry.go` (`explicitMemoryPrefixRules`), ADR 0006 |
| **4** Haven chat logging / audit | `server_haven_chat.go` (`haven_chat_*` diagnostic, `haven.chat*` audit) |
| **5** Admin console | `internal/loopgate/admin_console.go`, `loopgate --admin`, `config/runtime.yaml` → `admin_console`, `LOOPGATE_ADMIN_TOKEN`, `docs/setup/ADMIN_CONSOLE.md`, ADR 0016 |

**Doc drift fixed:** `LOOPGATE_MCP.md` — `LOOPGATE_MCP_TENANT_ID` / `LOOPGATE_MCP_USER_ID` match code (optional strings for personal mode; omit or empty when deployment tenant is unset).

---

## Phase completion log

Append rows here as phases ship:

| Phase | Completed (date) | Notes |
|-------|------------------|-------|
| 1 | 2026-04-01 (complete) | `controlSession` tenancy, session open from `runtime.yaml`, audit + `logEvent` (+ approval-path lock fix), morphling tenant mismatch → deny, memory **partitions** + migration + tests + `TENANCY.md`, ADR 0004, diagnostic **tenant_id** / **user_id** on audit-derived logs + Haven chat. Grants/secrets: instance-scoped for v1; Vault/IdP deferred (see § *Future enterprise integration layers*). |
| 2 | 2026-04-03 (complete) | `loopgate mcp-serve` now fetches dynamic capabilities and registers them as typed MCP tools (e.g., `memory.remember`, `fs_list`), passing correct environment telemetry through a configured `loopdiag` integration (`tenant_id`, `user_id`). Tests verify denial/success matching HTTP constraints. |
| 3 | 2026-04-03 (complete) | Added `goal.*` and `work.*` prefixes. Expanded preference facet mappings. Updated capabilities hint. Logged ADR 0006-explicit-memory-key-registry-compiled-until-signed-admin-distribution. |
| 4 | 2026-04-03 (complete) | Fixed Haven Chat panics (secured fail-closed route explicitly), appended missing loopdiag logs with tenant context to all HTTP early returns, added `haven.chat.error` audit telemetry, elongated local model timeout configs for `openai_compatible` inference, patched Swift array attachment force unwraps, repaired typing indicator. |
| 5 | 2026-04-01 (complete) | Loopback admin console v0: dual gate (`admin_console.enabled` + `--admin`), bcrypt-hashed `LOOPGATE_ADMIN_TOKEN`, `/admin/*` policy + redacted audit CSV/HTML + session list, tenant filter when `deployment_tenant_id` set, diagnostic `admin_console_*` events with deployment tenant id, tests for redirect/login/redaction/filter. |

---

## Review cadence

Before merging each phase: run `go test ./...`, reread changed paths against `AGENTS.md` invariants (audit failure = hard failure, no split locks on morphling/session/audit for one logical op, etc.), and add or update ADRs when a future reader would ask “why did they do it this way?” For any new failure or recovery path, confirm **diagnostic logs** appear at the configured level and remain free of secret-bearing fields.
