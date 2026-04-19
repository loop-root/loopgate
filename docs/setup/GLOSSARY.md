**Last updated:** 2026-04-19

# Loopgate Glossary

This is the short operator glossary for the terms that show up repeatedly in
the setup and troubleshooting docs.

## Control plane

The local Loopgate server process that owns policy evaluation, approvals,
secrets, and the authoritative audit path.

For the current product, it listens on HTTP over a Unix socket at
`runtime/state/loopgate.sock` unless you override `LOOPGATE_SOCKET`.

## Control session

A server-issued local session binding between one client process and Loopgate.

It is opened through `POST /v1/session/open` and is tied to the connecting
local process identity, not just a token string.

## Capability token

A bearer token scoped to the capabilities granted for one control session.

It is required for capability execution and many privileged read/write routes,
but possession of the token alone is not enough; the server also checks the
control-session binding and signed-request envelope where required.

## Approval token

A token for the approval-specific routes and UI approval surfaces.

It is separate from the capability token so approval workflows can stay typed
and narrow.

## Signed request

A privileged HTTP request that includes:
- `X-Loopgate-Control-Session`
- `X-Loopgate-Request-Timestamp`
- `X-Loopgate-Request-Nonce`
- `X-Loopgate-Request-Signature`

The signature is an HMAC over the request method, path, control session,
timestamp, nonce, and body hash using the session MAC key returned at session
open.

## Governed MCP broker

Loopgate’s server-side MCP execution path.

It launches and mediates declared MCP servers through policy, approvals, and
audit instead of trusting the IDE or model to do that directly.

## HMAC checkpoint

A periodic keyed integrity checkpoint written alongside the append-only audit
ledger.

The normal event chain is hash-linked JSONL. The HMAC checkpoints add a keyed
integrity signal backed by a secret outside the JSONL, using the shipped local
macOS Keychain path by default.

## Audit ledger

The authoritative append-only local event history for Loopgate-managed actions.

This is the source of truth for security-relevant activity. Human-friendly
status views and summaries are derived from it, not the other way around.

## Policy profile

A starter signed-policy template intended to reduce first-time setup friction.

Current built-in profiles are:
- `strict`
- `balanced`
- `developer`

They are starting points, not authority shortcuts. The selected profile is
still written as a normal policy file and signed through the standard policy
workflow.
