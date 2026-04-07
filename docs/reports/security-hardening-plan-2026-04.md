# Security hardening plan (April 2026)

**Status:** in progress (this iteration implements pending-approval and secret-export items below).

**Scope note:** Near-term product assumptions are **macOS-only** and **single-instance**; multi-OS and multi-node hardening are explicitly deferred (see `docs/adr/0009-macos-scope-and-approval-hardening.md`).

## Completed in this iteration

1. **Pending approval snapshot (`CapabilityRequest`)** — Deep-copy `Arguments` when storing `pendingApproval` and when emitting UI pending events so a shared map cannot mutate the approved payload after creation.
2. **Execution body integrity** — Before executing a non–morphling-spawn approval, verify `capabilityRequestBody256(pending.Request)` matches `ExecutionBodySHA256` from approval creation; fail closed with `DenialCodeApprovalExecutionBodyMismatch` and audit `approval.denied`.
3. **Registry-first secret export classification** — Optional `internal/tools` interfaces (`SecretExportNameHeuristicOptOut`, `RawSecretExportProhibited`) with legacy name heuristic when interfaces are absent. Unregistered capability names that match the heuristic remain denied before the unknown-capability path.
4. **Vulnerability scanning** — `scripts/govulncheck.sh` runs `govulncheck` over `./...` for local release hygiene (no CI assumption).

## Follow-on (not done here)

- **Resource caps** on in-memory replay / approval maps under abuse.
- **Morphling spawn** approvals: optionally record `ExecutionBodySHA256` for the same integrity check as capability approvals (currently skipped when hash is empty).
- **CI** wiring for `govulncheck` when the repo adds a standard pipeline.
- **Multi-platform** builds and tests when the project expands beyond macOS.

## Tests

- `internal/loopgate/approval_manifest_test.go` — clone and execution-body verification.
- `internal/loopgate/server_test.go` — secret-export registry/heuristic behavior; UI approval path denies on stored-body tampering.

## References

- ADR: `docs/adr/0009-macos-scope-and-approval-hardening.md`
- Prior review: `docs/reports/loopgate-security-architecture-review-2026-04-06.md`
