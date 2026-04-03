package memory

import (
	"path/filepath"
	"testing"
	"time"

	"morph/internal/audit"
	"morph/internal/ledger"
)

func TestRolloverContinuityThreads_SealsCurrentAndBuildsInspectCandidate(t *testing.T) {
	nowUTC := time.Date(2026, time.March, 12, 12, 0, 0, 0, time.UTC)
	continuityState := newContinuityThreadsState(nowUTC, ContinuityInspectionThresholds{
		SubmitPreviousMinEvents:       2,
		SubmitPreviousMinPayloadBytes: 1,
		SubmitPreviousMinPromptTokens: 1,
	})
	currentThreadID := continuityState.CurrentThreadID
	nextThreadID := continuityState.NextThreadID

	ledgerPath := filepath.Join(t.TempDir(), "ledger.jsonl")
	writeContinuityTestEvent(t, ledgerPath, currentThreadID, nowUTC.Add(1*time.Minute), "goal_opened", map[string]interface{}{
		"goal_id": "goal_status",
		"text":    "monitor github status",
	})
	writeContinuityTestEvent(t, ledgerPath, currentThreadID, nowUTC.Add(2*time.Minute), "provider_fact_observed", map[string]interface{}{
		"facts": map[string]interface{}{
			"status_indicator": "none",
		},
	})

	rolledState, inspectCandidate, err := RolloverContinuityThreads(continuityState, ledgerPath, nowUTC.Add(3*time.Minute), "session_end")
	if err != nil {
		t.Fatalf("roll continuity threads: %v", err)
	}
	if rolledState.PreviousThreadID != currentThreadID {
		t.Fatalf("expected previous thread %q, got %q", currentThreadID, rolledState.PreviousThreadID)
	}
	if rolledState.CurrentThreadID != nextThreadID {
		t.Fatalf("expected next thread %q to become current, got %q", nextThreadID, rolledState.CurrentThreadID)
	}
	if rolledState.NextThreadID == "" || rolledState.NextThreadID == nextThreadID {
		t.Fatalf("expected a fresh next thread, got %q", rolledState.NextThreadID)
	}
	sealedThreadRecord := rolledState.Threads[currentThreadID]
	if sealedThreadRecord.State != ContinuityThreadStateSealed {
		t.Fatalf("expected sealed current thread, got %q", sealedThreadRecord.State)
	}
	if sealedThreadRecord.EventCount != 2 {
		t.Fatalf("expected sealed thread event count 2, got %d", sealedThreadRecord.EventCount)
	}
	if inspectCandidate == nil {
		t.Fatal("expected inspect candidate after threshold crossing")
	}
	if inspectCandidate.ThreadID != currentThreadID {
		t.Fatalf("expected inspect candidate thread %q, got %q", currentThreadID, inspectCandidate.ThreadID)
	}
	if len(inspectCandidate.Events) != 2 {
		t.Fatalf("expected two continuity events in inspect candidate, got %d", len(inspectCandidate.Events))
	}
}

func TestRolloverContinuityThreads_SkipsEmptyCurrentThread(t *testing.T) {
	nowUTC := time.Date(2026, time.March, 12, 12, 0, 0, 0, time.UTC)
	continuityState := newContinuityThreadsState(nowUTC, ContinuityInspectionThresholds{
		SubmitPreviousMinEvents:       1,
		SubmitPreviousMinPayloadBytes: 1,
		SubmitPreviousMinPromptTokens: 1,
	})

	rolledState, inspectCandidate, err := RolloverContinuityThreads(continuityState, filepath.Join(t.TempDir(), "missing-ledger.jsonl"), nowUTC.Add(time.Minute), "session_end")
	if err != nil {
		t.Fatalf("roll empty continuity threads: %v", err)
	}
	if inspectCandidate != nil {
		t.Fatalf("expected no inspect candidate for empty current thread, got %#v", inspectCandidate)
	}
	if rolledState.CurrentThreadID != continuityState.CurrentThreadID {
		t.Fatalf("expected current thread id to remain unchanged, got %q", rolledState.CurrentThreadID)
	}
	if rolledState.PreviousThreadID != "" {
		t.Fatalf("expected no previous thread for empty rollover, got %q", rolledState.PreviousThreadID)
	}
}

func TestSummarizeContinuityThread_RejectsTamperedLedger(t *testing.T) {
	nowUTC := time.Date(2026, time.March, 12, 12, 0, 0, 0, time.UTC)
	ledgerPath := filepath.Join(t.TempDir(), "ledger.jsonl")
	threadID := "thread_test"
	writeContinuityTestEvent(t, ledgerPath, threadID, nowUTC.Add(1*time.Minute), "goal_opened", map[string]interface{}{
		"goal_id": "goal_status",
		"text":    "monitor github status",
	})

	tamperFirstLedgerLine(t, ledgerPath, func(rawEvent map[string]interface{}) {
		rawEvent["type"] = "memory.test.tampered"
	})

	_, _, err := SummarizeContinuityThread(ledgerPath, threadID)
	if err == nil {
		t.Fatal("expected tampered continuity ledger to fail closed")
	}
	if err != nil && err.Error() == "" {
		t.Fatal("expected non-empty ledger integrity error")
	}
}

func writeContinuityTestEvent(t *testing.T, ledgerPath string, threadID string, eventTime time.Time, continuityType string, payload map[string]interface{}) {
	t.Helper()

	ledgerEventData := AnnotateContinuityEvent(
		map[string]interface{}{},
		continuityType,
		MemoryScopeGlobal,
		EpistemicFlavorRemembered,
		nil,
		payload,
	)
	ledgerEventData = BindContinuityThread(ledgerEventData, threadID)

	if err := audit.RecordMustPersist(ledgerPath, ledger.Event{
		TS:      eventTime.UTC().Format(time.RFC3339Nano),
		Type:    "memory.test.continuity",
		Session: "session-test",
		Data:    ledgerEventData,
	}); err != nil {
		t.Fatalf("append continuity test event: %v", err)
	}
}
