**Last updated:** 2026-03-24

# RFC 0002: Delegated Session Refresh and Pipe Contract

- Status: Draft
- Applies to: Morph -> bridge delegated Loopgate credentials

## 1. Purpose

This RFC defines the typed message format and refresh timing rules for
delegated Loopgate session credentials shared from Morph to a presentation
adapter such as `morphui`.

The goal is to make bridge bootstrap and refresh behavior deterministic,
fail-closed, and reviewable.

## 2. Scope

This RFC covers:

- delegated Loopgate credential envelopes
- transport over a launch-bound local channel such as an anonymous pipe
- refresh timing states
- invalid/expired delegated credential handling

This RFC does not cover:

- browser cookie/session handling
- bridge UI rendering behavior
- third-party OAuth provider tokens

## 3. Core rules

- A bridge MUST NOT open its own independent `/v1/session/open`.
- Morph MUST provide delegated Loopgate transport credentials over a
  launch-bound local channel.
- Delegated credentials MUST be treated as high-sensitivity material and
  MUST NOT be written to repo state, user-visible audit, or browser-visible
  APIs.
- If delegated credentials are missing, malformed, or expired, the bridge
  MUST fail closed.

## 4. Delegated credential message

The current schema version is:

- `loopgate.delegated_session.v1`

The current message type is:

- `credentials`

Envelope shape:

```json
{
  "schema_version": "loopgate.delegated_session.v1",
  "message_type": "credentials",
  "sent_at_utc": "2026-03-08T11:30:00Z",
  "credentials": {
    "control_session_id": "abc123def456",
    "capability_token": "opaque-capability-token",
    "approval_token": "opaque-approval-token",
    "session_mac_key": "opaque-session-mac-key",
    "expires_at_utc": "2026-03-08T12:00:00Z"
  }
}
```

Rules:

- decoders MUST reject unknown fields
- `control_session_id` MUST be a safe inert identifier
- token and MAC fields MUST be non-empty
- `expires_at_utc` MUST parse as RFC3339Nano
- expired credential sets MUST be rejected

## 5. Refresh timing policy

The current lead time is:

- `2 minutes`

Credential health states:

- `healthy`
- `refresh_soon`
- `refresh_required`

Rules:

- `healthy`: expiry is more than 2 minutes away
- `refresh_soon`: expiry is within 2 minutes; Morph should push a refreshed
  credential set before the current one expires
- `refresh_required`: expiry is at or before now; delegated clients MUST deny
  privileged requests and MUST NOT fall back to `/v1/session/open`

## 6. Client behavior

Delegated Loopgate clients:

- MAY be constructed from a delegated credential set
- MAY be updated in-process when Morph sends a fresh credential set
- MUST continue using the normal signed-request path with fresh request nonces
- MUST fail closed if the delegated credential set expires before a refresh
  arrives
- MUST NOT silently mint a new Loopgate session

## 7. Remaining open question

This RFC intentionally leaves one operational detail open:

- what user-visible bridge behavior should occur during the short interval
  between a `refresh_soon` state and a successful refreshed credential swap

That policy belongs in bridge implementation docs, but the control-plane
client contract here is already fixed:

- refresh is expected before expiry
- expiry without refresh is a hard denial
- no independent session-open fallback is allowed
