package loopgate

import (
	"bytes"
	"context"
	"encoding/json"
	"loopgate/internal/ledger"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type claudeHookSessionStateTestFile struct {
	SchemaVersion string                      `json:"schema_version"`
	Sessions      []claudeHookSessionTestWire `json:"sessions,omitempty"`
}

type claudeHookSessionTestWire struct {
	SessionID    string `json:"session_id"`
	State        string `json:"state"`
	StartedAtUTC string `json:"started_at_utc"`
	EndedAtUTC   string `json:"ended_at_utc,omitempty"`
	ExitReason   string `json:"exit_reason,omitempty"`
}

func postHookPreValidateForTest(t *testing.T, server *Server, payload map[string]interface{}) controlapipkg.HookPreValidateResponse {
	t.Helper()

	rawBody, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal hook payload: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", bytes.NewBuffer(rawBody))
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
	return response
}

func requireSymlinkForHookTest(t *testing.T, oldname string, newname string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symlink test skipped on windows")
	}
	if err := os.Symlink(oldname, newname); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
}

func readLastHookAuditEvent(t *testing.T, repoRoot string) ledger.Event {
	t.Helper()

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(auditBytes)), "\n")
	lastAuditEvent, ok := ledger.ParseEvent([]byte(lines[len(lines)-1]))
	if !ok {
		t.Fatalf("parse hook audit event: %s", lines[len(lines)-1])
	}
	return lastAuditEvent
}

func auditEventTypesContain(t *testing.T, repoRoot string, wantedType string) bool {
	t.Helper()

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(auditBytes)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		ev, ok := ledger.ParseEvent([]byte(line))
		if !ok {
			t.Fatalf("parse hook audit event: %s", line)
		}
		if ev.Type == wantedType {
			return true
		}
	}
	return false
}

func readClaudeHookSessionState(t *testing.T, repoRoot string) claudeHookSessionStateTestFile {
	t.Helper()

	stateBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "claude_hook_sessions.json"))
	if err != nil {
		t.Fatalf("read claude hook session state: %v", err)
	}
	var stateFile claudeHookSessionStateTestFile
	if err := json.Unmarshal(stateBytes, &stateFile); err != nil {
		t.Fatalf("decode claude hook session state: %v", err)
	}
	return stateFile
}

func claudeHookApprovalStateFileExists(t *testing.T, repoRoot string, sessionID string) bool {
	t.Helper()

	storageKey := claudeHookSessionStorageKey(sessionID)
	statePath := filepath.Join(repoRoot, "runtime", "state", "claude_hook_sessions", storageKey, claudeHookApprovalsFileName)
	_, err := os.Stat(statePath)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("stat claude hook approval state: %v", err)
	return false
}

func requireHookApprovalOptions(t *testing.T, got []string, want ...string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("approval options = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("approval options = %#v, want %#v", got, want)
		}
	}
}

func auditStringSlice(rawValue interface{}) []string {
	switch typedValue := rawValue.(type) {
	case []string:
		return typedValue
	case []interface{}:
		values := make([]string, 0, len(typedValue))
		for _, item := range typedValue {
			stringItem, ok := item.(string)
			if !ok {
				return nil
			}
			values = append(values, stringItem)
		}
		return values
	default:
		return nil
	}
}
