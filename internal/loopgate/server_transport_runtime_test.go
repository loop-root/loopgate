package loopgate

import (
	"context"
	"encoding/json"
	"errors"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestHTTPStatusForResponseMapsTypedCapabilityResponses(t *testing.T) {
	testCases := []struct {
		name         string
		response     controlapipkg.CapabilityResponse
		expectedHTTP int
	}{
		{
			name:         "success",
			response:     controlapipkg.CapabilityResponse{Status: controlapipkg.ResponseStatusSuccess},
			expectedHTTP: http.StatusOK,
		},
		{
			name:         "pending approval",
			response:     controlapipkg.CapabilityResponse{Status: controlapipkg.ResponseStatusPendingApproval},
			expectedHTTP: http.StatusAccepted,
		},
		{
			name:         "denied unauthorized",
			response:     controlapipkg.CapabilityResponse{Status: controlapipkg.ResponseStatusDenied, DenialCode: controlapipkg.DenialCodeCapabilityTokenInvalid},
			expectedHTTP: http.StatusUnauthorized,
		},
		{
			name:         "denied rate limited",
			response:     controlapipkg.CapabilityResponse{Status: controlapipkg.ResponseStatusDenied, DenialCode: controlapipkg.DenialCodeSessionOpenRateLimited},
			expectedHTTP: http.StatusTooManyRequests,
		},
		{
			name:         "error audit unavailable",
			response:     controlapipkg.CapabilityResponse{Status: controlapipkg.ResponseStatusError, DenialCode: controlapipkg.DenialCodeAuditUnavailable},
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

func TestWrapHTTPHandlerSetsRequestIDHeaderAndContext(t *testing.T) {
	server := &Server{}
	var seenRequestID string

	handler := server.wrapHTTPHandler(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestID, ok := requestIDFromContext(request.Context())
		if !ok {
			t.Fatal("expected request id in request context")
		}
		seenRequestID = requestID
		writer.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, recorder.Code)
	}
	requestIDHeader := recorder.Header().Get(loopgateRequestIDHeader)
	if requestIDHeader == "" {
		t.Fatal("expected request id header to be set")
	}
	if seenRequestID != requestIDHeader {
		t.Fatalf("expected request context id %q to match response header %q", seenRequestID, requestIDHeader)
	}
}

func TestWrapHTTPHandlerRecoversPanics(t *testing.T) {
	server := &Server{}

	handler := server.wrapHTTPHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	request := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, recorder.Code)
	}
	if requestIDHeader := recorder.Header().Get(loopgateRequestIDHeader); requestIDHeader == "" {
		t.Fatal("expected request id header to be set on panic response")
	}
}

func TestWrapHTTPHandlerRejectsWhenInFlightLimitSaturated(t *testing.T) {
	server := &Server{
		httpRequestSlots: make(chan struct{}, 1),
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var handlerCalls int32

	handler := server.wrapHTTPHandler(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		atomic.AddInt32(&handlerCalls, 1)
		close(started)
		<-release
		writer.WriteHeader(http.StatusNoContent)
	}))

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		request := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNoContent {
			t.Errorf("first request expected status %d, got %d", http.StatusNoContent, recorder.Code)
		}
	}()
	<-started

	secondRequest := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	secondRecorder := httptest.NewRecorder()
	handler.ServeHTTP(secondRecorder, secondRequest)
	if secondRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("second request expected status %d, got %d", http.StatusTooManyRequests, secondRecorder.Code)
	}
	if requestIDHeader := secondRecorder.Header().Get(loopgateRequestIDHeader); requestIDHeader == "" {
		t.Fatal("expected saturated response to include request id header")
	}
	var denial controlapipkg.CapabilityResponse
	if err := json.Unmarshal(secondRecorder.Body.Bytes(), &denial); err != nil {
		t.Fatalf("decode saturated response: %v", err)
	}
	if denial.DenialCode != controlapipkg.DenialCodeControlPlaneStateSaturated {
		t.Fatalf("expected denial code %q, got %q", controlapipkg.DenialCodeControlPlaneStateSaturated, denial.DenialCode)
	}
	if gotCalls := atomic.LoadInt32(&handlerCalls); gotCalls != 1 {
		t.Fatalf("expected saturated request not to reach handler; handler calls=%d", gotCalls)
	}

	close(release)
	<-firstDone
}
