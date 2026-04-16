**Last updated:** 2026-04-16

# Loopgate locking model

This document explains why Loopgate uses multiple lock domains, what each lock
protects, and how request paths are expected to sequence them.

The short version:

- the lock count is deliberate
- the debugging hazard is not "many locks" by itself
- the real hazard would be undocumented nested locking or unclear ownership

Loopgate is intentionally request-driven and fail-closed. The locking model
follows that design:

- keep authoritative control-plane state under one primary lock
- keep unrelated subsystems out of that critical section
- treat most non-primary locks as leaf-domain locks
- prefer snapshot-under-lock, then release, then do I/O / audit / UI work

## 1. Primary rule

The `Server` lock model is anchored on one primary rule:

> Hold exactly one `Server` lock domain at a time whenever possible.

Today, production code mostly follows that rule. Request paths typically:

1. lock `mu` for authoritative control-plane state
2. snapshot or mutate the needed state
3. unlock `mu`
4. separately enter audit, UI, connection, or provider-runtime code

This is why the system can be reasoned about even though `Server` has many
mutexes.

## 2. Lock domains

### `mu`

Purpose:
- primary authoritative control-plane state lock

Protects:
- `sessions`
- `tokens`
- `approvals`
- `seenRequests`
- `seenAuthNonces`
- `usedTokens`
- `sessionOpenByUID`
- `approvalTokenIndex`
- `sessionReadCounts`
- expiry scheduling fields
- capacity counters/caps coupled to those maps
- `sessionMACRotationMaster`

Why separate:
- these tables participate in auth, replay prevention, approval lifecycle, and
  session lifecycle invariants
- they must be reasoned about as one state machine

Common rule:
- if a mutation changes what the system is authoritative about for a request,
  it probably belongs under `mu`

### `auditMu`

Purpose:
- serialize append-only audit sequencing and persistence

Protects:
- `auditSequence`
- `lastAuditHash`
- `auditEventsSinceCheckpoint`

Why separate:
- audit writes must behave like one logical commit
- sequence numbers and previous-hash assignment must not race
- this is intentionally isolated from normal request/session state

Critical invariant:
- never acquire `mu` while holding `auditMu`

Implementation note:
- `logEvent*` resolves tenancy/session-derived metadata before entering
  `auditMu`, specifically to avoid `auditMu -> mu`

### `auditExportMu`

Purpose:
- protect export progress state for audit shipping/batching

Why separate:
- export state is derived from the immutable ledger, not part of authoritative
  request handling
- export retries should not contend with `auditMu` or `mu`

### `promotionMu`

Purpose:
- serialize promotion/quarantine artifact moves and related file workflows

Why separate:
- promotion side effects are filesystem-centric and should not enlarge the
  control-plane critical section

### `uiMu`

Purpose:
- protect derived UI/event-stream projection state

Protects:
- `uiSequence`
- `uiEvents`
- `uiSubscribers`
- `nextUISubscriberID`

Why separate:
- UI event feeds are projections, not authority
- subscriber churn should not contend with auth/session maps

Rule:
- compute authoritative data first, then emit projection updates under `uiMu`

### `claudeHookSessionsMu`

Purpose:
- protect Claude hook harness session caches

Why separate:
- hook-session bookkeeping is harness metadata, not core control-plane
  authority

### `connectionsMu`

Purpose:
- protect persisted integration connection records

Protects:
- `connections`

Why separate:
- provider/subject credential metadata evolves independently from control
  sessions and approvals

### `modelConnectionsMu`

Purpose:
- protect model-provider connection records

Protects:
- `modelConnections`

Why separate:
- model endpoint credentials and generic integration credentials are distinct
  subsystems with separate lifecycles

### `hostAccessPlansMu`

Purpose:
- protect temporary host-access planning state

Protects:
- `hostAccessPlans`
- `hostAccessAppliedPlanAt`

Why separate:
- host plan drafting/application recovery has its own TTL and duplicate-apply
  semantics
- those semantics should not enlarge `mu`

### `providerTokenMu`

Purpose:
- protect live provider-token and configured connection/capability runtime data

Protects:
- `providerTokens`
- `configuredConnections`
- `configuredCapabilities`

Why separate:
- operator config reloads and provider token churn are not the same domain as
  sessions/approvals/replay protection

Rule:
- snapshot the runtime data under `providerTokenMu`, then release it before
  network calls or audit emission

### `policyRuntimeMu`

Purpose:
- protect the current `serverPolicyRuntime` snapshot

Protects:
- `policyRuntime`
- mirrored legacy compatibility fields updated with it:
  - `policy`
  - `policyContentSHA256`
  - `checker`
  - `registry`
  - `mcpGatewayManifests`
  - `httpClient`

Why separate:
- request paths need cheap concurrent reads
- policy/config reloads need coherent snapshot replacement

Why `RWMutex`:
- reads are far more common than writes

### `pkceMu`

Purpose:
- protect short-lived PKCE/OAuth browser handoff sessions

Protects:
- `pkceSessions`

Why separate:
- PKCE state is independent, ephemeral, and pruned on its own cadence

## 3. Non-Server locks in this package

### `Client.mu`

Purpose:
- protect cached client-side session/delegation credentials and approval
  decision nonce state

Why it exists:
- one local client can issue requests while refreshing/rotating session
  material
- this is transport convenience state only; the server remains authoritative

### `mcpGatewayLaunchedServer.ioMu`

Purpose:
- serialize stdio access to one launched MCP server process

Why it exists:
- stdio is one ordered byte stream; concurrent framing would corrupt it

Rule:
- never hold `ioMu` while taking `Server` locks

## 4. Sequencing rules that matter in practice

### Rule A: `auditMu` is a leaf lock

Do:
- gather session/control metadata first
- then call `logEvent*`

Do not:
- enter `auditMu` and then look up control-session state under `mu`

### Rule B: prefer snapshot -> unlock -> side effect

Common shape:

1. take `mu` or another domain lock
2. prune/check/mutate authoritative state
3. copy the data you need
4. unlock
5. perform audit, UI, file, or network side effects

This keeps lock scope boring and explainable.

### Rule C: do not invent nested lock orders casually

If future code must hold more than one `Server` lock at once:

- document the exact order in code and in this document
- explain why snapshot-and-release was insufficient
- add regression coverage if the new path is subtle

## 5. What this model is trying to prevent

- deadlocks from undocumented nested lock order
- audit sequencing races
- UI subscriber churn blocking request auth
- provider-config churn blocking session lifecycle
- OAuth/PKCE bookkeeping inflating the main control-plane critical section
- host plan bookkeeping inflating the approval/session critical section

## 6. What this model does **not** solve by itself

Multiple locks do not magically make the package easy to work in.

The remaining maintainability problems are:

- `Server` still aggregates too many state domains in one struct
- `internal/loopgate/` is still a god-package
- `executeCapabilityRequest` still carries too much decision logic in one path

So the right interpretation is:

- the current locks are defensible
- the state ownership still needs cleanup
- package extraction should follow clear domain boundaries rather than trying to
  "fix" things by collapsing locks into one bigger lock

## 7. Debugging checklist

When a future change touches one of these domains, ask:

1. Which fields are authoritative here?
2. Which single lock should own them?
3. Can I snapshot under that lock and release before doing I/O?
4. Am I accidentally introducing nested locking?
5. Am I moving derived/UI/export state into an authoritative lock for
   convenience?

If the answer to `4` is "yes", stop and document the order before merging.
