**Last updated:** 2026-04-14

# Loopgate

Loopgate is the policy-governed control plane and enforcement runtime in this repository.

It is not just a reverse proxy.
It is the enforcement point for capabilities, approvals, integration auth, and outbound execution.

**v1 transport:** Local clients use **HTTP** on the **Unix domain socket** control plane — Claude Code hook helpers, IDE bridges, tests, and custom integrators attach this way. (An in-tree stdio MCP server was **removed**; see `docs/adr/0010-macos-supported-target-and-mcp-removal.md`.) **Apple XPC** (or similar) is **optional future hardening** with **no committed milestone (TBD)** — not a v1 requirement — see `docs/rfcs/0001-loopgate-token-policy.md` and `docs/loopgate-threat-model.md`.

**Primary integrators:** [Loopgate HTTP API for local clients](../setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md) (session open, signing, route list). MCP-shaped hosts should use an **external forwarder**. In-tree MCP remains removed; ADR 0010 is the historical record and constraint on any future reintroduction.

**Security posture (honest v1 vs future work):** [Threat model](../loopgate-threat-model.md) and [RFC 0001](../rfcs/0001-loopgate-token-policy.md) — same-user threat scope, **`GET /v1/health`** as the only unauthenticated inventory-free probe, signed **`GET /v1/status`** / **`GET /v1/connections/status`**, peer binding, and v2 backlog (codesign / XPC).

Older legacy route and tool surfaces are being retired. Treat the active
Loopgate product as the governance kernel for Claude hooks, approvals, audit,
policy, sandbox mediation, and the MCP gateway path.

The former in-tree continuity and memory subsystem is no longer part of the
active Loopgate product. That work is being re-homed into a separate sibling
repository named `continuity`.

## Current state

As of **2026-03-24**, the repo contains a local Loopgate MVP (ongoing ship-prep and hardening; public status in `docs/roadmap/roadmap.md`):

- Unix-socket service at `runtime/state/loopgate.sock`
- authoritative policy loaded inside Loopgate
- server-issued control sessions for local operator clients (Claude Code hook helpers, HTTP-native clients, tests; **in-tree MCP removed** — external MCP forwarders out-of-tree)
- client-supplied `actor` / `session_id` treated as labels, not approval authority
- capability and approval tokens bound to the Unix-socket peer identity that opened the session
- subsequent privileged requests signed with a server-issued session MAC key and single-use request nonces
- capability-token minting for scoped execution
- separate approval token for approval decisions
- approval state machine created and resolved inside Loopgate
- single-use approval decision nonces bound to pending approval requests
- filesystem capabilities executed by Loopgate
- normalized capability arguments before approval/execution for current path-based tools
- duplicate `request_id` rejection for replay protection
- typed denial codes on capability responses
- result-classification metadata on success responses
- audit-safe gateway event log at `runtime/state/loopgate_events.jsonl`
- chained Loopgate audit metadata with `audit_sequence`, `previous_event_hash`, and `event_hash`
- rollover-sealed audit segments under `runtime/state/loopgate_event_segments/`
- append-only segment manifest at `runtime/state/loopgate_event_segments/manifest.jsonl`
- config-backed audit ledger limits in `config/runtime.yaml` via `logging.audit_ledger.*`
- local-first audit export cursor state under `runtime/state/audit_export_state.json` when `logging.audit_export.enabled` is configured
- explicit denial for secret-export-like capability names
- explicit `audit_unavailable` failures when pre-execution audit persistence fails
- explicit must-persist handling for capability execution/error and critical denial audit events
- typed display-safe UI status and approval list endpoints
- in-memory display-event ring buffer and SSE replay via `Last-Event-ID`
- UI approval decisions that stay inside Loopgate authority and do not expose decision nonces to UI callers
- persisted Loopgate connection records at `runtime/state/loopgate_connections.json`
- Loopgate-owned secret-ref validation and resolution via the shared `SecretRef` / `SecretStore` boundary
- macOS Keychain-backed secret storage for `secure` / `macos_keychain` refs on Darwin, with explicit fail-closed stubs on other desktop platforms
- internal Loopgate connection lifecycle methods for credential creation, validation, secret resolution, and rotation-safe overwrite without exposing raw secret material to the operator client
- YAML-backed Loopgate connection definitions loaded from `loopgate/connections/*.yaml`
- client-credentials token exchange inside Loopgate for configured provider connections, with in-memory access-token caching only
- PKCE authorization-code start/complete flow inside Loopgate for configured provider connections, with refresh-token storage in the secure backend and in-memory access-token caching
- explicit `public_read` configured connections for host-allowlisted unauthenticated GET workflows such as public status pages
- typed provider-backed HTTP read capabilities registered through Loopgate config, not the unprivileged client
- delegated Loopgate client construction for bridge/UI use without calling `/v1/session/open`
- explicit quarantine metadata and blob-view endpoints for operator inspection
- explicit site inspection and trust-draft endpoints for narrow runtime onboarding of new `public_read` sources
- sandbox root abstraction under a private Loopgate-owned runtime directory, with operator-visible workspace rooted at `/morph/home`
- explicit sandbox mediation endpoints for:
  - import from host into sandbox imports
  - stage sandbox artifacts into sandbox outputs
  - inspect staged artifact metadata before export
  - export staged sandbox outputs back to a host destination
- local model inference endpoint for the operator client, with Loopgate-owned live secret resolution
- append-only quarantine lifecycle with `artifact.viewed`, `artifact.promoted`, and `artifact.blob_pruned` events
- append-only hash-linked audit for governance-relevant local actions
Implemented endpoints:

- `GET /v1/health` (liveness, no auth)
- `GET /v1/status` (Bearer + signed GET — full inventory)
- `GET /v1/ui/status` (`ui.read`)
- `GET /v1/ui/events` (`ui.read`)
- `GET /v1/ui/approvals` (approval-token authenticated UI route)
- `POST /v1/ui/approvals/{id}/decision` (approval-token authenticated; body `{ "approved": bool }`)
- `GET /v1/connections/status` (Bearer + signed GET + `connection.read`)
- `POST /v1/connections/validate` (`connection.write`)
- `POST /v1/connections/pkce/start` (`connection.write`)
- `POST /v1/connections/pkce/complete` (`connection.write`)
- `POST /v1/quarantine/metadata` (`quarantine.read`)
- `POST /v1/quarantine/view` (`quarantine.read`)
- `POST /v1/quarantine/prune` (`quarantine.write`)
- `POST /v1/sites/inspect` (`site.inspect`)
- `POST /v1/sites/trust-draft` (`site.trust.write`)
- `POST /v1/sandbox/import` (`fs_write`; host source must be inside the control session's bound operator mounts from a pinned expected client session)
- `POST /v1/sandbox/stage`
- `POST /v1/sandbox/metadata`
- `POST /v1/sandbox/export` (`fs_write`; host destination must match a bound operator mount from a pinned expected client session and an active write grant)
- `POST /v1/session/open`
- `POST /v1/model/validate`
- `POST /v1/model/reply`
- `POST /v1/capabilities/execute`
- `POST /v1/approvals/{id}/decision`

Current authenticated request shape for privileged POSTs:

- bearer capability token or approval token
- `X-Loopgate-Control-Session`
- `X-Loopgate-Request-Timestamp`
- `X-Loopgate-Request-Nonce`
- `X-Loopgate-Request-Signature`

The normative token and request-integrity rules are defined in [RFC 0001](../rfcs/0001-loopgate-token-policy.md).
The current harness boundary note is [Claude Code Hooks MVP](./claude_code_hooks_mvp.md).

These are local socket endpoints, not a public network API. Operator clients
and out-of-tree bridges talk to Loopgate over the same repo-local Unix-socket
trust boundary.

Current MVP note:

- the active transport is the local Unix-socket control plane for Claude hooks, operator clients, and out-of-tree bridges
- retired legacy helper and worker surfaces are no longer part of the active product boundary
- the retired in-tree memory and continuity layers are no longer part of the active operator contract

Not yet implemented:

- Windows Credential Manager and Linux Secret Service backends
- authorization-code flow without PKCE
- broader provider adapter set beyond the current typed read capability path
- skills manifests and adapter bindings
- dry-run / explain-denial endpoint

## Boundary split

### Unprivileged operator client owns

- operator UX (Claude Code, IDE, or other local HTTP client; MCP hosts via **out-of-tree** forwarders unless a future ADR adds a thin in-tree adapter)
- model interaction and prompt compilation (where applicable)
- local session state
- rendering Loopgate decisions and approval prompts

### Loopgate owns

- policy evaluation
- capability orchestration
- approval creation and enforcement
- capability-token minting and validation
- integration auth and secret handling
- outbound integration execution
- structured result filtering and redaction
- the governed MCP broker lifecycle and execution path

## Core rule

The model never calls third-party systems directly.

The intended execution path is:

`model output -> client parsing/validation -> Loopgate capability request -> adapter/tool execution -> structured response -> client rendering or storage outside the authority boundary`

## Secret rule

Loopgate must never reveal raw provider credentials, refresh tokens, access tokens, client secrets, or stored key material to the operator client.

Allowed:

- Loopgate-minted short-lived capability tokens
- Loopgate-minted approval tokens
- redacted connection metadata
- structured capability results

Forbidden:

- secret export endpoints
- raw token inspection
- response shapes that include provider credentials

Current implementation detail:

- Loopgate can now persist non-secret connection records with `SecretRef` metadata only
- Loopgate resolves connection credentials through backend-specific `SecretStore` selection and fails closed on unavailable secure backends
- Loopgate can now load configured client-credentials and PKCE connections from repo YAML, resolve the referenced secret through the secure-store boundary, and exchange or refresh access tokens internally
- raw secret material is never written to `loopgate_connections.json`, Loopgate status responses, or UI status responses
- The operator client loads model runtime config locally for display/planning only, but live model validation, inference, and secret resolution happen inside Loopgate

## HTTP and prompt-injection rule

HTTP and integration payloads must not become prompt input by default.

Required design:

- Loopgate adapters extract typed, safe fields
- raw response bodies remain quarantined inside Loopgate
- standard client-side prompt compilation uses structured results only

## Sandbox boundary

Loopgate now exposes a conceptual operator sandbox (virtual tree) rooted under:

- `/morph/home`

This is the operator-visible mini-filesystem namespace. The current on-disk
implementation is private to Loopgate; the important boundary is semantic, not
poetic:

- imports move host content into sandbox space explicitly
- staging moves sandbox work products into sandbox outputs explicitly
- review inspects staged artifact metadata explicitly
- export moves staged sandbox outputs back to a host destination explicitly

Current operator-visible sandbox commands:

- `/sandbox import <host-path> [destination-name]`
- `/sandbox stage <sandbox-path> [output-name]`
- `/sandbox metadata <sandbox-output-path>`
- `/sandbox export <sandbox-output-path> <host-destination>`

Current rules:

- sandbox reads/writes stay inside sandbox home
- host paths are never implicitly discoverable from inside the sandbox
- imports and exports are mediated and audited by Loopgate
- sandbox copy sources are opened with no-follow semantics and copied from the
  opened handles rather than re-opening by path after validation
- staged outputs receive a first-class artifact record before export
- export from `/morph/home/outputs/` requires a matching staged artifact record
- export is explicit and review-gated
- sandbox copy only accepts regular files and directories; symlink entries and
  other special file types are denied
- expiry cleanup for short-lived control-plane state is opportunistic and
  bounded rather than full-sweep on every request path

The current MVP does not expose a generic HTTP capability yet, which keeps this boundary closed until the structured-only adapter path exists.

Current implementation detail:

- filesystem capabilities now use explicit per-capability classification instead of inferring eligibility from the absence of a quarantine ref
- `fs_read` is currently classified as a prompt-eligible display result; the old continuity-owned memory-eligibility axis has been removed from active Loopgate result metadata
- `quarantine_ref` now points to a Loopgate-owned record under `runtime/state/quarantine`
- quarantine metadata and raw payload bytes now live separately, so metadata/lineage can survive blob pruning without keeping the full payload inline
- quarantine metadata now exposes separate trust and storage facts:
  - `trust_state=quarantined`
  - `content_availability=blob_available|metadata_only`
- explicit metadata inspection is allowed after pruning, but fresh blob view and fresh promotion fail closed once source bytes are no longer retained
- configured provider-backed HTTP capabilities return only allowlisted structured fields while the full raw response body remains quarantined inside Loopgate
- unclassified/future capabilities default to quarantined `audit_only` handling unless they declare an explicit policy
- future HTTP/integration adapters must keep raw bodies inside Loopgate and return only extracted fields to the operator client
- untrusted public sites can now be inspected explicitly through Loopgate and converted into reviewable `public_read` trust drafts under `loopgate/connections/drafts/`
- trust drafts are exact-source declarations, not wildcard browsing permissions
- site inspection reports scheme, host, path, content type, HTTP status, and TLS certificate details where available
- trust-draft creation stays explicit and auditable; certificate information is informative and does not itself create trust

## Current hardening status

Implemented now:

- non-empty capability scope required at session open
- server-issued `control_session_id` returned from Loopgate
- Unix peer credential binding for session, capability-token, and approval-token use
- HMAC verification over request method, path, control-session binding, timestamp, nonce, and body hash
- execution token and approval token are distinct credentials
- approval decisions require a per-approval decision nonce and cannot be replayed once resolved
- duplicate `request_id` values are rejected per control session
- duplicate signed request nonces are rejected per control session
- session open is now rate-limited and capped per local peer UID to reduce same-user bootstrap abuse
- request bodies are size-limited and decoded with strict unknown-field rejection
- critical Loopgate audit events fail closed with `audit_unavailable` instead of degrading to warn-only behavior
- high-risk execution now uses internal single-use execution-token semantics bound to normalized request arguments
- the Loopgate client can now be constructed or refreshed from delegated session credentials and fails closed instead of reopening a session when delegated credentials expire
- delegated credential health is now classified as `healthy`, `refresh_soon`, or `refresh_required` with a 2-minute lead window for client-driven refresh
- capability execution and approval endpoints now return typed non-200 HTTP status codes for denied and error outcomes instead of always using HTTP 200
- peer-credential extraction failures remain fail-closed and now emit operator-visible warnings rather than being silently swallowed

Still pending:

- stronger client identity than local peer credential binding
- independently authenticated approval provenance beyond possession of the approval token and decision nonce
- token nonce/jti replay tracking beyond request-id replay protection
- stronger cryptographic integrity guarantees than local hash chaining alone
- automatic PKCE browser launch/callback UX

## Recommended next steps

1. Add authorization-code support without PKCE where providers require it.
2. Harden refresh-token lifecycle and rotation metadata further.
3. Define `skill.yaml` and adapter registration.
4. Expand beyond the first low-risk external capability with structured-only output.
5. Add dry-run / explain-denial and richer approval metadata.
