**Last updated:** 2026-03-24

# Phase 1 ship blockers + aligned hardening report

**Date:** 2026-03-25  
**Plan:** `docs/superpowers/plans/2026-03-25-master-implementation-plan.md` (Phase 1 Tasks 1–5; Phase 2 Tasks 6–7 verification)

## Summary

Phase 1 **Ship Blockers** (continuity mutation ordering + inspect bounds, secret redaction for quoted/Basic values, `ModelConnectionStoreRequest` JSON marshal guard, sandbox API error redaction) is **marked complete** in the master plan with tests and implementation in tree.

In the same effort window, several items that the plan lists under **Post-Launch Tier 1** were also implemented because they were small, invariant-preserving, and already touching the same files:

- `ensureWithinRoot` now **fails closed** if `EvalSymlinks` cannot resolve root or target (`internal/sandbox/sandbox.go`).
- Deny-list resolution in `ResolveSafePath` / `ExplainSafePath` **fails closed** when a deny path cannot be cleaned (`internal/safety/safepath.go`).
- Base ledger append **syncs** the file before stat/hash update (`internal/ledger/ledger.go`).
- Continuity snapshot writes use an injected **clock** (`saveContinuityMemoryState` / `writeContinuityArtifacts` take `nowUTC`; `Server` wires `server.now().UTC()`).
- **Morphling summaries** omit raw model/worker strings in API projections; contract and shell tests assert `StatusText`, `MemoryStringCount`, and omitted `TerminationReason` instead.

Phase 2 **Memory That Works** (Tasks 6–7: TCL key normalization + anchor supersession) was **already satisfied** before this report; master-plan checkboxes are marked complete. See `docs/superpowers/reports/2026-03-25-phase-1-implementation-report.md` for TCL-centric detail.

## Verification

```bash
go test ./... -count=1
```

Targeted:

```bash
go test ./internal/loopgate/... -run 'Test(MutateContinuityMemory|ContinuityInspectRequest_|RedactSandboxError|ModelConnectionStoreRequest_MarshalJSON)' -count=1
go test ./internal/secrets/... -run 'TestRedactText_' -count=1
```

## Follow-ups (still open vs master plan)

- **Phase 3** (persona, first-run, .dmg, demo): manual / release work; not code-complete from this run.
- **Phase 4**: dogfood week + top issues.
- Post-launch items **not** done here: morphling worker race-focused tests, `invoke_capability` XML parity tests (if still desired), `isSecretExportCapability` registry hardening, optional future transport hardening (e.g. XPC — **TBD**, not a v1 blocker), etc. **v1 ships HTTP** on the local control-plane socket per master plan.
