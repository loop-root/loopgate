# Audit Runtime Map

`internal/auditruntime` owns Loopgate-specific audit sequencing and checkpoint
runtime state. It is a sibling runtime package used by `internal/loopgate`,
which remains the HTTP/control-plane adapter and authority wiring package.

## Boundary

This package owns:

- startup audit-chain load and active-file anchor verification
- append serialization for must-persist Loopgate audit events
- audit sequence and previous-hash assignment
- HMAC checkpoint cadence and checkpoint event creation
- persisted active-ledger anchor writes

This package must not import `internal/loopgate`.

## Callers

`internal/loopgate/server_audit_runtime.go` wires this package into `Server`
and keeps `Server.logEvent` / `Server.logEventWithHash` as compatibility
facades for current call sites.

## Invariants

- Audit append failure for security-relevant actions remains a hard failure.
- Audit sequence assignment and previous-hash assignment remain one serialized
  runtime operation.
- HMAC checkpoint events participate in the same ledger chain.
- Checkpoint secrets are zeroed by the caller that loads them.
- Diagnostic logs are derived from successful authoritative audit append, not a
  replacement for the audit ledger.
