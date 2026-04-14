# Loopgate security and architecture review (code-grounded)

**Date:** 2026-04-06  
**Scope:** Current Go implementation under `internal/loopgate`, `internal/sandbox`, `internal/policy`, `internal/secrets`, `internal/ledger`, `cmd/loopgate`, and related security-sensitive paths. *(Review date: `internal/loopgate/mcpserve` still existed; **in-tree MCP since removed** — ADR 0010.)*  
**Method:** Static review of authoritative code paths (not legacy docs or removed surfaces unless verified in tree).  
**Out of scope:** Full dependency CVE audit, formal verification, penetration test.

This report consolidates: (1) security/architecture findings, (2) HTTP route authentication matrix, (3) corrected analysis of Unix peer binding vs “same-UID” trust, (4) prioritized recommendations, (5) impact of **AI-only authorship** on how to read these conclusions.

---

## 1. Executive summary

Loopgate is a **local Unix-socket HTTP control plane** that mediates tool execution with **peer credentials**, **session-scoped capability tokens**, **HMAC-signed request bodies**, **policy checks**, **approval flows with manifest binding**, **hash-chained audit ledgers**, and **sandbox-oriented path resolution**. At review time, an **in-tree MCP stdio bridge** forwarded to the same HTTP API; that package is **deprecated and removed** (ADR 0010 — shrinks attack surface; **reserved** for a possible future thin forwarder via new ADR).

**Verdict:** For a **single-user, local-first** deployment model, the design is **credible and materially stronger** than typical “agent + shell + skills” stacks, because enforcement is **in code** (signing, replay controls, audit fail-closed paths, registry + policy) rather than advisory.

**Primary residual risks:** **heuristic** secret-export blocking for unregistered capabilities, **Linux/Windows** secure-store stubs (fail-closed), **ledger hash chains without keyed authentication** (see hardening plan), and remaining **in-memory** monitoring. Executable path pinning and Haven auto-allow are **policy-configurable**; defaults remain permissive for pinning (off) and Haven auto-allow (on). (`GET /v1/diagnostic/report` requires signed GET headers; see F8.)

**Correction vs an earlier draft:** Token use is **not** “same UID only.” `authenticate` and `authenticateApproval` require **`PeerIdentity` equality including PID** (and EPID on Darwin), so **cross-process token theft across different PIDs fails** unless the attacker reuses the same process identity (same process / same connection semantics). Session **open** remains available to any process that can connect to the socket and pass validation, yielding **that process’s own** session—not the victim’s existing session.

---

## 2. Authorship note and revised assessment (AI-generated codebase)

Stakeholders report this repository is **entirely AI-generated** (code, comments, and docs), with **no human-authored lines**.

**What this does *not* change**

- **Runtime facts** are unchanged: security properties follow from what the binary does (peer binding, MAC verification, audit gating). A finding grounded in `authenticate` or `readAndVerifySignedBody` remains valid regardless of author.

**What it *does* change (process and confidence)**

1. **Comment–code drift risk is elevated.** Example already observed: `handleDiagnosticReport` states a trust model aligned with other routes but **skips** `verifySignedRequestWithoutBody` (`internal/loopgate/server_diagnostic_handlers.go`). AI-heavy repos often exhibit **convincing comments that lag refactors**.
2. **Invariant testing and human review become the primary quality gate.** Strengths (lock-order notes, AMP references) signal **prompt-driven discipline**, not independent verification. Treat “documented invariants” as **claims to test**, not guarantees.
3. **Uniform competence masking edge cases.** Broad consistency can hide **single-route inconsistencies** (diagnostic/MAC), **integration mismatches** (delegated credentials across subprocess PIDs vs peer binding—see §6), or **policy vs product exceptions** (`haven` auto-allow).
4. **Supply chain and secret-handling narratives need extra scrutiny**—not because AI is malicious, but because **operational security** (key storage on Linux, deployment guides) is easy to present as complete while stubs remain.

**Recommendation:** Institute **human security review** for boundary changes, maintain a **route × auth × MAC × audit** matrix as a CI-checked or review-checked artifact, and run **`govulncheck`** plus periodic dependency review.

---

## 3. Confirmed strengths (verified in code)

| Area | Evidence |
|------|-----------|
| Unix socket + peer identity | `Serve` uses `net.Listen("unix", ...)`, `chmod` `0o600`; `ConnContext` attaches peer identity (`internal/loopgate/server.go`). |
| Token + peer binding | `authenticate` compares `tokenClaims.PeerIdentity` to live peer (`internal/loopgate/server.go`). |
| Approval + peer binding | `authenticateApproval` compares `activeSession.PeerIdentity` to live peer (same file). |
| Signed requests | `verifySignedRequest` / `readAndVerifySignedBody`; nonces in `recordAuthNonce`; optional persistence `loadNonceReplayState` / `saveNonceReplayState`. |
| Request-ID replay window | `recordRequest` per control session + request id. |
| Single-use execution tokens | `deriveExecutionToken` + `consumeExecutionToken` with bound capability and sorted argument hash. |
| Approval manifest | `computeApprovalManifestSHA256` / `buildCapabilityApprovalManifest`; decision verifies manifest on approve; post-approval execution uses **stored** `pendingApproval.Request` for generic capabilities (`internal/loopgate/server_capability_handlers.go`). |
| Audit fail-closed (examples) | Many paths return `DenialCodeAuditUnavailable` when `logEvent` fails; morphling and task-plan tests assert behavior. |
| Policy deny default | Unknown tool category → deny (`internal/policy/checker.go`). |
| Sandbox paths | `NormalizeRelativePath`, `EvalSymlinks`, `ensureWithinRoot` (`internal/sandbox/sandbox.go`). |
| Secret export heuristic + audit | `isSecretExportCapability` + `logEvent` before deny (`internal/loopgate/server.go`). |
| Morphling parent session | Mismatch → `errMorphlingNotFound` pattern (non-leaking). |

---

## 4. Findings (severity, category, type)

Each item: **severity**, **category**, **location**, **issue**, **why it matters**, **scenario**, **recommendation**, **type** (`vulnerability` / `design risk` / `hardening` / `informational`).

### F1 — Session open: socket access ⇒ new control session (same OS user typically)

- **Severity:** High (threat-model constraint)  
- **Category:** Trust boundary  
- **Location:** `handleSessionOpen` (`internal/loopgate/server_model_handlers.go`)  
- **Issue:** No bearer/MAC at open. Any process that can connect to the socket and pass body validation can obtain **its own** session, MAC key, and tokens (subject to rate limits and capability intersection).  
- **Why it matters:** Local malware **as the socket’s user** can use Loopgate as a **policy-governed** API for its own session.  
- **Scenario:** Malicious binary connects, opens session, requests allowed capabilities.  
- **Recommendation:** Document **socket permissions** and optional **`expectedClientPath`** pinning; consider default-on pinning for shipped desktop bundles where binary path is stable.  
- **Type:** **Architectural / deployment assumption** (not a bug).

### F2 — Executable path pinning — **addressed (2026-04-07)**

- **Severity:** Medium  
- **Category:** Client binding  
- **Location:** `expectedClientPath` + `handleSessionOpen` (`internal/loopgate/server_model_handlers.go`); loaded from **`config/runtime.yaml`** → **`control_plane.expected_session_client_executable`** in `NewServerWithOptions` (`internal/loopgate/server.go`).  
- **Issue (historical):** Optional defense was **unwired** from operator config.  
- **Resolution:** Non-empty absolute path enables pin (after `filepath.Clean`); empty keeps default **off**. Tests: `session_executable_pin_test.go`, `config_test.go` (relative path rejected).  
- **Type:** **Hardening** (closed for wiring; still **off** by default).

### F3 — `isSecretExportCapability` is heuristic — **partially addressed (2026-04-07)**

- **Severity:** Medium  
- **Category:** Secret isolation  
- **Location:** `internal/loopgate/secret_export.go` (name heuristic for **unregistered** names); `internal/tools/tool.go` (optional interfaces); `capabilityProhibitsRawSecretExport` (registered tools: interfaces only; configured capabilities: `configuredCapabilityTool.RawSecretExportProhibited`)  
- **Issue:** Prefix/substring rules; future capability names could evade while exporting sensitive data.  
- **Resolution (partial):** Registry implements `SecretExportNameHeuristicOptOut` / `RawSecretExportProhibited`; unregistered names still use the heuristic before the unknown-capability path. Further tightening: explicit allow/deny per registered capability.  
- **Type:** **Design risk / hardening** (ongoing).

### F4 — `haven` + `TrustedSandboxLocal()` auto-allow under `NeedsApproval` policy — **policy-gated (2026-04-07)**

- **Severity:** Medium  
- **Category:** Policy  
- **Location:** `Server.shouldAutoAllowTrustedSandboxCapability` (`internal/loopgate/server.go`); **`core/policy/policy.yaml`** → **`safety.haven_trusted_sandbox_auto_allow`** (optional; **default-on** when omitted) and **`safety.haven_trusted_sandbox_auto_allow_capabilities`** (optional allowlist; non-nil empty slice = disable for all).  
- **Issue:** Intended product behavior reduces human approval for **actor `haven`** + **TrustedSandboxLocal** tools when policy would otherwise require approval.  
- **Recommendation:** Operators tighten via policy; continue to minimize the trusted tool set in code; optional future metrics.  
- **Type:** **Design risk** (explicit, now **configurable**).

### F5 — Linux/Windows secure store stubs (fail-closed)

- **Severity:** Medium (operational)  
- **Category:** Secrets  
- **Location:** `internal/secrets/local_dev_store.go`, `stub_secure_store.go`  
- **Issue:** Non-macOS backends return unavailable for Get/Put.  
- **Recommendation:** Real keyring integration or explicit supported mode docs.  
- **Type:** **Gap** (fail-closed, not silent bypass).

### F6 — In-memory maps: DoS / memory pressure — **partially addressed (2026-04-07)**

- **Severity:** Medium  
- **Category:** Availability  
- **Location:** `Server` maps + `pruneExpiredLocked`; `countPendingApprovalsForSessionLocked`, `recordRequest`, `recordAuthNonce`  
- **Issue:** Bounded only by time windows; hostile client can churn entries.  
- **Resolution (partial):** Default caps on pending approvals per control session (64) and on `seenRequests` / `seenAuthNonce` size (65536 each); fail closed with `pending_approval_limit_reached` / `replay_state_saturated` instead of evicting replay entries. Monitoring still recommended for production.  
- **Type:** **Hardening / reliability** (ongoing).

### F7 — `ExecutionBodySHA256` stored; shallow copy of `Arguments` in pending approvals — **addressed (2026-04-07)**

- **Severity:** Low  
- **Category:** Approval integrity  
- **Location:** `pendingApproval`, `buildCapabilityApprovalManifest`, `cloneCapabilityRequest`, approval decision handlers  
- **Issue:** Comment referenced future live-body verify; execution uses stored request (good). Map aliasing is a minor integrity footgun if ever mutated.  
- **Resolution:** Deep-copy at store (`cloneCapabilityRequest`); verify body hash before post-approval execute when `ExecutionBodySHA256` is non-empty; tests in `approval_manifest_test.go` and `server_test.go`.  
- **Type:** **Hardening** (closed for capability approvals; morphling spawn may add body hash in a follow-on).

### F8 — `GET /v1/diagnostic/report`: Bearer without request MAC — **addressed**

- **Severity:** Low–Medium (was)  
- **Category:** Transport consistency  
- **Location:** `internal/loopgate/server_diagnostic_handlers.go`  
- **Issue (historical):** Handler authenticated with Bearer only and skipped request MAC.  
- **Resolution (2026-04-06):** `handleDiagnosticReport` now calls `verifySignedRequestWithoutBody` after `authenticate`; regression test `TestDiagnosticReportRequiresSignedRequest` in `server_diagnostic_handlers_test.go`.  
- **Type:** **Hardening** (closed in code).

### F9 — Delegated MCP credentials vs peer PID binding *(historical — `mcpserve` removed)*

- **Severity:** Informational–Medium (integration) *(at time of review)*  
- **Category:** Integration  
- **Location:** `authenticate` peer check; **former** `mcpserve` delegated env path (**removed** per ADR 0010)  
- **Issue:** Tokens minted on process A **cannot** be used from process B (different PID). **Still true** for any **out-of-tree** bridge that reuses the HTTP API: each client process should open **its own** session on the socket.  
- **Recommendation:** Keep documenting **single-process** token use for HTTP clients; any **future** MCP forwarder must not weaken this model.  
- **Type:** **Informational / integration contract** (retained for peer-binding semantics).

### F10 — `/v1/hook/pre-validate` trust model

- **Severity:** Informational  
- **Category:** Hook  
- **Location:** `internal/loopgate/server_hook_handlers.go`  
- **Issue:** Peer **UID must equal server UID**; no session/MAC—by design for Claude Code hook.  
- **Type:** **Informational** (document for operators).

---

## 5. Architecture assessment

**Stated trust model vs implementation:** Largely aligned: NL is not authority for capabilities; registry + policy + tokens gate execution; approvals bind manifests; audit is mandatory on many sensitive transitions.

**Strong boundaries:** Unix locality; peer + token binding; MAC on most routes; sandbox resolver; morphling `ParentControlSessionID` checks.

**Drift / optimism:** heuristic secret export; ledger hash chain without keyed authentication; exe pin and Haven auto-allow **default permissive** until operators set policy/runtime fields; enterprise tenancy/IDP is **not** end-to-end proven in this review.

---

## 6. Product / security positioning

**Vs typical agent harnesses:** Stronger on **signed control plane**, **approval binding**, **audit fail-closed**, **sandbox path discipline**.

**Public claims require:** Clear **single-user / local socket** story; honest **non-macOS secret** posture; explicit **`haven`** behavior; **human** review for AI-generated changes.

---

## 7. Priority recommendations

### Immediate (top 5)

1. **`GET /v1/diagnostic/report` signed GET:** implemented 2026-04-06 (was: add `verifySignedRequestWithoutBody`).  
2. Replace **heuristic** `isSecretExportCapability` with **registry metadata** + tests — **partial:** optional registry interfaces + heuristic fallback (2026-04-07).  
3. **Deep-copy** pending approval `CapabilityRequest` / assert body hash before execute — **done** (2026-04-07).  
4. Review **`shouldAutoAllowTrustedSandboxCapability`** surface — **done:** policy hooks under **`safety`** in `core/policy/policy.yaml`; metrics still optional.  
5. Document **delegated session / peer PID** requirements for **HTTP and out-of-tree bridges** (in-tree MCP removed; see ADR 0010).

### Next hardening (top 5)

1. Linux/Windows **secure credential** backends.  
2. **Ledger** keyed signing or checkpoints + operator threat-model doc (see `docs/reports/security-hardening-plan-2026-04.md`).  
3. **Caps** on replay/nonce/map growth — **done** (2026-04-07); monitor in production.  
4. **govulncheck** + dependency policy for indirect stacks (e.g. Wails-related) — **partial:** `govulncheck` script + CI workflow.  
5. Fuzz/malformed JSON tests on hot handlers.

### Later but important (top 5)

1. HA / multi-instance ledger + nonce semantics.  
2. MAC key rotation story for long sessions.  
3. Formal **route × auth × MAC × audit-required** checklist in CI or review template.  
4. Morphling runner binary as **privileged helper** supply-chain review.  
5. Operator UX regression tests for approval manifest mismatch.

---

## 8. HTTP route × authentication matrix

**Transport:** All routes on **Unix domain socket** (`internal/loopgate/server.go`).  
**Peer:** For routes using `authenticate` / `authenticateApproval`, failed peer resolution → missing identity → **401** on those paths.

| Pattern | AuthN | Request MAC | Representative routes |
|---------|--------|-------------|------------------------|
| None | None | None | `GET /v1/health` |
| Peer UID = server UID | None | None | `POST /v1/hook/pre-validate` |
| Session bootstrap | None | None | `POST /v1/session/open` (optional exe path if `expectedClientPath` set) |
| Bearer + GET MAC | `authenticate` + `verifySignedRequestWithoutBody` | `GET /v1/status`, `GET /v1/connections/status`, `GET /v1/config/{section}`, `GET /v1/ui/*` (status-style), `GET /v1/tasks*`, Haven model list GETs |
| Bearer + POST MAC | `authenticate` + `readAndVerifySignedBody` | `/v1/capabilities/execute`, `/v1/memory/*`, `/v1/sandbox/*`, `/v1/morphlings/*` (except worker open), `/v1/task/*`, `/v1/model/reply`, `/v1/chat`, `/v1/connections/*` POSTs, quarantine, folder access, etc. |
| Haven actor gate | As above + `actor == haven` | `POST /v1/chat`, `GET /v1/model/openai/models`, … |
| Approval | `X-Loopgate-Approval-Token` + `authenticateApproval` + signed body | `POST /v1/approvals/{id}/decision` |
| Morphling worker | **Open:** peer + JSON, **no** bearer/MAC; **Actions:** worker auth + `readAndVerifyMorphlingWorkerSignedBody` | `/v1/morphlings/worker/open` vs `start|update|complete` |
| (formerly outlier; now aligned) | Bearer + GET MAC | `GET /v1/diagnostic/report` |

**Historical at review time:** `/v1/haven/...` aliases pointed to the same handlers as `/v1/...` (`internal/loopgate/server.go`). Those aliases have since been removed.

---

## 9. Session open and peer binding (precise)

### Checks required to open a session

See `handleSessionOpen` (`internal/loopgate/server_model_handlers.go`): valid POST JSON; non-empty granted capabilities (intersect with registry); peer identity present; optional executable path match if configured; `maxActiveSessionsPerUID` and `sessionOpenMinInterval`; successful `session.opened` audit or rollback.

### Is “same UID” enough to use someone else’s session?

**No** for Bearer or approval tokens: `tokenClaims.PeerIdentity != requestPeerIdentity` and `activeSession.PeerIdentity != requestPeerIdentity` deny (`internal/loopgate/server.go`). Identity includes **PID** (and **EPID** on Darwin via `LOCAL_PEERCRED` / `LOCAL_PEERPID` / `LOCAL_PEEREPID`).

### Is “same UID” enough to get *a* Loopgate session?

**If** the process can connect to the socket (typically same user as socket owner): **yes**, it can open **its own** session (subject to limits and capability filtering). That is **not** impersonation of another process’s session, but it can be **equivalent capability** for an attacker.

### Executable path binding

Implemented when **`control_plane.expected_session_client_executable`** in **`config/runtime.yaml`** is non-empty (sets `expectedClientPath` after clean). **Default is empty** (pinning off).

---

## 10. Final verdict

The implementation **earns** a serious local control-plane story: enforcement is mostly **explicit in code**, with stronger approval and audit mechanics than common agent frameworks. Residual work is **secret-export classification**, **ledger authenticity** vs filesystem attackers, **finishing cross-platform secrets**, and **operational clarity** for **peer binding** (and any **out-of-tree** IDE bridges). Diagnostic GET MAC consistency is **aligned** (F8).

**AI-only authorship** does not invalidate the technical findings above; it **raises the bar** for independent test coverage, human review on boundary changes, and distrust of **comments without tests**.

---

## 11. References (key files)

- `internal/loopgate/server.go` — mux, `ConnContext`, `authenticate`, `authenticateApproval`, `verifySignedRequest`, pruning, `capabilityProhibitsRawSecretExport`, `NewServerWithOptions`
- `internal/loopgate/secret_export.go` — unregistered secret-export name heuristic  
- `internal/loopgate/server_model_handlers.go` — `handleSessionOpen`  
- `internal/loopgate/server_capability_handlers.go` — execute + approval decision  
- `internal/loopgate/server_diagnostic_handlers.go` — diagnostic outlier  
- `internal/loopgate/server_hook_handlers.go` — hook trust model  
- `internal/loopgate/server_morphling_worker_handlers.go` — worker auth shapes  
- `internal/loopgate/approval_manifest.go` — manifest computation  
- `internal/loopgate/peercred_darwin.go`, `peercred_linux.go` — peer identity  
- `internal/sandbox/sandbox.go` — path resolution  
- `internal/policy/checker.go` — policy  
- `internal/secrets/stub_secure_store.go` — non-macOS stub  
- `docs/setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md` — peer binding, exe pin, Haven auto-allow policy  

---

*End of report.*
