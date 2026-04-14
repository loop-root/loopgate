package integration_test

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"loopgate/internal/loopgate"
)

func TestAuditChainAndRedactionRoundTrip(t *testing.T) {
	harness := newLoopgateHarness(t, integrationPolicyYAML(true))
	status := harness.waitForStatus(t)
	credentials := harness.openSession(t, "integration-actor", "integration-audit", capabilityNames(status.Capabilities))

	allowedRequestBody := mustJSON(t, loopgate.CapabilityRequest{
		RequestID:  "req-audit-allowed",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	allowedStatusCode, allowedResponseBody := harness.doSignedJSONBytes(
		t,
		http.MethodPost,
		"/v1/capabilities/execute",
		credentials,
		time.Now().UTC().Format(time.RFC3339Nano),
		"audit-allowed",
		allowedRequestBody,
	)
	if allowedStatusCode != http.StatusOK {
		t.Fatalf("allowed request returned status %d: %s", allowedStatusCode, string(allowedResponseBody))
	}
	var allowedResponse loopgate.CapabilityResponse
	decodeJSON(t, allowedResponseBody, &allowedResponse)
	if allowedResponse.Status != loopgate.ResponseStatusSuccess {
		t.Fatalf("expected allowed request success, got %#v", allowedResponse)
	}

	deniedRequestBody := mustJSON(t, loopgate.CapabilityRequest{
		RequestID:  "req-audit-denied",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "core/policy/policy.yaml",
			"content": "tamper attempt",
		},
	})
	deniedStatusCode, deniedResponseBody := harness.doSignedJSONBytes(
		t,
		http.MethodPost,
		"/v1/capabilities/execute",
		credentials,
		time.Now().UTC().Format(time.RFC3339Nano),
		"audit-denied",
		deniedRequestBody,
	)
	if deniedStatusCode != http.StatusInternalServerError {
		t.Fatalf("denied request returned status %d: %s", deniedStatusCode, string(deniedResponseBody))
	}
	var deniedResponse loopgate.CapabilityResponse
	decodeJSON(t, deniedResponseBody, &deniedResponse)
	if deniedResponse.Status != loopgate.ResponseStatusError {
		t.Fatalf("expected denied request error status, got %#v", deniedResponse)
	}

	lastSequence := harness.verifyAuditChain(t)
	events, auditBytes := harness.readAuditEvents(t)
	if lastSequence != int64(len(events)) {
		t.Fatalf("expected verified audit sequence %d to match %d stored events", lastSequence, len(events))
	}
	if len(events) < 5 {
		t.Fatalf("expected multiple audit events, got %#v", events)
	}

	// waitForStatus performs a bootstrap session.opened before this test opens its own session;
	// only capability-bearing audit lines for this test must bind to the test control session.
	var previousSequence int64
	for _, auditEvent := range events {
		sequence := eventSequence(t, auditEvent)
		if sequence <= previousSequence {
			t.Fatalf("expected strictly increasing audit_sequence, got %d after %d in %#v", sequence, previousSequence, auditEvent)
		}
		previousSequence = sequence
		switch auditEvent.Type {
		case "capability.executed", "capability.error":
			if auditEvent.Session != credentials.ControlSessionID {
				t.Fatalf("expected capability audit event for test control session %q, got %#v", credentials.ControlSessionID, auditEvent)
			}
		}
	}

	if _, found := findAuditEvent(events, "session.opened", ""); !found {
		t.Fatalf("expected session.opened event, got %#v", events)
	}
	if _, found := findAuditEvent(events, "capability.executed", "req-audit-allowed"); !found {
		t.Fatalf("expected capability.executed event for allowed request, got %#v", events)
	}
	if _, found := findAuditEvent(events, "capability.error", "req-audit-denied"); !found {
		t.Fatalf("expected capability.error event for denied request, got %#v", events)
	}

	for _, forbiddenValue := range []string{
		credentials.CapabilityToken,
		credentials.ApprovalToken,
		credentials.SessionMACKey,
	} {
		if bytes.Contains(auditBytes, []byte(forbiddenValue)) {
			t.Fatalf("expected audit log to redact live session secret %q", forbiddenValue)
		}
	}
}

func TestAuditFailureBlocksExecution(t *testing.T) {
	harness := newLoopgateHarness(t, integrationPolicyYAML(true))
	status := harness.waitForStatus(t)
	credentials := harness.openSession(t, "audit-block-actor", "audit-block-session", capabilityNames(status.Capabilities))

	// Make the audit file and directory unwritable so audit appends fail.
	// File must be chmod'd before the directory since the directory must be
	// accessible to chmod its contents.
	if err := os.Chmod(harness.auditPath(), 0o000); err != nil {
		t.Fatalf("chmod audit file: %v", err)
	}
	auditDir := filepath.Dir(harness.auditPath())
	if err := os.Chmod(auditDir, 0o000); err != nil {
		t.Fatalf("chmod audit dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(auditDir, 0o700)
		_ = os.Chmod(harness.auditPath(), 0o600)
	})

	// Create a test file to read — this should succeed if audit were working.
	testFilePath := filepath.Join(harness.repoRoot, "audit_test_file.txt")
	if err := os.WriteFile(testFilePath, []byte("test content"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	requestBody := mustJSON(t, loopgate.CapabilityRequest{
		RequestID:  "req-audit-blocked",
		Capability: "fs_read",
		Arguments: map[string]string{
			"path": "audit_test_file.txt",
		},
	})
	statusCode, responseBody := harness.doSignedJSONBytes(
		t,
		http.MethodPost,
		"/v1/capabilities/execute",
		credentials,
		time.Now().UTC().Format(time.RFC3339Nano),
		"audit-blocked-nonce",
		requestBody,
	)

	var response loopgate.CapabilityResponse
	decodeJSON(t, responseBody, &response)

	if response.DenialCode != loopgate.DenialCodeAuditUnavailable {
		t.Fatalf("expected denial code %q, got status=%d denial_code=%q reason=%q body=%s",
			loopgate.DenialCodeAuditUnavailable, statusCode, response.DenialCode, response.DenialReason, string(responseBody))
	}
	if response.Status != loopgate.ResponseStatusError {
		t.Fatalf("expected error status, got %q", response.Status)
	}
}
