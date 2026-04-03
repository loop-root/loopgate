package threadstore

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Store manages thread persistence for Haven Messenger.
//
// Invariants:
//   - Thread JSONL files are append-only; events are never modified in place.
//   - thread_index.json is a rebuildable derived view. If missing or corrupt,
//     RebuildIndex reconstructs it from thread files.
//   - Raw secrets and raw tool output are never stored. AppendEvent performs
//     centralized redaction as defense-in-depth before persisting events.
type Store struct {
	mu          sync.Mutex
	rootDir     string // parent directory: {rootDir}/threads/ and {rootDir}/thread_index.json
	workspaceID string // identifies the workspace/repo this store is bound to
	index       ThreadIndex
	indexErr    error // non-nil if the index could not be loaded or rebuilt
}

// NewStore opens or creates a thread store rooted at rootDir.
// If the index is missing or corrupt, it is rebuilt from thread files.
//
// The optional workspaceID scopes the store to a specific workspace/repo.
// When set, NewThread stamps threads with this ID and ListThreads only
// returns threads belonging to this workspace. Pass "" to disable scoping
// (backward-compatible behavior).
func NewStore(rootDir string, workspaceID ...string) (*Store, error) {
	threadsDir := filepath.Join(rootDir, "threads")
	if err := os.MkdirAll(threadsDir, 0o700); err != nil {
		return nil, fmt.Errorf("create threads directory: %w", err)
	}

	wsID := ""
	if len(workspaceID) > 0 {
		wsID = workspaceID[0]
	}

	store := &Store{rootDir: rootDir, workspaceID: wsID}
	if err := store.loadOrRebuildIndex(); err != nil {
		// Index load/rebuild failure is not fatal — the store is usable, but
		// ListThreads may return stale data until the next successful write.
		store.indexErr = err
	}
	return store, nil
}

// NewThread creates a new empty thread and updates the index.
func (s *Store) NewThread() (ThreadSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	threadID := MakeThreadID()
	now := NowUTC()

	// Create the empty JSONL file to establish the thread on disk.
	threadPath := s.threadFilePath(threadID)
	threadFile, err := os.OpenFile(threadPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return ThreadSummary{}, fmt.Errorf("create thread file: %w", err)
	}
	_ = threadFile.Close()

	summary := ThreadSummary{
		ThreadID:    threadID,
		Title:       "",
		WorkspaceID: s.workspaceID,
		CreatedAt:   now,
		UpdatedAt:   now,
		EventCount:  0,
	}
	s.index.Threads = append(s.index.Threads, summary)
	s.persistIndexBestEffort()
	return summary, nil
}

// ListThreads returns thread summaries, most recently updated first.
// If the store has a workspace ID, only threads belonging to that workspace
// are returned. This enforces workspace/repo boundary isolation.
func (s *Store) ListThreads() []ThreadSummary {
	s.mu.Lock()
	defer s.mu.Unlock()

	var result []ThreadSummary
	for _, t := range s.index.Threads {
		if s.workspaceID != "" && t.WorkspaceID != s.workspaceID {
			continue
		}
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].UpdatedAt > result[j].UpdatedAt
	})
	return result
}

// LoadThread reads all conversation events from a thread's JSONL file.
// If the store has a workspace ID, loading a thread from a different workspace
// is rejected to enforce workspace/repo boundary isolation.
func (s *Store) LoadThread(threadID string) ([]ConversationEvent, error) {
	if s.workspaceID != "" {
		s.mu.Lock()
		ownerWS := s.threadWorkspaceID(threadID)
		s.mu.Unlock()
		if ownerWS != "" && ownerWS != s.workspaceID {
			return nil, fmt.Errorf("thread %s belongs to workspace %q, not %q", threadID, ownerWS, s.workspaceID)
		}
	}
	threadPath := s.threadFilePath(threadID)
	return loadEventsFromFile(threadPath)
}

// threadWorkspaceID returns the workspace ID for a thread from the index.
// Must be called with s.mu held.
func (s *Store) threadWorkspaceID(threadID string) string {
	for _, t := range s.index.Threads {
		if t.ThreadID == threadID {
			return t.WorkspaceID
		}
	}
	return ""
}

// AppendEvent appends a single conversation event to a thread's JSONL file
// and updates the in-memory index. The index file is updated best-effort;
// if the index write fails, the event is still persisted in the thread file.
//
// Security invariant: event data is sanitized before persistence.
// Sensitive fields (raw tool output, secrets, full model payloads) are
// redacted at this layer as defense-in-depth, regardless of caller discipline.
func (s *Store) AppendEvent(threadID string, event ConversationEvent) error {
	event.SchemaVersion = ConversationEventSchemaVersion
	event.ThreadID = threadID
	if event.TS == "" {
		event.TS = NowUTC()
	}

	// Centralized redaction: sanitize event data before persistence.
	event.Data = redactEventData(event.Type, event.Data)

	encodedEvent, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	threadPath := s.threadFilePath(threadID)
	threadFile, err := os.OpenFile(threadPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("open thread file: %w", err)
	}
	if _, err := threadFile.Write(append(encodedEvent, '\n')); err != nil {
		_ = threadFile.Close()
		return fmt.Errorf("append event: %w", err)
	}
	if err := threadFile.Sync(); err != nil {
		_ = threadFile.Close()
		return fmt.Errorf("sync thread file: %w", err)
	}
	if err := threadFile.Close(); err != nil {
		return fmt.Errorf("close thread file: %w", err)
	}

	// Update in-memory index.
	s.updateIndexForEvent(threadID, event)
	s.persistIndexBestEffort()
	return nil
}

// RebuildIndex reconstructs the thread index from thread JSONL files on disk.
// This is safe to call at any time and handles the crash-recovery case where
// an event was appended successfully but the index update failed.
func (s *Store) RebuildIndex() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.rebuildIndexLocked()
}

// SetThreadFolder moves a thread into the given folder (empty string = unfiled).
func (s *Store) SetThreadFolder(threadID string, folder string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, t := range s.index.Threads {
		if t.ThreadID == threadID {
			s.index.Threads[i].Folder = folder
			s.persistIndexBestEffort()
			return nil
		}
	}
	return fmt.Errorf("thread %s not found", threadID)
}

// RenameThread sets the title of a thread.
func (s *Store) RenameThread(threadID string, title string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, t := range s.index.Threads {
		if t.ThreadID == threadID {
			s.index.Threads[i].Title = title
			s.persistIndexBestEffort()
			return nil
		}
	}
	return fmt.Errorf("thread %s not found", threadID)
}

// ListFolders returns the distinct non-empty folder names across all threads.
func (s *Store) ListFolders() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	seen := make(map[string]struct{})
	var folders []string
	for _, t := range s.index.Threads {
		if s.workspaceID != "" && t.WorkspaceID != s.workspaceID {
			continue
		}
		if t.Folder != "" {
			if _, ok := seen[t.Folder]; !ok {
				seen[t.Folder] = struct{}{}
				folders = append(folders, t.Folder)
			}
		}
	}
	sort.Strings(folders)
	return folders
}

// --- internal methods ---

func (s *Store) threadFilePath(threadID string) string {
	return filepath.Join(s.rootDir, "threads", threadID+".jsonl")
}

func (s *Store) indexFilePath() string {
	return filepath.Join(s.rootDir, "thread_index.json")
}

func (s *Store) loadOrRebuildIndex() error {
	indexPath := s.indexFilePath()
	indexBytes, err := os.ReadFile(indexPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s.rebuildIndexLocked()
		}
		return s.rebuildIndexLocked()
	}

	var loadedIndex ThreadIndex
	if err := json.Unmarshal(indexBytes, &loadedIndex); err != nil {
		return s.rebuildIndexLocked()
	}

	s.index = loadedIndex
	return nil
}

func (s *Store) rebuildIndexLocked() error {
	threadsDir := filepath.Join(s.rootDir, "threads")
	entries, err := os.ReadDir(threadsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.index = ThreadIndex{SchemaVersion: ThreadIndexSchemaVersion}
			return nil
		}
		return fmt.Errorf("read threads directory: %w", err)
	}

	var rebuiltThreads []ThreadSummary
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		threadID := strings.TrimSuffix(entry.Name(), ".jsonl")
		threadPath := filepath.Join(threadsDir, entry.Name())

		events, loadErr := loadEventsFromFile(threadPath)
		if loadErr != nil {
			continue // skip corrupt thread files during rebuild
		}

		summary := buildSummaryFromEvents(threadID, events)
		rebuiltThreads = append(rebuiltThreads, summary)
	}

	s.index = ThreadIndex{
		SchemaVersion: ThreadIndexSchemaVersion,
		Threads:       rebuiltThreads,
	}
	s.persistIndexBestEffort()
	return nil
}

func (s *Store) updateIndexForEvent(threadID string, event ConversationEvent) {
	for i, thread := range s.index.Threads {
		if thread.ThreadID == threadID {
			s.index.Threads[i].UpdatedAt = event.TS
			s.index.Threads[i].EventCount++
			if s.index.Threads[i].Title == "" && event.Type == EventUserMessage {
				if text, ok := event.Data["text"].(string); ok {
					s.index.Threads[i].Title = truncateTitle(text, 80)
				}
			}
			return
		}
	}
	// Thread not in index (shouldn't happen if NewThread was called, but handle gracefully).
	title := ""
	if event.Type == EventUserMessage {
		if text, ok := event.Data["text"].(string); ok {
			title = truncateTitle(text, 80)
		}
	}
	s.index.Threads = append(s.index.Threads, ThreadSummary{
		ThreadID:   threadID,
		Title:      title,
		CreatedAt:  event.TS,
		UpdatedAt:  event.TS,
		EventCount: 1,
	})
}

func (s *Store) persistIndexBestEffort() {
	s.index.SchemaVersion = ThreadIndexSchemaVersion
	indexBytes, err := json.MarshalIndent(s.index, "", "  ")
	if err != nil {
		return
	}

	indexPath := s.indexFilePath()
	tmpPath := indexPath + ".tmp"
	if err := os.WriteFile(tmpPath, indexBytes, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmpPath, indexPath)
}

func loadEventsFromFile(filePath string) ([]ConversationEvent, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var events []ConversationEvent
	scanner := bufio.NewScanner(file)
	// Allow up to 1MB per line for large assistant responses.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event ConversationEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue // skip malformed lines, preserve rest of thread
		}
		events = append(events, event)
	}
	return events, scanner.Err()
}

func buildSummaryFromEvents(threadID string, events []ConversationEvent) ThreadSummary {
	summary := ThreadSummary{
		ThreadID:   threadID,
		EventCount: len(events),
	}
	for _, event := range events {
		if summary.CreatedAt == "" {
			summary.CreatedAt = event.TS
		}
		summary.UpdatedAt = event.TS
		if summary.Title == "" && event.Type == EventUserMessage {
			if text, ok := event.Data["text"].(string); ok {
				summary.Title = truncateTitle(text, 80)
			}
		}
	}
	if summary.CreatedAt == "" {
		summary.CreatedAt = NowUTC()
	}
	if summary.UpdatedAt == "" {
		summary.UpdatedAt = summary.CreatedAt
	}
	return summary
}

func truncateTitle(text string, maxLen int) string {
	trimmed := strings.TrimSpace(text)
	// Replace newlines with spaces for single-line titles.
	trimmed = strings.Join(strings.Fields(trimmed), " ")
	if len(trimmed) > maxLen {
		return trimmed[:maxLen] + "..."
	}
	return trimmed
}
