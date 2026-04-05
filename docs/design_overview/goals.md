**Last updated:** 2026-03-24

# Goals

## Product goals

- **Governed AI execution** — Use models on real work without treating prompt text or completions as authority.
- **Explicit boundary** — **Loopgate** is the primary product and authority for policy, capabilities, secrets, sandbox, audit, and morphlings; **operator clients** attach via MCP, proxy, or local HTTP.
- **Auditable workflow** — Approvals, denials, lifecycle transitions, and memory governance are recorded in structured, reviewable ways.
- **Bounded agents** — **Morphlings** run as Loopgate-scoped workers, not self-authorizing background processes.

## Engineering goals

- **Deterministic policy** — Same inputs and state → same allow/deny decision where the design requires it.
- **Defense in depth** — Transport binding, session MAC, replay protection, approval nonces, symlink-safe paths, redaction before persistence.
- **Clear ownership** — Client ledgers vs Loopgate event logs; projected UI fields vs raw internal records.

## Non-goals (v1)

- Public network API for Loopgate or morphlings.
- “Give the model shell” without capability and policy mediation.
- Treating continuity as an immutable full chat archive.

For a feature-level snapshot of what is implemented vs planned, see [`docs/roadmap/roadmap.md`](../roadmap/roadmap.md). For norms and invariants, see [`docs/loopgate-threat-model.md`](../loopgate-threat-model.md) and the numbered RFCs under [`docs/rfcs/`](../rfcs/).
