# Security Best Practices Report

Generated: 2026-04-11

## Executive Summary

Targeted review scope:

- control-plane auth and signed-request handling
- approval/UI decision bridging
- morphling worker bootstrap handlers
- Haven operator settings routes

No critical or high-severity issues were found in the reviewed scope. Signed-request coverage, peer binding, approval manifest binding, and trusted-Haven route gating all held up in this pass.

Four lower-severity issues were identified and fixed in this change set:

1. a malformed morphling worker open request could return early without emitting an explicit denial response
2. Haven settings routes reflected raw backend/storage errors to authenticated UI clients, leaking local runtime implementation detail
3. Haven model settings routes reflected raw config and validation errors to authenticated UI clients, leaking config paths and internal connection metadata
4. Haven chat runtime setup reflected raw runtime config and model initialization errors to authenticated UI clients, leaking backend detail before the SSE stream opened

## Fixed Findings

### SG-001

- Rule ID: GO-HTTP-002 / explicit denial-path handling
- Severity: Medium
- Location: `internal/loopgate/server_morphling_worker_handlers.go:75-82`
- Evidence:

```go
var openRequest MorphlingWorkerOpenRequest
if err := server.decodeJSONBody(writer, request, maxCapabilityBodyBytes, &openRequest); err != nil {
	return
}
```

- Impact: a malformed `/v1/morphlings/worker/open` body could exit the handler without a structured denial response. That creates a success-looking transport edge for a worker bootstrap path and weakens fail-closed operator/client behavior.
- Fix: emit `400 Bad Request` with `DenialCodeMalformedRequest` when JSON decode fails.
- Mitigation: regression coverage added for malformed worker-open bodies.
- False positive notes: none; this was directly visible in handler code.

Status: fixed in the current worktree/commit.

### SG-004

- Rule ID: GO-CONFIG-001 / UI projection redaction invariant
- Severity: Low
- Location: `internal/loopgate/server_haven_chat_runtime_setup.go:34-42`, `internal/loopgate/server_haven_chat_runtime_setup.go:45-53`
- Evidence:

```go
server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
	Status:       ResponseStatusError,
	DenialReason: "load model runtime config: " + err.Error(),
	DenialCode:   DenialCodeExecutionFailed,
})
```

- Impact: authenticated Haven chat clients could receive raw model runtime configuration and initialization errors before the SSE stream opened. That could expose config-path or connection-binding detail through the operator chat surface.
- Fix: replace raw runtime prep failures with stable redacted operator messages and mark those responses `Redacted: true`.
- Mitigation: regression coverage added for unreadable runtime config and model-client initialization failure so those paths no longer leak config paths or backend detail.
- False positive notes: scoped only to the pre-stream runtime preparation path; streamed tool/runtime failures still use the existing fallback-text path once the SSE response has started.

Status: fixed in the current worktree/commit.

### SG-003

- Rule ID: GO-CONFIG-001 / UI projection redaction invariant
- Severity: Low
- Location: `internal/loopgate/server_haven_model_settings.go:68-76`, `internal/loopgate/server_haven_model_settings.go:153-161`, `internal/loopgate/server_haven_model_settings.go:294-301`, `internal/loopgate/server_haven_model_settings.go:305-311`
- Evidence:

```go
server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
	Status:       ResponseStatusError,
	DenialReason: fmt.Sprintf("load model config: %v", err),
	DenialCode:   DenialCodeExecutionFailed,
})
```

- Impact: authenticated Haven model-settings clients could receive raw filesystem/config errors and validation failures containing local config paths, stored connection IDs, or backend mismatch detail. This does not create new authority, but it leaks internal runtime detail through an operator-facing settings route likely to be part of the UI MVP.
- Fix: replace raw load/save/validation error text with stable redacted operator messages and mark the responses `Redacted: true`.
- Mitigation: regression coverage added to ensure GET does not leak the config path, POST validation does not leak stale connection metadata, and POST save failure does not leak the config path.
- False positive notes: malformed user input validation is still allowed to return specific request-shape errors; the fix is scoped to internal config/runtime failures rather than all bad-request responses.

Status: fixed in the current worktree/commit.

### SG-002

- Rule ID: GO-CONFIG-001 / UI projection redaction invariant
- Severity: Low
- Location: `internal/loopgate/server_haven_settings.go:61-68`, `internal/loopgate/server_haven_settings.go:97-103`, `internal/loopgate/server_haven_settings.go:159-166`, `internal/loopgate/server_haven_settings.go:189-195`
- Evidence:

```go
server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
	Status:       ResponseStatusError,
	DenialReason: err.Error(),
	DenialCode:   DenialCodeExecutionFailed,
})
```

- Impact: authenticated Haven settings clients could receive raw filesystem/storage errors, including local runtime path details such as `runtime/state/haven_preferences.json`. This does not create new authority, but it leaks implementation detail through an operator-facing UI surface.
- Fix: replace raw backend/storage error text with stable redacted operator messages and mark the response `Redacted: true`.
- Mitigation: regression coverage added to ensure the prefs path does not leak through the idle-settings read path.
- False positive notes: scope is intentionally narrow to Haven settings; other operator-only admin/config surfaces still deserve a similar cleanup pass later.

Status: fixed in the current worktree/commit.

## Reviewed Areas With No New Finding

- `internal/loopgate/request_auth.go`
  - bearer auth still requires peer binding and signed-request validation
- `internal/loopgate/approval_flow.go`
  - approval decision nonce and manifest binding still fail closed
- `internal/loopgate/ui_server.go`
  - UI approval routes still forward into the same approval authority path rather than inventing UI-local state
- `internal/loopgate/server_haven_chat_request.go`
  - Haven chat still requires trusted Haven session plus `model.reply`

## Follow-up Recommendations

1. Sweep remaining operator-only config and diagnostic handlers for raw `err.Error()` reflection and normalize them to stable redacted messages.
2. Add one small helper for “execution failed but redacted” responses so new UI endpoints do not reintroduce path leaks ad hoc.
3. Keep UI MVP work on typed Loopgate APIs only; do not let a desktop or browser surface read raw runtime state files directly.
