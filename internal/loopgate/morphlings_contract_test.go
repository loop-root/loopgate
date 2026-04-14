package loopgate

import (
	"context"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"morph/internal/sandbox"
)

func TestMorphlingConcurrentSpawnsCompeteForLastSlot(t *testing.T) {
	repoRoot := t.TempDir()
	clientOne, status, server := startLoopgateServer(t, repoRoot, loopgateMorphlingPolicyYAML(false, true, 1))
	clientTwo := NewClient(server.socketPath)
	clientTwo.ConfigureSession("test-actor-two", "test-session-two", advertisedSessionCapabilityNames(status))

	spawnRequest := MorphlingSpawnRequest{
		Class:                 "reviewer",
		Goal:                  "Inspect the repository",
		RequestedCapabilities: []string{"fs_list", "fs_read"},
	}

	var leftResponse MorphlingSpawnResponse
	var rightResponse MorphlingSpawnResponse
	var leftErr error
	var rightErr error
	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	go func() {
		defer waitGroup.Done()
		leftResponse, leftErr = clientOne.SpawnMorphling(context.Background(), spawnRequest)
	}()
	go func() {
		defer waitGroup.Done()
		rightResponse, rightErr = clientTwo.SpawnMorphling(context.Background(), spawnRequest)
	}()
	waitGroup.Wait()

	if leftErr != nil {
		t.Fatalf("first concurrent spawn returned transport error: %v", leftErr)
	}
	if rightErr != nil {
		t.Fatalf("second concurrent spawn returned transport error: %v", rightErr)
	}

	successCount := 0
	deniedCount := 0
	for _, response := range []MorphlingSpawnResponse{leftResponse, rightResponse} {
		switch response.Status {
		case ResponseStatusSuccess:
			successCount++
			if strings.TrimSpace(response.MorphlingID) == "" {
				t.Fatalf("successful spawn must mint morphling_id, got %#v", response)
			}
		case ResponseStatusDenied:
			deniedCount++
			if response.DenialCode != DenialCodeMorphlingActiveLimitReached {
				t.Fatalf("expected active-limit denial, got %#v", response)
			}
			if strings.TrimSpace(response.MorphlingID) != "" {
				t.Fatalf("denied spawn must not mint morphling_id, got %#v", response)
			}
		default:
			t.Fatalf("unexpected concurrent spawn response %#v", response)
		}
	}
	if successCount != 1 || deniedCount != 1 {
		t.Fatalf("expected one success and one denial, got success=%d denial=%d", successCount, deniedCount)
	}

	leftStatusResponse, err := clientOne.MorphlingStatus(context.Background(), MorphlingStatusRequest{})
	if err != nil {
		t.Fatalf("left morphling status after concurrent spawns: %v", err)
	}
	rightStatusResponse, err := clientTwo.MorphlingStatus(context.Background(), MorphlingStatusRequest{})
	if err != nil {
		t.Fatalf("right morphling status after concurrent spawns: %v", err)
	}
	if leftStatusResponse.ActiveCount != 1 {
		t.Fatalf("expected global active count of one from left client, got %#v", leftStatusResponse)
	}
	if rightStatusResponse.ActiveCount != 1 {
		t.Fatalf("expected global active count of one from right client, got %#v", rightStatusResponse)
	}
	if len(leftStatusResponse.Morphlings)+len(rightStatusResponse.Morphlings) != 1 {
		t.Fatalf("expected exactly one session-visible morphling after concurrent spawns, left=%#v right=%#v", leftStatusResponse, rightStatusResponse)
	}
}

func TestMorphlingDeniedSpawnNeverMintsID(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgateMorphlingPolicyYAML(false, false, 5))

	deniedResponse, err := client.SpawnMorphling(context.Background(), MorphlingSpawnRequest{
		Class:                 "reviewer",
		Goal:                  "Attempt a disabled morphling spawn",
		RequestedCapabilities: []string{"fs_list", "fs_read"},
	})
	if err != nil {
		t.Fatalf("disabled morphling spawn returned transport error: %v", err)
	}
	if deniedResponse.Status != ResponseStatusDenied {
		t.Fatalf("expected denied morphling spawn, got %#v", deniedResponse)
	}
	if deniedResponse.DenialCode != DenialCodeMorphlingSpawnDisabled {
		t.Fatalf("expected morphling spawn disabled denial, got %#v", deniedResponse)
	}
	if strings.TrimSpace(deniedResponse.MorphlingID) != "" || strings.TrimSpace(deniedResponse.TaskID) != "" {
		t.Fatalf("request-level denial must not mint morphling or task ids, got %#v", deniedResponse)
	}
}

func TestMorphlingDeniedApprovalUsesCancelledOutcome(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgateMorphlingPolicyYAML(false, true, 5))

	pendingResponse, err := client.SpawnMorphling(context.Background(), MorphlingSpawnRequest{
		Class:                 "editor",
		Goal:                  "Write a sandboxed patch",
		RequestedCapabilities: []string{"fs_list", "fs_read", "fs_write"},
	})
	if err != nil {
		t.Fatalf("pending morphling spawn: %v", err)
	}
	if pendingResponse.Status != ResponseStatusPendingApproval {
		t.Fatalf("expected pending approval morphling response, got %#v", pendingResponse)
	}

	approvalResponse, err := client.UIDecideApproval(context.Background(), pendingResponse.ApprovalID, false)
	if err != nil {
		t.Fatalf("deny pending morphling spawn approval: %v", err)
	}
	if approvalResponse.Status != ResponseStatusDenied {
		t.Fatalf("expected denied approval response, got %#v", approvalResponse)
	}

	statusResponse, err := client.MorphlingStatus(context.Background(), MorphlingStatusRequest{
		MorphlingID:       pendingResponse.MorphlingID,
		IncludeTerminated: true,
	})
	if err != nil {
		t.Fatalf("morphling status after denied approval: %v", err)
	}
	if len(statusResponse.Morphlings) != 1 {
		t.Fatalf("expected terminated morphling after denied approval, got %#v", statusResponse)
	}
	if statusResponse.Morphlings[0].State != morphlingStateTerminated {
		t.Fatalf("expected terminated morphling after denied approval, got %#v", statusResponse.Morphlings[0])
	}
	if statusResponse.Morphlings[0].Outcome != morphlingOutcomeCancelled {
		t.Fatalf("expected cancelled outcome after denied approval, got %#v", statusResponse.Morphlings[0])
	}
	if statusResponse.Morphlings[0].Outcome == "denied" {
		t.Fatalf("instantiated morphling must not use outcome denied, got %#v", statusResponse.Morphlings[0])
	}
}

func TestMorphlingPreSpawnTerminationEmitsSingleAuditEvent(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgateMorphlingPolicyYAML(false, true, 5))

	pendingResponse, err := client.SpawnMorphling(context.Background(), MorphlingSpawnRequest{
		Class:                 "editor",
		Goal:                  "Prepare a sandboxed write",
		RequestedCapabilities: []string{"fs_list", "fs_read", "fs_write"},
	})
	if err != nil {
		t.Fatalf("pending morphling spawn: %v", err)
	}
	if pendingResponse.Status != ResponseStatusPendingApproval {
		t.Fatalf("expected pending approval morphling response, got %#v", pendingResponse)
	}
	if _, err := client.UIDecideApproval(context.Background(), pendingResponse.ApprovalID, false); err != nil {
		t.Fatalf("deny pending morphling spawn approval: %v", err)
	}

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read loopgate audit log: %v", err)
	}

	terminatedEventCount := 0
	for _, auditLine := range strings.Split(strings.TrimSpace(string(auditBytes)), "\n") {
		if strings.TrimSpace(auditLine) == "" {
			continue
		}
		var auditEvent map[string]interface{}
		if err := json.Unmarshal([]byte(auditLine), &auditEvent); err != nil {
			t.Fatalf("decode audit line: %v", err)
		}
		if auditEvent["type"] != "morphling.terminated" {
			continue
		}
		eventData, _ := auditEvent["data"].(map[string]interface{})
		if eventData["morphling_id"] != pendingResponse.MorphlingID {
			continue
		}
		terminatedEventCount++
		if _, hasEvidencePath := eventData["virtual_evidence_path"]; hasEvidencePath {
			t.Fatalf("pre-spawn termination must not emit a virtual_evidence_path, got %#v", auditEvent)
		}
	}
	if terminatedEventCount != 1 {
		t.Fatalf("expected exactly one morphling.terminated event for pre-spawn termination, got %d", terminatedEventCount)
	}
}

func TestMorphlingRestartDuringTerminatingDoesNotLeakCapacity(t *testing.T) {
	repoRoot := t.TempDir()
	writeSignedTestPolicyYAML(t, repoRoot, loopgateMorphlingPolicyYAML(false, true, 1))
	writeTestMorphlingClassPolicy(t, repoRoot)

	nowUTC := time.Date(2026, time.March, 11, 18, 0, 0, 0, time.UTC)
	morphlingPath := filepath.Join(repoRoot, "runtime", "state", "loopgate_morphlings.json")
	if err := saveMorphlingRecords(morphlingPath, map[string]morphlingRecord{
		"morphling-recover": {
			SchemaVersion:          "loopgate.morphling.v2",
			MorphlingID:            "morphling-recover",
			TaskID:                 "task-recover",
			RequestID:              "req-recover",
			ParentControlSessionID: "session-recover",
			Class:                  "reviewer",
			GoalText:               "Recover the terminating morphling",
			GoalHMAC:               strings.Repeat("a", 64),
			GoalHint:               "Recover the terminating morphling",
			State:                  morphlingStateTerminating,
			Outcome:                morphlingOutcomeCancelled,
			WorkingDirRelativePath: "agents/task-recover",
			RequestedCapabilities:  []string{"fs_list", "fs_read"},
			GrantedCapabilities:    []string{"fs_list", "fs_read"},
			RequiresReview:         true,
			TimeBudgetSeconds:      300,
			TokenBudget:            50000,
			CreatedAtUTC:           nowUTC.Format(time.RFC3339Nano),
			SpawnedAtUTC:           nowUTC.Format(time.RFC3339Nano),
			LastEventAtUTC:         nowUTC.Format(time.RFC3339Nano),
			TokenExpiryUTC:         nowUTC.Add(5 * time.Minute).Format(time.RFC3339Nano),
			TerminationReason:      morphlingReasonOperatorCancelled,
		},
	}, nil); err != nil {
		t.Fatalf("seed terminating morphling state: %v", err)
	}

	socketFile, err := os.CreateTemp("", "loopgate-*.sock")
	if err != nil {
		t.Fatalf("create temp socket file: %v", err)
	}
	socketPath := socketFile.Name()
	_ = socketFile.Close()
	_ = os.Remove(socketPath)
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new loopgate server after terminating restart: %v", err)
	}
	server.sessionOpenMinInterval = 0
	recoveredRecord := server.morphlings["morphling-recover"]
	if recoveredRecord.State != morphlingStateTerminated {
		t.Fatalf("expected restart recovery to finish termination, got %#v", recoveredRecord)
	}
	if server.activeMorphlingCount(time.Now().UTC()) != 0 {
		t.Fatalf("expected zero active morphlings after recovering terminating record, got %d", server.activeMorphlingCount(time.Now().UTC()))
	}

	serverContext, cancel := context.WithCancel(context.Background())
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		_ = server.Serve(serverContext)
	}()
	t.Cleanup(func() {
		cancel()
		<-serverDone
	})

	client := NewClient(socketPath)
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, err = client.Health(context.Background())
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("wait for recovered loopgate health: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	client.ConfigureSession("recover-test", "recover-session", []string{"fs_list"})
	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("bootstrap status after recovery server: %v", err)
	}
	client.ConfigureSession("recover-test", "recover-session", advertisedSessionCapabilityNames(status))

	firstSpawnResponse, err := client.SpawnMorphling(context.Background(), MorphlingSpawnRequest{
		Class:                 "reviewer",
		Goal:                  "First recovered spawn",
		RequestedCapabilities: []string{"fs_list", "fs_read"},
	})
	if err != nil {
		t.Fatalf("spawn after terminating recovery: %v", err)
	}
	if firstSpawnResponse.Status != ResponseStatusSuccess {
		t.Fatalf("expected first spawn after recovery to succeed, got %#v", firstSpawnResponse)
	}

	secondSpawnResponse, err := client.SpawnMorphling(context.Background(), MorphlingSpawnRequest{
		Class:                 "reviewer",
		Goal:                  "Second recovered spawn",
		RequestedCapabilities: []string{"fs_list", "fs_read"},
	})
	if err != nil {
		t.Fatalf("second spawn after recovery returned transport error: %v", err)
	}
	if secondSpawnResponse.Status != ResponseStatusDenied || secondSpawnResponse.DenialCode != DenialCodeMorphlingActiveLimitReached {
		t.Fatalf("expected second spawn after recovery to hit active limit, got %#v", secondSpawnResponse)
	}
}

func TestMorphlingApprovalExpiryBeatsLateOperatorApproval(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgateMorphlingPolicyYAML(false, true, 1))

	pendingResponse, err := client.SpawnMorphling(context.Background(), MorphlingSpawnRequest{
		Class:                 "editor",
		Goal:                  "Patch the sandboxed file",
		RequestedCapabilities: []string{"fs_list", "fs_read", "fs_write"},
	})
	if err != nil {
		t.Fatalf("spawn morphling requiring approval: %v", err)
	}
	if pendingResponse.Status != ResponseStatusPendingApproval {
		t.Fatalf("expected pending approval response, got %#v", pendingResponse)
	}

	server.morphlingsMu.Lock()
	workingRecords := cloneMorphlingRecords(server.morphlings)
	pendingRecord := workingRecords[pendingResponse.MorphlingID]
	pendingRecord.ApprovalDeadlineUTC = time.Now().UTC().Add(-1 * time.Second).Format(time.RFC3339Nano)
	workingRecords[pendingResponse.MorphlingID] = pendingRecord
	if err := saveMorphlingRecords(server.morphlingPath, workingRecords, server.morphlingStateKey); err != nil {
		server.morphlingsMu.Unlock()
		t.Fatalf("persist expired pending morphling approval: %v", err)
	}
	server.morphlings = workingRecords
	server.morphlingsMu.Unlock()

	var approvalResponse CapabilityResponse
	var approvalErr error
	var statusErr error
	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	go func() {
		defer waitGroup.Done()
		approvalResponse, approvalErr = client.UIDecideApproval(context.Background(), pendingResponse.ApprovalID, true)
	}()
	go func() {
		defer waitGroup.Done()
		_, statusErr = client.MorphlingStatus(context.Background(), MorphlingStatusRequest{
			MorphlingID:       pendingResponse.MorphlingID,
			IncludeTerminated: true,
		})
	}()
	waitGroup.Wait()

	if statusErr != nil {
		t.Fatalf("status request during approval expiry race: %v", statusErr)
	}
	if approvalErr != nil {
		t.Fatalf("late operator approval returned transport error: %v", approvalErr)
	}
	if approvalResponse.Status != ResponseStatusDenied {
		t.Fatalf("expected late operator approval to lose to expiry, got %#v", approvalResponse)
	}

	finalStatusResponse, err := client.MorphlingStatus(context.Background(), MorphlingStatusRequest{
		MorphlingID:       pendingResponse.MorphlingID,
		IncludeTerminated: true,
	})
	if err != nil {
		t.Fatalf("morphling status after approval expiry race: %v", err)
	}
	if len(finalStatusResponse.Morphlings) != 1 {
		t.Fatalf("expected terminated morphling after approval expiry race, got %#v", finalStatusResponse)
	}
	if finalStatusResponse.Morphlings[0].TerminationReason != "" {
		t.Fatalf("expected projected summary to omit termination reason, got %#v", finalStatusResponse.Morphlings[0])
	}
	if finalStatusResponse.Morphlings[0].StatusText != "terminated" {
		t.Fatalf("expected projected status_text terminated after approval expiry, got %#v", finalStatusResponse.Morphlings[0])
	}
}

func TestMorphlingAuditNeverStoresRawGoalText(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgateMorphlingPolicyYAML(false, true, 5))

	rawGoalText := "TOP-SECRET morphling goal text that must not appear in the audit log"
	spawnResponse, err := client.SpawnMorphling(context.Background(), MorphlingSpawnRequest{
		Class:                 "reviewer",
		Goal:                  rawGoalText,
		RequestedCapabilities: []string{"fs_list", "fs_read"},
	})
	if err != nil {
		t.Fatalf("spawn morphling for audit goal test: %v", err)
	}
	if spawnResponse.Status != ResponseStatusSuccess {
		t.Fatalf("expected success response for audit goal test, got %#v", spawnResponse)
	}

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read loopgate audit log: %v", err)
	}
	if strings.Contains(string(auditBytes), rawGoalText) {
		t.Fatalf("raw goal text leaked into append-only audit log: %s", string(auditBytes))
	}

	var foundSpawnRequested bool
	for _, auditLine := range strings.Split(strings.TrimSpace(string(auditBytes)), "\n") {
		if strings.TrimSpace(auditLine) == "" {
			continue
		}
		var auditEvent map[string]interface{}
		if err := json.Unmarshal([]byte(auditLine), &auditEvent); err != nil {
			t.Fatalf("decode audit line: %v", err)
		}
		if auditEvent["type"] != "morphling.spawn_requested" {
			continue
		}
		eventData, _ := auditEvent["data"].(map[string]interface{})
		if eventData["request_id"] != spawnResponse.RequestID {
			continue
		}
		if goalHMAC, _ := eventData["goal_hmac"].(string); strings.TrimSpace(goalHMAC) == "" {
			t.Fatalf("expected goal_hmac on morphling.spawn_requested event, got %#v", auditEvent)
		}
		foundSpawnRequested = true
	}
	if !foundSpawnRequested {
		t.Fatal("expected morphling.spawn_requested audit event with goal_hmac")
	}
}

func TestMorphlingWorkerLifecycleStagesArtifactsAndHashesProjectionAudit(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgateMorphlingPolicyYAML(false, true, 5))

	rawStatusText := "TOP-SECRET morphling worker status that must not leak into audit"
	rawMemoryString := "TOP-SECRET morphling worker memory string"

	pendingReviewResponse := driveMorphlingToPendingReview(t, client, server, rawStatusText, rawMemoryString)
	if pendingReviewResponse.Morphling.State != morphlingStatePendingReview {
		t.Fatalf("expected pending review morphling after worker completion, got %#v", pendingReviewResponse.Morphling)
	}
	if pendingReviewResponse.Morphling.StatusText != "pending review" {
		t.Fatalf("expected projected status_text, got %#v", pendingReviewResponse.Morphling)
	}
	if pendingReviewResponse.Morphling.MemoryStringCount != 1 {
		t.Fatalf("expected memory string count in projection, got %#v", pendingReviewResponse.Morphling)
	}
	if !pendingReviewResponse.Morphling.PendingReview {
		t.Fatalf("expected pending review flag after worker completion, got %#v", pendingReviewResponse.Morphling)
	}
	if pendingReviewResponse.Morphling.ArtifactCount != 1 {
		t.Fatalf("expected one staged artifact after worker completion, got %#v", pendingReviewResponse.Morphling)
	}
	if len(pendingReviewResponse.Morphling.StagedArtifactRefs) != 1 {
		t.Fatalf("expected staged artifact refs after worker completion, got %#v", pendingReviewResponse.Morphling)
	}
	if strings.TrimSpace(pendingReviewResponse.Morphling.ReviewDeadlineUTC) == "" {
		t.Fatalf("expected review deadline on pending review morphling, got %#v", pendingReviewResponse.Morphling)
	}

	reviewResponse, err := client.ReviewMorphling(context.Background(), MorphlingReviewRequest{
		MorphlingID: pendingReviewResponse.Morphling.MorphlingID,
		Approved:    true,
	})
	if err != nil {
		t.Fatalf("review pending morphling: %v", err)
	}
	if reviewResponse.Morphling.State != morphlingStateTerminated {
		t.Fatalf("expected terminated morphling after review approval, got %#v", reviewResponse.Morphling)
	}
	if reviewResponse.Morphling.Outcome != morphlingOutcomeApproved {
		t.Fatalf("expected approved outcome after review approval, got %#v", reviewResponse.Morphling)
	}
	if reviewResponse.Morphling.TerminationReason != "" {
		t.Fatalf("expected projected summary to omit termination reason, got %#v", reviewResponse.Morphling)
	}
	if reviewResponse.Morphling.StatusText != "terminated" {
		t.Fatalf("expected projected status_text terminated after review, got %#v", reviewResponse.Morphling)
	}
	if len(reviewResponse.Morphling.StagedArtifactRefs) != 1 {
		t.Fatalf("expected staged artifact refs to be preserved on termination, got %#v", reviewResponse.Morphling)
	}

	statusResponse, err := client.MorphlingStatus(context.Background(), MorphlingStatusRequest{
		MorphlingID:       pendingReviewResponse.Morphling.MorphlingID,
		IncludeTerminated: true,
	})
	if err != nil {
		t.Fatalf("morphling status after worker review approval: %v", err)
	}
	if len(statusResponse.Morphlings) != 1 {
		t.Fatalf("expected one terminated morphling in status response, got %#v", statusResponse)
	}
	if statusResponse.Morphlings[0].ArtifactCount != 1 {
		t.Fatalf("expected terminated morphling artifact count to remain stable, got %#v", statusResponse.Morphlings[0])
	}

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read loopgate audit log: %v", err)
	}
	if strings.Contains(string(auditBytes), rawStatusText) {
		t.Fatalf("raw worker status text leaked into audit log: %s", string(auditBytes))
	}
	if strings.Contains(string(auditBytes), rawMemoryString) {
		t.Fatalf("raw worker memory string leaked into audit log: %s", string(auditBytes))
	}

	foundEventTypes := make(map[string]struct{})
	for _, auditLine := range strings.Split(strings.TrimSpace(string(auditBytes)), "\n") {
		if strings.TrimSpace(auditLine) == "" {
			continue
		}
		var auditEvent map[string]interface{}
		if err := json.Unmarshal([]byte(auditLine), &auditEvent); err != nil {
			t.Fatalf("decode audit line: %v", err)
		}
		foundEventTypes[auditEvent["type"].(string)] = struct{}{}
	}
	for _, expectedEventType := range []string{
		"morphling.worker_launch_created",
		"morphling.worker_session_opened",
		"morphling.execution_started",
		"morphling.execution_completed",
		"morphling.artifacts_staged",
		"morphling.review_decision",
		"morphling.terminated",
	} {
		if _, found := foundEventTypes[expectedEventType]; !found {
			t.Fatalf("expected audit event %q in worker lifecycle log, found %#v", expectedEventType, foundEventTypes)
		}
	}
}

func TestMorphlingWorkerLaunchTokenIsSingleUse(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgateMorphlingPolicyYAML(false, true, 5))

	spawnResponse, err := client.SpawnMorphling(context.Background(), MorphlingSpawnRequest{
		Class:                 "reviewer",
		Goal:                  "Open a worker session once",
		RequestedCapabilities: []string{"fs_list", "fs_read"},
	})
	if err != nil {
		t.Fatalf("spawn morphling for worker launch test: %v", err)
	}
	if spawnResponse.Status != ResponseStatusSuccess {
		t.Fatalf("expected successful spawn for worker launch test, got %#v", spawnResponse)
	}

	launchResponse, err := client.LaunchMorphlingWorker(context.Background(), MorphlingWorkerLaunchRequest{
		MorphlingID: spawnResponse.MorphlingID,
	})
	if err != nil {
		t.Fatalf("launch morphling worker: %v", err)
	}
	if strings.TrimSpace(launchResponse.LaunchToken) == "" {
		t.Fatalf("expected worker launch token, got %#v", launchResponse)
	}

	if _, _, err := OpenMorphlingWorkerSession(context.Background(), server.socketPath, launchResponse.LaunchToken); err != nil {
		t.Fatalf("open first morphling worker session: %v", err)
	}
	if _, _, err := OpenMorphlingWorkerSession(context.Background(), server.socketPath, launchResponse.LaunchToken); err == nil {
		t.Fatal("expected second worker session open attempt to fail after single-use launch token consumption")
	}
}

func TestMorphlingReviewExpiryBeatsLateOperatorDecision(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgateMorphlingPolicyYAML(false, true, 5))

	pendingReviewResponse := driveMorphlingToPendingReview(t, client, server, "review expiry status", "review expiry memory")
	server.morphlingsMu.Lock()
	workingRecords := cloneMorphlingRecords(server.morphlings)
	pendingReviewRecord := workingRecords[pendingReviewResponse.Morphling.MorphlingID]
	pendingReviewRecord.ReviewDeadlineUTC = time.Now().UTC().Add(-1 * time.Second).Format(time.RFC3339Nano)
	workingRecords[pendingReviewResponse.Morphling.MorphlingID] = pendingReviewRecord
	if err := saveMorphlingRecords(server.morphlingPath, workingRecords, server.morphlingStateKey); err != nil {
		server.morphlingsMu.Unlock()
		t.Fatalf("persist expired pending morphling review: %v", err)
	}
	server.morphlings = workingRecords
	server.morphlingsMu.Unlock()

	var reviewErr error
	var statusErr error
	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	go func() {
		defer waitGroup.Done()
		_, reviewErr = client.ReviewMorphling(context.Background(), MorphlingReviewRequest{
			MorphlingID: pendingReviewResponse.Morphling.MorphlingID,
			Approved:    true,
		})
	}()
	go func() {
		defer waitGroup.Done()
		_, statusErr = client.MorphlingStatus(context.Background(), MorphlingStatusRequest{
			MorphlingID:       pendingReviewResponse.Morphling.MorphlingID,
			IncludeTerminated: true,
		})
	}()
	waitGroup.Wait()

	if statusErr != nil {
		t.Fatalf("status request during review expiry race: %v", statusErr)
	}
	if reviewErr == nil {
		t.Fatal("expected late review approval to lose after review deadline expiry")
	}

	finalStatusResponse, err := client.MorphlingStatus(context.Background(), MorphlingStatusRequest{
		MorphlingID:       pendingReviewResponse.Morphling.MorphlingID,
		IncludeTerminated: true,
	})
	if err != nil {
		t.Fatalf("morphling status after review expiry race: %v", err)
	}
	if len(finalStatusResponse.Morphlings) != 1 {
		t.Fatalf("expected terminated morphling after review expiry race, got %#v", finalStatusResponse)
	}
	if finalStatusResponse.Morphlings[0].TerminationReason != "" {
		t.Fatalf("expected projected summary to omit termination reason, got %#v", finalStatusResponse.Morphlings[0])
	}
	if finalStatusResponse.Morphlings[0].StatusText != "terminated" {
		t.Fatalf("expected projected status_text terminated after review expiry, got %#v", finalStatusResponse.Morphlings[0])
	}
	if finalStatusResponse.Morphlings[0].Outcome != morphlingOutcomeCancelled {
		t.Fatalf("expected cancelled outcome after review ttl expiry, got %#v", finalStatusResponse.Morphlings[0])
	}
}

func TestMorphlingArtifactManifestHashIsStable(t *testing.T) {
	leftHash, err := hashMorphlingArtifactManifest(map[string]interface{}{
		"artifact_count": 2,
		"artifacts": []map[string]interface{}{
			{"name": "alpha.txt", "sha256": "aaa"},
			{"name": "beta.txt", "sha256": "bbb"},
		},
		"task_id": "task-123",
	})
	if err != nil {
		t.Fatalf("hash left manifest: %v", err)
	}
	rightHash, err := hashMorphlingArtifactManifest(map[string]interface{}{
		"task_id": "task-123",
		"artifacts": []map[string]interface{}{
			{"sha256": "aaa", "name": "alpha.txt"},
			{"sha256": "bbb", "name": "beta.txt"},
		},
		"artifact_count": 2,
	})
	if err != nil {
		t.Fatalf("hash right manifest: %v", err)
	}
	if leftHash != rightHash {
		t.Fatalf("expected stable manifest hash for equivalent manifests, left=%s right=%s", leftHash, rightHash)
	}
}

func TestNewServerFailsClosedOnInvalidMorphlingClassPolicy(t *testing.T) {
	repoRoot := t.TempDir()
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	classPolicyPath := filepath.Join(repoRoot, "core", "policy", "morphling_classes.yaml")
	if err := os.WriteFile(classPolicyPath, []byte("version: \"1\"\nclasses:\n  - name: reviewer\n    description: \"broken\"\n    capabilities:\n      allowed:\n        - unknown_capability\n    sandbox:\n      allowed_zones:\n        - workspace\n    resource_limits:\n      max_time_seconds: 1\n      max_tokens: 1\n      max_disk_bytes: 1\n    ttl:\n      spawn_approval_ttl_seconds: 1\n      capability_token_ttl_seconds: 1\n      review_ttl_seconds: 1\n    spawn_requires_approval: false\n    completion_requires_review: true\n    max_concurrent: 1\n"), 0o600); err != nil {
		t.Fatalf("write invalid morphling class policy: %v", err)
	}

	_, err := NewServer(repoRoot, filepath.Join(t.TempDir(), "loopgate.sock"))
	if err == nil || !strings.Contains(err.Error(), "unknown capability") {
		t.Fatalf("expected NewServer to fail closed on invalid morphling class policy, got %v", err)
	}
}

func driveMorphlingToPendingReview(t *testing.T, client *Client, server *Server, statusText string, memoryString string) MorphlingWorkerActionResponse {
	t.Helper()

	spawnResponse, err := client.SpawnMorphling(context.Background(), MorphlingSpawnRequest{
		Class:                 "reviewer",
		Goal:                  "Drive the morphling into pending review",
		RequestedCapabilities: []string{"fs_list", "fs_read"},
	})
	if err != nil {
		t.Fatalf("spawn morphling for pending review: %v", err)
	}
	if spawnResponse.Status != ResponseStatusSuccess {
		t.Fatalf("expected successful spawn for pending review fixture, got %#v", spawnResponse)
	}

	launchResponse, err := client.LaunchMorphlingWorker(context.Background(), MorphlingWorkerLaunchRequest{
		MorphlingID: spawnResponse.MorphlingID,
	})
	if err != nil {
		t.Fatalf("launch morphling worker: %v", err)
	}

	workerClient, sessionResponse, err := OpenMorphlingWorkerSession(context.Background(), server.socketPath, launchResponse.LaunchToken)
	if err != nil {
		t.Fatalf("open morphling worker session: %v", err)
	}
	if sessionResponse.MorphlingID != spawnResponse.MorphlingID {
		t.Fatalf("expected worker session to bind to morphling %s, got %#v", spawnResponse.MorphlingID, sessionResponse)
	}

	startResponse, err := workerClient.Start(context.Background(), MorphlingWorkerStartRequest{
		StatusText:    statusText,
		MemoryStrings: []string{memoryString},
	})
	if err != nil {
		t.Fatalf("start morphling worker: %v", err)
	}
	if startResponse.Morphling.State != morphlingStateRunning {
		t.Fatalf("expected morphling to enter running state, got %#v", startResponse.Morphling)
	}

	updateResponse, err := workerClient.Update(context.Background(), MorphlingWorkerUpdateRequest{
		StatusText:    statusText + " updated",
		MemoryStrings: []string{memoryString, memoryString + " updated"},
	})
	if err != nil {
		t.Fatalf("update morphling worker: %v", err)
	}
	if updateResponse.Morphling.State != morphlingStateRunning {
		t.Fatalf("expected morphling to remain running after update, got %#v", updateResponse.Morphling)
	}

	server.morphlingsMu.Lock()
	record, found := server.morphlings[spawnResponse.MorphlingID]
	server.morphlingsMu.Unlock()
	if !found {
		t.Fatalf("expected spawned morphling %s in authoritative state", spawnResponse.MorphlingID)
	}
	artifactRelativePath := path.Join(record.WorkingDirRelativePath, "worker-report.txt")
	artifactRuntimePath := filepath.Join(server.sandboxPaths.Home, filepath.FromSlash(artifactRelativePath))
	if err := os.WriteFile(artifactRuntimePath, []byte("worker artifact"), 0o600); err != nil {
		t.Fatalf("write morphling artifact in working dir: %v", err)
	}

	pendingReviewResponse, err := workerClient.Complete(context.Background(), MorphlingWorkerCompleteRequest{
		ExitReason:    "completed",
		StatusText:    statusText + " complete",
		MemoryStrings: []string{memoryString + " complete"},
		ArtifactPaths: []string{sandbox.VirtualizeRelativeHomePath(artifactRelativePath)},
	})
	if err != nil {
		t.Fatalf("complete morphling worker: %v", err)
	}
	if _, err := workerClient.Update(context.Background(), MorphlingWorkerUpdateRequest{
		StatusText:    "late update",
		MemoryStrings: []string{"late update"},
	}); err == nil {
		t.Fatal("expected worker updates to be rejected after completion moved the morphling out of running")
	}
	return pendingReviewResponse
}
