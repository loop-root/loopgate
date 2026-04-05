package threadstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	rootDir := t.TempDir()
	store, err := NewStore(rootDir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	return store
}

func TestNewStore_CreatesDirectories(t *testing.T) {
	rootDir := t.TempDir()
	_, err := NewStore(rootDir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	threadsDir := filepath.Join(rootDir, "threads")
	info, err := os.Stat(threadsDir)
	if err != nil {
		t.Fatalf("threads dir missing: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected threads to be a directory")
	}
}

func TestNewThread_CreatesThreadFile(t *testing.T) {
	store := testStore(t)
	summary, err := store.NewThread()
	if err != nil {
		t.Fatalf("new thread: %v", err)
	}

	if !strings.HasPrefix(summary.ThreadID, "t-") {
		t.Errorf("expected thread ID prefix 't-', got %q", summary.ThreadID)
	}
	if summary.CreatedAt == "" {
		t.Error("expected non-empty CreatedAt")
	}

	// Verify file exists on disk.
	threadPath := filepath.Join(store.rootDir, "threads", summary.ThreadID+".jsonl")
	if _, err := os.Stat(threadPath); err != nil {
		t.Errorf("thread file should exist: %v", err)
	}
}

func TestNewThread_AppearsInListThreads(t *testing.T) {
	store := testStore(t)
	summary, _ := store.NewThread()

	threads := store.ListThreads()
	if len(threads) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(threads))
	}
	if threads[0].ThreadID != summary.ThreadID {
		t.Errorf("expected thread ID %q, got %q", summary.ThreadID, threads[0].ThreadID)
	}
}

func TestAppendEvent_PersistsToJSONL(t *testing.T) {
	store := testStore(t)
	thread, _ := store.NewThread()

	err := store.AppendEvent(thread.ThreadID, ConversationEvent{
		Type: EventUserMessage,
		Data: map[string]interface{}{"text": "hello"},
	})
	if err != nil {
		t.Fatalf("append event: %v", err)
	}

	events, err := store.LoadThread(thread.ThreadID)
	if err != nil {
		t.Fatalf("load thread: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventUserMessage {
		t.Errorf("expected type %q, got %q", EventUserMessage, events[0].Type)
	}
	if events[0].Data["text"] != "hello" {
		t.Errorf("expected text 'hello', got %v", events[0].Data["text"])
	}
	if events[0].SchemaVersion != ConversationEventSchemaVersion {
		t.Errorf("expected schema version %q, got %q", ConversationEventSchemaVersion, events[0].SchemaVersion)
	}
	if events[0].ThreadID != thread.ThreadID {
		t.Errorf("expected thread ID %q, got %q", thread.ThreadID, events[0].ThreadID)
	}
}

func TestAppendEvent_SetsTitleFromFirstUserMessage(t *testing.T) {
	store := testStore(t)
	thread, _ := store.NewThread()

	_ = store.AppendEvent(thread.ThreadID, ConversationEvent{
		Type: EventUserMessage,
		Data: map[string]interface{}{"text": "Analyze my project files"},
	})

	threads := store.ListThreads()
	if len(threads) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(threads))
	}
	if threads[0].Title != "Analyze my project files" {
		t.Errorf("expected title 'Analyze my project files', got %q", threads[0].Title)
	}
}

func TestAppendEvent_TitleNotOverwrittenByLaterMessages(t *testing.T) {
	store := testStore(t)
	thread, _ := store.NewThread()

	_ = store.AppendEvent(thread.ThreadID, ConversationEvent{
		Type: EventUserMessage,
		Data: map[string]interface{}{"text": "First message"},
	})
	_ = store.AppendEvent(thread.ThreadID, ConversationEvent{
		Type: EventUserMessage,
		Data: map[string]interface{}{"text": "Second message"},
	})

	threads := store.ListThreads()
	if threads[0].Title != "First message" {
		t.Errorf("title should remain 'First message', got %q", threads[0].Title)
	}
}

func TestAppendEvent_OrchestrationEventsDoNotSetTitle(t *testing.T) {
	store := testStore(t)
	thread, _ := store.NewThread()

	_ = store.AppendEvent(thread.ThreadID, ConversationEvent{
		Type: EventOrchToolStarted,
		Data: map[string]interface{}{"capability": "fs_read"},
	})

	threads := store.ListThreads()
	if threads[0].Title != "" {
		t.Errorf("orchestration event should not set title, got %q", threads[0].Title)
	}
}

func TestAppendEvent_UpdatesEventCount(t *testing.T) {
	store := testStore(t)
	thread, _ := store.NewThread()

	for i := 0; i < 5; i++ {
		_ = store.AppendEvent(thread.ThreadID, ConversationEvent{
			Type: EventUserMessage,
			Data: map[string]interface{}{"text": "msg"},
		})
	}

	threads := store.ListThreads()
	if threads[0].EventCount != 5 {
		t.Errorf("expected event count 5, got %d", threads[0].EventCount)
	}
}

func TestLoadThread_EmptyThread(t *testing.T) {
	store := testStore(t)
	thread, _ := store.NewThread()

	events, err := store.LoadThread(thread.ThreadID)
	if err != nil {
		t.Fatalf("load thread: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events for empty thread, got %d", len(events))
	}
}

func TestLoadThread_NonexistentThread(t *testing.T) {
	store := testStore(t)
	_, err := store.LoadThread("t-nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent thread")
	}
}

func TestListThreads_SortedByUpdatedAt(t *testing.T) {
	store := testStore(t)
	thread1, _ := store.NewThread()
	thread2, _ := store.NewThread()

	// Append to thread1 last, so it should appear first.
	_ = store.AppendEvent(thread2.ThreadID, ConversationEvent{
		Type: EventUserMessage,
		Data: map[string]interface{}{"text": "older"},
	})
	_ = store.AppendEvent(thread1.ThreadID, ConversationEvent{
		Type: EventUserMessage,
		Data: map[string]interface{}{"text": "newer"},
	})

	threads := store.ListThreads()
	if len(threads) != 2 {
		t.Fatalf("expected 2 threads, got %d", len(threads))
	}
	if threads[0].ThreadID != thread1.ThreadID {
		t.Errorf("expected most recently updated thread first")
	}
}

func TestRebuildIndex_RecoverFromMissingIndex(t *testing.T) {
	store := testStore(t)
	thread, _ := store.NewThread()

	_ = store.AppendEvent(thread.ThreadID, ConversationEvent{
		Type: EventUserMessage,
		Data: map[string]interface{}{"text": "hello from rebuild"},
	})

	// Delete the index file to simulate crash after append but before index update.
	_ = os.Remove(filepath.Join(store.rootDir, "thread_index.json"))

	// Create a new store from the same directory — should rebuild.
	store2, err := NewStore(store.rootDir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}

	threads := store2.ListThreads()
	if len(threads) != 1 {
		t.Fatalf("expected 1 thread after rebuild, got %d", len(threads))
	}
	if threads[0].ThreadID != thread.ThreadID {
		t.Errorf("expected thread ID %q, got %q", thread.ThreadID, threads[0].ThreadID)
	}
	if threads[0].EventCount != 1 {
		t.Errorf("expected 1 event after rebuild, got %d", threads[0].EventCount)
	}
	if threads[0].Title != "hello from rebuild" {
		t.Errorf("expected title 'hello from rebuild', got %q", threads[0].Title)
	}
}

func TestRebuildIndex_RecoverFromCorruptIndex(t *testing.T) {
	store := testStore(t)
	thread, _ := store.NewThread()
	_ = store.AppendEvent(thread.ThreadID, ConversationEvent{
		Type: EventUserMessage,
		Data: map[string]interface{}{"text": "recover me"},
	})

	// Corrupt the index file.
	indexPath := filepath.Join(store.rootDir, "thread_index.json")
	_ = os.WriteFile(indexPath, []byte("{corrupt json"), 0o600)

	store2, err := NewStore(store.rootDir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}

	threads := store2.ListThreads()
	if len(threads) != 1 {
		t.Fatalf("expected 1 thread after corrupt rebuild, got %d", len(threads))
	}
	if threads[0].Title != "recover me" {
		t.Errorf("expected title 'recover me', got %q", threads[0].Title)
	}
}

func TestAppendEventThenIndexDeleteThenReopen(t *testing.T) {
	// Simulates the specific crash-recovery case: event appended successfully
	// to JSONL, then process crashes before index is updated.
	store := testStore(t)
	thread, _ := store.NewThread()

	_ = store.AppendEvent(thread.ThreadID, ConversationEvent{
		Type: EventUserMessage,
		Data: map[string]interface{}{"text": "first"},
	})

	// Append a second event directly to the JSONL file, bypassing the store,
	// then delete the index. This simulates: event written, crash before index.
	threadPath := filepath.Join(store.rootDir, "threads", thread.ThreadID+".jsonl")
	secondEvent := ConversationEvent{
		SchemaVersion: ConversationEventSchemaVersion,
		TS:            NowUTC(),
		ThreadID:      thread.ThreadID,
		Type:          EventAssistantMessage,
		Data:          map[string]interface{}{"text": "second"},
	}
	encodedSecondEvent, _ := json.Marshal(secondEvent)
	f, _ := os.OpenFile(threadPath, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.Write(append(encodedSecondEvent, '\n'))
	_ = f.Close()

	// Delete index.
	_ = os.Remove(filepath.Join(store.rootDir, "thread_index.json"))

	// Reopen.
	store2, err := NewStore(store.rootDir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}

	threads := store2.ListThreads()
	if len(threads) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(threads))
	}
	if threads[0].EventCount != 2 {
		t.Errorf("expected 2 events (recovered from JSONL), got %d", threads[0].EventCount)
	}

	events, err := store2.LoadThread(thread.ThreadID)
	if err != nil {
		t.Fatalf("load thread: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events in JSONL, got %d", len(events))
	}
}

func TestIsUserVisible(t *testing.T) {
	if !IsUserVisible(EventUserMessage) {
		t.Error("user_message should be user-visible")
	}
	if !IsUserVisible(EventAssistantMessage) {
		t.Error("assistant_message should be user-visible")
	}
	if IsUserVisible(EventOrchToolStarted) {
		t.Error("orchestration.tool_started should NOT be user-visible")
	}
	if IsUserVisible(EventOrchModelResponse) {
		t.Error("orchestration.model_response should NOT be user-visible")
	}
}

func TestExecutionState_AcceptsNewMessage(t *testing.T) {
	acceptsCases := map[ExecutionState]bool{
		ExecutionIdle:               true,
		ExecutionRunning:            false,
		ExecutionWaitingForApproval: false,
		ExecutionCompleted:          true,
		ExecutionFailed:             true,
		ExecutionCancelled:          true,
	}
	for state, expected := range acceptsCases {
		if state.AcceptsNewMessage() != expected {
			t.Errorf("state %q: expected AcceptsNewMessage()=%v", state, expected)
		}
	}
}

func TestTruncateTitle(t *testing.T) {
	if got := truncateTitle("short", 80); got != "short" {
		t.Errorf("expected 'short', got %q", got)
	}
	long := strings.Repeat("x", 100)
	got := truncateTitle(long, 80)
	if len(got) != 83 { // 80 + "..."
		t.Errorf("expected truncated to 83 chars, got %d", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Error("expected ... suffix")
	}

	// Newlines collapsed.
	if got := truncateTitle("line1\nline2\nline3", 80); got != "line1 line2 line3" {
		t.Errorf("expected newlines collapsed, got %q", got)
	}
}

func TestWorkspaceID_StampedOnNewThread(t *testing.T) {
	rootDir := t.TempDir()
	store, err := NewStore(rootDir, "ws-project-alpha")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	thread, err := store.NewThread()
	if err != nil {
		t.Fatalf("new thread: %v", err)
	}
	if thread.WorkspaceID != "ws-project-alpha" {
		t.Errorf("expected workspace ID 'ws-project-alpha', got %q", thread.WorkspaceID)
	}
}

func TestWorkspaceID_ListFiltersToCurrentWorkspace(t *testing.T) {
	rootDir := t.TempDir()

	// Create threads in workspace A.
	storeA, _ := NewStore(rootDir, "ws-a")
	threadA, _ := storeA.NewThread()
	_ = storeA.AppendEvent(threadA.ThreadID, ConversationEvent{
		Type: EventUserMessage,
		Data: map[string]interface{}{"text": "from A"},
	})

	// Create threads in workspace B using the same root dir.
	storeB, _ := NewStore(rootDir, "ws-b")
	threadB, _ := storeB.NewThread()
	_ = storeB.AppendEvent(threadB.ThreadID, ConversationEvent{
		Type: EventUserMessage,
		Data: map[string]interface{}{"text": "from B"},
	})

	// Store A should only see its own thread.
	threadsA := storeA.ListThreads()
	if len(threadsA) != 1 {
		t.Fatalf("store A: expected 1 thread, got %d", len(threadsA))
	}
	if threadsA[0].ThreadID != threadA.ThreadID {
		t.Errorf("store A: expected thread %q, got %q", threadA.ThreadID, threadsA[0].ThreadID)
	}

	// Store B should only see its own thread.
	threadsB := storeB.ListThreads()
	if len(threadsB) != 1 {
		t.Fatalf("store B: expected 1 thread, got %d", len(threadsB))
	}
	if threadsB[0].ThreadID != threadB.ThreadID {
		t.Errorf("store B: expected thread %q, got %q", threadB.ThreadID, threadsB[0].ThreadID)
	}
}

func TestWorkspaceID_LoadRejectsCrossWorkspace(t *testing.T) {
	rootDir := t.TempDir()

	storeA, _ := NewStore(rootDir, "ws-a")
	threadA, _ := storeA.NewThread()
	_ = storeA.AppendEvent(threadA.ThreadID, ConversationEvent{
		Type: EventUserMessage,
		Data: map[string]interface{}{"text": "secret A data"},
	})

	// Store B must not be able to load store A's thread.
	storeB, _ := NewStore(rootDir, "ws-b")
	_, err := storeB.LoadThread(threadA.ThreadID)
	if err == nil {
		t.Fatal("expected error loading cross-workspace thread")
	}
	if !strings.Contains(err.Error(), "belongs to workspace") {
		t.Errorf("expected workspace boundary error, got: %v", err)
	}
}

func TestWorkspaceID_EmptyDisablesFiltering(t *testing.T) {
	rootDir := t.TempDir()

	// Create threads in workspace A.
	storeA, _ := NewStore(rootDir, "ws-a")
	threadA, _ := storeA.NewThread()
	_ = storeA.AppendEvent(threadA.ThreadID, ConversationEvent{
		Type: EventUserMessage,
		Data: map[string]interface{}{"text": "hello"},
	})

	// A store with no workspace ID should see all threads (backward compat).
	storeAll, _ := NewStore(rootDir)
	threads := storeAll.ListThreads()
	if len(threads) != 1 {
		t.Fatalf("unscoped store: expected 1 thread, got %d", len(threads))
	}

	// Should also be able to load any thread.
	events, err := storeAll.LoadThread(threadA.ThreadID)
	if err != nil {
		t.Fatalf("unscoped store: unexpected error loading thread: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}
}

func TestSetThreadFolder(t *testing.T) {
	store := testStore(t)
	thread, _ := store.NewThread()

	if err := store.SetThreadFolder(thread.ThreadID, "Work"); err != nil {
		t.Fatalf("set folder: %v", err)
	}

	threads := store.ListThreads()
	if len(threads) != 1 || threads[0].Folder != "Work" {
		t.Errorf("expected folder 'Work', got %q", threads[0].Folder)
	}
}

func TestSetThreadFolder_Unfiled(t *testing.T) {
	store := testStore(t)
	thread, _ := store.NewThread()

	_ = store.SetThreadFolder(thread.ThreadID, "Research")
	_ = store.SetThreadFolder(thread.ThreadID, "")

	threads := store.ListThreads()
	if threads[0].Folder != "" {
		t.Errorf("expected empty folder, got %q", threads[0].Folder)
	}
}

func TestSetThreadFolder_NotFound(t *testing.T) {
	store := testStore(t)
	err := store.SetThreadFolder("t-nonexistent", "Work")
	if err == nil {
		t.Error("expected error for nonexistent thread")
	}
}

func TestRenameThread(t *testing.T) {
	store := testStore(t)
	thread, _ := store.NewThread()
	_ = store.AppendEvent(thread.ThreadID, ConversationEvent{
		Type: EventUserMessage,
		Data: map[string]interface{}{"text": "original title"},
	})

	if err := store.RenameThread(thread.ThreadID, "Custom Title"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	threads := store.ListThreads()
	if threads[0].Title != "Custom Title" {
		t.Errorf("expected 'Custom Title', got %q", threads[0].Title)
	}
}

func TestListFolders(t *testing.T) {
	store := testStore(t)
	t1, _ := store.NewThread()
	t2, _ := store.NewThread()
	t3, _ := store.NewThread()

	_ = store.SetThreadFolder(t1.ThreadID, "Research")
	_ = store.SetThreadFolder(t2.ThreadID, "Work")
	_ = store.SetThreadFolder(t3.ThreadID, "Research") // duplicate

	folders := store.ListFolders()
	if len(folders) != 2 {
		t.Fatalf("expected 2 folders, got %d: %v", len(folders), folders)
	}
	// Sorted alphabetically.
	if folders[0] != "Research" || folders[1] != "Work" {
		t.Errorf("expected [Research, Work], got %v", folders)
	}
}

func TestMakeThreadID_Format(t *testing.T) {
	id := MakeThreadID()
	if !strings.HasPrefix(id, "t-") {
		t.Errorf("expected prefix 't-', got %q", id)
	}
	// Should be t-YYYYMMDD-HHMMSS-16hexchars
	parts := strings.Split(id, "-")
	if len(parts) < 4 {
		t.Errorf("expected at least 4 parts in ID, got %d: %q", len(parts), id)
	}
}
