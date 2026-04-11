package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"morph/internal/loopgate"
)

// =============================================================================
// Integration tests for the morphling runner as a separate local process.
//
// These tests prove:
//   - A morphling runner (separate goroutine using RunTaskPlanMorphling) can
//     consume a lease, call /execute through Loopgate mediation, finalize
//     via /complete, and exit cleanly
//   - Expired leases are rejected at execute time
//   - Duplicate completion is rejected
//   - Crash recovery: if the runner crashes after /execute but before /complete,
//     the plan remains in executing state and the lease is not consumed
//
// These tests do NOT prove:
//   - Real process isolation (runner shares address space in goroutine tests)
//   - The subprocess binary test (TestMorphlingRunnerSubprocess) proves the
//     binary can be built and run as a separate OS process
// =============================================================================

// TestMorphlingRunnerGoldenPath exercises the full runner flow in a goroutine.
func TestMorphlingRunnerGoldenPath(t *testing.T) {
	harness := newLoopgateHarness(t, integrationPolicyYAML(true))
	status := harness.waitForStatus(t)
	credentials := harness.openSession(t, "runner-actor", "runner-golden", advertisedSessionCapabilityNames(status))

	planID, leaseID, morphlingID := submitPlanAndLease(t, harness, credentials)

	// Run the morphling runner in a goroutine (simulating a separate process).
	config := makeRunnerConfig(harness, credentials, leaseID, morphlingID, planID)
	result := loopgate.RunTaskPlanMorphling(context.Background(), config)
	if result.Status != "completed" {
		t.Fatalf("expected runner completed, got %s (error: %s)", result.Status, result.ErrorReason)
	}
	if result.StepResult == nil {
		t.Fatal("expected non-nil step_result")
	}
	if result.StepResult.OutputHash == "" {
		t.Fatal("expected non-empty output_hash")
	}

	// Verify the echo provider output via step result.
	var echoOutput loopgate.EchoProviderOutput
	if err := json.Unmarshal(result.StepResult.OutputData, &echoOutput); err != nil {
		t.Fatalf("unmarshal echo output: %v", err)
	}
	if echoOutput.Provider != "echo" {
		t.Fatalf("expected provider 'echo', got %s", echoOutput.Provider)
	}

	// Verify plan completed via result endpoint.
	resultRequest := loopgate.TaskPlanResultRequest{PlanID: planID}
	resultBody := mustJSON(t, resultRequest)
	resultStatusCode, resultResponseBody := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/result", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), randomTestNonce(t), resultBody,
	)
	if resultStatusCode != http.StatusOK {
		t.Fatalf("result query failed: status=%d body=%s", resultStatusCode, string(resultResponseBody))
	}
	var planResult loopgate.TaskPlanResultResponse
	decodeJSON(t, resultResponseBody, &planResult)
	if planResult.Status != "completed" {
		t.Fatalf("expected plan completed, got %s", planResult.Status)
	}

	// Verify audit chain integrity.
	harness.verifyAuditChain(t)
}

// TestMorphlingRunnerSubprocessBuild proves the morphling-runner binary can be
// built, accepts JSON config from stdin, and produces structured JSON output.
//
// This test validates the subprocess binary interface. The subprocess receives
// a "failed" result because capability tokens are peer-identity-bound (UID+PID)
// and the subprocess has a different PID than the session creator. This is a
// correct security constraint — the real architecture uses the morphling worker
// launch/open flow to create process-specific credentials.
//
// Full end-to-end execution is proven by TestMorphlingRunnerGoldenPath which
// uses the same RunTaskPlanMorphling function in-process.
func TestMorphlingRunnerSubprocessBuild(t *testing.T) {
	// Build the morphling-runner binary.
	binaryPath := t.TempDir() + "/morphling-runner"
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "morph/cmd/morphling-runner")
	buildCmd.Dir = findRepoRoot(t)
	buildOutput, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build morphling-runner: %v\n%s", err, string(buildOutput))
	}

	harness := newLoopgateHarness(t, integrationPolicyYAML(true))
	status := harness.waitForStatus(t)
	credentials := harness.openSession(t, "subprocess-actor", "subprocess-build", advertisedSessionCapabilityNames(status))

	_, leaseID, morphlingID := submitPlanAndLease(t, harness, credentials)

	config := makeRunnerConfig(harness, credentials, leaseID, morphlingID, "")
	configBytes, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	// Run the binary as a subprocess.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	runCmd := exec.CommandContext(ctx, binaryPath)
	runCmd.Stdin = jsonReader(t, configBytes)
	runCmd.Stderr = os.Stderr
	stdout, err := runCmd.Output()
	if err != nil {
		t.Fatalf("run morphling-runner subprocess: %v", err)
	}

	// The subprocess should produce valid JSON output (even if the execution
	// fails due to peer identity binding).
	var result loopgate.TaskPlanRunnerResult
	if err := json.Unmarshal(stdout, &result); err != nil {
		t.Fatalf("unmarshal subprocess result: %v\nstdout: %s", err, string(stdout))
	}
	// Result will be "failed" due to peer identity mismatch — this is expected.
	// The binary parsed config from stdin and produced structured JSON output.
	if result.Status != "failed" {
		t.Logf("subprocess result: status=%s (this may pass if peer identity binding is relaxed)", result.Status)
	}
	if result.Status == "" {
		t.Fatal("expected non-empty status from subprocess")
	}
}

// TestMorphlingRunnerLeaseExpired verifies that a runner cannot execute an
// expired lease. Uses SetNowForTest to advance the server clock past expiry.
func TestMorphlingRunnerLeaseExpired(t *testing.T) {
	harness := newLoopgateHarness(t, integrationPolicyYAML(true))
	status := harness.waitForStatus(t)
	credentials := harness.openSession(t, "expiry-actor", "expiry-session", advertisedSessionCapabilityNames(status))

	_, leaseID, morphlingID := submitPlanAndLease(t, harness, credentials)

	// Advance the server clock past the lease TTL (2 minutes).
	harness.server.SetNowForTest(func() time.Time {
		return time.Now().UTC().Add(3 * time.Minute)
	})

	config := makeRunnerConfig(harness, credentials, leaseID, morphlingID, "")
	result := loopgate.RunTaskPlanMorphling(context.Background(), config)
	if result.Status != "failed" {
		t.Fatalf("expected runner to fail on expired lease, got %s", result.Status)
	}
	if result.ErrorReason == "" {
		t.Fatal("expected non-empty error_reason for expired lease")
	}
}

// TestMorphlingRunnerDuplicateCompletion verifies that calling /complete twice
// is rejected. The first complete succeeds; the second returns a denial.
func TestMorphlingRunnerDuplicateCompletion(t *testing.T) {
	harness := newLoopgateHarness(t, integrationPolicyYAML(true))
	status := harness.waitForStatus(t)
	credentials := harness.openSession(t, "dup-actor", "dup-session", advertisedSessionCapabilityNames(status))

	_, leaseID, morphlingID := submitPlanAndLease(t, harness, credentials)

	// First runner completes successfully.
	config := makeRunnerConfig(harness, credentials, leaseID, morphlingID, "")
	result := loopgate.RunTaskPlanMorphling(context.Background(), config)
	if result.Status != "completed" {
		t.Fatalf("first runner should complete, got %s (error: %s)", result.Status, result.ErrorReason)
	}

	// Second attempt to complete the same lease should fail.
	// We can't use RunTaskPlanMorphling because it will try /execute first
	// and that will fail (lease is consumed). Instead, call /complete directly.
	completeRequest := loopgate.CompleteTaskLeaseRequest{
		LeaseID:     leaseID,
		MorphlingID: morphlingID,
	}
	completeBody := mustJSON(t, completeRequest)
	statusCode, responseBody := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/complete", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), randomTestNonce(t), completeBody,
	)
	if statusCode != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate complete, got %d: %s", statusCode, string(responseBody))
	}
	var completeResponse loopgate.CompleteTaskLeaseResponse
	decodeJSON(t, responseBody, &completeResponse)
	if completeResponse.DenialCode != loopgate.DenialCodeTaskLeaseConsumed {
		t.Fatalf("expected denial code %s, got %s", loopgate.DenialCodeTaskLeaseConsumed, completeResponse.DenialCode)
	}
}

// TestMorphlingRunnerCrashAfterExecute verifies crash recovery semantics:
// if the runner crashes after /execute but before /complete, the plan remains
// in executing state and the lease is in executing state (not consumed).
// A recovery process can detect this and either retry completion or fail the plan.
func TestMorphlingRunnerCrashAfterExecute(t *testing.T) {
	harness := newLoopgateHarness(t, integrationPolicyYAML(true))
	status := harness.waitForStatus(t)
	credentials := harness.openSession(t, "crash-actor", "crash-session", advertisedSessionCapabilityNames(status))

	planID, leaseID, morphlingID := submitPlanAndLease(t, harness, credentials)

	// Simulate a runner that executes but crashes before completing.
	// Call /execute directly, then do NOT call /complete.
	executeRequest := loopgate.ExecuteTaskLeaseRequest{
		LeaseID:     leaseID,
		MorphlingID: morphlingID,
	}
	executeBody := mustJSON(t, executeRequest)
	executeStatusCode, executeResponseBody := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/execute", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), randomTestNonce(t), executeBody,
	)
	if executeStatusCode != http.StatusOK {
		t.Fatalf("execute failed: status=%d body=%s", executeStatusCode, string(executeResponseBody))
	}

	// Verify plan is in executing state (not completed).
	resultRequest := loopgate.TaskPlanResultRequest{PlanID: planID}
	resultBody := mustJSON(t, resultRequest)
	resultStatusCode, resultResponseBody := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/result", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), randomTestNonce(t), resultBody,
	)
	if resultStatusCode != http.StatusOK {
		t.Fatalf("result query failed: status=%d", resultStatusCode)
	}
	var planResult loopgate.TaskPlanResultResponse
	decodeJSON(t, resultResponseBody, &planResult)
	if planResult.Status != "executing" {
		t.Fatalf("expected plan in executing state after crash, got %s", planResult.Status)
	}

	// Verify that the execution result is available (provider output was staged
	// by Loopgate during /execute, not by the runner).
	if planResult.StepResult == nil {
		t.Fatal("expected step_result to be available (Loopgate staged output during /execute)")
	}

	// Verify a second /execute call is rejected (lease is in executing state).
	executeStatusCode2, executeResponseBody2 := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/execute", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), randomTestNonce(t), executeBody,
	)
	if executeStatusCode2 != http.StatusConflict {
		t.Fatalf("expected 409 for second execute, got %d: %s", executeStatusCode2, string(executeResponseBody2))
	}

	// Recovery: a new runner (or recovery process) can still call /complete
	// with the correct morphling ID to finalize the lease.
	completeRequest := loopgate.CompleteTaskLeaseRequest{
		LeaseID:     leaseID,
		MorphlingID: morphlingID,
	}
	completeBody := mustJSON(t, completeRequest)
	completeStatusCode, completeResponseBody := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/complete", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), randomTestNonce(t), completeBody,
	)
	if completeStatusCode != http.StatusOK {
		t.Fatalf("recovery complete failed: status=%d body=%s", completeStatusCode, string(completeResponseBody))
	}

	// Now plan should be completed.
	resultStatusCode2, resultResponseBody2 := harness.doSignedJSONBytes(
		t, http.MethodPost, "/v1/task/result", credentials,
		time.Now().UTC().Format(time.RFC3339Nano), randomTestNonce(t), resultBody,
	)
	if resultStatusCode2 != http.StatusOK {
		t.Fatalf("result query failed after recovery: status=%d", resultStatusCode2)
	}
	var planResult2 loopgate.TaskPlanResultResponse
	decodeJSON(t, resultResponseBody2, &planResult2)
	if planResult2.Status != "completed" {
		t.Fatalf("expected plan completed after recovery, got %s", planResult2.Status)
	}
}

// TestMorphlingRunnerConcurrentExecute verifies that two runners attempting to
// execute the same lease concurrently results in exactly one success.
func TestMorphlingRunnerConcurrentExecute(t *testing.T) {
	harness := newLoopgateHarness(t, integrationPolicyYAML(true))
	status := harness.waitForStatus(t)
	credentials := harness.openSession(t, "concurrent-actor", "concurrent-session", advertisedSessionCapabilityNames(status))

	_, leaseID, morphlingID := submitPlanAndLease(t, harness, credentials)

	config := makeRunnerConfig(harness, credentials, leaseID, morphlingID, "")

	var wg sync.WaitGroup
	results := make([]loopgate.TaskPlanRunnerResult, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index] = loopgate.RunTaskPlanMorphling(context.Background(), config)
		}(i)
	}
	wg.Wait()

	completed := 0
	failed := 0
	for _, r := range results {
		if r.Status == "completed" {
			completed++
		} else {
			failed++
		}
	}
	if completed != 1 {
		t.Fatalf("expected exactly 1 completed runner, got %d completed and %d failed", completed, failed)
	}
}

// --- Helpers ---

func findRepoRoot(t *testing.T) string {
	t.Helper()
	// The integration tests run from internal/integration/, so repo root is ../..
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// Walk up to find go.mod
	for d := dir; d != "/"; d = d[:max(1, len(d)-1)] {
		if _, err := os.Stat(d + "/go.mod"); err == nil {
			return d
		}
		// Walk up one directory component.
		for len(d) > 0 && d[len(d)-1] != '/' {
			d = d[:len(d)-1]
		}
		if len(d) > 1 {
			d = d[:len(d)-1]
		}
		if _, err := os.Stat(d + "/go.mod"); err == nil {
			return d
		}
	}
	t.Fatal("could not find repo root (go.mod)")
	return ""
}

func jsonReader(t *testing.T, data []byte) *os.File {
	t.Helper()
	// Create a pipe and write the JSON data to it.
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	go func() {
		defer writer.Close()
		_, _ = writer.Write(data)
	}()
	return reader
}

func makeRunnerConfig(harness *loopgateHarness, credentials sessionCredentials, leaseID, morphlingID, planID string) loopgate.TaskPlanRunnerConfig {
	return loopgate.TaskPlanRunnerConfig{
		SocketPath:       harness.socketPath,
		ControlSessionID: credentials.ControlSessionID,
		CapabilityToken:  credentials.CapabilityToken,
		ApprovalToken:    credentials.ApprovalToken,
		SessionMACKey:    credentials.SessionMACKey,
		SessionExpiresAt: time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339Nano),
		LeaseID:          leaseID,
		MorphlingID:      morphlingID,
		PlanID:           planID,
	}
}
