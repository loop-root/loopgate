package integration_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"morph/internal/loopgate"
)

func TestDeniedPolicyPathWriteOverRealSocketDoesNotChangeFile(t *testing.T) {
	harness := newLoopgateHarness(t, integrationPolicyYAML(true))
	status := harness.waitForStatus(t)
	credentials := harness.openSession(t, "integration-actor", "integration-denied-path", capabilityNames(status.Capabilities))

	policyPath := filepath.Join(harness.repoRoot, "core", "policy", "policy.yaml")
	originalPolicyBytes, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatalf("read original policy file: %v", err)
	}

	requestID := "req-denied-policy-path"
	requestBody := mustJSON(t, loopgate.CapabilityRequest{
		RequestID:  requestID,
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "core/policy/policy.yaml",
			"content": "malicious rewrite",
		},
	})
	statusCode, responseBody := harness.doSignedJSONBytes(
		t,
		http.MethodPost,
		"/v1/capabilities/execute",
		credentials,
		time.Now().UTC().Format(time.RFC3339Nano),
		"denied-policy-path",
		requestBody,
	)
	if statusCode != http.StatusInternalServerError {
		t.Fatalf("expected denied path request to return transport status 500, got %d: %s", statusCode, string(responseBody))
	}

	var capabilityResponse loopgate.CapabilityResponse
	decodeJSON(t, responseBody, &capabilityResponse)
	if capabilityResponse.Status != loopgate.ResponseStatusError {
		t.Fatalf("expected denied path request to return error status, got %#v", capabilityResponse)
	}
	if capabilityResponse.DenialCode != loopgate.DenialCodeExecutionFailed {
		t.Fatalf("expected denied path request to use denial code %q, got %#v", loopgate.DenialCodeExecutionFailed, capabilityResponse)
	}
	if !strings.Contains(capabilityResponse.DenialReason, "path denied") {
		t.Fatalf("expected denied path reason to mention path denial, got %#v", capabilityResponse)
	}

	currentPolicyBytes, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatalf("read current policy file: %v", err)
	}
	if string(currentPolicyBytes) != string(originalPolicyBytes) {
		t.Fatalf("expected denied path write to leave policy file unchanged")
	}

	events, _ := harness.readAuditEvents(t)
	errorEvent, found := findAuditEvent(events, "capability.error", requestID)
	if !found {
		t.Fatalf("expected capability.error audit event for request %q, got %#v", requestID, events)
	}
	if errorEvent.Session != credentials.ControlSessionID {
		t.Fatalf("expected audit event session %q, got %#v", credentials.ControlSessionID, errorEvent)
	}
	if _, found := errorEvent.Data["error"]; !found {
		t.Fatalf("expected capability.error audit data to include redacted error text: %#v", errorEvent)
	}
	if _, found := findAuditEvent(events, "capability.executed", requestID); found {
		t.Fatalf("did not expect capability.executed audit event for denied path request %q", requestID)
	}
}
