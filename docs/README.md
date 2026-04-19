**Last updated:** 2026-04-16

# Docs Index

This repository documents a single product:

**Loopgate** — a local-first governance layer that tracks and constrains what your AI is doing on your machine.

The current active story is:
- Loopgate
- Claude Code hooks
- signed policy
- approvals
- local hash-linked audit
- default-on audit HMAC checkpoints on macOS
- governed local MCP/runtime work

Use the documents below as the current source of truth for the active product.
Internal review artifacts and temporary hardening notes now live under
[`notes/`](../notes/README.md) and are not part of the public operator path.

## Start here

- [Getting started](./setup/GETTING_STARTED.md)
- [Operator guide](./setup/OPERATOR_GUIDE.md)
- [Setup](./setup/SETUP.md)
- [Policy reference](./setup/POLICY_REFERENCE.md)
- [Doctor and ledger tools](./setup/DOCTOR_AND_LEDGER.md)
- [Loopgate HTTP API (local clients)](./setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md)
- [Policy signing](./setup/POLICY_SIGNING.md)
- [Ledger & audit integrity](./setup/LEDGER_AND_AUDIT_INTEGRITY.md)
- [Threat model](./loopgate-threat-model.md)
- [Active product gaps](./roadmap/loopgate_v1_product_gaps.md)
- [Loopgate V1 hardening plan](./roadmap/loopgate_v1_hardening_plan.md)
- [Release candidate checklist](./roadmap/release_candidate_checklist.md)
- [Changelog](../CHANGELOG.md)
- [Support](../SUPPORT.md)
- [Security reporting](../SECURITY.md)

## Current product docs

- [Claude Code hooks MVP](./design_overview/claude_code_hooks_mvp.md)
- [Claude Code authority surfaces threat model](./design_overview/claude_code_authority_surfaces_threat_model.md)
- [Loopgate design overview](./design_overview/loopgate.md)
- [Architecture](./design_overview/architecture.md)
- [Locking model](./design_overview/loopgate_locking.md)
- [RFC 0001: Loopgate Token and Request Integrity Policy](./rfcs/0001-loopgate-token-policy.md)
- [RFC 0016: Claude tool policy surface and governed MCP gateway](./rfcs/0016-claude-tool-policy-and-mcp-gateway.md)

## Operator and setup docs

- [Getting started](./setup/GETTING_STARTED.md)
- [Operator guide](./setup/OPERATOR_GUIDE.md)
- [Setup](./setup/SETUP.md)
- [Doctor and ledger tools](./setup/DOCTOR_AND_LEDGER.md)
- [Secrets](./setup/SECRETS.md)
- [Tool usage](./setup/TOOL_USAGE.md)

## Historical material

Older product notes, extracted continuity design docs, and related historical
material live in the separate `ARCHIVED` and `continuity` sibling repositories.
Internal review and hardening notes that still matter to maintainers but are
not part of the public docs surface live under [`notes/`](../notes/README.md).
