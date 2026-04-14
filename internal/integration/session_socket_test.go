package integration_test

import (
	"net/http"
	"testing"
	"time"

	"loopgate/internal/loopgate"
)

func TestSessionAuthReplayOverRealSocket(t *testing.T) {
	harness := newLoopgateHarness(t, integrationPolicyYAML(true))
	status := harness.waitForStatus(t)
	credentials := harness.openSession(t, "integration-actor", "integration-session", capabilityNames(status.Capabilities))

	requestBody := mustJSON(t, loopgate.CapabilityRequest{
		RequestID:  "req-replayed-nonce",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	requestTimestamp := time.Now().UTC().Format(time.RFC3339Nano)
	requestNonce := "replayed-nonce"

	firstStatusCode, firstBody := harness.doSignedJSONBytes(
		t,
		http.MethodPost,
		"/v1/capabilities/execute",
		credentials,
		requestTimestamp,
		requestNonce,
		requestBody,
	)
	if firstStatusCode != http.StatusOK {
		t.Fatalf("first signed request returned status %d: %s", firstStatusCode, string(firstBody))
	}
	var firstResponse loopgate.CapabilityResponse
	decodeJSON(t, firstBody, &firstResponse)
	if firstResponse.Status != loopgate.ResponseStatusSuccess {
		t.Fatalf("expected first signed request success, got %#v", firstResponse)
	}

	replayedStatusCode, replayedBody := harness.doSignedJSONBytes(
		t,
		http.MethodPost,
		"/v1/capabilities/execute",
		credentials,
		requestTimestamp,
		requestNonce,
		requestBody,
	)
	if replayedStatusCode != http.StatusUnauthorized {
		t.Fatalf("expected replayed request to return %d, got %d with body %s", http.StatusUnauthorized, replayedStatusCode, string(replayedBody))
	}
	var replayedResponse loopgate.CapabilityResponse
	decodeJSON(t, replayedBody, &replayedResponse)
	if replayedResponse.DenialCode != loopgate.DenialCodeRequestNonceReplayDetected {
		t.Fatalf("expected replay denial code %q, got %#v", loopgate.DenialCodeRequestNonceReplayDetected, replayedResponse)
	}

	invalidNonce := "invalid-signature"
	invalidTimestamp := time.Now().UTC().Format(time.RFC3339Nano)
	invalidSignature := mutateHexSignature(computeRequestSignature(
		credentials.SessionMACKey,
		http.MethodPost,
		"/v1/capabilities/execute",
		credentials.ControlSessionID,
		invalidTimestamp,
		invalidNonce,
		requestBody,
	))
	invalidStatusCode, invalidBody := harness.doJSONBytes(
		t,
		http.MethodPost,
		"/v1/capabilities/execute",
		credentials.CapabilityToken,
		map[string]string{
			"Content-Type":                 "application/json",
			"X-Loopgate-Control-Session":   credentials.ControlSessionID,
			"X-Loopgate-Request-Timestamp": invalidTimestamp,
			"X-Loopgate-Request-Nonce":     invalidNonce,
			"X-Loopgate-Request-Signature": invalidSignature,
		},
		requestBody,
	)
	if invalidStatusCode != http.StatusUnauthorized {
		t.Fatalf("expected invalid signature request to return %d, got %d with body %s", http.StatusUnauthorized, invalidStatusCode, string(invalidBody))
	}
	var invalidResponse loopgate.CapabilityResponse
	decodeJSON(t, invalidBody, &invalidResponse)
	if invalidResponse.DenialCode != loopgate.DenialCodeRequestSignatureInvalid {
		t.Fatalf("expected invalid signature denial code %q, got %#v", loopgate.DenialCodeRequestSignatureInvalid, invalidResponse)
	}
}
