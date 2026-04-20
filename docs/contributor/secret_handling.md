**Last updated:** 2026-04-20

# Secret Handling

This project handles API keys, access tokens, refresh tokens, client secrets,
and similar credentials as high-sensitivity material.

## Core rules

- Never hardcode secrets in source, tests, fixtures, example configs, or docs.
- Never store raw secrets in the append-only ledger.
- Never log raw secrets, full tokens, authorization headers, or refresh tokens.
- Never persist model-generated secret-like values unless they were explicitly
  validated and intentionally stored through the secret path.
- Treat secrets as untrusted input until parsed and validated for the specific
  use case.
- If the secure backend is unavailable, fail closed instead of silently falling
  back to plaintext storage.

## Preferred storage

Local desktop and single-user mode:

- macOS Keychain
- Windows credential store / DPAPI-backed storage
- Linux Secret Service / libsecret-compatible keyring

Server or multi-user mode:

- a real secret manager such as Vault or a cloud-native equivalent

CI and CD:

- environment variables are acceptable for runtime injection, but they are a
  delivery mechanism, not the preferred persistent backend

## Forbidden storage locations

Do not store secrets in:

- repo-tracked config files
- append-only ledger entries
- runtime logs or debug output
- user-facing audit trails
- panic messages or crash reports without guaranteed redaction
- test snapshots or golden files
- shell history
- repo-tracked runtime artifacts

## Storage model

Normal runtime state should store references and metadata, not raw secret
values.

Preferred pattern:

```go
type SecretRef struct {
	ID          string
	Backend     string
	AccountName string
	Scope       string
}
```

Ledger and user-visible audit data may record metadata such as:

- secret reference ID
- backend or provider
- created / rotated timestamps
- scope or policy association
- validation status
- truncated fingerprint or hash prefix

They must not record:

- raw secret values
- full access or refresh tokens
- private keys
- full authorization headers
- complete environment variable contents

## Rotation and lifecycle

Code should tolerate rotation:

- resolve secrets just in time where practical
- avoid permanent in-memory caching
- re-fetch on authentication failure when safe
- make expiration, revocation, and backend failure visible through explicit
  errors

Useful lifecycle metadata includes:

- `created_at`
- `last_used_at`
- `last_rotated_at`
- `expires_at`
- `status`
- `scope`
- `owner`

## Redaction and observability

Always redact:

- authorization headers
- bearer tokens
- API keys
- refresh tokens
- client secrets
- private key material
- known secret fields in JSON or YAML payloads
- tool arguments, tool output, and reason strings before audit persistence

Errors should preserve debugging value without leaking contents.

## Environment variables

Environment variables are acceptable for runtime injection in CI/CD and some
deployments, but they are not the preferred long-term store for local
credentials.

Allowed:

- runtime injection from CI, CD, or an orchestrator
- bootstrap configuration for connecting to a secret backend

Not preferred:

- long-lived user API keys in local shell startup files
- repo-local `.env` files containing real credentials

## Testing requirements

Secret-handling changes should include tests for:

- no raw secret persistence in the ledger
- no raw secret leakage in logs or errors
- backend failure handling
- secret-reference resolution
- rotation metadata behavior
- access-token in-memory handling where applicable
- explicit denial on missing or unavailable secure backend
- redaction paths for structured and unstructured logs
- no raw secret-bearing tool output persisted to audit logs
