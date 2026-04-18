package loopgate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	claudeHookApprovalStateSchemaVersion = "1"
	claudeHookApprovalsFileName          = "hook_approvals.json"

	claudeHookApprovalStatePending         = "pending"
	claudeHookApprovalStateExecuted        = "executed"
	claudeHookApprovalStateExecutionFailed = "execution_failed"
	claudeHookApprovalStateAbandoned       = "abandoned"

	claudeHookApprovalSurfaceInlineClaude = "inline_claude"
	claudeHookApprovalSurfaceLoopgateUI   = "loopgate_window"
)

type claudeHookApprovalStateFile struct {
	SchemaVersion string                   `json:"schema_version"`
	Approvals     []claudeHookApprovalWire `json:"approvals,omitempty"`
}

type claudeHookApprovalWire struct {
	ApprovalRequestID        string `json:"approval_request_id"`
	SessionID                string `json:"session_id"`
	ToolUseID                string `json:"tool_use_id"`
	ToolName                 string `json:"tool_name"`
	ApprovalSurface          string `json:"approval_surface,omitempty"`
	ToolFingerprintSHA256    string `json:"tool_fingerprint_sha256"`
	RequestFingerprintSHA256 string `json:"request_fingerprint_sha256,omitempty"`
	Reason                   string `json:"reason,omitempty"`
	State                    string `json:"state"`
	CreatedAtUTC             string `json:"created_at_utc"`
	ResolvedAtUTC            string `json:"resolved_at_utc,omitempty"`
	ResolutionReason         string `json:"resolution_reason,omitempty"`
	HookEventName            string `json:"hook_event_name,omitempty"`
	HookInterrupted          bool   `json:"hook_interrupted,omitempty"`
}

type claudeHookApprovalRecord struct {
	ApprovalRequestID        string
	SessionID                string
	ToolUseID                string
	ToolName                 string
	ApprovalSurface          string
	ToolFingerprintSHA256    string
	RequestFingerprintSHA256 string
	Reason                   string
	State                    string
	CreatedAtUTC             string
	ResolvedAtUTC            string
	ResolutionReason         string
	HookEventName            string
	HookInterrupted          bool
}

func claudeHookApprovalFingerprint(req controlapipkg.HookPreValidateRequest) (string, error) {
	canonicalToolInputBytes, err := json.Marshal(req.ToolInput)
	if err != nil {
		return "", fmt.Errorf("marshal tool input fingerprint: %w", err)
	}
	hashState := sha256.New()
	for _, part := range []string{
		strings.TrimSpace(req.ToolName),
		"\n",
		strings.TrimSpace(req.ToolUseID),
		"\n",
		strings.TrimSpace(req.CWD),
		"\n",
		string(canonicalToolInputBytes),
	} {
		if _, err := hashState.Write([]byte(part)); err != nil {
			return "", fmt.Errorf("hash tool fingerprint: %w", err)
		}
	}
	return hex.EncodeToString(hashState.Sum(nil)), nil
}

func claudeHookApprovalRequestFingerprint(req controlapipkg.HookPreValidateRequest) (string, error) {
	canonicalToolInputBytes, err := json.Marshal(req.ToolInput)
	if err != nil {
		return "", fmt.Errorf("marshal tool request fingerprint: %w", err)
	}
	hashState := sha256.New()
	for _, part := range []string{
		strings.TrimSpace(req.ToolName),
		"\n",
		strings.TrimSpace(req.CWD),
		"\n",
		string(canonicalToolInputBytes),
	} {
		if _, err := hashState.Write([]byte(part)); err != nil {
			return "", fmt.Errorf("hash tool request fingerprint: %w", err)
		}
	}
	return hex.EncodeToString(hashState.Sum(nil)), nil
}

func cloneClaudeHookApprovalRecords(recordsByToolUseID map[string]claudeHookApprovalRecord) map[string]claudeHookApprovalRecord {
	if recordsByToolUseID == nil {
		return map[string]claudeHookApprovalRecord{}
	}
	clonedRecords := make(map[string]claudeHookApprovalRecord, len(recordsByToolUseID))
	for toolUseID, approvalRecord := range recordsByToolUseID {
		clonedRecords[toolUseID] = approvalRecord
	}
	return clonedRecords
}

func (server *Server) createClaudeHookApprovalRequest(req controlapipkg.HookPreValidateRequest, approvalReason string) (claudeHookApprovalRecord, claudeHookSessionRecord, bool, map[string]claudeHookApprovalRecord, error) {
	validatedSessionID := strings.TrimSpace(req.SessionID)
	validatedToolUseID := strings.TrimSpace(req.ToolUseID)
	if validatedSessionID == "" {
		return claudeHookApprovalRecord{}, claudeHookSessionRecord{}, false, nil, fmt.Errorf("approval-tracked hook requires session_id")
	}
	if validatedToolUseID == "" {
		return claudeHookApprovalRecord{}, claudeHookSessionRecord{}, false, nil, fmt.Errorf("approval-tracked hook requires tool_use_id")
	}

	server.claudeHookRuntime.mu.Lock()
	defer server.claudeHookRuntime.mu.Unlock()

	sessionRecord, err := server.ensureClaudeHookSessionBindingLocked(validatedSessionID, claudeCodeHookEventPreToolUse, "")
	if err != nil {
		return claudeHookApprovalRecord{}, claudeHookSessionRecord{}, false, nil, err
	}
	approvalRecordsByToolUseID, err := server.loadClaudeHookApprovalStateLocked(sessionRecord.StorageKey)
	if err != nil {
		return claudeHookApprovalRecord{}, claudeHookSessionRecord{}, false, nil, err
	}
	previousRecordsByToolUseID := cloneClaudeHookApprovalRecords(approvalRecordsByToolUseID)

	fingerprintSHA256, err := claudeHookApprovalFingerprint(req)
	if err != nil {
		return claudeHookApprovalRecord{}, claudeHookSessionRecord{}, false, nil, err
	}
	requestFingerprintSHA256, err := claudeHookApprovalRequestFingerprint(req)
	if err != nil {
		return claudeHookApprovalRecord{}, claudeHookSessionRecord{}, false, nil, err
	}
	if existingRecord, found := approvalRecordsByToolUseID[validatedToolUseID]; found {
		if existingRecord.ToolFingerprintSHA256 != fingerprintSHA256 {
			return claudeHookApprovalRecord{}, claudeHookSessionRecord{}, false, nil, fmt.Errorf("tool_use_id %q was reused with different tool input", validatedToolUseID)
		}
		return existingRecord, sessionRecord, false, previousRecordsByToolUseID, nil
	}

	randomSuffix, err := randomHex(8)
	if err != nil {
		return claudeHookApprovalRecord{}, claudeHookSessionRecord{}, false, nil, fmt.Errorf("generate hook approval id: %w", err)
	}
	nowUTC := server.now().UTC().Format(time.RFC3339Nano)
	approvalRecord := claudeHookApprovalRecord{
		ApprovalRequestID:        "hookapr_" + randomSuffix,
		SessionID:                validatedSessionID,
		ToolUseID:                validatedToolUseID,
		ToolName:                 strings.TrimSpace(req.ToolName),
		ApprovalSurface:          claudeHookApprovalSurfaceInlineClaude,
		ToolFingerprintSHA256:    fingerprintSHA256,
		RequestFingerprintSHA256: requestFingerprintSHA256,
		Reason:                   strings.TrimSpace(approvalReason),
		State:                    claudeHookApprovalStatePending,
		CreatedAtUTC:             nowUTC,
		HookEventName:            claudeCodeHookEventPreToolUse,
	}
	approvalRecordsByToolUseID[validatedToolUseID] = approvalRecord
	if err := server.saveClaudeHookApprovalStateLocked(sessionRecord.StorageKey, approvalRecordsByToolUseID); err != nil {
		return claudeHookApprovalRecord{}, claudeHookSessionRecord{}, false, nil, err
	}
	return approvalRecord, sessionRecord, true, previousRecordsByToolUseID, nil
}

func (server *Server) transitionClaudeHookApproval(req controlapipkg.HookPreValidateRequest, resolutionState string, resolutionReason string) (claudeHookApprovalRecord, claudeHookSessionRecord, bool, bool, map[string]claudeHookApprovalRecord, error) {
	validatedSessionID := strings.TrimSpace(req.SessionID)
	validatedToolUseID := strings.TrimSpace(req.ToolUseID)
	if validatedSessionID == "" || validatedToolUseID == "" {
		return claudeHookApprovalRecord{}, claudeHookSessionRecord{}, false, false, nil, nil
	}

	server.claudeHookRuntime.mu.Lock()
	defer server.claudeHookRuntime.mu.Unlock()

	sessionRecord, err := server.ensureClaudeHookSessionBindingLocked(validatedSessionID, normalizedClaudeCodeHookEventName(req.HookEventName), req.HookReason)
	if err != nil {
		return claudeHookApprovalRecord{}, claudeHookSessionRecord{}, false, false, nil, err
	}
	approvalRecordsByToolUseID, err := server.loadClaudeHookApprovalStateLocked(sessionRecord.StorageKey)
	if err != nil {
		return claudeHookApprovalRecord{}, claudeHookSessionRecord{}, false, false, nil, err
	}
	previousRecordsByToolUseID := cloneClaudeHookApprovalRecords(approvalRecordsByToolUseID)
	approvalRecord, found := approvalRecordsByToolUseID[validatedToolUseID]
	if !found {
		return claudeHookApprovalRecord{}, sessionRecord, false, false, previousRecordsByToolUseID, nil
	}

	fingerprintSHA256, err := claudeHookApprovalFingerprint(req)
	if err != nil {
		return claudeHookApprovalRecord{}, claudeHookSessionRecord{}, false, false, nil, err
	}
	if approvalRecord.ToolFingerprintSHA256 != fingerprintSHA256 {
		return claudeHookApprovalRecord{}, claudeHookSessionRecord{}, false, false, nil, fmt.Errorf("hook approval %q tool fingerprint mismatch", approvalRecord.ApprovalRequestID)
	}
	if approvalRecord.State == resolutionState {
		return approvalRecord, sessionRecord, true, false, previousRecordsByToolUseID, nil
	}
	if approvalRecord.State != claudeHookApprovalStatePending {
		return approvalRecord, sessionRecord, true, false, previousRecordsByToolUseID, nil
	}

	approvalRecord.State = resolutionState
	approvalRecord.ResolvedAtUTC = server.now().UTC().Format(time.RFC3339Nano)
	approvalRecord.ResolutionReason = strings.TrimSpace(resolutionReason)
	approvalRecord.HookEventName = normalizedClaudeCodeHookEventName(req.HookEventName)
	approvalRecord.HookInterrupted = req.HookInterrupted
	approvalRecordsByToolUseID[validatedToolUseID] = approvalRecord
	if err := server.saveClaudeHookApprovalStateLocked(sessionRecord.StorageKey, approvalRecordsByToolUseID); err != nil {
		return claudeHookApprovalRecord{}, claudeHookSessionRecord{}, false, false, nil, err
	}
	return approvalRecord, sessionRecord, true, true, previousRecordsByToolUseID, nil
}

func (server *Server) abandonPendingClaudeHookApprovalsWithPrevious(rawSessionID string, hookReason string) (int, claudeHookSessionRecord, map[string]claudeHookApprovalRecord, error) {
	validatedSessionID := strings.TrimSpace(rawSessionID)
	if validatedSessionID == "" {
		return 0, claudeHookSessionRecord{}, nil, nil
	}

	server.claudeHookRuntime.mu.Lock()
	defer server.claudeHookRuntime.mu.Unlock()

	sessionRecord, err := server.ensureClaudeHookSessionBindingLocked(validatedSessionID, claudeCodeHookEventSessionEnd, hookReason)
	if err != nil {
		return 0, claudeHookSessionRecord{}, nil, err
	}
	approvalRecordsByToolUseID, err := server.loadClaudeHookApprovalStateLocked(sessionRecord.StorageKey)
	if err != nil {
		return 0, claudeHookSessionRecord{}, nil, err
	}
	previousRecordsByToolUseID := cloneClaudeHookApprovalRecords(approvalRecordsByToolUseID)

	abandonedCount := 0
	resolvedAtUTC := server.now().UTC().Format(time.RFC3339Nano)
	for toolUseID, approvalRecord := range approvalRecordsByToolUseID {
		if approvalRecord.State != claudeHookApprovalStatePending {
			continue
		}
		approvalRecord.State = claudeHookApprovalStateAbandoned
		approvalRecord.ResolvedAtUTC = resolvedAtUTC
		approvalRecord.ResolutionReason = strings.TrimSpace(hookReason)
		approvalRecord.HookEventName = claudeCodeHookEventSessionEnd
		approvalRecordsByToolUseID[toolUseID] = approvalRecord
		abandonedCount++
	}
	if abandonedCount == 0 {
		return 0, sessionRecord, previousRecordsByToolUseID, nil
	}
	if err := server.saveClaudeHookApprovalStateLocked(sessionRecord.StorageKey, approvalRecordsByToolUseID); err != nil {
		return 0, claudeHookSessionRecord{}, nil, err
	}
	return abandonedCount, sessionRecord, previousRecordsByToolUseID, nil
}

func (server *Server) restoreClaudeHookApprovalState(rawSessionID string, previousRecordsByToolUseID map[string]claudeHookApprovalRecord) error {
	validatedSessionID := strings.TrimSpace(rawSessionID)
	if validatedSessionID == "" {
		return nil
	}

	server.claudeHookRuntime.mu.Lock()
	defer server.claudeHookRuntime.mu.Unlock()

	sessionStateByID, err := server.loadClaudeHookSessionStateLocked()
	if err != nil {
		return err
	}
	sessionRecord, found := sessionStateByID[validatedSessionID]
	storageKey := claudeHookSessionStorageKey(validatedSessionID)
	if found && strings.TrimSpace(sessionRecord.StorageKey) != "" {
		storageKey = sessionRecord.StorageKey
	}
	return server.saveClaudeHookApprovalStateLocked(storageKey, previousRecordsByToolUseID)
}

func (server *Server) findClaudeHookApprovalByRequest(req controlapipkg.HookPreValidateRequest) (claudeHookApprovalRecord, claudeHookSessionRecord, bool, error) {
	validatedSessionID := strings.TrimSpace(req.SessionID)
	if validatedSessionID == "" {
		return claudeHookApprovalRecord{}, claudeHookSessionRecord{}, false, nil
	}

	server.claudeHookRuntime.mu.Lock()
	defer server.claudeHookRuntime.mu.Unlock()

	sessionRecord, err := server.ensureClaudeHookSessionBindingLocked(validatedSessionID, normalizedClaudeCodeHookEventName(req.HookEventName), req.HookReason)
	if err != nil {
		return claudeHookApprovalRecord{}, claudeHookSessionRecord{}, false, err
	}
	approvalRecordsByToolUseID, err := server.loadClaudeHookApprovalStateLocked(sessionRecord.StorageKey)
	if err != nil {
		return claudeHookApprovalRecord{}, claudeHookSessionRecord{}, false, err
	}
	requestFingerprintSHA256, err := claudeHookApprovalRequestFingerprint(req)
	if err != nil {
		return claudeHookApprovalRecord{}, claudeHookSessionRecord{}, false, err
	}
	for _, approvalRecord := range approvalRecordsByToolUseID {
		if approvalRecord.RequestFingerprintSHA256 != requestFingerprintSHA256 {
			continue
		}
		return approvalRecord, sessionRecord, true, nil
	}
	return claudeHookApprovalRecord{}, sessionRecord, false, nil
}

func (server *Server) claudeHookApprovalsPath(storageKey string) string {
	return filepath.Join(server.claudeHookSessionRoot(storageKey), claudeHookApprovalsFileName)
}

func (server *Server) loadClaudeHookApprovalStateLocked(storageKey string) (map[string]claudeHookApprovalRecord, error) {
	rawStateBytes, err := os.ReadFile(server.claudeHookApprovalsPath(storageKey))
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]claudeHookApprovalRecord{}, nil
		}
		return nil, fmt.Errorf("read claude hook approval state: %w", err)
	}

	var parsedStateFile claudeHookApprovalStateFile
	if err := decodeJSONBytes(rawStateBytes, &parsedStateFile); err != nil {
		return nil, fmt.Errorf("decode claude hook approval state: %w", err)
	}
	if schemaVersion := strings.TrimSpace(parsedStateFile.SchemaVersion); schemaVersion != "" && schemaVersion != claudeHookApprovalStateSchemaVersion {
		return nil, fmt.Errorf("unsupported claude hook approval state schema version %q", schemaVersion)
	}

	recordsByToolUseID := make(map[string]claudeHookApprovalRecord, len(parsedStateFile.Approvals))
	for _, approvalWire := range parsedStateFile.Approvals {
		toolUseID := strings.TrimSpace(approvalWire.ToolUseID)
		if toolUseID == "" {
			continue
		}
		recordsByToolUseID[toolUseID] = claudeHookApprovalRecord{
			ApprovalRequestID:        strings.TrimSpace(approvalWire.ApprovalRequestID),
			SessionID:                strings.TrimSpace(approvalWire.SessionID),
			ToolUseID:                toolUseID,
			ToolName:                 strings.TrimSpace(approvalWire.ToolName),
			ApprovalSurface:          strings.TrimSpace(approvalWire.ApprovalSurface),
			ToolFingerprintSHA256:    strings.TrimSpace(approvalWire.ToolFingerprintSHA256),
			RequestFingerprintSHA256: strings.TrimSpace(approvalWire.RequestFingerprintSHA256),
			Reason:                   strings.TrimSpace(approvalWire.Reason),
			State:                    strings.TrimSpace(approvalWire.State),
			CreatedAtUTC:             strings.TrimSpace(approvalWire.CreatedAtUTC),
			ResolvedAtUTC:            strings.TrimSpace(approvalWire.ResolvedAtUTC),
			ResolutionReason:         strings.TrimSpace(approvalWire.ResolutionReason),
			HookEventName:            strings.TrimSpace(approvalWire.HookEventName),
			HookInterrupted:          approvalWire.HookInterrupted,
		}
	}
	return recordsByToolUseID, nil
}

func (server *Server) saveClaudeHookApprovalStateLocked(storageKey string, approvalsByToolUseID map[string]claudeHookApprovalRecord) error {
	if err := os.MkdirAll(server.claudeHookSessionRoot(storageKey), 0o700); err != nil {
		return fmt.Errorf("ensure claude hook approval dir: %w", err)
	}
	toolUseIDs := make([]string, 0, len(approvalsByToolUseID))
	for toolUseID := range approvalsByToolUseID {
		toolUseIDs = append(toolUseIDs, toolUseID)
	}
	sort.Strings(toolUseIDs)

	stateFile := claudeHookApprovalStateFile{
		SchemaVersion: claudeHookApprovalStateSchemaVersion,
		Approvals:     make([]claudeHookApprovalWire, 0, len(toolUseIDs)),
	}
	for _, toolUseID := range toolUseIDs {
		approvalRecord := approvalsByToolUseID[toolUseID]
		stateFile.Approvals = append(stateFile.Approvals, claudeHookApprovalWire(approvalRecord))
	}

	stateBytes, err := json.MarshalIndent(stateFile, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal claude hook approval state: %w", err)
	}
	if err := atomicWritePrivateJSON(server.claudeHookApprovalsPath(storageKey), stateBytes); err != nil {
		return fmt.Errorf("save claude hook approval state: %w", err)
	}
	return nil
}
