package loopgate

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loopgate/internal/ledger"
)

func newControlSessionRecoveryTestServer(t *testing.T, repoRoot string) *Server {
	t.Helper()

	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	writeTestMorphlingClassPolicy(t, repoRoot)

	server, err := NewServer(repoRoot, filepath.Join(t.TempDir(), "loopgate.sock"))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	server.sessionOpenMinInterval = 0
	server.expirySweepMaxInterval = 0
	return server
}

func doSessionOpenWithPeer(t *testing.T, server *Server, requestPeerIdentity peerIdentity, actor string, sessionID string) *httptest.ResponseRecorder {
	t.Helper()

	requestBody, err := json.Marshal(OpenSessionRequest{
		Actor:                 actor,
		SessionID:             sessionID,
		RequestedCapabilities: []string{"fs_list"},
	})
	if err != nil {
		t.Fatalf("marshal session open request: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/session/open", bytes.NewReader(requestBody))
	request = request.WithContext(context.WithValue(request.Context(), peerIdentityContextKey, requestPeerIdentity))
	request.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	server.handleSessionOpen(recorder, request)
	return recorder
}

func openSessionWithPeer(t *testing.T, server *Server, requestPeerIdentity peerIdentity, actor string, sessionID string) OpenSessionResponse {
	t.Helper()

	recorder := doSessionOpenWithPeer(t, server, requestPeerIdentity, actor, sessionID)
	if recorder.Code != http.StatusOK {
		var denial CapabilityResponse
		_ = json.Unmarshal(recorder.Body.Bytes(), &denial)
		t.Fatalf("expected session open success, got status=%d body=%s denial=%#v", recorder.Code, recorder.Body.String(), denial)
	}

	var response OpenSessionResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode session open response: %v", err)
	}
	return response
}

func auditFileContains(t *testing.T, server *Server, fragment string) bool {
	t.Helper()

	auditBytes, err := os.ReadFile(server.auditPath)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	return strings.Contains(string(auditBytes), fragment)
}

func TestOpenSessionRetiresDeadPeerOrphanBeforeActiveLimit(t *testing.T) {
	repoRoot := t.TempDir()
	server := newControlSessionRecoveryTestServer(t, repoRoot)
	server.maxActiveSessionsPerUID = 1

	firstPeerIdentity := peerIdentity{UID: 501, PID: 4101, EPID: 4101}
	secondPeerIdentity := peerIdentity{UID: 501, PID: 4102, EPID: 4102}

	firstSession := openSessionWithPeer(t, server, firstPeerIdentity, "haven", "haven-launch-a")
	server.processExists = func(pid int) (bool, error) {
		return pid != firstPeerIdentity.PID, nil
	}

	secondSession := openSessionWithPeer(t, server, secondPeerIdentity, "haven", "haven-launch-b")

	server.mu.Lock()
	defer server.mu.Unlock()

	if len(server.sessions) != 1 {
		t.Fatalf("expected exactly one live control session after orphan recovery, got %d", len(server.sessions))
	}
	if _, found := server.sessions[firstSession.ControlSessionID]; found {
		t.Fatalf("expected original orphaned session %q to be retired", firstSession.ControlSessionID)
	}
	if _, found := server.sessions[secondSession.ControlSessionID]; !found {
		t.Fatalf("expected replacement session %q to remain active", secondSession.ControlSessionID)
	}
}

func TestOpenSessionCancelsPendingApprovalsForDeadPeerOrphan(t *testing.T) {
	repoRoot := t.TempDir()
	server := newControlSessionRecoveryTestServer(t, repoRoot)
	server.maxActiveSessionsPerUID = 1

	firstPeerIdentity := peerIdentity{UID: 501, PID: 4201, EPID: 4201}
	secondPeerIdentity := peerIdentity{UID: 501, PID: 4202, EPID: 4202}

	firstSession := openSessionWithPeer(t, server, firstPeerIdentity, "haven", "haven-launch-a")
	server.mu.Lock()
	server.approvals["approval-orphan"] = pendingApproval{
		ID:               "approval-orphan",
		Request:          CapabilityRequest{Capability: "fs_write"},
		CreatedAt:        server.now().UTC(),
		ExpiresAt:        server.now().UTC().Add(time.Minute),
		ControlSessionID: firstSession.ControlSessionID,
		State:            approvalStatePending,
		Metadata: map[string]interface{}{
			"approval_class": "filesystem_write",
		},
		ExecutionContext: approvalExecutionContext{
			ControlSessionID:   firstSession.ControlSessionID,
			ActorLabel:         "haven",
			ClientSessionLabel: "haven-launch-a",
		},
	}
	server.mu.Unlock()

	server.processExists = func(pid int) (bool, error) {
		return pid != firstPeerIdentity.PID, nil
	}
	openSessionWithPeer(t, server, secondPeerIdentity, "haven", "haven-launch-b")

	server.mu.Lock()
	approvalRecord, found := server.approvals["approval-orphan"]
	server.mu.Unlock()
	if !found {
		t.Fatalf("expected pending approval to remain as a cancelled record")
	}
	if approvalRecord.State != approvalStateCancelled {
		t.Fatalf("expected orphaned approval to be cancelled, got %#v", approvalRecord)
	}
	if !auditFileContains(t, server, "\"type\":\"approval.cancelled\"") {
		t.Fatalf("expected approval.cancelled audit event for orphan recovery")
	}
}

func TestOpenSessionOrphanRecoveryFailsClosedOnAuditFailure(t *testing.T) {
	repoRoot := t.TempDir()
	server := newControlSessionRecoveryTestServer(t, repoRoot)
	server.maxActiveSessionsPerUID = 1

	firstPeerIdentity := peerIdentity{UID: 501, PID: 4401, EPID: 4401}
	secondPeerIdentity := peerIdentity{UID: 501, PID: 4402, EPID: 4402}

	firstSession := openSessionWithPeer(t, server, firstPeerIdentity, "haven", "haven-launch-a")
	server.processExists = func(pid int) (bool, error) {
		return pid != firstPeerIdentity.PID, nil
	}

	originalAppendAuditEvent := server.appendAuditEvent
	server.appendAuditEvent = func(ledgerPath string, auditEvent ledger.Event) error {
		if auditEvent.Type == "session.orphan_retired" {
			return context.DeadlineExceeded
		}
		return originalAppendAuditEvent(ledgerPath, auditEvent)
	}

	recorder := doSessionOpenWithPeer(t, server, secondPeerIdentity, "haven", "haven-launch-b")
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected orphan recovery failure to fail closed with 503, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	var denial CapabilityResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &denial); err != nil {
		t.Fatalf("decode denial response: %v", err)
	}
	if denial.DenialCode != DenialCodeExecutionFailed {
		t.Fatalf("expected execution_failed denial for orphan recovery failure, got %#v", denial)
	}

	server.mu.Lock()
	defer server.mu.Unlock()
	if _, found := server.sessions[firstSession.ControlSessionID]; !found {
		t.Fatalf("expected orphaned session to remain when orphan recovery audit fails")
	}
	if len(server.sessions) != 1 {
		t.Fatalf("expected no replacement session after failed orphan recovery, got %d", len(server.sessions))
	}
}
