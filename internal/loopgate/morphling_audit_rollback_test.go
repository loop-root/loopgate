package loopgate

import (
	"errors"
	"strings"
	"testing"
	"time"

	"morph/internal/ledger"
)

func seedMorphlingRecordForAuditRollback(t *testing.T, server *Server, record morphlingRecord) {
	t.Helper()

	server.morphlingsMu.Lock()
	defer server.morphlingsMu.Unlock()

	workingRecords := cloneMorphlingRecords(server.morphlings)
	workingRecords[record.MorphlingID] = record
	if err := saveMorphlingRecords(server.morphlingPath, workingRecords, server.morphlingStateKey); err != nil {
		t.Fatalf("seed morphling record: %v", err)
	}
	server.morphlings = workingRecords
}

func testMorphlingRecord(state string) morphlingRecord {
	nowUTC := time.Date(2026, time.March, 25, 12, 0, 0, 0, time.UTC)
	return morphlingRecord{
		SchemaVersion:          "loopgate.morphling.v2",
		MorphlingID:            "morphling-audit-test",
		TaskID:                 "task-audit-test",
		RequestID:              "req-audit-test",
		ParentControlSessionID: "session_abc123",
		ActorLabel:             "test_actor",
		ClientSessionLabel:     "test_client",
		Class:                  "reviewer",
		GoalText:               "Audit rollback test",
		GoalHMAC:               strings.Repeat("a", 64),
		GoalHint:               "Audit rollback test",
		State:                  state,
		StatusText:             morphlingStatusText(morphlingRecord{State: state}),
		WorkingDirRelativePath: "agents/task-audit-test",
		RequestedCapabilities:  []string{"fs_list", "fs_read"},
		GrantedCapabilities:    []string{"fs_list", "fs_read"},
		RequiresReview:         true,
		TimeBudgetSeconds:      300,
		TokenBudget:            50000,
		CreatedAtUTC:           nowUTC.Format(time.RFC3339Nano),
		SpawnedAtUTC:           nowUTC.Format(time.RFC3339Nano),
		LastEventAtUTC:         nowUTC.Format(time.RFC3339Nano),
		TokenExpiryUTC:         nowUTC.Add(5 * time.Minute).Format(time.RFC3339Nano),
	}
}

func TestStartMorphlingWorkerRollsBackWhenAuditUnavailable(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	record := testMorphlingRecord(morphlingStateSpawned)
	seedMorphlingRecordForAuditRollback(t, server, record)

	originalAppend := server.appendAuditEvent
	server.appendAuditEvent = func(ledgerPath string, auditEvent ledger.Event) error {
		if auditEvent.Type == "morphling.execution_started" {
			return errors.New("forced morphling execution_started audit failure")
		}
		return originalAppend(ledgerPath, auditEvent)
	}

	_, err := server.startMorphlingWorker(morphlingWorkerSession{
		MorphlingID:            record.MorphlingID,
		ControlSessionID:       "worker-session",
		ParentControlSessionID: record.ParentControlSessionID,
	}, MorphlingWorkerStartRequest{
		StatusText:    "running task",
		MemoryStrings: []string{"memory one"},
	})
	if err == nil || !strings.Contains(err.Error(), errMorphlingAuditUnavailable.Error()) {
		t.Fatalf("expected morphling audit unavailable error, got %v", err)
	}

	rolledBackRecord := server.morphlings[record.MorphlingID]
	if rolledBackRecord.State != morphlingStateSpawned {
		t.Fatalf("expected spawned state after audit rollback, got %#v", rolledBackRecord)
	}
	if rolledBackRecord.StatusText != morphlingStatusText(record) {
		t.Fatalf("expected original status text after audit rollback, got %#v", rolledBackRecord)
	}
}

func TestCompleteMorphlingTerminationRollsBackWhenAuditUnavailable(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	record := testMorphlingRecord(morphlingStateTerminating)
	record.Outcome = morphlingOutcomeCancelled
	record.TerminationReason = morphlingReasonOperatorCancelled
	seedMorphlingRecordForAuditRollback(t, server, record)

	originalAppend := server.appendAuditEvent
	server.appendAuditEvent = func(ledgerPath string, auditEvent ledger.Event) error {
		if auditEvent.Type == "morphling.terminated" {
			return errors.New("forced morphling terminated audit failure")
		}
		return originalAppend(ledgerPath, auditEvent)
	}

	_, err := server.completeMorphlingTermination(record.ParentControlSessionID, record.MorphlingID)
	if err == nil || !strings.Contains(err.Error(), errMorphlingAuditUnavailable.Error()) {
		t.Fatalf("expected morphling audit unavailable error, got %v", err)
	}

	rolledBackRecord := server.morphlings[record.MorphlingID]
	if rolledBackRecord.State != morphlingStateTerminating {
		t.Fatalf("expected terminating state after audit rollback, got %#v", rolledBackRecord)
	}
	if strings.TrimSpace(rolledBackRecord.TerminatedAtUTC) != "" {
		t.Fatalf("expected cleared terminated timestamp after audit rollback, got %#v", rolledBackRecord)
	}
}
