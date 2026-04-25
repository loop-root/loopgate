**Last updated:** 2026-04-24

# Admin Console TUI MVP

This document defines a conservative admin-console TUI for Loopgate.

The goal is to make local governance easier to operate without moving authority
out of Loopgate.

## Product goal

Loopgate should reduce approval fatigue without weakening the control boundary.

The console should help an operator answer:

- is Loopgate running and governing the real harness path?
- which signed policy profile is active?
- what is allowed, approval-required, or denied?
- what approvals need attention?
- what did the agent recently attempt?
- why was a request denied?
- is audit integrity healthy enough for local review?

The console is not a chat UI and not an authority source.

## User value

For individual developers, the console should make Loopgate feel understandable:

- fewer repeated prompts for safe-ish work
- quick visibility into why a request was blocked
- a visible distinction between allowed, approval-required, and denied work
- a clear setup posture for Claude Code hooks and the local daemon

For business operators, the console should be the first local admin surface:

- inspect signed policy posture
- review pending approvals
- inspect recent audit and denial history
- verify hook and daemon status
- prepare for later managed policy and access-control features

The current scope remains local-first and single-node. Multi-machine policy
distribution, centralized identity, and remote admin are future directions.

## Authority contract

The TUI must preserve these invariants:

- Loopgate remains the only authority for privileged actions.
- Signed policy remains the only policy authority.
- Approval creation, decision validation, and execution gating stay server-side.
- The TUI renders derived views; it does not maintain authoritative approval,
  audit, policy, or daemon state.
- The first implementation slice is read-only. Future mutations must route
  through existing Loopgate CLI or HTTP-on-UDS APIs.
- If Loopgate is unavailable, the TUI shows that truth instead of simulating
  state from local files.
- Raw secrets, tokens, request MAC keys, and secret-bearing payloads are never
  displayed.

## MVP screens

### 1. Overview

Shows:

- daemon state: running, unreachable, temporary proof only, or LaunchAgent-managed
- socket path
- active operator mode
- selected setup profile when known
- signed policy path, signature status, signer `key_id`
- Claude hook install status
- audit integrity mode and latest verification summary

Allowed actions:

- refresh status
- run smoke test
- open doctor summary

### 2. Policy

Shows:

- current profile: `balanced`, `strict`, `read-only`, or custom
- plain-language policy explanation from `loopgate-policy-admin explain`
- counts by decision class: allowed, approval-required, denied
- last policy apply result

Allowed actions:

- validate signed policy
- hot-apply already signed on-disk policy
- render starter profile preview

Not allowed in MVP:

- editing policy inline
- signing arbitrary policy from the TUI
- weakening policy without a separate explicit signing workflow

### 3. Approvals

Shows:

- pending approvals for the current local Loopgate instance
- approval id
- capability/tool
- request summary
- requested path or command summary when display-safe
- expiry
- decision state

Allowed actions:

- refresh pending approval list
- show related audit timeline when available

Future actions:

- approve with a required operator reason
- deny with a required operator reason

Rules:

- approval decisions, when added, must call the existing approval path
- stale, expired, resolved, or cross-session approvals must not be resurrected
- display summaries must be redacted and bounded

### 4. Activity

Shows:

- recent allowed, denied, approval-created, approval-granted, approval-denied,
  and execution-failed events
- request id or approval id
- denial code and short reason
- event hash when available

Allowed actions:

- filter by event type
- filter by request id or approval id
- open `loopgate-doctor explain-denial` style detail

Rules:

- the TUI must consume display-safe APIs or verified ledger-derived summaries
- it must not tail raw diagnostic logs as audit truth

### 5. Harness

Shows:

- Claude Code hook install state
- expected hook scripts
- hook commands and managed settings target
- latest hook validation event when available

Allowed actions:

- install hooks
- remove hooks
- show next manual check, such as running `/hooks` in Claude Code

Rules:

- hook install/removal must reuse existing Loopgate-managed commands
- failures must be visible and explainable

### 6. Audit And Doctor

Shows:

- ledger verify status
- HMAC checkpoint status
- audit ledger path
- recent verification errors or warnings
- nonce replay and audit-export diagnostic summaries when available

Allowed actions:

- run doctor report
- run ledger verify
- open a compact support bundle summary

Rules:

- diagnostic views stay separate from authoritative audit
- support bundles must not include secrets or raw secret-bearing payloads

## Non-goals for MVP

- browser UI
- remote admin server
- multi-tenant SaaS control plane
- live policy text editor
- automatic background policy sync
- autonomous cleanup daemon
- broad MCP server management beyond current status and request-driven controls
- continuity or memory administration inside this repo

## Implementation shape

Preferred first slice:

- add `loopgate console` as a subcommand of `cmd/loopgate`
- implement the TUI as a thin local operator client
- reuse existing CLI/client helpers where possible
- keep all authoritative reads and writes behind Loopgate APIs or existing
  verification commands

This keeps installation simple: a user who has `loopgate` installed also has the
console.

Alternative:

- add `cmd/loopgate-admin-console`

Use this only if dependency or binary-size concerns make the main binary too
heavy.

## Suggested package boundaries

If implemented in-tree:

- `cmd/loopgate/console.go`: subcommand wiring
- `internal/console/`: TUI model, view state, key handling, rendering
- `internal/console/adapter/`: wrappers around Loopgate client and local CLI
  probes

The console package should own UI state only. It should not import low-level
server internals to bypass the control plane.

## State model

Use a small derived state object:

```text
ConsoleSnapshot
  fetched_at_utc
  daemon_status
  policy_status
  hook_status
  approval_summary
  activity_summary
  audit_integrity_summary
  warnings
```

Rules:

- refresh is explicit or timer-driven only after design review
- future mutation actions invalidate the relevant snapshot and refetch from Loopgate
- stale snapshots are visibly marked stale
- errors are rendered as first-class state, not swallowed

## Security and recovery notes

- If status fetch fails, show `unreachable` and the exact safe next command.
- When approval submission is later added, failed submissions must leave the
  approval in unknown/stale state until refetched from Loopgate.
- If audit verification fails, show the failure and do not present recent events
  as trusted audit truth.
- If policy validation fails, block apply and surface the typed failure.
- Never cache capability tokens, approval tokens, session MAC keys, or raw
  request bodies in console state.

## Tests and verification

The first implementation should include:

- unit tests for redaction and display formatting
- tests that the first console slice does not expose mutation commands
- tests for unreachable Loopgate state
- tests that audit-derived activity is hidden when verification fails
- golden tests for compact status rendering
- manual smoke test with `loopgate setup`, `loopgate status`, `loopgate test`,
  and one approval-required Claude action

## First implementation slice

Build only the useful center first:

1. `loopgate console` opens to Overview.
2. It can refresh daemon, policy, hook, and audit-integrity status.
3. It summarizes signed operator grants without treating them as authority.
4. It lists pending approvals as display-safe summaries.
5. It shows recent allow, ask, and block decision counts from verified audit
   events.
6. It shows the last 20 recent audit-derived events.

That slice proves the console is an admin surface for the real governed path,
not a parallel product shell.
