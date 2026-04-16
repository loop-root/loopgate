package loopgate

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loopgate/internal/ledger"
)

func TestDuplicateRequestIDIsRejected(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	firstResponse, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-duplicate",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("first execute: %v", err)
	}
	if firstResponse.Status != ResponseStatusSuccess {
		t.Fatalf("unexpected first response: %#v", firstResponse)
	}

	secondResponse, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-duplicate",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("second execute should return typed denial, got %v", err)
	}
	if secondResponse.Status != ResponseStatusDenied || secondResponse.DenialCode != DenialCodeRequestReplayDetected {
		t.Fatalf("expected replay denial, got %#v", secondResponse)
	}
}

func TestAuditFailureIsSurfacedExplicitly(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.appendAuditEvent = func(string, ledger.Event) error {
		return errors.New("audit sink unavailable")
	}
	client.ConfigureSession("audit-test", "audit-session", []string{"fs_list"})

	_, err := client.ensureCapabilityToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), DenialCodeAuditUnavailable) {
		t.Fatalf("expected audit unavailable error, got %v", err)
	}
}

func TestCapabilityExecutionAuditFailureReturnsAuditUnavailable(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	server.appendAuditEvent = func(string, ledger.Event) error {
		return errors.New("audit sink unavailable")
	}

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-audit-fail-after-open",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("expected typed audit unavailable response, got %v", err)
	}
	if response.Status != ResponseStatusError || response.DenialCode != DenialCodeAuditUnavailable {
		t.Fatalf("expected audit unavailable response, got %#v", response)
	}
}

func TestSingleUseExecutionTokenIsDeniedOnReuse(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	server.mu.Lock()
	baseToken := server.tokens[client.capabilityToken]
	server.mu.Unlock()

	capabilityRequest := normalizeCapabilityRequest(CapabilityRequest{
		RequestID:  "req-single-use",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "notes.txt",
			"content": "hello",
		},
	})
	executionToken := deriveExecutionToken(baseToken, capabilityRequest)

	firstResponse := server.executeCapabilityRequest(context.Background(), executionToken, capabilityRequest, false)
	if firstResponse.Status != ResponseStatusSuccess {
		t.Fatalf("expected first single-use execution to succeed, got %#v", firstResponse)
	}
	server.mu.Lock()
	consumedToken, found := server.usedTokens[executionToken.TokenID]
	server.mu.Unlock()
	if !found {
		t.Fatal("expected single-use execution token to be recorded in used token registry")
	}
	if consumedToken.ParentTokenID != baseToken.TokenID {
		t.Fatalf("expected parent token id %q, got %#v", baseToken.TokenID, consumedToken)
	}
	if consumedToken.Capability != capabilityRequest.Capability {
		t.Fatalf("expected consumed capability %q, got %#v", capabilityRequest.Capability, consumedToken)
	}
	if consumedToken.NormalizedArgHash != normalizedArgumentHash(capabilityRequest.Arguments) {
		t.Fatalf("expected normalized argument hash to be recorded, got %#v", consumedToken)
	}

	secondResponse := server.executeCapabilityRequest(context.Background(), executionToken, capabilityRequest, false)
	if secondResponse.Status != ResponseStatusDenied || secondResponse.DenialCode != DenialCodeCapabilityTokenReused {
		t.Fatalf("expected reused single-use token denial, got %#v", secondResponse)
	}
}

func TestApprovalExecuteDeniesWhenStoredExecutionBodyMutated(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}
	pendingResponse, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-integrity",
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
	server.mu.Lock()
	pa := server.approvals[pendingResponse.ApprovalRequestID]
	pa.Request.Arguments["path"] = "evil.txt"
	server.approvals[pendingResponse.ApprovalRequestID] = pa
	server.mu.Unlock()
	approvedResponse, err := client.UIDecideApproval(context.Background(), pendingResponse.ApprovalRequestID, true)
	if err != nil {
		t.Fatalf("ui approval decision: %v", err)
	}
	if approvedResponse.DenialCode != DenialCodeApprovalExecutionBodyMismatch {
		t.Fatalf("expected execution body mismatch, got %#v", approvedResponse)
	}
	if approvedResponse.Status != ResponseStatusError {
		t.Fatalf("expected error status, got %#v", approvedResponse)
	}
}

func TestPendingApprovalLimitPerControlSession(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))
	server.maxPendingApprovalsPerControlSession = 2
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}
	for i := range 2 {
		resp, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
			RequestID:  "req-ap-limit-" + string(rune('0'+i)),
			Capability: "fs_write",
			Arguments: map[string]string{
				"path":    "limit-" + string(rune('0'+i)) + ".txt",
				"content": "x",
			},
		})
		if err != nil {
			t.Fatalf("execute %d: %v", i, err)
		}
		if !resp.ApprovalRequired {
			t.Fatalf("expected pending approval %d, got %#v", i, resp)
		}
	}
	resp, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-ap-limit-2",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "limit-2.txt",
			"content": "x",
		},
	})
	if err != nil {
		t.Fatalf("execute third: %v", err)
	}
	if resp.Status != ResponseStatusDenied || resp.DenialCode != DenialCodePendingApprovalLimitReached {
		t.Fatalf("expected pending approval limit, got %#v", resp)
	}
}

func TestRequestReplayStoreSaturates(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))
	server.maxSeenRequestReplayEntries = 2
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}
	for i := range 2 {
		_, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
			RequestID:  "req-replay-sat-" + string(rune('0'+i)),
			Capability: "fs_write",
			Arguments: map[string]string{
				"path":    "rs" + string(rune('0'+i)) + ".txt",
				"content": "x",
			},
		})
		if err != nil {
			t.Fatalf("execute %d: %v", i, err)
		}
	}
	resp, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-replay-sat-2",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "rs2.txt",
			"content": "x",
		},
	})
	if err != nil {
		t.Fatalf("execute third: %v", err)
	}
	if resp.Status != ResponseStatusDenied || resp.DenialCode != DenialCodeReplayStateSaturated {
		t.Fatalf("expected replay store saturated, got %#v", resp)
	}
}

func TestBoundExecutionTokenRejectsDifferentNormalizedArguments(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	server.mu.Lock()
	baseToken := server.tokens[client.capabilityToken]
	server.mu.Unlock()

	approvedRequest := normalizeCapabilityRequest(CapabilityRequest{
		RequestID:  "req-bound",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "./notes.txt",
			"content": "hello",
		},
	})
	executionToken := deriveExecutionToken(baseToken, approvedRequest)

	mutatedRequest := normalizeCapabilityRequest(CapabilityRequest{
		RequestID:  "req-bound-mutated",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "other.txt",
			"content": "hello",
		},
	})
	response := server.executeCapabilityRequest(context.Background(), executionToken, mutatedRequest, false)
	if response.Status != ResponseStatusDenied || response.DenialCode != DenialCodeCapabilityTokenBindingInvalid {
		t.Fatalf("expected bound token mismatch denial, got %#v", response)
	}
}

func TestLoopgateAuditEventsIncludeHashChainMetadata(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	_, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-audit-chain",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("execute fs_list: %v", err)
	}

	auditPath := filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl")
	auditFile, err := os.Open(auditPath)
	if err != nil {
		t.Fatalf("open audit log: %v", err)
	}
	defer auditFile.Close()

	scanner := bufio.NewScanner(auditFile)
	lineCount := 0
	var previousEventHash string
	for scanner.Scan() {
		lineCount++
		var auditEvent ledger.Event
		if err := json.Unmarshal(scanner.Bytes(), &auditEvent); err != nil {
			t.Fatalf("decode audit event: %v", err)
		}
		eventHash, _ := auditEvent.Data["event_hash"].(string)
		if strings.TrimSpace(eventHash) == "" {
			t.Fatalf("expected event_hash on audit event %#v", auditEvent)
		}
		if sequenceValue, found := auditEvent.Data["audit_sequence"]; !found || sequenceValue == nil {
			t.Fatalf("expected audit_sequence on audit event %#v", auditEvent)
		}
		previousHash, _ := auditEvent.Data["previous_event_hash"].(string)
		if previousHash != previousEventHash {
			t.Fatalf("expected previous_event_hash %q, got %q", previousEventHash, previousHash)
		}
		previousEventHash = eventHash
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan audit log: %v", err)
	}
	if lineCount < 2 {
		t.Fatalf("expected multiple chained audit events, got %d", lineCount)
	}
}

func TestHashAuditEventMatchesStoredLedgerHash(t *testing.T) {
	auditEvent := ledger.Event{
		TS:      time.Now().UTC().Format(time.RFC3339Nano),
		Type:    "test.audit",
		Session: "session-a",
		Data: map[string]interface{}{
			"audit_sequence":      uint64(1),
			"ledger_sequence":     uint64(1),
			"previous_event_hash": "",
			"step":                "one",
		},
	}

	precomputedHash, err := hashAuditEvent(auditEvent)
	if err != nil {
		t.Fatalf("hash audit event: %v", err)
	}
	auditEvent.Data["event_hash"] = precomputedHash

	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	if err := ledger.Append(auditPath, auditEvent); err != nil {
		t.Fatalf("append audit event: %v", err)
	}

	auditBytes, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	lines := bytes.Split(bytes.TrimSpace(auditBytes), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("expected one audit line, got %d", len(lines))
	}
	storedEvent, ok := ledger.ParseEvent(lines[0])
	if !ok {
		t.Fatalf("parse stored audit event: %s", string(lines[0]))
	}
	storedHash, _ := storedEvent.Data["event_hash"].(string)
	if storedHash != precomputedHash {
		t.Fatalf("expected stored hash %q to match precomputed hash %q, got event %#v", storedHash, precomputedHash, storedEvent)
	}
}

func TestLogEventWritesVerifiableAuditChain(t *testing.T) {
	repoRoot := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, "runtime", "state"), 0o700); err != nil {
		t.Fatalf("mkdir runtime state: %v", err)
	}

	verifyAuditChain := func(expectedSequence int64) {
		t.Helper()
		auditFile, err := os.Open(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
		if err != nil {
			t.Fatalf("open audit file: %v", err)
		}
		defer auditFile.Close()
		lastSequence, _, err := ledger.ReadVerifiedChainState(auditFile, "audit_sequence")
		if err != nil {
			t.Fatalf("verify audit chain: %v", err)
		}
		if lastSequence != expectedSequence {
			t.Fatalf("expected audit sequence %d, got %d", expectedSequence, lastSequence)
		}
	}

	if err := server.logEvent("test.audit", "session-a", map[string]interface{}{"step": "one"}); err != nil {
		t.Fatalf("log first audit event: %v", err)
	}
	verifyAuditChain(1)

	if err := server.logEvent("test.audit", "session-a", map[string]interface{}{"step": "two"}); err != nil {
		t.Fatalf("log second audit event: %v", err)
	}
	verifyAuditChain(2)
}

func TestHookPreValidateWritesAuditSequenceMetadata(t *testing.T) {
	repoRoot := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	requestBody := bytes.NewBufferString(`{"tool_name":"Bash","session_id":"session-hook"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", requestBody)
	request = request.WithContext(context.WithValue(request.Context(), peerIdentityContextKey, peerIdentity{
		UID: uint32(os.Getuid()),
		PID: 4242,
	}))
	recorder := httptest.NewRecorder()

	server.handleHookPreValidate(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response HookPreValidateResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}
	if response.Decision != "block" {
		t.Fatalf("expected blocked Bash hook response, got %#v", response)
	}

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(auditBytes)), "\n")
	if len(lines) == 0 {
		t.Fatal("expected hook audit event")
	}
	lastAuditEvent, ok := ledger.ParseEvent([]byte(lines[len(lines)-1]))
	if !ok {
		t.Fatalf("parse hook audit event: %s", lines[len(lines)-1])
	}
	if lastAuditEvent.Type != "hook.pre_validate" {
		t.Fatalf("expected hook.pre_validate event, got %#v", lastAuditEvent)
	}
	if _, found := lastAuditEvent.Data["audit_sequence"]; !found {
		t.Fatalf("expected audit_sequence on hook audit event %#v", lastAuditEvent)
	}
	if decisionValue, _ := lastAuditEvent.Data["decision"].(string); decisionValue != "block" {
		t.Fatalf("expected hook audit decision block, got %#v", lastAuditEvent.Data["decision"])
	}
}
