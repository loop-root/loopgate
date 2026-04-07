package loopgate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"morph/internal/config"
	"morph/internal/identifiers"
	modelpkg "morph/internal/model"
	modelruntime "morph/internal/modelruntime"
	"morph/internal/sandbox"
	tclpkg "morph/internal/tcl"
)

const (
	ResponseStatusSuccess         = "success"
	ResponseStatusDenied          = "denied"
	ResponseStatusError           = "error"
	ResponseStatusPendingApproval = "pending_approval"
)

const (
	GrantTypePublicRead        = "public_read"
	GrantTypePKCE              = "pkce"
	GrantTypeAuthorizationCode = "authorization_code"
	GrantTypeClientCredentials = "client_credentials"
)

const (
	DenialCodeCapabilityTokenMissing            = "capability_token_missing"
	DenialCodeCapabilityTokenInvalid            = "capability_token_invalid"
	DenialCodeCapabilityTokenExpired            = "capability_token_expired"
	DenialCodeCapabilityTokenScopeDenied        = "capability_token_scope_denied"
	DenialCodeCapabilityTokenReused             = "capability_token_reused"
	DenialCodeCapabilityTokenBindingInvalid     = "capability_token_binding_invalid"
	DenialCodeRequestSignatureMissing           = "request_signature_missing"
	DenialCodeRequestSignatureInvalid           = "request_signature_invalid"
	DenialCodeRequestTimestampInvalid           = "request_timestamp_invalid"
	DenialCodeRequestNonceReplayDetected        = "request_nonce_replay_detected"
	DenialCodeControlSessionBindingInvalid      = "control_session_binding_invalid"
	DenialCodeApprovalTokenMissing              = "approval_token_missing"
	DenialCodeApprovalTokenInvalid              = "approval_token_invalid"
	DenialCodeApprovalTokenExpired              = "approval_token_expired"
	DenialCodeApprovalDecisionNonceMissing      = "approval_decision_nonce_missing"
	DenialCodeApprovalDecisionNonceInvalid      = "approval_decision_nonce_invalid"
	DenialCodeApprovalNotFound                  = "approval_request_not_found"
	DenialCodeApprovalOwnerMismatch             = "approval_owner_mismatch"
	DenialCodeApprovalRequired                  = "approval_required"
	DenialCodeApprovalDenied                    = "approval_denied"
	DenialCodeApprovalStateInvalid              = "approval_state_invalid"
	DenialCodeSecretExportProhibited            = "secret_export_prohibited"
	DenialCodeCapabilityScopeRequired           = "capability_scope_required"
	DenialCodeRequestReplayDetected             = "request_replay_detected"
	DenialCodeUnknownCapability                 = "unknown_capability"
	DenialCodeInvalidCapabilityArguments        = "invalid_capability_arguments"
	DenialCodePolicyDenied                      = "policy_denied"
	DenialCodeApprovalCreationFailed            = "approval_creation_failed"
	DenialCodeExecutionFailed                   = "capability_execution_failed"
	DenialCodeMalformedRequest                  = "malformed_request"
	DenialCodeAuditUnavailable                  = "audit_unavailable"
	DenialCodeSourceBytesUnavailable            = "source_bytes_unavailable"
	DenialCodeQuarantinePruneNotEligible        = "quarantine_prune_not_eligible"
	DenialCodeSiteURLInvalid                    = "site_url_invalid"
	DenialCodeHTTPSRequired                     = "https_required"
	DenialCodeSiteTrustDraftNotAvailable        = "site_trust_draft_not_available"
	DenialCodeSiteTrustDraftExists              = "site_trust_draft_exists"
	DenialCodeSiteInspectionUnsupportedType     = "site_inspection_unsupported_content_type"
	DenialCodeSandboxPathInvalid                = "sandbox_path_invalid"
	DenialCodeSandboxSourceUnavailable          = "sandbox_source_unavailable"
	DenialCodeSandboxDestinationExists          = "sandbox_destination_exists"
	DenialCodeSandboxHostDestinationInvalid     = "sandbox_host_destination_invalid"
	DenialCodeSandboxSymlinkNotAllowed          = "sandbox_symlink_not_allowed"
	DenialCodeSandboxArtifactNotStaged          = "sandbox_artifact_not_staged"
	DenialCodeMorphlingSpawnDisabled            = "morphling_spawn_disabled"
	DenialCodeMorphlingClassInvalid             = "morphling_class_invalid"
	DenialCodeMorphlingInputInvalid             = "morphling_input_invalid"
	DenialCodeMorphlingArtifactInvalid          = "morphling_artifact_invalid"
	DenialCodeMorphlingActiveLimitReached       = "morphling_active_limit_reached"
	DenialCodeMorphlingNotFound                 = "morphling_not_found"
	DenialCodeMorphlingStateInvalid             = "morphling_state_invalid"
	DenialCodeMorphlingReviewInvalid            = "morphling_review_invalid"
	DenialCodeMorphlingWorkerLaunchInvalid      = "morphling_worker_launch_invalid"
	DenialCodeMorphlingWorkerTokenMissing       = "morphling_worker_token_missing"
	DenialCodeMorphlingWorkerTokenInvalid       = "morphling_worker_token_invalid"
	DenialCodeContinuityLineageIneligible       = "continuity_lineage_ineligible"
	DenialCodeContinuityGovernanceStateConflict = "continuity_governance_state_conflict"
	DenialCodeContinuityInspectionNotFound      = "continuity_inspection_not_found"
	DenialCodeContinuityRetentionWindowActive   = "continuity_retention_window_active"
	DenialCodeMemoryFactWriteRateLimited        = "memory_fact_write_rate_limited"
	DenialCodeMemoryCandidateDangerous          = "memory_candidate_dangerous"
	DenialCodeMemoryCandidateInvalid            = "memory_candidate_invalid"
	DenialCodeMemoryCandidateDropped            = "memory_candidate_dropped"
	DenialCodeMemoryCandidateQuarantineRequired = "memory_candidate_quarantine_required"
	DenialCodeMemoryCandidateReviewRequired     = "memory_candidate_review_required"
	DenialCodeTodoItemNotFound                  = "todo_item_not_found"
	DenialCodeSessionOpenRateLimited            = "session_open_rate_limited"
	DenialCodeSessionActiveLimitReached         = "session_active_limit_reached"
	// DenialCodeApprovalManifestMismatch is returned when the submitted approval manifest SHA256
	// does not match the server-computed manifest for the pending approval. This prevents an
	// operator decision from being bound to a different action than the one that was displayed.
	DenialCodeApprovalManifestMismatch = "approval_manifest_mismatch"
	// DenialCodeApprovalExecutionBodyMismatch is returned when the stored CapabilityRequest no
	// longer matches ExecutionBodySHA256 recorded at approval creation (memory corruption,
	// bug, or unexpected mutation of the pending record).
	DenialCodeApprovalExecutionBodyMismatch = "approval_execution_body_mismatch"
	// DenialCodeApprovalStateConflict is returned when an approval decision is submitted but
	// the approval is already in a terminal or non-pending state due to a concurrent consume or
	// revoke operation. This is distinct from DenialCodeApprovalStateInvalid, which covers
	// general state violations such as a decision on an expired or cancelled approval.
	DenialCodeApprovalStateConflict = "approval_state_conflict"

	// Task plan denial codes.
	DenialCodeTaskPlanInvalid            = "task_plan_invalid"
	DenialCodeTaskPlanHashMismatch       = "task_plan_hash_mismatch"
	DenialCodeTaskPlanNotFound           = "task_plan_not_found"
	DenialCodeTaskPlanStateInvalid       = "task_plan_state_invalid"
	DenialCodeTaskLeaseNotFound          = "task_lease_not_found"
	DenialCodeTaskLeaseConsumed          = "task_lease_consumed"
	DenialCodeTaskLeaseExpired           = "task_lease_expired"
	DenialCodeTaskLeaseMorphlingMismatch = "task_lease_morphling_mismatch"
	DenialCodeProcessBindingRejected     = "process_binding_rejected"
	DenialCodeFsReadSizeLimitExceeded    = "fs_read_size_limit_exceeded"

	// Hook denial codes.
	DenialCodeHookPeerBindingRejected = "hook_peer_binding_rejected"
	DenialCodeHookUnknownTool         = "hook_unknown_tool"
)

type ControlPlaneClient interface {
	Status(ctx context.Context) (StatusResponse, error)
	ConfigureSession(actor string, sessionID string, requestedCapabilities []string)
	ModelReply(ctx context.Context, request modelpkg.Request) (modelpkg.Response, error)
	ValidateModelConfig(ctx context.Context, runtimeConfig modelruntime.Config) (modelruntime.Config, error)
	StoreModelConnection(ctx context.Context, request ModelConnectionStoreRequest) (ModelConnectionStatus, error)
	ConnectionsStatus(ctx context.Context) ([]ConnectionStatus, error)
	ValidateConnection(ctx context.Context, provider string, subject string) (ConnectionStatus, error)
	StartPKCEConnection(ctx context.Context, request PKCEStartRequest) (PKCEStartResponse, error)
	CompletePKCEConnection(ctx context.Context, request PKCECompleteRequest) (ConnectionStatus, error)
	InspectSite(ctx context.Context, request SiteInspectionRequest) (SiteInspectionResponse, error)
	CreateTrustDraft(ctx context.Context, request SiteTrustDraftRequest) (SiteTrustDraftResponse, error)
	SandboxImport(ctx context.Context, request SandboxImportRequest) (SandboxOperationResponse, error)
	SandboxStage(ctx context.Context, request SandboxStageRequest) (SandboxOperationResponse, error)
	SandboxMetadata(ctx context.Context, request SandboxMetadataRequest) (SandboxArtifactMetadataResponse, error)
	SandboxExport(ctx context.Context, request SandboxExportRequest) (SandboxOperationResponse, error)
	SandboxList(ctx context.Context, request SandboxListRequest) (SandboxListResponse, error)
	InspectContinuityThread(ctx context.Context, request ContinuityInspectRequest) (ContinuityInspectResponse, error)
	LoadMemoryWakeState(ctx context.Context) (MemoryWakeStateResponse, error)
	LoadMemoryDiagnosticWake(ctx context.Context) (MemoryDiagnosticWakeResponse, error)
	LoadHavenMemoryInventory(ctx context.Context) (HavenMemoryInventoryResponse, error)
	ResetHavenMemory(ctx context.Context, request HavenMemoryResetRequest) (HavenMemoryResetResponse, error)
	DiscoverMemory(ctx context.Context, request MemoryDiscoverRequest) (MemoryDiscoverResponse, error)
	RecallMemory(ctx context.Context, request MemoryRecallRequest) (MemoryRecallResponse, error)
	RememberMemoryFact(ctx context.Context, request MemoryRememberRequest) (MemoryRememberResponse, error)
	SpawnMorphling(ctx context.Context, request MorphlingSpawnRequest) (MorphlingSpawnResponse, error)
	MorphlingStatus(ctx context.Context, request MorphlingStatusRequest) (MorphlingStatusResponse, error)
	TerminateMorphling(ctx context.Context, request MorphlingTerminateRequest) (MorphlingTerminateResponse, error)
	LaunchMorphlingWorker(ctx context.Context, request MorphlingWorkerLaunchRequest) (MorphlingWorkerLaunchResponse, error)
	ReviewMorphling(ctx context.Context, request MorphlingReviewRequest) (MorphlingReviewResponse, error)
	QuarantineMetadata(ctx context.Context, quarantineRef string) (QuarantineMetadataResponse, error)
	ViewQuarantinedPayload(ctx context.Context, quarantineRef string) (QuarantineViewResponse, error)
	PruneQuarantinedPayload(ctx context.Context, quarantineRef string) (QuarantineMetadataResponse, error)
	ExecuteCapability(ctx context.Context, capabilityRequest CapabilityRequest) (CapabilityResponse, error)
	DecideApproval(ctx context.Context, approvalRequestID string, approved bool) (CapabilityResponse, error)
	UIStatus(ctx context.Context) (UIStatusResponse, error)
	UIApprovals(ctx context.Context) (UIApprovalsResponse, error)
	UIDecideApproval(ctx context.Context, approvalRequestID string, approved bool) (CapabilityResponse, error)
	SharedFolderStatus(ctx context.Context) (SharedFolderStatusResponse, error)
	SyncSharedFolder(ctx context.Context) (SharedFolderStatusResponse, error)
	FolderAccessStatus(ctx context.Context) (FolderAccessStatusResponse, error)
	SyncFolderAccess(ctx context.Context) (FolderAccessSyncResponse, error)
	UpdateFolderAccess(ctx context.Context, request FolderAccessUpdateRequest) (FolderAccessStatusResponse, error)
	TaskStandingGrantStatus(ctx context.Context) (TaskStandingGrantStatusResponse, error)
	UpdateTaskStandingGrant(ctx context.Context, request TaskStandingGrantUpdateRequest) (TaskStandingGrantStatusResponse, error)
	HavenAgentWorkItemEnsure(ctx context.Context, request HavenAgentWorkEnsureRequest) (HavenAgentWorkItemResponse, error)
	HavenAgentWorkItemComplete(ctx context.Context, itemID string, reason string) (HavenAgentWorkItemResponse, error)
}

type ModelValidateRequest struct {
	RuntimeConfig modelruntime.Config `json:"runtime_config"`
}

type ModelValidateResponse struct {
	RuntimeConfig modelruntime.Config `json:"runtime_config"`
}

type ModelConnectionStoreRequest struct {
	ConnectionID string `json:"connection_id"`
	ProviderName string `json:"provider_name"`
	BaseURL      string `json:"base_url"`
	SecretValue  string `json:"secret_value"`
}

// MarshalJSON omits raw secret material so accidental json.Marshal of this request
// (logging, debug echoes) cannot leak credentials — mirrors CapabilityRequest.
func (r ModelConnectionStoreRequest) MarshalJSON() ([]byte, error) {
	type modelConnectionStoreWire struct {
		ConnectionID string `json:"connection_id"`
		ProviderName string `json:"provider_name"`
		BaseURL      string `json:"base_url"`
	}
	return json.Marshal(modelConnectionStoreWire{
		ConnectionID: r.ConnectionID,
		ProviderName: r.ProviderName,
		BaseURL:      r.BaseURL,
	})
}

type ModelConnectionStatus struct {
	ConnectionID       string `json:"connection_id"`
	ProviderName       string `json:"provider_name"`
	BaseURL            string `json:"base_url"`
	Status             string `json:"status"`
	SecureStoreRefID   string `json:"secure_store_ref_id"`
	LastValidatedAtUTC string `json:"last_validated_at_utc,omitempty"`
	LastUsedAtUTC      string `json:"last_used_at_utc,omitempty"`
	LastRotatedAtUTC   string `json:"last_rotated_at_utc,omitempty"`
}

type CapabilitySummary struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Operation   string `json:"operation"`
	Description string `json:"description"`
}

type ConnectionStatus struct {
	Provider           string   `json:"provider"`
	GrantType          string   `json:"grant_type"`
	Subject            string   `json:"subject"`
	Scopes             []string `json:"scopes"`
	Status             string   `json:"status"`
	SecureStoreRefID   string   `json:"secure_store_ref_id"`
	LastValidatedAtUTC string   `json:"last_validated_at_utc,omitempty"`
	LastUsedAtUTC      string   `json:"last_used_at_utc,omitempty"`
	LastRotatedAtUTC   string   `json:"last_rotated_at_utc,omitempty"`
}

// HealthResponse is the unauthenticated liveness payload for operators and launch scripts.
// It intentionally omits policy, capabilities, and connection metadata.
type HealthResponse struct {
	Version string `json:"version"`
	OK      bool   `json:"ok"`
}

type StatusResponse struct {
	Version          string              `json:"version"`
	Policy           config.Policy       `json:"policy"`
	Capabilities     []CapabilitySummary `json:"capabilities"`
	PendingApprovals int                 `json:"pending_approvals"`
	ActiveMorphlings int                 `json:"active_morphlings"`
	Connections      []ConnectionStatus  `json:"connections"`
}

type ConnectionsStatusResponse struct {
	Connections []ConnectionStatus `json:"connections"`
}

type OpenSessionRequest struct {
	Actor                 string   `json:"actor"`
	SessionID             string   `json:"session_id"`
	RequestedCapabilities []string `json:"requested_capabilities"`
	CorrelationID         string   `json:"correlation_id"`
	WorkspaceID           string   `json:"workspace_id,omitempty"`
}

type OpenSessionResponse struct {
	ControlSessionID string `json:"control_session_id"`
	CapabilityToken  string `json:"capability_token"`
	ApprovalToken    string `json:"approval_token"`
	SessionMACKey    string `json:"session_mac_key"`
	ExpiresAtUTC     string `json:"expires_at_utc"`
}

type CapabilityRequest struct {
	RequestID     string            `json:"request_id"`
	SessionID     string            `json:"session_id"`
	Actor         string            `json:"actor"`
	Capability    string            `json:"capability"`
	Arguments     map[string]string `json:"arguments"`
	CorrelationID string            `json:"correlation_id"`
	// The following fields accept mistaken copies of provider-native tool metadata
	// (OpenAI/Kimi/Moonshot shapes) into this envelope. They are stripped in
	// normalizeCapabilityRequest and must never influence policy — Capability is canonical.
	EchoedNativeToolName       string `json:"ToolName,omitempty"`
	EchoedNativeToolNameSnake  string `json:"tool_name,omitempty"`
	EchoedNativeToolNameCamel  string `json:"toolName,omitempty"`
	EchoedNativeToolUseID      string `json:"ToolUseID,omitempty"`
	EchoedNativeToolUseIDSnake string `json:"tool_use_id,omitempty"`
	EchoedNativeToolCallID     string `json:"tool_call_id,omitempty"`
	EchoedNativeToolCallIDAlt  string `json:"ToolCallID,omitempty"`
}

// MarshalJSON emits only the canonical capability-execute fields so provider-echo
// metadata decoded into the struct is never sent back on the wire (defense in depth).
func (r CapabilityRequest) MarshalJSON() ([]byte, error) {
	type capabilityRequestWire struct {
		RequestID     string            `json:"request_id"`
		SessionID     string            `json:"session_id"`
		Actor         string            `json:"actor"`
		Capability    string            `json:"capability"`
		Arguments     map[string]string `json:"arguments"`
		CorrelationID string            `json:"correlation_id"`
	}
	return json.Marshal(capabilityRequestWire{
		RequestID:     r.RequestID,
		SessionID:     r.SessionID,
		Actor:         r.Actor,
		Capability:    r.Capability,
		Arguments:     r.Arguments,
		CorrelationID: r.CorrelationID,
	})
}

type CapabilityResponse struct {
	RequestID         string                         `json:"request_id"`
	Status            string                         `json:"status"`
	StructuredResult  map[string]interface{}         `json:"structured_result,omitempty"`
	FieldsMeta        map[string]ResultFieldMetadata `json:"fields_meta,omitempty"`
	Classification    ResultClassification           `json:"classification,omitempty"`
	DenialReason      string                         `json:"denial_reason,omitempty"`
	DenialCode        string                         `json:"denial_code,omitempty"`
	ApprovalRequired  bool                           `json:"approval_required,omitempty"`
	ApprovalRequestID string                         `json:"approval_request_id,omitempty"`
	// ApprovalManifestSHA256 is the canonical approval manifest hash (AMP RFC 0005 §6).
	// Included in pending approval responses so the operator can submit it back with their
	// decision, proving they reviewed the exact manifest that was presented to them.
	ApprovalManifestSHA256 string                 `json:"approval_manifest_sha256,omitempty"`
	QuarantineRef          string                 `json:"quarantine_ref,omitempty"`
	Redacted               bool                   `json:"redacted,omitempty"`
	Metadata               map[string]interface{} `json:"metadata,omitempty"`
}

type ResultClassification struct {
	Exposure    string            `json:"exposure"`
	Eligibility ResultEligibility `json:"eligibility"`
	Quarantine  ResultQuarantine  `json:"quarantine"`
}

type ResultEligibility struct {
	Prompt bool `json:"prompt"`
	Memory bool `json:"memory"`
}

type ResultQuarantine struct {
	Quarantined bool   `json:"quarantined"`
	Ref         string `json:"ref,omitempty"`
}

type ResultFieldMetadata struct {
	Origin         string `json:"origin"`
	ContentType    string `json:"content_type"`
	Trust          string `json:"trust"`
	Sensitivity    string `json:"sensitivity"`
	SizeBytes      int    `json:"size_bytes"`
	Kind           string `json:"kind"`
	ScalarSubclass string `json:"scalar_subclass,omitempty"`
	PromptEligible bool   `json:"prompt_eligible"`
	MemoryEligible bool   `json:"memory_eligible"`
}

type ApprovalDecisionRequest struct {
	Approved bool `json:"approved"`
	// DecisionNonce is the single-use nonce issued at approval creation time. Required.
	DecisionNonce string `json:"decision_nonce"`
	// ApprovalManifestSHA256 is the canonical approval manifest hash per AMP RFC 0005 §6.
	// When provided, the server verifies it matches the manifest computed at approval creation
	// time, binding the decision to the exact method, path, and request body that was approved.
	// The server computes the manifest from the stored approval; the operator obtains this value
	// from the pending approval response and must include it to prove they reviewed the manifest.
	ApprovalManifestSHA256 string `json:"approval_manifest_sha256,omitempty"`
}

type ConnectionKeyRequest struct {
	Provider string `json:"provider"`
	Subject  string `json:"subject"`
}

type PKCEStartRequest struct {
	Provider string `json:"provider"`
	Subject  string `json:"subject"`
}

type PKCEStartResponse struct {
	Provider         string `json:"provider"`
	Subject          string `json:"subject"`
	AuthorizationURL string `json:"authorization_url"`
	State            string `json:"state"`
	ExpiresAtUTC     string `json:"expires_at_utc"`
}

type PKCECompleteRequest struct {
	Provider string `json:"provider"`
	Subject  string `json:"subject"`
	State    string `json:"state"`
	Code     string `json:"code"`
}

type SiteInspectionRequest struct {
	URL string `json:"url"`
}

type SiteCertificateInfo struct {
	Subject           string   `json:"subject"`
	Issuer            string   `json:"issuer"`
	DNSNames          []string `json:"dns_names,omitempty"`
	NotBeforeUTC      string   `json:"not_before_utc,omitempty"`
	NotAfterUTC       string   `json:"not_after_utc,omitempty"`
	FingerprintSHA256 string   `json:"fingerprint_sha256,omitempty"`
}

type SiteDraftField struct {
	Name                 string `json:"name"`
	Sensitivity          string `json:"sensitivity"`
	MaxInlineBytes       int    `json:"max_inline_bytes"`
	AllowBlobRefFallback bool   `json:"allow_blob_ref_fallback"`
	JSONPath             string `json:"json_path,omitempty"`
	HTMLTitle            bool   `json:"html_title,omitempty"`
	MetaName             string `json:"meta_name,omitempty"`
	MetaProperty         string `json:"meta_property,omitempty"`
}

type SiteTrustDraftSuggestion struct {
	Provider       string           `json:"provider"`
	Subject        string           `json:"subject"`
	CapabilityName string           `json:"capability_name"`
	ContentClass   string           `json:"content_class"`
	Extractor      string           `json:"extractor"`
	CapabilityPath string           `json:"capability_path"`
	Fields         []SiteDraftField `json:"fields"`
}

type SiteInspectionResponse struct {
	NormalizedURL     string                    `json:"normalized_url"`
	Scheme            string                    `json:"scheme"`
	Host              string                    `json:"host"`
	Path              string                    `json:"path"`
	HTTPStatusCode    int                       `json:"http_status_code"`
	ContentType       string                    `json:"content_type"`
	HTTPS             bool                      `json:"https"`
	TLSValid          bool                      `json:"tls_valid"`
	Certificate       *SiteCertificateInfo      `json:"certificate,omitempty"`
	TrustDraftAllowed bool                      `json:"trust_draft_allowed"`
	DraftSuggestion   *SiteTrustDraftSuggestion `json:"draft_suggestion,omitempty"`
}

type SiteTrustDraftRequest struct {
	URL string `json:"url"`
}

type SiteTrustDraftResponse struct {
	NormalizedURL string                   `json:"normalized_url"`
	DraftPath     string                   `json:"draft_path"`
	Suggestion    SiteTrustDraftSuggestion `json:"suggestion"`
}

type SandboxImportRequest struct {
	HostSourcePath  string `json:"host_source_path"`
	DestinationName string `json:"destination_name"`
}

type SandboxStageRequest struct {
	SandboxSourcePath string `json:"sandbox_source_path"`
	OutputName        string `json:"output_name"`
}

type SandboxMetadataRequest struct {
	SandboxSourcePath string `json:"sandbox_source_path"`
}

type SandboxExportRequest struct {
	SandboxSourcePath   string `json:"sandbox_source_path"`
	HostDestinationPath string `json:"host_destination_path"`
}

type SandboxListRequest struct {
	SandboxPath string `json:"sandbox_path"`
}

type SandboxListEntry struct {
	Name       string `json:"name"`
	EntryType  string `json:"entry_type"`
	SizeBytes  int64  `json:"size_bytes"`
	ModTimeUTC string `json:"mod_time_utc"`
}

type SandboxListResponse struct {
	SandboxPath         string             `json:"sandbox_path"`
	SandboxAbsolutePath string             `json:"sandbox_absolute_path"`
	Entries             []SandboxListEntry `json:"entries"`
}

type SandboxOperationResponse struct {
	Action              string `json:"action"`
	EntryType           string `json:"entry_type"`
	SandboxRelativePath string `json:"sandbox_relative_path,omitempty"`
	SandboxAbsolutePath string `json:"sandbox_absolute_path,omitempty"`
	SourceSandboxPath   string `json:"source_sandbox_path,omitempty"`
	HostPath            string `json:"host_path,omitempty"`
	SandboxRoot         string `json:"sandbox_root,omitempty"`
	ArtifactRef         string `json:"artifact_ref,omitempty"`
	ContentSHA256       string `json:"content_sha256,omitempty"`
	SizeBytes           int64  `json:"size_bytes,omitempty"`
}

type SandboxArtifactMetadataResponse struct {
	ArtifactRef         string `json:"artifact_ref"`
	EntryType           string `json:"entry_type"`
	SandboxRelativePath string `json:"sandbox_relative_path"`
	SandboxAbsolutePath string `json:"sandbox_absolute_path"`
	SandboxRoot         string `json:"sandbox_root"`
	SourceSandboxPath   string `json:"source_sandbox_path,omitempty"`
	ContentSHA256       string `json:"content_sha256"`
	SizeBytes           int64  `json:"size_bytes"`
	StagedAtUTC         string `json:"staged_at_utc"`
	ReviewAction        string `json:"review_action"`
	ExportAction        string `json:"export_action"`
}

type MorphlingInput struct {
	SandboxPath string `json:"sandbox_path"`
	Role        string `json:"role,omitempty"`
}

type ContinuitySourceRefInput struct {
	Kind   string `json:"kind"`
	Ref    string `json:"ref"`
	SHA256 string `json:"sha256,omitempty"`
}

type ContinuityEventInput struct {
	TimestampUTC    string                     `json:"ts_utc"`
	SessionID       string                     `json:"session_id"`
	Type            string                     `json:"type"`
	Scope           string                     `json:"scope"`
	ThreadID        string                     `json:"thread_id"`
	EpistemicFlavor string                     `json:"epistemic_flavor"`
	LedgerSequence  int64                      `json:"ledger_sequence"`
	EventHash       string                     `json:"event_hash"`
	SourceRefs      []ContinuitySourceRefInput `json:"source_refs,omitempty"`
	Payload         map[string]interface{}     `json:"payload,omitempty"`
}

type ContinuityInspectRequest struct {
	InspectionID       string                 `json:"inspection_id"`
	ThreadID           string                 `json:"thread_id"`
	Scope              string                 `json:"scope"`
	SealedAtUTC        string                 `json:"sealed_at_utc"`
	EventCount         int                    `json:"event_count"`
	ApproxPayloadBytes int                    `json:"approx_payload_bytes"`
	ApproxPromptTokens int                    `json:"approx_prompt_tokens"`
	Tags               []string               `json:"tags,omitempty"`
	Events             []ContinuityEventInput `json:"events"`
}

type ContinuityInspectResponse struct {
	InspectionID          string   `json:"inspection_id"`
	ThreadID              string   `json:"thread_id"`
	Outcome               string   `json:"outcome"`
	DerivationOutcome     string   `json:"derivation_outcome,omitempty"`
	ReviewStatus          string   `json:"review_status,omitempty"`
	LineageStatus         string   `json:"lineage_status,omitempty"`
	DerivedDistillateIDs  []string `json:"derived_distillate_ids,omitempty"`
	DerivedResonateKeyIDs []string `json:"derived_resonate_key_ids,omitempty"`
}

// HavenContinuityInspectThreadRequest is the JSON body for POST /v1/continuity/inspect-thread.
// The legacy /v1/haven/continuity/inspect-thread alias uses the same payload.
// Loopgate loads the thread from its threadstore and proposes continuity for
// inspection; the client does not supply raw events (unlike POST /v1/continuity/inspect).
type HavenContinuityInspectThreadRequest struct {
	ThreadID string `json:"thread_id"`
}

// HavenContinuityInspectThreadResponse is returned by POST /v1/continuity/inspect-thread.
// The legacy /v1/haven/continuity/inspect-thread alias returns the same shape.
// SubmitStatus is "submitted" when an inspection ran, or "skipped_no_continuity_events" when
// the thread had no user_message / assistant_response / tool_executed mappable rows.
type HavenContinuityInspectThreadResponse struct {
	ThreadID              string   `json:"thread_id"`
	SubmitStatus          string   `json:"submit_status"`
	InspectionID          string   `json:"inspection_id,omitempty"`
	Outcome               string   `json:"outcome,omitempty"`
	DerivationOutcome     string   `json:"derivation_outcome,omitempty"`
	ReviewStatus          string   `json:"review_status,omitempty"`
	LineageStatus         string   `json:"lineage_status,omitempty"`
	DerivedDistillateIDs  []string `json:"derived_distillate_ids,omitempty"`
	DerivedResonateKeyIDs []string `json:"derived_resonate_key_ids,omitempty"`
}

type MemoryInspectionReviewRequest struct {
	Decision    string `json:"decision"`
	OperationID string `json:"operation_id"`
	Reason      string `json:"reason,omitempty"`
}

type MemoryInspectionLineageRequest struct {
	OperationID string `json:"operation_id"`
	Reason      string `json:"reason,omitempty"`
}

type MemoryInspectionGovernanceResponse struct {
	InspectionID          string   `json:"inspection_id"`
	ThreadID              string   `json:"thread_id"`
	DerivationOutcome     string   `json:"derivation_outcome"`
	ReviewStatus          string   `json:"review_status"`
	LineageStatus         string   `json:"lineage_status"`
	DerivedDistillateIDs  []string `json:"derived_distillate_ids,omitempty"`
	DerivedResonateKeyIDs []string `json:"derived_resonate_key_ids,omitempty"`
}

type MemoryWakeStateSourceRef struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

type MemoryWakeStateOpenItem struct {
	ID              string `json:"id"`
	Text            string `json:"text"`
	TaskKind        string `json:"task_kind,omitempty"`
	SourceKind      string `json:"source_kind,omitempty"`
	NextStep        string `json:"next_step,omitempty"`
	ScheduledForUTC string `json:"scheduled_for_utc,omitempty"`
	ExecutionClass  string `json:"execution_class,omitempty"`
	CreatedAtUTC    string `json:"created_at_utc,omitempty"`
	// Status is explicit Task Board workflow: "todo" or "in_progress" (default "todo" when absent).
	Status string `json:"status,omitempty"`
}

// UITasksResponse is the task board projection for local operator clients
// (control session auth; not a capability execute surface).
type UITasksResponse struct {
	Goals []string           `json:"goals"`
	Items []UITasksItemEntry `json:"items"`
}

// UITasksItemEntry is one row in GET /v1/tasks.
type UITasksItemEntry struct {
	ID           string `json:"id"`
	Text         string `json:"text"`
	TaskKind     string `json:"task_kind"`
	SourceKind   string `json:"source_kind"`
	NextStep     string `json:"next_step,omitempty"`
	Status       string `json:"status"`
	CreatedAtUTC string `json:"created_at_utc"`
}

// UITasksStatusUpdateRequest is the body for PUT /v1/tasks/{id}/status.
type UITasksStatusUpdateRequest struct {
	Status string `json:"status"`
}

// --- Local operator UI payloads (GET/POST /v1/ui/*).
// Type names are retained for compatibility with existing clients.

// HavenMemoryInventoryResponse is the operator-facing memory inventory projection for GET /v1/ui/memory.
type HavenMemoryInventoryResponse struct {
	WakeStateID             string                   `json:"wake_state_id,omitempty"`
	WakeCreatedAtUTC        string                   `json:"wake_created_at_utc,omitempty"`
	RecentFactCount         int                      `json:"recent_fact_count"`
	ActiveGoalCount         int                      `json:"active_goal_count"`
	UnresolvedItemCount     int                      `json:"unresolved_item_count"`
	ResonateKeyCount        int                      `json:"resonate_key_count"`
	IncludedDiagnosticCount int                      `json:"included_diagnostic_count"`
	ExcludedDiagnosticCount int                      `json:"excluded_diagnostic_count"`
	PendingReviewCount      int                      `json:"pending_review_count"`
	EligibleCount           int                      `json:"eligible_count"`
	TombstonedCount         int                      `json:"tombstoned_count"`
	PurgedCount             int                      `json:"purged_count"`
	Objects                 []HavenMemoryObjectEntry `json:"objects"`
}

// HavenMemoryObjectEntry is one operator-manageable continuity lineage root.
type HavenMemoryObjectEntry struct {
	InspectionID             string `json:"inspection_id"`
	ThreadID                 string `json:"thread_id"`
	Scope                    string `json:"scope"`
	ObjectKind               string `json:"object_kind"`
	Summary                  string `json:"summary,omitempty"`
	SubmittedAtUTC           string `json:"submitted_at_utc,omitempty"`
	CompletedAtUTC           string `json:"completed_at_utc,omitempty"`
	DerivationOutcome        string `json:"derivation_outcome"`
	ReviewStatus             string `json:"review_status"`
	LineageStatus            string `json:"lineage_status"`
	GoalType                 string `json:"goal_type,omitempty"`
	GoalFamilyID             string `json:"goal_family_id,omitempty"`
	DerivedDistillateCount   int    `json:"derived_distillate_count"`
	DerivedResonateKeyCount  int    `json:"derived_resonate_key_count"`
	SupersedesInspectionID   string `json:"supersedes_inspection_id,omitempty"`
	SupersededByInspectionID string `json:"superseded_by_inspection_id,omitempty"`
	RetentionWindowActive    bool   `json:"retention_window_active,omitempty"`
	CanReview                bool   `json:"can_review"`
	CanTombstone             bool   `json:"can_tombstone"`
	CanPurge                 bool   `json:"can_purge"`
}

// HavenMemoryResetRequest is the body for POST /v1/ui/memory/reset.
type HavenMemoryResetRequest struct {
	OperationID string `json:"operation_id"`
	Reason      string `json:"reason,omitempty"`
}

// HavenMemoryResetResponse reports the archived fresh-start reset result.
type HavenMemoryResetResponse struct {
	ResetAtUTC               string `json:"reset_at_utc"`
	ArchiveID                string `json:"archive_id,omitempty"`
	PreviousInspectionCount  int    `json:"previous_inspection_count"`
	PreviousDistillateCount  int    `json:"previous_distillate_count"`
	PreviousResonateKeyCount int    `json:"previous_resonate_key_count"`
	WakeStateID              string `json:"wake_state_id,omitempty"`
}

// HavenAgentWorkEnsureRequest is the JSON body for POST /v1/agent/work-item/ensure.
// The legacy /v1/haven/agent/work-item/ensure alias uses the same payload.
type HavenAgentWorkEnsureRequest struct {
	Text     string `json:"text"`
	NextStep string `json:"next_step,omitempty"`
}

// HavenAgentWorkItemResponse is returned by work-item ensure and complete routes.
// The type name is retained for compatibility with existing clients.
type HavenAgentWorkItemResponse struct {
	ItemID         string `json:"item_id"`
	Text           string `json:"text"`
	AlreadyPresent bool   `json:"already_present"`
}

// HavenAgentWorkCompleteRequest is the JSON body for POST /v1/agent/work-item/complete.
// The legacy /v1/haven/agent/work-item/complete alias uses the same payload.
type HavenAgentWorkCompleteRequest struct {
	ItemID string `json:"item_id"`
	Reason string `json:"reason,omitempty"`
}

// HavenDeskNote is the runtime/state/haven_desk_notes.json entry shape.
// The type name is retained for compatibility with existing clients.
type HavenDeskNote struct {
	ID                  string               `json:"id"`
	Kind                string               `json:"kind"`
	Title               string               `json:"title"`
	Body                string               `json:"body"`
	Action              *HavenDeskNoteAction `json:"action,omitempty"`
	ActionExecutedAtUTC string               `json:"action_executed_at_utc,omitempty"`
	ActionThreadID      string               `json:"action_thread_id,omitempty"`
	CreatedAtUTC        string               `json:"created_at_utc"`
	ArchivedAtUTC       string               `json:"archived_at_utc,omitempty"`
}

// HavenDeskNoteAction is the desk-note action shape for UI consumers.
type HavenDeskNoteAction struct {
	Kind    string `json:"kind"`
	Label   string `json:"label,omitempty"`
	Message string `json:"message,omitempty"`
}

// HavenDeskNotesResponse is the body for GET /v1/ui/desk-notes.
type HavenDeskNotesResponse struct {
	Notes []HavenDeskNote `json:"notes"`
}

// HavenDeskNoteDismissRequest is the body for POST /v1/ui/desk-notes/dismiss.
type HavenDeskNoteDismissRequest struct {
	NoteID string `json:"note_id"`
}

// HavenJournalEntrySummary is one journal entry row for local UI consumers.
type HavenJournalEntrySummary struct {
	Path         string `json:"path"`
	Title        string `json:"title"`
	Preview      string `json:"preview"`
	UpdatedAtUTC string `json:"updated_at_utc"`
	EntryCount   int    `json:"entry_count"`
}

// HavenJournalEntriesResponse is GET /v1/ui/journal/entries.
type HavenJournalEntriesResponse struct {
	Entries []HavenJournalEntrySummary `json:"entries"`
}

// HavenJournalEntryResponse is GET /v1/ui/journal/entry.
type HavenJournalEntryResponse struct {
	Path         string `json:"path"`
	Title        string `json:"title"`
	Content      string `json:"content"`
	EntryCount   int    `json:"entry_count"`
	UpdatedAtUTC string `json:"updated_at_utc,omitempty"`
	Error        string `json:"error,omitempty"`
}

// HavenWorkingNoteSummary is one working-note row for local UI consumers.
type HavenWorkingNoteSummary struct {
	Path         string `json:"path"`
	Title        string `json:"title"`
	Preview      string `json:"preview"`
	UpdatedAtUTC string `json:"updated_at_utc"`
}

// HavenWorkingNotesResponse is GET /v1/ui/working-notes.
type HavenWorkingNotesResponse struct {
	Notes []HavenWorkingNoteSummary `json:"notes"`
}

// HavenWorkingNoteResponse is GET /v1/ui/working-notes/entry.
type HavenWorkingNoteResponse struct {
	Path    string `json:"path"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}

// HavenWorkingNoteSaveRequest is POST /v1/ui/working-notes/save.
type HavenWorkingNoteSaveRequest struct {
	Path    string `json:"path,omitempty"`
	Title   string `json:"title,omitempty"`
	Content string `json:"content"`
}

// HavenWorkingNoteSaveResponse is the save response for local UI consumers.
type HavenWorkingNoteSaveResponse struct {
	Saved bool   `json:"saved"`
	Path  string `json:"path,omitempty"`
	Title string `json:"title,omitempty"`
	Error string `json:"error,omitempty"`
}

// HavenWorkspaceListRequest is POST /v1/ui/workspace/list.
type HavenWorkspaceListRequest struct {
	Path string `json:"path"`
}

// HavenWorkspaceListEntry is one workspace row for local UI consumers.
type HavenWorkspaceListEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	EntryType  string `json:"entry_type"`
	SizeBytes  int64  `json:"size_bytes"`
	ModTimeUTC string `json:"mod_time_utc,omitempty"`
}

// HavenWorkspaceListResponse is the workspace listing response for local UI consumers.
type HavenWorkspaceListResponse struct {
	Path    string                    `json:"path"`
	Entries []HavenWorkspaceListEntry `json:"entries"`
	Error   string                    `json:"error,omitempty"`
}

// HavenWorkspacePreviewRequest is POST /v1/ui/workspace/preview.
type HavenWorkspacePreviewRequest struct {
	Path string `json:"path"`
}

// HavenWorkspacePreviewResponse is the workspace preview response for local UI consumers.
type HavenWorkspacePreviewResponse struct {
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
	Path      string `json:"path"`
	Error     string `json:"error,omitempty"`
}

// HavenPresenceResponse is the presence projection for GET /v1/ui/presence.
type HavenPresenceResponse struct {
	State      string `json:"state"`
	StatusText string `json:"status_text"`
	DetailText string `json:"detail_text,omitempty"`
	Anchor     string `json:"anchor"`
}

// HavenMorphSleepResponse extends presence with booleans for light clients (GET /v1/ui/morph-sleep).
type HavenMorphSleepResponse struct {
	State      string `json:"state"`
	StatusText string `json:"status_text"`
	DetailText string `json:"detail_text,omitempty"`
	Anchor     string `json:"anchor"`
	IsSleeping bool   `json:"is_sleeping"`
	IsResting  bool   `json:"is_resting"`
}

type MemoryWakeStateRecentFact struct {
	Name               string      `json:"name"`
	Value              interface{} `json:"value"`
	SourceRef          string      `json:"source_ref"`
	EpistemicFlavor    string      `json:"epistemic_flavor"`
	ConflictKeyVersion string      `json:"conflict_key_version,omitempty"`
	ConflictKey        string      `json:"conflict_key,omitempty"`
	CertaintyScore     int         `json:"certainty_score,omitempty"`
}

type MemoryWakeStateResponse struct {
	ID                 string                      `json:"id"`
	Scope              string                      `json:"scope"`
	CreatedAtUTC       string                      `json:"created_at_utc"`
	SourceRefs         []MemoryWakeStateSourceRef  `json:"source_refs,omitempty"`
	ActiveGoals        []string                    `json:"active_goals,omitempty"`
	UnresolvedItems    []MemoryWakeStateOpenItem   `json:"unresolved_items,omitempty"`
	RecentFacts        []MemoryWakeStateRecentFact `json:"recent_facts,omitempty"`
	ResonateKeys       []string                    `json:"resonate_keys,omitempty"`
	PromptTokenBudget  int                         `json:"prompt_token_budget,omitempty"`
	ApproxPromptTokens int                         `json:"approx_prompt_tokens,omitempty"`
}

type MemoryDiagnosticWakeEntry struct {
	ItemKind         string   `json:"item_kind"`
	GoalFamilyID     string   `json:"goal_family_id,omitempty"`
	Scope            string   `json:"scope,omitempty"`
	RetentionScore   int      `json:"retention_score,omitempty"`
	EffectiveHotness int      `json:"effective_hotness,omitempty"`
	Reason           string   `json:"reason,omitempty"`
	TrimReason       string   `json:"trim_reason,omitempty"`
	PrecedenceSource string   `json:"precedence_source,omitempty"`
	ScoreTrace       []string `json:"score_trace,omitempty"`
	RedactedSummary  string   `json:"redacted_summary,omitempty"`
}

type MemoryDiagnosticWakeResponse struct {
	SchemaVersion     string                      `json:"schema_version"`
	ResolutionVersion string                      `json:"resolution_version"`
	ReportID          string                      `json:"report_id"`
	CreatedAtUTC      string                      `json:"created_at_utc"`
	RuntimeWakeID     string                      `json:"runtime_wake_id"`
	IncludedCount     int                         `json:"included_count"`
	ExcludedCount     int                         `json:"excluded_count"`
	Entries           []MemoryDiagnosticWakeEntry `json:"entries,omitempty"`
	ExcludedEntries   []MemoryDiagnosticWakeEntry `json:"excluded_entries,omitempty"`
}

type MemoryDiscoverRequest struct {
	Scope    string `json:"scope,omitempty"`
	Query    string `json:"query"`
	MaxItems int    `json:"max_items,omitempty"`
}

type MemoryDiscoverItem struct {
	KeyID        string   `json:"key_id"`
	ThreadID     string   `json:"thread_id"`
	DistillateID string   `json:"distillate_id"`
	Scope        string   `json:"scope"`
	CreatedAtUTC string   `json:"created_at_utc"`
	Tags         []string `json:"tags,omitempty"`
	MatchCount   int      `json:"match_count"`
}

type MemoryDiscoverResponse struct {
	Scope string               `json:"scope"`
	Query string               `json:"query"`
	Items []MemoryDiscoverItem `json:"items,omitempty"`
}

type MemoryRecallRequest struct {
	Scope         string   `json:"scope,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	MaxItems      int      `json:"max_items,omitempty"`
	MaxTokens     int      `json:"max_tokens,omitempty"`
	RequestedKeys []string `json:"requested_keys"`
}

type MemoryRecallFact struct {
	Name               string      `json:"name"`
	Value              interface{} `json:"value"`
	SourceRef          string      `json:"source_ref"`
	EpistemicFlavor    string      `json:"epistemic_flavor"`
	ConflictKeyVersion string      `json:"conflict_key_version,omitempty"`
	ConflictKey        string      `json:"conflict_key,omitempty"`
	CertaintyScore     int         `json:"certainty_score,omitempty"`
}

type MemoryRecallItem struct {
	KeyID           string                    `json:"key_id"`
	ThreadID        string                    `json:"thread_id"`
	DistillateID    string                    `json:"distillate_id"`
	Scope           string                    `json:"scope"`
	CreatedAtUTC    string                    `json:"created_at_utc"`
	Tags            []string                  `json:"tags,omitempty"`
	ActiveGoals     []string                  `json:"active_goals,omitempty"`
	UnresolvedItems []MemoryWakeStateOpenItem `json:"unresolved_items,omitempty"`
	Facts           []MemoryRecallFact        `json:"facts,omitempty"`
	EpistemicFlavor string                    `json:"epistemic_flavor"`
}

type MemoryRecallResponse struct {
	Scope            string             `json:"scope"`
	MaxItems         int                `json:"max_items"`
	MaxTokens        int                `json:"max_tokens"`
	ApproxTokenCount int                `json:"approx_token_count"`
	Items            []MemoryRecallItem `json:"items,omitempty"`
}

type MemoryRememberRequest struct {
	Scope           string `json:"scope,omitempty"`
	FactKey         string `json:"fact_key"`
	FactValue       string `json:"fact_value"`
	Reason          string `json:"reason,omitempty"`
	SourceText      string `json:"source_text,omitempty"`
	CandidateSource string `json:"candidate_source,omitempty"`
	SourceChannel   string `json:"source_channel,omitempty"`
}

type MemoryRememberResponse struct {
	Scope               string `json:"scope"`
	FactKey             string `json:"fact_key"`
	FactValue           string `json:"fact_value"`
	InspectionID        string `json:"inspection_id"`
	DistillateID        string `json:"distillate_id"`
	ResonateKeyID       string `json:"resonate_key_id"`
	RememberedAtUTC     string `json:"remembered_at_utc"`
	SupersededFactValue string `json:"superseded_fact_value,omitempty"`
	UpdatedExisting     bool   `json:"updated_existing"`
}

type TodoAddRequest struct {
	Scope           string `json:"scope,omitempty"`
	Text            string `json:"text"`
	TaskKind        string `json:"task_kind,omitempty"`
	SourceKind      string `json:"source_kind,omitempty"`
	NextStep        string `json:"next_step,omitempty"`
	ScheduledForUTC string `json:"scheduled_for_utc,omitempty"`
	ExecutionClass  string `json:"execution_class,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

type TodoAddResponse struct {
	Scope           string `json:"scope"`
	ItemID          string `json:"item_id"`
	Text            string `json:"text"`
	TaskKind        string `json:"task_kind,omitempty"`
	SourceKind      string `json:"source_kind,omitempty"`
	NextStep        string `json:"next_step,omitempty"`
	ScheduledForUTC string `json:"scheduled_for_utc,omitempty"`
	ExecutionClass  string `json:"execution_class,omitempty"`
	InspectionID    string `json:"inspection_id"`
	DistillateID    string `json:"distillate_id"`
	ResonateKeyID   string `json:"resonate_key_id"`
	AddedAtUTC      string `json:"added_at_utc"`
	AlreadyPresent  bool   `json:"already_present"`
}

type TodoCompleteRequest struct {
	Scope  string `json:"scope,omitempty"`
	ItemID string `json:"item_id"`
	Reason string `json:"reason,omitempty"`
}

type TodoCompleteResponse struct {
	Scope          string `json:"scope"`
	ItemID         string `json:"item_id"`
	Text           string `json:"text,omitempty"`
	InspectionID   string `json:"inspection_id,omitempty"`
	DistillateID   string `json:"distillate_id,omitempty"`
	CompletedAtUTC string `json:"completed_at_utc"`
}

type TodoListResponse struct {
	Scope           string                    `json:"scope"`
	UnresolvedItems []MemoryWakeStateOpenItem `json:"unresolved_items,omitempty"`
	ActiveGoals     []string                  `json:"active_goals,omitempty"`
}

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
	RequestID           string   `json:"request_id,omitempty"`
	Status              string   `json:"status"`
	DenialReason        string   `json:"denial_reason,omitempty"`
	DenialCode          string   `json:"denial_code,omitempty"`
	MorphlingID         string   `json:"morphling_id,omitempty"`
	TaskID              string   `json:"task_id,omitempty"`
	State               string   `json:"state,omitempty"`
	Class               string   `json:"class,omitempty"`
	ApprovalID          string   `json:"approval_id,omitempty"`
	ApprovalDeadlineUTC string   `json:"approval_deadline_utc,omitempty"`
	GrantedCapabilities []string `json:"granted_capabilities,omitempty"`
	VirtualSandboxPath  string   `json:"virtual_sandbox_path,omitempty"`
	SpawnedAtUTC        string   `json:"spawned_at_utc,omitempty"`
	TokenExpiryUTC      string   `json:"token_expiry_utc,omitempty"`
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

func (continuityInspectRequest ContinuityInspectRequest) Validate() error {
	if err := identifiers.ValidateSafeIdentifier("inspection_id", continuityInspectRequest.InspectionID); err != nil {
		return err
	}
	if err := identifiers.ValidateSafeIdentifier("thread_id", continuityInspectRequest.ThreadID); err != nil {
		return err
	}
	if strings.TrimSpace(continuityInspectRequest.Scope) == "" {
		return fmt.Errorf("scope is required")
	}
	if strings.TrimSpace(continuityInspectRequest.SealedAtUTC) == "" {
		return fmt.Errorf("sealed_at_utc is required")
	}
	if _, err := time.Parse(time.RFC3339Nano, continuityInspectRequest.SealedAtUTC); err != nil {
		return fmt.Errorf("sealed_at_utc is invalid: %w", err)
	}
	if continuityInspectRequest.EventCount < 0 {
		return fmt.Errorf("event_count must be non-negative")
	}
	if continuityInspectRequest.ApproxPayloadBytes < 0 {
		return fmt.Errorf("approx_payload_bytes must be non-negative")
	}
	if continuityInspectRequest.ApproxPromptTokens < 0 {
		return fmt.Errorf("approx_prompt_tokens must be non-negative")
	}
	if len(continuityInspectRequest.Events) == 0 {
		return fmt.Errorf("events is required")
	}
	if len(continuityInspectRequest.Events) > maxContinuityEventsPerInspection {
		return fmt.Errorf("events exceeds maximum allowed (%d)", maxContinuityEventsPerInspection)
	}
	if continuityInspectRequest.ApproxPayloadBytes > maxContinuityInspectApproxPayloadBytes {
		return fmt.Errorf("approx_payload_bytes exceeds maximum allowed (%d)", maxContinuityInspectApproxPayloadBytes)
	}
	measuredPayloadBytes := actualContinuityPayloadBytes(continuityInspectRequest.Events)
	if measuredPayloadBytes > maxContinuityInspectApproxPayloadBytes {
		return fmt.Errorf("continuity event payload size exceeds maximum allowed (%d bytes)", maxContinuityInspectApproxPayloadBytes)
	}
	for _, continuityEvent := range continuityInspectRequest.Events {
		if err := continuityEvent.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (continuityEvent ContinuityEventInput) Validate() error {
	if strings.TrimSpace(continuityEvent.TimestampUTC) == "" {
		return fmt.Errorf("continuity event ts_utc is required")
	}
	if _, err := time.Parse(time.RFC3339Nano, continuityEvent.TimestampUTC); err != nil {
		return fmt.Errorf("continuity event ts_utc is invalid: %w", err)
	}
	if strings.TrimSpace(continuityEvent.SessionID) == "" {
		return fmt.Errorf("continuity event session_id is required")
	}
	if strings.TrimSpace(continuityEvent.Type) == "" {
		return fmt.Errorf("continuity event type is required")
	}
	if strings.TrimSpace(continuityEvent.Scope) == "" {
		return fmt.Errorf("continuity event scope is required")
	}
	if strings.TrimSpace(continuityEvent.ThreadID) == "" {
		return fmt.Errorf("continuity event thread_id is required")
	}
	if continuityEvent.LedgerSequence < 0 {
		return fmt.Errorf("continuity event ledger_sequence must be non-negative")
	}
	if strings.TrimSpace(continuityEvent.EventHash) == "" {
		return fmt.Errorf("continuity event event_hash is required")
	}
	for _, sourceRef := range continuityEvent.SourceRefs {
		if strings.TrimSpace(sourceRef.Kind) == "" || strings.TrimSpace(sourceRef.Ref) == "" {
			return fmt.Errorf("continuity event source_refs require kind and ref")
		}
	}
	return nil
}

func (memoryDiscoverRequest *MemoryDiscoverRequest) Validate() error {
	if strings.TrimSpace(memoryDiscoverRequest.Scope) == "" {
		memoryDiscoverRequest.Scope = "global"
	}
	if strings.TrimSpace(memoryDiscoverRequest.Query) == "" {
		return fmt.Errorf("query is required")
	}
	if memoryDiscoverRequest.MaxItems == 0 {
		memoryDiscoverRequest.MaxItems = 5
	}
	if memoryDiscoverRequest.MaxItems < 1 || memoryDiscoverRequest.MaxItems > 10 {
		return fmt.Errorf("max_items must be between 1 and 10")
	}
	return nil
}

func (memoryRecallRequest *MemoryRecallRequest) Validate() error {
	if strings.TrimSpace(memoryRecallRequest.Scope) == "" {
		memoryRecallRequest.Scope = "global"
	}
	if memoryRecallRequest.MaxItems == 0 {
		memoryRecallRequest.MaxItems = 10
	}
	if memoryRecallRequest.MaxTokens == 0 {
		memoryRecallRequest.MaxTokens = 2000
	}
	if memoryRecallRequest.MaxItems < 1 || memoryRecallRequest.MaxItems > 10 {
		return fmt.Errorf("max_items must be between 1 and 10")
	}
	if memoryRecallRequest.MaxTokens < 1 || memoryRecallRequest.MaxTokens > 8000 {
		return fmt.Errorf("max_tokens must be between 1 and 8000")
	}
	if len(memoryRecallRequest.RequestedKeys) == 0 {
		return fmt.Errorf("requested_keys is required")
	}
	requestedKeySet := make(map[string]struct{}, len(memoryRecallRequest.RequestedKeys))
	for _, rawKeyID := range memoryRecallRequest.RequestedKeys {
		validatedKeyID := strings.TrimSpace(rawKeyID)
		if validatedKeyID == "" {
			return fmt.Errorf("requested_keys entries must be non-empty")
		}
		if _, duplicate := requestedKeySet[validatedKeyID]; duplicate {
			return fmt.Errorf("requested_keys contains duplicate %q", validatedKeyID)
		}
		requestedKeySet[validatedKeyID] = struct{}{}
	}
	if len(memoryRecallRequest.RequestedKeys) > memoryRecallRequest.MaxItems {
		return fmt.Errorf("requested_keys exceeds max_items")
	}
	return nil
}

func (memoryRememberRequest MemoryRememberRequest) Validate() error {
	if strings.TrimSpace(memoryRememberRequest.FactKey) == "" {
		return fmt.Errorf("fact_key is required")
	}
	if strings.TrimSpace(memoryRememberRequest.FactValue) == "" {
		return fmt.Errorf("fact_value is required")
	}
	if len([]byte(strings.TrimSpace(memoryRememberRequest.FactValue))) > 256 {
		return fmt.Errorf("fact_value exceeds maximum length")
	}
	if strings.ContainsAny(memoryRememberRequest.FactValue, "\r\n") {
		return fmt.Errorf("fact_value must be a single line")
	}
	if len(strings.TrimSpace(memoryRememberRequest.Reason)) > 200 {
		return fmt.Errorf("reason exceeds maximum length")
	}
	if len(strings.TrimSpace(memoryRememberRequest.SourceText)) > 512 {
		return fmt.Errorf("source_text exceeds maximum length")
	}
	trimmedCandidateSource := strings.TrimSpace(memoryRememberRequest.CandidateSource)
	if len(trimmedCandidateSource) > 64 {
		return fmt.Errorf("candidate_source exceeds maximum length")
	}
	if trimmedCandidateSource != "" && trimmedCandidateSource != string(tclpkg.CandidateSourceExplicitFact) {
		return fmt.Errorf("candidate_source %q is not supported; only %q is implemented for explicit memory writes", trimmedCandidateSource, tclpkg.CandidateSourceExplicitFact)
	}
	if len(strings.TrimSpace(memoryRememberRequest.SourceChannel)) > 64 {
		return fmt.Errorf("source_channel exceeds maximum length")
	}
	return nil
}

func (todoAddRequest TodoAddRequest) Validate() error {
	if strings.TrimSpace(todoAddRequest.Text) == "" {
		return fmt.Errorf("text is required")
	}
	if len([]byte(strings.TrimSpace(todoAddRequest.Text))) > 200 {
		return fmt.Errorf("text exceeds maximum length")
	}
	if strings.ContainsAny(todoAddRequest.Text, "\r\n") {
		return fmt.Errorf("text must be a single line")
	}
	if len(strings.TrimSpace(todoAddRequest.TaskKind)) > 32 {
		return fmt.Errorf("task_kind exceeds maximum length")
	}
	if len(strings.TrimSpace(todoAddRequest.SourceKind)) > 64 {
		return fmt.Errorf("source_kind exceeds maximum length")
	}
	if len([]byte(strings.TrimSpace(todoAddRequest.NextStep))) > 200 {
		return fmt.Errorf("next_step exceeds maximum length")
	}
	if strings.ContainsAny(todoAddRequest.NextStep, "\r\n") {
		return fmt.Errorf("next_step must be a single line")
	}
	if strings.TrimSpace(todoAddRequest.ScheduledForUTC) != "" {
		if _, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(todoAddRequest.ScheduledForUTC)); err != nil {
			return fmt.Errorf("scheduled_for_utc is invalid: %w", err)
		}
	}
	if len(strings.TrimSpace(todoAddRequest.ExecutionClass)) > 64 {
		return fmt.Errorf("execution_class exceeds maximum length")
	}
	if len(strings.TrimSpace(todoAddRequest.Reason)) > 200 {
		return fmt.Errorf("reason exceeds maximum length")
	}
	return nil
}

func (todoCompleteRequest TodoCompleteRequest) Validate() error {
	if strings.TrimSpace(todoCompleteRequest.ItemID) == "" {
		return fmt.Errorf("item_id is required")
	}
	if len([]byte(strings.TrimSpace(todoCompleteRequest.ItemID))) > 96 {
		return fmt.Errorf("item_id exceeds maximum length")
	}
	if len(strings.TrimSpace(todoCompleteRequest.Reason)) > 200 {
		return fmt.Errorf("reason exceeds maximum length")
	}
	return nil
}

func (memoryInspectionReviewRequest MemoryInspectionReviewRequest) Validate() error {
	switch strings.TrimSpace(memoryInspectionReviewRequest.Decision) {
	case "accepted", "rejected":
	default:
		return fmt.Errorf("decision must be accepted or rejected")
	}
	if err := identifiers.ValidateSafeIdentifier("operation_id", strings.TrimSpace(memoryInspectionReviewRequest.OperationID)); err != nil {
		return err
	}
	return nil
}

func (memoryInspectionLineageRequest MemoryInspectionLineageRequest) Validate() error {
	if err := identifiers.ValidateSafeIdentifier("operation_id", strings.TrimSpace(memoryInspectionLineageRequest.OperationID)); err != nil {
		return err
	}
	return nil
}

func (havenMemoryResetRequest HavenMemoryResetRequest) Validate() error {
	if err := identifiers.ValidateSafeIdentifier("operation_id", strings.TrimSpace(havenMemoryResetRequest.OperationID)); err != nil {
		return err
	}
	if len(strings.TrimSpace(havenMemoryResetRequest.Reason)) > 200 {
		return fmt.Errorf("reason exceeds maximum length")
	}
	return nil
}

func ValidateGrantType(rawGrantType string) error {
	normalizedGrantType := strings.TrimSpace(rawGrantType)
	switch normalizedGrantType {
	case GrantTypePublicRead, GrantTypePKCE, GrantTypeAuthorizationCode, GrantTypeClientCredentials:
		return nil
	default:
		return fmt.Errorf("unsupported grant type %q", rawGrantType)
	}
}

func (connectionStatus ConnectionStatus) Validate() error {
	if err := identifiers.ValidateSafeIdentifier("connection provider", connectionStatus.Provider); err != nil {
		return err
	}
	if strings.TrimSpace(connectionStatus.GrantType) != "" {
		if err := ValidateGrantType(connectionStatus.GrantType); err != nil {
			return err
		}
	}
	if strings.TrimSpace(connectionStatus.Subject) != "" {
		if err := identifiers.ValidateSafeIdentifier("connection subject", connectionStatus.Subject); err != nil {
			return err
		}
	}
	for _, rawScope := range connectionStatus.Scopes {
		if err := identifiers.ValidateSafeIdentifier("connection scope", rawScope); err != nil {
			return err
		}
	}
	if strings.TrimSpace(connectionStatus.Status) != "" {
		if err := identifiers.ValidateSafeIdentifier("connection status", connectionStatus.Status); err != nil {
			return err
		}
	}
	if strings.TrimSpace(connectionStatus.SecureStoreRefID) != "" {
		if err := identifiers.ValidateSafeIdentifier("connection secure store ref id", connectionStatus.SecureStoreRefID); err != nil {
			return err
		}
	}
	return nil
}

func (openSessionRequest OpenSessionRequest) Validate() error {
	if strings.TrimSpace(openSessionRequest.Actor) != "" {
		if err := identifiers.ValidateSafeIdentifier("actor", openSessionRequest.Actor); err != nil {
			return err
		}
	}
	if strings.TrimSpace(openSessionRequest.SessionID) != "" {
		if err := identifiers.ValidateSafeIdentifier("session_id", openSessionRequest.SessionID); err != nil {
			return err
		}
	}
	if strings.TrimSpace(openSessionRequest.CorrelationID) != "" {
		if err := identifiers.ValidateSafeIdentifier("correlation_id", openSessionRequest.CorrelationID); err != nil {
			return err
		}
	}
	for _, requestedCapability := range openSessionRequest.RequestedCapabilities {
		if err := identifiers.ValidateSafeIdentifier("requested capability", requestedCapability); err != nil {
			return err
		}
	}
	return nil
}

func (capabilityRequest CapabilityRequest) Validate() error {
	if strings.TrimSpace(capabilityRequest.RequestID) != "" {
		if err := identifiers.ValidateSafeIdentifier("request_id", capabilityRequest.RequestID); err != nil {
			return err
		}
	}
	if strings.TrimSpace(capabilityRequest.SessionID) != "" {
		if err := identifiers.ValidateSafeIdentifier("session_id", capabilityRequest.SessionID); err != nil {
			return err
		}
	}
	if strings.TrimSpace(capabilityRequest.Actor) != "" {
		if err := identifiers.ValidateSafeIdentifier("actor", capabilityRequest.Actor); err != nil {
			return err
		}
	}
	if err := identifiers.ValidateSafeIdentifier("capability", capabilityRequest.Capability); err != nil {
		return err
	}
	if strings.TrimSpace(capabilityRequest.CorrelationID) != "" {
		if err := identifiers.ValidateSafeIdentifier("correlation_id", capabilityRequest.CorrelationID); err != nil {
			return err
		}
	}
	for argumentKey := range capabilityRequest.Arguments {
		if err := identifiers.ValidateSafeIdentifier("capability argument name", argumentKey); err != nil {
			return err
		}
	}
	return nil
}

func (approvalDecisionRequest ApprovalDecisionRequest) Validate() error {
	if strings.TrimSpace(approvalDecisionRequest.DecisionNonce) == "" {
		return nil
	}
	return identifiers.ValidateSafeIdentifier("approval decision nonce", approvalDecisionRequest.DecisionNonce)
}

func (connectionKeyRequest ConnectionKeyRequest) Validate() error {
	if err := identifiers.ValidateSafeIdentifier("connection provider", strings.TrimSpace(connectionKeyRequest.Provider)); err != nil {
		return err
	}
	if strings.TrimSpace(connectionKeyRequest.Subject) != "" {
		if err := identifiers.ValidateSafeIdentifier("connection subject", strings.TrimSpace(connectionKeyRequest.Subject)); err != nil {
			return err
		}
	}
	return nil
}

func (pkceStartRequest PKCEStartRequest) Validate() error {
	return ConnectionKeyRequest(pkceStartRequest).Validate()
}

func (pkceCompleteRequest PKCECompleteRequest) Validate() error {
	if err := (ConnectionKeyRequest{Provider: pkceCompleteRequest.Provider, Subject: pkceCompleteRequest.Subject}).Validate(); err != nil {
		return err
	}
	if err := identifiers.ValidateSafeIdentifier("pkce state", strings.TrimSpace(pkceCompleteRequest.State)); err != nil {
		return err
	}
	if err := identifiers.ValidateSafeIdentifier("pkce code", strings.TrimSpace(pkceCompleteRequest.Code)); err != nil {
		return err
	}
	return nil
}

func (siteInspectionRequest SiteInspectionRequest) Validate() error {
	if strings.TrimSpace(siteInspectionRequest.URL) == "" {
		return fmt.Errorf("url is required")
	}
	return nil
}

func (siteTrustDraftRequest SiteTrustDraftRequest) Validate() error {
	if strings.TrimSpace(siteTrustDraftRequest.URL) == "" {
		return fmt.Errorf("url is required")
	}
	return nil
}

func (sandboxImportRequest SandboxImportRequest) Validate() error {
	if strings.TrimSpace(sandboxImportRequest.HostSourcePath) == "" {
		return fmt.Errorf("host_source_path is required")
	}
	if strings.TrimSpace(sandboxImportRequest.DestinationName) == "" {
		return fmt.Errorf("destination_name is required")
	}
	return nil
}

func (sandboxStageRequest SandboxStageRequest) Validate() error {
	if strings.TrimSpace(sandboxStageRequest.SandboxSourcePath) == "" {
		return fmt.Errorf("sandbox_source_path is required")
	}
	if strings.TrimSpace(sandboxStageRequest.OutputName) == "" {
		return fmt.Errorf("output_name is required")
	}
	return nil
}

func (sandboxMetadataRequest SandboxMetadataRequest) Validate() error {
	if strings.TrimSpace(sandboxMetadataRequest.SandboxSourcePath) == "" {
		return fmt.Errorf("sandbox_source_path is required")
	}
	return nil
}

func (sandboxListRequest SandboxListRequest) Validate() error {
	// SandboxPath is optional — empty means list the home root.
	return nil
}

func (sandboxExportRequest SandboxExportRequest) Validate() error {
	if strings.TrimSpace(sandboxExportRequest.SandboxSourcePath) == "" {
		return fmt.Errorf("sandbox_source_path is required")
	}
	if strings.TrimSpace(sandboxExportRequest.HostDestinationPath) == "" {
		return fmt.Errorf("host_destination_path is required")
	}
	return nil
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

const (
	ResultExposureNone    = "none"
	ResultExposureDisplay = "display"
	ResultExposureAudit   = "audit"

	ResultFieldOriginLocal  = "local"
	ResultFieldOriginRemote = "remote"

	ResultFieldTrustDeterministic = "deterministic"

	ResultFieldSensitivityBenign      = "benign"
	ResultFieldSensitivityTaintedText = "tainted_text"

	ResultFieldKindScalar  = "scalar"
	ResultFieldKindObject  = "object"
	ResultFieldKindArray   = "array"
	ResultFieldKindBlobRef = "blob_ref"

	ResultFieldScalarSubclassBoolean          = "boolean"
	ResultFieldScalarSubclassValidatedNumber  = "validated_number"
	ResultFieldScalarSubclassEnum             = "enum"
	ResultFieldScalarSubclassTimestamp        = "timestamp"
	ResultFieldScalarSubclassStrictIdentifier = "strict_identifier"
	ResultFieldScalarSubclassShortTextLabel   = "short_text_label"
)

func (resultClassification ResultClassification) Validate() error {
	switch resultClassification.Exposure {
	case ResultExposureNone, ResultExposureDisplay, ResultExposureAudit:
	default:
		return fmt.Errorf("invalid result exposure %q", resultClassification.Exposure)
	}
	if resultClassification.Exposure == ResultExposureAudit {
		if resultClassification.Eligibility.Prompt || resultClassification.Eligibility.Memory {
			return fmt.Errorf("audit exposure cannot also be prompt- or memory-eligible")
		}
	}
	if resultClassification.Quarantine.Quarantined {
		if resultClassification.Eligibility.Prompt || resultClassification.Eligibility.Memory {
			return fmt.Errorf("quarantined classification cannot be prompt- or memory-eligible")
		}
		if strings.TrimSpace(resultClassification.Quarantine.Ref) == "" {
			return fmt.Errorf("quarantined classification requires quarantine ref")
		}
		return nil
	}
	if strings.TrimSpace(resultClassification.Quarantine.Ref) != "" {
		return fmt.Errorf("non-quarantined classification must not set quarantine ref")
	}
	return nil
}

func (resultClassification ResultClassification) PromptEligible() bool {
	return resultClassification.Eligibility.Prompt
}

func (resultClassification ResultClassification) MemoryEligible() bool {
	return resultClassification.Eligibility.Memory
}

func (resultClassification ResultClassification) DisplayOnly() bool {
	return resultClassification.Exposure == ResultExposureDisplay &&
		!resultClassification.Eligibility.Prompt &&
		!resultClassification.Eligibility.Memory
}

func (resultClassification ResultClassification) AuditOnly() bool {
	return resultClassification.Exposure == ResultExposureAudit
}

func (resultClassification ResultClassification) Quarantined() bool {
	return resultClassification.Quarantine.Quarantined
}

func (resultFieldMetadata ResultFieldMetadata) Validate() error {
	switch resultFieldMetadata.Origin {
	case ResultFieldOriginLocal, ResultFieldOriginRemote:
	default:
		return fmt.Errorf("invalid field origin %q", resultFieldMetadata.Origin)
	}
	if strings.TrimSpace(resultFieldMetadata.ContentType) == "" {
		return fmt.Errorf("field content_type is required")
	}
	switch resultFieldMetadata.Trust {
	case ResultFieldTrustDeterministic:
	default:
		return fmt.Errorf("invalid field trust %q", resultFieldMetadata.Trust)
	}
	switch resultFieldMetadata.Sensitivity {
	case ResultFieldSensitivityBenign, ResultFieldSensitivityTaintedText:
	default:
		return fmt.Errorf("invalid field sensitivity %q", resultFieldMetadata.Sensitivity)
	}
	switch resultFieldMetadata.Kind {
	case ResultFieldKindScalar, ResultFieldKindObject, ResultFieldKindArray, ResultFieldKindBlobRef:
	default:
		return fmt.Errorf("invalid field kind %q", resultFieldMetadata.Kind)
	}
	switch resultFieldMetadata.ScalarSubclass {
	case "",
		ResultFieldScalarSubclassBoolean,
		ResultFieldScalarSubclassValidatedNumber,
		ResultFieldScalarSubclassEnum,
		ResultFieldScalarSubclassTimestamp,
		ResultFieldScalarSubclassStrictIdentifier,
		ResultFieldScalarSubclassShortTextLabel:
	default:
		return fmt.Errorf("invalid field scalar_subclass %q", resultFieldMetadata.ScalarSubclass)
	}
	if resultFieldMetadata.Kind == ResultFieldKindScalar && resultFieldMetadata.ScalarSubclass == "" && (resultFieldMetadata.PromptEligible || resultFieldMetadata.MemoryEligible) {
		return fmt.Errorf("scalar field eligible for prompt or memory requires scalar_subclass")
	}
	if resultFieldMetadata.Kind != ResultFieldKindScalar && resultFieldMetadata.ScalarSubclass != "" {
		return fmt.Errorf("non-scalar field must not set scalar_subclass")
	}
	if resultFieldMetadata.SizeBytes < 0 {
		return fmt.Errorf("field size_bytes must be non-negative")
	}
	return nil
}

func (capabilityResponse CapabilityResponse) ResultClassification() (ResultClassification, error) {
	if err := capabilityResponse.Classification.Validate(); err != nil {
		return ResultClassification{}, err
	}
	if capabilityResponse.Classification.Quarantine.Quarantined && strings.TrimSpace(capabilityResponse.QuarantineRef) == "" {
		return ResultClassification{}, fmt.Errorf("quarantined result is missing quarantine_ref")
	}
	if !capabilityResponse.Classification.Quarantine.Quarantined && strings.TrimSpace(capabilityResponse.QuarantineRef) != "" {
		return ResultClassification{}, fmt.Errorf("non-quarantined result unexpectedly set quarantine_ref")
	}
	if capabilityResponse.Classification.Quarantine.Quarantined &&
		strings.TrimSpace(capabilityResponse.Classification.Quarantine.Ref) != strings.TrimSpace(capabilityResponse.QuarantineRef) {
		return ResultClassification{}, fmt.Errorf("classification quarantine ref does not match response quarantine_ref")
	}
	if err := capabilityResponse.ValidateStructuredResultFields(); err != nil {
		return ResultClassification{}, err
	}
	return capabilityResponse.Classification, nil
}

func (capabilityResponse CapabilityResponse) ValidateStructuredResultFields() error {
	if len(capabilityResponse.StructuredResult) == 0 {
		if len(capabilityResponse.FieldsMeta) != 0 {
			return fmt.Errorf("fields_meta requires structured_result")
		}
		return nil
	}
	if len(capabilityResponse.FieldsMeta) == 0 {
		return fmt.Errorf("structured_result requires fields_meta")
	}
	for fieldName := range capabilityResponse.StructuredResult {
		fieldMetadata, found := capabilityResponse.FieldsMeta[fieldName]
		if !found {
			return fmt.Errorf("structured_result field %q is missing fields_meta", fieldName)
		}
		if err := fieldMetadata.Validate(); err != nil {
			return fmt.Errorf("invalid fields_meta for %q: %w", fieldName, err)
		}
	}
	for fieldName := range capabilityResponse.FieldsMeta {
		if _, found := capabilityResponse.StructuredResult[fieldName]; !found {
			return fmt.Errorf("fields_meta field %q does not exist in structured_result", fieldName)
		}
	}
	return nil
}

// HookPreValidateRequest is the request body for POST /v1/hook/pre-validate.
// It is sent by the Claude Code PreToolUse hook script over the local Unix socket.
// Auth: Unix socket peer UID binding only — no session or MAC required.
type HookPreValidateRequest struct {
	// ToolName is the Claude Code tool name (e.g. "Bash", "Write", "Edit").
	ToolName string `json:"tool_name"`
	// ToolInput is the raw tool input payload, forwarded as-is for audit.
	ToolInput map[string]interface{} `json:"tool_input,omitempty"`
	// SessionID is an optional hint for correlating audit events.
	SessionID string `json:"session_id,omitempty"`
}

// HookPreValidateResponse is the response body for POST /v1/hook/pre-validate.
// The hook script inspects Decision to determine whether to allow or block the tool call.
type HookPreValidateResponse struct {
	// Decision is "allow", "block", or "pending_approval".
	Decision string `json:"decision"`
	// Reason is a human-readable explanation. Present when Decision != "allow".
	Reason string `json:"reason,omitempty"`
	// DenialCode is a machine-readable denial code. Present when Decision == "block".
	DenialCode string `json:"denial_code,omitempty"`
}
