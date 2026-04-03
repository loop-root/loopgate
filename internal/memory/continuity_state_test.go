package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"morph/internal/audit"
	"morph/internal/ledger"
)

func TestLoadGlobalContinuityState_RejectsTamperedLedger(t *testing.T) {
	ledgerPath := filepath.Join(t.TempDir(), "ledger.jsonl")
	if err := audit.RecordMustPersist(ledgerPath, ledger.Event{
		TS:      time.Date(2026, time.March, 13, 6, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
		Type:    "memory.goal.opened",
		Session: "session-test",
		Data: AnnotateContinuityEvent(
			map[string]interface{}{"goal_id": "goal_live", "text": "monitor github status"},
			ContinuityEventTypeGoalOpened,
			MemoryScopeGlobal,
			EpistemicFlavorRemembered,
			nil,
			map[string]interface{}{"goal_id": "goal_live", "text": "monitor github status"},
		),
	}); err != nil {
		t.Fatalf("seed continuity ledger: %v", err)
	}

	tamperFirstLedgerLine(t, ledgerPath, func(rawEvent map[string]interface{}) {
		rawEvent["type"] = "memory.goal.tampered"
	})

	_, err := LoadGlobalContinuityState(ledgerPath)
	if err == nil {
		t.Fatal("expected tampered continuity ledger to fail closed")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "ledger integrity") {
		t.Fatalf("expected ledger integrity error, got %v", err)
	}
}

func tamperFirstLedgerLine(t *testing.T, ledgerPath string, mutate func(map[string]interface{})) {
	t.Helper()

	ledgerBytes, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	ledgerLines := strings.Split(strings.TrimSpace(string(ledgerBytes)), "\n")
	if len(ledgerLines) == 0 {
		t.Fatal("expected at least one ledger line")
	}

	var rawEvent map[string]interface{}
	if err := json.Unmarshal([]byte(ledgerLines[0]), &rawEvent); err != nil {
		t.Fatalf("unmarshal first ledger line: %v", err)
	}
	mutate(rawEvent)
	tamperedLine, err := json.Marshal(rawEvent)
	if err != nil {
		t.Fatalf("marshal tampered event: %v", err)
	}
	ledgerLines[0] = string(tamperedLine)
	if err := os.WriteFile(ledgerPath, []byte(strings.Join(ledgerLines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write tampered ledger: %v", err)
	}
}
