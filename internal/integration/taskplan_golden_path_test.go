package integration_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"morph/internal/loopgate"
)

// TestTaskPlanGoldenPath exercises the full TaskPlan → validation → lease →
// mediated execution → staged result → completion flow.
//
// This is a minimal vertical slice proving:
//   - Plan submission and validation against capability policy
//   - Lease issuance binding a logical morphling identity to a plan step
//   - Mediated capability execution (morphling → Loopgate → echo provider)
//   - Loopgate stages provider output (morphling output is untrusted)
//   - Lease finalization via /v1/task/complete
//   - Result retrieval
//   - Audit chain integrity
//
// NOT proven by this test:
//   - Morphling process isolation (morphling is a logical identity)
//   - Real external providers (echo is a local fake; in-tree MCP removed — ADR 0010)
//   - Multi-step plans
//   - Durable persistence
func TestTaskPlanGoldenPath(t *testing.T) {
	harness := newLoopgateHarness(t, integrationPolicyYAML(true))
	status := harness.waitForStatus(t)
	credentials := harness.openSession(t, "integration-actor", "integration-taskplan", capabilityNames(status.Capabilities))

	// Step 1: Compute canonical hash and submit plan.
	steps := []loopgate.TaskPlanStep{
		{
			StepIndex:  0,
			Capability: "echo.generate_summary",
			Arguments:  map[string]string{"input_text": "Hello from golden path test"},
		},
	}
	planRequest := loopgate.SubmitTaskPlanRequest{
		GoalText:      "Summarize test input",
		Steps:         steps,
		CanonicalHash: computeTaskPlanHash(t, "Summarize test input", steps),
	}

	planBody := mustJSON(t, planRequest)
	planStatusCode, planResponseBody := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/plan", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), "plan-nonce-1", planBody,
	)
	if planStatusCode != http.StatusOK {
		t.Fatalf("plan submit failed: status=%d body=%s", planStatusCode, string(planResponseBody))
	}
	var planResponse loopgate.SubmitTaskPlanResponse
	decodeJSON(t, planResponseBody, &planResponse)
	if planResponse.Status != "validated" {
		t.Fatalf("expected plan validated, got %s (denial: %s)", planResponse.Status, planResponse.DenialReason)
	}
	if planResponse.PlanID == "" {
		t.Fatal("expected non-empty plan_id")
	}

	// Step 2: Request lease.
	leaseRequest := loopgate.RequestTaskLeaseRequest{
		PlanID:    planResponse.PlanID,
		StepIndex: 0,
		PlanHash:  planRequest.CanonicalHash,
	}
	leaseBody := mustJSON(t, leaseRequest)
	leaseStatusCode, leaseResponseBody := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/lease", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), "lease-nonce-1", leaseBody,
	)
	if leaseStatusCode != http.StatusOK {
		t.Fatalf("lease request failed: status=%d body=%s", leaseStatusCode, string(leaseResponseBody))
	}
	var leaseResponse loopgate.RequestTaskLeaseResponse
	decodeJSON(t, leaseResponseBody, &leaseResponse)
	if leaseResponse.Status != "issued" {
		t.Fatalf("expected lease issued, got %s (denial: %s)", leaseResponse.Status, leaseResponse.DenialReason)
	}
	if leaseResponse.LeaseID == "" || leaseResponse.MorphlingID == "" {
		t.Fatalf("expected non-empty lease_id and morphling_id, got lease=%q morphling=%q",
			leaseResponse.LeaseID, leaseResponse.MorphlingID)
	}
	if leaseResponse.Capability != "echo.generate_summary" {
		t.Fatalf("expected capability echo.generate_summary, got %s", leaseResponse.Capability)
	}
	if !strings.HasPrefix(leaseResponse.StagingDir, "taskplan://staging/") {
		t.Fatalf("expected opaque staging_dir ref, got %q", leaseResponse.StagingDir)
	}
	if strings.Contains(leaseResponse.StagingDir, harness.repoRoot) {
		t.Fatalf("staging_dir leaked repo path: %q", leaseResponse.StagingDir)
	}

	// Step 3: Morphling executes mediated capability call.
	// The morphling does NOT supply capability or arguments — those are bound by the lease.
	executeRequest := loopgate.ExecuteTaskLeaseRequest{
		LeaseID:     leaseResponse.LeaseID,
		MorphlingID: leaseResponse.MorphlingID,
	}
	executeBody := mustJSON(t, executeRequest)
	executeStatusCode, executeResponseBody := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/execute", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), "execute-nonce-1", executeBody,
	)
	if executeStatusCode != http.StatusOK {
		t.Fatalf("execute failed: status=%d body=%s", executeStatusCode, string(executeResponseBody))
	}
	var executeResponse loopgate.ExecuteTaskLeaseResponse
	decodeJSON(t, executeResponseBody, &executeResponse)
	if executeResponse.Status != loopgate.ResponseStatusSuccess {
		t.Fatalf("expected execution success, got %s (denial: %s)", executeResponse.Status, executeResponse.DenialReason)
	}
	if executeResponse.StepResult == nil {
		t.Fatal("expected non-nil step_result")
	}
	if executeResponse.StepResult.OutputHash == "" {
		t.Fatal("expected non-empty output_hash")
	}

	// Verify the echo provider output.
	var echoOutput loopgate.EchoProviderOutput
	if err := json.Unmarshal(executeResponse.StepResult.OutputData, &echoOutput); err != nil {
		t.Fatalf("unmarshal echo output: %v", err)
	}
	if echoOutput.InputLength != len("Hello from golden path test") {
		t.Fatalf("expected input_length %d, got %d", len("Hello from golden path test"), echoOutput.InputLength)
	}
	if echoOutput.Provider != "echo" {
		t.Fatalf("expected provider 'echo', got %s", echoOutput.Provider)
	}

	// Step 4: Morphling completes the lease.
	completeRequest := loopgate.CompleteTaskLeaseRequest{
		LeaseID:     leaseResponse.LeaseID,
		MorphlingID: leaseResponse.MorphlingID,
	}
	completeBody := mustJSON(t, completeRequest)
	completeStatusCode, completeResponseBody := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/complete", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), "complete-nonce-1", completeBody,
	)
	if completeStatusCode != http.StatusOK {
		t.Fatalf("complete failed: status=%d body=%s", completeStatusCode, string(completeResponseBody))
	}
	var completeResponse loopgate.CompleteTaskLeaseResponse
	decodeJSON(t, completeResponseBody, &completeResponse)
	if completeResponse.Status != loopgate.ResponseStatusSuccess {
		t.Fatalf("expected complete success, got %s (denial: %s)", completeResponse.Status, completeResponse.DenialReason)
	}

	// Step 5: Query result.
	resultRequest := loopgate.TaskPlanResultRequest{PlanID: planResponse.PlanID}
	resultBody := mustJSON(t, resultRequest)
	resultStatusCode, resultResponseBody := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/result", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), "result-nonce-1", resultBody,
	)
	if resultStatusCode != http.StatusOK {
		t.Fatalf("result query failed: status=%d body=%s", resultStatusCode, string(resultResponseBody))
	}
	var resultResponse loopgate.TaskPlanResultResponse
	decodeJSON(t, resultResponseBody, &resultResponse)
	if resultResponse.Status != "completed" {
		t.Fatalf("expected plan completed, got %s", resultResponse.Status)
	}
	if resultResponse.StepResult == nil {
		t.Fatal("expected non-nil step_result in result")
	}
	if resultResponse.ArtifactRef == "" {
		t.Fatal("expected non-empty artifact_ref")
	}
	if !strings.HasPrefix(resultResponse.ArtifactRef, "taskplan://artifacts/") {
		t.Fatalf("expected opaque artifact_ref, got %q", resultResponse.ArtifactRef)
	}
	if strings.Contains(resultResponse.ArtifactRef, harness.repoRoot) {
		t.Fatalf("artifact_ref leaked repo path: %q", resultResponse.ArtifactRef)
	}

	// Step 6: Verify audit chain integrity.
	harness.verifyAuditChain(t)

	// Step 7: Verify audit events.
	events, _ := harness.readAuditEvents(t)
	if _, found := findAuditEvent(events, "task.plan.validated", ""); !found {
		t.Fatal("expected task.plan.validated audit event")
	}
	if _, found := findAuditEvent(events, "task.lease.issued", ""); !found {
		t.Fatal("expected task.lease.issued audit event")
	}
	if _, found := findAuditEvent(events, "task.step.executed", ""); !found {
		t.Fatal("expected task.step.executed audit event")
	}
	if _, found := findAuditEvent(events, "task.lease.completed", ""); !found {
		t.Fatal("expected task.lease.completed audit event")
	}
}

// --- Boundary tests ---

func TestTaskPlanUnknownCapabilityDenied(t *testing.T) {
	harness := newLoopgateHarness(t, integrationPolicyYAML(true))
	status := harness.waitForStatus(t)
	credentials := harness.openSession(t, "integration-actor", "integration-taskplan-deny-cap", capabilityNames(status.Capabilities))

	steps := []loopgate.TaskPlanStep{
		{StepIndex: 0, Capability: "unknown.capability", Arguments: map[string]string{}},
	}
	planRequest := loopgate.SubmitTaskPlanRequest{
		GoalText:      "Test unknown capability",
		Steps:         steps,
		CanonicalHash: computeTaskPlanHash(t, "Test unknown capability", steps),
	}
	body := mustJSON(t, planRequest)
	statusCode, responseBody := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/plan", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), "deny-cap-nonce", body,
	)
	if statusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", statusCode, string(responseBody))
	}
	var response loopgate.SubmitTaskPlanResponse
	decodeJSON(t, responseBody, &response)
	if response.DenialCode != loopgate.DenialCodeTaskPlanInvalid {
		t.Fatalf("expected denial code %s, got %s", loopgate.DenialCodeTaskPlanInvalid, response.DenialCode)
	}
}

func TestTaskPlanHashMismatchDenied(t *testing.T) {
	harness := newLoopgateHarness(t, integrationPolicyYAML(true))
	status := harness.waitForStatus(t)
	credentials := harness.openSession(t, "integration-actor", "integration-taskplan-hash", capabilityNames(status.Capabilities))

	steps := []loopgate.TaskPlanStep{
		{StepIndex: 0, Capability: "echo.generate_summary", Arguments: map[string]string{"input_text": "test"}},
	}
	planRequest := loopgate.SubmitTaskPlanRequest{
		GoalText:      "Test hash mismatch",
		Steps:         steps,
		CanonicalHash: "0000000000000000000000000000000000000000000000000000000000000000",
	}
	body := mustJSON(t, planRequest)
	statusCode, responseBody := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/plan", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), "hash-mismatch-nonce", body,
	)
	if statusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", statusCode, string(responseBody))
	}
	var response loopgate.SubmitTaskPlanResponse
	decodeJSON(t, responseBody, &response)
	if response.DenialCode != loopgate.DenialCodeTaskPlanHashMismatch {
		t.Fatalf("expected denial code %s, got %s", loopgate.DenialCodeTaskPlanHashMismatch, response.DenialCode)
	}
}

func TestTaskLeaseWrongPlanHashDenied(t *testing.T) {
	harness := newLoopgateHarness(t, integrationPolicyYAML(true))
	status := harness.waitForStatus(t)
	credentials := harness.openSession(t, "integration-actor", "integration-lease-hash", capabilityNames(status.Capabilities))

	planID := submitValidatedPlan(t, harness, credentials)

	leaseRequest := loopgate.RequestTaskLeaseRequest{
		PlanID:    planID,
		StepIndex: 0,
		PlanHash:  "0000000000000000000000000000000000000000000000000000000000000000",
	}
	body := mustJSON(t, leaseRequest)
	statusCode, responseBody := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/lease", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), "lease-hash-nonce", body,
	)
	if statusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", statusCode, string(responseBody))
	}
	var response loopgate.RequestTaskLeaseResponse
	decodeJSON(t, responseBody, &response)
	if response.DenialCode != loopgate.DenialCodeTaskPlanHashMismatch {
		t.Fatalf("expected denial code %s, got %s", loopgate.DenialCodeTaskPlanHashMismatch, response.DenialCode)
	}
}

func TestTaskPlanCrossSessionAccessDenied(t *testing.T) {
	harness := newLoopgateHarness(t, integrationPolicyYAML(true))
	status := harness.waitForStatus(t)
	ownerCredentials := harness.openSession(t, "integration-owner", "integration-owner-taskplan", capabilityNames(status.Capabilities))
	time.Sleep(600 * time.Millisecond)
	otherCredentials := harness.openSession(t, "integration-other", "integration-other-taskplan", capabilityNames(status.Capabilities))

	planID, canonicalHash := submitValidatedPlanWithHash(t, harness, ownerCredentials)

	leaseRequest := loopgate.RequestTaskLeaseRequest{
		PlanID:    planID,
		StepIndex: 0,
		PlanHash:  canonicalHash,
	}
	leaseBody := mustJSON(t, leaseRequest)
	statusCode, responseBody := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/lease", otherCredentials,
		time.Now().UTC().Format(time.RFC3339Nano), randomTestNonce(t), leaseBody,
	)
	if statusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-session lease request, got %d: %s", statusCode, string(responseBody))
	}

	_, ownerLeaseID, ownerMorphlingID := submitPlanAndLease(t, harness, ownerCredentials)

	executeRequest := loopgate.ExecuteTaskLeaseRequest{
		LeaseID:     ownerLeaseID,
		MorphlingID: ownerMorphlingID,
	}
	executeBody := mustJSON(t, executeRequest)
	statusCode, responseBody = harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/execute", otherCredentials,
		time.Now().UTC().Format(time.RFC3339Nano), randomTestNonce(t), executeBody,
	)
	if statusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-session execute request, got %d: %s", statusCode, string(responseBody))
	}

	resultRequest := loopgate.TaskPlanResultRequest{PlanID: planID}
	resultBody := mustJSON(t, resultRequest)
	statusCode, responseBody = harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/result", otherCredentials,
		time.Now().UTC().Format(time.RFC3339Nano), randomTestNonce(t), resultBody,
	)
	if statusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-session result request, got %d: %s", statusCode, string(responseBody))
	}
}

func TestTaskLeaseNonExistentPlanDenied(t *testing.T) {
	harness := newLoopgateHarness(t, integrationPolicyYAML(true))
	status := harness.waitForStatus(t)
	credentials := harness.openSession(t, "integration-actor", "integration-lease-404", capabilityNames(status.Capabilities))

	leaseRequest := loopgate.RequestTaskLeaseRequest{
		PlanID:    "nonexistent-plan-id",
		StepIndex: 0,
		PlanHash:  "anything",
	}
	body := mustJSON(t, leaseRequest)
	statusCode, responseBody := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/lease", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), "lease-404-nonce", body,
	)
	if statusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", statusCode, string(responseBody))
	}
	var response loopgate.RequestTaskLeaseResponse
	decodeJSON(t, responseBody, &response)
	if response.DenialCode != loopgate.DenialCodeTaskPlanNotFound {
		t.Fatalf("expected denial code %s, got %s", loopgate.DenialCodeTaskPlanNotFound, response.DenialCode)
	}
}

func TestTaskLeaseAlreadyLeasedPlanDenied(t *testing.T) {
	harness := newLoopgateHarness(t, integrationPolicyYAML(true))
	status := harness.waitForStatus(t)
	credentials := harness.openSession(t, "integration-actor", "integration-double-lease", capabilityNames(status.Capabilities))

	planID, canonicalHash := submitValidatedPlanWithHash(t, harness, credentials)

	// First lease succeeds.
	leaseRequest := loopgate.RequestTaskLeaseRequest{
		PlanID:    planID,
		StepIndex: 0,
		PlanHash:  canonicalHash,
	}
	body := mustJSON(t, leaseRequest)
	statusCode, _ := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/lease", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), "double-lease-1", body,
	)
	if statusCode != http.StatusOK {
		t.Fatalf("first lease request failed: status=%d", statusCode)
	}

	// Second lease denied (plan is now in lease_issued state).
	statusCode, responseBody := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/lease", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), "double-lease-2", body,
	)
	if statusCode != http.StatusConflict {
		t.Fatalf("expected 409 for double lease, got %d: %s", statusCode, string(responseBody))
	}
	var response loopgate.RequestTaskLeaseResponse
	decodeJSON(t, responseBody, &response)
	if response.DenialCode != loopgate.DenialCodeTaskPlanStateInvalid {
		t.Fatalf("expected denial code %s, got %s", loopgate.DenialCodeTaskPlanStateInvalid, response.DenialCode)
	}
}

func TestTaskExecuteWrongMorphlingIDDenied(t *testing.T) {
	harness := newLoopgateHarness(t, integrationPolicyYAML(true))
	status := harness.waitForStatus(t)
	credentials := harness.openSession(t, "integration-actor", "integration-wrong-morphling", capabilityNames(status.Capabilities))

	_, leaseID, _ := submitPlanAndLease(t, harness, credentials)

	executeRequest := loopgate.ExecuteTaskLeaseRequest{
		LeaseID:     leaseID,
		MorphlingID: "wrong-morphling-id",
	}
	body := mustJSON(t, executeRequest)
	statusCode, responseBody := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/execute", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), "wrong-morphling-nonce", body,
	)
	if statusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", statusCode, string(responseBody))
	}
	var response loopgate.ExecuteTaskLeaseResponse
	decodeJSON(t, responseBody, &response)
	if response.DenialCode != loopgate.DenialCodeTaskLeaseMorphlingMismatch {
		t.Fatalf("expected denial code %s, got %s", loopgate.DenialCodeTaskLeaseMorphlingMismatch, response.DenialCode)
	}
}

func TestTaskDoubleExecuteDenied(t *testing.T) {
	harness := newLoopgateHarness(t, integrationPolicyYAML(true))
	status := harness.waitForStatus(t)
	credentials := harness.openSession(t, "integration-actor", "integration-double-execute", capabilityNames(status.Capabilities))

	_, leaseID, morphlingID := submitPlanAndLease(t, harness, credentials)

	executeRequest := loopgate.ExecuteTaskLeaseRequest{
		LeaseID:     leaseID,
		MorphlingID: morphlingID,
	}
	body := mustJSON(t, executeRequest)

	// First execute succeeds.
	statusCode, _ := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/execute", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), "double-exec-1", body,
	)
	if statusCode != http.StatusOK {
		t.Fatalf("first execute failed: status=%d", statusCode)
	}

	// Second execute denied (lease is now in executing state, not issued).
	statusCode, responseBody := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/execute", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), "double-exec-2", body,
	)
	if statusCode != http.StatusConflict {
		t.Fatalf("expected 409 for double execute, got %d: %s", statusCode, string(responseBody))
	}
	var response loopgate.ExecuteTaskLeaseResponse
	decodeJSON(t, responseBody, &response)
	if response.DenialCode != loopgate.DenialCodeTaskLeaseConsumed {
		t.Fatalf("expected denial code %s, got %s", loopgate.DenialCodeTaskLeaseConsumed, response.DenialCode)
	}
}

func TestTaskDoubleCompleteDenied(t *testing.T) {
	harness := newLoopgateHarness(t, integrationPolicyYAML(true))
	status := harness.waitForStatus(t)
	credentials := harness.openSession(t, "integration-actor", "integration-double-complete", capabilityNames(status.Capabilities))

	_, leaseID, morphlingID := submitPlanAndLease(t, harness, credentials)

	// Execute.
	executeRequest := loopgate.ExecuteTaskLeaseRequest{LeaseID: leaseID, MorphlingID: morphlingID}
	executeBody := mustJSON(t, executeRequest)
	statusCode, _ := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/execute", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), "dc-exec", executeBody,
	)
	if statusCode != http.StatusOK {
		t.Fatalf("execute failed: status=%d", statusCode)
	}

	// First complete succeeds.
	completeRequest := loopgate.CompleteTaskLeaseRequest{LeaseID: leaseID, MorphlingID: morphlingID}
	completeBody := mustJSON(t, completeRequest)
	statusCode, _ = harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/complete", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), "dc-complete-1", completeBody,
	)
	if statusCode != http.StatusOK {
		t.Fatalf("first complete failed: status=%d", statusCode)
	}

	// Second complete denied.
	statusCode, responseBody := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/complete", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), "dc-complete-2", completeBody,
	)
	if statusCode != http.StatusConflict {
		t.Fatalf("expected 409 for double complete, got %d: %s", statusCode, string(responseBody))
	}
	var response loopgate.CompleteTaskLeaseResponse
	decodeJSON(t, responseBody, &response)
	if response.DenialCode != loopgate.DenialCodeTaskLeaseConsumed {
		t.Fatalf("expected denial code %s, got %s", loopgate.DenialCodeTaskLeaseConsumed, response.DenialCode)
	}
}

// --- Helpers ---

func computeTaskPlanHash(t *testing.T, goalText string, steps []loopgate.TaskPlanStep) string {
	t.Helper()

	type canonicalStep struct {
		StepIndex  int               `json:"step_index"`
		Capability string            `json:"capability"`
		Arguments  map[string]string `json:"arguments"`
	}

	sortedSteps := make([]canonicalStep, len(steps))
	for i, step := range steps {
		args := make(map[string]string, len(step.Arguments))
		for k, v := range step.Arguments {
			args[k] = v
		}
		sortedSteps[i] = canonicalStep{
			StepIndex:  step.StepIndex,
			Capability: step.Capability,
			Arguments:  args,
		}
	}

	payload := struct {
		GoalText string          `json:"goal_text"`
		Steps    []canonicalStep `json:"steps"`
	}{
		GoalText: goalText,
		Steps:    sortedSteps,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal canonical hash payload: %v", err)
	}

	hash := sha256.Sum256(payloadBytes)
	return hex.EncodeToString(hash[:])
}

func submitValidatedPlan(t *testing.T, harness *loopgateHarness, credentials sessionCredentials) string {
	t.Helper()
	planID, _ := submitValidatedPlanWithHash(t, harness, credentials)
	return planID
}

func submitValidatedPlanWithHash(t *testing.T, harness *loopgateHarness, credentials sessionCredentials) (string, string) {
	t.Helper()

	steps := []loopgate.TaskPlanStep{
		{StepIndex: 0, Capability: "echo.generate_summary", Arguments: map[string]string{"input_text": "test input"}},
	}
	canonicalHash := computeTaskPlanHash(t, "Test goal", steps)
	planRequest := loopgate.SubmitTaskPlanRequest{
		GoalText:      "Test goal",
		Steps:         steps,
		CanonicalHash: canonicalHash,
	}
	body := mustJSON(t, planRequest)
	statusCode, responseBody := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/plan", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), randomTestNonce(t), body,
	)
	if statusCode != http.StatusOK {
		t.Fatalf("plan submit failed: status=%d body=%s", statusCode, string(responseBody))
	}
	var response loopgate.SubmitTaskPlanResponse
	decodeJSON(t, responseBody, &response)
	if response.Status != "validated" {
		t.Fatalf("expected validated, got %s", response.Status)
	}
	return response.PlanID, canonicalHash
}

func submitPlanAndLease(t *testing.T, harness *loopgateHarness, credentials sessionCredentials) (string, string, string) {
	t.Helper()

	planID, canonicalHash := submitValidatedPlanWithHash(t, harness, credentials)

	leaseRequest := loopgate.RequestTaskLeaseRequest{
		PlanID:    planID,
		StepIndex: 0,
		PlanHash:  canonicalHash,
	}
	body := mustJSON(t, leaseRequest)
	statusCode, responseBody := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/lease", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), randomTestNonce(t), body,
	)
	if statusCode != http.StatusOK {
		t.Fatalf("lease request failed: status=%d body=%s", statusCode, string(responseBody))
	}
	var response loopgate.RequestTaskLeaseResponse
	decodeJSON(t, responseBody, &response)
	return planID, response.LeaseID, response.MorphlingID
}

func randomTestNonce(t *testing.T) string {
	t.Helper()
	// Use a unique nonce per call to avoid replay detection.
	return time.Now().UTC().Format(time.RFC3339Nano) + "-" + t.Name()
}
