package loopgate

import (
	"fmt"
	"strings"
)

const (
	UIEventTypeSessionInfo      = "session.info"
	UIEventTypeToolResult       = "tool.result"
	UIEventTypeToolDenied       = "tool.denied"
	UIEventTypeApprovalPending  = "approval.pending"
	UIEventTypeApprovalResolved = "approval.resolved"
	UIEventTypeWarning          = "warning"
)

type UIStatusPolicySummary struct {
	ReadEnabled           bool `json:"read_enabled"`
	WriteEnabled          bool `json:"write_enabled"`
	WriteRequiresApproval bool `json:"write_requires_approval"`
}

const (
	OperatorMountWriteGrantActionRevoke = "revoke"
	OperatorMountWriteGrantActionRenew  = "renew"
)

type UIOperatorMountWriteGrant struct {
	RootPath     string `json:"root_path"`
	ExpiresAtUTC string `json:"expires_at_utc,omitempty"`
}

type UIOperatorMountWriteGrantStatusResponse struct {
	Grants []UIOperatorMountWriteGrant `json:"grants,omitempty"`
}

type UIOperatorMountWriteGrantUpdateRequest struct {
	RootPath string `json:"root_path"`
	Action   string `json:"action"`
}

func (uiOperatorMountWriteGrantUpdateRequest UIOperatorMountWriteGrantUpdateRequest) Validate() error {
	if strings.TrimSpace(uiOperatorMountWriteGrantUpdateRequest.RootPath) == "" {
		return fmt.Errorf("root_path must not be blank")
	}
	switch strings.TrimSpace(uiOperatorMountWriteGrantUpdateRequest.Action) {
	case OperatorMountWriteGrantActionRevoke, OperatorMountWriteGrantActionRenew:
		return nil
	default:
		return fmt.Errorf("action must be one of %q or %q", OperatorMountWriteGrantActionRevoke, OperatorMountWriteGrantActionRenew)
	}
}

type UIStatusResponse struct {
	Version                  string                      `json:"version"`
	PersonaName              string                      `json:"persona_name"`
	PersonaVersion           string                      `json:"persona_version"`
	ControlSessionID         string                      `json:"control_session_id"`
	ActorLabel               string                      `json:"actor_label"`
	ClientSessionLabel       string                      `json:"client_session_label"`
	RuntimeSessionID         string                      `json:"runtime_session_id,omitempty"`
	TurnCount                int                         `json:"turn_count"`
	DistillCursorLine        int                         `json:"distill_cursor_line"`
	PendingApprovals         int                         `json:"pending_approvals"`
	CapabilityCount          int                         `json:"capability_count"`
	ConnectionCount          int                         `json:"connection_count"`
	OperatorMountWriteGrants []UIOperatorMountWriteGrant `json:"operator_mount_write_grants,omitempty"`
	Policy                   UIStatusPolicySummary       `json:"policy"`
}

type UIApprovalSummary struct {
	ApprovalRequestID string `json:"approval_request_id"`
	Capability        string `json:"capability"`
	Path              string `json:"path,omitempty"`
	ContentBytes      int    `json:"content_bytes,omitempty"`
	Preview           string `json:"preview,omitempty"`
	Redacted          bool   `json:"redacted"`
	Reason            string `json:"reason,omitempty"`
	ExpiresAtUTC      string `json:"expires_at_utc"`
	// Operator-facing host plan context (host.plan.apply); safe strings only, no raw plan_json.
	OperatorIntentLine    string `json:"operator_intent_line,omitempty"`
	PlanSummary           string `json:"plan_summary,omitempty"`
	HostFolderDisplayName string `json:"host_folder_display_name,omitempty"`
	PlanOperationCount    int    `json:"plan_operation_count,omitempty"`
	PlanMkdirCount        int    `json:"plan_mkdir_count,omitempty"`
	PlanMoveCount         int    `json:"plan_move_count,omitempty"`
}

type UIApprovalsResponse struct {
	Approvals []UIApprovalSummary `json:"approvals"`
}

type SharedFolderStatusResponse struct {
	Name                string `json:"name"`
	HostPath            string `json:"host_path"`
	SandboxRelativePath string `json:"sandbox_relative_path"`
	SandboxAbsolutePath string `json:"sandbox_absolute_path"`
	HostExists          bool   `json:"host_exists"`
	MirrorReady         bool   `json:"mirror_ready"`
	EntryCount          int    `json:"entry_count"`
}

type FolderAccessStatus struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Description         string `json:"description"`
	Warning             string `json:"warning,omitempty"`
	Recommended         bool   `json:"recommended"`
	AlwaysGranted       bool   `json:"always_granted"`
	Granted             bool   `json:"granted"`
	HostPath            string `json:"host_path"`
	SandboxRelativePath string `json:"sandbox_relative_path"`
	SandboxAbsolutePath string `json:"sandbox_absolute_path"`
	HostExists          bool   `json:"host_exists"`
	MirrorReady         bool   `json:"mirror_ready"`
	EntryCount          int    `json:"entry_count"`
	// HostAccessOnly is true for folders accessed via host.folder.* capabilities
	// directly on the real filesystem — no sandbox mirror is maintained.
	HostAccessOnly bool `json:"host_access_only,omitempty"`
}

type FolderAccessStatusResponse struct {
	Folders []FolderAccessStatus `json:"folders"`
}

type FolderAccessSyncResponse struct {
	Folders    []FolderAccessStatus `json:"folders"`
	ChangedIDs []string             `json:"changed_ids,omitempty"`
}

type FolderAccessUpdateRequest struct {
	GrantedIDs []string `json:"granted_ids"`
}

func (folderAccessUpdateRequest FolderAccessUpdateRequest) Validate() error {
	for _, rawGrantedID := range folderAccessUpdateRequest.GrantedIDs {
		if strings.TrimSpace(rawGrantedID) == "" {
			return fmt.Errorf("granted_ids must not contain blank values")
		}
	}
	return nil
}

type UIApprovalDecisionRequest struct {
	Approved *bool `json:"approved"`
}

func (uiApprovalDecisionRequest UIApprovalDecisionRequest) Validate() error {
	if uiApprovalDecisionRequest.Approved == nil {
		return fmt.Errorf("approved field is required")
	}
	return nil
}

type UIEventEnvelope struct {
	ControlSessionID string      `json:"-"`
	ID               string      `json:"id"`
	Type             string      `json:"type"`
	TS               string      `json:"ts"`
	Data             interface{} `json:"data"`
}

type UIRecentEventsResponse struct {
	Events []UIEventEnvelope `json:"events"`
}

type UIEventSessionInfo struct {
	ControlSessionID   string `json:"control_session_id"`
	ActorLabel         string `json:"actor_label"`
	ClientSessionLabel string `json:"client_session_label"`
	PersonaName        string `json:"persona_name"`
	PersonaVersion     string `json:"persona_version"`
}

type UIEventToolResult struct {
	RequestID        string `json:"request_id"`
	Capability       string `json:"capability"`
	Path             string `json:"path,omitempty"`
	Bytes            int    `json:"bytes,omitempty"`
	EntryCount       int    `json:"entry_count,omitempty"`
	Message          string `json:"message,omitempty"`
	Content          string `json:"content,omitempty"`
	DisplayOnly      bool   `json:"display_only"`
	PromptEligible   bool   `json:"prompt_eligible"`
	MemoryEligible   bool   `json:"memory_eligible"`
	Quarantined      bool   `json:"quarantined"`
	QuarantineNotice string `json:"quarantine_notice,omitempty"`
}

type UIEventToolDenied struct {
	RequestID    string `json:"request_id"`
	Capability   string `json:"capability"`
	DenialCode   string `json:"denial_code"`
	DenialReason string `json:"denial_reason"`
}

type UIEventApprovalPending struct {
	ApprovalRequestID string `json:"approval_request_id"`
	Capability        string `json:"capability"`
	Path              string `json:"path,omitempty"`
	ContentBytes      int    `json:"content_bytes,omitempty"`
	Preview           string `json:"preview,omitempty"`
	Redacted          bool   `json:"redacted"`
	Reason            string `json:"reason,omitempty"`
	ExpiresAtUTC      string `json:"expires_at_utc"`
}

type UIEventApprovalResolved struct {
	ApprovalRequestID string `json:"approval_request_id"`
	Capability        string `json:"capability"`
	Decision          string `json:"decision"`
	Status            string `json:"status"`
}

type UIEventWarning struct {
	Message string `json:"message"`
}

func validateUIEventEnvelope(uiEventEnvelope UIEventEnvelope) error {
	if strings.TrimSpace(uiEventEnvelope.ID) == "" {
		return fmt.Errorf("ui event id is required")
	}
	if strings.TrimSpace(uiEventEnvelope.Type) == "" {
		return fmt.Errorf("ui event type is required")
	}
	if uiEventEnvelope.Data == nil {
		return fmt.Errorf("ui event data is required")
	}
	return nil
}
