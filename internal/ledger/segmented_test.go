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
)

func TestAppendWithRotation_ReturnsErrorWhenSyncFails(t *testing.T) {
	tmpDir := t.TempDir()
	activePath := filepath.Join(tmpDir, "loopgate_events.jsonl")
	rotationSettings := RotationSettings{
		MaxEventBytes: 8 * 1024,
	}

	syncFailure := errors.New("sync failed for test")
	restoreFileSync := useLedgerFileSyncForTest(func(fileHandle *os.File) error {
		return syncFailure
	})
	defer restoreFileSync()

	err := AppendWithRotation(activePath, Event{
		TS:      "2026-03-13T06:59:00Z",
		Type:    "audit.test",
		Session: "session-a",
		Data: map[string]interface{}{
			"payload": "first event",
		},
	}, rotationSettings)
	if !errors.Is(err, syncFailure) {
		t.Fatalf("expected sync failure, got %v", err)
	}
	if !strings.Contains(err.Error(), "sync ledger") {
		t.Fatalf("expected sync ledger context, got %v", err)
	}

	activeHandle, openErr := os.Open(activePath)
	if openErr != nil {
		t.Fatalf("open active ledger after sync failure: %v", openErr)
	}
	defer activeHandle.Close()

	activeFileInfo, statErr := activeHandle.Stat()
	if statErr != nil {
		t.Fatalf("stat active ledger after sync failure: %v", statErr)
	}
	activeFileState, fileStateErr := ledgerFileStateFromFileInfo(activeFileInfo)
	if fileStateErr != nil {
		t.Fatalf("load active ledger file state after sync failure: %v", fileStateErr)
	}
	if _, found := loadCachedChainState(normalizeLedgerPath(activePath), "ledger_sequence", activeFileState); found {
		t.Fatal("expected sync failure to clear cached chain state")
	}
}

func TestAppendWithRotation_RotatesAndContinuesChainAcrossSegments(t *testing.T) {
	tmpDir := t.TempDir()
	activePath := filepath.Join(tmpDir, "loopgate_events.jsonl")
	rotationSettings := RotationSettings{
		MaxEventBytes:                 8 * 1024,
		RotateAtBytes:                 550,
		SegmentDir:                    filepath.Join(tmpDir, "segments"),
		ManifestPath:                  filepath.Join(tmpDir, "segments", "manifest.jsonl"),
		VerifyClosedSegmentsOnStartup: true,
	}

	for eventIndex := 0; eventIndex < 5; eventIndex++ {
		if err := AppendWithRotation(activePath, Event{
			TS:      fmt.Sprintf("2026-03-13T07:00:%02dZ", eventIndex),
			Type:    "audit.test",
			Session: "session-a",
			Data: map[string]interface{}{
				"payload": strings.Repeat(string(rune('a'+eventIndex)), 140),
			},
		}, rotationSettings); err != nil {
			t.Fatalf("append rotated event %d: %v", eventIndex, err)
		}
	}

	lastSequence, lastEventHash, err := ReadSegmentedChainState(activePath, "ledger_sequence", rotationSettings)
	if err != nil {
		t.Fatalf("read segmented chain state: %v", err)
	}
	if lastSequence != 5 {
		t.Fatalf("expected last sequence 5, got %d", lastSequence)
	}
	if strings.TrimSpace(lastEventHash) == "" {
		t.Fatal("expected non-empty last event hash")
	}

	manifestEntries := readManifestEntriesForTest(t, rotationSettings.ManifestPath)
	if len(manifestEntries) == 0 {
		t.Fatal("expected at least one sealed segment manifest entry")
	}

	clearCachedChainState(normalizeLedgerPath(activePath), "ledger_sequence")
	if err := AppendWithRotation(activePath, Event{
		TS:      "2026-03-13T07:00:10Z",
		Type:    "audit.test",
		Session: "session-a",
		Data: map[string]interface{}{
			"payload": strings.Repeat("z", 140),
		},
	}, rotationSettings); err != nil {
		t.Fatalf("append after clearing cache: %v", err)
	}

	lastSequence, _, err = ReadSegmentedChainState(activePath, "ledger_sequence", rotationSettings)
	if err != nil {
		t.Fatalf("re-read segmented chain state: %v", err)
	}
	if lastSequence != 6 {
		t.Fatalf("expected resumed sequence 6, got %d", lastSequence)
	}
}

func TestReadSegmentedChainState_FailsClosedOnTamperedSealedSegment(t *testing.T) {
	tmpDir := t.TempDir()
	activePath := filepath.Join(tmpDir, "loopgate_events.jsonl")
	rotationSettings := RotationSettings{
		MaxEventBytes:                 8 * 1024,
		RotateAtBytes:                 550,
		SegmentDir:                    filepath.Join(tmpDir, "segments"),
		ManifestPath:                  filepath.Join(tmpDir, "segments", "manifest.jsonl"),
		VerifyClosedSegmentsOnStartup: true,
	}

	for eventIndex := 0; eventIndex < 4; eventIndex++ {
		if err := AppendWithRotation(activePath, Event{
			TS:      fmt.Sprintf("2026-03-13T07:10:%02dZ", eventIndex),
			Type:    "audit.test",
			Session: "session-a",
			Data: map[string]interface{}{
				"payload": strings.Repeat("x", 140),
			},
		}, rotationSettings); err != nil {
			t.Fatalf("append rotated event %d: %v", eventIndex, err)
		}
	}

	manifestEntries := readManifestEntriesForTest(t, rotationSettings.ManifestPath)
	if len(manifestEntries) == 0 {
		t.Fatal("expected sealed segment manifest entry")
	}

	sealedSegmentPath := filepath.Join(rotationSettings.SegmentDir, manifestEntries[0].SegmentFilename)
	sealedBytes, err := os.ReadFile(sealedSegmentPath)
	if err != nil {
		t.Fatalf("read sealed segment: %v", err)
	}
	tamperedBytes := append([]byte(nil), sealedBytes...)
	if len(tamperedBytes) < 2 {
		t.Fatal("expected sealed segment bytes to be non-trivial")
	}
	tamperedBytes[len(tamperedBytes)-2] = 'x'
	if err := os.WriteFile(sealedSegmentPath, tamperedBytes, 0o600); err != nil {
		t.Fatalf("tamper sealed segment: %v", err)
	}

	_, _, err = ReadSegmentedChainState(activePath, "ledger_sequence", rotationSettings)
	if !errors.Is(err, ErrLedgerIntegrity) {
		t.Fatalf("expected ErrLedgerIntegrity for tampered sealed segment, got %v", err)
	}
}

func TestReadSegmentedChainState_FailsClosedOnOrphanSealedSegment(t *testing.T) {
	tmpDir := t.TempDir()
	activePath := filepath.Join(tmpDir, "loopgate_events.jsonl")
	rotationSettings := RotationSettings{
		MaxEventBytes:                 8 * 1024,
		RotateAtBytes:                 550,
		SegmentDir:                    filepath.Join(tmpDir, "segments"),
		ManifestPath:                  filepath.Join(tmpDir, "segments", "manifest.jsonl"),
		VerifyClosedSegmentsOnStartup: true,
	}

	if err := os.MkdirAll(rotationSettings.SegmentDir, 0o700); err != nil {
		t.Fatalf("mkdir segment dir: %v", err)
	}
	orphanSegmentPath := filepath.Join(rotationSettings.SegmentDir, segmentFilename(1, 1))
	if err := os.WriteFile(orphanSegmentPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write orphan sealed segment: %v", err)
	}

	_, _, err := ReadSegmentedChainState(activePath, "ledger_sequence", rotationSettings)
	if !errors.Is(err, ErrLedgerIntegrity) {
		t.Fatalf("expected ErrLedgerIntegrity for orphan sealed segment, got %v", err)
	}
}

func TestAppendWithRotation_RejectsOversizedEventLine(t *testing.T) {
	tmpDir := t.TempDir()
	activePath := filepath.Join(tmpDir, "loopgate_events.jsonl")
	rotationSettings := RotationSettings{
		MaxEventBytes:                 256,
		RotateAtBytes:                 4 * 1024,
		SegmentDir:                    filepath.Join(tmpDir, "segments"),
		ManifestPath:                  filepath.Join(tmpDir, "segments", "manifest.jsonl"),
		VerifyClosedSegmentsOnStartup: true,
	}

	err := AppendWithRotation(activePath, Event{
		TS:      "2026-03-13T08:00:00Z",
		Type:    "audit.test",
		Session: "session-a",
		Data: map[string]interface{}{
			"payload": strings.Repeat("x", 512),
		},
	}, rotationSettings)
	if err == nil {
		t.Fatal("expected oversized event line to be denied")
	}
	if !strings.Contains(err.Error(), "max_event_bytes") {
		t.Fatalf("expected max_event_bytes error, got %v", err)
	}
}

func readManifestEntriesForTest(t *testing.T, manifestPath string) []SegmentManifestEntry {
	t.Helper()

	manifestFile, err := os.Open(manifestPath)
	if err != nil {
		t.Fatalf("open manifest: %v", err)
	}
	defer manifestFile.Close()

	manifestScanner := bufio.NewScanner(manifestFile)
	manifestEntries := []SegmentManifestEntry{}
	for manifestScanner.Scan() {
		var manifestEntry SegmentManifestEntry
		if err := json.Unmarshal(manifestScanner.Bytes(), &manifestEntry); err != nil {
			t.Fatalf("decode manifest entry: %v", err)
		}
		manifestEntries = append(manifestEntries, manifestEntry)
	}
	if err := manifestScanner.Err(); err != nil {
		t.Fatalf("scan manifest: %v", err)
	}
	return manifestEntries
}
