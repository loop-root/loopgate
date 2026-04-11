package loopgate

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"morph/internal/identifiers"
	"morph/internal/sandbox"
)

var errMorphlingSpawnDisabled = errors.New("morphling spawn is disabled")
var errMorphlingClassInvalid = errors.New("morphling class is invalid")
var errMorphlingInputInvalid = errors.New("morphling input is invalid")
var errMorphlingArtifactInvalid = errors.New("morphling artifact is invalid")
var errMorphlingActiveLimitReached = errors.New("morphling active limit reached")
var errMorphlingNotFound = errors.New("morphling was not found")
var errMorphlingStateInvalid = errors.New("morphling state is invalid")
var errMorphlingReviewInvalid = errors.New("morphling review is invalid")
var errMorphlingWorkerLaunchInvalid = errors.New("morphling worker launch token is invalid")
var errMorphlingWorkerTokenInvalid = errors.New("morphling worker session is invalid")
var errMorphlingWorkerSessionsSaturated = errors.New("morphling worker session store is at capacity")
var errMorphlingAuditUnavailable = errors.New("morphling audit is unavailable")

type morphlingRecord struct {
	SchemaVersion          string `json:"schema_version"`
	MorphlingID            string `json:"morphling_id"`
	TaskID                 string `json:"task_id"`
	RequestID              string `json:"request_id"`
	ParentControlSessionID string `json:"parent_control_session_id"`
	// TenantID is stamped at spawn from the parent capability token; non-empty rows reject cross-tenant parent access.
	TenantID               string   `json:"tenant_id,omitempty"`
	ActorLabel             string   `json:"actor_label"`
	ClientSessionLabel     string   `json:"client_session_label"`
	Class                  string   `json:"class"`
	GoalText               string   `json:"goal_text"`
	GoalHMAC               string   `json:"goal_hmac"`
	GoalHint               string   `json:"goal_hint"`
	State                  string   `json:"state"`
	StatusText             string   `json:"status_text,omitempty"`
	Outcome                string   `json:"outcome,omitempty"`
	WorkingDirRelativePath string   `json:"working_dir_relative_path,omitempty"`
	InputRelativePaths     []string `json:"input_relative_paths,omitempty"`
	AllowedRelativePaths   []string `json:"allowed_relative_paths,omitempty"`
	RequestedCapabilities  []string `json:"requested_capabilities,omitempty"`
	GrantedCapabilities    []string `json:"granted_capabilities,omitempty"`
	MemoryStrings          []string `json:"memory_strings,omitempty"`
	ArtifactCount          int      `json:"artifact_count,omitempty"`
	StagedArtifactRefs     []string `json:"staged_artifact_refs,omitempty"`
	ArtifactManifestHash   string   `json:"artifact_manifest_hash,omitempty"`
	RequiresReview         bool     `json:"requires_review"`
	TimeBudgetSeconds      int      `json:"time_budget_seconds,omitempty"`
	TokenBudget            int      `json:"token_budget,omitempty"`
	ApprovalID             string   `json:"approval_id,omitempty"`
	ApprovalDeadlineUTC    string   `json:"approval_deadline_utc,omitempty"`
	ReviewDeadlineUTC      string   `json:"review_deadline_utc,omitempty"`
	CreatedAtUTC           string   `json:"created_at_utc"`
	SpawnedAtUTC           string   `json:"spawned_at_utc,omitempty"`
	LastEventAtUTC         string   `json:"last_event_at_utc"`
	TokenExpiryUTC         string   `json:"token_expiry_utc,omitempty"`
	TerminatedAtUTC        string   `json:"terminated_at_utc,omitempty"`
	TerminationReason      string   `json:"termination_reason,omitempty"`
}

type morphlingStateFile struct {
	Morphlings []morphlingRecord `json:"morphlings"`
	Signature  string            `json:"signature,omitempty"`
}

func (record morphlingRecord) Validate() error {
	if err := identifiers.ValidateSafeIdentifier("morphling_id", record.MorphlingID); err != nil {
		return err
	}
	if strings.TrimSpace(record.TaskID) != "" {
		if err := identifiers.ValidateSafeIdentifier("task_id", record.TaskID); err != nil {
			return err
		}
	}
	if err := identifiers.ValidateSafeIdentifier("request_id", record.RequestID); err != nil {
		return err
	}
	if err := identifiers.ValidateSafeIdentifier("parent_control_session_id", record.ParentControlSessionID); err != nil {
		return err
	}
	if strings.TrimSpace(record.ActorLabel) != "" {
		if err := identifiers.ValidateSafeIdentifier("actor_label", record.ActorLabel); err != nil {
			return err
		}
	}
	if strings.TrimSpace(record.ClientSessionLabel) != "" {
		if err := identifiers.ValidateSafeIdentifier("client_session_label", record.ClientSessionLabel); err != nil {
			return err
		}
	}
	if err := identifiers.ValidateSafeIdentifier("morphling class", record.Class); err != nil {
		return err
	}
	if strings.TrimSpace(record.GoalText) == "" {
		return fmt.Errorf("goal_text is required")
	}
	if len(strings.TrimSpace(record.GoalText)) > 500 {
		return fmt.Errorf("goal_text exceeds maximum length")
	}
	if strings.TrimSpace(record.GoalHMAC) == "" {
		return fmt.Errorf("goal_hmac is required")
	}
	if len(record.GoalHint) > 80 {
		return fmt.Errorf("goal_hint exceeds maximum length")
	}
	if len(strings.TrimSpace(record.StatusText)) > 200 {
		return fmt.Errorf("status_text exceeds maximum length")
	}
	switch record.State {
	case morphlingStateRequested,
		morphlingStateAuthorizing,
		morphlingStatePendingSpawnApproval,
		morphlingStateSpawned,
		morphlingStateRunning,
		morphlingStateCompleting,
		morphlingStatePendingReview,
		morphlingStateTerminating,
		morphlingStateTerminated:
	default:
		return fmt.Errorf("invalid morphling state %q", record.State)
	}
	switch record.Outcome {
	case "", morphlingOutcomeApproved, morphlingOutcomeRejected, morphlingOutcomeCancelled, morphlingOutcomeFailed:
	default:
		return fmt.Errorf("invalid morphling outcome %q", record.Outcome)
	}
	if record.Outcome == "denied" {
		return fmt.Errorf("instantiated morphlings must not use outcome denied")
	}
	if record.State == morphlingStateTerminated || record.State == morphlingStateTerminating {
		if strings.TrimSpace(record.Outcome) == "" {
			return fmt.Errorf("%s morphling requires outcome", record.State)
		}
		if strings.TrimSpace(record.TerminationReason) == "" {
			return fmt.Errorf("%s morphling requires termination_reason", record.State)
		}
	} else if strings.TrimSpace(record.Outcome) != "" || strings.TrimSpace(record.TerminationReason) != "" || strings.TrimSpace(record.TerminatedAtUTC) != "" {
		return fmt.Errorf("non-terminating morphling must not set outcome, termination_reason, or terminated_at_utc")
	}
	if record.State == morphlingStatePendingSpawnApproval {
		if strings.TrimSpace(record.ApprovalID) == "" {
			return fmt.Errorf("pending_spawn_approval morphling requires approval_id")
		}
		if strings.TrimSpace(record.ApprovalDeadlineUTC) == "" {
			return fmt.Errorf("pending_spawn_approval morphling requires approval_deadline_utc")
		}
	}
	if record.State == morphlingStatePendingReview && strings.TrimSpace(record.ReviewDeadlineUTC) == "" {
		return fmt.Errorf("pending_review morphling requires review_deadline_utc")
	}
	if morphlingStateConsumesCapacity(record.State) && record.TimeBudgetSeconds <= 0 {
		return fmt.Errorf("active morphling requires positive time_budget_seconds")
	}
	if morphlingStateConsumesCapacity(record.State) && record.TokenBudget <= 0 {
		return fmt.Errorf("active morphling requires positive token_budget")
	}
	if record.State == morphlingStateSpawned ||
		record.State == morphlingStateRunning ||
		record.State == morphlingStateCompleting ||
		record.State == morphlingStatePendingReview {
		if strings.TrimSpace(record.WorkingDirRelativePath) == "" {
			return fmt.Errorf("%s morphling requires working_dir_relative_path", record.State)
		}
	}
	if strings.TrimSpace(record.WorkingDirRelativePath) != "" {
		if normalizedRelativePath, err := sandbox.NormalizeRelativePath(record.WorkingDirRelativePath); err != nil {
			return fmt.Errorf("working_dir_relative_path: %w", err)
		} else if normalizedRelativePath != record.WorkingDirRelativePath {
			return fmt.Errorf("working_dir_relative_path must be normalized")
		}
	}
	for _, relativePath := range record.InputRelativePaths {
		if normalizedRelativePath, err := sandbox.NormalizeRelativePath(relativePath); err != nil {
			return fmt.Errorf("input_relative_paths: %w", err)
		} else if normalizedRelativePath != relativePath {
			return fmt.Errorf("input_relative_paths must be normalized")
		}
	}
	for _, relativePath := range record.AllowedRelativePaths {
		if normalizedRelativePath, err := sandbox.NormalizeRelativePath(relativePath); err != nil {
			return fmt.Errorf("allowed_relative_paths: %w", err)
		} else if normalizedRelativePath != relativePath {
			return fmt.Errorf("allowed_relative_paths must be normalized")
		}
	}
	for _, capabilityName := range record.RequestedCapabilities {
		if err := identifiers.ValidateSafeIdentifier("requested capability", capabilityName); err != nil {
			return err
		}
	}
	for _, capabilityName := range record.GrantedCapabilities {
		if err := identifiers.ValidateSafeIdentifier("granted capability", capabilityName); err != nil {
			return err
		}
	}
	for _, memoryString := range record.MemoryStrings {
		if strings.TrimSpace(memoryString) == "" {
			return fmt.Errorf("memory_strings entries must be non-empty")
		}
		if len(memoryString) > 200 {
			return fmt.Errorf("memory_strings entries exceed maximum length")
		}
	}
	if len(record.MemoryStrings) > 8 {
		return fmt.Errorf("memory_strings exceeds maximum entry count")
	}
	if record.ArtifactCount < 0 {
		return fmt.Errorf("artifact_count must be non-negative")
	}
	for _, stagedArtifactRef := range record.StagedArtifactRefs {
		if !strings.HasPrefix(strings.TrimSpace(stagedArtifactRef), stagedArtifactRefPrefix) {
			return fmt.Errorf("staged_artifact_refs contains invalid ref %q", stagedArtifactRef)
		}
	}
	if strings.TrimSpace(record.CreatedAtUTC) == "" {
		return fmt.Errorf("created_at_utc is required")
	}
	if _, err := time.Parse(time.RFC3339Nano, record.CreatedAtUTC); err != nil {
		return fmt.Errorf("created_at_utc is invalid: %w", err)
	}
	if strings.TrimSpace(record.LastEventAtUTC) == "" {
		return fmt.Errorf("last_event_at_utc is required")
	}
	if _, err := time.Parse(time.RFC3339Nano, record.LastEventAtUTC); err != nil {
		return fmt.Errorf("last_event_at_utc is invalid: %w", err)
	}
	for _, timestampField := range []struct {
		name  string
		value string
	}{
		{name: "spawned_at_utc", value: record.SpawnedAtUTC},
		{name: "approval_deadline_utc", value: record.ApprovalDeadlineUTC},
		{name: "review_deadline_utc", value: record.ReviewDeadlineUTC},
		{name: "token_expiry_utc", value: record.TokenExpiryUTC},
		{name: "terminated_at_utc", value: record.TerminatedAtUTC},
	} {
		if strings.TrimSpace(timestampField.value) == "" {
			continue
		}
		if _, err := time.Parse(time.RFC3339Nano, timestampField.value); err != nil {
			return fmt.Errorf("%s is invalid: %w", timestampField.name, err)
		}
	}
	return nil
}

func loadMorphlingRecords(path string, hmacKey []byte) (map[string]morphlingRecord, error) {
	rawStateBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]morphlingRecord{}, nil
		}
		return nil, fmt.Errorf("read morphling records: %w", err)
	}

	var stateFile morphlingStateFile
	decoder := json.NewDecoder(bytes.NewReader(rawStateBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stateFile); err != nil {
		return nil, fmt.Errorf("decode morphling records: %w", err)
	}

	// Verify HMAC signature if key is available and a signature is present.
	// If the state file has no signature (e.g. written before signing was enabled),
	// we accept it but will sign on the next save.
	if len(hmacKey) > 0 && stateFile.Signature != "" {
		savedSignature := stateFile.Signature
		stateFile.Signature = ""
		contentBytes, marshalErr := json.Marshal(stateFile)
		if marshalErr != nil {
			return nil, fmt.Errorf("re-marshal morphling state for signature verification: %w", marshalErr)
		}
		mac := hmac.New(sha256.New, hmacKey)
		mac.Write(contentBytes)
		expectedSignature := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(savedSignature), []byte(expectedSignature)) {
			return nil, fmt.Errorf("morphling state file signature mismatch (tampered or key changed)")
		}
	}

	morphlingRecords := make(map[string]morphlingRecord, len(stateFile.Morphlings))
	for _, loadedRecord := range stateFile.Morphlings {
		if err := loadedRecord.Validate(); err != nil {
			return nil, fmt.Errorf("validate morphling record: %w", err)
		}
		morphlingRecords[loadedRecord.MorphlingID] = loadedRecord
	}
	return morphlingRecords, nil
}

func saveMorphlingRecords(path string, morphlingRecords map[string]morphlingRecord, hmacKey []byte) error {
	records := make([]morphlingRecord, 0, len(morphlingRecords))
	for _, morphlingRecord := range morphlingRecords {
		if err := morphlingRecord.Validate(); err != nil {
			return fmt.Errorf("validate morphling record: %w", err)
		}
		records = append(records, morphlingRecord)
	}
	sort.Slice(records, func(leftIndex int, rightIndex int) bool {
		return records[leftIndex].MorphlingID < records[rightIndex].MorphlingID
	})

	stateFile := morphlingStateFile{Morphlings: records}

	if len(hmacKey) > 0 {
		contentBytes, marshalErr := json.Marshal(stateFile)
		if marshalErr != nil {
			return fmt.Errorf("marshal morphling state for signing: %w", marshalErr)
		}
		mac := hmac.New(sha256.New, hmacKey)
		mac.Write(contentBytes)
		stateFile.Signature = hex.EncodeToString(mac.Sum(nil))
	}

	jsonBytes, err := json.MarshalIndent(stateFile, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal morphling records: %w", err)
	}
	if len(jsonBytes) == 0 || jsonBytes[len(jsonBytes)-1] != '\n' {
		jsonBytes = append(jsonBytes, '\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create morphling state dir: %w", err)
	}

	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, jsonBytes, 0o600); err != nil {
		return fmt.Errorf("write morphling temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("rename morphling temp file: %w", err)
	}
	if stateDir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = stateDir.Sync()
		_ = stateDir.Close()
	}
	return nil
}

func cloneMorphlingRecords(sourceRecords map[string]morphlingRecord) map[string]morphlingRecord {
	clonedRecords := make(map[string]morphlingRecord, len(sourceRecords))
	for recordID, record := range sourceRecords {
		clonedRecords[recordID] = cloneMorphlingRecord(record)
	}
	return clonedRecords
}

// morphlingTenantDenied enforces tenant isolation for morphlings created under a non-empty tenant.
// Empty TenantID keeps legacy behavior: ParentControlSessionID alone defines visibility.
func morphlingTenantDenied(record morphlingRecord, token capabilityToken) bool {
	recordTenantID := strings.TrimSpace(record.TenantID)
	if recordTenantID == "" {
		return false
	}
	return recordTenantID != strings.TrimSpace(token.TenantID)
}

// morphlingParentTenantInconsistent means on-disk morphling tenant does not match the parent session (tamper or partial upgrade).
func morphlingParentTenantInconsistent(recordTenantID string, parentSession controlSession) bool {
	normalizedRecordTenantID := strings.TrimSpace(recordTenantID)
	if normalizedRecordTenantID == "" {
		return false
	}
	return normalizedRecordTenantID != strings.TrimSpace(parentSession.TenantID)
}

func cloneMorphlingRecord(record morphlingRecord) morphlingRecord {
	record.InputRelativePaths = append([]string(nil), record.InputRelativePaths...)
	record.AllowedRelativePaths = append([]string(nil), record.AllowedRelativePaths...)
	record.RequestedCapabilities = append([]string(nil), record.RequestedCapabilities...)
	record.GrantedCapabilities = append([]string(nil), record.GrantedCapabilities...)
	record.MemoryStrings = append([]string(nil), record.MemoryStrings...)
	record.StagedArtifactRefs = append([]string(nil), record.StagedArtifactRefs...)
	return record
}

func morphlingGoalHint(goalText string) string {
	trimmedGoalText := strings.TrimSpace(goalText)
	if len(trimmedGoalText) <= 80 {
		return trimmedGoalText
	}
	return strings.TrimSpace(trimmedGoalText[:80])
}

func (server *Server) goalHMACForSession(controlSessionID string, goalText string) (string, error) {
	server.mu.Lock()
	_, found := server.sessions[controlSessionID]
	server.mu.Unlock()
	if !found {
		return "", fmt.Errorf("control session not found for goal hmac: %s", controlSessionID)
	}
	sessionMACKey := server.sessionMACKeyForControlSessionAtEpoch(controlSessionID, server.currentSessionMACEpochIndex())
	if strings.TrimSpace(sessionMACKey) == "" {
		return "", fmt.Errorf("session mac key not available for control session %s", controlSessionID)
	}
	mac := hmac.New(sha256.New, []byte(sessionMACKey))
	_, _ = mac.Write([]byte(strings.TrimSpace(goalText)))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// morphlingProjectionStatusText is operator-facing lifecycle text derived only from
// authoritative state (no worker/model status or termination prose).
func morphlingProjectionStatusText(record morphlingRecord) string {
	switch record.State {
	case morphlingStateRequested:
		return "requested"
	case morphlingStateAuthorizing:
		return "authorizing"
	case morphlingStatePendingSpawnApproval:
		return "awaiting spawn approval"
	case morphlingStateSpawned:
		return "spawned"
	case morphlingStateRunning:
		return "running"
	case morphlingStateCompleting:
		return "completing"
	case morphlingStatePendingReview:
		return "pending review"
	case morphlingStateTerminating:
		return "terminating"
	case morphlingStateTerminated:
		return "terminated"
	default:
		return record.State
	}
}

func morphlingStatusText(record morphlingRecord) string {
	switch record.State {
	case morphlingStateRequested:
		return "requested"
	case morphlingStateAuthorizing:
		return "authorizing"
	case morphlingStatePendingSpawnApproval:
		return "awaiting spawn approval"
	case morphlingStateSpawned:
		return "spawned"
	case morphlingStateRunning:
		return "running"
	case morphlingStateCompleting:
		return "completing"
	case morphlingStatePendingReview:
		return "pending review"
	case morphlingStateTerminating:
		if record.TerminationReason != "" {
			return "terminating; " + record.TerminationReason
		}
		return "terminating"
	case morphlingStateTerminated:
		if record.TerminationReason != "" {
			return "terminated; " + record.TerminationReason
		}
		return "terminated"
	default:
		return record.State
	}
}

func morphlingSummaryFromRecord(record morphlingRecord) MorphlingSummary {
	virtualSandboxPath := ""
	if strings.TrimSpace(record.WorkingDirRelativePath) != "" {
		virtualSandboxPath = sandbox.VirtualizeRelativeHomePath(record.WorkingDirRelativePath)
	}
	return MorphlingSummary{
		MorphlingID:           record.MorphlingID,
		TaskID:                record.TaskID,
		Class:                 record.Class,
		State:                 record.State,
		GoalHint:              "",
		StatusText:            morphlingProjectionStatusText(record),
		VirtualSandboxPath:    virtualSandboxPath,
		InputPaths:            virtualizeSandboxPaths(record.InputRelativePaths),
		AllowedPaths:          virtualizeSandboxPaths(record.AllowedRelativePaths),
		RequestedCapabilities: append([]string(nil), record.RequestedCapabilities...),
		GrantedCapabilities:   append([]string(nil), record.GrantedCapabilities...),
		MemoryStrings:         nil,
		MemoryStringCount:     len(record.MemoryStrings),
		ArtifactCount:         record.ArtifactCount,
		StagedArtifactRefs:    append([]string(nil), record.StagedArtifactRefs...),
		PendingReview:         record.State == morphlingStatePendingReview,
		RequiresReview:        record.RequiresReview,
		Outcome:               record.Outcome,
		TimeBudgetSeconds:     record.TimeBudgetSeconds,
		TokenBudget:           record.TokenBudget,
		ApprovalID:            record.ApprovalID,
		ApprovalDeadlineUTC:   record.ApprovalDeadlineUTC,
		ReviewDeadlineUTC:     record.ReviewDeadlineUTC,
		CreatedAtUTC:          record.CreatedAtUTC,
		SpawnedAtUTC:          record.SpawnedAtUTC,
		LastEventAtUTC:        record.LastEventAtUTC,
		TokenExpiryUTC:        record.TokenExpiryUTC,
		TerminatedAtUTC:       record.TerminatedAtUTC,
		TerminationReason:     "",
	}
}

func virtualizeSandboxPaths(relativePaths []string) []string {
	virtualPaths := make([]string, 0, len(relativePaths))
	for _, relativePath := range relativePaths {
		virtualPaths = append(virtualPaths, sandbox.VirtualizeRelativeHomePath(relativePath))
	}
	return virtualPaths
}

func activeMorphlingCountLocked(morphlingRecords map[string]morphlingRecord) int {
	activeCount := 0
	for _, morphlingRecord := range morphlingRecords {
		if morphlingStateConsumesCapacity(morphlingRecord.State) {
			activeCount++
		}
	}
	return activeCount
}

func activeMorphlingCountForClassLocked(morphlingRecords map[string]morphlingRecord, className string) int {
	activeCount := 0
	for _, morphlingRecord := range morphlingRecords {
		if morphlingRecord.Class == className && morphlingStateConsumesCapacity(morphlingRecord.State) {
			activeCount++
		}
	}
	return activeCount
}

func pendingReviewCountLocked(morphlingRecords map[string]morphlingRecord, controlSessionID string) int {
	pendingCount := 0
	for _, morphlingRecord := range morphlingRecords {
		if morphlingRecord.ParentControlSessionID == controlSessionID && morphlingRecord.State == morphlingStatePendingReview {
			pendingCount++
		}
	}
	return pendingCount
}

func (server *Server) activeMorphlingCount(now time.Time) int {
	server.morphlingsMu.Lock()
	defer server.morphlingsMu.Unlock()
	return activeMorphlingCountLocked(server.morphlings)
}
