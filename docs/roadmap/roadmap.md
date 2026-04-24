**Last updated:** 2026-04-24

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
- assistant-persona products
- in-tree continuity or memory
- remote or multi-node deployment

The near-term operator UI direction is a local admin-console TUI over the
existing Loopgate authority APIs. It is an operator/admin surface, not a new
authority boundary and not a remote management plane.

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
- make the value proposition explicit: fewer rubber-stamp prompts, stronger
  policy clarity, and better audit visibility
- define and build the local admin-console TUI as a thin client over existing
  Loopgate authority surfaces
- improve readable audit and ledger views for demos and troubleshooting
- document common failure and recovery paths clearly

### 3. Governance hardening

- keep signed policy as the only accepted policy authority
- preserve fail-closed hook behavior
- keep the governed MCP path request-driven and auditable
- tighten the append-only audit story, especially around tamper and replacement gaps
- execute the review-driven local-core hardening plan in `docs/roadmap/loopgate_v1_hardening_plan.md`

### 4. Core simplification

- continue removing retired legacy seams from active code and docs
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

1. sharpen the public setup and value story for local governed Claude work
2. implement the first admin-console TUI slice without moving authority into the UI
3. finish the open-source sanitization sweep on active files and defaults
4. remove the last unnecessary historical naming from active comments, tests, and maps
5. harden the local audit integrity story further
6. keep the operator setup and demo workflows tight and readable

## Implementation roadmaps

- `loopgate_v1_hardening_plan.md` — correctness and security fixes from the 2026-04-15 review
- `loopgate_v1_product_gaps.md` — product and UX improvements for operator confidence and OSS launch readiness
- `admin_console_tui_mvp.md` — local admin-console TUI scope and authority contract
- `harness_usability_execution_plan.md` — focused plan for Claude usability and future harness readiness
