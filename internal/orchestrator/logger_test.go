package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loopgate/internal/policy"
)

func TestLedgerLogger_RedactsToolArgsReasonAndOutput(t *testing.T) {
	ledgerPath := filepath.Join(t.TempDir(), "ledger.jsonl")
	logger := NewLedgerLogger(ledgerPath, "session-logger-test")

	call := ToolCall{
		ID:   "call-1",
		Name: "fs_write",
		Args: map[string]string{
			"path":          "notes.txt",
			"token":         "super-secret-token",
			"authorization": "Bearer hidden-token",
		},
	}
	logger.LogToolCall(call, policy.Allow, "Authorization: Bearer reason-secret")
	logger.LogToolResult(call, ToolResult{
		CallID: call.ID,
		Status: StatusSuccess,
		Output: "api_key=my-api-key",
		Reason: "refresh_token=top-secret-refresh",
	})

	events := readLedgerEventsForTest(t, ledgerPath)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	callEventData := events[0].Data
	argsValue, ok := callEventData["args"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected args map, got %#v", callEventData["args"])
	}
	if argsValue["token"] != "[REDACTED]" {
		t.Fatalf("expected token to be redacted, got %#v", argsValue["token"])
	}
	if strings.Contains(strings.ToLower(toString(argsValue["authorization"])), "hidden-token") {
		t.Fatalf("authorization arg leaked: %#v", argsValue["authorization"])
	}
	if strings.Contains(strings.ToLower(toString(callEventData["reason"])), "reason-secret") {
		t.Fatalf("reason leaked secret: %#v", callEventData["reason"])
	}

	resultEventData := events[1].Data
	if strings.Contains(strings.ToLower(toString(resultEventData["output"])), "my-api-key") {
		t.Fatalf("output leaked api key: %#v", resultEventData["output"])
	}
	if strings.Contains(strings.ToLower(toString(resultEventData["reason"])), "top-secret-refresh") {
		t.Fatalf("result reason leaked refresh token: %#v", resultEventData["reason"])
	}
}

func TestLedgerLogger_AppendFailureIsReported(t *testing.T) {
	nonexistentDirPath := filepath.Join(t.TempDir(), "missing", "ledger.jsonl")

	var reportedErr error
	logger := NewLedgerLogger(nonexistentDirPath, "session-logger-error")
	logger.ReportError = func(err error) {
		reportedErr = err
	}

	logger.LogToolCall(ToolCall{
		ID:   "call-err",
		Name: "fs_read",
		Args: map[string]string{"path": "README.md"},
	}, policy.Allow, "")

	if reportedErr == nil {
		t.Fatal("expected append failure to be reported")
	}
}

type testLedgerEvent struct {
	Data map[string]interface{} `json:"data"`
}

func readLedgerEventsForTest(t *testing.T, ledgerPath string) []testLedgerEvent {
	t.Helper()

	rawBytes, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatalf("read ledger file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(rawBytes)), "\n")
	events := make([]testLedgerEvent, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event testLedgerEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("unmarshal event line: %v", err)
		}
		events = append(events, event)
	}
	return events
}

func toString(rawValue interface{}) string {
	textValue, _ := rawValue.(string)
	return textValue
}
