package ledger

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

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
