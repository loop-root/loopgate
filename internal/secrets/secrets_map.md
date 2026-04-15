# Secrets Map

This file maps `internal/secrets/`, secret references, backends, and redaction helpers.

Use it when changing:

- how API keys and tokens are stored or resolved
- `SecretRef` validation and backends (env, macOS keychain, etc.)
- redaction for logs, summaries, or audit-adjacent paths
- store selection for local dev vs production

## Core Role

`internal/secrets/` is the **central place for secret references** (not raw secrets in config), backend resolution, and **redaction** so sensitive material does not leak into ledgers or logs.

## Key Files (representative)

- `types.go`
  - `SecretRef`, validation, backend constants

- `redact.go`, `summary.go`
  - redaction and safe summaries for operators

- `store_selector.go`, `local_dev_store.go`, `env_store.go`, `stub_secure_store.go`
  - backend selection and implementations

- `macos_keychain_darwin.go`, `macos_keychain_other.go`
  - platform keychain integration

- `audit.go`
  - audit-safe handling of secret-related events

- `secrets_test.go`, `summary_test.go`
  - tests for validation and redaction

## Relationship Notes

- Model runtime loading keys: `internal/modelruntime/runtime.go`
- Loopgate model connections: `internal/loopgate/model_connections.go`
- Local client setup should resolve secrets through Loopgate-managed refs rather than client-owned secret files.

## Important Watchouts

- Never log raw secret values or full tokens.
- Prefer references and metadata in persisted state, not embedded secrets.
