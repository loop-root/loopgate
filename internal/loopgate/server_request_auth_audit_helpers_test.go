package loopgate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loopgate/internal/ledger"
)

func readLastAuditEventOfType(t *testing.T, repoRoot string, eventType string) ledger.Event {
	t.Helper()

	auditEvents := readAuditEventsOfType(t, repoRoot, eventType)
	if len(auditEvents) == 0 {
		t.Fatalf("expected audit event type %q", eventType)
	}
	return auditEvents[len(auditEvents)-1]
}

func readAuditEventsOfType(t *testing.T, repoRoot string, eventType string) []ledger.Event {
	t.Helper()

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(auditBytes)), "\n")
	auditEvents := make([]ledger.Event, 0, len(lines))
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		var auditEvent ledger.Event
		if err := json.Unmarshal([]byte(line), &auditEvent); err != nil {
			t.Fatalf("decode audit event: %v", err)
		}
		if auditEvent.Type == eventType {
			auditEvents = append(auditEvents, auditEvent)
		}
	}
	return auditEvents
}
