# Loopgate Claude Contract

**Last updated:** 2026-04-20

`AGENTS.md` is the canonical instruction file for this repo. This file exists
as a compatibility entrypoint for Claude-oriented tooling and repeats only the
short non-negotiable contract.

## Non-negotiable rules

- Loopgate is the authority boundary.
- The active product is local-first, single-node, and uses HTTP over a Unix
  domain socket for privileged local transport.
- Clients, prompts, model output, tool output, files, environment variables,
  and config loaded from disk are untrusted until validated.
- Natural language never creates authority.
- Prefer fail-closed behavior over fail-open behavior.
- Preserve append-only and tamper-evident audit expectations.
- Keep user-visible audit history separate from internal telemetry.
- Do not widen policy, transport, path, or secret-storage boundaries for
  convenience.
- Do not log secrets or raw secret-bearing payloads.
- Do not add hidden authority paths or client-side “fake authority.”
- Do not add background autonomous lifecycle workers without explicit design
  review.

## Contributor docs

- [Canonical root contract](./AGENTS.md)
- [Runtime invariants](./docs/contributor/runtime_invariants.md)
- [Coding standards](./docs/contributor/coding_standards.md)
- [Review checklist](./docs/contributor/review_checklist.md)
- [Secret handling](./docs/contributor/secret_handling.md)
