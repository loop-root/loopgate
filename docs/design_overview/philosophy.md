**Last updated:** 2026-04-01

# Philosophy

## Authority, not charisma

**Natural language is not permission.** Nothing the model says can mint capabilities, widen paths, or skip approvals. The **control plane** (Loopgate) is the only place authority is created, scoped, and spent.

## Three roles

1. **Operator (human)** — intent, review, and explicit approval.
2. **Client (IDE, CLI, native UI, or TUI shell)** — conversation, planning, rendering, and **unprivileged** session state.
3. **Loopgate** — policy, tokens, secrets, sandbox mediation, governed MCP mediation, and **authoritative** audit for those actions.

## Untrusted content by default

Model output, tool output, files, env, config, and memory strings are **content** until validated. Prompt compilation may include them; **policy and types** decide what may execute or persist.

## Deny by default

If a path, capability, or promotion is not explicitly allowed, it is denied. Fail **closed** when validation or audit append cannot complete for security-relevant actions.

## Observable and bounded

Security-relevant transitions should be **explainable**: typed denials,
append-only history, separation between **operator-visible** ledgers and
**internal** telemetry.

## Local-first v1

v1 is **single-user, local transport** (operator clients talk to Loopgate over **HTTP on a Unix domain socket**). Treat any future remote or multi-tenant profile as a **new design**, not a stretched default.

## Why older RFC IDs are stable

Older product RFCs, worker/runtime specs, and historical continuity material have been moved to the separate `ARCHIVED` repository. Treat the active Loopgate design docs in this repo as the current source of truth.
