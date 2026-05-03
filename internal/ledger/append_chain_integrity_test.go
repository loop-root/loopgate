package ledger

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppend_AddsPersistentChainMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "chain.jsonl")

	for index := 0; index < 2; index++ {
		if err := Append(path, Event{
			TS:      fmt.Sprintf("2025-01-01T00:00:0%dZ", index),
			Type:    "chain.event",
			Session: "test-session",
			Data:    map[string]interface{}{"index": index},
		}); err != nil {
			t.Fatalf("append event %d: %v", index, err)
		}
	}

	fileHandle, err := os.Open(path)
	if err != nil {
		t.Fatalf("open chain ledger: %v", err)
	}
	defer fileHandle.Close()

	scanner := bufio.NewScanner(fileHandle)
	var previousEventHash string
	expectedSequence := 1
	for scanner.Scan() {
		parsedEvent, ok := ParseEvent(scanner.Bytes())
		if !ok {
			t.Fatalf("failed to parse ledger event: %s", scanner.Bytes())
		}

		sequenceValue, found := parsedEvent.Data["ledger_sequence"].(float64)
		if !found || int(sequenceValue) != expectedSequence {
			t.Fatalf("expected ledger_sequence=%d, got %#v", expectedSequence, parsedEvent.Data["ledger_sequence"])
		}
		previousHashValue, _ := parsedEvent.Data["previous_event_hash"].(string)
		if previousHashValue != previousEventHash {
			t.Fatalf("expected previous_event_hash %q, got %q", previousEventHash, previousHashValue)
		}
		eventHashValue, _ := parsedEvent.Data["event_hash"].(string)
		if eventHashValue == "" {
			t.Fatalf("expected event_hash, got %#v", parsedEvent.Data)
		}
		previousEventHash = eventHashValue
		expectedSequence++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan chain ledger: %v", err)
	}

	if err := Append(path, Event{
		TS:      "2025-01-01T00:00:02Z",
		Type:    "chain.event",
		Session: "test-session",
		Data:    map[string]interface{}{"index": 2},
	}); err != nil {
		t.Fatalf("append resumed event: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read resumed ledger: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	var lastEvent Event
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &lastEvent); err != nil {
		t.Fatalf("decode last event: %v", err)
	}
	if sequenceValue, _ := lastEvent.Data["ledger_sequence"].(float64); int(sequenceValue) != 3 {
		t.Fatalf("expected resumed ledger_sequence=3, got %#v", lastEvent.Data["ledger_sequence"])
	}
}

func TestComputeEventHash_DeterministicAcrossMapInsertionOrder(t *testing.T) {
	firstEventData := map[string]interface{}{}
	firstEventData["name"] = "deterministic"
	firstNestedData := map[string]interface{}{}
	firstNestedData["zeta"] = "tail"
	firstNestedChild := map[string]interface{}{}
	firstNestedChild["second"] = "two"
	firstNestedChild["first"] = "one"
	firstNestedData["child"] = firstNestedChild
	firstEventData["nested"] = firstNestedData
	firstEventData["list"] = []interface{}{
		map[string]interface{}{
			"beta":  2,
			"alpha": 1,
		},
		"done",
	}
	firstEventData["event_hash"] = "placeholder-one"

	secondEventData := map[string]interface{}{}
	secondEventData["event_hash"] = "placeholder-two"
	secondEventData["list"] = []interface{}{
		map[string]interface{}{
			"alpha": 1,
			"beta":  2,
		},
		"done",
	}
	secondNestedData := map[string]interface{}{}
	secondNestedChild := map[string]interface{}{}
	secondNestedChild["first"] = "one"
	secondNestedChild["second"] = "two"
	secondNestedData["child"] = secondNestedChild
	secondNestedData["zeta"] = "tail"
	secondEventData["nested"] = secondNestedData
	secondEventData["name"] = "deterministic"

	firstHash, err := ComputeEventHash(Event{
		TS:      "2026-04-17T00:00:00Z",
		Type:    "test.event",
		Session: "session-a",
		Data:    firstEventData,
	})
	if err != nil {
		t.Fatalf("compute first hash: %v", err)
	}

	secondHash, err := ComputeEventHash(Event{
		TS:      "2026-04-17T00:00:00Z",
		Type:    "test.event",
		Session: "session-a",
		Data:    secondEventData,
	})
	if err != nil {
		t.Fatalf("compute second hash: %v", err)
	}

	if firstHash != secondHash {
		t.Fatalf("expected deterministic hash across map insertion order, got %q vs %q", firstHash, secondHash)
	}
}

func TestAppend_FailsClosedOnMalformedPriorLedgerLine(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "corrupt.jsonl")

	if err := os.WriteFile(path, []byte("{not-json}\n"), 0o600); err != nil {
		t.Fatalf("write corrupt ledger: %v", err)
	}

	err := Append(path, Event{
		TS:      "2025-01-01T00:00:00Z",
		Type:    "test.event",
		Session: "test-session",
		Data:    map[string]interface{}{"key": "value"},
	})
	if !errors.Is(err, ErrLedgerIntegrity) {
		t.Fatalf("expected ErrLedgerIntegrity, got %v", err)
	}

	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read corrupt ledger: %v", readErr)
	}
	if strings.TrimSpace(string(content)) != "{not-json}" {
		t.Fatalf("expected corrupt ledger content to remain unchanged, got %q", string(content))
	}
}

func TestAppend_FailsClosedOnTamperedPriorLedgerChain(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "tampered.jsonl")

	for index := 0; index < 2; index++ {
		if err := Append(path, Event{
			TS:      fmt.Sprintf("2025-01-01T00:00:0%dZ", index),
			Type:    "chain.event",
			Session: "test-session",
			Data:    map[string]interface{}{"index": index},
		}); err != nil {
			t.Fatalf("append event %d: %v", index, err)
		}
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 ledger lines, got %d", len(lines))
	}

	var tamperedEvent Event
	if err := json.Unmarshal([]byte(lines[0]), &tamperedEvent); err != nil {
		t.Fatalf("decode first event: %v", err)
	}
	tamperedEvent.Type = "chain.event.tampered"
	tamperedLineBytes, err := json.Marshal(tamperedEvent)
	if err != nil {
		t.Fatalf("marshal tampered event: %v", err)
	}
	lines[0] = string(tamperedLineBytes)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write tampered ledger: %v", err)
	}

	err = Append(path, Event{
		TS:      "2025-01-01T00:00:02Z",
		Type:    "chain.event",
		Session: "test-session",
		Data:    map[string]interface{}{"index": 2},
	})
	if !errors.Is(err, ErrLedgerIntegrity) {
		t.Fatalf("expected ErrLedgerIntegrity, got %v", err)
	}
}

func TestAppend_ReverifiesAfterExternalMetadataTouch(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "touched.jsonl")

	if err := Append(path, Event{
		TS:      "2025-01-01T00:00:00Z",
		Type:    "test.event",
		Session: "test-session",
		Data:    map[string]interface{}{"index": 0},
	}); err != nil {
		t.Fatalf("append initial event: %v", err)
	}

	touchedTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, touchedTime, touchedTime); err != nil {
		t.Fatalf("touch ledger file: %v", err)
	}

	if err := Append(path, Event{
		TS:      "2025-01-01T00:00:01Z",
		Type:    "test.event",
		Session: "test-session",
		Data:    map[string]interface{}{"index": 1},
	}); err != nil {
		t.Fatalf("append after metadata touch: %v", err)
	}

	fileHandle, err := os.Open(path)
	if err != nil {
		t.Fatalf("open touched ledger: %v", err)
	}
	defer fileHandle.Close()

	lastSequence, _, err := ReadVerifiedChainState(fileHandle, "ledger_sequence")
	if err != nil {
		t.Fatalf("verify touched ledger chain: %v", err)
	}
	if lastSequence != 2 {
		t.Fatalf("expected touched ledger sequence 2, got %d", lastSequence)
	}
}
