**Last updated:** 2026-04-11

# Secrets Subsystem (First Pass)

The current repo has two different secret stories, and they should not be
confused:

- current client-side model/runtime secret resolution (non-integration config)
- future Loopgate-owned third-party integration and OAuth secret handling

The current implementation is conservative by design: explicit trust
boundaries, no plaintext persistence, and fail-closed behavior.

## 1) Core types

Defined in `internal/secrets/types.go`:

- `SecretRef`
  - `ID`
  - `Backend`
  - `AccountName`
  - `Scope`
- `SecretMetadata`
  - lifecycle fields (`CreatedAt`, `LastUsedAt`, `LastRotatedAt`, `ExpiresAt`)
  - `Status`, `Scope`, `Fingerprint`
- `SecretStore` interface (`Put`, `Get`, `Delete`, `Metadata`)

Errors:

- `ErrSecretNotFound`
- `ErrSecretBackendUnavailable`
- `ErrSecretValidation`

Backend constants:

- `BackendEnv`
- `BackendSecure`
- `BackendMacOSKeychain`
- `BackendWindowsCreds`
- `BackendLinuxSecretSvc`

## 2) Backends

### EnvSecretStore (`internal/secrets/env_store.go`)

- Backend name: `env`
- Read-only runtime injection (`Get`/`Metadata`)
- `Put` and `Delete` return validation errors
- Fails closed when env var is missing/empty

### StubSecureStore (`internal/secrets/stub_secure_store.go`)

- Backend name: `secure`
- Explicitly returns `ErrSecretBackendUnavailable`
- Does not pretend to store values

### MacOSKeychainStore (`internal/secrets/macos_keychain.go`)

- Backend name: `macos_keychain`
- Uses the macOS `security` tool against the user keychain
- Stores secret bytes via stdin-prompted `-w` usage rather than command-line
  secret arguments
- Returns metadata and secret refs only; no raw secret values are written to
  repo state
- Fails closed on missing keychain items or unavailable backend tooling

Current limitations:

- implemented for macOS only
- metadata is intentionally minimal and does not attempt to scrape full
  Keychain timestamps

### Local dev secure selector

`NewLocalDevSecretStore()` now selects:

- `MacOSKeychainStore` on macOS
- explicit fail-closed stubs for Windows and Linux until their native
  backends are implemented

### Backend selection by reference

`NewStoreForRef()` now resolves the backend from `SecretRef.Backend` and returns:

- `EnvSecretStore` for `BackendEnv`
- `NewLocalDevSecretStore()` for `BackendSecure`
- `MacOSKeychainStore` for `BackendMacOSKeychain` on macOS
- platform-specific secure stubs for unsupported or not-yet-implemented native
  backends

This is intentionally strict:

- no secret backend auto-correction
- no secure-to-env fallback
- unavailable secure backends fail closed with `ErrSecretBackendUnavailable`

## Implemented Today

Current reality:

- A local client may still store non-secret model runtime config locally, but live model
  provider credentials are resolved by Loopgate during inference
- OS-backed secret storage
- macOS Keychain backend
- `SecretRef` configuration references
- in-memory access token caching
- Loopgate-only token exchange
- Loopgate now persists connection records with `SecretRef` metadata only
- Loopgate can create, validate, and resolve connection secret refs through the
  shared secret-store boundary
- Loopgate can now rotate an existing connection credential through an explicit
  rotation-safe overwrite path
- Loopgate can load connection definitions from `loopgate/connections/*.yaml`
  and use the referenced secret for client-credentials token exchange
- Loopgate can now use PKCE configuration to persist refresh tokens through the
  secure backend and keep access tokens in memory only
- Loopgate-managed remote model inference now requires `model_connection_id`
  with a Loopgate-resolved secret ref; the older `api_key_env_var` path
  remains only a generic compatibility/bootstrap mechanism outside the
  Loopgate-owned remote runtime path

Target direction:

- third-party integration secrets live in Loopgate-owned secure storage
- Loopgate performs OAuth token exchange and refresh itself
- Connected clients do not receive provider access tokens, refresh tokens, or client
  secrets

Current operator pattern for Loopgate-managed client credentials:

1. Define a connection in `loopgate/connections/<name>.yaml` with:
   - provider
   - grant_type: `client_credentials`
   - subject
   - client_id
   - token_url
   - api_base_url
   - allowed_hosts
   - typed capability definitions
   - `SecretRef` metadata only
2. Store the actual secret through the secure backend or a runtime env-backed
   ref.
3. Register or validate the connection record in Loopgate.
4. Let Loopgate exchange the client secret for an access token internally.

The raw access token remains in Loopgate memory only. Clients receive only the
structured capability result and any `quarantine_ref`.

Current operator pattern for Loopgate-managed PKCE:

1. Define a connection in `loopgate/connections/<name>.yaml` with:
   - grant_type: `pkce`
   - authorization_url
   - token_url
   - redirect_url
     - must use `https` or loopback `http`
   - api_base_url
   - allowed_hosts
   - typed capability definitions
   - a `SecretRef` that will store the refresh token
2. Start the flow from the operator shell (e.g. Loopgate CLI):
   - `/connections pkce-start <provider> <subject>`
3. Open the returned authorization URL in a browser and capture the returned
   `code` and `state`.
4. Complete the flow from the operator shell:
   - `/connections pkce-complete <provider> <subject> <state> <code>`

Loopgate stores the refresh token in the secure backend and keeps the access
token in memory only.

## Planned Improvements

- additional OS secret backends
- additional authorization code + PKCE support
- refresh token lifecycle management
- broader secret rotation workflows and metadata

## Enterprise integration layer (roadmap)

Loopgate needs the same **pluggability** for organizational secret systems as for **identity**: customers will expect to bring **HashiCorp Vault**, **cloud KMS** (and envelope-encrypted blobs), **HSM**-backed enterprise stores, and sometimes **TPM** / platform keys for **machine identity** or bootstrap — not only macOS Keychain on a laptop.

**Intended shape:** extend the existing **`SecretStore`** / `SecretRef.Backend` model (`internal/secrets/types.go`) with explicit backends or adapters, operator runbooks (paths, IAM, least privilege, rotation), and ADRs for ordering (e.g. Vault before niche HSM profiles). Resolution stays **inside Loopgate**; IDEs and other clients do not receive raw long-lived secrets.

**Sequencing:** documented in `sprints/2026-04-01-loopgate-enterprise-phased-plan.md` § *Future enterprise integration layers*. Single-org local nodes can keep OS keyrings; **tenant-scoped secret keys** become necessary when multiple orgs share one runtime (e.g. hosted multi-tenant).

**Identity counterpart:** organizational **IdP via OIDC/OAuth** (admin / node bootstrap) is the parallel track — see the same sprint section and `AGENTS/BUILD_NOW.md` (*Explicitly out of scope* lists IdP until the tenancy/session model is stable).

## 3) Ledger-safe secret audit path

Use `AppendSecretMetadataEvent` in `internal/secrets/audit.go`.

It writes only:

- validated `SecretRef`
- `SecretMetadata`
- redacted detail summary (`{"redacted": true, "keys": [...]}`)

It never writes raw secret bytes or unredacted detail values.

## 4) Redaction helpers

Shared helpers in `internal/secrets/redact.go`:

- `RedactText(string) string`
- `RedactStringMap(map[string]string) map[string]interface{}`
- `RedactStructuredFields(map[string]interface{}) map[string]interface{}`

These are used by orchestrator ledger logging for tool args/output/reasons.

## 5) Non-negotiable constraints

- No plaintext file secret backend
- No silent secure->env fallback
- Exact backend matching (case-sensitive)
- Treat model/tool/config input as untrusted
- Fail closed when a secret cannot be resolved securely
- Do not move future integration-secret authority back into thin clients for
  convenience

## 6) Minimal usage example

```go
validatedRef := secrets.SecretRef{
    ID:          "openai-prod",
    Backend:     secrets.BackendEnv,
    AccountName: "OPENAI_API_KEY",
    Scope:       "model_inference",
}

store := secrets.NewEnvSecretStore()
rawSecret, secretMetadata, err := store.Get(ctx, validatedRef)
if err != nil {
    // handle ErrSecretNotFound / ErrSecretValidation
}
_ = rawSecret
_ = secretMetadata
```
