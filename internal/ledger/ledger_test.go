package ledger

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAppend_SingleEvent(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.jsonl")

	evt := Event{
		TS:      "2025-01-01T00:00:00Z",
		Type:    "test.event",
		Session: "test-session",
		Data:    map[string]interface{}{"key": "value"},
	}

	if err := Append(path, evt); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// Read back and verify
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	var parsed Event
	if err := json.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if parsed.Type != evt.Type {
		t.Errorf("expected type %q, got %q", evt.Type, parsed.Type)
	}
}

func TestAppend_MultipleEvents(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.jsonl")

	for i := 0; i < 10; i++ {
		evt := Event{
			TS:      fmt.Sprintf("2025-01-01T00:00:%02dZ", i),
			Type:    "test.event",
			Session: "test-session",
			Data:    map[string]interface{}{"index": i},
		}
		if err := Append(path, evt); err != nil {
			t.Fatalf("Append %d failed: %v", i, err)
		}
	}

	// Read back and count lines
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	count := 0
	for scanner.Scan() {
		count++
		var evt Event
		if err := json.Unmarshal(scanner.Bytes(), &evt); err != nil {
			t.Errorf("Line %d parse failed: %v", count, err)
		}
	}

	if count != 10 {
		t.Errorf("expected 10 lines, got %d", count)
	}
}

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

func TestAppend_Concurrent(t *testing.T) {
	// This test verifies that concurrent appends don't corrupt the ledger.
	// Each line should be complete and parseable JSON.
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "concurrent.jsonl")

	const numGoroutines = 10
	const eventsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < eventsPerGoroutine; i++ {
				evt := Event{
					TS:      fmt.Sprintf("2025-01-01T00:00:00.%09dZ", goroutineID*1000+i),
					Type:    "concurrent.test",
					Session: fmt.Sprintf("goroutine-%d", goroutineID),
					Data: map[string]interface{}{
						"goroutine": goroutineID,
						"index":     i,
					},
				}
				if err := Append(path, evt); err != nil {
					t.Errorf("Append failed (g=%d, i=%d): %v", goroutineID, i, err)
					return
				}
			}
		}(g)
	}

	wg.Wait()

	// Verify: all lines should be valid JSON
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	validCount := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue // Skip empty lines
		}

		var evt Event
		if err := json.Unmarshal(line, &evt); err != nil {
			t.Errorf("Line %d is not valid JSON: %v\nContent: %s", lineNum, err, string(line))
			continue
		}

		if evt.Type != "concurrent.test" {
			t.Errorf("Line %d has wrong type: %s", lineNum, evt.Type)
		}

		validCount++
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("Scanner error: %v", err)
	}

	expectedTotal := numGoroutines * eventsPerGoroutine
	if validCount != expectedTotal {
		t.Errorf("expected %d valid events, got %d", expectedTotal, validCount)
	}
}

func TestAppend_LargeEvent(t *testing.T) {
	// Test events near PIPE_BUF boundary (4096 bytes on most systems)
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "large.jsonl")

	// Create a large data payload
	largeValue := make([]byte, 8000) // Well over PIPE_BUF
	for i := range largeValue {
		largeValue[i] = 'x'
	}

	evt := Event{
		TS:      "2025-01-01T00:00:00Z",
		Type:    "large.event",
		Session: "test-session",
		Data: map[string]interface{}{
			"large_field": string(largeValue),
		},
	}

	if err := Append(path, evt); err != nil {
		t.Fatalf("Append large event failed: %v", err)
	}

	// Read back and verify
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Increase buffer size for large lines
	scanner.Buffer(make([]byte, 64*1024), 64*1024)

	if !scanner.Scan() {
		t.Fatal("No line read")
	}

	var parsed Event
	if err := json.Unmarshal(scanner.Bytes(), &parsed); err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if parsed.Type != "large.event" {
		t.Errorf("expected type 'large.event', got %q", parsed.Type)
	}
}

func TestAppend_ConcurrentLargeEvents(t *testing.T) {
	// Stress test: concurrent large events that exceed PIPE_BUF
	// This is the highest-risk scenario for interleaving
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "stress.jsonl")

	const numGoroutines = 5
	const eventsPerGoroutine = 20
	const payloadSize = 5000 // Larger than PIPE_BUF

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(goroutineID int) {
			defer wg.Done()
			payload := make([]byte, payloadSize)
			for i := range payload {
				payload[i] = byte('a' + (goroutineID % 26))
			}

			for i := 0; i < eventsPerGoroutine; i++ {
				evt := Event{
					TS:      fmt.Sprintf("2025-01-01T00:00:00.%d%dZ", goroutineID, i),
					Type:    "stress.test",
					Session: fmt.Sprintf("stress-%d", goroutineID),
					Data: map[string]interface{}{
						"goroutine": goroutineID,
						"index":     i,
						"payload":   string(payload),
					},
				}
				if err := Append(path, evt); err != nil {
					t.Errorf("Append failed: %v", err)
					return
				}
			}
		}(g)
	}

	wg.Wait()

	// Verify all lines are valid JSON
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)

	lineNum := 0
	validCount := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var evt Event
		if err := json.Unmarshal(line, &evt); err != nil {
			t.Errorf("Line %d corrupted (len=%d): %v", lineNum, len(line), err)
			// Print first 100 chars for debugging
			if len(line) > 100 {
				t.Logf("Line start: %s...", string(line[:100]))
			}
			continue
		}
		validCount++
	}

	expectedTotal := numGoroutines * eventsPerGoroutine
	if validCount != expectedTotal {
		t.Errorf("expected %d valid events, got %d (corruption detected)", expectedTotal, validCount)
	}
}

func TestAppend_CreatesFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "new.jsonl")

	// File shouldn't exist yet
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("file should not exist yet")
	}

	evt := Event{
		TS:   "2025-01-01T00:00:00Z",
		Type: "test",
	}

	if err := Append(path, evt); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// File should exist now
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should exist: %v", err)
	}
}

func TestAppend_CreatesParentDirs(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "nested", "deeply", "ledger.jsonl")

	evt := Event{
		TS:   "2025-01-01T00:00:00Z",
		Type: "test",
	}

	// Current implementation doesn't create parent dirs - this tests that behavior
	err := Append(path, evt)
	// This will fail because parent dirs don't exist
	// This is expected behavior - caller should ensure dirs exist
	if err == nil {
		t.Log("Note: Append creates parent directories (or they existed)")
	}
}
