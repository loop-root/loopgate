package loopgate

import (
	"fmt"
	"strings"

	"morph/internal/identifiers"
	"morph/internal/sandbox"
)

type MorphlingSpawnRequest struct {
	RequestID                  string           `json:"request_id,omitempty"`
	Class                      string           `json:"class"`
	Goal                       string           `json:"goal"`
	Inputs                     []MorphlingInput `json:"inputs,omitempty"`
	OutputTag                  string           `json:"output_tag,omitempty"`
	RequestedCapabilities      []string         `json:"requested_capabilities,omitempty"`
	RequestedTimeBudgetSeconds int              `json:"requested_time_budget_seconds,omitempty"`
	RequestedTokenBudget       int              `json:"requested_token_budget,omitempty"`
	ParentSessionID            string           `json:"parent_session_id,omitempty"`
}

type MorphlingStatusRequest struct {
	MorphlingID       string `json:"morphling_id,omitempty"`
	IncludeTerminated bool   `json:"include_terminated,omitempty"`
}

type MorphlingTerminateRequest struct {
	MorphlingID string `json:"morphling_id"`
	Reason      string `json:"reason,omitempty"`
}

type MorphlingWorkerLaunchRequest struct {
	MorphlingID string `json:"morphling_id"`
}

type MorphlingWorkerLaunchResponse struct {
	MorphlingID  string `json:"morphling_id"`
	LaunchToken  string `json:"launch_token"`
	ExpiresAtUTC string `json:"expires_at_utc"`
}

type MorphlingWorkerOpenRequest struct {
	LaunchToken string `json:"launch_token"`
}

type MorphlingWorkerSessionResponse struct {
	MorphlingID      string `json:"morphling_id"`
	ControlSessionID string `json:"control_session_id"`
	WorkerToken      string `json:"worker_token"`
	SessionMACKey    string `json:"session_mac_key"`
	ExpiresAtUTC     string `json:"expires_at_utc"`
}

type MorphlingWorkerStartRequest struct {
	StatusText    string   `json:"status_text,omitempty"`
	MemoryStrings []string `json:"memory_strings,omitempty"`
}

type MorphlingWorkerUpdateRequest struct {
	StatusText    string   `json:"status_text,omitempty"`
	MemoryStrings []string `json:"memory_strings,omitempty"`
}

type MorphlingWorkerCompleteRequest struct {
	ExitReason    string   `json:"exit_reason,omitempty"`
	StatusText    string   `json:"status_text,omitempty"`
	MemoryStrings []string `json:"memory_strings,omitempty"`
	ArtifactPaths []string `json:"artifact_paths,omitempty"`
}

type MorphlingWorkerActionResponse struct {
	Status    string           `json:"status"`
	Morphling MorphlingSummary `json:"morphling"`
}

type MorphlingReviewRequest struct {
	MorphlingID string `json:"morphling_id"`
	Approved    bool   `json:"approved"`
}

type MorphlingReviewResponse struct {
	Status        string           `json:"status"`
	DecisionNonce string           `json:"decision_nonce,omitempty"`
	Morphling     MorphlingSummary `json:"morphling"`
}

type MorphlingSummary struct {
	MorphlingID           string   `json:"morphling_id"`
	TaskID                string   `json:"task_id,omitempty"`
	Class                 string   `json:"class"`
	State                 string   `json:"state"`
	GoalHint              string   `json:"goal_hint,omitempty"`
	StatusText            string   `json:"status_text,omitempty"`
	VirtualSandboxPath    string   `json:"virtual_sandbox_path,omitempty"`
	InputPaths            []string `json:"input_paths,omitempty"`
	AllowedPaths          []string `json:"allowed_paths,omitempty"`
	RequestedCapabilities []string `json:"requested_capabilities,omitempty"`
	GrantedCapabilities   []string `json:"granted_capabilities,omitempty"`
	MemoryStrings         []string `json:"memory_strings,omitempty"`
	MemoryStringCount     int      `json:"memory_string_count,omitempty"`
	ArtifactCount         int      `json:"artifact_count,omitempty"`
	StagedArtifactRefs    []string `json:"staged_artifact_refs,omitempty"`
	PendingReview         bool     `json:"pending_review"`
	RequiresReview        bool     `json:"requires_review"`
	Outcome               string   `json:"outcome,omitempty"`
	TimeBudgetSeconds     int      `json:"time_budget_seconds,omitempty"`
	TokenBudget           int      `json:"token_budget,omitempty"`
	ApprovalID            string   `json:"approval_id,omitempty"`
	ApprovalDeadlineUTC   string   `json:"approval_deadline_utc,omitempty"`
	ReviewDeadlineUTC     string   `json:"review_deadline_utc,omitempty"`
	CreatedAtUTC          string   `json:"created_at_utc"`
	SpawnedAtUTC          string   `json:"spawned_at_utc,omitempty"`
	LastEventAtUTC        string   `json:"last_event_at_utc,omitempty"`
	TokenExpiryUTC        string   `json:"token_expiry_utc,omitempty"`
	TerminatedAtUTC       string   `json:"terminated_at_utc,omitempty"`
	TerminationReason     string   `json:"termination_reason,omitempty"`
}

type MorphlingSpawnResponse struct {
	RequestID           string `json:"request_id,omitempty"`
	Status              string `json:"status"`
	DenialReason        string `json:"denial_reason,omitempty"`
	DenialCode          string `json:"denial_code,omitempty"`
	MorphlingID         string `json:"morphling_id,omitempty"`
	TaskID              string `json:"task_id,omitempty"`
	State               string `json:"state,omitempty"`
	Class               string `json:"class,omitempty"`
	ApprovalID          string `json:"approval_id,omitempty"`
	ApprovalDeadlineUTC string `json:"approval_deadline_utc,omitempty"`
	// ApprovalManifestSHA256 and ApprovalDecisionNonce are set when Status is pending_approval
	// so DecideApproval can bind to the same manifest as capability.execute approvals (AMP RFC 0005 §6).
	ApprovalManifestSHA256 string   `json:"approval_manifest_sha256,omitempty"`
	ApprovalDecisionNonce  string   `json:"approval_decision_nonce,omitempty"`
	GrantedCapabilities    []string `json:"granted_capabilities,omitempty"`
	VirtualSandboxPath     string   `json:"virtual_sandbox_path,omitempty"`
	SpawnedAtUTC           string   `json:"spawned_at_utc,omitempty"`
	TokenExpiryUTC         string   `json:"token_expiry_utc,omitempty"`
}

type MorphlingStatusResponse struct {
	SpawnEnabled       bool               `json:"spawn_enabled"`
	MaxActive          int                `json:"max_active"`
	ActiveCount        int                `json:"active_count"`
	PendingReviewCount int                `json:"pending_review_count"`
	Morphlings         []MorphlingSummary `json:"morphlings"`
}

type MorphlingTerminateResponse struct {
	Status    string           `json:"status"`
	Morphling MorphlingSummary `json:"morphling"`
}

func (morphlingSpawnRequest MorphlingSpawnRequest) Validate() error {
	if strings.TrimSpace(morphlingSpawnRequest.RequestID) != "" {
		if err := identifiers.ValidateSafeIdentifier("request_id", strings.TrimSpace(morphlingSpawnRequest.RequestID)); err != nil {
			return err
		}
	}
	if err := identifiers.ValidateSafeIdentifier("morphling class", strings.TrimSpace(morphlingSpawnRequest.Class)); err != nil {
		return err
	}
	if strings.TrimSpace(morphlingSpawnRequest.Goal) == "" {
		return fmt.Errorf("goal is required")
	}
	if len(strings.TrimSpace(morphlingSpawnRequest.Goal)) > 500 {
		return fmt.Errorf("goal exceeds maximum length")
	}
	if strings.TrimSpace(morphlingSpawnRequest.OutputTag) != "" {
		if err := identifiers.ValidateSafeIdentifier("output_tag", strings.TrimSpace(morphlingSpawnRequest.OutputTag)); err != nil {
			return err
		}
	}
	if len(morphlingSpawnRequest.RequestedCapabilities) == 0 {
		return fmt.Errorf("requested_capabilities must include at least one capability")
	}
	seenCapabilities := make(map[string]struct{}, len(morphlingSpawnRequest.RequestedCapabilities))
	for _, rawCapabilityName := range morphlingSpawnRequest.RequestedCapabilities {
		capabilityName := strings.TrimSpace(rawCapabilityName)
		if err := identifiers.ValidateSafeIdentifier("requested capability", capabilityName); err != nil {
			return err
		}
		if _, exists := seenCapabilities[capabilityName]; exists {
			return fmt.Errorf("requested_capabilities contains duplicate capability %q", capabilityName)
		}
		seenCapabilities[capabilityName] = struct{}{}
	}
	if morphlingSpawnRequest.RequestedTimeBudgetSeconds < 0 {
		return fmt.Errorf("requested_time_budget_seconds must be non-negative")
	}
	if morphlingSpawnRequest.RequestedTokenBudget < 0 {
		return fmt.Errorf("requested_token_budget must be non-negative")
	}
	if strings.TrimSpace(morphlingSpawnRequest.ParentSessionID) != "" {
		if err := identifiers.ValidateSafeIdentifier("parent_session_id", strings.TrimSpace(morphlingSpawnRequest.ParentSessionID)); err != nil {
			return err
		}
	}
	for _, inputSpec := range morphlingSpawnRequest.Inputs {
		if _, err := sandbox.NormalizeHomePath(inputSpec.SandboxPath); err != nil {
			return fmt.Errorf("inputs contains invalid sandbox path: %w", err)
		}
		if strings.TrimSpace(inputSpec.Role) != "" {
			if err := identifiers.ValidateSafeIdentifier("input role", strings.TrimSpace(inputSpec.Role)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (morphlingStatusRequest MorphlingStatusRequest) Validate() error {
	if strings.TrimSpace(morphlingStatusRequest.MorphlingID) == "" {
		return nil
	}
	return identifiers.ValidateSafeIdentifier("morphling_id", strings.TrimSpace(morphlingStatusRequest.MorphlingID))
}

func (morphlingTerminateRequest MorphlingTerminateRequest) Validate() error {
	if err := identifiers.ValidateSafeIdentifier("morphling_id", strings.TrimSpace(morphlingTerminateRequest.MorphlingID)); err != nil {
		return err
	}
	if len(strings.TrimSpace(morphlingTerminateRequest.Reason)) > 200 {
		return fmt.Errorf("reason exceeds maximum length")
	}
	return nil
}

func (morphlingWorkerLaunchRequest MorphlingWorkerLaunchRequest) Validate() error {
	return identifiers.ValidateSafeIdentifier("morphling_id", strings.TrimSpace(morphlingWorkerLaunchRequest.MorphlingID))
}

func (morphlingWorkerOpenRequest MorphlingWorkerOpenRequest) Validate() error {
	if strings.TrimSpace(morphlingWorkerOpenRequest.LaunchToken) == "" {
		return fmt.Errorf("launch_token is required")
	}
	return nil
}

func validateMorphlingWorkerUpdateFields(statusText string, memoryStrings []string) error {
	if len(strings.TrimSpace(statusText)) > 200 {
		return fmt.Errorf("status_text exceeds maximum length")
	}
	if len(memoryStrings) > 8 {
		return fmt.Errorf("memory_strings exceeds maximum entry count")
	}
	for _, memoryString := range memoryStrings {
		if strings.TrimSpace(memoryString) == "" {
			return fmt.Errorf("memory_strings entries must be non-empty")
		}
		if len(strings.TrimSpace(memoryString)) > 200 {
			return fmt.Errorf("memory_strings entries exceed maximum length")
		}
	}
	return nil
}

func (morphlingWorkerStartRequest MorphlingWorkerStartRequest) Validate() error {
	return validateMorphlingWorkerUpdateFields(morphlingWorkerStartRequest.StatusText, morphlingWorkerStartRequest.MemoryStrings)
}

func (morphlingWorkerUpdateRequest MorphlingWorkerUpdateRequest) Validate() error {
	return validateMorphlingWorkerUpdateFields(morphlingWorkerUpdateRequest.StatusText, morphlingWorkerUpdateRequest.MemoryStrings)
}

func (morphlingWorkerCompleteRequest MorphlingWorkerCompleteRequest) Validate() error {
	if err := validateMorphlingWorkerUpdateFields(morphlingWorkerCompleteRequest.StatusText, morphlingWorkerCompleteRequest.MemoryStrings); err != nil {
		return err
	}
	if len(strings.TrimSpace(morphlingWorkerCompleteRequest.ExitReason)) > 200 {
		return fmt.Errorf("exit_reason exceeds maximum length")
	}
	seenArtifactPaths := make(map[string]struct{}, len(morphlingWorkerCompleteRequest.ArtifactPaths))
	for _, artifactPath := range morphlingWorkerCompleteRequest.ArtifactPaths {
		normalizedArtifactPath, err := sandbox.NormalizeHomePath(artifactPath)
		if err != nil {
			return fmt.Errorf("artifact_paths contains invalid sandbox path: %w", err)
		}
		if _, exists := seenArtifactPaths[normalizedArtifactPath]; exists {
			return fmt.Errorf("artifact_paths contains duplicate sandbox path %q", normalizedArtifactPath)
		}
		seenArtifactPaths[normalizedArtifactPath] = struct{}{}
	}
	return nil
}

func (morphlingReviewRequest MorphlingReviewRequest) Validate() error {
	return identifiers.ValidateSafeIdentifier("morphling_id", strings.TrimSpace(morphlingReviewRequest.MorphlingID))
}
