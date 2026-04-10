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
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAMLWithRawContinuityInspect(false, false))
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
	client.ConfigureSession("haven", "haven-continuity-inspect-test", capabilityNames(status.Capabilities))
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
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAMLWithRawContinuityInspect(false, false))
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
	client.ConfigureSession("haven", "legacy-continuity-alias-test", capabilityNames(status.Capabilities))
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
