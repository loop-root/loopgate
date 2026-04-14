package loopgate

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	memorypkg "loopgate/internal/memory"
)

const (
	claudeHookSessionStateSchemaVersion = "1"
	claudeHookSessionStateActive        = "active"
	claudeHookSessionStateEnded         = "ended"
	claudeHookContinuityThreadsFileName = "continuity_threads.json"
	claudeHookContinuityLedgerFileName  = "continuity_ledger.jsonl"
)

type claudeHookSessionStateFile struct {
	SchemaVersion string                  `json:"schema_version"`
	Sessions      []claudeHookSessionWire `json:"sessions,omitempty"`
}

type claudeHookSessionWire struct {
	SessionID        string `json:"session_id"`
	StorageKey       string `json:"storage_key"`
	State            string `json:"state"`
	StartedAtUTC     string `json:"started_at_utc"`
	LastSeenAtUTC    string `json:"last_seen_at_utc,omitempty"`
	EndedAtUTC       string `json:"ended_at_utc,omitempty"`
	ExitReason       string `json:"exit_reason,omitempty"`
	CurrentThreadID  string `json:"current_thread_id,omitempty"`
	NextThreadID     string `json:"next_thread_id,omitempty"`
	PreviousThreadID string `json:"previous_thread_id,omitempty"`
}

type claudeHookSessionRecord struct {
	SessionID        string
	StorageKey       string
	State            string
	StartedAtUTC     string
	LastSeenAtUTC    string
	EndedAtUTC       string
	ExitReason       string
	CurrentThreadID  string
	NextThreadID     string
	PreviousThreadID string
}

func claudeHookSessionStorageKey(rawSessionID string) string {
	trimmedSessionID := strings.TrimSpace(rawSessionID)
	if trimmedSessionID == "" {
		return ""
	}
	hashSum := sha256.Sum256([]byte(trimmedSessionID))
	return fmt.Sprintf("chs%x", hashSum[:16])
}

func (server *Server) ensureClaudeHookSessionBinding(rawSessionID string, hookEventName string, hookReason string) (claudeHookSessionRecord, error) {
	server.claudeHookSessionsMu.Lock()
	defer server.claudeHookSessionsMu.Unlock()
	return server.ensureClaudeHookSessionBindingLocked(rawSessionID, hookEventName, hookReason)
}

func (server *Server) ensureClaudeHookSessionBindingLocked(rawSessionID string, hookEventName string, hookReason string) (claudeHookSessionRecord, error) {
	validatedSessionID := strings.TrimSpace(rawSessionID)
	if validatedSessionID == "" {
		return claudeHookSessionRecord{}, nil
	}

	stateFile, err := server.loadClaudeHookSessionStateLocked()
	if err != nil {
		return claudeHookSessionRecord{}, err
	}

	nowUTC := server.now().UTC()
	storageKey := claudeHookSessionStorageKey(validatedSessionID)
	continuityThreadsPath, continuityLedgerPath := server.claudeHookSessionContinuityPaths(storageKey)
	if err := os.MkdirAll(filepath.Dir(continuityThreadsPath), 0o700); err != nil {
		return claudeHookSessionRecord{}, fmt.Errorf("ensure claude hook session dir: %w", err)
	}
	continuityThreadsState, err := memorypkg.LoadOrInitContinuityThreads(continuityThreadsPath, server.claudeHookContinuityThresholds(), nowUTC)
	if err != nil {
		return claudeHookSessionRecord{}, fmt.Errorf("load claude hook continuity threads: %w", err)
	}

	record, found := stateFile[validatedSessionID]
	if !found {
		record = claudeHookSessionRecord{
			SessionID:    validatedSessionID,
			StorageKey:   storageKey,
			StartedAtUTC: nowUTC.Format(time.RFC3339Nano),
		}
	}
	if strings.TrimSpace(record.StorageKey) == "" {
		record.StorageKey = storageKey
	}
	if strings.TrimSpace(record.StartedAtUTC) == "" {
		record.StartedAtUTC = nowUTC.Format(time.RFC3339Nano)
	}

	record.LastSeenAtUTC = nowUTC.Format(time.RFC3339Nano)
	switch normalizedClaudeCodeHookEventName(hookEventName) {
	case claudeCodeHookEventSessionEnd:
		rolledContinuityState, _, rolloverErr := memorypkg.RolloverContinuityThreads(
			continuityThreadsState,
			continuityLedgerPath,
			nowUTC,
			"session_end",
		)
		if rolloverErr != nil {
			return claudeHookSessionRecord{}, fmt.Errorf("roll claude hook continuity threads: %w", rolloverErr)
		}
		continuityThreadsState = rolledContinuityState
		record.State = claudeHookSessionStateEnded
		record.EndedAtUTC = nowUTC.Format(time.RFC3339Nano)
		record.ExitReason = strings.TrimSpace(hookReason)
	default:
		record.State = claudeHookSessionStateActive
		record.EndedAtUTC = ""
		record.ExitReason = ""
	}

	record.CurrentThreadID = continuityThreadsState.CurrentThreadID
	record.NextThreadID = continuityThreadsState.NextThreadID
	record.PreviousThreadID = continuityThreadsState.PreviousThreadID
	stateFile[validatedSessionID] = record

	if err := memorypkg.SaveContinuityThreads(continuityThreadsPath, continuityThreadsState); err != nil {
		return claudeHookSessionRecord{}, fmt.Errorf("save claude hook continuity threads: %w", err)
	}
	if err := server.saveClaudeHookSessionStateLocked(stateFile); err != nil {
		return claudeHookSessionRecord{}, err
	}
	return record, nil
}

func (server *Server) claudeHookSessionRoot(storageKey string) string {
	return filepath.Join(server.claudeHookSessionsRoot, storageKey)
}

func (server *Server) claudeHookSessionContinuityPaths(storageKey string) (string, string) {
	sessionRoot := server.claudeHookSessionRoot(storageKey)
	return filepath.Join(sessionRoot, claudeHookContinuityThreadsFileName), filepath.Join(sessionRoot, claudeHookContinuityLedgerFileName)
}

func (server *Server) claudeHookContinuityThresholds() memorypkg.ContinuityInspectionThresholds {
	policyRuntime := server.currentPolicyRuntime()
	return memorypkg.ContinuityInspectionThresholds{
		SubmitPreviousMinEvents:       policyRuntime.policy.Memory.SubmitPreviousMinEvents,
		SubmitPreviousMinPayloadBytes: policyRuntime.policy.Memory.SubmitPreviousMinPayloadBytes,
		SubmitPreviousMinPromptTokens: policyRuntime.policy.Memory.SubmitPreviousMinPromptTokens,
	}
}

func (server *Server) loadClaudeHookSessionStateLocked() (map[string]claudeHookSessionRecord, error) {
	rawStateBytes, err := os.ReadFile(server.claudeHookSessionsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]claudeHookSessionRecord{}, nil
		}
		return nil, fmt.Errorf("read claude hook session state: %w", err)
	}

	var parsedStateFile claudeHookSessionStateFile
	decoder := json.NewDecoder(strings.NewReader(string(rawStateBytes)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parsedStateFile); err != nil {
		return nil, fmt.Errorf("decode claude hook session state: %w", err)
	}
	if schemaVersion := strings.TrimSpace(parsedStateFile.SchemaVersion); schemaVersion != "" && schemaVersion != claudeHookSessionStateSchemaVersion {
		return nil, fmt.Errorf("unsupported claude hook session state schema version %q", schemaVersion)
	}

	recordsBySessionID := make(map[string]claudeHookSessionRecord, len(parsedStateFile.Sessions))
	for _, sessionWire := range parsedStateFile.Sessions {
		sessionID := strings.TrimSpace(sessionWire.SessionID)
		if sessionID == "" {
			continue
		}
		recordsBySessionID[sessionID] = claudeHookSessionRecord{
			SessionID:        sessionID,
			StorageKey:       strings.TrimSpace(sessionWire.StorageKey),
			State:            strings.TrimSpace(sessionWire.State),
			StartedAtUTC:     strings.TrimSpace(sessionWire.StartedAtUTC),
			LastSeenAtUTC:    strings.TrimSpace(sessionWire.LastSeenAtUTC),
			EndedAtUTC:       strings.TrimSpace(sessionWire.EndedAtUTC),
			ExitReason:       strings.TrimSpace(sessionWire.ExitReason),
			CurrentThreadID:  strings.TrimSpace(sessionWire.CurrentThreadID),
			NextThreadID:     strings.TrimSpace(sessionWire.NextThreadID),
			PreviousThreadID: strings.TrimSpace(sessionWire.PreviousThreadID),
		}
	}
	return recordsBySessionID, nil
}

func (server *Server) saveClaudeHookSessionStateLocked(stateBySessionID map[string]claudeHookSessionRecord) error {
	sessionIDs := make([]string, 0, len(stateBySessionID))
	for sessionID := range stateBySessionID {
		sessionIDs = append(sessionIDs, sessionID)
	}
	sort.Strings(sessionIDs)

	stateFile := claudeHookSessionStateFile{
		SchemaVersion: claudeHookSessionStateSchemaVersion,
		Sessions:      make([]claudeHookSessionWire, 0, len(sessionIDs)),
	}
	for _, sessionID := range sessionIDs {
		record := stateBySessionID[sessionID]
		stateFile.Sessions = append(stateFile.Sessions, claudeHookSessionWire{
			SessionID:        record.SessionID,
			StorageKey:       record.StorageKey,
			State:            record.State,
			StartedAtUTC:     record.StartedAtUTC,
			LastSeenAtUTC:    record.LastSeenAtUTC,
			EndedAtUTC:       record.EndedAtUTC,
			ExitReason:       record.ExitReason,
			CurrentThreadID:  record.CurrentThreadID,
			NextThreadID:     record.NextThreadID,
			PreviousThreadID: record.PreviousThreadID,
		})
	}

	stateBytes, err := json.MarshalIndent(stateFile, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal claude hook session state: %w", err)
	}
	if err := atomicWritePrivateJSON(server.claudeHookSessionsPath, stateBytes); err != nil {
		return fmt.Errorf("save claude hook session state: %w", err)
	}
	return nil
}
