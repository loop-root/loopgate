# Security hardening follow-up (2026-04-10)

This report records the second security hardening pass after the initial
current-state review and route-scope cleanup.

## What changed

- Added explicit control scopes for model and diagnostic control-plane routes:
  - `diagnostic.read`
  - `model.reply`
  - `model.validate`
  - `model.settings.read`
  - `model.settings.write`
- Gated these routes accordingly:
  - `/v1/diagnostic/report`
  - `/v1/model/reply`
  - `/v1/model/validate`
  - `/v1/model/connections/store`
  - `/v1/model/settings`
  - `/v1/model/openai/models`
  - `/v1/model/ollama/tags`
- Fixed two client bugs uncovered by the hardening work:
  - `StoreModelConnection(...)` now sends the secret-bearing wire payload instead
    of the redacted JSON shape used for safe logging/marshaling.
  - signed requests with query parameters now MAC the canonical path
    (`request.URL.Path`) instead of the raw path-plus-query string.
- Closed a second route-governance gap on sandbox and Haven filesystem
  projections by applying route-level scope checks that match the underlying
  capability class:
  - sandbox mutation routes => `fs_write`
  - sandbox metadata => `fs_read`
  - sandbox list => `fs_list`
  - Haven journal / working-notes / workspace listing surfaces => `fs_list`
  - Haven entry / preview surfaces => `fs_read`
  - Haven working-note save => `notes.write`
  - Haven shell-dev / idle settings => `config.read` / `config.write`

## Why

The first hardening pass closed obvious control-capability drift on memory,
connection, site-trust, and operator mount routes. A second review found three
more classes of mismatch:

1. model and diagnostic routes were still signed and session-bound, but not
   least-privilege scoped
2. the client had two latent correctness bugs on secret-bearing model
   connection writes and query-string request signing
3. some Haven and sandbox routes were calling internal filesystem helpers
   directly instead of requiring the same scope the underlying capability path
   would have required

These were real authority and correctness gaps, not documentation-only drift.

## Invariant impact

- Preserves the rule that signed local transport is not enough by itself;
  explicit route scope is required for privileged actions.
- Reduces opportunities for typed control-plane helpers to become side doors
  around the capability boundary.
- Keeps the governed execution path real for model setup, sandbox operations,
  and Haven filesystem projections.

## Security impact

- Narrows outbound model probing and model-runtime mutation to explicit scopes.
- Narrows sandbox and Haven filesystem projections to the same capability family
  they would need through the ordinary capability-execution path.
- Fixes a secret-bearing client request path that would otherwise self-redact
  before transmission.
- Fixes a signed GET bug for query-bearing routes so clients and server agree on
  the request MAC envelope.

## Concurrency impact

None material. This pass tightened request-time checks and client request
construction. It did not add background work, new shared mutable state, or new
lock ordering.

## Recovery / crash impact

None material. The changes fail closed earlier in the request path; they do not
change append-only or crash-recovery behavior.

## Verification

Focused regressions:

```bash
go test ./internal/loopgate -run 'Test(StatusAdvertisesAdditionalControlCapabilities|DiagnosticRouteRequiresScopedCapability|ModelRoutesRequireScopedCapabilities|HavenModelRoutesRequireScopedCapabilities|SandboxRoutesRequireCapabilityScopes|HavenProjectionRoutesRequireCapabilityScopes|HavenSettingsRoutesRequireConfigScopes|FolderAccessRoutesRequireScopedCapabilities|TaskStandingGrantRoutesRequireScopedCapabilities|TaskRoutesRequireScopedCapabilities)' -count=1
```

Broader verification:

```bash
go test ./internal/loopgate
go test ./...
```

## Remaining lower-risk review surface

Follow-up pass completed after this report:

- Added `ui.read` / `ui.write` and applied them to the previously signed-only UI
  projection routes:
  - `/v1/ui/status`
  - `/v1/ui/events`
  - `/v1/ui/desk-notes`
  - `/v1/ui/desk-notes/dismiss`
  - `/v1/ui/presence`
  - `/v1/ui/morph-sleep`
- Tightened `haven_presence.json` handling so Loopgate now projects only a
  normalized state/anchor summary instead of replaying raw `status_text` /
  `detail_text` from an untrusted runtime file.

That closes the leftover signed-only UI mutation path and removes one more
client/file-originated text leak from the public projection surface.

## Later drift cleanup on delegated sessions

Another follow-up sweep found contract drift rather than a live tenant bypass:

- `DelegatedSessionConfig` still exposed `TenantID` / `UserID` fields even
  though the delegated-session envelope, validation, and client state never
  used them
- several docs and comments still described generic delegated-session reuse as
  if it were the current cross-process bridge/bootstrap story
- the repo still carried `cmd/morphling-runner` and `taskplan_runner.go` as a
  compatibility/task-plan seam, but the current peer-binding rules mean a
  distinct subprocess reusing a parent process's delegated capability token is
  expected to fail

What changed:

- removed the stale tenant/user fields from `DelegatedSessionConfig`
- updated the delegated-session RFC and HTTP docs to say explicitly that the
  generic helper preserves peer binding and is only appropriate for same-peer
  continuation today
- updated task-plan runner comments and repo maps so `cmd/morphling-runner` is
  no longer described as the real cross-process execution path

Why this matters:

- it removes a misleading compatibility surface that implied delegated
  credentials carried more authority than they do
- it aligns the docs with the actual security model instead of leaving a future
  bridge story sounding already implemented
- it keeps the real cross-process path explicit: dedicated morphling worker
  launch/open sessions, not generic delegated-session reuse

## Later route-scope cleanup on task-plan prototype routes

Another follow-up sweep found one more live signed-only route class:

- `/v1/task/plan`
- `/v1/task/lease`
- `/v1/task/execute`
- `/v1/task/complete`
- `/v1/task/result`

These handlers were already session-bound and request-MAC protected, but they
were not explicitly control-capability gated.

What changed:

- added `task_plan.write` for submit / lease / execute / complete
- added `task_plan.read` for result lookup
- updated status capability advertising and route-scope regressions
- updated the HTTP and design docs so the task-plan seam no longer looks like a
  signed-only exception

Why this matters:

- it closes another route-governance drift path where signed transport could be
  mistaken for sufficient authority
- it keeps the prototype seam aligned with the same least-privilege rules as
  the rest of the control plane

## Later route-scope cleanup on quarantine routes

The next parity sweep found the quarantine handlers were still signed-only:

- `/v1/quarantine/metadata`
- `/v1/quarantine/view`
- `/v1/quarantine/prune`

That meant any signed session token could inspect quarantined payload metadata,
read bounded quarantined content, or prune stored blobs without an explicit
route scope.

What changed:

- added `quarantine.read` for metadata and view
- added `quarantine.write` for prune
- updated status capability advertising and route-scope regressions
- updated quarantine integration coverage to request the new control scopes
- updated HTTP/design/checklist docs so quarantine is no longer documented like
  a signed-only exception

Why this matters:

- quarantined payloads are exact evidence records; reading or pruning them is a
  privileged operator action, not something any signed session should inherit
- this closes another authority leak where transport integrity existed without
  least-privilege route binding

## Later route-scope cleanup on morphling lifecycle routes

The next parity sweep found the parent-session morphling routes were also
signed-only:

- `/v1/morphlings/spawn`
- `/v1/morphlings/status`
- `/v1/morphlings/terminate`
- `/v1/morphlings/review`
- `/v1/morphlings/worker/launch`

This was a real privilege boundary issue, not a stylistic inconsistency:
`spawnMorphling` derives child capabilities from class policy and the requested
set, not from the parent token's executable tool scopes. Without an explicit
route scope, any signed session could reach the morphling lifecycle surface.

What changed:

- added `morphling.read` for status
- added `morphling.write` for spawn / terminate / review / worker launch
- added explicit route-scope regressions for morphling lifecycle paths
- updated morphling contract tests that were still opening secondary sessions
  from executable capabilities only
- updated the HTTP/design/checklist docs so morphling lifecycle is no longer
  described as a signed-only Bearer surface

Why this matters:

- it prevents signed-session tokens from treating morphling lifecycle as an
  ambient privilege escalation surface
- it keeps child worker creation behind an explicit operator/client grant
  instead of relying only on class policy and per-session ownership checks
