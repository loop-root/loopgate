**Last updated:** 2026-04-01

# Philosophy

## Authority, not charisma

**Natural language is not permission.** Nothing the model says can mint capabilities, widen paths, or skip approvals. The **control plane** (Loopgate) is the only place authority is created, scoped, and spent.

## Three roles

1. **Operator (human)** — intent, review, and explicit approval.
2. **Client (IDE, CLI, native UI, or proxy-integrated editor)** — conversation, planning, rendering, and **unprivileged** session state.
3. **Loopgate** — policy, tokens, secrets, sandbox mediation, morphling lifecycle, and **authoritative** audit for those actions.

Morphlings are **not** a fourth public tier; they are **Loopgate-governed** workers with derived envelopes.

## Untrusted content by default

Model output, tool output, files, env, config, and memory strings are **content** until validated. Prompt compilation may include them; **policy and types** decide what may execute or persist.

## Deny by default

If a path, capability, or promotion is not explicitly allowed, it is denied. Fail **closed** when validation or audit append cannot complete for security-relevant actions.

## Observable and bounded

Security-relevant transitions should be **explainable**: typed denials, append-only history, separation between **operator-visible** ledgers and **internal** telemetry. Memory and recall stay **bounded** (sleep/wake, inspection, TCL governance)—not an unbounded transcript dump.

## Local-first v1

v1 is **single-user, local transport** (operator clients talk to Loopgate over **HTTP on a Unix domain socket**). Treat any future remote or multi-tenant profile as a **new design**, not a stretched default.

## Why `RFC-MORPH-*` IDs are stable

Specs under `docs/product-rfcs/` describe **Loopgate** (primary), **operator clients**, sandbox, continuity, and **morphlings** (bounded workers). The **`RFC-MORPH-*`** prefix is a **legacy stable ID** for links, not the public product name. See [`docs/product-rfcs/README.md`](../product-rfcs/README.md) for the index (0001 = operator client architecture, 0009 = Loopgate kernel).
