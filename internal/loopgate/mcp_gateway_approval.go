package loopgate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"strings"
	"time"

	approvalpkg "loopgate/internal/approvalruntime"
)

const (
	approvalActionClassMCPGatewayInvoke = "mcp_gateway.invoke"
	approvalSubjectClassMCPGatewayTool  = "mcp_gateway_tool"
	approvalExecutionMethodMCPGateway   = "POST"
	// Reserved for the future brokered execution path. This approval-preparation slice binds to
	// the eventual governed execution boundary without launching or executing MCP servers yet.
	approvalExecutionPathMCPGateway = "/v1/mcp-gateway/invocation/execute"
)

var (
	errMCPGatewayApprovalPreparationNotRequired = errors.New("mcp gateway invocation does not require approval preparation")
	errMCPGatewayApprovalStoreSaturated         = errors.New("mcp gateway approval store is at capacity")
	errMCPGatewayApprovalSessionLimitReached    = errors.New("mcp gateway approval session limit reached")
)

type pendingMCPGatewayApprovalRequest struct {
	ID                     string
	ControlSessionID       string
	ActorLabel             string
	ClientSessionLabel     string
	TenantID               string
	UserID                 string
	ServerID               string
	ToolName               string
	ValidatedArgumentKeys  []string
	CreatedAt              time.Time
	ExpiresAt              time.Time
	DecisionNonce          string
	DecisionSubmittedAt    time.Time
	ExecutedAt             time.Time
	ApprovalManifestSHA256 string
	InvocationBodySHA256   string
	State                  string
}

func setMCPGatewayApprovalStateLocked(approvalRequests map[string]pendingMCPGatewayApprovalRequest, approvalRequestID string, approvalRequest pendingMCPGatewayApprovalRequest, nextState string) (pendingMCPGatewayApprovalRequest, error) {
	if err := approvalpkg.ValidateStateTransition(approvalRequest.State, nextState); err != nil {
		return approvalRequest, err
	}
	approvalRequest.State = nextState
	approvalRequests[approvalRequestID] = approvalRequest
	return approvalRequest, nil
}

func mcpGatewayInvocationRequestBodySHA256(validatedRequest controlapipkg.ValidatedMCPGatewayInvocationRequest) (string, error) {
	type canonicalInvocationRequest struct {
		ServerID  string                     `json:"server_id"`
		ToolName  string                     `json:"tool_name"`
		Arguments map[string]json.RawMessage `json:"arguments"`
	}
	requestBytes, err := json.Marshal(canonicalInvocationRequest{
		ServerID:  validatedRequest.ServerID,
		ToolName:  validatedRequest.ToolName,
		Arguments: validatedRequest.Arguments,
	})
	if err != nil {
		return "", fmt.Errorf("marshal MCP gateway invocation request: %w", err)
	}
	bodyHash := sha256.Sum256(requestBytes)
	return hex.EncodeToString(bodyHash[:]), nil
}

func mcpGatewayApprovalSubjectBinding(serverID string, toolName string) string {
	toolHash := sha256.Sum256([]byte("mcp-server:" + serverID + "\nmcp-tool:" + toolName))
	return "object-sha256:" + hex.EncodeToString(toolHash[:])
}

func buildMCPGatewayApprovalManifest(validatedRequest controlapipkg.ValidatedMCPGatewayInvocationRequest, expiresAtUTC time.Time) (manifestSHA256 string, bodySHA256 string, err error) {
	bodySHA256, err = mcpGatewayInvocationRequestBodySHA256(validatedRequest)
	if err != nil {
		return "", "", err
	}
	manifestSHA256 = approvalpkg.ComputeManifestSHA256(
		approvalActionClassMCPGatewayInvoke,
		approvalSubjectClassMCPGatewayTool,
		validatedRequest.ServerID+"/"+validatedRequest.ToolName,
		mcpGatewayApprovalSubjectBinding(validatedRequest.ServerID, validatedRequest.ToolName),
		approvalExecutionMethodMCPGateway,
		approvalExecutionPathMCPGateway,
		bodySHA256,
		approvalpkg.ScopeSingleUse,
		expiresAtUTC.UTC().UnixMilli(),
	)
	return manifestSHA256, bodySHA256, nil
}

func buildMCPGatewayInvocationApprovalResponse(validationResponse controlapipkg.MCPGatewayInvocationValidationResponse, approvalRequest pendingMCPGatewayApprovalRequest) controlapipkg.MCPGatewayInvocationApprovalResponse {
	return controlapipkg.MCPGatewayInvocationApprovalResponse{
		ServerID:               validationResponse.ServerID,
		ToolName:               validationResponse.ToolName,
		Decision:               validationResponse.Decision,
		RequiresApproval:       validationResponse.RequiresApproval,
		ValidatedArgumentCount: validationResponse.ValidatedArgumentCount,
		ValidatedArgumentKeys:  append([]string(nil), validationResponse.ValidatedArgumentKeys...),
		DenialCode:             validationResponse.DenialCode,
		DenialReason:           validationResponse.DenialReason,
		ApprovalPrepared:       strings.TrimSpace(approvalRequest.ID) != "",
		ApprovalRequestID:      approvalRequest.ID,
		ApprovalDecisionNonce:  approvalRequest.DecisionNonce,
		ApprovalManifestSHA256: approvalRequest.ApprovalManifestSHA256,
		ApprovalExpiresAtUTC:   approvalRequest.ExpiresAt.UTC().Format(time.RFC3339Nano),
	}
}

func buildDeniedMCPGatewayInvocationApprovalResponse(validationResponse controlapipkg.MCPGatewayInvocationValidationResponse, denialCode string, denialReason string) controlapipkg.MCPGatewayInvocationApprovalResponse {
	deniedResponse := controlapipkg.MCPGatewayInvocationApprovalResponse{
		ServerID:               validationResponse.ServerID,
		ToolName:               validationResponse.ToolName,
		Decision:               "deny",
		RequiresApproval:       false,
		ValidatedArgumentCount: validationResponse.ValidatedArgumentCount,
		ValidatedArgumentKeys:  append([]string(nil), validationResponse.ValidatedArgumentKeys...),
		DenialCode:             strings.TrimSpace(denialCode),
		DenialReason:           strings.TrimSpace(denialReason),
	}
	if deniedResponse.DenialReason == "" {
		deniedResponse.DenialReason = "MCP gateway invocation approval could not be prepared"
	}
	return deniedResponse
}

func buildMCPGatewayApprovalCreatedAuditData(approvalRequest pendingMCPGatewayApprovalRequest) map[string]interface{} {
	return map[string]interface{}{
		"approval_request_id":      approvalRequest.ID,
		"approval_class":           ApprovalClassMCPGatewayInvoke,
		"approval_state":           approvalStatePending,
		"control_session_id":       approvalRequest.ControlSessionID,
		"actor_label":              approvalRequest.ActorLabel,
		"client_session_label":     approvalRequest.ClientSessionLabel,
		"tenant_id":                approvalRequest.TenantID,
		"user_id":                  approvalRequest.UserID,
		"server_id":                approvalRequest.ServerID,
		"tool_name":                approvalRequest.ToolName,
		"validated_argument_keys":  append([]string(nil), approvalRequest.ValidatedArgumentKeys...),
		"validated_argument_count": len(approvalRequest.ValidatedArgumentKeys),
		"approval_manifest_sha256": approvalRequest.ApprovalManifestSHA256,
	}
}

func buildMCPGatewayApprovalGrantedAuditData(approvalRequest pendingMCPGatewayApprovalRequest) map[string]interface{} {
	return map[string]interface{}{
		"approval_request_id":      approvalRequest.ID,
		"approval_class":           ApprovalClassMCPGatewayInvoke,
		"approval_state":           approvalStateGranted,
		"control_session_id":       approvalRequest.ControlSessionID,
		"actor_label":              approvalRequest.ActorLabel,
		"client_session_label":     approvalRequest.ClientSessionLabel,
		"tenant_id":                approvalRequest.TenantID,
		"user_id":                  approvalRequest.UserID,
		"server_id":                approvalRequest.ServerID,
		"tool_name":                approvalRequest.ToolName,
		"validated_argument_keys":  append([]string(nil), approvalRequest.ValidatedArgumentKeys...),
		"validated_argument_count": len(approvalRequest.ValidatedArgumentKeys),
		"approval_manifest_sha256": approvalRequest.ApprovalManifestSHA256,
	}
}

func buildMCPGatewayApprovalDeniedAuditData(approvalRequest pendingMCPGatewayApprovalRequest, denialCode string, denialReason string) map[string]interface{} {
	auditData := map[string]interface{}{
		"approval_request_id":      approvalRequest.ID,
		"approval_class":           ApprovalClassMCPGatewayInvoke,
		"approval_state":           approvalStateDenied,
		"control_session_id":       approvalRequest.ControlSessionID,
		"actor_label":              approvalRequest.ActorLabel,
		"client_session_label":     approvalRequest.ClientSessionLabel,
		"tenant_id":                approvalRequest.TenantID,
		"user_id":                  approvalRequest.UserID,
		"server_id":                approvalRequest.ServerID,
		"tool_name":                approvalRequest.ToolName,
		"validated_argument_keys":  append([]string(nil), approvalRequest.ValidatedArgumentKeys...),
		"validated_argument_count": len(approvalRequest.ValidatedArgumentKeys),
		"approval_manifest_sha256": approvalRequest.ApprovalManifestSHA256,
	}
	if strings.TrimSpace(denialCode) != "" {
		auditData["denial_code"] = denialCode
	}
	if strings.TrimSpace(denialReason) != "" {
		auditData["reason"] = denialReason
	}
	return auditData
}

func buildMCPGatewayApprovalDecisionResponse(approvalRequest pendingMCPGatewayApprovalRequest, approved bool) controlapipkg.MCPGatewayApprovalDecisionResponse {
	return controlapipkg.MCPGatewayApprovalDecisionResponse{
		ApprovalRequestID:      approvalRequest.ID,
		ServerID:               approvalRequest.ServerID,
		ToolName:               approvalRequest.ToolName,
		ValidatedArgumentCount: len(approvalRequest.ValidatedArgumentKeys),
		ValidatedArgumentKeys:  append([]string(nil), approvalRequest.ValidatedArgumentKeys...),
		Approved:               approved,
		ApprovalState:          approvalRequest.State,
	}
}

func buildMCPGatewayExecutionValidationResponse(approvalRequest pendingMCPGatewayApprovalRequest) controlapipkg.MCPGatewayExecutionValidationResponse {
	return controlapipkg.MCPGatewayExecutionValidationResponse{
		ApprovalRequestID:      approvalRequest.ID,
		ApprovalState:          approvalRequest.State,
		ServerID:               approvalRequest.ServerID,
		ToolName:               approvalRequest.ToolName,
		ValidatedArgumentCount: len(approvalRequest.ValidatedArgumentKeys),
		ValidatedArgumentKeys:  append([]string(nil), approvalRequest.ValidatedArgumentKeys...),
		ExecutionAuthorized:    true,
		ExecutionMethod:        approvalExecutionMethodMCPGateway,
		ExecutionPath:          approvalExecutionPathMCPGateway,
	}
}

func buildMCPGatewayExecutionResponse(approvalRequest pendingMCPGatewayApprovalRequest, processPID int, toolResult json.RawMessage, remoteError *mcpGatewayJSONRPCError) controlapipkg.MCPGatewayExecutionResponse {
	executionResponse := controlapipkg.MCPGatewayExecutionResponse{
		ApprovalRequestID:      approvalRequest.ID,
		ApprovalState:          approvalRequest.State,
		ServerID:               approvalRequest.ServerID,
		ToolName:               approvalRequest.ToolName,
		ValidatedArgumentCount: len(approvalRequest.ValidatedArgumentKeys),
		ValidatedArgumentKeys:  append([]string(nil), approvalRequest.ValidatedArgumentKeys...),
		ProcessPID:             processPID,
	}
	if len(toolResult) > 0 {
		executionResponse.ToolResult = append(json.RawMessage(nil), toolResult...)
		toolResultHash := sha256.Sum256(toolResult)
		executionResponse.ToolResultSHA256 = hex.EncodeToString(toolResultHash[:])
	}
	if remoteError != nil {
		executionResponse.RemoteErrorCode = remoteError.Code
		executionResponse.RemoteErrorMessage = remoteError.Message
	}
	return executionResponse
}

func buildMCPGatewayExecutionAuditData(tokenClaims capabilityToken, validationResponse controlapipkg.MCPGatewayExecutionValidationResponse) map[string]interface{} {
	return map[string]interface{}{
		"approval_request_id":      validationResponse.ApprovalRequestID,
		"approval_state":           validationResponse.ApprovalState,
		"control_session_id":       tokenClaims.ControlSessionID,
		"actor_label":              tokenClaims.ActorLabel,
		"client_session_label":     tokenClaims.ClientSessionLabel,
		"tenant_id":                tokenClaims.TenantID,
		"user_id":                  tokenClaims.UserID,
		"server_id":                validationResponse.ServerID,
		"tool_name":                validationResponse.ToolName,
		"validated_argument_keys":  append([]string(nil), validationResponse.ValidatedArgumentKeys...),
		"validated_argument_count": validationResponse.ValidatedArgumentCount,
		"execution_method":         validationResponse.ExecutionMethod,
		"execution_path":           validationResponse.ExecutionPath,
	}
}

func buildMCPGatewayExecutionStartedAuditData(tokenClaims capabilityToken, approvalRequest pendingMCPGatewayApprovalRequest, launchedServer *mcpGatewayLaunchedServer) map[string]interface{} {
	return map[string]interface{}{
		"approval_request_id":      approvalRequest.ID,
		"approval_state":           approvalRequest.State,
		"control_session_id":       tokenClaims.ControlSessionID,
		"actor_label":              tokenClaims.ActorLabel,
		"client_session_label":     tokenClaims.ClientSessionLabel,
		"tenant_id":                tokenClaims.TenantID,
		"user_id":                  tokenClaims.UserID,
		"server_id":                approvalRequest.ServerID,
		"tool_name":                approvalRequest.ToolName,
		"validated_argument_keys":  append([]string(nil), approvalRequest.ValidatedArgumentKeys...),
		"validated_argument_count": len(approvalRequest.ValidatedArgumentKeys),
		"execution_method":         approvalExecutionMethodMCPGateway,
		"execution_path":           approvalExecutionPathMCPGateway,
		"pid":                      launchedServer.PID,
	}
}

func buildMCPGatewayExecutionCompletedAuditData(tokenClaims capabilityToken, approvalRequest pendingMCPGatewayApprovalRequest, launchedServer *mcpGatewayLaunchedServer, toolResult json.RawMessage, remoteError *mcpGatewayJSONRPCError) map[string]interface{} {
	auditData := buildMCPGatewayExecutionStartedAuditData(tokenClaims, approvalRequest, launchedServer)
	if len(toolResult) > 0 {
		toolResultHash := sha256.Sum256(toolResult)
		auditData["tool_result_sha256"] = hex.EncodeToString(toolResultHash[:])
		auditData["tool_result_bytes"] = len(toolResult)
	}
	if remoteError != nil {
		auditData["remote_error_code"] = remoteError.Code
		auditData["remote_error_message"] = remoteError.Message
	}
	return auditData
}

func buildMCPGatewayExecutionFailedAuditData(tokenClaims capabilityToken, approvalRequest pendingMCPGatewayApprovalRequest, launchedServer *mcpGatewayLaunchedServer, denialCode string) map[string]interface{} {
	auditData := buildMCPGatewayExecutionStartedAuditData(tokenClaims, approvalRequest, launchedServer)
	if strings.TrimSpace(denialCode) != "" {
		auditData["denial_code"] = denialCode
	}
	return auditData
}

func (server *Server) rollbackMCPGatewayApprovalRequestAfterAuditFailure(approvalRequest pendingMCPGatewayApprovalRequest) {
	server.mu.Lock()
	defer server.mu.Unlock()

	currentApprovalRequest, found := server.mcpGatewayApprovalRequests[approvalRequest.ID]
	if !found {
		return
	}
	if currentApprovalRequest.State != approvalStatePending {
		return
	}
	if currentApprovalRequest.ApprovalManifestSHA256 != approvalRequest.ApprovalManifestSHA256 {
		return
	}
	delete(server.mcpGatewayApprovalRequests, approvalRequest.ID)
}

func (server *Server) createOrReuseMCPGatewayApprovalRequest(tokenClaims capabilityToken, invocationRequest controlapipkg.MCPGatewayInvocationRequest, validationResponse controlapipkg.MCPGatewayInvocationValidationResponse) (pendingMCPGatewayApprovalRequest, bool, error) {
	if validationResponse.Decision != "needs_approval" || !validationResponse.RequiresApproval {
		return pendingMCPGatewayApprovalRequest{}, false, errMCPGatewayApprovalPreparationNotRequired
	}

	validatedRequest, err := controlapipkg.ValidateMCPGatewayInvocationRequest(invocationRequest)
	if err != nil {
		return pendingMCPGatewayApprovalRequest{}, false, err
	}

	nowUTC := server.now().UTC()
	expiresAtUTC := nowUTC.Add(approvalTTL)
	approvalManifestSHA256, invocationBodySHA256, err := buildMCPGatewayApprovalManifest(validatedRequest, expiresAtUTC)
	if err != nil {
		return pendingMCPGatewayApprovalRequest{}, false, err
	}

	server.mu.Lock()
	defer server.mu.Unlock()

	server.pruneExpiredLocked()
	for _, existingApprovalRequest := range server.mcpGatewayApprovalRequests {
		if existingApprovalRequest.ControlSessionID != tokenClaims.ControlSessionID {
			continue
		}
		if existingApprovalRequest.State != approvalStatePending {
			continue
		}
		if !existingApprovalRequest.ExpiresAt.After(nowUTC) {
			continue
		}
		if existingApprovalRequest.InvocationBodySHA256 != invocationBodySHA256 {
			continue
		}
		return existingApprovalRequest, false, nil
	}

	if len(server.approvalState.records)+len(server.mcpGatewayApprovalRequests) >= server.maxTotalApprovalRecords {
		return pendingMCPGatewayApprovalRequest{}, false, errMCPGatewayApprovalStoreSaturated
	}
	if server.countPendingApprovalsForSessionLocked(tokenClaims.ControlSessionID)+server.countPendingMCPGatewayApprovalRequestsForSessionLocked(tokenClaims.ControlSessionID) >= server.maxPendingApprovalsPerControlSession {
		return pendingMCPGatewayApprovalRequest{}, false, errMCPGatewayApprovalSessionLimitReached
	}

	approvalRequestID, err := randomHex(12)
	if err != nil {
		return pendingMCPGatewayApprovalRequest{}, false, fmt.Errorf("create MCP gateway approval request id: %w", err)
	}
	decisionNonce, err := randomHex(16)
	if err != nil {
		return pendingMCPGatewayApprovalRequest{}, false, fmt.Errorf("create MCP gateway approval nonce: %w", err)
	}

	approvalRequest := pendingMCPGatewayApprovalRequest{
		ID:                     approvalRequestID,
		ControlSessionID:       tokenClaims.ControlSessionID,
		ActorLabel:             tokenClaims.ActorLabel,
		ClientSessionLabel:     tokenClaims.ClientSessionLabel,
		TenantID:               tokenClaims.TenantID,
		UserID:                 tokenClaims.UserID,
		ServerID:               validatedRequest.ServerID,
		ToolName:               validatedRequest.ToolName,
		ValidatedArgumentKeys:  append([]string(nil), validationResponse.ValidatedArgumentKeys...),
		CreatedAt:              nowUTC,
		ExpiresAt:              expiresAtUTC,
		DecisionNonce:          decisionNonce,
		ApprovalManifestSHA256: approvalManifestSHA256,
		InvocationBodySHA256:   invocationBodySHA256,
		State:                  approvalStatePending,
	}
	server.mcpGatewayApprovalRequests[approvalRequest.ID] = approvalRequest
	server.noteExpiryCandidateLocked(approvalRequest.ExpiresAt)
	return approvalRequest, true, nil
}

func (server *Server) validatePendingMCPGatewayApprovalDecisionLocked(tokenClaims capabilityToken, decisionRequest controlapipkg.MCPGatewayApprovalDecisionRequest) (pendingMCPGatewayApprovalRequest, controlapipkg.CapabilityResponse, bool) {
	server.pruneExpiredLocked()

	approvalRequestID := strings.TrimSpace(decisionRequest.ApprovalRequestID)
	approvalRequest, found := server.mcpGatewayApprovalRequests[approvalRequestID]
	if !found {
		return pendingMCPGatewayApprovalRequest{}, controlapipkg.CapabilityResponse{
			RequestID:         approvalRequestID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "approval request not found",
			DenialCode:        controlapipkg.DenialCodeApprovalNotFound,
			ApprovalRequestID: approvalRequestID,
		}, false
	}

	if approvalRequest.ExpiresAt.Before(server.now().UTC()) {
		expiredApprovalRequest, transitionErr := setMCPGatewayApprovalStateLocked(server.mcpGatewayApprovalRequests, approvalRequestID, approvalRequest, approvalStateExpired)
		if transitionErr != nil {
			return approvalRequest, controlapipkg.CapabilityResponse{
				RequestID:         approvalRequestID,
				Status:            controlapipkg.ResponseStatusDenied,
				DenialReason:      "approval request is in an invalid state",
				DenialCode:        controlapipkg.DenialCodeApprovalStateInvalid,
				ApprovalRequestID: approvalRequestID,
			}, false
		}
		approvalRequest = expiredApprovalRequest
		return approvalRequest, controlapipkg.CapabilityResponse{
			RequestID:         approvalRequestID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "approval request expired",
			DenialCode:        controlapipkg.DenialCodeApprovalDenied,
			ApprovalRequestID: approvalRequestID,
		}, false
	}
	if tokenClaims.ControlSessionID != approvalRequest.ControlSessionID {
		return approvalRequest, controlapipkg.CapabilityResponse{
			RequestID:         approvalRequestID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "approval token does not match approval owner",
			DenialCode:        controlapipkg.DenialCodeApprovalOwnerMismatch,
			ApprovalRequestID: approvalRequestID,
		}, false
	}
	if approvalRequest.State != approvalStatePending {
		denialCode := controlapipkg.DenialCodeApprovalStateInvalid
		if approvalRequest.State == approvalStateGranted || approvalRequest.State == approvalStateConsumed || approvalRequest.State == approvalStateExecutionFailed {
			denialCode = controlapipkg.DenialCodeApprovalStateConflict
		}
		return approvalRequest, controlapipkg.CapabilityResponse{
			RequestID:         approvalRequestID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "approval request is no longer pending",
			DenialCode:        denialCode,
			ApprovalRequestID: approvalRequestID,
		}, false
	}

	decisionNonce := strings.TrimSpace(decisionRequest.DecisionNonce)
	if decisionNonce == "" {
		return approvalRequest, controlapipkg.CapabilityResponse{
			RequestID:         approvalRequestID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "approval decision nonce is required",
			DenialCode:        controlapipkg.DenialCodeApprovalDecisionNonceMissing,
			ApprovalRequestID: approvalRequestID,
		}, false
	}
	if decisionNonce != approvalRequest.DecisionNonce {
		return approvalRequest, controlapipkg.CapabilityResponse{
			RequestID:         approvalRequestID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "approval decision nonce is invalid",
			DenialCode:        controlapipkg.DenialCodeApprovalDecisionNonceInvalid,
			ApprovalRequestID: approvalRequestID,
		}, false
	}

	submittedManifest := strings.TrimSpace(decisionRequest.ApprovalManifestSHA256)
	if decisionRequest.Approved && approvalRequest.ApprovalManifestSHA256 != "" {
		if submittedManifest == "" {
			return approvalRequest, controlapipkg.CapabilityResponse{
				RequestID:         approvalRequestID,
				Status:            controlapipkg.ResponseStatusDenied,
				DenialReason:      "approval manifest sha256 is required for this approval",
				DenialCode:        controlapipkg.DenialCodeApprovalManifestMismatch,
				ApprovalRequestID: approvalRequestID,
			}, false
		}
		if submittedManifest != approvalRequest.ApprovalManifestSHA256 {
			return approvalRequest, controlapipkg.CapabilityResponse{
				RequestID:         approvalRequestID,
				Status:            controlapipkg.ResponseStatusDenied,
				DenialReason:      "approval manifest sha256 does not match the pending approval",
				DenialCode:        controlapipkg.DenialCodeApprovalManifestMismatch,
				ApprovalRequestID: approvalRequestID,
			}, false
		}
	}

	return approvalRequest, controlapipkg.CapabilityResponse{}, true
}

func (server *Server) validateAndRecordMCPGatewayApprovalDecision(tokenClaims capabilityToken, decisionRequest controlapipkg.MCPGatewayApprovalDecisionRequest) (pendingMCPGatewayApprovalRequest, controlapipkg.CapabilityResponse, bool) {
	server.mu.Lock()
	defer server.mu.Unlock()

	approvalRequest, denialResponse, ok := server.validatePendingMCPGatewayApprovalDecisionLocked(tokenClaims, decisionRequest)
	if !ok {
		return approvalRequest, denialResponse, false
	}

	var auditData map[string]interface{}
	if decisionRequest.Approved {
		auditData = buildMCPGatewayApprovalGrantedAuditData(approvalRequest)
		if err := server.logEvent("approval.granted", approvalRequest.ControlSessionID, auditData); err != nil {
			return approvalRequest, controlapipkg.CapabilityResponse{
				RequestID:         approvalRequest.ID,
				Status:            controlapipkg.ResponseStatusError,
				DenialReason:      "control-plane audit is unavailable",
				DenialCode:        controlapipkg.DenialCodeAuditUnavailable,
				ApprovalRequestID: approvalRequest.ID,
			}, false
		}
		updatedApprovalRequest, transitionErr := setMCPGatewayApprovalStateLocked(server.mcpGatewayApprovalRequests, approvalRequest.ID, approvalRequest, approvalStateGranted)
		if transitionErr != nil {
			return approvalRequest, controlapipkg.CapabilityResponse{
				RequestID:         approvalRequest.ID,
				Status:            controlapipkg.ResponseStatusDenied,
				DenialReason:      "approval request is in an invalid state",
				DenialCode:        controlapipkg.DenialCodeApprovalStateInvalid,
				ApprovalRequestID: approvalRequest.ID,
			}, false
		}
		approvalRequest = updatedApprovalRequest
	} else {
		auditData = buildMCPGatewayApprovalDeniedAuditData(approvalRequest, controlapipkg.DenialCodeApprovalDenied, "approval denied")
		if err := server.logEvent("approval.denied", approvalRequest.ControlSessionID, auditData); err != nil {
			return approvalRequest, controlapipkg.CapabilityResponse{
				RequestID:         approvalRequest.ID,
				Status:            controlapipkg.ResponseStatusError,
				DenialReason:      "control-plane audit is unavailable",
				DenialCode:        controlapipkg.DenialCodeAuditUnavailable,
				ApprovalRequestID: approvalRequest.ID,
			}, false
		}
		updatedApprovalRequest, transitionErr := setMCPGatewayApprovalStateLocked(server.mcpGatewayApprovalRequests, approvalRequest.ID, approvalRequest, approvalStateDenied)
		if transitionErr != nil {
			return approvalRequest, controlapipkg.CapabilityResponse{
				RequestID:         approvalRequest.ID,
				Status:            controlapipkg.ResponseStatusDenied,
				DenialReason:      "approval request is in an invalid state",
				DenialCode:        controlapipkg.DenialCodeApprovalStateInvalid,
				ApprovalRequestID: approvalRequest.ID,
			}, false
		}
		approvalRequest = updatedApprovalRequest
	}
	approvalRequest.DecisionSubmittedAt = server.now().UTC()
	approvalRequest.DecisionNonce = ""
	server.mcpGatewayApprovalRequests[approvalRequest.ID] = approvalRequest
	return approvalRequest, controlapipkg.CapabilityResponse{}, true
}

func (server *Server) validateMCPGatewayExecutionRequest(tokenClaims capabilityToken, executionRequest controlapipkg.MCPGatewayExecutionRequest) (controlapipkg.MCPGatewayExecutionValidationResponse, controlapipkg.CapabilityResponse, bool) {
	_, validationResponse, denialResponse, ok := server.validateMCPGatewayExecutionRequestWithApproval(tokenClaims, executionRequest)
	return validationResponse, denialResponse, ok
}

func (server *Server) validateMCPGatewayExecutionRequestWithApproval(tokenClaims capabilityToken, executionRequest controlapipkg.MCPGatewayExecutionRequest) (pendingMCPGatewayApprovalRequest, controlapipkg.MCPGatewayExecutionValidationResponse, controlapipkg.CapabilityResponse, bool) {
	validatedRequest, err := controlapipkg.ValidateMCPGatewayInvocationRequest(controlapipkg.MCPGatewayInvocationRequest{
		ServerID:  executionRequest.ServerID,
		ToolName:  executionRequest.ToolName,
		Arguments: executionRequest.Arguments,
	})
	if err != nil {
		return pendingMCPGatewayApprovalRequest{}, controlapipkg.MCPGatewayExecutionValidationResponse{}, controlapipkg.CapabilityResponse{
			RequestID:         strings.TrimSpace(executionRequest.ApprovalRequestID),
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      err.Error(),
			DenialCode:        controlapipkg.DenialCodeMalformedRequest,
			ApprovalRequestID: strings.TrimSpace(executionRequest.ApprovalRequestID),
		}, false
	}
	invocationBodySHA256, err := mcpGatewayInvocationRequestBodySHA256(validatedRequest)
	if err != nil {
		return pendingMCPGatewayApprovalRequest{}, controlapipkg.MCPGatewayExecutionValidationResponse{}, controlapipkg.CapabilityResponse{
			RequestID:         strings.TrimSpace(executionRequest.ApprovalRequestID),
			Status:            controlapipkg.ResponseStatusError,
			DenialReason:      "failed to validate MCP gateway execution request",
			DenialCode:        controlapipkg.DenialCodeExecutionFailed,
			ApprovalRequestID: strings.TrimSpace(executionRequest.ApprovalRequestID),
		}, false
	}

	server.mu.Lock()
	defer server.mu.Unlock()

	server.pruneExpiredLocked()
	approvalRequestID := strings.TrimSpace(executionRequest.ApprovalRequestID)
	approvalRequest, found := server.mcpGatewayApprovalRequests[approvalRequestID]
	if !found {
		return pendingMCPGatewayApprovalRequest{}, controlapipkg.MCPGatewayExecutionValidationResponse{}, controlapipkg.CapabilityResponse{
			RequestID:         approvalRequestID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "approval request not found",
			DenialCode:        controlapipkg.DenialCodeApprovalNotFound,
			ApprovalRequestID: approvalRequestID,
		}, false
	}
	if approvalRequest.ExpiresAt.Before(server.now().UTC()) {
		expiredApprovalRequest, transitionErr := setMCPGatewayApprovalStateLocked(server.mcpGatewayApprovalRequests, approvalRequestID, approvalRequest, approvalStateExpired)
		if transitionErr != nil {
			return approvalRequest, controlapipkg.MCPGatewayExecutionValidationResponse{}, controlapipkg.CapabilityResponse{
				RequestID:         approvalRequestID,
				Status:            controlapipkg.ResponseStatusDenied,
				DenialReason:      "approval request is in an invalid state",
				DenialCode:        controlapipkg.DenialCodeApprovalStateInvalid,
				ApprovalRequestID: approvalRequestID,
			}, false
		}
		approvalRequest = expiredApprovalRequest
		return approvalRequest, controlapipkg.MCPGatewayExecutionValidationResponse{}, controlapipkg.CapabilityResponse{
			RequestID:         approvalRequestID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "approval request expired",
			DenialCode:        controlapipkg.DenialCodeApprovalDenied,
			ApprovalRequestID: approvalRequestID,
		}, false
	}
	if approvalRequest.ControlSessionID != tokenClaims.ControlSessionID {
		return approvalRequest, controlapipkg.MCPGatewayExecutionValidationResponse{}, controlapipkg.CapabilityResponse{
			RequestID:         approvalRequestID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "approval token does not match approval owner",
			DenialCode:        controlapipkg.DenialCodeApprovalOwnerMismatch,
			ApprovalRequestID: approvalRequestID,
		}, false
	}
	if approvalRequest.State != approvalStateGranted {
		denialCode := controlapipkg.DenialCodeApprovalStateInvalid
		if approvalRequest.State == approvalStateConsumed || approvalRequest.State == approvalStateExecutionFailed {
			denialCode = controlapipkg.DenialCodeApprovalStateConflict
		}
		return approvalRequest, controlapipkg.MCPGatewayExecutionValidationResponse{}, controlapipkg.CapabilityResponse{
			RequestID:         approvalRequestID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "approval request is not ready for execution",
			DenialCode:        denialCode,
			ApprovalRequestID: approvalRequestID,
		}, false
	}
	if strings.TrimSpace(executionRequest.ApprovalManifestSHA256) != approvalRequest.ApprovalManifestSHA256 {
		return approvalRequest, controlapipkg.MCPGatewayExecutionValidationResponse{}, controlapipkg.CapabilityResponse{
			RequestID:         approvalRequestID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "approval manifest sha256 does not match the granted approval",
			DenialCode:        controlapipkg.DenialCodeApprovalManifestMismatch,
			ApprovalRequestID: approvalRequestID,
		}, false
	}
	if approvalRequest.ServerID != validatedRequest.ServerID || approvalRequest.ToolName != validatedRequest.ToolName {
		return approvalRequest, controlapipkg.MCPGatewayExecutionValidationResponse{}, controlapipkg.CapabilityResponse{
			RequestID:         approvalRequestID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "execution request does not match the granted approval",
			DenialCode:        controlapipkg.DenialCodeApprovalManifestMismatch,
			ApprovalRequestID: approvalRequestID,
		}, false
	}
	if approvalRequest.InvocationBodySHA256 != invocationBodySHA256 {
		return approvalRequest, controlapipkg.MCPGatewayExecutionValidationResponse{}, controlapipkg.CapabilityResponse{
			RequestID:         approvalRequestID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "execution request body does not match the granted approval",
			DenialCode:        controlapipkg.DenialCodeApprovalManifestMismatch,
			ApprovalRequestID: approvalRequestID,
		}, false
	}

	return approvalRequest, buildMCPGatewayExecutionValidationResponse(approvalRequest), controlapipkg.CapabilityResponse{}, true
}

func (server *Server) consumeGrantedMCPGatewayApprovalForExecution(tokenClaims capabilityToken, executionRequest controlapipkg.MCPGatewayExecutionRequest) (pendingMCPGatewayApprovalRequest, controlapipkg.CapabilityResponse, bool) {
	validatedRequest, err := controlapipkg.ValidateMCPGatewayInvocationRequest(controlapipkg.MCPGatewayInvocationRequest{
		ServerID:  executionRequest.ServerID,
		ToolName:  executionRequest.ToolName,
		Arguments: executionRequest.Arguments,
	})
	if err != nil {
		return pendingMCPGatewayApprovalRequest{}, controlapipkg.CapabilityResponse{
			RequestID:         strings.TrimSpace(executionRequest.ApprovalRequestID),
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      err.Error(),
			DenialCode:        controlapipkg.DenialCodeMalformedRequest,
			ApprovalRequestID: strings.TrimSpace(executionRequest.ApprovalRequestID),
		}, false
	}
	invocationBodySHA256, err := mcpGatewayInvocationRequestBodySHA256(validatedRequest)
	if err != nil {
		return pendingMCPGatewayApprovalRequest{}, controlapipkg.CapabilityResponse{
			RequestID:         strings.TrimSpace(executionRequest.ApprovalRequestID),
			Status:            controlapipkg.ResponseStatusError,
			DenialReason:      "failed to validate MCP gateway execution request",
			DenialCode:        controlapipkg.DenialCodeExecutionFailed,
			ApprovalRequestID: strings.TrimSpace(executionRequest.ApprovalRequestID),
		}, false
	}

	server.mu.Lock()
	defer server.mu.Unlock()

	server.pruneExpiredLocked()
	approvalRequestID := strings.TrimSpace(executionRequest.ApprovalRequestID)
	approvalRequest, found := server.mcpGatewayApprovalRequests[approvalRequestID]
	if !found {
		return pendingMCPGatewayApprovalRequest{}, controlapipkg.CapabilityResponse{
			RequestID:         approvalRequestID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "approval request not found",
			DenialCode:        controlapipkg.DenialCodeApprovalNotFound,
			ApprovalRequestID: approvalRequestID,
		}, false
	}
	if approvalRequest.ExpiresAt.Before(server.now().UTC()) {
		expiredApprovalRequest, transitionErr := setMCPGatewayApprovalStateLocked(server.mcpGatewayApprovalRequests, approvalRequestID, approvalRequest, approvalStateExpired)
		if transitionErr != nil {
			return approvalRequest, controlapipkg.CapabilityResponse{
				RequestID:         approvalRequestID,
				Status:            controlapipkg.ResponseStatusDenied,
				DenialReason:      "approval request is in an invalid state",
				DenialCode:        controlapipkg.DenialCodeApprovalStateInvalid,
				ApprovalRequestID: approvalRequestID,
			}, false
		}
		approvalRequest = expiredApprovalRequest
		return approvalRequest, controlapipkg.CapabilityResponse{
			RequestID:         approvalRequestID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "approval request expired",
			DenialCode:        controlapipkg.DenialCodeApprovalDenied,
			ApprovalRequestID: approvalRequestID,
		}, false
	}
	if approvalRequest.ControlSessionID != tokenClaims.ControlSessionID {
		return approvalRequest, controlapipkg.CapabilityResponse{
			RequestID:         approvalRequestID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "approval token does not match approval owner",
			DenialCode:        controlapipkg.DenialCodeApprovalOwnerMismatch,
			ApprovalRequestID: approvalRequestID,
		}, false
	}
	if approvalRequest.State != approvalStateGranted {
		denialCode := controlapipkg.DenialCodeApprovalStateInvalid
		if approvalRequest.State == approvalStateConsumed || approvalRequest.State == approvalStateExecutionFailed {
			denialCode = controlapipkg.DenialCodeApprovalStateConflict
		}
		return approvalRequest, controlapipkg.CapabilityResponse{
			RequestID:         approvalRequestID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "approval request is not ready for execution",
			DenialCode:        denialCode,
			ApprovalRequestID: approvalRequestID,
		}, false
	}
	if strings.TrimSpace(executionRequest.ApprovalManifestSHA256) != approvalRequest.ApprovalManifestSHA256 {
		return approvalRequest, controlapipkg.CapabilityResponse{
			RequestID:         approvalRequestID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "approval manifest sha256 does not match the granted approval",
			DenialCode:        controlapipkg.DenialCodeApprovalManifestMismatch,
			ApprovalRequestID: approvalRequestID,
		}, false
	}
	if approvalRequest.ServerID != validatedRequest.ServerID || approvalRequest.ToolName != validatedRequest.ToolName {
		return approvalRequest, controlapipkg.CapabilityResponse{
			RequestID:         approvalRequestID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "execution request does not match the granted approval",
			DenialCode:        controlapipkg.DenialCodeApprovalManifestMismatch,
			ApprovalRequestID: approvalRequestID,
		}, false
	}
	if approvalRequest.InvocationBodySHA256 != invocationBodySHA256 {
		return approvalRequest, controlapipkg.CapabilityResponse{
			RequestID:         approvalRequestID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "execution request body does not match the granted approval",
			DenialCode:        controlapipkg.DenialCodeApprovalManifestMismatch,
			ApprovalRequestID: approvalRequestID,
		}, false
	}

	approvalRequest, err = setMCPGatewayApprovalStateLocked(server.mcpGatewayApprovalRequests, approvalRequest.ID, approvalRequest, approvalStateConsumed)
	if err != nil {
		return approvalRequest, controlapipkg.CapabilityResponse{
			RequestID:         approvalRequestID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "approval request is in an invalid state",
			DenialCode:        controlapipkg.DenialCodeApprovalStateInvalid,
			ApprovalRequestID: approvalRequestID,
		}, false
	}
	approvalRequest.ExecutedAt = server.now().UTC()
	server.mcpGatewayApprovalRequests[approvalRequest.ID] = approvalRequest
	return approvalRequest, controlapipkg.CapabilityResponse{}, true
}

func (server *Server) markMCPGatewayApprovalExecutionFailed(approvalRequestID string, approvalManifestSHA256 string) {
	server.mu.Lock()
	defer server.mu.Unlock()

	approvalRequest, found := server.mcpGatewayApprovalRequests[strings.TrimSpace(approvalRequestID)]
	if !found {
		return
	}
	if approvalRequest.ApprovalManifestSHA256 != strings.TrimSpace(approvalManifestSHA256) {
		return
	}
	if approvalRequest.State != approvalStateConsumed {
		return
	}
	updatedApprovalRequest, err := setMCPGatewayApprovalStateLocked(server.mcpGatewayApprovalRequests, approvalRequest.ID, approvalRequest, approvalStateExecutionFailed)
	if err != nil {
		if server.reportSecurityWarning != nil {
			server.reportSecurityWarning("approval_state_transition_invalid", err)
		}
		return
	}
	approvalRequest = updatedApprovalRequest
	server.mcpGatewayApprovalRequests[approvalRequest.ID] = approvalRequest
}
