package loopgate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"morph/internal/ledger"
)

func submitTaskPlanForTest(t *testing.T, client *Client, goalText string) SubmitTaskPlanResponse {
	t.Helper()

	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	steps := []TaskPlanStep{
		{StepIndex: 0, Capability: "echo.generate_summary", Arguments: map[string]string{"input_text": "test input"}},
	}
	requestBody := SubmitTaskPlanRequest{
		GoalText:      goalText,
		Steps:         steps,
		CanonicalHash: computeCanonicalHash(goalText, steps),
	}

	var response SubmitTaskPlanResponse
	if err := client.doJSONWithHeaders(context.Background(), httpMethodPost, "/v1/task/plan", client.capabilityToken, requestBody, &response, nil); err != nil {
		t.Fatalf("submit task plan: %v", err)
	}
	return response
}

func requestTaskLeaseForTest(t *testing.T, client *Client, planID string, goalText string) RequestTaskLeaseResponse {
	t.Helper()

	steps := []TaskPlanStep{
		{StepIndex: 0, Capability: "echo.generate_summary", Arguments: map[string]string{"input_text": "test input"}},
	}
	requestBody := RequestTaskLeaseRequest{
		PlanID:    planID,
		StepIndex: 0,
		PlanHash:  computeCanonicalHash(goalText, steps),
	}

	var response RequestTaskLeaseResponse
	if err := client.doJSONWithHeaders(context.Background(), httpMethodPost, "/v1/task/lease", client.capabilityToken, requestBody, &response, nil); err != nil {
		t.Fatalf("request task lease: %v", err)
	}
	return response
}

func executeTaskLeaseForTest(t *testing.T, client *Client, leaseID string, morphlingID string) ExecuteTaskLeaseResponse {
	t.Helper()

	requestBody := ExecuteTaskLeaseRequest{
		LeaseID:     leaseID,
		MorphlingID: morphlingID,
	}
	var response ExecuteTaskLeaseResponse
	if err := client.doJSONWithHeaders(context.Background(), httpMethodPost, "/v1/task/execute", client.capabilityToken, requestBody, &response, nil); err != nil {
		t.Fatalf("execute task lease: %v", err)
	}
	return response
}

func completeTaskLeaseForTest(t *testing.T, client *Client, leaseID string, morphlingID string) CompleteTaskLeaseResponse {
	t.Helper()

	requestBody := CompleteTaskLeaseRequest{
		LeaseID:     leaseID,
		MorphlingID: morphlingID,
	}
	var response CompleteTaskLeaseResponse
	if err := client.doJSONWithHeaders(context.Background(), httpMethodPost, "/v1/task/complete", client.capabilityToken, requestBody, &response, nil); err != nil {
		t.Fatalf("complete task lease: %v", err)
	}
	return response
}

const httpMethodPost = "POST"

func TestTaskPlanSubmitFailsClosedWhenAuditUnavailable(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	originalAppend := server.appendAuditEvent
	server.appendAuditEvent = func(ledgerPath string, auditEvent ledger.Event) error {
		if auditEvent.Type == "task.plan.validated" {
			return errors.New("forced task plan audit append failure")
		}
		return originalAppend(ledgerPath, auditEvent)
	}

	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	steps := []TaskPlanStep{
		{StepIndex: 0, Capability: "echo.generate_summary", Arguments: map[string]string{"input_text": "test input"}},
	}
	requestBody := SubmitTaskPlanRequest{
		GoalText:      "task plan audit failure",
		Steps:         steps,
		CanonicalHash: computeCanonicalHash("task plan audit failure", steps),
	}
	var response SubmitTaskPlanResponse
	err := client.doJSONWithHeaders(context.Background(), httpMethodPost, "/v1/task/plan", client.capabilityToken, requestBody, &response, nil)
	if err == nil || !strings.Contains(err.Error(), DenialCodeAuditUnavailable) {
		t.Fatalf("expected audit_unavailable error, got %v", err)
	}
	if len(server.taskPlans) != 0 {
		t.Fatalf("expected no persisted task plans after audit failure, got %#v", server.taskPlans)
	}
}

func TestTaskLeaseIssueFailsClosedWhenAuditUnavailable(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	planResponse := submitTaskPlanForTest(t, client, "lease audit failure")

	originalAppend := server.appendAuditEvent
	server.appendAuditEvent = func(ledgerPath string, auditEvent ledger.Event) error {
		if auditEvent.Type == "task.lease.issued" {
			return errors.New("forced task lease audit append failure")
		}
		return originalAppend(ledgerPath, auditEvent)
	}

	steps := []TaskPlanStep{
		{StepIndex: 0, Capability: "echo.generate_summary", Arguments: map[string]string{"input_text": "test input"}},
	}
	requestBody := RequestTaskLeaseRequest{
		PlanID:    planResponse.PlanID,
		StepIndex: 0,
		PlanHash:  computeCanonicalHash("lease audit failure", steps),
	}
	var response RequestTaskLeaseResponse
	err := client.doJSONWithHeaders(context.Background(), httpMethodPost, "/v1/task/lease", client.capabilityToken, requestBody, &response, nil)
	if err == nil || !strings.Contains(err.Error(), DenialCodeAuditUnavailable) {
		t.Fatalf("expected audit_unavailable error, got %v", err)
	}
	if len(server.taskLeases) != 0 {
		t.Fatalf("expected no persisted task leases after audit failure, got %#v", server.taskLeases)
	}
	planRecord := server.taskPlans[planResponse.PlanID]
	if planRecord.State != taskPlanStateValidated || planRecord.LeaseID != "" {
		t.Fatalf("expected validated plan with no lease after audit failure, got %#v", planRecord)
	}
}

func TestTaskExecuteFailsClosedWhenAuditUnavailable(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	planResponse := submitTaskPlanForTest(t, client, "execute audit failure")
	leaseResponse := requestTaskLeaseForTest(t, client, planResponse.PlanID, "execute audit failure")
	internalLease := server.taskLeases[leaseResponse.LeaseID]

	originalAppend := server.appendAuditEvent
	server.appendAuditEvent = func(ledgerPath string, auditEvent ledger.Event) error {
		if auditEvent.Type == "task.step.executed" {
			return errors.New("forced task execute audit append failure")
		}
		return originalAppend(ledgerPath, auditEvent)
	}

	requestBody := ExecuteTaskLeaseRequest{
		LeaseID:     leaseResponse.LeaseID,
		MorphlingID: leaseResponse.MorphlingID,
	}
	var response ExecuteTaskLeaseResponse
	err := client.doJSONWithHeaders(context.Background(), httpMethodPost, "/v1/task/execute", client.capabilityToken, requestBody, &response, nil)
	if err == nil || !strings.Contains(err.Error(), DenialCodeAuditUnavailable) {
		t.Fatalf("expected audit_unavailable error, got %v", err)
	}

	leaseRecord := server.taskLeases[leaseResponse.LeaseID]
	if leaseRecord.State != taskLeaseStateIssued {
		t.Fatalf("expected issued lease after audit rollback, got %#v", leaseRecord)
	}
	planRecord := server.taskPlans[planResponse.PlanID]
	if planRecord.State != taskPlanStateLeaseIssued || planRecord.ExecutionID != "" {
		t.Fatalf("expected lease_issued plan with cleared execution after audit rollback, got %#v", planRecord)
	}
	if len(server.taskExecutions) != 0 {
		t.Fatalf("expected no persisted task executions after audit failure, got %#v", server.taskExecutions)
	}
	resultArtifactPath := filepath.Join(internalLease.StagingDir, "result.json")
	if _, statErr := os.Stat(resultArtifactPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected staged result removal after audit rollback, stat err=%v", statErr)
	}
}

func TestTaskCompleteFailsClosedWhenAuditUnavailable(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	planResponse := submitTaskPlanForTest(t, client, "complete audit failure")
	leaseResponse := requestTaskLeaseForTest(t, client, planResponse.PlanID, "complete audit failure")
	_ = executeTaskLeaseForTest(t, client, leaseResponse.LeaseID, leaseResponse.MorphlingID)

	originalAppend := server.appendAuditEvent
	server.appendAuditEvent = func(ledgerPath string, auditEvent ledger.Event) error {
		if auditEvent.Type == "task.lease.completed" {
			return errors.New("forced task complete audit append failure")
		}
		return originalAppend(ledgerPath, auditEvent)
	}

	requestBody := CompleteTaskLeaseRequest{
		LeaseID:     leaseResponse.LeaseID,
		MorphlingID: leaseResponse.MorphlingID,
	}
	var response CompleteTaskLeaseResponse
	err := client.doJSONWithHeaders(context.Background(), httpMethodPost, "/v1/task/complete", client.capabilityToken, requestBody, &response, nil)
	if err == nil || !strings.Contains(err.Error(), DenialCodeAuditUnavailable) {
		t.Fatalf("expected audit_unavailable error, got %v", err)
	}

	leaseRecord := server.taskLeases[leaseResponse.LeaseID]
	if leaseRecord.State != taskLeaseStateExecuting {
		t.Fatalf("expected executing lease after audit rollback, got %#v", leaseRecord)
	}
	planRecord := server.taskPlans[planResponse.PlanID]
	if planRecord.State != taskPlanStateExecuting || planRecord.CompletedAtUTC != nil {
		t.Fatalf("expected executing plan with no completion timestamp after audit rollback, got %#v", planRecord)
	}
}
