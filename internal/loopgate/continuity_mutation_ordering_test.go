package loopgate

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"morph/internal/config"
	"morph/internal/ledger"
)

func countNonEmptyJSONLLines(t *testing.T, path string) int {
	t.Helper()
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read %q: %v", path, err)
	}
	lineCount := 0
	for _, line := range strings.Split(string(rawBytes), "\n") {
		if strings.TrimSpace(line) != "" {
			lineCount++
		}
	}
	return lineCount
}

func countAllContinuityMutationLines(t *testing.T, memoryRoot string, legacyPath string) int {
	t.Helper()
	paths := newContinuityMemoryPaths(memoryRoot, legacyPath)
	return countNonEmptyJSONLLines(t, paths.ContinuityEventsPath) +
		countNonEmptyJSONLLines(t, paths.GoalEventsPath) +
		countNonEmptyJSONLLines(t, paths.ProfileEventsPath)
}

func TestMutateContinuityMemory_DoesNotLeaveReplayableMutationWhenAuditFails(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	before := countAllContinuityMutationLines(t, testDefaultPartitionRoot(t, server), server.memoryLegacyPath)

	origAppend := server.appendAuditEvent
	server.appendAuditEvent = func(ledgerPath string, ev ledger.Event) error {
		if ev.Type == "memory.continuity.inspected" {
			return errors.New("forced audit append failure")
		}
		return origAppend(ledgerPath, ev)
	}

	_, err := submitSyntheticObservedContinuityForTests(t, server, client.controlSessionID, testContinuityInspectRequest("inspect_audit_fail", "thread_audit_fail", "monitor audit ordering"))
	if err == nil {
		t.Fatal("expected inspect to fail when audit ledger append fails")
	}

	after := countAllContinuityMutationLines(t, testDefaultPartitionRoot(t, server), server.memoryLegacyPath)
	if after != before {
		t.Fatalf("continuity mutation jsonl grew despite failed audit (before=%d after=%d)", before, after)
	}
}

// TestMutateContinuityMemory_SaveFailureDoesNotCreateAmbiguousDurableState checks that a failed
// continuity snapshot write does not update the on-disk state file even when the audit ledger
// and continuity JSONL mutation log have already advanced (reload still reconciles from JSONL).
func TestMutateContinuityMemory_SaveFailureDoesNotCreateAmbiguousDurableState(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	if _, err := submitSyntheticObservedContinuityForTests(t, server, client.controlSessionID, testContinuityInspectRequest("inspect_sf_base", "thread_sf_base", "baseline goal")); err != nil {
		t.Fatalf("baseline inspect: %v", err)
	}

	paths := newContinuityMemoryPaths(testDefaultPartitionRoot(t, server), server.memoryLegacyPath)
	previousSnapshot, err := os.ReadFile(paths.CurrentStatePath)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	beforeMutationLines := countAllContinuityMutationLines(t, testDefaultPartitionRoot(t, server), server.memoryLegacyPath)

	server.saveMemoryState = func(string, continuityMemoryState, config.RuntimeConfig) error {
		return errors.New("forced continuity memory save failure")
	}

	_, err = submitSyntheticObservedContinuityForTests(t, server, client.controlSessionID, testContinuityInspectRequest("inspect_sf_second", "thread_sf_second", "second goal"))
	if err == nil {
		t.Fatal("expected second inspect to fail when saveMemoryState fails")
	}

	currentSnapshot, err := os.ReadFile(paths.CurrentStatePath)
	if err != nil {
		t.Fatalf("read snapshot after failure: %v", err)
	}
	if string(currentSnapshot) != string(previousSnapshot) {
		t.Fatal("persisted continuity snapshot changed after save failure")
	}
	afterMutationLines := countAllContinuityMutationLines(t, testDefaultPartitionRoot(t, server), server.memoryLegacyPath)
	if afterMutationLines <= beforeMutationLines {
		t.Fatalf("expected continuity jsonl to record mutation before save failure (before=%d after=%d)", beforeMutationLines, afterMutationLines)
	}
}

func TestContinuityReplay_RejectsOrRepairsOrphanedMutationSequence(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := submitSyntheticObservedContinuityForTests(t, server, client.controlSessionID, testContinuityInspectRequest("inspect_replay_corrupt", "thread_replay_corrupt", "seed continuity jsonl")); err != nil {
		t.Fatalf("seed inspect: %v", err)
	}
	paths := newContinuityMemoryPaths(testDefaultPartitionRoot(t, server), server.memoryLegacyPath)

	fileHandle, err := os.OpenFile(paths.ContinuityEventsPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open continuity events: %v", err)
	}
	if _, err := fileHandle.WriteString("not-a-valid-continuity-json-line\n"); err != nil {
		t.Fatalf("append corrupt line: %v", err)
	}
	if err := fileHandle.Close(); err != nil {
		t.Fatalf("close continuity events: %v", err)
	}

	_, err = loadContinuityMemoryState(testDefaultPartitionRoot(t, server), server.memoryLegacyPath)
	if err == nil {
		t.Fatal("expected continuity replay to reject corrupted jsonl")
	}
	if !strings.Contains(err.Error(), paths.ContinuityEventsPath) {
		t.Fatalf("expected replay error to include continuity path, got %v", err)
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("expected replay error to include corrupt line number, got %v", err)
	}
}

func TestEnsureMemoryPartitionLocked_CorruptReplayFailureIsExplicit(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := submitSyntheticObservedContinuityForTests(t, server, client.controlSessionID, testContinuityInspectRequest("inspect_partition_replay_corrupt", "thread_partition_replay_corrupt", "seed continuity jsonl")); err != nil {
		t.Fatalf("seed inspect: %v", err)
	}

	defaultPartitionRoot := testDefaultPartitionRoot(t, server)
	paths := newContinuityMemoryPaths(defaultPartitionRoot, server.memoryLegacyPath)
	fileHandle, err := os.OpenFile(paths.ContinuityEventsPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open continuity events: %v", err)
	}
	if _, err := fileHandle.WriteString("not-a-valid-continuity-json-line\n"); err != nil {
		t.Fatalf("append corrupt line: %v", err)
	}
	if err := fileHandle.Close(); err != nil {
		t.Fatalf("close continuity events: %v", err)
	}

	var warningEventCode string
	var warningCause error
	server.reportSecurityWarning = func(eventCode string, cause error) {
		warningEventCode = eventCode
		warningCause = cause
	}

	server.memoryMu.Lock()
	delete(server.memoryPartitions, memoryPartitionKey(""))
	_, err = server.ensureMemoryPartitionLocked("")
	server.memoryMu.Unlock()
	if err == nil {
		t.Fatal("expected corrupt continuity replay to deny partition load")
	}
	if !strings.Contains(err.Error(), "load continuity memory partition") {
		t.Fatalf("expected partition load context, got %v", err)
	}
	if !strings.Contains(err.Error(), memoryPartitionKey("")) {
		t.Fatalf("expected partition key in error, got %v", err)
	}
	if !strings.Contains(err.Error(), paths.ContinuityEventsPath) || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("expected replay location in error, got %v", err)
	}
	if warningEventCode != "continuity_partition_load_failed" {
		t.Fatalf("expected continuity_partition_load_failed warning, got %q", warningEventCode)
	}
	if warningCause == nil {
		t.Fatal("expected warning cause for corrupt replay")
	}
	if !strings.Contains(warningCause.Error(), paths.ContinuityEventsPath) || !strings.Contains(warningCause.Error(), "line 2") {
		t.Fatalf("expected warning cause to retain replay location, got %v", warningCause)
	}
}

func TestContinuityInspectRequest_RejectsTooManyEvents(t *testing.T) {
	events := make([]ContinuityEventInput, maxContinuityEventsPerInspection+1)
	sealed := time.Date(2026, 3, 12, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	for i := range events {
		events[i] = ContinuityEventInput{
			TimestampUTC:    sealed,
			SessionID:       "session-test",
			Type:            "goal_opened",
			Scope:           "global",
			ThreadID:        "thread_many",
			EpistemicFlavor: "remembered",
			LedgerSequence:  int64(i + 1),
			EventHash:       "hash",
			Payload:         map[string]interface{}{"goal_id": "g", "text": "x"},
		}
	}
	req := ContinuityInspectRequest{
		InspectionID: "inspect_many",
		ThreadID:     "thread_many",
		Scope:        "global",
		SealedAtUTC:  sealed,
		Events:       events,
	}
	if err := req.Validate(); err == nil {
		t.Fatal("expected validation error for too many events")
	}
}

func TestContinuityInspectRequest_RejectsOversizedApproxPayload(t *testing.T) {
	sealed := time.Date(2026, 3, 12, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	req := ContinuityInspectRequest{
		InspectionID:       "inspect_big",
		ThreadID:           "thread_big",
		Scope:              "global",
		SealedAtUTC:        sealed,
		ApproxPayloadBytes: maxContinuityInspectApproxPayloadBytes + 1,
		Events: []ContinuityEventInput{
			{
				TimestampUTC:    sealed,
				SessionID:       "session-test",
				Type:            "goal_opened",
				Scope:           "global",
				ThreadID:        "thread_big",
				EpistemicFlavor: "remembered",
				LedgerSequence:  1,
				EventHash:       "eventhash_big",
				Payload:         map[string]interface{}{"goal_id": "g1", "text": "small"},
			},
		},
	}
	if err := req.Validate(); err == nil {
		t.Fatal("expected validation error for oversized approx_payload_bytes")
	}
}
