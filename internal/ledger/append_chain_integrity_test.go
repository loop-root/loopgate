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

func TestCanonicalEventJSON_GoldenBytesAndHash(t *testing.T) {
	event := Event{
		V:       SchemaVersion,
		TS:      "2026-05-05T12:34:56.789Z",
		Type:    "ledger.canonical.golden",
		Session: "session-golden",
		Data: map[string]interface{}{
			"nested": map[string]interface{}{
				"name":  "nested",
				"inner": int64(7),
			},
			"previous_event_hash": "prev",
			"event_hash":          "must-be-stripped-before-hashing",
			"list": []interface{}{
				map[string]interface{}{
					"z": "zed",
					"a": "aye",
				},
				"tail",
			},
			"null":  nil,
			"bool":  true,
			"float": 1.5,
			"int":   int64(42),
			"alpha": "first",
		},
	}

	gotBytes, err := marshalCanonicalEventJSON(event, true)
	if err != nil {
		t.Fatalf("marshal canonical event: %v", err)
	}
	const wantJSON = `{"v":1,"ts":"2026-05-05T12:34:56.789Z","type":"ledger.canonical.golden","session":"session-golden","data":{"alpha":"first","bool":true,"float":1.5,"int":42,"list":[{"a":"aye","z":"zed"},"tail"],"nested":{"inner":7,"name":"nested"},"null":null,"previous_event_hash":"prev"}}`
	if string(gotBytes) != wantJSON {
		t.Fatalf("canonical JSON changed\nwant: %s\n got: %s", wantJSON, gotBytes)
	}

	gotHash, err := ComputeEventHash(event)
	if err != nil {
		t.Fatalf("compute event hash: %v", err)
	}
	const wantHash = "ad53d29c342358f8662c0dac3b8b3e02616730c9dcce0131a8934d6751c5290f"
	if gotHash != wantHash {
		t.Fatalf("canonical event hash changed: want %s got %s", wantHash, gotHash)
	}
}

func TestComputeEventHash_RejectsUnsafeJSONIntegers(t *testing.T) {
	cases := []struct {
		name  string
		value interface{}
	}{
		{name: "uint64", value: uint64(1<<53 + 1)},
		{name: "int64", value: int64(1 << 53)},
		{name: "negative int64", value: int64(-(1 << 53))},
		{name: "json number", value: json.Number("9007199254740993")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event := Event{
				TS:      "2026-05-05T00:00:00Z",
				Type:    "ledger.unsafe.integer",
				Session: "session-a",
				Data: map[string]interface{}{
					"unsafe": tc.value,
				},
			}

			_, err := ComputeEventHash(event)
			if err == nil {
				t.Fatal("expected unsafe integer to be rejected")
			}
			if !strings.Contains(err.Error(), "unsafe JSON integer") {
				t.Fatalf("expected unsafe JSON integer error, got %v", err)
			}
		})
	}
}

func TestReadVerifiedChainState_RejectsFractionalSequence(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "fractional-sequence.jsonl")
	event := Event{
		TS:      "2026-05-05T00:00:00Z",
		Type:    "ledger.bad.sequence",
		Session: "session-a",
		Data: map[string]interface{}{
			"ledger_sequence":     1.5,
			"previous_event_hash": "",
		},
	}
	eventHash, err := ComputeEventHash(event)
	if err != nil {
		t.Fatalf("compute event hash: %v", err)
	}
	event.Data["event_hash"] = eventHash
	lineBytes, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	lineBytes = append(lineBytes, '\n')
	if err := os.WriteFile(path, lineBytes, 0o600); err != nil {
		t.Fatalf("write fractional sequence ledger: %v", err)
	}

	fileHandle, err := os.Open(path)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer fileHandle.Close()

	_, _, err = ReadVerifiedChainState(fileHandle, "ledger_sequence")
	if !errors.Is(err, ErrLedgerIntegrity) {
		t.Fatalf("expected ErrLedgerIntegrity, got %v", err)
	}
	if !strings.Contains(err.Error(), "non-integer ledger_sequence") {
		t.Fatalf("expected non-integer sequence error, got %v", err)
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
