package memory

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"morph/internal/audit"
	"morph/internal/ledger"
)

func TestFinalizeSession_WritesKeyAndAuditsCreation(t *testing.T) {
	tmpDir := t.TempDir()
	ledgerPath := filepath.Join(tmpDir, "ledger.jsonl")
	keysPath := filepath.Join(tmpDir, "keys")

	if err := audit.RecordMustPersist(ledgerPath, ledger.Event{
		TS:      "2026-03-07T00:05:00Z",
		Type:    "memory.goal.opened",
		Session: "s-test",
		Data: map[string]interface{}{
			"goal_id": "goal_status",
			"text":    "monitor github service status",
			"continuity_event": map[string]interface{}{
				"type":             ContinuityEventTypeGoalOpened,
				"scope":            MemoryScopeGlobal,
				"epistemic_flavor": EpistemicFlavorRemembered,
				"payload": map[string]interface{}{
					"goal_id": "goal_status",
					"text":    "monitor github service status",
				},
			},
		},
	}); err != nil {
		t.Fatalf("seed continuity ledger event: %v", err)
	}

	err := FinalizeSession(
		Paths{
			KeysPath:   keysPath,
			LedgerPath: ledgerPath,
		},
		SessionState{
			SessionID:    "s-test",
			StartedAtUTC: "2026-03-07T00:00:00Z",
			TurnCount:    2,
		},
	)
	if err != nil {
		t.Fatalf("finalize session: %v", err)
	}

	keyFilePath := filepath.Join(keysPath, "s-test.json")
	keyBytes, err := os.ReadFile(keyFilePath)
	if err != nil {
		t.Fatalf("read key file: %v", err)
	}
	var keyDocument resonateKeyDocument
	if err := json.Unmarshal(keyBytes, &keyDocument); err != nil {
		t.Fatalf("parse key file: %v", err)
	}
	if keyDocument.ID != "rk-s-test" {
		t.Fatalf("unexpected key document: %#v", keyDocument)
	}
	if keyDocument.Scope != MemoryScopeGlobal {
		t.Fatalf("expected default global scope, got %#v", keyDocument)
	}
	if !slices.Contains(keyDocument.Tags, "github") || !slices.Contains(keyDocument.Tags, "status") {
		t.Fatalf("expected continuity tags on key document, got %#v", keyDocument.Tags)
	}

	ledgerFile, err := os.Open(ledgerPath)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer ledgerFile.Close()

	ledgerScanner := bufio.NewScanner(ledgerFile)
	var ledgerEvent map[string]interface{}
	for ledgerScanner.Scan() {
		if err := json.Unmarshal(ledgerScanner.Bytes(), &ledgerEvent); err != nil {
			t.Fatalf("parse ledger event: %v", err)
		}
		if ledgerEvent["type"] == "memory.resonate_key.created" {
			break
		}
		ledgerEvent = nil
	}
	if ledgerEvent == nil {
		t.Fatal("expected memory.resonate_key.created ledger entry")
	}
	ledgerEventData, ok := ledgerEvent["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected ledger event data map, got %#v", ledgerEvent["data"])
	}
	rawMemoryCandidate, ok := ledgerEventData[MemoryCandidateKey].(map[string]interface{})
	if !ok {
		t.Fatalf("expected memory_candidate annotation, got %#v", ledgerEventData)
	}
	if rawMemoryCandidate["type"] != MemoryCandidateTypeResonateKey {
		t.Fatalf("unexpected memory candidate type: %#v", rawMemoryCandidate["type"])
	}
	if rawMemoryCandidate["scope"] != MemoryScopeGlobal {
		t.Fatalf("unexpected memory candidate scope: %#v", rawMemoryCandidate["scope"])
	}
	if rawMemoryCandidate["epistemic_flavor"] != EpistemicFlavorRemembered {
		t.Fatalf("unexpected epistemic flavor: %#v", rawMemoryCandidate["epistemic_flavor"])
	}
	rawContinuityEvent, ok := ledgerEventData[ContinuityEventKey].(map[string]interface{})
	if !ok {
		t.Fatalf("expected continuity_event annotation, got %#v", ledgerEventData)
	}
	if rawContinuityEvent["type"] != ContinuityEventTypeResonateKeyCreated {
		t.Fatalf("unexpected continuity event type: %#v", rawContinuityEvent["type"])
	}
	if rawContinuityEvent["scope"] != MemoryScopeGlobal {
		t.Fatalf("unexpected continuity event scope: %#v", rawContinuityEvent["scope"])
	}
}

func TestFinalizeSession_RemovesKeyFileWhenLedgerAppendFails(t *testing.T) {
	tmpDir := t.TempDir()
	keysPath := filepath.Join(tmpDir, "keys")
	ledgerPath := filepath.Join(tmpDir, "missing", "ledger.jsonl")

	err := FinalizeSession(
		Paths{
			KeysPath:   keysPath,
			LedgerPath: ledgerPath,
		},
		SessionState{
			SessionID:    "s-test",
			StartedAtUTC: "2026-03-07T00:00:00Z",
			TurnCount:    2,
		},
	)
	if err == nil {
		t.Fatal("expected ledger append failure")
	}

	if _, statErr := os.Stat(filepath.Join(keysPath, "s-test.json")); !os.IsNotExist(statErr) {
		t.Fatalf("expected key file cleanup after ledger failure, got stat err %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(keysPath, "s-test.json.tmp")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no leftover temp file, got stat err %v", statErr)
	}
}
