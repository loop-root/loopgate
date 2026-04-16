package loopgate

import (
	"context"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"testing"
)

func TestHTTPStatusForResponseMapsTypedCapabilityResponses(t *testing.T) {
	testCases := []struct {
		name         string
		response     CapabilityResponse
		expectedHTTP int
	}{
		{
			name:         "success",
			response:     CapabilityResponse{Status: ResponseStatusSuccess},
			expectedHTTP: http.StatusOK,
		},
		{
			name:         "pending approval",
			response:     CapabilityResponse{Status: ResponseStatusPendingApproval},
			expectedHTTP: http.StatusAccepted,
		},
		{
			name:         "denied unauthorized",
			response:     CapabilityResponse{Status: ResponseStatusDenied, DenialCode: DenialCodeCapabilityTokenInvalid},
			expectedHTTP: http.StatusUnauthorized,
		},
		{
			name:         "denied rate limited",
			response:     CapabilityResponse{Status: ResponseStatusDenied, DenialCode: DenialCodeSessionOpenRateLimited},
			expectedHTTP: http.StatusTooManyRequests,
		},
		{
			name:         "error audit unavailable",
			response:     CapabilityResponse{Status: ResponseStatusError, DenialCode: DenialCodeAuditUnavailable},
			expectedHTTP: http.StatusServiceUnavailable,
		},
	}

	for _, testCase := range testCases {
		if gotHTTP := httpStatusForResponse(testCase.response); gotHTTP != testCase.expectedHTTP {
			t.Fatalf("%s: expected %d, got %d", testCase.name, testCase.expectedHTTP, gotHTTP)
		}
	}
}

func TestServerConnContextReportsPeerCredentialFailure(t *testing.T) {
	repoRoot := t.TempDir()
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	server, err := NewServer(repoRoot, filepath.Join(t.TempDir(), "loopgate.sock"))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	var reportedSecurityCode string
	server.resolvePeerIdentity = func(net.Conn) (peerIdentity, error) {
		return peerIdentity{}, errors.New("synthetic peer credential failure")
	}
	server.reportSecurityWarning = func(eventCode string, cause error) {
		reportedSecurityCode = eventCode
		_ = cause
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	ctx := server.server.ConnContext(context.Background(), clientConn)
	if _, ok := peerIdentityFromContext(ctx); ok {
		t.Fatal("expected peer identity to be absent after credential lookup failure")
	}
	if reportedSecurityCode != "unix_peer_resolve_failed" {
		t.Fatalf("expected unix_peer_resolve_failed security event, got %q", reportedSecurityCode)
	}
}
