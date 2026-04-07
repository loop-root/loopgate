# Security hardening plan (April 2026)

**Status:** executable path pinning and Haven policy hooks documented; ledger authenticity follow-up tracked below. **Deferred (explicit):** Linux/Windows secure stores (no test matrix here), multi-instance / HA.

**Scope note:** Near-term product assumptions are **macOS-only** and **single-instance** where not otherwise stated (see `docs/adr/0009-macos-scope-and-approval-hardening.md`).

## Completed in this iteration

1. **Pending approval snapshot (`CapabilityRequest`)** — Deep-copy `Arguments` when storing `pendingApproval` and when emitting UI pending events so a shared map cannot mutate the approved payload after creation.
2. **Execution body integrity** — Before executing a non–morphling-spawn approval, verify `capabilityRequestBody256(pending.Request)` matches `ExecutionBodySHA256` from approval creation; fail closed with `DenialCodeApprovalExecutionBodyMismatch` and audit `approval.denied`.
3. **Registry-first secret export classification** — Optional `internal/tools` interfaces (`SecretExportNameHeuristicOptOut`, `RawSecretExportProhibited`) with legacy name heuristic when interfaces are absent. Unregistered capability names that match the heuristic remain denied before the unknown-capability path.
4. **Vulnerability scanning** — `scripts/govulncheck.sh` runs `govulncheck` over `./...` for local release hygiene; `.github/workflows/govulncheck.yml` on `main` and PRs.

## Completed (2026-04-07 continuation)

5. **Resource caps (F6)** — Default limits: **64** pending approvals per control session, **65536** entries each for `seenRequests` and `seenAuthNonce` replay maps. Fail closed when full (no eviction). Denial codes: `pending_approval_limit_reached`, `replay_state_saturated` (HTTP **429** where applicable).
6. **Morphling spawn + non-pending response** — If spawn approval returns anything other than `pending_approval` (e.g. pending limit), `failMorphlingAfterAdmission` runs so morphlings do not stick in an authorizing state.
7. **Haven trusted-sandbox auto-allow policy** — `core/policy/policy.yaml` → `safety.haven_trusted_sandbox_auto_allow` (optional `*bool`, default-on when omitted) and `safety.haven_trusted_sandbox_auto_allow_capabilities` (optional allowlist; empty list disables auto-allow for all). Wired in `Server.shouldAutoAllowTrustedSandboxCapability`.

## Completed (2026-04-07 — executable pinning)

8. **Executable path pinning (F2)** — `config/runtime.yaml` → `control_plane.expected_session_client_executable`: non-empty absolute path required when set; compared (after clean) to the peer executable at `POST /v1/session/open`. Empty = disabled (default).

## Follow-on (tracked, not done)

### Ledger authenticity (hash chain vs signing)

Append-only ledgers use **SHA-256 hash chaining** (`event_hash`, `previous_event_hash`) for **tamper-evidence** and **ordering**, not a **secret-keyed MAC or signature**. An attacker with **filesystem write** to ledger paths can replace a file with a **new internally consistent chain** from genesis; verification does not prove Loopgate authorship.

**Follow-up options:** document the threat model in operator docs; periodic **remote append-only** export; **HMAC or asymmetric signing** of events or checkpoints with a key in **secure storage** (not colocated with a mutable ledger file); enterprise **admin-node** aggregation.

### Other

- Further **secret-export** tightening (explicit per-capability classification vs heuristic for unregistered names).
- **MAC key rotation** for long-lived sessions.
- **Route × auth × MAC × audit** checklist in CI or review template.

**Explicitly out of scope here:** Linux/Windows secure credential backends (untested in current environment); multi-instance ledger + nonce semantics.

## Tests

- `internal/loopgate/approval_manifest_test.go` — clone and execution-body verification.
- `internal/loopgate/server_test.go` — secret-export registry/heuristic; UI approval path; pending/replay caps.
- `internal/loopgate/session_executable_pin_test.go` — executable pin mismatch and match.
- `internal/config/config_test.go` — relative executable pin rejected at write/load validation.

## References

- ADR: `docs/adr/0009-macos-scope-and-approval-hardening.md`
- Prior review: `docs/reports/loopgate-security-architecture-review-2026-04-06.md`
