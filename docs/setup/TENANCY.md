# Tenancy (single-node)

Loopgate stamps **`tenant_id`** and **`user_id`** onto each **control session** when `/v1/session/open` succeeds. Values come only from operator config, not from the session-open JSON body.

## Configuration

In **`config/runtime.yaml`**:

```yaml
tenancy:
  deployment_tenant_id: ""
  deployment_user_id: ""
```

Leave both empty for **personal / desktop** deployments (default). Set non-empty strings when running a shared node that should partition audit and morphling state by org (Phase 1 uses this for **audit fields**, **diagnostic log attributes**, and **morphling tenant checks**).

## Validation

Identity strings may be up to **256** characters and must not contain ASCII control characters (NUL, CR, LF). Looser than `identifiers.ValidateSafeIdentifier` so future IDP subjects (e.g. emails) remain representable.

## On-disk memory (continuity) layout

Authoritative continuity state (JSON / JSONL, distillate artifacts, `continuity_tcl.sqlite`, etc.) lives under **`runtime/state/memory/`**, partitioned by deployment tenant:

| Session `tenant_id` (from config) | Directory under `memory/partitions/` |
|-----------------------------------|--------------------------------------|
| Empty (personal / default)        | `default/`                           |
| Non-empty                         | `t` + first 16 bytes of SHA-256 of the trimmed tenant string (hex), e.g. `tabc123...` — **raw tenant strings are not used in paths** |

Loopgate opens and mutates only the partition that matches the authenticated session’s `TenantID`. There is no cross-partition read or merge from the API.

### Upgrade migration (legacy layout)

If **`memory/partitions/`** does not exist yet but there are files or directories directly under **`memory/`** (the pre-partition layout), **`NewServer`** runs a one-time migration: each top-level entry except `partitions` is **`rename`**d into **`memory/partitions/default/`**. If the base directory is empty, **`memory/partitions/default/`** is created so new installs have a stable path.

If **`memory/partitions/`** already exists as a directory, migration is a **no-op** (safe to call repeatedly). Operators who still have legacy files at the root after a partial upgrade should move them into `partitions/default/` manually or restore from backup; Loopgate does not merge two layouts automatically.

### Haven memory reset

**`POST /v1/ui/memory/reset`** archives and clears **only the caller’s tenant partition** (the session’s `TenantID`), not every tenant on the node.

## Related code

- Session minting: `internal/loopgate/server_model_handlers.go` (`handleSessionOpen`)
- Audit enrichment: `internal/loopgate/server.go` (`logEvent`, `tenantUserForControlSession`)
- Morphling isolation: `internal/loopgate/morphlings.go` (`morphlingTenantDenied`)
- Memory partition keys, migration, per-tenant state: `internal/loopgate/memory_partition.go` (`memoryPartitionKey`, `maybeMigrateMemoryToPartitionedLayout`, `ensureMemoryPartitionLocked`)
- ADR: `docs/adr/0004-deployment-tenant-from-runtime-config.md`

## Enterprise identity and secrets (roadmap)

Today, deployment identity is **config-driven**. Future work will add **customer IdP (OIDC/OAuth)** for operators and **enterprise secret backends** (Vault, KMS, HSM-class, TPM/bootstrap patterns) as first-class integration layers — same phased plan, RFC-first: `sprints/2026-04-01-loopgate-enterprise-phased-plan.md` § *Future enterprise integration layers*; secrets detail in `docs/setup/SECRETS.md` § *Enterprise integration layer*.
