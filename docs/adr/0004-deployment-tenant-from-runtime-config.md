# ADR 0004: Deployment tenant from runtime config (not client JSON)

**Date:** 2026-04-01  
**Status:** accepted

## Decision

`tenant_id` and `user_id` on each control session are copied from **`config/runtime.yaml` → `tenancy.deployment_tenant_id` / `deployment_user_id`** at **session open**; clients cannot supply them in `OpenSessionRequest`. Personal / single-user mode keeps both **empty strings**, preserving prior behavior.

## Tradeoff

Operators must restart Loopgate or rely on config reload semantics when tenancy changes; the alternative (client-supplied tenant) would be an immediate trust bypass.

## Consequences

When IDP-backed identity ships, replace or augment this static YAML path with verified claims while keeping the same session fields so audit, morphling records, and diagnostic logs stay aligned.
