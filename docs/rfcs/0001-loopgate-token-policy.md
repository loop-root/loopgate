**Last updated:** 2026-03-24

# RFC 0001: Loopgate Token and Request Integrity Policy

- Status: Draft
- Applies to: local **operator client** ↔ Loopgate control plane (HTTP on UDS in v1)

**AMP alignment:** Neutral transport and integrity rules for the same class of deployment are specified in the vendored [AMP RFC 0001](../AMP/AMP-RFCs/0001-local-transport-profile.md) (`local-uds-v1`) and [AMP RFC 0004](../AMP/AMP-RFCs/0004-canonical-envelope-and-integrity-binding.md) (canonical envelope and MAC). **On conflict, AMP RFC 0004 is authoritative** for canonical request serialization and MAC; this document describes Loopgate’s current HTTP routes, tokens, and operator-facing denial codes.

## 1. Purpose

This document defines the normative token and request-integrity rules for the local **operator client** / Loopgate control-plane boundary.

The goal is to make the token model reviewable and deterministic rather than inferred from code or model-generated implementation choices.

## 2. Scope

This RFC covers:

- control session identifiers
- capability tokens
- approval tokens
- UI observation for display-safe `/v1/ui/*` endpoints
- approval decision nonces
- signed request envelopes
- replay detection rules
- token scope and expiry rules
- prohibited behaviors

This RFC does not yet cover:

- third-party OAuth provider tokens
- remote deployment transport
- Loopgate secure-store internals

## 3. Terminology

- `Operator client`: the unprivileged local client/runtime (IDE agent, CLI, native UI, or test harness)
- `Loopgate`: the local privileged control plane
- `control session`: a Loopgate-issued server-side session binding for one operator client instance
- `capability token`: an opaque Loopgate-issued token authorizing scoped capability requests
- `approval token`: an opaque Loopgate-issued token authorizing approval decisions only
- `decision nonce`: a single-use Loopgate-issued proof bound to one pending approval request
- `request nonce`: a per-request single-use value used in the signed request envelope

## 4. Security goals

The token system MUST:

- keep provider credentials and secret material inside Loopgate
- prevent the operator client from exchanging Loopgate tokens for provider credentials
- keep capability authorization scoped and short-lived
- distinguish execution authorization from approval authorization
- make replay attempts detectable and deniable
- make request tampering detectable
- remain local-first and deny by default

## 5. Token classes

### 5.1 Control session identifier

- Issued only by Loopgate at session open.
- Opaque server-generated identifier.
- Not sufficient by itself to authorize privileged actions.
- MUST be presented on signed privileged requests.

### 5.2 Session MAC key

- Issued only by Loopgate at session open.
- Shared only with the operator client bound to that control session.
- Used to sign privileged requests with HMAC-SHA256.
- MUST NOT be logged, persisted in repo state, or surfaced in user-visible audit output.
- Expires with the control session.

### 5.3 Capability token

- Issued only by Loopgate.
- Opaque, random, server-side token.
- NOT a JWT.
- NOT self-describing.
- MUST be bound server-side to:
  - control session ID
  - peer identity
  - capability scope set
  - expiry time
- MUST NOT be convertible into provider credentials or secret material.
- MUST NOT authorize approval decisions.

For high-risk execution, Loopgate MAY derive an internal execution token from a capability token.
That derived execution token:

- is single-use
- is bound to one capability
- is bound to the normalized argument set for that execution
- is not exposed to the operator client

### 5.4 Approval token

- Issued only by Loopgate.
- Opaque, random, server-side token.
- MUST be distinct from the capability token.
- MUST be bound server-side to:
  - control session ID
  - peer identity
  - expiry time
- MUST authorize approval decisions.
- In the current implementation, it also authorizes read-only pending-approval
  listing via `GET /v1/ui/approvals`.
- MUST NOT authorize capability execution by itself.

### 5.6 UI observation

Display-safe UI observation is a control-plane function.

In the current implementation:

- `GET /v1/ui/status` and `GET /v1/ui/events` use the capability token plus the
  signed request envelope
- `GET /v1/ui/approvals` uses the approval token plus the signed request envelope
- `POST /v1/ui/approvals/{id}/decision` uses the approval token plus the signed
  request envelope

This is an implementation detail of the current local UI/API split, not a
statement that approval tokens may generally authorize unrelated actions.

Future revisions MAY define a dedicated `ui_observe` scope or token class to
separate UI observation from execution and approval authority more cleanly.

### 5.5 Approval decision nonce

- Issued only by Loopgate when an approval request is created.
- Bound to exactly one pending approval request.
- Single-use.
- Cleared on approval resolution.
- MUST NOT be accepted after the approval leaves `pending`.

## 6. Transport and integrity

### 6.1 Transport

- v1 transport is local Unix domain socket HTTP only.
- Privileged Loopgate routes MUST NOT be exposed over remote network transport in v1.

### 6.2 Required signed request envelope

Privileged requests to Loopgate control-plane routes MUST include the signed
request envelope.

This currently includes:

- privileged `POST` routes
- `GET /v1/ui/status`
- `GET /v1/ui/events`
- `GET /v1/ui/approvals`

Signed requests MUST include:

- `Authorization: Bearer <capability token>` for capability execution, or
- `X-Loopgate-Approval-Token: <approval token>` for approval decision
- `X-Loopgate-Control-Session`
- `X-Loopgate-Request-Timestamp`
- `X-Loopgate-Request-Nonce`
- `X-Loopgate-Request-Signature`

The signature MUST cover:

- HTTP method
- request path
- control session ID
- request timestamp
- request nonce
- request body hash

The MAC algorithm for the current implementation is HMAC-SHA256.

### 6.3 Peer binding

Loopgate MUST bind control sessions and tokens to the authenticated Unix-socket peer identity.

Token possession alone MUST NOT be sufficient.

## 7. Replay and uniqueness

### 7.1 Request nonce

- Each signed privileged request MUST carry a fresh request nonce.
- Loopgate MUST reject replayed request nonces within the active control session.

### 7.2 Request ID

- Capability requests MUST carry a `request_id`.
- If the operator client omits it, Loopgate may assign one before execution.
- Loopgate MUST reject duplicate `request_id` values per control session for requests that enter execution/approval flow.

### 7.3 Approval decision nonce

- Approval decisions MUST include the current decision nonce.
- Loopgate MUST reject:
  - missing nonces
  - invalid nonces
  - reused nonces
  - nonces for non-pending approvals

For the current UI API:

- the browser/UI-facing approval list MUST NOT expose the decision nonce
- Loopgate resolves the nonce server-side for `POST /v1/ui/approvals/{id}/decision`
- bridge or UI code MUST NOT accept a browser-supplied nonce as normal input

### 7.4 Single-use execution token replay

- Single-use execution tokens MUST be denied on reuse.
- If a token is bound to a capability or normalized argument hash, Loopgate MUST deny execution when the request does not match the binding.

## 8. Scope rules

Capability tokens MUST have a non-empty requested capability set.

Loopgate MUST reject session-open requests that ask for an empty capability scope.

Capability execution MUST be denied when the requested capability is not inside the token scope.

Approval tokens MUST NOT widen capability scope.

For high-risk execution, derived execution tokens MUST narrow scope further, not widen it.

## 9. Expiry and lifecycle

- Control sessions, capability tokens, and approval tokens MUST be short-lived.
- Expired tokens MUST be denied explicitly.
- Approval requests MUST expire explicitly.
- Loopgate restart MAY revoke in-memory tokens in the current MVP.

## 10. Result and secret boundaries

Loopgate responses to the operator client MUST NOT include:

- provider credentials
- access tokens
- refresh tokens
- client secrets
- raw secure-store material

Loopgate MAY return:

- capability tokens
- approval tokens
- redacted metadata
- structured capability results

Raw HTTP or integration payload bodies MUST NOT be included in standard prompt-eligible responses by default.

## 11. Identifier policy

Identifiers used in the control plane MUST be inert labels, not executable or path-like strings.

This applies to:

- actor labels
- session labels
- provider identifiers
- connection subjects
- capability names
- connection scopes
- future skill IDs and manifest IDs

Traversal-like or shell-like identifiers MUST be denied, including examples such as:

- `../../etc`
- `..\\..\\windows`
- `$whoami`
- backtick command fragments

## 12. Denial behavior

Loopgate MUST fail closed and return explicit denial codes for token and integrity failures, including:

- missing token
- invalid token
- expired token
- scope denied
- missing signature
- invalid signature
- invalid timestamp
- replayed request nonce
- invalid control-session binding
- invalid approval token
- invalid approval decision nonce

## 13. Forbidden behaviors

The implementation MUST NOT:

- expose raw provider tokens or raw secrets to the operator client
- treat capability tokens as provider credentials
- allow approval tokens to execute capabilities directly
- accept unsigned privileged requests
- silently widen token scope
- silently fall back from Loopgate authority into unprivileged client-side authority
- feed raw transport payloads into prompt compilation by default

## 14. Current implementation notes

As of this RFC revision:

- tokens are opaque server-side random values, not signed JWTs
- transport integrity is provided by:
  - Unix domain socket locality
  - peer credential binding
  - HMAC-signed request envelopes
  - nonce replay detection
- audit integrity is hash-chained locally, but not yet externally anchored

## 15. Future work

- launch-bound operator client → Loopgate bootstrap identity
- stronger single-use JTI semantics for risky execution tokens
- persistent revocation and replay state beyond process lifetime
- remote deployment transport profile
- provider OAuth token policy inside Loopgate
