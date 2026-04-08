**Last updated:** 2026-04-07

# Loopgate local HTTP API — guide for HTTP-native clients

This document explains how a **local process** (native app, test harness, or bridge) talks to **Loopgate** over **HTTP on a Unix domain socket**. It is the **only supported in-tree v1 transport** for privileged control-plane calls. **In-tree MCP is deprecated and removed** ([ADR 0010](../adr/0010-macos-supported-target-and-mcp-removal.md)); use this HTTP API directly or an **out-of-tree** MCP→HTTP forwarder if needed.

**Normative details:** [RFC 0001: Loopgate Token and Request Integrity Policy](../rfcs/0001-loopgate-token-policy.md).  
**Reference implementation:** `internal/loopgate/client.go` (Go) — match its wire behavior byte-for-byte when in doubt.

### Neutral routes, legacy aliases, and the `haven` actor

Prefer the neutral routes in this document, such as **`/v1/chat`** and **`/v1/continuity/inspect-thread`**. Loopgate still keeps the historical **`/v1/haven/...`** routes as compatibility aliases for existing local HTTP clients. The session **actor label `haven`** is also still used by a few compatibility paths; it is a stable identifier, not a product boundary. **Prefer this HTTP API** for new IDE and native integrations (in-tree MCP removed — see [LOOPGATE_MCP.md](./LOOPGATE_MCP.md)).

---

## 1. What you are connecting to

| Item | Detail |
|------|--------|
| **Process** | Loopgate — local authority for capabilities, approvals, audit, secrets, sandbox, memory, etc. |
| **Wire protocol** | **HTTP/1.1** (JSON bodies, `Content-Type: application/json`) |
| **Transport** | **Unix domain stream socket** only in v1 — **not** TCP to `localhost` |
| **Default socket path** | `{repoRoot}/runtime/state/loopgate.sock` when Loopgate is started with cwd = repo root (`cmd/loopgate`) |
| **Host header** | Any stable placeholder is fine; the Go client uses `http://loopgate` as the base URL. The server routes by path, not host. |
| **Override path** | Environment variable **`LOOPGATE_SOCKET`** — absolute path to the socket file. Supported by `./start.sh` and `./scripts/start-loopgate-swift-dev.sh` so clients and launcher agree without hardcoding. |

There is **no** public HTTP listener for the control plane in v1. Apple XPC is optional future work and **not** required for this API.

### 1.1 macOS App Sandbox and `homeDirectoryForCurrentUser`

If your macOS app is **sandboxed** (typical for a signed `.app` in `/Applications`), `FileManager.default.homeDirectoryForCurrentUser` is **not** `/Users/<you>` — it is the app’s **container**, e.g. `~/Library/Containers/<bundle-id>/Data/`. Building paths like `\(home)/Dev/<checkout>/runtime/state/loopgate.sock` therefore becomes `…/Containers/…/Data/Dev/<checkout>/…`, which will not match a Loopgate process you started from a normal shell in the real repo.

**Do not use a hard-coded “home + Dev/…” checkout path as the only resolver inside a sandboxed app.**

Practical approaches:

1. **`LOOPGATE_SOCKET` first** — Read `ProcessInfo.processInfo.environment["LOOPGATE_SOCKET"]` (or the value your launcher sets). If present and non-empty, use it. This matches typical repo launch scripts for Loopgate and is the fastest fix for dev.
2. **Operator-configured path** — Settings field or “Choose Loopgate repository…” (`NSOpenPanel`) plus a **security-scoped bookmark** so the sandboxed app can reconnect after relaunch. Store the socket path (or repo root) the user picked.
3. **Shipped / agreed layout** — For a consumer install, pick one authoritative layout (for example under `~/Library/Application Support/<YourProduct>/…`) and run Loopgate (or a small unsandboxed helper) so it **creates the socket at that path**. The UI app and the daemon must share the same contract; the real user home is only reachable from unsandboxed code or from paths the user has granted.

**`$PATH` does not apply** to Unix socket locations. Use an explicit **`LOOPGATE_SOCKET`** (or app-specific equivalent, e.g. `MORPH_LOOPGATE_SOCKET`) passed by the launcher, plist `LSEnvironment`, or Xcode scheme environment for debug builds.

**Connecting, not just path resolution:** Even with the correct absolute path, a **sandboxed** GUI app may still fail `connect()` to a socket under an arbitrary `~/Dev/…` checkout because the sandbox treats that as access to a file **outside the container**. `com.apple.security.network.client` does **not** grant that. For local development, use a **Debug** build **without** App Sandbox, or ship an **unsandboxed helper** / agreed socket location both processes can access. Wrong-path fixes alone will not unblock sandboxed `connect()`.

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

Policy hash changes may require:

```bash
go run ./cmd/loopgate --accept-policy
```

See [SETUP.md](./SETUP.md) for repo layout, runtime paths, and policy/runtime config.

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

**Executable path pinning:** When **`control_plane.expected_session_client_executable`** in `config/runtime.yaml` is a non-empty absolute path, Loopgate compares it (after `filepath.Clean`) to the connecting peer’s resolved executable at **`POST /v1/session/open`**. A mismatch returns **403** with `denial_code` **`process_binding_rejected`**. The repository default is **empty** (pinning off). Set this in production desktop bundles where the client path is stable.

**Haven trusted-sandbox auto-allow:** If the session **`actor`** is **`haven`** and a tool implements **`TrustedSandboxLocal()`**, Loopgate may treat **`NeedsApproval`** policy as **`Allow`** for that capability (audit still records the decision). Operators can tighten this in **`core/policy/policy.yaml`** → **`safety`**: **`haven_trusted_sandbox_auto_allow`** (`false` to disable), and optionally **`haven_trusted_sandbox_auto_allow_capabilities`** (omit = all such tools; `[]` = none; non-empty list = allowlist by capability name).

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
| `workspace_id` | No | Optional workspace binding when used by multi-surface clients. |
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

### Session MAC key rotation (12-hour epochs)

`session_mac_key` is **derived** from a server-held master secret and the **control session id**, and changes each **12-hour UTC epoch**. Loopgate accepts signatures built with the **previous**, **current**, or **next** epoch’s derived key so clients can cross a single boundary without dropping traffic.

**`GET /v1/session/mac-keys`** — same authentication as **`GET /v1/status`**: `Authorization: Bearer …` plus the **signed GET envelope** (§6, empty body). Response JSON includes:

- `rotation_period_seconds` — always **43200** (12 hours).
- `derived_key_schema` — **`loopgate-session-mac-v1`** (stable identifier for the derivation rule).
- `current_epoch_index` — non-negative epoch counter.
- **`previous`**, **`current`**, **`next`** — each has `slot`, `epoch_index`, `valid_from_utc`, `valid_until_utc`, `epoch_key_material_hex` (32-byte key as 64 hex chars), and `derived_session_mac_key` (the **64-hex-character** string to use as `session_mac_key` UTF-8 for HMAC, same shape as session open).

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
| `GET /v1/connections/status` | Connection summaries — **Bearer + signed GET** |
| `POST /v1/session/open` | Obtain tokens and MAC key |
| `POST /v1/model/reply` | Model round-trip through Loopgate |
| `POST /v1/model/validate` | Validate runtime model config |
| `POST /v1/model/connections/store` | Store provider credentials (secret handled server-side) |
| `POST /v1/capabilities/execute` | Execute a registered capability |
| `POST /v1/connections/validate` | Validate a configured connection |
| `POST /v1/connections/pkce/start` / `complete` | OAuth PKCE helper flows |
| `POST /v1/sites/inspect` / `trust-draft` | Site inspection / trust draft |
| `POST /v1/sandbox/*` | import, stage, metadata, export, list |
| `POST /v1/continuity/inspect` | Continuity thread inspection (caller supplies `events` JSON) |
| `POST /v1/continuity/inspect-thread` | **Actor `haven` only** for the current compatibility gate — signed POST; body `{ "thread_id": "…" }`; Loopgate loads the thread from `internal/threadstore` and proposes continuity (client does **not** send transcript payloads) |
| `GET /v1/memory/wake-state` | Wake state projection |
| `GET` / `PUT /v1/tasks` … | Task board sync |
| `GET /v1/memory/diagnostic-wake` | Diagnostic wake |
| `POST /v1/memory/discover` / `recall` / `remember` | Memory surfaces |
| `POST /v1/memory/inspections/…` | Inspection governance |
| `POST /v1/morphlings/*` | Bounded worker (**morphling**) lifecycle + worker IPC |
| `POST /v1/quarantine/*` | Quarantine metadata / view / prune |
| `POST /v1/task/plan` / `lease` / `execute` / `complete` / `result` | Task-plan vertical slice |
| `GET` / `PUT /v1/config/…` | Policy, runtime, connections, etc. (capability-gated) |
| `POST /v1/approvals/{id}/decision` | Approval decisions (approval token + manifest binding) |
| `GET /v1/ui/status` / `events` | Display-safe UI observation (signed Bearer routes) |
| `GET /v1/ui/approvals` | Pending UI approvals for the current control session (**signed + `X-Loopgate-Approval-Token`**) |
| `POST /v1/ui/approvals/{id}/decision` | UI approval path (**signed + `X-Loopgate-Approval-Token`**, body `{ "approved": bool }`) |
| `GET` / `POST /v1/ui/folder-access*` | Folder access UI helpers |
| `GET /v1/ui/desk-notes` | Active desk (sticky) notes from `runtime/state/haven_desk_notes.json` (signed GET) |
| `POST /v1/ui/desk-notes/dismiss` | Archive a desk note by id (signed POST) |
| `GET /v1/ui/memory` | Display-safe memory inventory for operator UI controls (signed GET; manageable objects, counts, redacted summaries) |
| `POST /v1/ui/memory/reset` | Archive current memory state and start fresh for demo/operator reset (signed POST; body `operation_id`, `reason`) |
| `GET /v1/ui/journal/entries` | Journal entry summaries (signed GET; lists sandbox `scratch/journal`) |
| `GET /v1/ui/journal/entry` | Single journal file (signed GET; query selects entry) |
| `GET /v1/ui/working-notes` | Working-note summaries (signed GET; `scratch/notes`) |
| `GET /v1/ui/working-notes/entry` | Single working note (signed GET) |
| `POST /v1/ui/working-notes/save` | Save working note content (signed POST; uses `notes.write` capability) |
| `POST /v1/ui/workspace/list` | Workspace listing for sandbox virtual paths (signed POST; body `path`; root lists `projects`, `imports`, `artifacts`, `research`, `agents`, and optional `shared`) |
| `POST /v1/ui/workspace/preview` | Read workspace file preview (signed POST; body `path`, using the same virtual path mapping as the list route) |
| `GET /v1/ui/presence` | Presence projection from `runtime/state/haven_presence.json` (signed GET); written by clients that implement presence |
| `GET /v1/ui/morph-sleep` | Same snapshot as presence plus `is_sleeping` / `is_resting` (signed GET) |
| `POST /v1/agent/work-item/ensure` | **Actor `haven` only** for the current compatibility gate — signed POST; runs **`todo.add`** with `source_kind: haven_agent` (dedupes by text; see §7.2) |
| `POST /v1/agent/work-item/complete` | **Actor `haven` only** for the current compatibility gate — signed POST; runs **`todo.complete`** for a task-board item id |
| … | Other `/v1/ui/*` task standing grants, shared folder — see `server.go` |

For **request/response JSON shapes**, use `internal/loopgate/types.go` as the source of truth (field names are `json` tagged).

### 7.1 Memory operator routes (UI-safe)

These routes exist so native clients can manage memory through Loopgate's typed
surface instead of direct runtime-state reads or writes.

- `GET /v1/ui/memory`
  - returns a display-safe memory inventory
  - includes wake-state counts plus a list of manageable memory objects
  - object summaries are redacted for UI safety
  - each object carries booleans such as `can_review`, `can_tombstone`, and
    `can_purge`
- `POST /v1/ui/memory/reset`
  - performs an operator-visible "fresh start" reset
  - archives the previous memory root under
    `runtime/state/memory_archives/<archive_id>`
  - reinitializes the authoritative continuity state
  - returns counts for the archived inspection, distillate, and resonate-key
    objects

This reset path is intentionally fail-closed and auditable. It does **not**
silently delete memory in place.

### 7.2 Agent work-item helpers (bounded task board; actor `haven`)

These routes let a client using **actor label `haven`** create or complete **Task Board** items through the **same** capability execution path as `POST /v1/capabilities/execute` for `todo.add` / `todo.complete` — policy, audit, and continuity hooks apply unchanged. They do **not** grant new authority; the session token must already include **`todo.add`** (ensure) or **`todo.complete`** (complete).

**`POST /v1/agent/work-item/ensure`**

- **Auth:** `Authorization: Bearer` + **signed body** (same rules as other signed POSTs for this actor; see §6).
- **Body:** `{ "text": "<required>", "next_step": "<optional>" }` — `text` is the human-visible task line (trimmed server-side).
- **Behavior:** Executes `todo.add` with `task_kind` carry-over, `source_kind: haven_agent`, and optional `next_step`. If an equivalent item already exists, the structured result sets `already_present: true` and returns the same `item_id`.
- **Success (200):** `{ "item_id": "…", "text": "…", "already_present": bool }` — see `HavenAgentWorkItemResponse` in `internal/loopgate/types.go` (Go identifier retained for compatibility).

**`POST /v1/agent/work-item/complete`**

- **Auth:** same as ensure; requires **`todo.complete`** on the token.
- **Body:** `{ "item_id": "<required>", "reason": "<optional>" }` — default reason if omitted: `haven_agent_work_completed`.
- **Success (200):** same JSON shape as ensure (`already_present` is always false on this path).

**Product note:** Classification of user messages (answer-only vs task vs tool vs approval-gated), UI phase (`planning`, `waiting_for_approval`, etc.), and deep-link behavior are **unprivileged client** responsibilities. Loopgate only exposes narrow, auditable capability wrappers. Simple host-folder work typically flows through **`/v1/chat`** and normal approvals; use ensure/complete when the client wants an explicit Task Board row.

### 7.3 Continuity inspection (threadstore-loaded; actor `haven`)

**`POST /v1/continuity/inspect-thread`**

- **Auth:** Actor label **`haven`** + **signed body** (same pattern as `POST /v1/chat` for the current compatibility gate).
- **Body:** `{ "thread_id": "<required>" }` — must match a thread in Loopgate’s threadstore for the session workspace (on-disk implementation under `internal/threadstore`; default paths may use a **`.haven`** segment as a historical directory name—treat as an implementation detail, not a product name).
- **Behavior:** Maps persisted thread events to continuity inspection input server-side and runs the same inspection pipeline as other continuity proposals. If the thread has no mappable continuity rows, returns **200** with `submit_status: "skipped_no_continuity_events"`.
- **Success (200):** `HavenContinuityInspectThreadResponse` in `internal/loopgate/types.go` (`submit_status`, optional `inspection_id`, derivation/review fields; Go identifier retained for compatibility).
- **Product:** Clients may call this **best-effort after a completed chat turn** when the turn did **not** stop for `approval_required`, so operators get continuity proposals without shipping raw transcripts over HTTP.

### 7.4 Operator diagnostics (“doctor” / troubleshooting)

**`GET /v1/diagnostic/report`**

- **Auth:** `Authorization: Bearer` with a valid **capability token** (same peer binding rules as other privileged routes).
- **Response:** JSON aggregate for operators and in-app doctor UIs: ledger chain verification summary (`ledger_verify`), active audit JSONL line count and top event types (`ledger_active`), diagnostic logging flags (`diagnostics`). **No** raw audit JSONL, tool payloads, or secrets.
- **Go client:** `(*loopgate.Client).FetchDiagnosticReport(ctx, &dest)` unmarshals the same JSON.
- **CLI (no server):** `go run ./cmd/loopgate-doctor report` and `go run ./cmd/loopgate-doctor bundle -out /path/to/dir` write `report.json` plus optional tails of configured diagnostic `*.log` files.

---

## 8. Response and error model

- Many endpoints return **200** with a JSON body that includes `status: "denied"` or `status: "error"` — always parse the body; do not assume HTTP status alone equals success.
- **`CapabilityResponse`**-shaped errors include `denial_code` and `denial_reason` (safe strings; still do not log tokens).

---

## 9. Delegated sessions (advanced)

If a parent process opens Loopgate and passes tokens to a child via a **launch-bound channel**, follow [RFC 0002: Delegated Session Refresh and Pipe Contract](../rfcs/0002-delegated-session-refresh.md). The Go helper is `NewClientFromDelegatedSession`.

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
