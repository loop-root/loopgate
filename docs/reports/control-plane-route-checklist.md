# Control-plane route checklist (review aid)

Use this when adding or changing HTTP routes on the Loopgate mux (`internal/loopgate/server.go`, `NewServer` / route registration). Goal: every `/v1/...` path has explicit **transport**, **auth**, **request MAC** (where applicable), and **audit** expectations.

## PR hygiene

- [ ] If you changed `mux.HandleFunc` paths or handler auth/MAC behavior, updated this file (or the route table below) and noted any new primary audit event types.

## Auth categories (typical)

| Category | Meaning |
|----------|---------|
| **none** | No Bearer / approval / worker token (still usually requires local UDS peer identity where the listener enforces it). |
| **peer** | Authenticated local peer (e.g. UDS credential passing); see `peerIdentityFromContext`. |
| **Bearer** | `Authorization: Bearer …` capability token; validated in `authenticate`. |
| **MAC** | Session-bound HMAC on the canonical request envelope (`verifySignedRequestWithoutBody`, `readAndVerifySignedBody`, etc.). |
| **approval** | `X-Loopgate-Approval-Token` and related approval-session checks (`authenticateApproval`). |
| **worker** | Morphling worker launch/open/session path (see `server_morphling_worker_handlers.go` — not the same as Bearer for many worker endpoints). |

Exact rules live in each handler; this table is a map for reviewers, not a substitute for reading code.

## Exceptions (verify in code)

| Route | Method | Auth + MAC (summary) |
|-------|--------|----------------------|
| `/v1/health` | GET | **none** (no Bearer, no MAC). |
| `/v1/session/open` | POST | **peer** + JSON body validation; **no** Bearer/MAC until tokens are returned. |
| `/v1/session/mac-keys` | GET | Same as **`GET /v1/status`**: **peer** + **Bearer** + signed GET (empty body). |
| `/v1/morphlings/worker/open` | POST | **peer** + worker open body (see handler). |
| `/v1/ui/approvals` (and related UI approval routes) | varies | Often **approval** token path instead of Bearer (see `authenticateApproval`). |
| Most other `/v1/...` routes | varies | Typically **peer** + **Bearer** + **MAC** (GET empty-body vs POST signed body per handler). |

## Route inventory (source of truth: `server.go`)

The following paths are registered next to `NewServer` (search for `mux.HandleFunc`). When you add a line there, add or adjust a row here.

- `/v1/health`
- `/v1/status`
- `/v1/ui/status`, `/v1/ui/events`, `/v1/ui/approvals`, `/v1/ui/approvals/…`
- `/v1/ui/folder-access`, `/v1/ui/folder-access/sync`
- `/v1/ui/task-standing-grants`
- `/v1/ui/shared-folder`, `/v1/ui/shared-folder/sync`
- `/v1/ui/desk-notes/dismiss`, `/v1/ui/desk-notes`
- `/v1/ui/journal/entries`, `/v1/ui/journal/entry`
- `/v1/ui/paint/gallery`
- `/v1/ui/working-notes/save`, `/v1/ui/working-notes/entry`, `/v1/ui/working-notes`
- `/v1/ui/workspace/list`, `/v1/ui/workspace/host-layout`, `/v1/ui/workspace/preview`
- `/v1/ui/memory/reset`, `/v1/ui/memory`
- `/v1/ui/morph-sleep`, `/v1/ui/presence`
- `/v1/diagnostic/report`
- `/v1/session/open`, `/v1/session/mac-keys`
- `/v1/model/reply`, `/v1/model/validate`, `/v1/model/ollama/tags`, `/v1/model/openai/models`, `/v1/model/connections/store`
- `/v1/chat`, `/v1/settings/shell-dev`, `/v1/settings/idle`, `/v1/model/settings`
- `/v1/resident/journal-tick`
- `/v1/agent/work-item/ensure`, `/v1/agent/work-item/complete`
- `/v1/continuity/inspect-thread`
- `/v1/haven/…` (parallel aliases for several Haven routes)
- `/v1/capabilities/execute`
- `/v1/connections/status`, `/v1/connections/validate`, `/v1/connections/pkce/start`, `/v1/connections/pkce/complete`
- `/v1/sites/inspect`, `/v1/sites/trust-draft`
- `/v1/sandbox/import`, `/v1/sandbox/stage`, `/v1/sandbox/metadata`, `/v1/sandbox/export`, `/v1/sandbox/list`
- `/v1/memory/wake-state`, `/v1/memory/diagnostic-wake`, `/v1/memory/discover`, `/v1/memory/recall`, `/v1/memory/remember`, `/v1/memory/inspections/…`
- `/v1/tasks`, `/v1/tasks/…`
- `/v1/morphlings/spawn`, `/v1/morphlings/status`, `/v1/morphlings/terminate`, `/v1/morphlings/review`
- `/v1/morphlings/worker/launch`, `…/open`, `…/start`, `…/update`, `…/complete`
- `/v1/quarantine/metadata`, `/v1/quarantine/view`, `/v1/quarantine/prune`
- `/v1/task/plan`, `/v1/task/lease`, `/v1/task/execute`, `/v1/task/complete`, `/v1/task/result`
- `/v1/config/…`
- `/v1/approvals/…`
- `/v1/hook/pre-validate`

## Audit reminder

Security-relevant denials and many successes append via `logEvent` / ledger paths. If a new route performs capability execution, approvals, morphling lifecycle, or secret-adjacent work, confirm the audit event is not skipped on failure modes that must fail closed (see `AGENTS.md`).

## Current explicit control-scope reminders

- `/v1/diagnostic/report` should require **`diagnostic.read`**
- `/v1/model/reply` should require **`model.reply`**
- `/v1/model/validate` should require **`model.validate`**
- `/v1/model/connections/store` should require **`connection.write`**
- `/v1/model/settings` should require actor **`haven`** plus **`model.settings.read`** / **`model.settings.write`**
- `/v1/model/openai/models` and `/v1/model/ollama/tags` should require actor **`haven`** plus **`connection.write`**
- `/v1/sandbox/import`, `/v1/sandbox/stage`, and `/v1/sandbox/export` should require **`fs_write`**
- `/v1/sandbox/metadata` should require **`fs_read`**
- `/v1/sandbox/list` should require **`fs_list`**
- `/v1/settings/shell-dev` and `/v1/settings/idle` should require actor **`haven`** plus **`config.read`** / **`config.write`**
- Haven filesystem projection routes should use the same route scopes as their underlying capability class:
  - listing surfaces => **`fs_list`**
  - file content surfaces => **`fs_read`**
  - working-note save => **`notes.write`**
