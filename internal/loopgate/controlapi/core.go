package controlapi

import (
	"fmt"
	"strings"

	"loopgate/internal/config"
	"loopgate/internal/identifiers"
	protocolpkg "loopgate/internal/loopgate/protocol"
)

const (
	ResponseStatusSuccess         = "success"
	ResponseStatusDenied          = "denied"
	ResponseStatusError           = "error"
	ResponseStatusPendingApproval = "pending_approval"
)

const (
	HookReasonCodePolicyAllowed           = "policy_allowed"
	HookReasonCodeOperatorOverrideAllowed = "operator_override_allowed"
	HookReasonCodeApprovalRequired        = "approval_required"
	HookReasonCodePolicyDenied            = "policy_denied"

	HookApprovalOwnerHarness     = "harness"
	HookApprovalOptionOnce       = "once"
	HookApprovalOptionSession    = "session"
	HookApprovalOptionPermanent  = "permanent"
	HookApprovalOptionPersistent = HookApprovalOptionPermanent
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
	DenialCodeControlPlaneStateSaturated    = "control_plane_state_saturated"
	DenialCodeUnknownCapability             = "unknown_capability"
	DenialCodeInvalidCapabilityArguments    = "invalid_capability_arguments"
	DenialCodePolicyDenied                  = "policy_denied"
	DenialCodeApprovalCreationFailed        = "approval_creation_failed"
	DenialCodeExecutionFailed               = "capability_execution_failed"
	DenialCodeMalformedRequest              = "malformed_request"
	DenialCodeAuditUnavailable              = "audit_unavailable"
	DenialCodeSourceBytesUnavailable        = "source_bytes_unavailable"
	DenialCodeQuarantinePruneNotEligible    = "quarantine_prune_not_eligible"
	DenialCodeSiteURLInvalid                = "site_url_invalid"
	DenialCodeHTTPSRequired                 = "https_required"
	DenialCodeSiteTrustDraftNotAvailable    = "site_trust_draft_not_available"
	DenialCodeSiteTrustDraftExists          = "site_trust_draft_exists"
	DenialCodeSiteInspectionUnsupportedType = "site_inspection_unsupported_content_type"
	DenialCodeSandboxPathInvalid            = "sandbox_path_invalid"
	DenialCodeSandboxSourceUnavailable      = "sandbox_source_unavailable"
	DenialCodeSandboxDestinationExists      = "sandbox_destination_exists"
	DenialCodeSandboxHostDestinationInvalid = "sandbox_host_destination_invalid"
	DenialCodeSandboxSymlinkNotAllowed      = "sandbox_symlink_not_allowed"
	DenialCodeSandboxArtifactNotStaged      = "sandbox_artifact_not_staged"
	DenialCodeSessionOpenRateLimited        = "session_open_rate_limited"
	DenialCodeSessionActiveLimitReached     = "session_active_limit_reached"
	DenialCodeSessionCloseBlocked           = "session_close_blocked"
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

	DenialCodeProcessBindingRejected      = "process_binding_rejected"
	DenialCodeFsReadRateLimitExceeded     = "fs_read_rate_limit_exceeded"
	DenialCodeFsReadSizeLimitExceeded     = "fs_read_size_limit_exceeded"
	DenialCodeMCPGatewayServerNotFound    = "mcp_gateway_server_not_found"
	DenialCodeMCPGatewayServerDisabled    = "mcp_gateway_server_disabled"
	DenialCodeMCPGatewayServerNotLaunched = "mcp_gateway_server_not_launched"
	DenialCodeMCPGatewayToolNotFound      = "mcp_gateway_tool_not_found"
	DenialCodeMCPGatewayToolDisabled      = "mcp_gateway_tool_disabled"
	DenialCodeMCPGatewayArgumentsInvalid  = "mcp_gateway_arguments_invalid"
	DenialCodeMCPGatewayApprovalLimit     = "mcp_gateway_approval_limit_reached"

	// Hook denial codes.
	DenialCodeHookPeerBindingRejected = "hook_peer_binding_rejected"
	DenialCodeHookUnknownTool         = "hook_unknown_tool"
	DenialCodeHookUnknownEvent        = "hook_unknown_event"
	DenialCodeHookEventUnimplemented  = "hook_event_unimplemented"
	DenialCodeHookRateLimitExceeded   = "hook_rate_limit_exceeded"
)

type CapabilitySummary struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Operation   string `json:"operation"`
	Description string `json:"description"`
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
	Connections         []ConnectionStatus  `json:"connections"`
}

type ConfigPolicyReloadResponse struct {
	Status               string `json:"status"`
	PreviousPolicySHA256 string `json:"previous_policy_sha256"`
	PolicySHA256         string `json:"policy_sha256"`
	PolicyChanged        bool   `json:"policy_changed"`
}

type ConfigOperatorOverrideReloadResponse struct {
	Status                         string `json:"status"`
	PreviousOperatorOverrideSHA256 string `json:"previous_operator_override_sha256"`
	OperatorOverrideSHA256         string `json:"operator_override_sha256"`
	OperatorOverrideChanged        bool   `json:"operator_override_changed"`
	OperatorOverridePresent        bool   `json:"operator_override_present"`
	ActiveGrantCount               int    `json:"active_grant_count"`
}

type OpenSessionRequest struct {
	Actor                 string   `json:"actor"`
	SessionID             string   `json:"session_id"`
	RequestedCapabilities []string `json:"requested_capabilities"`
	CorrelationID         string   `json:"correlation_id"`
	// WorkspaceID is a compatibility hint for multi-surface clients. Loopgate derives the
	// authoritative workspace binding from repoRoot at session open and rejects mismatches.
	WorkspaceID string `json:"workspace_id,omitempty"`
	// OperatorMountPaths binds operator-granted host directories to this control session.
	// Loopgate canonicalizes and rejects unsafe paths, and only accepts them when the
	// server is pinning the expected operator executable for session open.
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

type CloseSessionResponse struct {
	Status           string `json:"status"`
	ControlSessionID string `json:"control_session_id"`
	ClosedAtUTC      string `json:"closed_at_utc"`
}

// SessionMACKeySlotInfo is one epoch slot in GET /v1/session/mac-keys.
type SessionMACKeySlotInfo struct {
	Slot                 string `json:"slot"`
	EpochIndex           int64  `json:"epoch_index"`
	ValidFromUTC         string `json:"valid_from_utc"`
	ValidUntilUTC        string `json:"valid_until_utc"`
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

type CapabilityRequest = protocolpkg.CapabilityRequest

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

type OperatorApprovalSummary struct {
	UIApprovalSummary
	DecisionNonce          string `json:"decision_nonce"`
	ApprovalManifestSHA256 string `json:"approval_manifest_sha256,omitempty"`
}

type OperatorApprovalsResponse struct {
	Approvals []OperatorApprovalSummary `json:"approvals"`
}

type OperatorApprovalDecisionResponse struct {
	CapabilityResponse
	AuditEventHash string `json:"audit_event_hash,omitempty"`
}

type ResultClassification struct {
	Exposure    string            `json:"exposure"`
	Eligibility ResultEligibility `json:"eligibility"`
	Quarantine  ResultQuarantine  `json:"quarantine"`
}

type ResultEligibility struct {
	Prompt bool `json:"prompt"`
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
}

type ApprovalDecisionRequest = protocolpkg.ApprovalDecisionRequest

func ValidateGrantType(rawGrantType string) error {
	normalizedGrantType := strings.TrimSpace(rawGrantType)
	switch normalizedGrantType {
	case GrantTypePublicRead, GrantTypePKCE, GrantTypeAuthorizationCode, GrantTypeClientCredentials:
		return nil
	default:
		return fmt.Errorf("unsupported grant type %q", rawGrantType)
	}
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
		if resultClassification.Eligibility.Prompt {
			return fmt.Errorf("audit exposure cannot also be prompt-eligible")
		}
	}
	if resultClassification.Quarantine.Quarantined {
		if resultClassification.Eligibility.Prompt {
			return fmt.Errorf("quarantined classification cannot also be prompt-eligible")
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

func (resultClassification ResultClassification) DisplayOnly() bool {
	return resultClassification.Exposure == ResultExposureDisplay &&
		!resultClassification.Eligibility.Prompt
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
	if resultFieldMetadata.Kind == ResultFieldKindScalar && resultFieldMetadata.ScalarSubclass == "" && resultFieldMetadata.PromptEligible {
		return fmt.Errorf("prompt-eligible scalar field requires scalar_subclass")
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
// It is sent by Claude Code hook scripts over the local Unix socket.
// Auth: Unix socket peer UID binding only — no session or MAC required.
type HookPreValidateRequest struct {
	// HookEventName is the Claude Code hook event name.
	HookEventName string `json:"hook_event_name,omitempty"`
	// ToolName is the Claude Code tool name (e.g. "Bash", "Write", "Edit").
	ToolName string `json:"tool_name"`
	// ToolUseID is Claude Code's stable tool invocation identifier when provided by the hook event.
	ToolUseID string `json:"tool_use_id,omitempty"`
	// ToolInput is the raw tool input payload, forwarded as-is for audit.
	ToolInput map[string]interface{} `json:"tool_input,omitempty"`
	// Prompt is the submitted user prompt for UserPromptSubmit.
	Prompt string `json:"prompt,omitempty"`
	// HookReason is Claude's lifecycle reason for events like SessionEnd.
	HookReason string `json:"reason,omitempty"`
	// HookError is Claude's tool failure error text for PostToolUseFailure.
	HookError string `json:"error,omitempty"`
	// HookInterrupted reports whether PostToolUseFailure was caused by an interrupt.
	HookInterrupted bool `json:"is_interrupt,omitempty"`
	// CWD is Claude Code's working directory for the hook event.
	CWD string `json:"cwd,omitempty"`
	// SessionID is an optional hint for correlating audit events.
	SessionID string `json:"session_id,omitempty"`
}

// HookPreValidateResponse is the response body for POST /v1/hook/pre-validate.
// The hook script inspects Decision to determine whether to allow or block the tool call.
type HookPreValidateResponse struct {
	// Decision is "allow", "block", or "ask".
	Decision string `json:"decision"`
	// Reason is a human-readable explanation. Present when Decision != "allow".
	Reason string `json:"reason,omitempty"`
	// ReasonCode is a stable machine-readable explanation for the decision.
	ReasonCode string `json:"reason_code,omitempty"`
	// DenialCode is a machine-readable denial code. Present when Decision == "block".
	DenialCode string `json:"denial_code,omitempty"`
	// AdditionalContext is bounded historical context for events like SessionStart.
	AdditionalContext string `json:"additional_context,omitempty"`
	// ApprovalRequestID is reserved for Loopgate-owned approval records. Claude
	// hook asks are currently harness-owned, so this remains empty for ask
	// decisions.
	ApprovalRequestID string `json:"approval_request_id,omitempty"`
	// ApprovalOwner names who should prompt the operator when Decision == "ask".
	ApprovalOwner string `json:"approval_owner,omitempty"`
	// ApprovalOptions lists the approval scopes the harness may offer without
	// exceeding the root policy ceiling.
	ApprovalOptions []string `json:"approval_options,omitempty"`
	// OperatorOverrideClass is the parent-policy action class associated with
	// this Claude tool, when one exists.
	OperatorOverrideClass string `json:"operator_override_class,omitempty"`
	// OperatorOverrideMaxDelegation describes whether the parent policy would
	// allow future operator-created exceptions for this action class. It is
	// metadata only and does not itself grant permission.
	OperatorOverrideMaxDelegation string `json:"operator_override_max_delegation,omitempty"`
	// OperatorOverrideMaxGrantScope is the operator-facing label for the same
	// policy ceiling exposed by OperatorOverrideMaxDelegation.
	OperatorOverrideMaxGrantScope string `json:"operator_override_max_grant_scope,omitempty"`
}
