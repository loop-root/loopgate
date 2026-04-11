package loopgate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"morph/internal/config"
	"morph/internal/identifiers"
	modelpkg "morph/internal/model"
	modelruntime "morph/internal/modelruntime"
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
	DenialCodeCapabilityTokenMissing        = "capability_token_missing"
	DenialCodeCapabilityTokenInvalid        = "capability_token_invalid"
	DenialCodeCapabilityTokenExpired        = "capability_token_expired"
	DenialCodeCapabilityTokenScopeDenied    = "capability_token_scope_denied"
	DenialCodeCapabilityTokenReused         = "capability_token_reused"
	DenialCodeCapabilityTokenBindingInvalid = "capability_token_binding_invalid"
	DenialCodeRequestSignatureMissing       = "request_signature_missing"
	DenialCodeRequestSignatureInvalid       = "request_signature_invalid"
	DenialCodeRequestTimestampInvalid       = "request_timestamp_invalid"
	DenialCodeRequestNonceReplayDetected    = "request_nonce_replay_detected"
	DenialCodeControlSessionBindingInvalid  = "control_session_binding_invalid"
	DenialCodeApprovalTokenMissing          = "approval_token_missing"
	DenialCodeApprovalTokenInvalid          = "approval_token_invalid"
	DenialCodeApprovalTokenExpired          = "approval_token_expired"
	DenialCodeApprovalDecisionNonceMissing  = "approval_decision_nonce_missing"
	DenialCodeApprovalDecisionNonceInvalid  = "approval_decision_nonce_invalid"
	DenialCodeApprovalNotFound              = "approval_request_not_found"
	DenialCodeApprovalOwnerMismatch         = "approval_owner_mismatch"
	DenialCodeApprovalRequired              = "approval_required"
	DenialCodeApprovalDenied                = "approval_denied"
	DenialCodeApprovalStateInvalid          = "approval_state_invalid"
	DenialCodeSecretExportProhibited        = "secret_export_prohibited"
	DenialCodeCapabilityScopeRequired       = "capability_scope_required"
	DenialCodeRequestReplayDetected         = "request_replay_detected"
	// DenialCodeReplayStateSaturated is returned when the in-memory replay table cannot accept
	// another entry without evicting (eviction would weaken replay protection). Fail closed.
	DenialCodeReplayStateSaturated = "replay_state_saturated"
	// DenialCodePendingApprovalLimitReached caps pending (undecided) approvals per control session.
	DenialCodePendingApprovalLimitReached = "pending_approval_limit_reached"
	// DenialCodeControlPlaneStateSaturated is returned when an in-memory control-plane map
	// (sessions, approvals, worker sessions) cannot accept new entries (fail closed).
	DenialCodeControlPlaneStateSaturated        = "control_plane_state_saturated"
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
	SubmitHavenContinuityInspectionForThread(ctx context.Context, threadID string) (HavenContinuityInspectThreadResponse, error)
	LoadMemoryWakeState(ctx context.Context) (MemoryWakeStateResponse, error)
	LoadMemoryDiagnosticWake(ctx context.Context) (MemoryDiagnosticWakeResponse, error)
	LoadHavenMemoryInventory(ctx context.Context) (HavenMemoryInventoryResponse, error)
	ResetHavenMemory(ctx context.Context, request HavenMemoryResetRequest) (HavenMemoryResetResponse, error)
	DiscoverMemory(ctx context.Context, request MemoryDiscoverRequest) (MemoryDiscoverResponse, error)
	LookupMemoryArtifacts(ctx context.Context, request MemoryArtifactLookupRequest) (MemoryArtifactLookupResponse, error)
	RecallMemory(ctx context.Context, request MemoryRecallRequest) (MemoryRecallResponse, error)
	GetMemoryArtifacts(ctx context.Context, request MemoryArtifactGetRequest) (MemoryArtifactGetResponse, error)
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
	Version             string              `json:"version"`
	Policy              config.Policy       `json:"policy"`
	Capabilities        []CapabilitySummary `json:"capabilities"`
	ControlCapabilities []CapabilitySummary `json:"control_capabilities,omitempty"`
	PendingApprovals    int                 `json:"pending_approvals"`
	ActiveMorphlings    int                 `json:"active_morphlings"`
	Connections         []ConnectionStatus  `json:"connections"`
}

type ConnectionsStatusResponse struct {
	Connections []ConnectionStatus `json:"connections"`
}

type OpenSessionRequest struct {
	Actor                 string   `json:"actor"`
	SessionID             string   `json:"session_id"`
	RequestedCapabilities []string `json:"requested_capabilities"`
	CorrelationID         string   `json:"correlation_id"`
	// WorkspaceID is a compatibility hint for multi-surface clients. Loopgate derives the
	// authoritative workspace binding from repoRoot at session open and rejects mismatches.
	WorkspaceID string `json:"workspace_id,omitempty"`
	// OperatorMountPaths binds Haven-granted host directories to this control session.
	// Loopgate canonicalizes and rejects unsafe paths, and only accepts them when the
	// server is pinning the expected Haven executable for session open.
	OperatorMountPaths []string `json:"operator_mount_paths,omitempty"`
	// PrimaryOperatorMountPath selects the default repo root for relative
	// operator_mount.fs_* paths. It must match one of OperatorMountPaths after
	// Loopgate canonicalization; it never widens scope and is accepted only with
	// the same pinned-client operator mount binding.
	PrimaryOperatorMountPath string `json:"primary_operator_mount_path,omitempty"`
}

type OpenSessionResponse struct {
	ControlSessionID string `json:"control_session_id"`
	CapabilityToken  string `json:"capability_token"`
	ApprovalToken    string `json:"approval_token"`
	SessionMACKey    string `json:"session_mac_key"`
	ExpiresAtUTC     string `json:"expires_at_utc"`
}

// SessionMACKeySlotInfo is one epoch slot in GET /v1/session/mac-keys.
type SessionMACKeySlotInfo struct {
	Slot                 string `json:"slot"`
	EpochIndex           int64  `json:"epoch_index"`
	ValidFromUTC         string `json:"valid_from_utc"`
	ValidUntilUTC        string `json:"valid_until_utc"`
	EpochKeyMaterialHex  string `json:"epoch_key_material_hex"`
	DerivedSessionMACKey string `json:"derived_session_mac_key"`
}

// SessionMACKeysResponse is the JSON body for GET /v1/session/mac-keys.
type SessionMACKeysResponse struct {
	SchemaVersion         string                `json:"schema_version"`
	RotationPeriodSeconds int64                 `json:"rotation_period_seconds"`
	DerivedKeySchema      string                `json:"derived_key_schema"`
	ControlSessionID      string                `json:"control_session_id"`
	CurrentEpochIndex     int64                 `json:"current_epoch_index"`
	Previous              SessionMACKeySlotInfo `json:"previous"`
	Current               SessionMACKeySlotInfo `json:"current"`
	Next                  SessionMACKeySlotInfo `json:"next"`
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
