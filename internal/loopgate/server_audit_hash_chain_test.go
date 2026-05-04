package loopgate

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loopgate/internal/ledger"
)

func TestLoopgateAuditEventsIncludeHashChainMetadata(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	_, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
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

func TestHashAuditEvent_DeterministicAcrossMapInsertionOrder(t *testing.T) {
	firstAuditData := map[string]interface{}{}
	firstAuditData["audit_sequence"] = uint64(1)
	firstAuditData["ledger_sequence"] = uint64(1)
	firstAuditData["previous_event_hash"] = ""
	firstNested := map[string]interface{}{}
	firstNested["later"] = "value"
	firstNested["earlier"] = "value"
	firstAuditData["details"] = firstNested
	firstAuditData["validated_argument_keys"] = []string{"query", "limit"}
	firstAuditData["event_hash"] = "placeholder-a"

	secondAuditData := map[string]interface{}{}
	secondAuditData["event_hash"] = "placeholder-b"
	secondAuditData["validated_argument_keys"] = []string{"query", "limit"}
	secondNested := map[string]interface{}{}
	secondNested["earlier"] = "value"
	secondNested["later"] = "value"
	secondAuditData["details"] = secondNested
	secondAuditData["previous_event_hash"] = ""
	secondAuditData["ledger_sequence"] = uint64(1)
	secondAuditData["audit_sequence"] = uint64(1)

	firstHash, err := hashAuditEvent(ledger.Event{
		TS:      "2026-04-17T00:00:00Z",
		Type:    "test.audit",
		Session: "session-a",
		Data:    firstAuditData,
	})
	if err != nil {
		t.Fatalf("hash first audit event: %v", err)
	}

	secondHash, err := hashAuditEvent(ledger.Event{
		TS:      "2026-04-17T00:00:00Z",
		Type:    "test.audit",
		Session: "session-a",
		Data:    secondAuditData,
	})
	if err != nil {
		t.Fatalf("hash second audit event: %v", err)
	}

	if firstHash != secondHash {
		t.Fatalf("expected deterministic audit hash across map insertion order, got %q vs %q", firstHash, secondHash)
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
	var response controlapipkg.HookPreValidateResponse
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
