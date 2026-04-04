# Admin console (v0)

**Last updated:** 2026-04-01

The admin console is a **loopback-only** HTTP surface for operators: **Dashboard** (live sessions, connections, subordinate-runtime load, rolling model-chat token sums from the audit tail, ledger size, policy gate counts), structured **capability policy** (field-level, with hover “?” help), **configuration** (read-only view of `config/runtime.yaml` + memory backend selector), **audit** (filter form, table, redacted CSV export), and **control sessions**. It shares the same Loopgate process and state as the Unix socket control plane. Operator copy is product-agnostic; ledger wire types and config keys may still use historical identifiers. Editing config/policy still happens in YAML on disk and requires restart — the UI is inspect-first, not a silent write path.

## Security model (v0)

- **No IdP** in v0; see `docs/adr/0016-admin-console-v0-auth.md`.
- **Dual gate:** TCP listener starts only when **`admin_console.enabled: true`** in `config/runtime.yaml` **and** the process is started with **`loopgate --admin`**.
- **Bootstrap secret:** Set **`LOOPGATE_ADMIN_TOKEN`** to a high-entropy value (minimum **24 characters**). At startup Loopgate hashes it with **bcrypt**; the login form checks the submitted token with `bcrypt.CompareHashAndPassword`. The raw token is **not** stored on disk.
- **Fail closed:** If `--admin` is passed with `admin_console.enabled` true but **`LOOPGATE_ADMIN_TOKEN` is missing or too short**, Loopgate **exits** on startup.
- **Bind:** `admin_console.listen_addr` must resolve to a **loopback** address (e.g. `127.0.0.1:9847`). Non-loopback binds are rejected by config validation.
- **Session:** After login, an **`HttpOnly`**, **`SameSite=Lax`** cookie (`lg_admin_session`) holds an opaque server-issued session id (in-memory map, default lifetime **8 hours**).

## Configuration

Example `config/runtime.yaml` fragment:

```yaml
admin_console:
  enabled: true
  listen_addr: "127.0.0.1:9847"
```

When `enabled` is true and `listen_addr` is omitted, the default is **`127.0.0.1:9847`**.

## Running

```bash
export LOOPGATE_ADMIN_TOKEN='your-long-random-secret-here'
loopgate --admin
```

Open `http://127.0.0.1:9847/admin/` (or your configured host/port) and sign in with the same token.

## Tenancy

When **`tenancy.deployment_tenant_id`** is set in `config/runtime.yaml`, the audit view and session list **only include rows for that tenant** (using `tenant_id` on audit events and control sessions). When deployment tenant is **empty** (personal mode), all events and sessions are visible to the admin.

## Future work

- mTLS or IdP-backed operator auth for real admin-node deployments.
- Richer filters, pagination, and tamper-evidence surfacing without weakening redaction.
