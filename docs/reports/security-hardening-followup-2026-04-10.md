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

The remaining signed-only Haven projection endpoints are smaller-risk UI state
surfaces rather than direct authority or filesystem mutation paths. Examples:

- `/v1/ui/status`
- `/v1/ui/events`
- `/v1/ui/desk-notes`
- `/v1/ui/presence`
- `/v1/ui/morph-sleep`

Those may still merit a future least-privilege pass, but they are not the same
class of direct bypass as the routes remediated here.
