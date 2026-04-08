# ADR 0002: Single-node tenant model before multi-node

**Date:** 2026-04-01  
**Status:** accepted

## Decision

We implement **`tenant_id` (and related identity) on the local enforcement node first**, with hard isolation in storage and audit, and **defer** multi-node replication, admin-node transport, and IDP until that model is correct in single-node deployments.

## Tradeoff

Enterprise operators wait longer for “full hub-and-spoke” stories; the alternative is bolting tenant semantics onto an **IDE bridge stack** (e.g. historical in-tree MCP, now removed) or admin UI later and rewriting hot paths under time pressure.

## Consequences

If single-node tenant work stalls the market, we still must not fake isolation (empty results, permissive defaults). Partial delivery should be **feature-flagged surfaces**, not silent cross-tenant reads. When multi-node ships, migrate by teaching sync and admin APIs the same tenant fields already enforced locally.
