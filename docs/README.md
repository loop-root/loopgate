**Last updated:** 2026-04-13

# Docs Index

This repository is being normalized around a single product:

**Loopgate** — a local-first governance layer that tracks and constrains what your AI is doing on your machine.

The current active story is:
- Loopgate
- Claude Code hooks
- signed policy
- approvals
- local audit
- governed local MCP/runtime work

The docs still contain older material about Haven, Morph, morphlings, memory-heavy flows, and future enterprise directions. Those are being trimmed or archived. Use the documents below as the current source of truth for the active product.

## Start here

- [Operator guide](./setup/OPERATOR_GUIDE.md)
- [Setup](./setup/SETUP.md)
- [Loopgate HTTP API (local clients)](./setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md)
- [Policy signing](./setup/POLICY_SIGNING.md)
- [Ledger & audit integrity](./setup/LEDGER_AND_AUDIT_INTEGRITY.md)
- [Threat model](./loopgate-threat-model.md)
- [Loopgate cleanup plan](./roadmap/loopgate_cleanup_plan.md)

## Current product docs

- [Claude Code hooks MVP](./design_overview/claude_code_hooks_mvp.md)
- [Claude Code authority surfaces threat model](./design_overview/claude_code_authority_surfaces_threat_model.md)
- [Loopgate design overview](./design_overview/loopgate.md)
- [Architecture](./design_overview/architecture.md)
- [How It Works](./design_overview/how_it_works.md)
- [RFC 0001: Loopgate Token and Request Integrity Policy](./rfcs/0001-loopgate-token-policy.md)
- [RFC 0016: Claude tool policy surface and governed MCP gateway](./rfcs/0016-claude-tool-policy-and-mcp-gateway.md)

## Operator and setup docs

- [Operator guide](./setup/OPERATOR_GUIDE.md)
- [Setup](./setup/SETUP.md)
- [Secrets](./setup/SECRETS.md)
- [Tool usage](./setup/TOOL_USAGE.md)

## Cleanup / archive note

Not all docs under `docs/` reflect the current product boundary.

Known categories under cleanup:
- Haven / Morph compatibility language
- morphling-specific design and product RFCs
- memory-heavy design work that is not part of the current Claude v1 path
- multi-tenant and admin-node forward-looking material
- docs containing hardcoded local filesystem paths

Those materials are not deleted in this slice. They are being tracked in:
- [Loopgate cleanup plan](./roadmap/loopgate_cleanup_plan.md)
