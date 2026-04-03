**Last updated:** 2026-03-29

# RFC-MORPH-0001: Haven operator client architecture

- **Status:** Draft — target / north-star for the **Haven** desktop shell
- **Implementation note:** The **shipped** operator client is the **native Swift/macOS Haven app** (separate repository; typical path `~/Dev/Haven`). This repository’s **`cmd/haven/`** Wails+React shell is **reference-only / deprecated** for product UX—use it for contracts, tests, and parity experiments, not as the canonical frontend.
- **Primary authority:** **Haven** as the sole shipped operator UI; **Loopgate** remains the only policy and execution authority
- **Normative revision:** 2026-03-24 (split from former combined operator RFC)
- **Pairs with:** [RFC-MORPH-0009 — Loopgate control plane architecture](./RFC-MORPH-0009:%20Loopgate%20control%20plane%20architecture.md)

---

# Summary

**Haven** is the **only** user-shipped operator client for the **Loopgate-centered** system (consumer demo path). In production this is a **native desktop app** (Swift); an older **Wails/React + Go** prototype remains under `cmd/haven/` in **this** repository **for reference and tests only**. Haven handles conversation, session state, continuity **thread projection**, approval UX, and structured control-plane calls. It does **not** implement policy, hold secrets as authority, or execute host-affecting tools without Loopgate.

**Loopgate** is the primary product and authority boundary; **morphlings** are Loopgate-governed workers. There is **no separate Morph CLI** in the supported product path.

For tokens, sandbox zones, morphling lifecycle **authority**, symlink policy, and append-only control-plane audit, read **RFC-MORPH-0009**.

---

# Architecture (client view)

```
                    ┌─────────────────────┐
                    │        User         │
                    │  intent / approval  │
                    └──────────┬──────────┘
                               │
                               ▼
              ┌────────────────────────────────┐
              │            Haven               │
              │   desktop operator client      │
              │                                │
              │ - chat & workspace UI          │
              │ - prompt compilation           │
              │ - session / continuity threads │
              │ - approval & capability UX     │
              │ - Loopgate API consumer        │
              └───────────────┬────────────────┘
                              │ typed HTTP (Unix socket)
                              ▼
              ┌────────────────────────────────┐
              │           Loopgate             │
              │   (see RFC-MORPH-0009)         │
              └────────────────────────────────┘
```

---

# Design goals

## 1. Continuity (operator experience)

Haven should feel like the **same assistant across runs** without treating raw chat as durable truth.

Continuity on the client is:

- bounded (thread roles, ledger discipline)
- provenance-bearing (what came from model vs operator vs Loopgate)
- purgeable (operator can drop thread context)
- clearly **historical** vs **fresh**

Durable wake state, distillates, and governed recall remain **Loopgate-owned** (RFC-MORPH-0005, RFC-MORPH-0009).

## 2. Delegation

Haven **requests** morphling work and **displays** Loopgate-projected status. It does not spawn workers or mint capability scope by itself.

Morphlings are single-task, capability-limited, short-lived workers under **Loopgate** lifecycle authority (RFC-MORPH-0002, RFC-MORPH-0008).

## 3. Constrained client

Haven must not:

- write arbitrary host paths
- bypass approval for export or promotion
- treat model output as capability arguments without validation and Loopgate mediation

## 4. Approvals (UX)

Approvals should be **fast for the operator** and **strict inside Loopgate**: object-scoped, time-scoped, capability-scoped (RFC-MORPH-0006, RFC-MORPH-0009).

---

# Operator workflows (summary)

## Import / export

1. Operator selects import/export through Haven UI.
2. Loopgate mediates filesystem and promotion boundaries (RFC-MORPH-0004, RFC-MORPH-0006).

Concrete path rules and symlink handling are **Loopgate** concerns (RFC-MORPH-0009).

## Morphling coordination

At a high level:

```
Plan → authorize (Loopgate) → spawn (Loopgate) → execute → artifacts
→ stage → operator review (Haven) → promote/export (Loopgate)
```

Haven shows **display-safe** summaries (class, state, counts—not raw model goal text in public projections).

---

# Morphling classes (operator-facing templates)

Suggested templates the operator may choose (policy maps scopes in Loopgate):

- Reviewer
- Editor
- Tester
- Researcher
- Refactorer
- Builder

---

# Client-side invariants

1. Haven does not modify Loopgate policy, secret material, or authoritative audit logs.
2. Natural language is never permission.
3. Every privileged action is a Loopgate request with valid session and token binding.
4. UI surfaces use Loopgate-classified **projections** for morphlings and memory where raw strings are unsafe.

---

# Product identity

**Haven** is the desktop where you work with AI **inside deliberate, Loopgate-enforced boundaries**—not across your whole machine by default.

```
"Haven is a workspace shell, not ambient root access."
```
