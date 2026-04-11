package loopgate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// =============================================================================
// HTTP handlers for the TaskPlan vertical slice.
//
// Endpoints:
//   POST /v1/task/plan     — Submit and validate a plan
//   POST /v1/task/lease    — Request a lease (prototype/testing seam)
//   POST /v1/task/execute  — Mediated capability execution (morphling → Loopgate → provider)
//   POST /v1/task/complete — Finalize lease consumption
//   POST /v1/task/result   — Query plan execution result
//
// /v1/task/lease is a prototype seam. In the final architecture, Loopgate owns
// dispatch and lease issuance internally after validated approval. This endpoint
// exists to enable integration testing of the full flow.
//
// /v1/task/execute does NOT accept caller-supplied capability or arguments.
// The lease binds the exact approved capability and arguments from the plan.
// Loopgate executes from lease contents only, keeping execution lineage
// deterministic and avoiding re-submission of authority-bearing inputs.
//
// Loopgate stages provider output during /v1/task/execute. Morphling output
// is treated as untrusted. /v1/task/complete only finalizes lease consumption.
//
// All handlers: authenticate via server.mu (authenticate releases server.mu
// before returning), then acquire taskPlansMu for state operations. These
// locks are never held simultaneously.
// =============================================================================

// --- POST /v1/task/plan ---

func (server *Server) handleTaskPlanSubmit(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// authenticate acquires and releases server.mu internally.
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityTaskPlanWrite) {
		return
	}

	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxTaskPlanBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var submitRequest SubmitTaskPlanRequest
	if err := decodeJSONBytes(requestBodyBytes, &submitRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, SubmitTaskPlanResponse{
			Status:       ResponseStatusDenied,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	if submitRequest.GoalText == "" {
		server.writeJSON(writer, http.StatusBadRequest, SubmitTaskPlanResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "goal_text must not be empty",
			DenialCode:   DenialCodeTaskPlanInvalid,
		})
		return
	}
	if len(submitRequest.Steps) == 0 {
		server.writeJSON(writer, http.StatusBadRequest, SubmitTaskPlanResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "plan must contain at least one step",
			DenialCode:   DenialCodeTaskPlanInvalid,
		})
		return
	}
	for _, step := range submitRequest.Steps {
		if step.Capability == "" {
			server.writeJSON(writer, http.StatusBadRequest, SubmitTaskPlanResponse{
				Status:       ResponseStatusDenied,
				DenialReason: fmt.Sprintf("step %d missing capability", step.StepIndex),
				DenialCode:   DenialCodeTaskPlanInvalid,
			})
			return
		}
	}

	// Verify canonical hash: recompute from goal+steps and compare.
	computedHash := computeCanonicalHash(submitRequest.GoalText, submitRequest.Steps)
	if computedHash != submitRequest.CanonicalHash {
		server.writeJSON(writer, http.StatusBadRequest, SubmitTaskPlanResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "canonical hash mismatch",
			DenialCode:   DenialCodeTaskPlanHashMismatch,
		})
		return
	}

	// Fail-closed: deny unknown capabilities.
	for _, step := range submitRequest.Steps {
		if !knownTaskCapabilities[step.Capability] {
			server.writeJSON(writer, http.StatusBadRequest, SubmitTaskPlanResponse{
				Status:       ResponseStatusDenied,
				DenialReason: fmt.Sprintf("unknown capability: %s", step.Capability),
				DenialCode:   DenialCodeTaskPlanInvalid,
			})
			return
		}
	}

	planID, err := randomHex(16)
	if err != nil {
		server.writeJSON(writer, http.StatusInternalServerError, SubmitTaskPlanResponse{
			Status:       ResponseStatusError,
			DenialReason: "failed to generate plan id",
		})
		return
	}

	nowUTC := server.now().UTC()
	validatedAt := nowUTC
	plan := &taskPlanRecord{
		PlanID:         planID,
		SessionID:      tokenClaims.ControlSessionID,
		ActorLabel:     tokenClaims.ActorLabel,
		GoalText:       submitRequest.GoalText,
		Steps:          submitRequest.Steps,
		CanonicalHash:  computedHash,
		State:          taskPlanStateValidated,
		CreatedAtUTC:   nowUTC,
		ValidatedAtUTC: &validatedAt,
	}

	// taskPlansMu acquired after server.mu is released (authentication already complete).
	server.taskPlansMu.Lock()
	server.taskPlans[planID] = plan
	server.taskPlansMu.Unlock()

	// Audit event: append-only ledger, separate from runtime telemetry.
	// logEvent acquires auditMu internally; taskPlansMu is not held.
	if err := server.logEvent("task.plan.validated", tokenClaims.ControlSessionID, map[string]interface{}{
		"plan_id":        planID,
		"goal_text":      submitRequest.GoalText,
		"step_count":     len(submitRequest.Steps),
		"canonical_hash": computedHash,
	}); err != nil {
		server.taskPlansMu.Lock()
		delete(server.taskPlans, planID)
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusServiceUnavailable, taskPlanAuditUnavailableResponse())
		return
	}

	server.writeJSON(writer, http.StatusOK, SubmitTaskPlanResponse{
		PlanID: planID,
		Status: taskPlanStateValidated,
	})
}

// --- POST /v1/task/lease ---
// Prototype/testing seam. In the final architecture, Loopgate owns dispatch
// and lease issuance internally after validated approval.

func (server *Server) handleTaskLeaseRequest(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityTaskPlanWrite) {
		return
	}

	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxTaskPlanBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var leaseRequest RequestTaskLeaseRequest
	if err := decodeJSONBytes(requestBodyBytes, &leaseRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, RequestTaskLeaseResponse{
			Status:       ResponseStatusDenied,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	// All state reads/writes under taskPlansMu. server.mu is not held.
	server.taskPlansMu.Lock()

	plan, found := server.taskPlans[leaseRequest.PlanID]
	if !found {
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusNotFound, RequestTaskLeaseResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "plan not found",
			DenialCode:   DenialCodeTaskPlanNotFound,
		})
		return
	}
	if plan.SessionID != tokenClaims.ControlSessionID {
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusNotFound, RequestTaskLeaseResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "plan not found",
			DenialCode:   DenialCodeTaskPlanNotFound,
		})
		return
	}

	if plan.State != taskPlanStateValidated {
		currentState := plan.State
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusConflict, RequestTaskLeaseResponse{
			Status:       ResponseStatusDenied,
			DenialReason: fmt.Sprintf("plan is in state %s, expected validated", currentState),
			DenialCode:   DenialCodeTaskPlanStateInvalid,
		})
		return
	}

	if leaseRequest.PlanHash != plan.CanonicalHash {
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusBadRequest, RequestTaskLeaseResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "plan hash does not match canonical hash",
			DenialCode:   DenialCodeTaskPlanHashMismatch,
		})
		return
	}

	if leaseRequest.StepIndex < 0 || leaseRequest.StepIndex >= len(plan.Steps) {
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusBadRequest, RequestTaskLeaseResponse{
			Status:       ResponseStatusDenied,
			DenialReason: fmt.Sprintf("invalid step index %d", leaseRequest.StepIndex),
			DenialCode:   DenialCodeTaskPlanInvalid,
		})
		return
	}

	step := plan.Steps[leaseRequest.StepIndex]

	leaseID, err := randomHex(16)
	if err != nil {
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusInternalServerError, RequestTaskLeaseResponse{
			Status:       ResponseStatusError,
			DenialReason: "failed to generate lease id",
		})
		return
	}

	morphlingIDSuffix, err := randomHex(16)
	if err != nil {
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusInternalServerError, RequestTaskLeaseResponse{
			Status:       ResponseStatusError,
			DenialReason: "failed to generate morphling id",
		})
		return
	}
	morphlingID := "morphling-" + morphlingIDSuffix

	nowUTC := server.now().UTC()
	expiresAt := nowUTC.Add(taskLeaseTTL)
	stagingDir := filepath.Join(server.sandboxPaths.Outputs, "task-staging", leaseRequest.PlanID, leaseID)

	lease := &taskLeaseRecord{
		LeaseID:      leaseID,
		PlanID:       leaseRequest.PlanID,
		PlanHash:     plan.CanonicalHash,
		StepIndex:    leaseRequest.StepIndex,
		MorphlingID:  morphlingID,
		Capability:   step.Capability,
		Arguments:    step.Arguments,
		StagingDir:   stagingDir,
		State:        taskLeaseStateIssued,
		ExpiresAtUTC: expiresAt,
		CreatedAtUTC: nowUTC,
	}

	if err := transitionTaskPlanState(plan, taskPlanStateLeaseIssued); err != nil {
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusInternalServerError, RequestTaskLeaseResponse{
			Status:       ResponseStatusError,
			DenialReason: "failed to transition plan state",
		})
		return
	}
	plan.LeaseID = leaseID
	server.taskLeases[leaseID] = lease
	server.taskPlansMu.Unlock()

	// Audit: taskPlansMu released before logEvent acquires auditMu.
	if err := server.logEvent("task.lease.issued", tokenClaims.ControlSessionID, map[string]interface{}{
		"plan_id":      leaseRequest.PlanID,
		"lease_id":     leaseID,
		"morphling_id": morphlingID,
		"step_index":   leaseRequest.StepIndex,
		"capability":   step.Capability,
		"expires_at":   expiresAt.Format(time.RFC3339Nano),
	}); err != nil {
		server.taskPlansMu.Lock()
		delete(server.taskLeases, leaseID)
		plan, found := server.taskPlans[leaseRequest.PlanID]
		if found {
			plan.LeaseID = ""
			plan.State = taskPlanStateValidated
		}
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusServiceUnavailable, taskLeaseAuditUnavailableResponse())
		return
	}

	server.writeJSON(writer, http.StatusOK, RequestTaskLeaseResponse{
		LeaseID:      leaseID,
		MorphlingID:  morphlingID,
		Capability:   step.Capability,
		StagingDir:   taskPlanStagingRef(leaseRequest.PlanID, leaseID),
		ExpiresAtUTC: expiresAt.Format(time.RFC3339Nano),
		Status:       taskLeaseStateIssued,
	})
}

// --- POST /v1/task/execute ---
// Mediated capability execution. The morphling invokes this endpoint; Loopgate
// validates the lease, executes the provider internally using the lease's bound
// capability and arguments, stages the provider output, and returns the result.
//
// The caller does NOT supply capability or arguments — those are bound by the
// lease from the validated plan. This keeps execution lineage deterministic.
//
// Provider output is staged by Loopgate (trusted). The morphling receives the
// result but does not control what is staged.

func (server *Server) handleTaskLeaseExecute(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityTaskPlanWrite) {
		return
	}

	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxTaskPlanBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var executeRequest ExecuteTaskLeaseRequest
	if err := decodeJSONBytes(requestBodyBytes, &executeRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, ExecuteTaskLeaseResponse{
			Status:       ResponseStatusDenied,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	// Phase 1: Validate lease and transition to executing state.
	// All under taskPlansMu; server.mu is not held.
	server.taskPlansMu.Lock()

	lease, found := server.taskLeases[executeRequest.LeaseID]
	if !found {
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusNotFound, ExecuteTaskLeaseResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "lease not found",
			DenialCode:   DenialCodeTaskLeaseNotFound,
		})
		return
	}
	plan, planFound := server.taskPlans[lease.PlanID]
	if !planFound || plan.SessionID != tokenClaims.ControlSessionID {
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusNotFound, ExecuteTaskLeaseResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "lease not found",
			DenialCode:   DenialCodeTaskLeaseNotFound,
		})
		return
	}

	if lease.State == taskLeaseStateConsumed {
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusConflict, ExecuteTaskLeaseResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "lease already consumed",
			DenialCode:   DenialCodeTaskLeaseConsumed,
		})
		return
	}

	if lease.State != taskLeaseStateIssued {
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusConflict, ExecuteTaskLeaseResponse{
			Status:       ResponseStatusDenied,
			DenialReason: fmt.Sprintf("lease is in state %s, expected issued", lease.State),
			DenialCode:   DenialCodeTaskLeaseConsumed,
		})
		return
	}

	if executeRequest.MorphlingID != lease.MorphlingID {
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusForbidden, ExecuteTaskLeaseResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "morphling id does not match lease",
			DenialCode:   DenialCodeTaskLeaseMorphlingMismatch,
		})
		return
	}

	// Fail-closed on expiry.
	if server.now().UTC().After(lease.ExpiresAtUTC) {
		lease.State = taskLeaseStateExpired
		plan, planFound := server.taskPlans[lease.PlanID]
		if planFound {
			plan.State = taskPlanStateFailed
		}
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusGone, ExecuteTaskLeaseResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "lease has expired",
			DenialCode:   DenialCodeTaskLeaseExpired,
		})
		return
	}

	// Transition lease and plan to executing state.
	if err := transitionTaskLeaseState(lease, taskLeaseStateExecuting); err != nil {
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusInternalServerError, ExecuteTaskLeaseResponse{
			Status:       ResponseStatusError,
			DenialReason: "failed to transition lease state",
		})
		return
	}
	if planFound {
		_ = transitionTaskPlanState(plan, taskPlanStateExecuting)
	}

	// Capture lease-bound values for execution outside the lock.
	capability := lease.Capability
	arguments := make(map[string]string, len(lease.Arguments))
	for k, v := range lease.Arguments {
		arguments[k] = v
	}
	leaseID := lease.LeaseID
	leasePlanID := lease.PlanID
	leaseStepIndex := lease.StepIndex
	stagingDir := lease.StagingDir

	// Create execution record.
	executionID, err := randomHex(16)
	if err != nil {
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusInternalServerError, ExecuteTaskLeaseResponse{
			Status:       ResponseStatusError,
			DenialReason: "failed to generate execution id",
		})
		return
	}
	nowUTC := server.now().UTC()
	execution := &taskExecutionRecord{
		ExecutionID:  executionID,
		LeaseID:      leaseID,
		PlanID:       leasePlanID,
		Capability:   capability,
		State:        "running",
		StartedAtUTC: nowUTC,
	}
	server.taskExecutions[executionID] = execution
	if planFound {
		plan.ExecutionID = executionID
	}

	server.taskPlansMu.Unlock()

	// Phase 2: Execute provider. No locks held during execution.
	var providerOutput json.RawMessage
	var execErr error

	switch capability {
	case "echo.generate_summary":
		providerOutput, execErr = executeEchoGenerateSummary(arguments)
	default:
		execErr = fmt.Errorf("unknown capability: %s", capability)
	}

	// Phase 3: Stage output and update execution record.
	// Re-acquire taskPlansMu for state updates.
	server.taskPlansMu.Lock()

	execution = server.taskExecutions[executionID]
	_ = server.taskLeases[leaseID]
	plan = server.taskPlans[leasePlanID]

	if execErr != nil {
		if execution != nil {
			execution.State = "failed"
			execution.ErrorMessage = execErr.Error()
			completedAt := server.now().UTC()
			execution.CompletedAtUTC = &completedAt
		}
		if plan != nil {
			plan.State = taskPlanStateFailed
		}
		// Lease remains in executing state (not consumed) on provider failure.
		server.taskPlansMu.Unlock()

		server.writeJSON(writer, http.StatusInternalServerError, ExecuteTaskLeaseResponse{
			Status:       ResponseStatusError,
			DenialReason: execErr.Error(),
		})
		return
	}

	// Compute output hash for audit determinism.
	outputHash := sha256.Sum256(providerOutput)
	outputHashHex := hex.EncodeToString(outputHash[:])

	// Stage provider output to disk. Loopgate controls staging — morphling
	// output is untrusted and not involved here.
	artifactRef := ""
	artifactPath := ""
	if stagingDir != "" {
		if mkdirErr := os.MkdirAll(stagingDir, 0o700); mkdirErr == nil {
			artifactPath = filepath.Join(stagingDir, "result.json")
			if writeErr := os.WriteFile(artifactPath, providerOutput, 0o600); writeErr == nil {
				artifactRef = taskPlanArtifactRef(leasePlanID, leaseID)
			}
		}
	}

	stepResult := &TaskStepResult{
		StepIndex:    leaseStepIndex,
		Capability:   capability,
		ProviderName: "echo",
		OutputData:   providerOutput,
		OutputHash:   outputHashHex,
	}

	if execution != nil {
		execution.State = "succeeded"
		execution.ProviderOutput = providerOutput
		execution.OutputHash = outputHashHex
		execution.ArtifactRef = artifactRef
		completedAt := server.now().UTC()
		execution.CompletedAtUTC = &completedAt
	}

	server.taskPlansMu.Unlock()

	// Audit: logEvent acquires auditMu internally. No other locks held.
	if err := server.logEvent("task.step.executed", tokenClaims.ControlSessionID, map[string]interface{}{
		"plan_id":      leasePlanID,
		"lease_id":     leaseID,
		"execution_id": executionID,
		"capability":   capability,
		"output_hash":  outputHashHex,
		"artifact_ref": artifactRef,
	}); err != nil {
		server.taskPlansMu.Lock()
		execution = server.taskExecutions[executionID]
		lease = server.taskLeases[leaseID]
		plan = server.taskPlans[leasePlanID]
		delete(server.taskExecutions, executionID)
		if lease != nil {
			lease.State = taskLeaseStateIssued
		}
		if plan != nil {
			plan.State = taskPlanStateLeaseIssued
			plan.ExecutionID = ""
		}
		server.taskPlansMu.Unlock()
		if artifactPath != "" {
			_ = os.Remove(artifactPath)
		}
		server.writeJSON(writer, http.StatusServiceUnavailable, taskExecuteAuditUnavailableResponse())
		return
	}

	server.writeJSON(writer, http.StatusOK, ExecuteTaskLeaseResponse{
		Status:     ResponseStatusSuccess,
		StepResult: stepResult,
	})
}

// --- POST /v1/task/complete ---
// Finalizes lease consumption. Provider output was already staged by Loopgate
// during /v1/task/execute. This endpoint exists so the morphling explicitly
// signals completion, establishing the pattern for future multi-step flows.

func (server *Server) handleTaskLeaseComplete(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityTaskPlanWrite) {
		return
	}

	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxTaskPlanBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var completeRequest CompleteTaskLeaseRequest
	if err := decodeJSONBytes(requestBodyBytes, &completeRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CompleteTaskLeaseResponse{
			Status:       ResponseStatusDenied,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	server.taskPlansMu.Lock()

	lease, found := server.taskLeases[completeRequest.LeaseID]
	if !found {
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusNotFound, CompleteTaskLeaseResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "lease not found",
			DenialCode:   DenialCodeTaskLeaseNotFound,
		})
		return
	}
	plan, planFound := server.taskPlans[lease.PlanID]
	if !planFound || plan.SessionID != tokenClaims.ControlSessionID {
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusNotFound, CompleteTaskLeaseResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "lease not found",
			DenialCode:   DenialCodeTaskLeaseNotFound,
		})
		return
	}

	if completeRequest.MorphlingID != lease.MorphlingID {
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusForbidden, CompleteTaskLeaseResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "morphling id does not match lease",
			DenialCode:   DenialCodeTaskLeaseMorphlingMismatch,
		})
		return
	}

	if lease.State == taskLeaseStateConsumed {
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusConflict, CompleteTaskLeaseResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "lease already consumed",
			DenialCode:   DenialCodeTaskLeaseConsumed,
		})
		return
	}

	if lease.State != taskLeaseStateExecuting {
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusConflict, CompleteTaskLeaseResponse{
			Status:       ResponseStatusDenied,
			DenialReason: fmt.Sprintf("lease is in state %s, expected executing", lease.State),
			DenialCode:   DenialCodeTaskPlanStateInvalid,
		})
		return
	}

	// Check expiry even at completion time.
	if server.now().UTC().After(lease.ExpiresAtUTC) {
		lease.State = taskLeaseStateExpired
		plan, planFound := server.taskPlans[lease.PlanID]
		if planFound {
			plan.State = taskPlanStateFailed
		}
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusGone, CompleteTaskLeaseResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "lease has expired",
			DenialCode:   DenialCodeTaskLeaseExpired,
		})
		return
	}

	if err := transitionTaskLeaseState(lease, taskLeaseStateConsumed); err != nil {
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusInternalServerError, CompleteTaskLeaseResponse{
			Status:       ResponseStatusError,
			DenialReason: "failed to transition lease state",
		})
		return
	}

	previousPlanCompletedAtUTC := plan.CompletedAtUTC
	if planFound {
		_ = transitionTaskPlanState(plan, taskPlanStateCompleted)
		nowUTC := server.now().UTC()
		plan.CompletedAtUTC = &nowUTC
	}

	leasePlanID := lease.PlanID
	leaseID := lease.LeaseID

	server.taskPlansMu.Unlock()

	// Audit
	if err := server.logEvent("task.lease.completed", tokenClaims.ControlSessionID, map[string]interface{}{
		"plan_id":  leasePlanID,
		"lease_id": leaseID,
	}); err != nil {
		server.taskPlansMu.Lock()
		lease = server.taskLeases[leaseID]
		plan = server.taskPlans[leasePlanID]
		if lease != nil {
			lease.State = taskLeaseStateExecuting
		}
		if plan != nil {
			plan.State = taskPlanStateExecuting
			plan.CompletedAtUTC = previousPlanCompletedAtUTC
		}
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusServiceUnavailable, taskCompleteAuditUnavailableResponse())
		return
	}

	server.writeJSON(writer, http.StatusOK, CompleteTaskLeaseResponse{
		Status: ResponseStatusSuccess,
	})
}

// --- POST /v1/task/result ---

func (server *Server) handleTaskPlanResult(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityTaskPlanRead) {
		return
	}

	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxTaskPlanBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var resultRequest TaskPlanResultRequest
	if err := decodeJSONBytes(requestBodyBytes, &resultRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, TaskPlanResultResponse{
			Status: ResponseStatusDenied,
		})
		return
	}

	server.taskPlansMu.Lock()

	plan, found := server.taskPlans[resultRequest.PlanID]
	if !found {
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusNotFound, TaskPlanResultResponse{
			PlanID: resultRequest.PlanID,
			Status: ResponseStatusDenied,
		})
		return
	}
	if plan.SessionID != tokenClaims.ControlSessionID {
		server.taskPlansMu.Unlock()
		server.writeJSON(writer, http.StatusNotFound, TaskPlanResultResponse{
			PlanID: resultRequest.PlanID,
			Status: ResponseStatusDenied,
		})
		return
	}

	response := TaskPlanResultResponse{
		PlanID:   plan.PlanID,
		Status:   plan.State,
		GoalText: plan.GoalText,
	}

	// Populate step result from execution record if available.
	if plan.ExecutionID != "" {
		execution, execFound := server.taskExecutions[plan.ExecutionID]
		if execFound && execution.State == "succeeded" {
			response.StepResult = &TaskStepResult{
				StepIndex:    0,
				Capability:   execution.Capability,
				ProviderName: "echo",
				OutputData:   execution.ProviderOutput,
				OutputHash:   execution.OutputHash,
			}
			response.ArtifactRef = execution.ArtifactRef
		}
	}

	server.taskPlansMu.Unlock()

	server.writeJSON(writer, http.StatusOK, response)
}
