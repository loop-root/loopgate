package loopgate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"loopgate/internal/ledger"
	"loopgate/internal/secrets"
	toolspkg "loopgate/internal/tools"
)

func TestUIStatusReturnsDisplaySafeFields(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	uiStatus, err := client.UIStatus(context.Background())
	if err != nil {
		t.Fatalf("ui status: %v", err)
	}
	if strings.TrimSpace(uiStatus.ControlSessionID) == "" {
		t.Fatalf("expected control session id in ui status, got %#v", uiStatus)
	}
	if uiStatus.Policy.ReadEnabled != true {
		t.Fatalf("expected read-enabled policy summary, got %#v", uiStatus.Policy)
	}

	encodedStatus, err := json.Marshal(uiStatus)
	if err != nil {
		t.Fatalf("marshal ui status: %v", err)
	}
	lowerJSON := strings.ToLower(string(encodedStatus))
	for _, forbiddenField := range []string{"access_token", "refresh_token", "client_secret", "approval_token", "session_mac_key"} {
		if strings.Contains(lowerJSON, forbiddenField) {
			t.Fatalf("ui status leaked forbidden field %q: %s", forbiddenField, encodedStatus)
		}
	}
}

func TestUIStatusIncludesActiveOperatorMountWriteGrants(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}

	server.mu.Lock()
	controlSession := server.sessionState.sessions[client.controlSessionID]
	controlSession.OperatorMountWriteGrants = map[string]time.Time{
		resolvedRepoRoot: server.now().UTC().Add(2 * time.Hour),
		filepath.Join(resolvedRepoRoot, "expired"): server.now().UTC().Add(-1 * time.Minute),
	}
	server.sessionState.sessions[client.controlSessionID] = controlSession
	server.mu.Unlock()

	uiStatus, err := client.UIStatus(context.Background())
	if err != nil {
		t.Fatalf("ui status: %v", err)
	}
	if len(uiStatus.OperatorMountWriteGrants) != 1 {
		t.Fatalf("operator mount write grants: %#v", uiStatus.OperatorMountWriteGrants)
	}
	if uiStatus.OperatorMountWriteGrants[0].RootPath != resolvedRepoRoot {
		t.Fatalf("grant root = %q want %q", uiStatus.OperatorMountWriteGrants[0].RootPath, resolvedRepoRoot)
	}
	if strings.TrimSpace(uiStatus.OperatorMountWriteGrants[0].ExpiresAtUTC) == "" {
		t.Fatalf("expected expiry in ui status grant, got %#v", uiStatus.OperatorMountWriteGrants[0])
	}
}

func TestUpdateUIOperatorMountWriteGrantRevokesAndRenews(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}

	server.mu.Lock()
	controlSession := server.sessionState.sessions[client.controlSessionID]
	controlSession.OperatorMountPaths = []string{resolvedRepoRoot}
	controlSession.OperatorMountWriteGrants = map[string]time.Time{
		resolvedRepoRoot: server.now().UTC().Add(time.Hour),
	}
	server.sessionState.sessions[client.controlSessionID] = controlSession
	server.mu.Unlock()

	revokedResponse, err := client.UpdateUIOperatorMountWriteGrant(context.Background(), controlapipkg.UIOperatorMountWriteGrantUpdateRequest{
		RootPath: resolvedRepoRoot,
		Action:   controlapipkg.OperatorMountWriteGrantActionRevoke,
	})
	if err != nil {
		t.Fatalf("revoke write grant: %v", err)
	}
	if len(revokedResponse.Grants) != 0 {
		t.Fatalf("expected no grants after revoke, got %#v", revokedResponse.Grants)
	}

	server.mu.Lock()
	controlSession = server.sessionState.sessions[client.controlSessionID]
	controlSession.OperatorMountWriteGrants[resolvedRepoRoot] = server.now().UTC().Add(time.Hour)
	server.sessionState.sessions[client.controlSessionID] = controlSession
	server.mu.Unlock()

	if _, err := client.UpdateUIOperatorMountWriteGrant(context.Background(), controlapipkg.UIOperatorMountWriteGrantUpdateRequest{
		RootPath: resolvedRepoRoot,
		Action:   controlapipkg.OperatorMountWriteGrantActionRenew,
	}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeApprovalRequired) {
		t.Fatalf("expected renew to require fresh approval, got %v", err)
	}
}

func TestUpdateUIOperatorMountWriteGrantFailsClosedWhenAuditUnavailable(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}

	server.mu.Lock()
	controlSession := server.sessionState.sessions[client.controlSessionID]
	originalExpiresAtUTC := server.now().UTC().Add(time.Hour)
	controlSession.OperatorMountPaths = []string{resolvedRepoRoot}
	controlSession.OperatorMountWriteGrants = map[string]time.Time{
		resolvedRepoRoot: originalExpiresAtUTC,
	}
	server.sessionState.sessions[client.controlSessionID] = controlSession
	server.mu.Unlock()

	appendAuditEvent := server.appendAuditEvent
	server.appendAuditEvent = func(path string, ledgerEvent ledger.Event) error {
		if ledgerEvent.Type == "operator_mount.write_grant.updated" {
			return errors.New("audit append unavailable")
		}
		return appendAuditEvent(path, ledgerEvent)
	}

	if _, err := client.UpdateUIOperatorMountWriteGrant(context.Background(), controlapipkg.UIOperatorMountWriteGrantUpdateRequest{
		RootPath: resolvedRepoRoot,
		Action:   controlapipkg.OperatorMountWriteGrantActionRevoke,
	}); err == nil {
		t.Fatal("expected revoke error when audit unavailable")
	}

	server.mu.Lock()
	defer server.mu.Unlock()
	controlSession = server.sessionState.sessions[client.controlSessionID]
	if got := controlSession.OperatorMountWriteGrants[resolvedRepoRoot]; !got.Equal(originalExpiresAtUTC) {
		t.Fatalf("grant expiry changed on audit failure: got %v want %v", got, originalExpiresAtUTC)
	}
}

func TestApprovalDecisionFailsClosedWhenAuditUnavailable(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))

	appendAuditEvent := server.appendAuditEvent
	server.appendAuditEvent = func(path string, ledgerEvent ledger.Event) error {
		if ledgerEvent.Type == "approval.denied" {
			return errors.New("audit append unavailable")
		}
		return appendAuditEvent(path, ledgerEvent)
	}

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-approval-deny",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "guarded.txt",
			"content": "hello",
		},
	})
	if err != nil {
		t.Fatalf("execute pending approval: %v", err)
	}
	if !response.ApprovalRequired {
		t.Fatalf("expected approval required, got %#v", response)
	}

	decisionResponse, err := client.DecideApproval(context.Background(), response.ApprovalRequestID, false)
	if err != nil {
		t.Fatalf("decide approval: %v", err)
	}
	if decisionResponse.Status != controlapipkg.ResponseStatusError || decisionResponse.DenialCode != controlapipkg.DenialCodeAuditUnavailable {
		t.Fatalf("expected audit_unavailable approval failure, got %#v", decisionResponse)
	}
}

func TestUIApprovalDecisionFailsClosedWhenAuditUnavailable(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))

	appendAuditEvent := server.appendAuditEvent
	server.appendAuditEvent = func(path string, ledgerEvent ledger.Event) error {
		if ledgerEvent.Type == "approval.denied" {
			return errors.New("audit append unavailable")
		}
		return appendAuditEvent(path, ledgerEvent)
	}

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-ui-approval-deny",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "guarded-ui.txt",
			"content": "hello",
		},
	})
	if err != nil {
		t.Fatalf("execute pending approval: %v", err)
	}
	if !response.ApprovalRequired {
		t.Fatalf("expected approval required, got %#v", response)
	}

	decisionResponse, err := client.UIDecideApproval(context.Background(), response.ApprovalRequestID, false)
	if err != nil {
		t.Fatalf("ui decide approval: %v", err)
	}
	if decisionResponse.Status != controlapipkg.ResponseStatusError || decisionResponse.DenialCode != controlapipkg.DenialCodeAuditUnavailable {
		t.Fatalf("expected ui audit_unavailable approval failure, got %#v", decisionResponse)
	}
}

func TestWriteJSONReportsSerializationFailure(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	var reportedStatus []int
	var reportedCauses []error
	server.reportResponseWriteError = func(httpStatus int, cause error) {
		reportedStatus = append(reportedStatus, httpStatus)
		reportedCauses = append(reportedCauses, cause)
	}

	responseRecorder := httptest.NewRecorder()
	server.writeJSON(responseRecorder, http.StatusOK, map[string]interface{}{
		"bad": func() {},
	})
	if len(reportedCauses) != 1 {
		t.Fatalf("expected one reported serialization error, got %#v", reportedCauses)
	}
	if reportedStatus[0] != http.StatusOK {
		t.Fatalf("expected reported status 200, got %d", reportedStatus[0])
	}
	if class := secrets.LoopgateOperatorErrorClass(reportedCauses[0]); class != "json_unsupported_type" && class != "json_marshal" {
		t.Fatalf("unexpected error class %q for %v", class, reportedCauses[0])
	}
}

func TestUIApprovalsHideDecisionNonceAndAllowDecision(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))

	pendingResponse, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-ui-approval",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "pending.txt",
			"content": "hidden",
		},
	})
	if err != nil {
		t.Fatalf("execute pending approval: %v", err)
	}
	if !pendingResponse.ApprovalRequired {
		t.Fatalf("expected pending approval, got %#v", pendingResponse)
	}

	uiApprovals, err := client.UIApprovals(context.Background())
	if err != nil {
		t.Fatalf("ui approvals: %v", err)
	}
	if len(uiApprovals.Approvals) != 1 {
		t.Fatalf("expected one ui approval, got %#v", uiApprovals)
	}
	if uiApprovals.Approvals[0].ApprovalRequestID != pendingResponse.ApprovalRequestID {
		t.Fatalf("expected matching approval id, got %#v", uiApprovals)
	}

	encodedApprovals, err := json.Marshal(uiApprovals)
	if err != nil {
		t.Fatalf("marshal ui approvals: %v", err)
	}
	lowerJSON := strings.ToLower(string(encodedApprovals))
	for _, forbiddenField := range []string{"decision_nonce", "approval_token", "session_mac_key"} {
		if strings.Contains(lowerJSON, forbiddenField) {
			t.Fatalf("ui approvals leaked forbidden field %q: %s", forbiddenField, encodedApprovals)
		}
	}

	approvedResponse, err := client.UIDecideApproval(context.Background(), pendingResponse.ApprovalRequestID, true)
	if err != nil {
		t.Fatalf("ui approval decision: %v", err)
	}
	if approvedResponse.Status != controlapipkg.ResponseStatusSuccess {
		t.Fatalf("expected successful ui approval resolution, got %#v", approvedResponse)
	}
}

func TestUIApprovalDecisionRejectsUnknownFields(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))

	pendingResponse, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-ui-unknown",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "pending.txt",
			"content": "hidden",
		},
	})
	if err != nil {
		t.Fatalf("execute pending approval: %v", err)
	}

	approvalToken, err := client.ensureApprovalToken(context.Background())
	if err != nil {
		t.Fatalf("approval token: %v", err)
	}

	var response controlapipkg.CapabilityResponse
	err = client.doJSON(context.Background(), http.MethodPost, "/v1/ui/approvals/"+pendingResponse.ApprovalRequestID+"/decision", "", map[string]interface{}{
		"approved": true,
		"extra":    "forbidden",
	}, &response, map[string]string{
		"X-Loopgate-Approval-Token": approvalToken,
	})
	if err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeMalformedRequest) {
		t.Fatalf("expected malformed-request denial for unknown field, got %v", err)
	}
}

func TestUIApprovalDecisionRejectsMissingApprovedField(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))

	pendingResponse, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-ui-missing-approved",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "pending.txt",
			"content": "hidden",
		},
	})
	if err != nil {
		t.Fatalf("execute pending approval: %v", err)
	}

	approvalToken, err := client.ensureApprovalToken(context.Background())
	if err != nil {
		t.Fatalf("approval token: %v", err)
	}

	var response controlapipkg.CapabilityResponse
	err = client.doJSON(context.Background(), http.MethodPost, "/v1/ui/approvals/"+pendingResponse.ApprovalRequestID+"/decision", "", map[string]interface{}{}, &response, map[string]string{
		"X-Loopgate-Approval-Token": approvalToken,
	})
	if err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeMalformedRequest) {
		t.Fatalf("expected malformed-request denial for missing approved field, got %v", err)
	}
}

func TestUIEventsReplayAndFilterAuditOnlyResults(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))

	if err := server.registry.Register(fakeLoopgateTool{
		name:        "remote_fetch",
		category:    "filesystem",
		operation:   toolspkg.OpRead,
		description: "test-only remote fetch stand-in",
		output:      "raw remote payload",
	}); err != nil {
		t.Fatalf("register remote_fetch: %v", err)
	}
	capabilities := append(capabilityNames(status.Capabilities), "remote_fetch", controlCapabilityUIRead)
	client.ConfigureSession("test-actor", "test-session", capabilities)
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	if _, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-ui-fs-list",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	}); err != nil {
		t.Fatalf("execute fs_list: %v", err)
	}

	pendingResponse, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-ui-pending",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "pending.txt",
			"content": "needs approval",
		},
	})
	if err != nil {
		t.Fatalf("execute fs_write pending approval: %v", err)
	}
	if !pendingResponse.ApprovalRequired {
		t.Fatalf("expected approval-required response, got %#v", pendingResponse)
	}

	if _, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-ui-audit-only",
		Capability: "remote_fetch",
	}); err != nil {
		t.Fatalf("execute unclassified capability: %v", err)
	}

	replayedEvents := readUIReplayEvents(t, client, "")
	if len(replayedEvents) < 3 {
		t.Fatalf("expected replayed ui events, got %#v", replayedEvents)
	}
	if !containsUIEventType(replayedEvents, controlapipkg.UIEventTypeSessionInfo) {
		t.Fatalf("expected session.info event, got %#v", replayedEvents)
	}
	if !containsUIEventType(replayedEvents, controlapipkg.UIEventTypeToolResult) {
		t.Fatalf("expected tool.result event, got %#v", replayedEvents)
	}
	if !containsUIEventType(replayedEvents, controlapipkg.UIEventTypeApprovalPending) {
		t.Fatalf("expected approval.pending event, got %#v", replayedEvents)
	}
	if containsUICapabilityEvent(replayedEvents, "remote_fetch") {
		t.Fatalf("expected audit-only unclassified capability to stay out of ui stream, got %#v", replayedEvents)
	}

	replayedFromLast := readUIReplayEvents(t, client, replayedEvents[0].ID)
	if len(replayedFromLast) >= len(replayedEvents) {
		t.Fatalf("expected Last-Event-ID replay to omit earlier events, got %#v", replayedFromLast)
	}

	recentEvents := readUIRecentEvents(t, client, "")
	if len(recentEvents) != len(replayedEvents) {
		t.Fatalf("expected recent ui events endpoint to mirror replay buffer, got %#v", recentEvents)
	}
	if !containsUIEventType(recentEvents, controlapipkg.UIEventTypeApprovalPending) {
		t.Fatalf("expected approval.pending event in recent ui events, got %#v", recentEvents)
	}
}

func TestEmitUIEventReusesReplayBufferBackingArrayAfterOverflow(t *testing.T) {
	server := &Server{
		now: func() time.Time {
			return time.Date(2026, time.May, 4, 12, 0, 0, 0, time.UTC)
		},
	}
	server.ui.events = make([]controlapipkg.UIEventEnvelope, maxUIEventBuffer, maxUIEventBuffer+1)
	for index := range server.ui.events {
		server.ui.events[index] = controlapipkg.UIEventEnvelope{
			ControlSessionID: "session-a",
			ID:               strconv.Itoa(index + 1),
			Type:             controlapipkg.UIEventTypeWarning,
			TS:               "2026-05-04T12:00:00Z",
			Data:             controlapipkg.UIEventWarning{Message: "preloaded"},
		}
	}
	server.ui.sequence = uint64(maxUIEventBuffer)
	initialBackingArray := fmt.Sprintf("%p", &server.ui.events[0])

	server.emitUIEvent("session-a", controlapipkg.UIEventTypeWarning, controlapipkg.UIEventWarning{Message: "overflow"})
	afterFirstOverflowBackingArray := fmt.Sprintf("%p", &server.ui.events[0])
	server.emitUIEvent("session-a", controlapipkg.UIEventTypeWarning, controlapipkg.UIEventWarning{Message: "overflow again"})
	afterSecondOverflowBackingArray := fmt.Sprintf("%p", &server.ui.events[0])

	if len(server.ui.events) != maxUIEventBuffer {
		t.Fatalf("expected ui event buffer length %d, got %d", maxUIEventBuffer, len(server.ui.events))
	}
	if afterFirstOverflowBackingArray != initialBackingArray || afterSecondOverflowBackingArray != initialBackingArray {
		t.Fatalf("expected overflow trim to reuse backing array, got initial=%s first=%s second=%s", initialBackingArray, afterFirstOverflowBackingArray, afterSecondOverflowBackingArray)
	}
	if server.ui.events[0].ID != "3" {
		t.Fatalf("expected replay buffer to drop oldest events after two overflows, first id=%q", server.ui.events[0].ID)
	}
}
