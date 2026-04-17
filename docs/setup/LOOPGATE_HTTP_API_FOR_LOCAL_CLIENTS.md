**Last updated:** 2026-04-11

# Loopgate local HTTP API — guide for HTTP-native clients

This document explains how a **local process** (Claude Code hook helper, native app, test harness, or bridge) talks to **Loopgate** over **HTTP on a Unix domain socket**. It is the **only supported in-tree v1 transport** for privileged control-plane calls. **In-tree MCP is removed** ([ADR 0010](../adr/0010-macos-supported-target-and-mcp-removal.md)); use this HTTP API directly or an **out-of-tree** MCP→HTTP forwarder if needed.

**Current MVP note:** the active operator harness is **Claude Code + project hooks + Loopgate**. For primary governance, prefer a **command hook** that talks to Loopgate over the Unix socket. Claude's raw HTTP hook mode is not a safe primary enforcement mechanism because non-2xx responses, timeouts, and connection failures are non-blocking in Claude Code's hook model.

**Retired surface note:** older Haven-specific routes such as `/v1/chat`, `/v1/model/settings`, `/v1/settings/*`, `/v1/continuity/inspect-thread`, older `/v1/ui/*` projections, and Haven-only sandbox tools like `journal.*`, `notes.*`, `paint.*`, `note.create`, `desktop.organize`, and `haven.operator_context` are retired from the active Loopgate product. This guide should be read as the Claude hooks / governance / MCP control-plane reference.

**Normative details:** [RFC 0001: Loopgate Token and Request Integrity Policy](../rfcs/0001-loopgate-token-policy.md).  
**Reference implementation:** `internal/loopgate/client.go` (Go) — match its wire behavior byte-for-byte when in doubt.

### Neutral routes and the `operator` actor

The historical **`/v1/haven/...`** compatibility aliases are removed. The
current Loopgate operator/session label is **`operator`**. The older actor
label **`haven`** remains a compatibility alias in a few runtime paths, but it
is not part of the current product boundary and should not be used by new
clients.

---

## 1. What you are connecting to

| Item | Detail |
|------|--------|
| **Process** | Loopgate — local authority for capabilities, approvals, audit, secrets, sandbox, and the governed MCP path. |
| **Wire protocol** | **HTTP/1.1** (JSON bodies, `Content-Type: application/json`) |
| **Transport** | **Unix domain stream socket** only in v1 — **not** TCP to `localhost` |
| **Default socket path** | `{repoRoot}/runtime/state/loopgate.sock` when Loopgate is started with cwd = repo root (`cmd/loopgate`) |
| **Host header** | Any stable placeholder is fine; the Go client uses `http://loopgate` as the base URL. The server routes by path, not host. |
| **Override path** | Environment variable **`LOOPGATE_SOCKET`** — absolute path to the socket file. Set this explicitly in launchers, hook scripts, or local app config so clients and Loopgate agree without hardcoding. |

There is **no** public HTTP listener for the control plane in v1. Apple XPC is optional future work and **not** required for this API.

### 1.1 macOS App Sandbox and `homeDirectoryForCurrentUser`

If your macOS app is **sandboxed** (typical for a signed `.app` in `/Applications`), `FileManager.default.homeDirectoryForCurrentUser` is **not** `/Users/<you>` — it is the app’s **container**, e.g. `~/Library/Containers/<bundle-id>/Data/`. Building paths like `\(home)/Dev/<checkout>/runtime/state/loopgate.sock` therefore becomes `…/Containers/…/Data/Dev/<checkout>/…`, which will not match a Loopgate process you started from a normal shell in the real repo.

**Do not use a hard-coded “home + Dev/…” checkout path as the only resolver inside a sandboxed app.**

Practical approaches:

1. **`LOOPGATE_SOCKET` first** — Read `ProcessInfo.processInfo.environment["LOOPGATE_SOCKET"]` (or the value your launcher sets). If present and non-empty, use it. This matches typical repo launch scripts for Loopgate and is the fastest fix for dev.
2. **Operator-configured path** — Settings field or “Choose Loopgate repository…” (`NSOpenPanel`) plus a **security-scoped bookmark** so the sandboxed app can reconnect after relaunch. Store the socket path (or repo root) the user picked.
3. **Shipped / agreed layout** — For a consumer install, pick one authoritative layout (for example under `~/Library/Application Support/<YourProduct>/…`) and run Loopgate (or a small unsandboxed helper) so it **creates the socket at that path**. The UI app and the daemon must share the same contract; the real user home is only reachable from unsandboxed code or from paths the user has granted.

**`$PATH` does not apply** to Unix socket locations. Use an explicit **`LOOPGATE_SOCKET`** passed by the launcher, plist `LSEnvironment`, or Xcode scheme environment for debug builds.

**Connecting, not just path resolution:** Even with the correct absolute path, a **sandboxed** GUI app may still fail `connect()` to a socket under an arbitrary checkout such as `~/src/loopgate/...` because the sandbox treats that as access to a file **outside the container**. `com.apple.security.network.client` does **not** grant that. For local development, use a **Debug** build **without** App Sandbox, or ship an **unsandboxed helper** / agreed socket location both processes can access. Wrong-path fixes alone will not unblock sandboxed `connect()`.

---

## 2. Running Loopgate

From this repository’s root (with Go toolchain):

```bash
go run ./cmd/loopgate
```

You should see:

```text
Loopgate listening on …/runtime/state/loopgate.sock
```

Policy changes now require a valid detached signature:

```bash
go run ./cmd/loopgate-policy-sign
```

See [SETUP.md](./SETUP.md) and [POLICY_SIGNING.md](./POLICY_SIGNING.md) for repo layout, runtime paths, and signed-policy workflow.

---

## 3. Connecting from Swift (or any native client)

`URLSession` does **not** expose Unix domain sockets directly. Typical options:

1. **POSIX `AF_UNIX` + manual HTTP framing** — connect to the socket path, write a complete HTTP request (`METHOD path HTTP/1.1`, headers, blank line, body), read until you have a full response (handle `Content-Length` or chunked encoding).
2. **SwiftNIO** (or **NIOTransportServices**) — same idea: byte stream on a Unix socket, HTTP client codec on top.
3. **Small local helper** — a thin subprocess or XPC helper that forwards to the socket (only if you accept the extra moving parts).

**Important:** The HTTP request path must match what Loopgate registers (e.g. `/v1/session/open`), including leading `/`.

**curl (debugging only):**

```bash
curl --unix-socket runtime/state/loopgate.sock http://loopgate/v1/health
```

---

## 4. Peer binding (security)

Loopgate records the **Unix socket peer identity** (e.g. UID/PID on macOS) when you open a **control session**. Tokens issued at `/v1/session/open` are bound to that peer. **Possession of a token is not enough** if a different OS process presents it.

Design your Swift app so the **same process** that called `/v1/session/open` performs subsequent signed requests (or you refresh the session from that same process).

**Executable path pinning:** When **`control_plane.expected_session_client_executable`** in `config/runtime.yaml` is a non-empty absolute path, Loopgate compares it (after `filepath.Clean`) to the connecting peer’s resolved executable at **`POST /v1/session/open`**. A mismatch, or inability to resolve the connecting executable, returns **403** with `denial_code` **`process_binding_rejected`**. The repository default is **empty** (pinning off). Set this in production desktop bundles where the client path is stable. Operator-mount bindings are rejected unless this pin is configured.

**Legacy compatibility note:** some runtime internals still accept `haven` as a
compatibility actor label. Treat that as migration debt, not as an active
product surface or route namespace.

---

## 5. Session open (no MAC yet)

**`POST /v1/session/open`**

- **No** `Authorization` header.
- **No** signed-request headers yet (you do not have `session_mac_key` or `control_session_id`).

**Request body (JSON):**

| Field | Required | Notes |
|-------|----------|--------|
| `actor` | Yes | Stable label for audit (safe identifier; see `identifiers` package rules in Go). |
| `session_id` | Yes | Client-side session label (safe identifier). |
| `requested_capabilities` | Yes | **Non-empty** array of capability **names** to request. Must be a subset of what the server exposes. **Unknown names are rejected.** |
| `workspace_id` | No | Compatibility workspace hint for multi-surface clients. Loopgate derives the authoritative workspace binding from the current repo/runtime and rejects mismatches. |
| `operator_mount_paths` | No | Compatibility field for host-directory bindings. Loopgate canonicalizes and rejects unsafe paths, but only accepts them when the server is pinning an expected client executable for session open. |
| `primary_operator_mount_path` | No | Optional default root for relative `operator_mount.fs_*` paths. Must match one of `operator_mount_paths` after Loopgate canonicalization and is accepted only with the same client-executable pinning. |
| `correlation_id` | No | Optional tracing. |

**Capability names for `requested_capabilities`:** you must use names the server’s tool registry actually registers. **Ship a fixed allowlist** in your client (recommended), or call **`GET /v1/status` after** `POST /v1/session/open` with a **minimal bootstrap** session (e.g. one known tool such as `fs_list`) and the **signed GET envelope** (§6). The status response includes `capabilities[]` with a `name` field per tool. **Unauthenticated `GET /v1/status` is not supported** — it returns **401** without `Authorization` and the HMAC headers.

**Response (JSON) — treat as secret-bearing:**

| Field | Client must |
|-------|-------------|
| `control_session_id` | Store; send on signed requests. |
| `capability_token` | Store; send as `Authorization: Bearer …` for execution and most privileged routes. |
| `approval_token` | Store; send as `X-Loopgate-Approval-Token` for approval UI routes (see RFC 0001). |
| `session_mac_key` | Store in memory only; **never** log, persist to disk unencrypted, or ship in analytics. Used for HMAC-SHA256 request signing. The server **derives** this from rotating epoch material (12-hour UTC windows); see **Session MAC key rotation** below. |
| `expires_at_utc` | Refresh the session before expiry (call `/v1/session/open` again with the same labels, or implement refresh policy your product needs). |

**Dead-peer orphan recovery:** before enforcing the per-UID active-session limit, Loopgate performs a request-driven sweep for existing sessions owned by the same Unix-peer UID whose recorded peer PID no longer exists. Those orphaned sessions are retired server-side and any pending approvals are cancelled before the new session open proceeds.

**`POST /v1/session/close`** — same authentication as other signed Bearer routes: `Authorization: Bearer …` plus the signed POST envelope (§6) with an empty body.

- Use this on **graceful client shutdown** or before deliberately rebuilding the local client transport.
- Loopgate retires the current control session, removes its capability and approval tokens, and records a `session.closed` audit event.
- The close fails **closed** with `denial_code: "session_close_blocked"` if the session still has **pending approvals**. Clients should surface that truth rather than pretending the session was retired.

### Session MAC key rotation (12-hour epochs)

`session_mac_key` is **derived** from a server-held master secret and the **control session id**, and changes each **12-hour UTC epoch**. Loopgate accepts signatures built with the **previous**, **current**, or **next** epoch’s derived key so clients can cross a single boundary without dropping traffic.

**`GET /v1/session/mac-keys`** — same authentication as **`GET /v1/status`**: `Authorization: Bearer …` plus the **signed GET envelope** (§6, empty body). Response JSON includes:

- `rotation_period_seconds` — always **43200** (12 hours).
- `derived_key_schema` — **`loopgate-session-mac-v1`** (stable identifier for the derivation rule).
- `current_epoch_index` — non-negative epoch counter.
- **`previous`**, **`current`**, **`next`** — each has `slot`, `epoch_index`, `valid_from_utc`, `valid_until_utc`, and `derived_session_mac_key` (the **64-hex-character** string to use as `session_mac_key` UTF-8 for HMAC, same shape as session open).

Long-lived processes should **refresh** the in-memory signing key from **`current.derived_session_mac_key`** periodically (or call **`GET /v1/session/mac-keys`** after each epoch), because verification only overlaps **three** epochs (~36 hours of slack, depending on where the session started).

The Go client exposes **`SessionMACKeys`** and **`RefreshSessionMACKeyFromServer`** (`internal/loopgate/client.go`): the latter fetches mac-keys and sets `session_mac_key` from the **current** slot. It requires a **still-valid** request signature (same as any signed GET); if the in-memory key is garbage or too many epochs stale, open a **new** session instead.

**Typical error shape:** JSON body compatible with `CapabilityResponse` (`status`, `denial_code`, `denial_reason`, …) with non-2xx HTTP status on failures.

---

## 6. Signed request envelope (after session open)

For privileged traffic, Loopgate expects the **signed envelope** on routes defined in RFC 0001 §6.2 (notably **POST** bodies and specific **GET** UI routes).

### 6.1 When to sign

After you have `control_session_id` and `session_mac_key`, attach signatures to requests that the Go client signs — i.e. whenever `attachRequestSignature` in `client.go` would run: **not** for `/v1/session/open`, **not** for **`GET /v1/health`**, and **not** when you have no session yet. **`GET /v1/status`** and **`GET /v1/connections/status`** require the same signed envelope as other authenticated GETs (empty body → `body_hash` of SHA256 of empty string).

If the client already sent `X-Loopgate-Control-Session` (etc.), the Go client skips re-signing; for Swift, always compute a **fresh** nonce per request unless you intentionally mirror that optimization.

### 6.2 Headers (capability execution path)

| Header | Value |
|--------|--------|
| `Content-Type` | `application/json` (when there is a body) |
| `Authorization` | `Bearer <capability_token>` |
| `X-Loopgate-Control-Session` | `<control_session_id>` |
| `X-Loopgate-Request-Timestamp` | RFC3339Nano UTC string |
| `X-Loopgate-Request-Nonce` | Fresh random hex (Go uses 12 random bytes → 24 hex chars) |
| `X-Loopgate-Request-Signature` | Hex-encoded HMAC-SHA256 (see below) |

**Approval-only routes** (e.g. some `/v1/ui/*` and `/v1/approvals/...`) use **`X-Loopgate-Approval-Token`** instead of `Authorization`, per RFC 0001.

### 6.3 Signature payload (must match Go)

Let `body` be the **exact** raw JSON bytes you send in the request body (empty for GET with no body).

```
body_hash = SHA256(body) as lowercase hex string

signing_payload = join with newline:
  HTTP_METHOD
  request_path   // e.g. /v1/capabilities/execute — path only, no query string in current Go impl
  control_session_id
  request_timestamp_rfc3339nano
  request_nonce_hex
  body_hash

signature = HMAC_SHA256(key = utf8(session_mac_key), message = utf8(signing_payload))
signature_hex = lowercase hex(signature)
```

Set `X-Loopgate-Request-Signature` to `signature_hex`.

This matches `computeRequestSignature` in `internal/loopgate/client.go`.

### 6.4 Replay and clocks

- Each **`X-Loopgate-Request-Nonce`** must be **unique** within the control session (server rejects replays).
- Timestamp must be within the server’s accepted skew (invalid clock → denied).
- Capability **`request_id`** in `CapabilityRequest` must not collide with an in-flight or completed execution for that session (server enforces replay rules).

---

## 7. Routes registered today (inventory)

The following paths are registered on the Loopgate mux (`internal/loopgate/server.go`). **Method** is mostly **POST** for mutations; **GET** where noted. Exact auth and signing requirements follow the handler (authenticate + `verifySignedRequest` patterns); when unsure, mirror `internal/loopgate/client.go`.

| Path | Typical use |
|------|-------------|
| `GET /v1/health` | Liveness only: `version`, `ok` — **no token**, **no** policy/capability/connection data |
| `GET /v1/status` | Capability inventory, policy snapshot, counts — **Bearer + signed GET** |
| `GET /v1/connections/status` | Connection summaries — **Bearer + signed GET** + **`connection.read`** |
| `GET /v1/mcp-gateway/inventory` | Declared MCP server/tool inventory and effective read-only decisions — **`diagnostic.read`** + **signed GET with empty body**. No launch, no network write. |
| `GET /v1/mcp-gateway/server/status` | Read-only launched-server runtime projection for declared MCP servers — **`diagnostic.read`** + **signed GET with empty body**. Returns `absent`, `starting`, or `launched` plus safe runtime facts like PID, initialized flag, and stderr path. |
| `POST /v1/mcp-gateway/decision` | Typed read-only decision check for one declared MCP server/tool pair — **`diagnostic.read`** + **signed POST**. Returns `allow`, `needs_approval`, or `deny` without launching anything. |
| `POST /v1/mcp-gateway/server/ensure-launched` | Request-driven launch ownership for one declared `stdio` MCP server — **`mcp_gateway.write`** + **signed POST**. Reuses an already-running declared server inside the same Loopgate runtime, appends `mcp_gateway.server_launched` on first launch, injects only policy-declared env/secret refs, and still does not execute any MCP tool. |
| `POST /v1/mcp-gateway/server/stop` | Request-driven stop/reset for one launched MCP server — **`mcp_gateway.write`** + **signed POST**. Removes the launched server from authoritative in-memory state, serializes with in-flight stdio I/O, closes pipes, kills the child if present, and appends `mcp_gateway.server_stopped` on a real stop. |
| `POST /v1/mcp-gateway/invocation/validate` | Strict invocation-envelope validation for one declared MCP server/tool pair — **`diagnostic.read`** + **signed POST**. Validates `server_id`, `tool_name`, and top-level `arguments` object before any launch path exists. |
| `POST /v1/mcp-gateway/invocation/request-approval` | Prepare a pending MCP approval object for one declared MCP server/tool pair — **`mcp_gateway.write`** + **signed POST**. Reuses validation, dedupes identical pending requests inside one control session, and still does not launch anything. |
| `POST /v1/mcp-gateway/invocation/decide-approval` | Resolve one prepared MCP approval object — **`mcp_gateway.write`** + **signed POST**. Verifies `approval_request_id`, `decision_nonce`, and, for approval grants, `approval_manifest_sha256`; appends `approval.granted` or `approval.denied`; still does not launch anything. |
| `POST /v1/mcp-gateway/invocation/validate-execution` | Validate the exact future MCP execution envelope against one granted approval — **`mcp_gateway.write`** + **signed POST**. Verifies `approval_request_id`, `approval_manifest_sha256`, and the canonical invocation body hash; appends `mcp_gateway.execution_checked`; still does not launch anything. |
| `POST /v1/mcp-gateway/invocation/execute` | Execute one approved MCP tool call against an already launched declared `stdio` server — **`mcp_gateway.write`** + **signed POST**. Re-validates the exact approval binding, appends `mcp_gateway.execution_started`, consumes the granted approval, performs a synchronous JSON-RPC `tools/call`, and appends `mcp_gateway.execution_completed` or `mcp_gateway.execution_failed`. |
| `GET /v1/audit/export/trust-check` | Read-only audit-export trust preflight summary — **`diagnostic.read`** + **signed GET with empty body**. No cursor movement and no downstream network write. |
| `POST /v1/audit/export/flush` | Trigger one local-first audit export flush to the configured downstream sink — **`audit.export`** + **signed POST with empty body**. Delivery to a configured downstream sink uses the server-side `logging.audit_export.authorization.secret_ref` and, for non-loopback TLS sinks, `logging.audit_export.tls.*` client material; the client does not supply downstream credentials on this route. |
| `POST /v1/session/open` | Obtain tokens and MAC key |
| `POST /v1/session/close` | Retire the current idle control session — **Bearer + signed POST** |
| `POST /v1/model/reply` | Model round-trip through Loopgate — **`model.reply`** |
| `POST /v1/model/validate` | Validate runtime model config — **`model.validate`** |
| `POST /v1/model/connections/store` | Store provider credentials (secret handled server-side) — **`connection.write`** |
| `POST /v1/capabilities/execute` | Execute a registered capability |
| `POST /v1/connections/validate` | Validate a configured connection — **`connection.write`** |
| `POST /v1/connections/pkce/start` / `complete` | OAuth PKCE helper flows — **`connection.write`** |
| `POST /v1/sites/inspect` / `trust-draft` | Site inspection / trust draft — **`site.inspect`** / **`site.trust.write`** |
| `POST /v1/sandbox/import` / `stage` / `export` | Sandbox mutation helpers — **`fs_write`**; host import/export additionally require a pinned client session bound to matching `operator_mount_paths`, and export requires an active operator-mount write grant |
| `POST /v1/sandbox/metadata` | Sandbox artifact metadata — **`fs_read`** |
| `POST /v1/sandbox/list` | Sandbox directory listing — **`fs_list`** |
| `POST /v1/quarantine/metadata` / `view` | Quarantine metadata / bounded payload view (**`quarantine.read`**) |
| `POST /v1/quarantine/prune` | Quarantine blob prune while preserving metadata (**`quarantine.write`**) |
| `GET` / `PUT /v1/config/…` | Policy, runtime, connections, etc. (capability-gated). `PUT /v1/config/policy` hot-reloads the already signed on-disk `core/policy/policy.yaml`; it does not trust policy content sent in the HTTP body. |
| `POST /v1/approvals/{id}/decision` | Approval decisions (approval token + manifest binding) |
| `GET /v1/ui/status` / `events` | Display-safe UI observation (signed Bearer routes; **`ui.read`**) |
| `GET /v1/ui/approvals` | Pending UI approvals for the current control session (**signed + `X-Loopgate-Approval-Token`**) |
| `POST /v1/ui/approvals/{id}/decision` | UI approval path (**signed + `X-Loopgate-Approval-Token`**, body `{ "approved": bool }`) |
| `GET` / `PUT /v1/ui/folder-access`, `POST /v1/ui/folder-access/sync`, `GET /v1/ui/shared-folder`, `POST /v1/ui/shared-folder/sync` | Folder access UI helpers (**`folder_access.read`** / **`folder_access.write`**) |

For **request/response JSON shapes**, use `internal/loopgate/types.go` as the source of truth (field names are `json` tagged).

`GET /v1/status` only includes connection summaries when the session token
includes **`connection.read`**. Use `GET /v1/connections/status` when the client
explicitly needs the connection surface.

### 7.1.1 Folder access control routes

- `GET /v1/ui/folder-access` and `GET /v1/ui/shared-folder`
  - require **`folder_access.read`**
  - return display-safe folder-access and shared-folder status projections
- `PUT /v1/ui/folder-access`, `POST /v1/ui/folder-access/sync`, and `POST /v1/ui/shared-folder/sync`
  - require **`folder_access.write`**
  - update or synchronize Loopgate-managed folder-access state

### 7.2 Operator diagnostics (“doctor” / troubleshooting)

**`GET /v1/diagnostic/report`**

- **Auth:** `Authorization: Bearer` with a valid **capability token** (same peer binding rules as other privileged routes).
- **Scope:** **`diagnostic.read`**
- **Response:** JSON aggregate for operators and in-app doctor UIs: ledger chain verification summary (`ledger_verify`), active audit JSONL line count and top event types (`ledger_active`), diagnostic logging flags (`diagnostics`), and audit-export sink / trust-material status (`audit_export`). The `audit_export.trust.*` section includes renewal-window fields such as `renewal_threshold_at_utc`, `seconds_until_renewal_threshold`, `days_until_renewal_threshold`, and `renewal_window_active`. **No** raw audit JSONL, tool payloads, private keys, tokens, or other secrets.
- **Go client:** `(*loopgate.Client).FetchDiagnosticReport(ctx, &dest)` unmarshals the same JSON.
- **CLI (no server):** `go run ./cmd/loopgate-doctor report` and `go run ./cmd/loopgate-doctor bundle -out /path/to/dir` write `report.json` plus optional tails of configured diagnostic `*.log` files.

**`GET /v1/audit/export/trust-check`**

- **Auth:** `Authorization: Bearer` with a valid capability token and the same signed GET envelope used by `/v1/diagnostic/report`.
- **Scope:** **`diagnostic.read`**
- **Behavior:** read-only audit-export trust preflight. It does **not** move the export cursor, perform a downstream POST, or trigger a new remote handshake.
- **Response:** concise operator summary with `status`, `action_needed`, `summary`, `recommended_action`, sink metadata, `last_error_class`, `consecutive_failures`, and the same `trust` projection family used under `audit_export` in the full diagnostic report.
- **Go client:** `(*loopgate.Client).CheckAuditExportTrust(ctx)`.

**`GET /v1/mcp-gateway/inventory`**

- **Auth:** `Authorization: Bearer` with a valid capability token and the same signed GET envelope used by `/v1/diagnostic/report`.
- **Scope:** **`diagnostic.read`**
- **Behavior:** returns the policy-declared MCP gateway inventory that Loopgate loaded at startup.
- **Response:** read-only JSON with `deny_unknown_servers` plus a sorted list of declared servers and declared tools. Secret injection is projected only as environment variable names; secret refs and secret values are not returned.
- **Go client:** `(*loopgate.Client).LoadMCPGatewayInventory(ctx)`.

**`POST /v1/mcp-gateway/decision`**

- **Auth:** `Authorization: Bearer` with a valid capability token and the same signed POST envelope used by other privileged body-bearing routes.
- **Scope:** **`diagnostic.read`**
- **Body:** `{ "server_id": "<required>", "tool_name": "<required>" }`
- **Behavior:** returns a typed, read-only policy decision for one MCP server/tool pair.
- **Response:** JSON with `decision` (`allow`, `needs_approval`, or `deny`), `requires_approval`, and typed `denial_code` / `denial_reason` when denied. This route does **not** launch a server or invoke a tool.
- **Go client:** `(*loopgate.Client).CheckMCPGatewayDecision(ctx, request)`.

**`GET /v1/mcp-gateway/server/status`**

- **Auth:** `Authorization: Bearer` with a valid capability token and the standard signed GET envelope for empty-body routes.
- **Scope:** **`diagnostic.read`**
- **Behavior:** projects declared MCP servers against current authoritative launched-server state and returns a safe runtime view for each declared server.
- **Runtime state values:** `absent`, `starting`, or `launched`
- **Returned fields:** `server_id`, `declared_enabled`, `transport`, `runtime_state`, and when present safe runtime facts such as `pid`, `initialized`, `started_at_utc`, `working_directory`, `command_path`, and `stderr_path`
- **Cleanup behavior:** the route performs the same request-driven dead-process cleanup used by execution and launch reuse, so a dead child is projected as `absent` rather than stale `launched`
- **Go client:** `(*loopgate.Client).LoadMCPGatewayServerStatus(ctx)`.

**`POST /v1/mcp-gateway/invocation/validate`**

- **Auth:** `Authorization: Bearer` with a valid capability token and the same signed POST envelope used by other privileged body-bearing routes.
- **Scope:** **`diagnostic.read`**
- **Body:** `{ "server_id": "<required>", "tool_name": "<required>", "arguments": { ... } }`
- **Behavior:** validates the future MCP invocation envelope without launching anything. Today that validation is intentionally narrow:
  - `server_id` and `tool_name` must be safe identifiers
  - `arguments` must be a JSON object
  - top-level argument names must be bounded and safe
  - top-level argument values must be valid JSON
  - optional policy-declared top-level argument constraints may require, allowlist, denylist, or kind-check specific argument keys
- **Audit:** successful envelope validation appends a minimal `mcp_gateway.invocation_checked` audit event containing `server_id`, `tool_name`, `validated_argument_keys`, and the resulting decision. Raw argument values are not persisted.
- **Response:** JSON with the same typed policy decision family as the decision route plus `validated_argument_count` and sorted `validated_argument_keys`.
- **Go client:** `(*loopgate.Client).ValidateMCPGatewayInvocation(ctx, request)`.

**`POST /v1/mcp-gateway/invocation/request-approval`**

- **Auth:** `Authorization: Bearer` with a valid capability token and the same signed POST envelope used by other privileged body-bearing routes.
- **Scope:** **`mcp_gateway.write`**
- **Body:** `{ "server_id": "<required>", "tool_name": "<required>", "arguments": { ... } }`
- **Behavior:** reuses the same strict invocation-envelope validation as `/v1/mcp-gateway/invocation/validate`.
  - if the validated invocation resolves to `allow` or `deny`, this route returns a typed response with no approval object and appends `mcp_gateway.invocation_checked`
  - if the validated invocation resolves to `needs_approval`, this route creates or reuses one pending MCP approval object for the control session and appends `approval.created`
- **Response:** JSON with the typed decision fields plus, on approval preparation, `approval_request_id`, `approval_decision_nonce`, `approval_manifest_sha256`, and `approval_expires_at_utc`
- **Audit:** raw argument values are not persisted in either `mcp_gateway.invocation_checked` or `approval.created`

**`POST /v1/mcp-gateway/server/stop`**

- **Auth:** `Authorization: Bearer` with a valid capability token and the same signed POST envelope used by other privileged body-bearing routes.
- **Scope:** **`mcp_gateway.write`**
- **Body:** `{ "server_id": "<required>" }`
- **Behavior:** removes the launched server from authoritative in-memory state before any future execution can resolve it, then serializes with the server's stdio mutex, closes retained pipes, kills the child if it still has a PID, and appends `mcp_gateway.server_stopped`.
- **No-op behavior:** if the server is not currently launched, Loopgate returns `200` with `"stopped": false` and does not append a stop audit event.
- **Failure truth:** if the child has already been retired but the stop audit append fails, Loopgate returns `audit_unavailable` and does **not** resurrect launched state.
- **Go client:** `(*loopgate.Client).StopMCPGatewayServer(ctx, request)`.

**`POST /v1/mcp-gateway/invocation/decide-approval`**

- **Purpose:** resolve one previously prepared MCP approval object without launching or executing an MCP server yet
- **Capability:** `mcp_gateway.write`
- **Request:** signed POST with:
  - `approval_request_id`
  - `approved`
  - `decision_nonce`
  - optional `approval_manifest_sha256` (required when `approved=true` and the pending approval carries a manifest)
- **Behavior:** validates session ownership, expiry, pending state, nonce, and manifest binding before recording the decision
  - approval grant records `approval.granted` and moves the isolated MCP approval record to `granted`
  - approval denial records `approval.denied` and moves the isolated MCP approval record to `denied`
  - this route is still approval-only; it does not launch, execute, or consume MCP tool work
- **Audit:** decision events persist only MCP approval identity and validated argument keys; raw argument values are not written to the ledger
- **Go client:** `(*loopgate.Client).DecideMCPGatewayInvocationApproval(ctx, request)`.

**`POST /v1/mcp-gateway/invocation/validate-execution`**

- **Purpose:** validate the exact future MCP execution contract after approval grant, but before any subprocess lifecycle exists
- **Capability:** `mcp_gateway.write`
- **Request:** signed POST with:
  - `approval_request_id`
  - `approval_manifest_sha256`
  - `server_id`
  - `tool_name`
  - `arguments`
- **Behavior:** verifies:
  - the approval exists and belongs to the current control session
  - the approval is in `granted` state
  - the submitted manifest matches the granted approval
  - the canonical invocation body hash of `{server_id, tool_name, arguments}` matches the granted approval exactly
- **Audit:** successful validation appends `mcp_gateway.execution_checked` with approval id, MCP identity, validated argument keys, and execution method/path. Raw argument values are not written to the ledger.
- **Go client:** `(*loopgate.Client).ValidateMCPGatewayExecution(ctx, request)`.

**`POST /v1/mcp-gateway/invocation/execute`**

- **Purpose:** execute one exact approved MCP tool call through an already launched declared `stdio` server
- **Capability:** `mcp_gateway.write`
- **Request:** the same signed POST envelope and body shape as `validate-execution`:
  - `approval_request_id`
  - `approval_manifest_sha256`
  - `server_id`
  - `tool_name`
  - `arguments`
- **Behavior:** re-validates the same exact granted approval binding as `validate-execution`, requires that the declared server is already launched, appends `mcp_gateway.execution_started`, atomically marks the MCP approval as consumed, then performs a synchronous JSON-RPC `tools/call` over the retained stdio transport. The first execution against a launched server also performs a request-driven MCP `initialize` + `notifications/initialized` handshake.
- **Failure model:** protocol / transport failures fail closed, mark the consumed approval as `execution_failed`, and drop the launched server from in-memory runtime state. There is still no background restart loop.
- **Audit:** completion records only MCP identity, validated argument keys, process pid, and result hash / byte count or remote JSON-RPC error metadata. Raw argument values and raw tool result content are not written to the ledger.
- **Response:** typed JSON including approval/session identity, process pid, and either `tool_result` plus `tool_result_sha256` or `remote_error_code` / `remote_error_message`.
- **Go client:** `(*loopgate.Client).ExecuteMCPGatewayInvocation(ctx, request)`.

### 7.5 Model control routes

Loopgate-managed remote model runtime follows a stricter secret rule than the
generic `internal/modelruntime` package:

- remote `openai_compatible` and `anthropic` configs must use
  **`model_connection_id`**
- the older `api_key_env_var` compatibility field is rejected on Loopgate's
  remote validate/inference path
- loopback `openai_compatible` remains the narrow exception for local no-auth
  model servers

- `POST /v1/model/reply`
  - requires **`model.reply`**
  - runs a model round-trip through Loopgate’s runtime and audit path
- `POST /v1/model/validate`
  - requires **`model.validate`**
  - validates runtime model configuration without executing a model round-trip
- `POST /v1/model/connections/store`
  - requires **`connection.write`**
  - stores provider credentials through Loopgate’s secure connection path
### 7.4 Sandbox and host filesystem routes

- `POST /v1/sandbox/import`, `POST /v1/sandbox/stage`, and `POST /v1/sandbox/export`
  - require **`fs_write`**
  - mutate sandbox or host-adjacent artifact state through Loopgate-owned paths
  - `import` and `export` fail closed unless the control session is bound to matching operator mount paths from a session opened by the pinned expected client executable
  - `export` also requires an active operator-mount write grant for the matched root; otherwise Loopgate returns `approval_required`
- `POST /v1/sandbox/metadata`
  - requires **`fs_read`**
  - returns metadata for a staged output artifact
- `POST /v1/sandbox/list`
  - requires **`fs_list`**
  - returns a directory listing inside the sandbox home

---

## 8. Response and error model

- Many endpoints return **200** with a JSON body that includes `status: "denied"` or `status: "error"` — always parse the body; do not assume HTTP status alone equals success.
- **`CapabilityResponse`**-shaped errors include `denial_code` and `denial_reason` (safe strings; still do not log tokens).

---

## 9. Delegated sessions (legacy helper note)

There is still legacy helper code for delegated-session continuation in the Go
client, but it is **not** part of the current recommended Loopgate operator
path.

Important current constraint:

- the generic delegated-session helper does **not** relax Unix peer binding
- a distinct OS subprocess reusing another process's capability token will still be denied
- the supported integration path is still a fresh session opened by the real local client process

---

## 10. Checklist for Swift integration

1. Start Loopgate; confirm socket exists and is `0600` or stricter as created by server.
2. Open **Unix** connection to `loopgate.sock`.
3. Optional: `GET /v1/health` to confirm the process is listening (no secrets returned).
4. `POST /v1/session/open` with `actor`, `session_id`, `requested_capabilities` (non-empty; use a **client-shipped allowlist** intersected with product expectations, or open with one known tool then refresh after status — see §5).
5. Store **tokens + MAC key** in secure process memory; never log them.
6. `GET /v1/status` (and other privileged GETs) with **Bearer + signed envelope** per §6.
7. For each privileged call: fresh nonce, correct body hash, HMAC as above, `Authorization: Bearer`.
8. On `approval_required` responses, drive operator UI and use **approval token** + decision nonce paths per RFC 0001/0005.

---

## 11. Related docs

- [SETUP.md](./SETUP.md) — minimal repo setup and pointers to HTTP and deprecated-MCP docs.
- [SECRETS.md](./SECRETS.md) — how secrets are supposed to flow (Loopgate-owned).
- [loopgate.md](../design_overview/loopgate.md) — product/system overview.
- [RFC 0001](../rfcs/0001-loopgate-token-policy.md) — token and signing rules.

When this document and code disagree, **code wins** — file a bug to update the doc.
