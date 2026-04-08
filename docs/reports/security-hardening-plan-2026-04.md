# Security hardening plan (April 2026)

**Status:** session MAC rotation (12h epochs), secret-export classification tightening, and control-plane route checklist landed; ledger **append** wiring for optional audit HMAC checkpoints remains follow-up. **Deferred (explicit):** Linux/Windows secure stores (no test matrix here), multi-instance / HA.

**Scope note:** Near-term product assumptions are **macOS-only** and **single-instance** where not otherwise stated (see `docs/adr/0009-macos-scope-and-approval-hardening.md`).

## Completed in this iteration

1. **Pending approval snapshot (`CapabilityRequest`)** — Deep-copy `Arguments` when storing `pendingApproval` and when emitting UI pending events so a shared map cannot mutate the approved payload after creation.
2. **Execution body integrity** — Before executing a non–morphling-spawn approval, verify `capabilityRequestBody256(pending.Request)` matches `ExecutionBodySHA256` from approval creation; fail closed with `DenialCodeApprovalExecutionBodyMismatch` and audit `approval.denied`.
3. **Secret export classification** — Optional `internal/tools` interfaces (`SecretExportNameHeuristicOptOut`, `RawSecretExportProhibited`). **Registered** tools are judged **only** via those interfaces (no name fallback). **Unregistered** names still use the legacy name heuristic in `internal/loopgate/secret_export.go` before the unknown-capability path. **Configured HTTP capabilities** (`configuredCapabilityTool`) implement `RawSecretExportProhibited` using the same heuristic on the configured name so YAML-defined integrations cannot bypass blocking by being “registered.”
4. **Vulnerability scanning** — `scripts/govulncheck.sh` runs `govulncheck` over `./...` for local release hygiene; `.github/workflows/govulncheck.yml` on `main` and PRs.

## Completed (2026-04-07 continuation)

1. **Resource caps (F6)** — Default limits: **64** pending approvals per control session, **65536** entries each for `seenRequests` and `seenAuthNonce` replay maps. Fail closed when full (no eviction). Denial codes: `pending_approval_limit_reached`, `replay_state_saturated` (HTTP **429** where applicable).
2. **Morphling spawn + non-pending response** — If spawn approval returns anything other than `pending_approval` (e.g. pending limit), `failMorphlingAfterAdmission` runs so morphlings do not stick in an authorizing state.
3. **Haven trusted-sandbox auto-allow policy** — `core/policy/policy.yaml` → `safety.haven_trusted_sandbox_auto_allow` (optional `*bool`, default-on when omitted) and `safety.haven_trusted_sandbox_auto_allow_capabilities` (optional allowlist; empty list disables auto-allow for all). Wired in `Server.shouldAutoAllowTrustedSandboxCapability`.

## Completed (2026-04-07 — executable pinning)

1. **Executable path pinning (F2)** — `config/runtime.yaml` → `control_plane.expected_session_client_executable`: non-empty absolute path required when set; compared (after clean) to the peer executable at `POST /v1/session/open`. Empty = disabled (default).

## Completed (2026-04-08) — session MAC rotation

1. **12-hour UTC epoch keys** — Server derives session MAC material per epoch; verifies requests with **previous / current / next** epoch overlap (`internal/loopgate/session_mac_rotation.go` and tests). `verifySignedRequest` uses rotating verification when the session MAC rotation master is loaded (same overlap as morphling worker signed bodies).
2. **`GET /v1/session/mac-keys`** — Same auth and signed GET envelope as `GET /v1/status`; returns epoch slots and derived keys for client refresh (`handleSessionMACKeys`).
3. **Go client** — `SessionMACKeys`, `RefreshSessionMACKeyFromServer` (`internal/loopgate/client.go`); tests in `client_session_mac_test.go`.

## Completed (2026-04-08) — secret-export tightening + route checklist

1. **Registered vs unregistered** — `capabilityProhibitsRawSecretExport` applies the name heuristic only when the capability is **not** in the registry; registered tools require explicit interface classification (`pending_approval_integrity.go`, `secret_export.go`).
2. **Default registry guardrail** — `default_registry_secret_export_test.go` asserts any default tool whose **name** matches the heuristic implements one of the two optional interfaces.
3. **Control-plane route checklist** — [control-plane-route-checklist.md](control-plane-route-checklist.md) for review/PR hygiene (auth × MAC × audit expectations); optional checkbox in `.github/pull_request_template.md`.

## Completed (2026-04-08) — ledger authenticity (documentation)

1. **Operator semantics** — [docs/setup/LEDGER_AND_AUDIT_INTEGRITY.md](../setup/LEDGER_AND_AUDIT_INTEGRITY.md): what SHA-256 chaining proves, same-user filesystem rewrite limitation, macOS single-instance scope, pointers to TM-05 and code. Threat model and maps cross-linked.

## Completed (2026-04-07) — audit ledger HMAC checkpoints (implementation)

1. **Keyed checkpoints** — Optional `logging.audit_ledger.hmac_checkpoint` in `config/runtime.yaml` (config + `internal/ledger/hmac_checkpoint.go` verification helpers). **Server append path** to the audit JSONL is tracked for a follow-up wiring pass when re-integrated with `Server.logEvent`.

## Follow-on (tracked, not done)

### Ledger authenticity (remaining backlog)

SHA-256 chaining plus optional **HMAC checkpoints** still assume **local** control of the JSONL and signing key. A same-user attacker with both can forge a new chain and new checkpoints if they possess the key.

**Remaining options:** periodic **remote append-only** export; **asymmetric signing** or **off-box verification** workflows; enterprise **admin-node** aggregation; documented **rotation** for the checkpoint key (operators must overlap verify windows when rotating).

### Other

- Optional **CI** that diffs `mux.HandleFunc` registrations against the route checklist (not implemented; checklist is manual + PR template).

**Explicitly out of scope here:** Linux/Windows secure credential backends (untested in current environment); multi-instance ledger + nonce semantics.

## Tests

- `internal/ledger/ledger_test.go` — append and chain verification (integrity anomalies fail closed).
- `internal/ledger/hmac_checkpoint_test.go` — checkpoint message and `VerifyAuditLedgerHMACCheckpointEvent`.
- `internal/loopgate/session_mac_rotation_test.go` — epoch index and MAC derivation helpers.
- `internal/loopgate/client_session_mac_test.go` — client mac-keys refresh behavior.
- `internal/loopgate/default_registry_secret_export_test.go` — default registry vs secret-export name heuristic.
- `internal/config/config_test.go` — HMAC checkpoint runtime validation (enabled without `secret_ref`, bad interval).
- `internal/loopgate/approval_manifest_test.go` — clone and execution-body verification.
- `internal/loopgate/server_test.go` — secret-export registry/heuristic; UI approval path; pending/replay caps.
- `internal/loopgate/session_executable_pin_test.go` — executable pin mismatch and match.
- `internal/config/config_test.go` — relative executable pin rejected at write/load validation.

## References

- ADR: `docs/adr/0009-macos-scope-and-approval-hardening.md`
- Prior review: `docs/reports/loopgate-security-architecture-review-2026-04-06.md`