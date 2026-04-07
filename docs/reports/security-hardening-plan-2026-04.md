# Security hardening plan (April 2026)

**Status:** resource caps and CI govulncheck added (2026-04-07); remaining items are multi-platform and deeper policy hooks.

**Scope note:** Near-term product assumptions are **macOS-only** and **single-instance**; multi-OS and multi-node hardening are explicitly deferred (see `docs/adr/0009-macos-scope-and-approval-hardening.md`).

## Completed in this iteration

1. **Pending approval snapshot (`CapabilityRequest`)** — Deep-copy `Arguments` when storing `pendingApproval` and when emitting UI pending events so a shared map cannot mutate the approved payload after creation.
2. **Execution body integrity** — Before executing a non–morphling-spawn approval, verify `capabilityRequestBody256(pending.Request)` matches `ExecutionBodySHA256` from approval creation; fail closed with `DenialCodeApprovalExecutionBodyMismatch` and audit `approval.denied`.
3. **Registry-first secret export classification** — Optional `internal/tools` interfaces (`SecretExportNameHeuristicOptOut`, `RawSecretExportProhibited`) with legacy name heuristic when interfaces are absent. Unregistered capability names that match the heuristic remain denied before the unknown-capability path.
4. **Vulnerability scanning** — `scripts/govulncheck.sh` runs `govulncheck` over `./...` for local release hygiene (no CI assumption).

## Completed (2026-04-07 continuation)

5. **Resource caps (F6)** — Default limits: **64** pending approvals per control session, **65536** entries each for `seenRequests` and `seenAuthNonce` replay maps. Fail closed when full (no eviction). Denial codes: `pending_approval_limit_reached`, `replay_state_saturated` (HTTP **429** where applicable).
6. **Morphling spawn + non-pending response** — If spawn approval returns anything other than `pending_approval` (e.g. pending limit), `failMorphlingAfterAdmission` runs so morphlings do not stick in an authorizing state.
7. **CI** — `.github/workflows/govulncheck.yml` runs `govulncheck` on push to `main` and on pull requests.

## Follow-on (not done here)

- **Executable path pinning** in production profiles.
- **Linux/Windows** secure credential backends.
- **Multi-platform** builds and tests when the project expands beyond macOS.
- **Multi-platform** builds and tests when the project expands beyond macOS.

## Tests

- `internal/loopgate/approval_manifest_test.go` — clone and execution-body verification.
- `internal/loopgate/server_test.go` — secret-export registry/heuristic behavior; UI approval path denies on stored-body tampering; `TestPendingApprovalLimitPerControlSession`, `TestRequestReplayStoreSaturates`.

## References

- ADR: `docs/adr/0009-macos-scope-and-approval-hardening.md`
- Prior review: `docs/reports/loopgate-security-architecture-review-2026-04-06.md`
