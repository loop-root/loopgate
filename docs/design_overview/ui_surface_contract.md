**Last updated:** 2026-03-28

# UI Surface Contract

This document defines the source-of-truth rules for any **operator-client** UI that renders Loopgate state.

It exists so browser, desktop, and terminal work do not quietly invent a
second control plane.

## 1) Authority model

Loopgate remains the sole control plane.

Unprivileged UI surfaces may:

- render status
- render approved display-safe events
- show pending approvals
- submit user approval decisions
- show explicit denials and warnings

They may not:

- apply their own policy decisions
- re-classify Loopgate results or events
- widen prompt or memory eligibility
- export secrets, tokens, or secure-store material
- become a fallback execution path if Loopgate is unavailable

If a UI path needs authority, that authority belongs in Loopgate.

## 2) Source of truth

UI state must come from typed, display-safe Loopgate APIs.

The UI must not use any of the following as its primary source of truth:

- raw ledger tailing
- raw Loopgate audit event files
- direct filesystem reads of runtime state
- ad hoc parsing of command output

Required pattern:

`Loopgate typed UI API -> presentation adapter or local UI client -> rendering`

Current implemented UI API in this repository:

- `GET /v1/ui/status` (`ui.read`)
- `GET /v1/ui/events` (`ui.read`)
- `GET /v1/ui/approvals` using `X-Loopgate-Approval-Token` for the current control session
- `POST /v1/ui/approvals/{id}/decision` using `X-Loopgate-Approval-Token` and body `{ "approved": bool }`
- `POST /v1/ui/workspace/list` for local-client workspace roots and mapped sandbox paths
- `POST /v1/ui/workspace/preview` for local-client file preview
- `GET /v1/ui/working-notes`, `GET /v1/ui/working-notes/entry`, `POST /v1/ui/working-notes/save`
- `GET /v1/ui/journal/entries`, `GET /v1/ui/journal/entry`
- `GET /v1/ui/desk-notes` (`ui.read`), `POST /v1/ui/desk-notes/dismiss` (`ui.write`)
- `GET /v1/ui/memory` for display-safe memory inventory and operator-manageable memory objects
- `POST /v1/ui/memory/reset` for auditable archive-and-fresh-start demo resets
- `GET /v1/ui/presence`, `GET /v1/ui/morph-sleep` (`ui.read`; normalized projection only, not raw file text)

The memory inventory/reset routes exist so UI clients can inspect, tombstone,
purge, and reset memory through Loopgate's typed contract rather than direct
reads or writes under `runtime/state/memory`.

## 3) Event and result classification

Loopgate classifies every event and capability result before it reaches a UI.

Minimum classes:

- `display`
- `display_only`
- `audit_only`
- `quarantined`

UI code must treat Loopgate classification as authoritative.

Required behavior:

- `display` may render and may enter prompt/memory only if Loopgate says it is
  eligible
- `display_only` may render, but must not enter prompt or memory paths unless
  Loopgate explicitly marks it eligible
- `audit_only` must not render as normal user output
- `quarantined` must render only as a placeholder or redacted summary

Missing, malformed, or contradictory classification metadata is a denial, not a
hint to infer intent locally.

## 4) Prompt and memory safety

Raw transport payloads are not prompt input by default.

This includes:

- raw HTTP bodies
- raw integration payloads
- quarantined model output
- raw file content from capabilities that are not prompt-eligible

UI layers must not override Loopgate prompt or memory eligibility.

## 5) Bridge-server rules

If a bridge server exists, it is a presentation adapter only.

It may:

- terminate browser HTTP
- hold Loopgate transport credentials in memory
- proxy typed UI APIs
- proxy approval submissions
- proxy SSE or equivalent display-safe event feeds

It may not:

- tail raw ledger files
- perform policy enforcement
- invent its own approval state
- store long-lived credentials
- expose Loopgate tokens or MAC material to the browser

The bridge bootstrap path must also preserve the control-plane boundary:

- a bridge MUST NOT open its own independent Loopgate session as a parallel
  authority
- a bridge should receive delegated transport credentials from the operator client over a
  launch-bound local channel
- delegated bridge clients should use the existing Loopgate delegated-session
  client path rather than `/v1/session/open`
- delegated credential updates should use the typed contract defined in
  [RFC 0002](../rfcs/0002-delegated-session-refresh.md)
- if delegated credentials are unavailable or invalid, the bridge should fail
  closed and exit rather than starting in degraded mode

## 6) Browser auth bootstrap

Browser auth bootstrap must be launch-bound and local-only.

Acceptable v1 pattern:

- one-time launch token generated at startup
- token consumed exactly once
- token exchanged for an HttpOnly session cookie
- token removed from the URL after exchange

Known limitation:

- same-user local processes may still race for the launch token if they can
  observe stdout

That limitation must be documented explicitly until a stronger launch-bound
identity channel exists.

## 7) Request handling rules

Privileged UI POST endpoints must:

- use strict JSON decoding
- reject unknown fields
- reject missing required fields
- return explicit denial/error codes

UI endpoints must never accept raw secret material as normal request input.
UI approval endpoints must not expose or accept approval decision nonces from a
browser-facing caller.

## 8) Asset and CSP rules

UI assets should be local, embedded, and offline-capable.

Allowed:

- embedded static HTML/CSS/JS
- CSP nonces for inline content when necessary
- separate local asset files with `script-src 'self'` and `style-src 'self'`

Not allowed:

- CDN-loaded scripts
- remote analytics
- frameworks that require a networked build/runtime path

## 9) Observability rules

UI-friendly event feeds and status APIs are separate from audit storage.

The UI may show:

- display-safe event streams
- approval states
- redacted denials
- connection and health summaries

The UI must not expose:

- raw audit chain internals as normal feed items
- secret-bearing telemetry
- raw quarantined payloads through default views

If a bridge server is implemented, it should write a separate append-only event
stream for bridge-local auth, proxy, and browser-session events. That log must
remain separate from both the operator client's user ledger and Loopgate's control-plane
telemetry.

## 10) Design principle

The UI is allowed to be expressive.

It is not allowed to be clever about trust.

When there is tension between convenience and boundary clarity, preserve the
boundary.
