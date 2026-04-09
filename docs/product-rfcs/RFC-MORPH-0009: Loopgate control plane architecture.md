**Last updated:** 2026-04-03

# RFC-MORPH-0009: Loopgate control plane architecture

- **Status:** Draft — target / north-star for the **Loopgate** daemon (`cmd/loopgate/`)
- **Primary authority:** **Loopgate** (policy, tokens, sandbox mediation, morphlings, audit)
- **Normative revision:** 2026-03-24

---

# Summary

**Loopgate** is the **kernel** of the governed system: the local control plane that enforces policy, issues capability and approval tokens, mediates the sandbox, owns morphling lifecycle, resolves secrets, and appends authoritative control-plane audit. **Unprivileged operator clients** (IDE, Haven/reference shell, and **out-of-tree** MCP→HTTP forwarders) attach over the local socket; Loopgate is the **authority boundary**. **In-tree MCP is deprecated and removed** (ADR 0010).

This RFC is a **future-state design target**. Much of the token, approval, morphling, and memory path is implemented; the full `/morph/home` product layout and every invariant below may still be partially mapped to repo-local paths—see RFC-MORPH-0004 and implementation code.

## Local transport (v1 vs future)

- **v1 (current product standard):** privileged **local clients** use **HTTP** on a **Unix domain socket** control-plane binding, with session binding, signed requests, and replay protection as described in `docs/rfcs/0001-loopgate-token-policy.md` and AMP local-transport docs.
- **Future (TBD):** **Apple XPC** (or similar Mach-bound IPC) is **optional hardening** on macOS with **no committed ship date**. It does not replace Loopgate as the authority boundary; it would only change how a desktop process invokes the control plane. See `docs/rfcs/0001-loopgate-token-policy.md` and `docs/loopgate-threat-model.md`.

---

# Architecture overview (control plane + workers)

```
                          ┌─────────────────────┐
                          │        User         │
                          │  intent / approval  │
                          └──────────┬──────────┘
                                     │
                                     ▼
                    ┌────────────────────────────────┐
                    │        Operator client         │
                    │   operator client (RFC-0001)     │
                    └───────────────┬────────────────┘
                                    │ structured requests
                                    ▼
                    ┌────────────────────────────────┐
                    │            Loopgate            │
                    │ strict control plane / kernel  │
                    │                                │
                    │ - policy enforcement           │
                    │ - token issuance               │
                    │ - approval gating              │
                    │ - filesystem mediation         │
                    │ - provider/integration access  │
                    │ - audit / ledger append        │
                    │ - export / promotion           │
                    └───────┬───────────────┬────────┘
                            │               │
            scoped tokens   │               │ controlled ops
                            ▼               ▼
              ┌────────────────────┐   ┌──────────────────────┐
              │     Morphlings     │   │   Loopgate-owned     │
              │ disposable workers │   │   protected state    │
              │                    │   │                      │
              │ - single task      │   │ /morph/config        │
              │ - bounded scope    │   │ /morph/policy        │
              │ - bounded lifetime │   │ /morph/state         │
              │ - artifact output  │   │ /morph/var           │
              └─────────┬──────────┘   │ ledger / trust / keys│
                        │              └──────────────────────┘
                        │
                        ▼
               ┌──────────────────────┐
               │   /morph/home        │
               │   sandbox filesystem │
               │                      │
               │ workspace/           │
               │ imports/             │
               │ outputs/             │
               │ scratch/             │
               │ agents/              │
               │ quarantine/          │
               │ tmp/                 │
               │ logs/                │
               └──────────────────────┘
```

---

# Filesystem model

Loopgate owns **`/morph`** (conceptual product root). The operator client and morphlings operate only inside **`/morph/home`** unless Loopgate explicitly mediates export/import.

Example layout:

```
/morph

  /config
  /policy
  /state
  /var

  /home
```

Sandbox (mutable / input / untrusted zones):

```
/morph/home

workspace/
imports/
outputs/
scratch/
agents/
quarantine/
tmp/
logs/
```

**Mutable zones:** workspace, scratch, outputs, agents  
**Input zones:** imports  
**Untrusted zones:** quarantine  
**Protected Loopgate zones:** `/morph/config`, `/morph/policy`, `/morph/state`, `/morph/var`

The client cannot modify protected zones.

---

# Symlink handling

Loopgate **must** resolve and canonicalize paths before access:

1. resolve symlink
2. canonicalize path
3. verify containment inside the allowed sandbox root
4. **deny** if outside scope

Symlinks must never escape the sandbox.

---

# Morphling lifecycle (authority)

High-level state flow (detail in RFC-MORPH-0008):

```
Plan → Authorize → Spawn → Execute → Produce artifacts
→ Stage changes → Operator review (via client UI) → Promote / export → Terminate
```

Morphlings terminate when the task completes or Loopgate ends the lifecycle. Only **staged** artifacts persist subject to promotion rules.

---

# Approval flow (authority)

```
User request
→ Operator client forwards plan / capability request
→ Loopgate authorization
→ Spawn / execute under Loopgate
→ Morphling executes
→ Operator client summarizes **projected** status for operator
→ User approval decision via Loopgate
→ Loopgate promotion / export
```

Loopgate is always the **final** authority for side effects.

---

# Non-negotiable invariants (control plane)

1. Nothing outside `/morph/home` is writable by morphlings without explicit Loopgate-mediated promotion.
2. Morphlings cannot inherit the full authority of the operator session; scopes are **derived** from class policy.
3. Model output is never authority.
4. All external side effects require Loopgate mediation.
5. All exports require explicit approval.
6. Control-plane security audit is append-only where the design requires it.
7. Symlinks cannot escape sandbox boundaries.
8. Quarantined content is untrusted until promoted.
9. Durable persistence remains purgeable per product policy.
10. Approvals map to narrow, machine-enforced scopes.

---

# Implementation phases (historical roadmap sketch)

These phases overlap with RFC-MORPH-0007 and current code; kept as a **sequencing mental model**:

## Phase 1 — Sandbox boundary

- enforce `/morph/home` as the conceptual agent root
- enforce filesystem zones
- validate symlink containment

## Phase 2 — Operator requests (client)

- no direct tool execution from model output; typed Loopgate requests only

## Phase 3 — Morphling runtime

- strict task schema; spawn / run / terminate under Loopgate

## Phase 4 — Approval system

- scoped approvals; capability tokens; export/promotion gating

## Phase 5 — Persistence

- wake-state continuity; purge semantics; governed memory (RFC-MORPH-0005, `docs/rfcs/0009`–`0010`)

---

# Product identity (control plane)

Loopgate is the **deliberate kernel** that keeps AI-assisted work inside **policy**, **audit**, and **bounded workers**—not ad-hoc shell access.
