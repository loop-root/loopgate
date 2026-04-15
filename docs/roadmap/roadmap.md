**Last updated:** 2026-04-14

# Loopgate roadmap

This is the current near-term roadmap for **Loopgate** as a single product.

Loopgate's active product scope is:
- local-first governance
- signed policy
- explicit approvals
- append-only local audit
- Claude Code hook governance
- governed local MCP broker work

This is not the active roadmap for:
- Haven as a product
- Morph as a separate assistant product
- morphlings
- in-tree continuity or memory
- multi-tenant or admin-node deployment

Historical roadmap material has been moved to the separate `ARCHIVED`
repository. Continuity-specific planning now belongs in the separate
`continuity` repository.

## Current priorities

### 1. Open-source readiness

- remove tracked runtime and sandbox artifacts
- archive stale internal planning and old subsystem docs
- sanitize hardcoded local paths from active docs and tests
- trim remaining legacy naming that no longer matches the product boundary

### 2. Operator usability

- keep setup and operator docs current
- make hook install/remove flows straightforward
- improve readable audit and ledger views for demos and troubleshooting
- document common failure and recovery paths clearly

### 3. Governance hardening

- keep signed policy as the only accepted policy authority
- preserve fail-closed hook behavior
- keep the governed MCP path request-driven and auditable
- tighten the append-only audit story, especially around tamper and replacement gaps

### 4. Core simplification

- continue removing retired Haven/Morph-era seams from active code and docs
- keep Loopgate focused on governance, not assistant behavior or memory
- reduce unnecessary dependencies and local-only baggage

## Definition of "clean enough"

The repository is in the right shape when:

- a new reader sees one product, not several
- the active docs match the active code paths
- local runtime state is no longer tracked in git
- current operator flows are documented and testable
- old continuity and product-experiment material is archived elsewhere

## Next useful slices

1. finish the open-source sanitization sweep on active files and defaults
2. remove the last unnecessary historical naming from active comments, tests, and maps
3. harden the local audit integrity story further
4. keep the operator setup and demo workflows tight and readable
