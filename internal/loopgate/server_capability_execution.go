package loopgate

import (
	"context"
	"fmt"
	"strings"
	"time"

	controlapipkg "loopgate/internal/loopgate/controlapi"
	policypkg "loopgate/internal/policy"
	"loopgate/internal/secrets"
	toolspkg "loopgate/internal/tools"
)

func (server *Server) executeCapabilityRequest(ctx context.Context, tokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest, allowApprovalCreation bool) controlapipkg.CapabilityResponse {
	normalizedRequest, earlyResponse := server.prepareCapabilityRequestExecution(tokenClaims, capabilityRequest, allowApprovalCreation)
	if earlyResponse != nil {
		return *earlyResponse
	}
	capabilityRequest = normalizedRequest

	policyRuntime := server.currentPolicyRuntime()
	tool, earlyResponse := server.resolveCapabilityExecutionTool(policyRuntime, tokenClaims, capabilityRequest)
	if earlyResponse != nil {
		return *earlyResponse
	}

	originalPolicyDecision, policyDecision, lowRiskHostPlanAutoAllowed := server.evaluateCapabilityPolicyDecision(policyRuntime, tool, tokenClaims, capabilityRequest)
	if originalPolicyDecision.Decision == policypkg.NeedsApproval && policyDecision.Decision == policypkg.Allow && lowRiskHostPlanAutoAllowed {
		if err := server.logEvent("capability.low_risk_host_plan_auto_allow", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
		}); err != nil {
			return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
		}
	}
	if err := server.logEvent("capability.requested", tokenClaims.ControlSessionID, map[string]interface{}{
		"request_id":           capabilityRequest.RequestID,
		"capability":           capabilityRequest.Capability,
		"decision":             policyDecision.Decision.String(),
		"reason":               secrets.RedactText(policyDecision.Reason),
		"actor_label":          tokenClaims.ActorLabel,
		"client_session_label": tokenClaims.ClientSessionLabel,
		"control_session_id":   tokenClaims.ControlSessionID,
	}); err != nil {
		return controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "control-plane audit is unavailable",
			DenialCode:   controlapipkg.DenialCodeAuditUnavailable,
		}
	}

	if policyDecision.Decision == policypkg.Deny {
		if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"reason":               secrets.RedactText(policyDecision.Reason),
			"denial_code":          controlapipkg.DenialCodePolicyDenied,
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
		}); err != nil {
			return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
		}
		deniedResponse := controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: policyDecision.Reason,
			DenialCode:   controlapipkg.DenialCodePolicyDenied,
		}
		server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, deniedResponse.DenialCode, deniedResponse.DenialReason)
		return deniedResponse
	}

	if policyDecision.Decision == policypkg.NeedsApproval && allowApprovalCreation {
		return server.createCapabilityApprovalResponse(tokenClaims, capabilityRequest, policyDecision)
	}
	if policyDecision.Decision == policypkg.NeedsApproval && !allowApprovalCreation && !tokenClaims.ApprovedExecution {
		if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"reason":               "capability requires approval and this route does not support approval creation",
			"denial_code":          controlapipkg.DenialCodeApprovalRequired,
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
		}); err != nil {
			return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
		}
		deniedResponse := controlapipkg.CapabilityResponse{
			RequestID:        capabilityRequest.RequestID,
			Status:           controlapipkg.ResponseStatusDenied,
			DenialReason:     "capability requires approval and this route does not support approval creation",
			DenialCode:       controlapipkg.DenialCodeApprovalRequired,
			ApprovalRequired: true,
		}
		server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, deniedResponse.DenialCode, deniedResponse.DenialReason)
		return deniedResponse
	}

	effectiveTokenClaims, earlyResponse := server.prepareCapabilityExecution(tokenClaims, capabilityRequest, policyDecision, tool)
	if earlyResponse != nil {
		return *earlyResponse
	}

	if specialResponse, handled := server.dispatchDirectCapabilityExecution(effectiveTokenClaims, capabilityRequest); handled {
		return specialResponse
	}

	output, earlyResponse := server.executeCapabilityTool(ctx, tool, effectiveTokenClaims, capabilityRequest)
	if earlyResponse != nil {
		return *earlyResponse
	}

	return server.finalizeCapabilityExecution(effectiveTokenClaims, capabilityRequest, output)
}

func (server *Server) prepareCapabilityRequestExecution(tokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest, allowApprovalCreation bool) (controlapipkg.CapabilityRequest, *controlapipkg.CapabilityResponse) {
	if strings.TrimSpace(capabilityRequest.RequestID) == "" {
		requestID, err := randomHex(8)
		if err != nil {
			return capabilityRequest, &controlapipkg.CapabilityResponse{
				Status:       controlapipkg.ResponseStatusError,
				DenialReason: "allocate request_id: " + err.Error(),
				DenialCode:   controlapipkg.DenialCodeExecutionFailed,
			}
		}
		capabilityRequest.RequestID = "req_" + requestID
	}
	if capabilityRequest.Arguments == nil {
		capabilityRequest.Arguments = make(map[string]string)
	}
	capabilityRequest = normalizeCapabilityRequest(capabilityRequest)
	capabilityRequest.Actor = tokenClaims.ActorLabel
	capabilityRequest.SessionID = tokenClaims.ControlSessionID
	if err := capabilityRequest.Validate(); err != nil {
		return capabilityRequest, &controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   controlapipkg.DenialCodeMalformedRequest,
		}
	}
	if allowApprovalCreation {
		if replayDenied := server.recordRequest(tokenClaims.ControlSessionID, capabilityRequest); replayDenied != nil {
			return capabilityRequest, replayDenied
		}
	}
	return capabilityRequest, nil
}

func (server *Server) resolveCapabilityExecutionTool(policyRuntime serverPolicyRuntime, tokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest) (toolspkg.Tool, *controlapipkg.CapabilityResponse) {
	tool := policyRuntime.registry.Get(capabilityRequest.Capability)
	if server.capabilityProhibitsRawSecretExport(tool, capabilityRequest.Capability) {
		if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"reason":               "raw secret export is prohibited",
			"denial_code":          controlapipkg.DenialCodeSecretExportProhibited,
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
		}); err != nil {
			auditUnavailable := auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
			return nil, &auditUnavailable
		}
		deniedResponse := controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "raw secret export is prohibited",
			DenialCode:   controlapipkg.DenialCodeSecretExportProhibited,
			Redacted:     true,
		}
		server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, deniedResponse.DenialCode, deniedResponse.DenialReason)
		return nil, &deniedResponse
	}
	if len(tokenClaims.AllowedCapabilities) > 0 {
		if _, allowed := tokenClaims.AllowedCapabilities[capabilityRequest.Capability]; !allowed {
			if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
				"request_id":           capabilityRequest.RequestID,
				"capability":           capabilityRequest.Capability,
				"reason":               "capability token scope denied requested capability",
				"denial_code":          controlapipkg.DenialCodeCapabilityTokenScopeDenied,
				"actor_label":          tokenClaims.ActorLabel,
				"client_session_label": tokenClaims.ClientSessionLabel,
				"control_session_id":   tokenClaims.ControlSessionID,
			}); err != nil {
				auditUnavailable := auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
				return nil, &auditUnavailable
			}
			deniedResponse := controlapipkg.CapabilityResponse{
				RequestID:    capabilityRequest.RequestID,
				Status:       controlapipkg.ResponseStatusDenied,
				DenialReason: "capability token scope denied requested capability",
				DenialCode:   controlapipkg.DenialCodeCapabilityTokenScopeDenied,
			}
			server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, deniedResponse.DenialCode, deniedResponse.DenialReason)
			return nil, &deniedResponse
		}
	}
	if tool == nil {
		if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"reason":               "unknown capability",
			"denial_code":          controlapipkg.DenialCodeUnknownCapability,
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
		}); err != nil {
			auditUnavailable := auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
			return nil, &auditUnavailable
		}
		deniedResponse := controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "unknown capability",
			DenialCode:   controlapipkg.DenialCodeUnknownCapability,
		}
		server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, deniedResponse.DenialCode, deniedResponse.DenialReason)
		return nil, &deniedResponse
	}
	if err := tool.Schema().Validate(capabilityRequest.Arguments); err != nil {
		if auditErr := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"reason":               secrets.RedactText(err.Error()),
			"denial_code":          controlapipkg.DenialCodeInvalidCapabilityArguments,
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
		}); auditErr != nil {
			auditUnavailable := auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
			return nil, &auditUnavailable
		}
		errorResponse := controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   controlapipkg.DenialCodeInvalidCapabilityArguments,
			Redacted:     true,
		}
		server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, errorResponse.DenialCode, errorResponse.DenialReason)
		return nil, &errorResponse
	}
	return tool, nil
}

func (server *Server) evaluateCapabilityPolicyDecision(policyRuntime serverPolicyRuntime, tool toolspkg.Tool, tokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest) (policypkg.CheckResult, policypkg.CheckResult, bool) {
	policyDecision := policyRuntime.checker.Check(tool)
	if argumentValidator, ok := tool.(toolspkg.PolicyArgumentValidator); ok && policyDecision.Decision != policypkg.Deny {
		if err := argumentValidator.ValidatePolicyArguments(capabilityRequest.Arguments); err != nil {
			policyDecision = policypkg.CheckResult{
				Decision: policypkg.Deny,
				Reason:   err.Error(),
			}
		}
	}
	originalPolicyDecision := policyDecision
	lowRiskHostPlanAutoAllowed := false
	if operatorMountGrant, granted, grantErr := operatorMountWriteGrantForRequest(server, tokenClaims.ControlSessionID, capabilityRequest); grantErr == nil && granted {
		policyDecision = policypkg.CheckResult{
			Decision: policypkg.Allow,
			Reason:   "active operator-mounted write grant for " + operatorMountGrant.root,
		}
	}
	if adjustedDecision, adjusted := server.autoAllowLowRiskHostPlanApply(tokenClaims.ControlSessionID, capabilityRequest, policyDecision); adjusted {
		policyDecision = adjustedDecision
		lowRiskHostPlanAutoAllowed = true
	}
	return originalPolicyDecision, policyDecision, lowRiskHostPlanAutoAllowed
}

func (server *Server) createCapabilityApprovalResponse(tokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest, policyDecision policypkg.CheckResult) controlapipkg.CapabilityResponse {
	approvalID, err := randomHex(8)
	if err != nil {
		return controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "failed to create approval request",
			DenialCode:   controlapipkg.DenialCodeApprovalCreationFailed,
		}
	}

	decisionNonce, err := randomHex(16)
	if err != nil {
		return controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "failed to create approval decision nonce",
			DenialCode:   controlapipkg.DenialCodeApprovalCreationFailed,
		}
	}

	metadata := server.approvalMetadata(tokenClaims.ControlSessionID, capabilityRequest)
	approvalReason := approvalReasonForCapability(policyDecision, metadata, capabilityRequest)
	expiresAt := server.now().UTC().Add(approvalTTL)
	manifestSHA256, bodySHA256, manifestErr := buildCapabilityApprovalManifest(capabilityRequest, expiresAt.UnixMilli())
	if manifestErr != nil {
		return controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "failed to compute approval manifest",
			DenialCode:   controlapipkg.DenialCodeApprovalCreationFailed,
		}
	}
	server.mu.Lock()
	server.pruneExpiredLocked()
	if len(server.approvalState.records) >= server.maxTotalApprovalRecords {
		server.mu.Unlock()
		if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"reason":               "control-plane approval store is at capacity",
			"denial_code":          controlapipkg.DenialCodeControlPlaneStateSaturated,
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
		}); err != nil {
			return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
		}
		deniedResponse := controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "control-plane approval store is at capacity",
			DenialCode:   controlapipkg.DenialCodeControlPlaneStateSaturated,
		}
		server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, deniedResponse.DenialCode, deniedResponse.DenialReason)
		return deniedResponse
	}
	if server.countPendingApprovalsForSessionLocked(tokenClaims.ControlSessionID) >= server.maxPendingApprovalsPerControlSession {
		server.mu.Unlock()
		if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"reason":               "pending approval limit reached for control session",
			"denial_code":          controlapipkg.DenialCodePendingApprovalLimitReached,
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
		}); err != nil {
			return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
		}
		deniedResponse := controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "pending approval limit reached for control session",
			DenialCode:   controlapipkg.DenialCodePendingApprovalLimitReached,
		}
		server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, deniedResponse.DenialCode, deniedResponse.DenialReason)
		return deniedResponse
	}
	createdApproval := pendingApproval{
		ID:               approvalID,
		Request:          cloneCapabilityRequest(capabilityRequest),
		CreatedAt:        server.now().UTC(),
		ExpiresAt:        expiresAt,
		Metadata:         metadata,
		Reason:           approvalReason,
		ControlSessionID: tokenClaims.ControlSessionID,
		DecisionNonce:    decisionNonce,
		ExecutionContext: approvalExecutionContext{
			ControlSessionID:    tokenClaims.ControlSessionID,
			ActorLabel:          tokenClaims.ActorLabel,
			ClientSessionLabel:  tokenClaims.ClientSessionLabel,
			AllowedCapabilities: copyCapabilitySet(tokenClaims.AllowedCapabilities),
			TenantID:            tokenClaims.TenantID,
			UserID:              tokenClaims.UserID,
		},
		State:                  approvalStatePending,
		ApprovalManifestSHA256: manifestSHA256,
		ExecutionBodySHA256:    bodySHA256,
	}
	server.approvalState.records[approvalID] = createdApproval
	server.noteExpiryCandidateLocked(expiresAt)

	approvalCreatedAuditData := map[string]interface{}{
		"request_id":               capabilityRequest.RequestID,
		"approval_request_id":      approvalID,
		"capability":               capabilityRequest.Capability,
		"approval_class":           metadata["approval_class"],
		"approval_state":           approvalStatePending,
		"actor_label":              tokenClaims.ActorLabel,
		"client_session_label":     tokenClaims.ClientSessionLabel,
		"control_session_id":       tokenClaims.ControlSessionID,
		"approval_manifest_sha256": manifestSHA256,
		"tenant_id":                tokenClaims.TenantID,
		"user_id":                  tokenClaims.UserID,
	}
	if approvalClass, ok := metadata["approval_class"].(string); ok && strings.TrimSpace(approvalClass) != "" {
		approvalCreatedAuditData["approval_class"] = approvalClass
	}
	if err := server.logEvent("approval.created", tokenClaims.ControlSessionID, approvalCreatedAuditData); err != nil {
		delete(server.approvalState.records, approvalID)
		server.mu.Unlock()
		return controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "control-plane audit is unavailable",
			DenialCode:   controlapipkg.DenialCodeAuditUnavailable,
		}
	}
	server.mu.Unlock()

	metadata["approval_reason"] = approvalReason
	metadata["approval_expires_at_utc"] = expiresAt.Format(time.RFC3339Nano)
	metadata["approval_decision_nonce"] = decisionNonce
	metadata["approval_manifest_sha256"] = manifestSHA256
	pendingResponse := controlapipkg.CapabilityResponse{
		RequestID:              capabilityRequest.RequestID,
		Status:                 controlapipkg.ResponseStatusPendingApproval,
		DenialCode:             controlapipkg.DenialCodeApprovalRequired,
		ApprovalRequired:       true,
		ApprovalRequestID:      approvalID,
		ApprovalManifestSHA256: manifestSHA256,
		Metadata:               metadata,
	}
	createdApproval.Metadata = metadata
	createdApproval.Reason = approvalReason
	server.emitUIApprovalPending(createdApproval)
	return pendingResponse
}

func (server *Server) prepareCapabilityExecution(tokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest, policyDecision policypkg.CheckResult, tool toolspkg.Tool) (capabilityToken, *controlapipkg.CapabilityResponse) {
	effectiveTokenClaims := tokenClaims
	if isHighRiskCapability(tool, policyDecision) && !tokenClaims.SingleUse {
		effectiveTokenClaims = deriveExecutionToken(tokenClaims, capabilityRequest)
	}
	if denialResponse, denied := server.consumeExecutionToken(effectiveTokenClaims, capabilityRequest); denied {
		if err := server.logEvent("capability.denied", effectiveTokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"reason":               secrets.RedactText(denialResponse.DenialReason),
			"denial_code":          denialResponse.DenialCode,
			"actor_label":          effectiveTokenClaims.ActorLabel,
			"client_session_label": effectiveTokenClaims.ClientSessionLabel,
			"control_session_id":   effectiveTokenClaims.ControlSessionID,
			"token_id":             effectiveTokenClaims.TokenID,
			"parent_token_id":      effectiveTokenClaims.ParentTokenID,
		}); err != nil {
			auditUnavailable := auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
			return effectiveTokenClaims, &auditUnavailable
		}
		server.emitUIToolDenied(effectiveTokenClaims.ControlSessionID, capabilityRequest, denialResponse.DenialCode, denialResponse.DenialReason)
		return effectiveTokenClaims, &denialResponse
	}
	if capabilityRequest.Capability == "fs_read" || capabilityRequest.Capability == "operator_mount.fs_read" {
		if denied := server.checkFsReadRateLimit(effectiveTokenClaims.ControlSessionID); denied {
			if auditErr := server.logEvent("capability.denied", effectiveTokenClaims.ControlSessionID, map[string]interface{}{
				"request_id":           capabilityRequest.RequestID,
				"capability":           capabilityRequest.Capability,
				"reason":               "fs_read rate limit exceeded",
				"denial_code":          controlapipkg.DenialCodeFsReadRateLimitExceeded,
				"actor_label":          effectiveTokenClaims.ActorLabel,
				"client_session_label": effectiveTokenClaims.ClientSessionLabel,
				"control_session_id":   effectiveTokenClaims.ControlSessionID,
			}); auditErr != nil {
				auditUnavailable := auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
				return effectiveTokenClaims, &auditUnavailable
			}
			deniedResponse := controlapipkg.CapabilityResponse{
				RequestID:    capabilityRequest.RequestID,
				Status:       controlapipkg.ResponseStatusDenied,
				DenialReason: "fs_read rate limit exceeded",
				DenialCode:   controlapipkg.DenialCodeFsReadRateLimitExceeded,
			}
			return effectiveTokenClaims, &deniedResponse
		}
	}
	return effectiveTokenClaims, nil
}

func (server *Server) dispatchDirectCapabilityExecution(effectiveTokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest) (controlapipkg.CapabilityResponse, bool) {
	switch capabilityRequest.Capability {
	case "host.folder.list":
		return server.executeHostFolderListCapability(effectiveTokenClaims, capabilityRequest), true
	case "host.folder.read":
		return server.executeHostFolderReadCapability(effectiveTokenClaims, capabilityRequest), true
	case "host.organize.plan":
		return server.executeHostOrganizePlanCapability(effectiveTokenClaims, capabilityRequest), true
	case "host.plan.apply":
		return server.executeHostPlanApplyCapability(effectiveTokenClaims, capabilityRequest), true
	default:
		return controlapipkg.CapabilityResponse{}, false
	}
}

func (server *Server) executeCapabilityTool(ctx context.Context, tool toolspkg.Tool, effectiveTokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest) (string, *controlapipkg.CapabilityResponse) {
	executionContext := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		executionContext, cancel = context.WithTimeout(ctx, defaultCapabilityExecutionTimeout)
		defer cancel()
	}
	executionContext = withOperatorMountControlSession(executionContext, effectiveTokenClaims.ControlSessionID)

	output, err := tool.Execute(executionContext, capabilityRequest.Arguments)
	if err != nil {
		if auditErr := server.logEvent("capability.error", effectiveTokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"error":                secrets.RedactText(err.Error()),
			"operator_error_class": secrets.LoopgateOperatorErrorClass(err),
			"actor_label":          effectiveTokenClaims.ActorLabel,
			"client_session_label": effectiveTokenClaims.ClientSessionLabel,
			"control_session_id":   effectiveTokenClaims.ControlSessionID,
			"token_id":             effectiveTokenClaims.TokenID,
			"parent_token_id":      effectiveTokenClaims.ParentTokenID,
		}); auditErr != nil {
			auditUnavailable := auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
			return "", &auditUnavailable
		}
		errorResponse := controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   controlapipkg.DenialCodeExecutionFailed,
			Redacted:     true,
		}
		server.emitUIEvent(effectiveTokenClaims.ControlSessionID, controlapipkg.UIEventTypeWarning, controlapipkg.UIEventWarning{
			Message: "capability execution failed: " + secrets.RedactText(err.Error()),
		})
		return "", &errorResponse
	}
	return output, nil
}

func (server *Server) finalizeCapabilityExecution(effectiveTokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest, output string) controlapipkg.CapabilityResponse {
	var (
		quarantineRef string
		err           error
	)
	if _, configuredCapability := server.configuredCapabilitySnapshot(capabilityRequest.Capability); configuredCapability {
		quarantineRef, err = server.storeQuarantinedPayload(capabilityRequest, output)
		if err != nil {
			return server.capabilityQuarantinePersistenceFailureResponse(effectiveTokenClaims, capabilityRequest, err)
		}
	}

	structuredResult, fieldsMeta, classification, builtQuarantineRef, err := server.buildCapabilityResult(capabilityRequest, output, quarantineRef)
	if err != nil {
		return controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   controlapipkg.DenialCodeExecutionFailed,
			Redacted:     true,
		}
	}
	if quarantineRef == "" && classification.Quarantined() {
		quarantineRef, err = server.storeQuarantinedPayload(capabilityRequest, output)
		if err != nil {
			return server.capabilityQuarantinePersistenceFailureResponse(effectiveTokenClaims, capabilityRequest, err)
		}
	}
	if strings.TrimSpace(builtQuarantineRef) != "" {
		quarantineRef = builtQuarantineRef
	}
	classification, err = normalizeResultClassification(classification, quarantineRef)
	if err != nil {
		return controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "capability result classification is invalid",
			DenialCode:   controlapipkg.DenialCodeExecutionFailed,
			Redacted:     true,
		}
	}
	resultMetadata := server.capabilityProvenanceMetadata(capabilityRequest.Capability, quarantineRef)
	if resultMetadata == nil {
		resultMetadata = make(map[string]interface{})
	}
	resultMetadata["prompt_eligible"] = classification.PromptEligible()
	resultMetadata["display_only"] = classification.DisplayOnly()
	resultMetadata["audit_only"] = classification.AuditOnly()
	resultMetadata["quarantined"] = classification.Quarantined()
	if err := server.logEvent("capability.executed", effectiveTokenClaims.ControlSessionID, map[string]interface{}{
		"request_id":            capabilityRequest.RequestID,
		"capability":            capabilityRequest.Capability,
		"status":                controlapipkg.ResponseStatusSuccess,
		"result_classification": classification,
		"result_provenance":     resultMetadata,
		"quarantine_ref":        quarantineRef,
		"actor_label":           effectiveTokenClaims.ActorLabel,
		"client_session_label":  effectiveTokenClaims.ClientSessionLabel,
		"control_session_id":    effectiveTokenClaims.ControlSessionID,
		"token_id":              effectiveTokenClaims.TokenID,
		"parent_token_id":       effectiveTokenClaims.ParentTokenID,
	}); err != nil {
		return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
	}
	successResponse := controlapipkg.CapabilityResponse{
		RequestID:        capabilityRequest.RequestID,
		Status:           controlapipkg.ResponseStatusSuccess,
		StructuredResult: structuredResult,
		FieldsMeta:       fieldsMeta,
		Classification:   classification,
		QuarantineRef:    quarantineRef,
		Metadata:         resultMetadata,
	}
	if !classification.AuditOnly() {
		server.emitUIToolResult(effectiveTokenClaims.ControlSessionID, capabilityRequest, successResponse)
	}
	return successResponse
}

func (server *Server) capabilityQuarantinePersistenceFailureResponse(effectiveTokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest, quarantineErr error) controlapipkg.CapabilityResponse {
	wrappedErr := fmt.Errorf("quarantine persistence failed: %w", quarantineErr)
	if auditErr := server.logEvent("capability.error", effectiveTokenClaims.ControlSessionID, map[string]interface{}{
		"request_id":           capabilityRequest.RequestID,
		"capability":           capabilityRequest.Capability,
		"error":                secrets.RedactText(wrappedErr.Error()),
		"operator_error_class": secrets.LoopgateOperatorErrorClass(wrappedErr),
		"actor_label":          effectiveTokenClaims.ActorLabel,
		"client_session_label": effectiveTokenClaims.ClientSessionLabel,
		"control_session_id":   effectiveTokenClaims.ControlSessionID,
		"token_id":             effectiveTokenClaims.TokenID,
		"parent_token_id":      effectiveTokenClaims.ParentTokenID,
	}); auditErr != nil {
		return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
	}
	server.emitUIEvent(effectiveTokenClaims.ControlSessionID, controlapipkg.UIEventTypeWarning, controlapipkg.UIEventWarning{
		Message: "capability quarantine persistence failed",
	})
	return controlapipkg.CapabilityResponse{
		RequestID:    capabilityRequest.RequestID,
		Status:       controlapipkg.ResponseStatusError,
		DenialReason: "capability quarantine persistence failed",
		DenialCode:   controlapipkg.DenialCodeExecutionFailed,
		Redacted:     true,
	}
}
