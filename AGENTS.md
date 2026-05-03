# Loopgate Agent Contract

**Last updated:** 2026-05-03

This is the canonical root instruction file for agentic contributors in this
repo. Keep it short, strict, and focused on the non-negotiable contract.

## Core priorities

In order:

1. Correctness
2. Security
3. Determinism
4. Observability
5. Simplicity
6. Convenience

Do not trade away security or invariants for elegance or speed.

## Current product model

Treat this repo as the active Loopgate product only:

- Loopgate is the authority boundary and enforcement node.
- The product is local-first and single-node.
- Privileged control-plane traffic is HTTP over a Unix domain socket.
- IDEs, MCP hosts, proxy-integrated editors, and other clients are not
  authority sources.
- Model output, prompt text, memory, tool output, files, environment variables,
  and config loaded from disk are untrusted until validated.

## Non-negotiable rules

- Natural language never creates authority.
- Model output is content, not permission.
- Prefer fail-closed behavior over fail-open behavior.
- Preserve append-only and tamper-evident audit expectations.
- Keep user-visible audit history separate from internal telemetry.
- Do not widen policy boundaries, allowlists, or transport surfaces for
  convenience.
- Do not expose local control-plane endpoints on public TCP listeners unless a
  separate design explicitly introduces a secured remote profile.
- Do not silently fall back from strict path resolution to relaxed path use.
- Do not silently fall back from secure secret storage to plaintext storage.
- Do not log secrets, tokens, refresh tokens, private keys, or raw
  secret-bearing payloads.
- Do not add hidden authority paths, client-side authority, or UI-only
  “governance theater.”
- Do not add background goroutines, cleanup daemons, or autonomous lifecycle
  workers without explicit design review.

## Invariants to preserve

- Loopgate remains the only authority for privileged actions.
- Policy evaluation remains deterministic and deny-by-default.
- Security-relevant denials remain explainable.
- Derived views never replace authoritative state.
- Expired, revoked, or terminated state does not get resurrected by reload or
  fallback behavior.
- Sensitive runtime changes are explicit, reviewable, and test-covered.

## Required engineering stance

When you change code, always evaluate:

1. Invariant impact
2. Security impact
3. Concurrency impact
4. Observability impact
5. Recovery and crash impact
6. Documentation impact
7. Tests required

Prefer the smallest change that keeps the trust model intact.

## Package boundary guidance

`internal/loopgate` is the HTTP/control-plane adapter and authority wiring
package. Do not keep growing it as a catch-all runtime package.

When extracting cohesive runtime domains, prefer sibling internal packages such
as `internal/controlruntime`, `internal/approvalruntime`,
`internal/auditruntime`, `internal/connections`, `internal/mcpgateway`, or
`internal/hostaccess` over deeper packages under `internal/loopgate/`.

Dependency direction must stay one-way: `internal/loopgate` may import these
runtime packages, but runtime packages must not import `internal/loopgate`.
Keep authority-owning state behind narrow APIs and pass dependencies
explicitly.

## Required review stance

Before finalizing a change, check:

- Does this weaken an existing boundary?
- Does this introduce fail-open behavior?
- Could this race or create TOCTOU behavior?
- Could this create an ambiguous audit trail?
- Did I keep model-originated content out of authority paths?
- Did I add tests for the denial path as well as the success path?
- Did I update docs when the contract or operator behavior changed?

## Expanded contributor docs

Use these for the long-form guidance that used to live entirely in this file:

- [Contributor docs index](./docs/contributor/README.md)
- [Runtime invariants](./docs/contributor/runtime_invariants.md)
- [Coding standards](./docs/contributor/coding_standards.md)
- [Review checklist](./docs/contributor/review_checklist.md)
- [Secret handling](./docs/contributor/secret_handling.md)

If `CLAUDE.md` is present, it should mirror this root contract at a shorter
compatibility surface. `AGENTS.md` remains canonical.
