---
status: active
owner_area: loopgate-audit-runtime
tags:
  - audit
  - refactor
  - authority-boundary
  - hmac-checkpoint
related_code:
  - ./server.go
  - ./server_audit_runtime.go
  - ../auditruntime/runtime.go
  - ./audit_export_batch.go
  - ./audit_export_state.go
  - ./audit_export_sender.go
  - ../audit/ledger.go
  - ../ledger/ledger.go
  - ../ledger/hmac_checkpoint.go
related_docs:
  - ../../docs/setup/LEDGER_AND_AUDIT_INTEGRITY.md
  - ../../docs/design_overview/loopgate_locking.md
  - ../../docs/roadmap/refactor_and_agent_first_docs_plan.md
---

# Loopgate audit runtime extraction map

**Last updated:** 2026-05-03

## Purpose

This map defines the safe boundary for extracting Loopgate-specific audit
runtime code out of the main `internal/loopgate` god-package.

Do not move code until the dependency direction and invariants below are
preserved in tests.

## Current ownership

Three packages participate in audit, but they are different layers:

- `internal/ledger`
  - append-only JSONL storage
  - hash chaining
  - segmented ledger rotation
  - HMAC checkpoint primitives and verification

- `internal/audit`
  - `must_persist` vs `warn_only` append policy
  - generic wrapper over ledger append

- `internal/loopgate`
  - authoritative audit sequence state for Loopgate control-plane events
  - tenant/user enrichment
  - audit HMAC checkpoint creation policy
  - diagnostic log projection after successful append
  - audit export request/flush handlers and local export cursor state

The extraction target is only the third layer.

As of 2026-05-05, `internal/auditruntime` owns the audit sequence,
last-hash state, checkpoint counter, append serialization lock, startup load,
and HMAC checkpoint creation. `Server.logEvent` and `Server.logEventWithHash`
remain compatibility facades so the rest of `internal/loopgate` does not churn.

This is now a sibling runtime package. `internal/loopgate` imports it as an
adapter/wiring layer. Do not add more runtime subpackages under
`internal/loopgate/` by default.

## Current state fields

Fields currently on `Server` that belong to the audit runtime boundary:

- `auditPath`
- `appendAuditEvent`
- `auditLedgerRuntime`
- `auditRuntime`

Fields adjacent but not the first extraction target:

- `auditExportStatePath`
- `auditExportMu`
- `runtimeConfig.Logging.AuditExport`
- audit export sender/client helpers

Keep audit export in `internal/loopgate` for the first extraction unless the
audit runtime interface already exposes the needed verified ledger read method.

## Current functions

Moved into `internal/auditruntime`:

- authoritative startup chain load
- authoritative event recording
- HMAC checkpoint append policy
- integrity mode message formatting
- runtime state snapshot

Candidate future-move functions:

- `auditLedgerRotationSettings`
- `loadAuditLedgerCheckpointSecret`
- `hashAuditEvent`

Candidate stay-behind adapters on `Server`:

- `tenantUserForControlSession`
- `diagnosticTextAfterAuditEvent`
- `ensureDefaultAuditLedgerCheckpointSecret`
- `DiagnosticLogDirectoryMessage`
- `copyInterfaceMap`

Reason: those still reach broader server state, secret-store selection,
diagnostic managers, or operator startup messaging. Move them only after the
first boundary is stable.

## Proposed boundary

Create or migrate toward a small sibling package:

```text
internal/auditruntime
```

The package should own:

- an `Runtime` type
- sequence/last-hash/checkpoint counters
- append serialization lock
- checkpoint creation
- startup load and verification

The package should receive dependencies explicitly:

- clock function
- append function
- ledger path
- rotation settings
- HMAC checkpoint config
- checkpoint secret loader
- tenancy lookup callback
- successful-append observer callback for diagnostics

The package must not import `internal/loopgate`.

## Sketch

```go
type Runtime struct {
    mu sync.Mutex
    sequence uint64
    lastHash string
    eventsSinceCheckpoint int
    anchorPath string
    append func(path string, event ledger.Event) error
    loadCheckpointSecret func(context.Context) ([]byte, error)
    tenancyForSession func(sessionID string) (tenantID, userID string)
    afterAppend func(ledger.Event)
}

func (r *Runtime) Load(ctx context.Context, path string, settings ledger.RotationSettings, checkpoint config.AuditLedgerHMACCheckpoint) error
func (r *Runtime) Record(eventType, sessionID string, data map[string]interface{}) (eventHash string, err error)
func (r *Runtime) IntegrityModeMessage(checkpoint config.AuditLedgerHMACCheckpoint) string
```

Names can change during implementation; the dependency shape should not.

## Invariants to preserve

- Audit append failure for security-relevant actions remains a hard failure.
- Audit sequence and previous hash assignment remain one logical commit.
- No code acquires `Server.mu` while holding the audit runtime lock.
- HMAC checkpoint events participate in the same chain and update the same
  sequence/last-hash state.
- When HMAC checkpoints are enabled, the runtime writes a signed local head
  anchor after successful appends and compares it on startup before trusting the
  loaded head.
- The anchor fast path is limited to the active-file case where the OS file
  state still matches the signed anchor. Rotated ledgers fall back to the full
  startup verification path until a separate sealed-segment state digest exists.
- Checkpoint secrets are zeroed after use by the caller that loads them.
- Tenancy is resolved before entering audit append serialization.
- Diagnostic text logs remain derived from successful authoritative audit
  append, not a replacement for it.
- Tests can still inject `appendAuditEvent` failures for denial-path coverage.

## Tests that must stay green

Run at minimum:

```bash
go test ./internal/audit ./internal/ledger ./internal/loopgate
```

Specific high-signal tests:

- `TestLogEvent_AppendsConfiguredAuditHMACCheckpoint`
- `TestNewServerFailsClosedOnReplacedAuditLedgerWithAnchor`
- `TestNewServer_RestoresAuditCheckpointCadenceFromLedger`
- `TestAuditIntegrityModeMessage_*`
- audit-unavailable denial tests in `server_request_auth_runtime_test.go`
- audit export request/failure tests in `server_audit_export_handlers_test.go`
- startup chain integrity tests in `server_audit_chain_startup_test.go`

Also run:

```bash
make bench
```

The refactor should not materially change the benchmark shape. Audit fsync
should remain visible as the dominant cost for audited paths.

## First implementation slice

1. Create a narrow audit runtime package. The first slice used
   `internal/loopgate/auditruntime`; the low-churn sibling migration to
   `internal/auditruntime` landed on 2026-05-05.
2. Move only pure audit sequencing/checkpoint code.
3. Keep `Server.logEvent` and `Server.logEventWithHash` as delegating methods
   for compatibility with current call sites.
4. Keep audit export code unchanged.
5. Update this map with the final API after tests pass.

## Non-goals

- no audit batching that weakens durability
- no async audit append for must-persist events
- no export-daemon introduction
- no remote admin-node design in this extraction
- no conversion of `internal/audit` or `internal/ledger` into Loopgate-specific
  packages
