# Loopgate

**Last updated:** 2026-04-14

**Loopgate** is a local-first governance layer for AI-assisted engineering work.

Current product scope:
- govern what your AI tools are allowed to do
- require approval for higher-risk actions
- record a durable local audit trail of what happened
- provide a local control plane for hook-based harnesses and governed tool execution
- verify local audit integrity with hash-chain and optional HMAC checkpoint tooling

**Current MVP harness:** **Claude Code + project hooks + Loopgate**

This repository documents and ships a single product: **Loopgate**.

## Project status

- security-sensitive and experimental
- local-first
- governance-focused
- not yet a stable compatibility target

## What Loopgate does

Most AI harnesses give the model broad ambient authority and rely on prompt discipline to keep it safe.

Loopgate takes the opposite position:

- natural language is never authority
- model output is untrusted input
- policy decides what can run
- approvals are explicit
- audit is local and durable

For the current local-first product, that means:

- Claude Code hooks can be governed before tool execution
- risky actions can be denied or approval-gated
- low-risk actions can be allowed with audit
- governed MCP execution can run through Loopgate’s own broker path
- local audit stays authoritative even if later export or aggregation changes

## Active product surface

What is active now:
- Loopgate server on a local Unix socket
- signed policy
- Claude Code hook governance
- approval and denial recording
- append-only local audit ledger
- request-driven governed MCP broker work
- local operator CLI flows

What is not the active product story right now:
- a new desktop UI surface
- a separate assistant product
- legacy worker/runtime experiments
- distributed enterprise deployment
- automatic memory or continuity inside Loopgate

## Quick start

```bash
go test ./...
go run ./cmd/loopgate
```

Default local socket:

```text
runtime/state/loopgate.sock
```

Loopgate uses a signed policy:

```bash
go run ./cmd/loopgate-policy-sign -verify-setup
go run ./cmd/loopgate-policy-admin validate
```

If Loopgate is already running:

```bash
go run ./cmd/loopgate-policy-admin apply -verify-setup
```

## Operator flow

The current practical operator flow is:

1. start Loopgate locally
2. connect Claude Code hooks to the local socket
3. tune signed policy for your real low-risk vs approval-required actions
4. inspect local audit when something is denied, approved, or surprising
5. hot-apply policy changes without restarting Loopgate

Start here:
- [Operator guide](./docs/setup/OPERATOR_GUIDE.md)
- [Setup](./docs/setup/SETUP.md)
- [HTTP API for local clients](./docs/setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md)
- [Policy signing](./docs/setup/POLICY_SIGNING.md)
- [Ledger and audit integrity](./docs/setup/LEDGER_AND_AUDIT_INTEGRITY.md)
- [Threat model](./docs/loopgate-threat-model.md)
- [Support](./SUPPORT.md)
- [Security reporting](./SECURITY.md)

## Repository layout

```text
cmd/loopgate/              primary Loopgate server
cmd/loopgate-policy-sign/  policy signing CLI
cmd/loopgate-policy-admin/ policy validate/diff/explain/apply CLI
cmd/loopgate-doctor/       operator diagnostics CLI
internal/loopgate/         Loopgate control plane and governed runtime
core/policy/               signed policy files
config/                    runtime configuration
docs/                      setup, operator docs, architecture, cleanup plan
runtime/                   local state and logs (fully gitignored)
```

## Related repositories

Loopgate’s memory and continuity work now lives in the separate sibling
repository named `continuity`, so this repo can stay focused on:

- policy
- approvals
- audit
- Claude hook governance
- sandbox mediation
- governed MCP broker flows

Historical design notes and older product planning that no longer describe the
current Loopgate product have been moved to the separate `ARCHIVED`
repository.

## Status

Experimental and under active hardening.

For current behavior, prefer the operator-facing docs in [docs/](./docs), the
running code, and the signed policy files under [core/policy/](./core/policy).
Historical material lives in the `ARCHIVED` and `continuity` sibling repos.

## License

Loopgate is licensed under the Apache License, Version 2.0. See
[LICENSE](./LICENSE) and [NOTICE](./NOTICE).

## Support

For setup questions, non-sensitive bug reports, and operator workflow issues,
see [SUPPORT.md](./SUPPORT.md). For vulnerability reports or trust-boundary
issues, use the private path described in [SECURITY.md](./SECURITY.md).
