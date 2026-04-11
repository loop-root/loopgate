package loopgate

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"morph/internal/threadstore"
)

func TestHavenContinuityInspectThread_SubmittedAndSkipped(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.resolveUserHomeDir = func() (string, error) { return repoRoot, nil }

	wsID := server.deriveWorkspaceIDFromRepoRoot()
	// Match handleHavenChat / cmd/haven: NewStore parent is ~/.haven/threads (Store adds /threads/ for JSONL).
	threadStoreRoot := filepath.Join(repoRoot, ".haven", "threads")
	store, err := threadstore.NewStore(threadStoreRoot, wsID)
	if err != nil {
		t.Fatalf("thread store: %v", err)
	}
	summary, err := store.NewThread()
	if err != nil {
		t.Fatalf("new thread: %v", err)
	}
	threadID := summary.ThreadID
	_ = store.AppendEvent(threadID, threadstore.ConversationEvent{
		Type: threadstore.EventUserMessage,
		Data: map[string]interface{}{"text": "hello"},
	})
	_ = store.AppendEvent(threadID, threadstore.ConversationEvent{
		Type: threadstore.EventAssistantMessage,
		Data: map[string]interface{}{"text": "hi there"},
	})

	client.SetWorkspaceID(wsID)
	client.ConfigureSession("haven", "haven-continuity-inspect-test", advertisedSessionCapabilityNames(status))
	ctx := context.Background()
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		t.Fatalf("token: %v", err)
	}

	var submitted HavenContinuityInspectThreadResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/continuity/inspect-thread", token, HavenContinuityInspectThreadRequest{ThreadID: threadID}, &submitted, nil); err != nil {
		t.Fatalf("inspect-thread: %v", err)
	}
	if submitted.SubmitStatus != havenContinuitySubmitStatusSubmitted {
		t.Fatalf("submit_status: got %q want %q", submitted.SubmitStatus, havenContinuitySubmitStatusSubmitted)
	}
	if strings.TrimSpace(submitted.InspectionID) == "" {
		t.Fatalf("missing inspection_id: %#v", submitted)
	}

	emptySummary, err := store.NewThread()
	if err != nil {
		t.Fatalf("empty thread: %v", err)
	}
	var skipped HavenContinuityInspectThreadResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/continuity/inspect-thread", token, HavenContinuityInspectThreadRequest{ThreadID: emptySummary.ThreadID}, &skipped, nil); err != nil {
		t.Fatalf("inspect-thread empty: %v", err)
	}
	if skipped.SubmitStatus != havenContinuitySubmitStatusSkippedNoEvents {
		t.Fatalf("empty thread submit_status: got %q want %q", skipped.SubmitStatus, havenContinuitySubmitStatusSkippedNoEvents)
	}
}

func TestHavenContinuityInspectThread_LegacyAliasStillWorks(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.resolveUserHomeDir = func() (string, error) { return repoRoot, nil }

	workspaceID := server.deriveWorkspaceIDFromRepoRoot()
	threadStoreRoot := filepath.Join(repoRoot, ".haven", "threads")
	store, err := threadstore.NewStore(threadStoreRoot, workspaceID)
	if err != nil {
		t.Fatalf("thread store: %v", err)
	}
	summary, err := store.NewThread()
	if err != nil {
		t.Fatalf("new thread: %v", err)
	}
	_ = store.AppendEvent(summary.ThreadID, threadstore.ConversationEvent{
		Type: threadstore.EventUserMessage,
		Data: map[string]interface{}{"text": "legacy alias check"},
	})

	client.SetWorkspaceID(workspaceID)
	client.ConfigureSession("haven", "legacy-continuity-alias-test", advertisedSessionCapabilityNames(status))
	ctx := context.Background()
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		t.Fatalf("token: %v", err)
	}

	var response HavenContinuityInspectThreadResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/haven/continuity/inspect-thread", token, HavenContinuityInspectThreadRequest{ThreadID: summary.ThreadID}, &response, nil); err != nil {
		t.Fatalf("legacy alias inspect-thread: %v", err)
	}
	if response.SubmitStatus != havenContinuitySubmitStatusSubmitted {
		t.Fatalf("submit_status: got %q want %q", response.SubmitStatus, havenContinuitySubmitStatusSubmitted)
	}
}

func TestHavenContinuityInspectThread_PersistsThreadstoreSourceRefs(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.resolveUserHomeDir = func() (string, error) { return repoRoot, nil }

	workspaceID := server.deriveWorkspaceIDFromRepoRoot()
	threadStoreRoot := filepath.Join(repoRoot, ".haven", "threads")
	store, err := threadstore.NewStore(threadStoreRoot, workspaceID)
	if err != nil {
		t.Fatalf("thread store: %v", err)
	}
	summary, err := store.NewThread()
	if err != nil {
		t.Fatalf("new thread: %v", err)
	}
	_ = store.AppendEvent(summary.ThreadID, threadstore.ConversationEvent{
		Type: threadstore.EventUserMessage,
		Data: map[string]interface{}{"text": "remember my timezone"},
	})
	_ = store.AppendEvent(summary.ThreadID, threadstore.ConversationEvent{
		Type: threadstore.EventAssistantMessage,
		Data: map[string]interface{}{"text": "noted"},
	})

	client.SetWorkspaceID(workspaceID)
	client.ConfigureSession("haven", "haven-continuity-source-ref-test", advertisedSessionCapabilityNames(status))
	if _, err := client.SubmitHavenContinuityInspectionForThread(context.Background(), summary.ThreadID); err != nil {
		t.Fatalf("submit haven continuity inspect-thread: %v", err)
	}

	authoritativeEvents := readContinuityAuthoritativeEventsForTests(t, server)
	if len(authoritativeEvents) != 1 {
		t.Fatalf("expected one continuity authoritative event, got %#v", authoritativeEvents)
	}
	observedPacket := authoritativeEvents[0].ObservedPacket
	if observedPacket == nil || len(observedPacket.Events) == 0 {
		t.Fatalf("expected observed packet with thread events, got %#v", authoritativeEvents[0])
	}
	if observedPacket.Events[0].SessionID != client.controlSessionID {
		t.Fatalf("expected observed packet to bind authoritative control session id, got %#v", observedPacket.Events[0])
	}
	firstSourceRefs := observedPacket.Events[0].SourceRefs
	if len(firstSourceRefs) != 1 {
		t.Fatalf("expected one threadstore source ref, got %#v", firstSourceRefs)
	}
	if firstSourceRefs[0].Kind != havenThreadEventSourceKind || firstSourceRefs[0].Ref != summary.ThreadID+":1" {
		t.Fatalf("expected threadstore source ref on observed packet, got %#v", firstSourceRefs[0])
	}
	if firstSourceRefs[0].SHA256 != observedPacket.Events[0].EventHash {
		t.Fatalf("expected threadstore source ref to carry event hash, got %#v event=%#v", firstSourceRefs[0], observedPacket.Events[0])
	}
}

func TestHavenContinuityInspectThread_DerivesFactsFromToolResultFacts(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.resolveUserHomeDir = func() (string, error) { return repoRoot, nil }

	workspaceID := server.deriveWorkspaceIDFromRepoRoot()
	threadStoreRoot := filepath.Join(repoRoot, ".haven", "threads")
	store, err := threadstore.NewStore(threadStoreRoot, workspaceID)
	if err != nil {
		t.Fatalf("thread store: %v", err)
	}
	summary, err := store.NewThread()
	if err != nil {
		t.Fatalf("new thread: %v", err)
	}
	_ = store.AppendEvent(summary.ThreadID, threadstore.ConversationEvent{
		Type: threadstore.EventOrchToolResult,
		Data: map[string]interface{}{
			"text":       "Current user profile preview card: timezone label is Mountain Time on the preview card.",
			"capability": "benchmark.fixture.observe",
			"status":     "ok",
			"facts": map[string]interface{}{
				"profile.preview_timezone_label": "Mountain Time",
			},
		},
	})

	client.SetWorkspaceID(workspaceID)
	client.ConfigureSession("haven", "haven-continuity-tool-result-facts-test", advertisedSessionCapabilityNames(status))
	response, err := client.SubmitHavenContinuityInspectionForThread(context.Background(), summary.ThreadID)
	if err != nil {
		t.Fatalf("submit haven continuity inspect-thread: %v", err)
	}
	if len(response.DerivedDistillateIDs) != 1 {
		t.Fatalf("expected one derived distillate, got %#v", response)
	}

	derivedDistillate := testDefaultMemoryState(t, server).Distillates[response.DerivedDistillateIDs[0]]
	if len(derivedDistillate.Facts) != 1 {
		t.Fatalf("expected one derived fact, got %#v", derivedDistillate.Facts)
	}
	if derivedDistillate.Facts[0].Name != "profile.preview_timezone_label" || derivedDistillate.Facts[0].Value != "Mountain Time" {
		t.Fatalf("expected preview timezone fact from tool result, got %#v", derivedDistillate.Facts[0])
	}
	if !containsString(derivedDistillate.Tags, "preview") || !containsString(derivedDistillate.Tags, "card") {
		t.Fatalf("expected preview-card context tags to survive tool-result derivation, got %#v", derivedDistillate.Tags)
	}
}
