package loopgate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"morph/internal/config"
)

func TestHavenUIPresenceReadsSnapshotFile(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := filepath.Join(repoRoot, "runtime", "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	presencePath := filepath.Join(stateDir, "haven_presence.json")
	snapshot := HavenPresenceResponse{
		State:      "working",
		StatusText: "Morph is coding",
		DetailText: "in the zone",
		Anchor:     "workspace",
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(presencePath, append(raw, '\n'), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	ctx := context.Background()
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	var gotPresence HavenPresenceResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/ui/presence", capabilityToken, nil, &gotPresence, nil); err != nil {
		t.Fatalf("GET /v1/ui/presence: %v", err)
	}
	if gotPresence.State != "working" || gotPresence.StatusText != "Morph is coding" || gotPresence.Anchor != "workspace" {
		t.Fatalf("unexpected presence: %#v", gotPresence)
	}
	if gotPresence.DetailText != "in the zone" {
		t.Fatalf("unexpected detail_text: %q", gotPresence.DetailText)
	}

	var gotMorphSleep HavenMorphSleepResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/ui/morph-sleep", capabilityToken, nil, &gotMorphSleep, nil); err != nil {
		t.Fatalf("GET /v1/ui/morph-sleep: %v", err)
	}
	if gotMorphSleep.IsSleeping {
		t.Fatalf("expected not sleeping, got %#v", gotMorphSleep)
	}
}

func TestHavenUIDeskNotesListActiveNotes(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := filepath.Join(repoRoot, "runtime", "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	deskPath := filepath.Join(stateDir, "haven_desk_notes.json")
	stateFile := havenDeskNoteStateFile{
		Notes: []HavenDeskNote{
			{ID: "a", Kind: "note", Title: "T1", Body: "...", CreatedAtUTC: "2025-01-02T00:00:00Z"},
			{ID: "b", Kind: "note", Title: "T2", Body: "...", CreatedAtUTC: "2025-01-01T00:00:00Z", ArchivedAtUTC: "2025-01-03T00:00:00Z"},
		},
	}
	raw, err := json.Marshal(stateFile)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(deskPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	ctx := context.Background()
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	var got HavenDeskNotesResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/ui/desk-notes", capabilityToken, nil, &got, nil); err != nil {
		t.Fatalf("GET /v1/ui/desk-notes: %v", err)
	}
	if len(got.Notes) != 1 {
		t.Fatalf("expected 1 active note, got %#v", got)
	}
	if got.Notes[0].ID != "a" {
		t.Fatalf("unexpected note id: %q", got.Notes[0].ID)
	}
}

func TestHavenUIWorkspaceListRootMapsSandboxFolders(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if err := server.sandboxPaths.Ensure(); err != nil {
		t.Fatalf("ensure sandbox paths: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(server.sandboxPaths.Imports, "shared"), 0o700); err != nil {
		t.Fatalf("mkdir shared: %v", err)
	}

	ctx := context.Background()
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	var got HavenWorkspaceListResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/ui/workspace/list", capabilityToken, HavenWorkspaceListRequest{}, &got, nil); err != nil {
		t.Fatalf("POST /v1/ui/workspace/list: %v", err)
	}
	if got.Path != "" {
		t.Fatalf("expected empty root path, got %q", got.Path)
	}
	entryNames := make([]string, 0, len(got.Entries))
	for _, entry := range got.Entries {
		entryNames = append(entryNames, entry.Name)
	}
	wantNames := []string{"agents", "artifacts", "imports", "projects", "research", "shared"}
	for _, wantName := range wantNames {
		found := false
		for _, gotName := range entryNames {
			if gotName == wantName {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected root entry %q in %#v", wantName, entryNames)
		}
	}
}

func TestHavenUIMemoryInventoryListsManageableObjects(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	if _, err := client.RememberMemoryFact(context.Background(), MemoryRememberRequest{
		FactKey:   "name",
		FactValue: "Ada",
	}); err != nil {
		t.Fatalf("remember explicit fact: %v", err)
	}

	server.memoryMu.Lock()
	partition := server.memoryPartitions[memoryPartitionKey("")]
	if partition == nil {
		server.memoryMu.Unlock()
		t.Fatal("missing default memory partition")
	}
	currentState := cloneContinuityMemoryState(partition.state)
	nowUTC := server.now().UTC()
	currentState.Inspections["inspect_ui_memory_secret"] = continuityInspectionRecord{
		InspectionID:          "inspect_ui_memory_secret",
		ThreadID:              "thread_ui_memory_secret",
		Scope:                 memoryScopeGlobal,
		SubmittedAtUTC:        nowUTC.Format(time.RFC3339Nano),
		CompletedAtUTC:        nowUTC.Format(time.RFC3339Nano),
		DerivationOutcome:     continuityInspectionOutcomeDerived,
		Review:                continuityInspectionReview{Status: continuityReviewStatusAccepted},
		Lineage:               continuityInspectionLineage{Status: continuityLineageStatusEligible},
		DerivedDistillateIDs:  []string{"dist_ui_memory_secret"},
		DerivedResonateKeyIDs: []string{"key_ui_memory_secret"},
	}
	currentState.Distillates["dist_ui_memory_secret"] = continuityDistillateRecord{
		SchemaVersion:     continuityMemorySchemaVersion,
		DerivationVersion: continuityDerivationVersion,
		DistillateID:      "dist_ui_memory_secret",
		InspectionID:      "inspect_ui_memory_secret",
		ThreadID:          "thread_ui_memory_secret",
		Scope:             memoryScopeGlobal,
		GoalType:          goalTypeTechnicalReview,
		GoalFamilyID:      "technical_review:redaction_inventory",
		CreatedAtUTC:      nowUTC.Format(time.RFC3339Nano),
		RetentionScore:    60,
		EffectiveHotness:  60,
		MemoryState:       memoryStateHot,
		GoalOps: []continuityGoalOp{{
			GoalID: "goal_ui_memory_secret",
			Text:   "authorization: Bearer super-secret-token",
			Action: "opened",
		}},
	}
	currentState.ResonateKeys["key_ui_memory_secret"] = continuityResonateKeyRecord{
		SchemaVersion:     continuityMemorySchemaVersion,
		DerivationVersion: continuityDerivationVersion,
		KeyID:             "key_ui_memory_secret",
		DistillateID:      "dist_ui_memory_secret",
		ThreadID:          "thread_ui_memory_secret",
		Scope:             memoryScopeGlobal,
		GoalType:          goalTypeTechnicalReview,
		GoalFamilyID:      "technical_review:redaction_inventory",
		CreatedAtUTC:      nowUTC.Format(time.RFC3339Nano),
		RetentionScore:    60,
		EffectiveHotness:  60,
		MemoryState:       memoryStateHot,
	}
	currentState.WakeState, currentState.DiagnosticWake = buildLoopgateWakeProducts(currentState, nowUTC, server.runtimeConfig)
	partition.state = canonicalizeContinuityMemoryState(currentState)
	server.memoryMu.Unlock()

	inventory, err := client.LoadHavenMemoryInventory(context.Background())
	if err != nil {
		t.Fatalf("load Haven memory inventory: %v", err)
	}
	if inventory.RecentFactCount < 1 {
		t.Fatalf("expected at least one recent fact, got %#v", inventory)
	}
	if len(inventory.Objects) < 2 {
		t.Fatalf("expected at least two memory objects, got %#v", inventory.Objects)
	}

	foundExplicitFact := false
	foundRedactedThreadInspection := false
	for _, objectEntry := range inventory.Objects {
		switch objectEntry.InspectionID {
		case "inspect_ui_memory_secret":
			foundRedactedThreadInspection = true
			if strings.Contains(objectEntry.Summary, "super-secret-token") {
				t.Fatalf("secret-bearing memory summary leaked raw token: %#v", objectEntry)
			}
			if !strings.Contains(objectEntry.Summary, "[REDACTED]") {
				t.Fatalf("expected redacted secret-bearing summary, got %#v", objectEntry)
			}
			if !objectEntry.CanTombstone || !objectEntry.CanPurge {
				t.Fatalf("expected eligible inspection to be manually manageable, got %#v", objectEntry)
			}
		default:
			if objectEntry.ObjectKind == havenMemoryObjectKindExplicitFact && strings.Contains(objectEntry.Summary, "name=Ada") {
				foundExplicitFact = true
			}
		}
	}
	if !foundExplicitFact {
		t.Fatalf("expected explicit fact summary in inventory, got %#v", inventory.Objects)
	}
	if !foundRedactedThreadInspection {
		t.Fatalf("expected inspected thread entry in inventory, got %#v", inventory.Objects)
	}
}

func TestHavenUIMemoryResetArchivesAndClearsContinuityState(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	if _, err := client.RememberMemoryFact(context.Background(), MemoryRememberRequest{
		FactKey:   "name",
		FactValue: "Ada",
	}); err != nil {
		t.Fatalf("remember explicit fact: %v", err)
	}

	resetResponse, err := client.ResetHavenMemory(context.Background(), HavenMemoryResetRequest{
		OperationID: "reset_demo_memory",
		Reason:      "prepare fresh demo",
	})
	if err != nil {
		t.Fatalf("reset Haven memory: %v", err)
	}
	if resetResponse.PreviousInspectionCount != 1 || resetResponse.PreviousDistillateCount != 1 || resetResponse.PreviousResonateKeyCount != 1 {
		t.Fatalf("unexpected reset counts: %#v", resetResponse)
	}
	if strings.TrimSpace(resetResponse.ArchiveID) == "" {
		t.Fatalf("expected archive id in reset response, got %#v", resetResponse)
	}

	archivePath := filepath.Join(repoRoot, "runtime", "state", "memory_archives", resetResponse.ArchiveID)
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("expected archived memory root at %s: %v", archivePath, err)
	}
	if _, err := os.Stat(filepath.Join(archivePath, "state.json")); err != nil {
		t.Fatalf("expected archived state.json in %s: %v", archivePath, err)
	}
	memAfterReset := testDefaultMemoryState(t, server)
	if len(memAfterReset.Inspections) != 0 || len(memAfterReset.Distillates) != 0 || len(memAfterReset.ResonateKeys) != 0 {
		t.Fatalf("expected in-memory continuity state cleared after reset, got %#v", memAfterReset)
	}

	wakeState, err := client.LoadMemoryWakeState(context.Background())
	if err != nil {
		t.Fatalf("load wake state after reset: %v", err)
	}
	if len(wakeState.RecentFacts) != 0 || len(wakeState.ActiveGoals) != 0 || len(wakeState.UnresolvedItems) != 0 {
		t.Fatalf("expected empty wake state after reset, got %#v", wakeState)
	}

	inventory, err := client.LoadHavenMemoryInventory(context.Background())
	if err != nil {
		t.Fatalf("load inventory after reset: %v", err)
	}
	if len(inventory.Objects) != 0 {
		t.Fatalf("expected empty inventory after reset, got %#v", inventory.Objects)
	}

	if _, err := client.RememberMemoryFact(context.Background(), MemoryRememberRequest{
		FactKey:   "name",
		FactValue: "Grace",
	}); err != nil {
		t.Fatalf("remember fact after reset: %v", err)
	}
	refreshedWakeState, err := client.LoadMemoryWakeState(context.Background())
	if err != nil {
		t.Fatalf("load wake state after fresh remember: %v", err)
	}
	if factValue, found := memoryWakeFactValue(refreshedWakeState, "name"); !found || factValue != "Grace" {
		t.Fatalf("expected only fresh remembered name Grace after reset, got %#v", refreshedWakeState)
	}
}

func TestHavenUIMemoryResetRollsBackOnSaveFailure(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	if _, err := client.RememberMemoryFact(context.Background(), MemoryRememberRequest{
		FactKey:   "name",
		FactValue: "Ada",
	}); err != nil {
		t.Fatalf("remember explicit fact: %v", err)
	}

	server.saveMemoryState = func(string, continuityMemoryState, config.RuntimeConfig) error {
		return fmt.Errorf("forced save failure")
	}

	_, err := client.ResetHavenMemory(context.Background(), HavenMemoryResetRequest{
		OperationID: "reset_demo_memory_failure",
		Reason:      "exercise rollback path",
	})
	if err == nil || !strings.Contains(err.Error(), "forced save failure") {
		t.Fatalf("expected forced save failure from memory reset, got %v", err)
	}

	wakeState, wakeErr := client.LoadMemoryWakeState(context.Background())
	if wakeErr != nil {
		t.Fatalf("load wake state after rollback: %v", wakeErr)
	}
	if factValue, found := memoryWakeFactValue(wakeState, "name"); !found || factValue != "Ada" {
		t.Fatalf("expected remembered fact to survive rollback, got %#v", wakeState)
	}
	memAfterRollback := testDefaultMemoryState(t, server)
	if len(memAfterRollback.Inspections) != 1 || len(memAfterRollback.Distillates) != 1 || len(memAfterRollback.ResonateKeys) != 1 {
		t.Fatalf("expected continuity state restored after rollback, got %#v", memAfterRollback)
	}
}

func TestHavenJournalTitleFromFilename(t *testing.T) {
	t.Parallel()
	wantLegacy, err := time.ParseInLocation("2006-01-02", "2026-03-15", time.Local)
	if err != nil {
		t.Fatalf("parse legacy ref: %v", err)
	}
	if got := havenJournalTitleFromFilename("2026-03-15.md"); got != wantLegacy.Format("January 2, 2006") {
		t.Fatalf("legacy daily filename: got %q want %q", got, wantLegacy.Format("January 2, 2006"))
	}
	wantPerEntry, err := time.ParseInLocation("2006-01-02 15-04-05", "2026-03-15 14-30-00", time.Local)
	if err != nil {
		t.Fatalf("parse per-entry ref: %v", err)
	}
	wantPerEntryTitle := wantPerEntry.Format("Jan 2, 2006 · 15:04 MST")
	if got := havenJournalTitleFromFilename("2026-03-15T14-30-00-1700000000000000000.md"); got != wantPerEntryTitle {
		t.Fatalf("per-entry filename: got %q want %q", got, wantPerEntryTitle)
	}
}
