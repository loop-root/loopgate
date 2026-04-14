package loopgate

import (
	"errors"
	"net/http"
	"strings"
)

func (server *Server) handleMCPGatewayInventory(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityDiagnosticRead) {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	server.writeJSON(writer, http.StatusOK, server.buildMCPGatewayInventoryResponse())
}

func (server *Server) handleMCPGatewayServerStatus(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityDiagnosticRead) {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	server.writeJSON(writer, http.StatusOK, server.buildMCPGatewayServerStatusResponse())
}

func (server *Server) handleMCPGatewayDecision(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityDiagnosticRead) {
		return
	}
	requestBodyBytes, denialResponse, verified := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var decisionRequest MCPGatewayDecisionRequest
	if err := decodeJSONBytes(requestBodyBytes, &decisionRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialCode:   DenialCodeMalformedRequest,
			DenialReason: err.Error(),
			Redacted:     true,
		})
		return
	}
	if strings.TrimSpace(decisionRequest.ServerID) == "" || strings.TrimSpace(decisionRequest.ToolName) == "" {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialCode:   DenialCodeMalformedRequest,
			DenialReason: "server_id and tool_name are required",
			Redacted:     true,
		})
		return
	}

	server.writeJSON(writer, http.StatusOK, server.buildMCPGatewayDecisionResponse(decisionRequest))
}

func (server *Server) handleMCPGatewayEnsureLaunched(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityMCPGatewayWrite) {
		return
	}
	requestBodyBytes, denialResponse, verified := server.readAndVerifySignedBody(writer, request, maxApprovalBodyBytes, tokenClaims.ControlSessionID)
	if !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var ensureLaunchRequest MCPGatewayEnsureLaunchRequest
	if err := decodeJSONBytes(requestBodyBytes, &ensureLaunchRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialCode:   DenialCodeMalformedRequest,
			DenialReason: err.Error(),
			Redacted:     true,
		})
		return
	}
	if err := ensureLaunchRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialCode:   DenialCodeMalformedRequest,
			DenialReason: err.Error(),
			Redacted:     true,
		})
		return
	}

	launchResponse, capabilityResponse, ok := server.ensureMCPGatewayServerLaunched(request.Context(), tokenClaims, ensureLaunchRequest)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(capabilityResponse.DenialCode), capabilityResponse)
		return
	}
	server.writeJSON(writer, http.StatusOK, launchResponse)
}

func (server *Server) handleMCPGatewayServerStop(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityMCPGatewayWrite) {
		return
	}
	requestBodyBytes, denialResponse, verified := server.readAndVerifySignedBody(writer, request, maxApprovalBodyBytes, tokenClaims.ControlSessionID)
	if !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var stopRequest MCPGatewayStopRequest
	if err := decodeJSONBytes(requestBodyBytes, &stopRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialCode:   DenialCodeMalformedRequest,
			DenialReason: err.Error(),
			Redacted:     true,
		})
		return
	}
	if err := stopRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialCode:   DenialCodeMalformedRequest,
			DenialReason: err.Error(),
			Redacted:     true,
		})
		return
	}

	stopResponse, capabilityResponse, ok := server.stopMCPGatewayServer(request.Context(), tokenClaims, stopRequest)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(capabilityResponse.DenialCode), capabilityResponse)
		return
	}
	server.writeJSON(writer, http.StatusOK, stopResponse)
}

func (server *Server) handleMCPGatewayInvocationValidate(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityDiagnosticRead) {
		return
	}
	requestBodyBytes, denialResponse, verified := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var invocationRequest MCPGatewayInvocationRequest
	if err := decodeJSONBytes(requestBodyBytes, &invocationRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialCode:   DenialCodeMalformedRequest,
			DenialReason: err.Error(),
			Redacted:     true,
		})
		return
	}

	validationResponse, err := server.buildMCPGatewayInvocationValidationResponse(invocationRequest)
	if err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialCode:   DenialCodeMalformedRequest,
			DenialReason: err.Error(),
			Redacted:     true,
		})
		return
	}
	if err := server.logEvent("mcp_gateway.invocation_checked", tokenClaims.ControlSessionID, buildMCPGatewayInvocationAuditData(tokenClaims, validationResponse)); err != nil {
		server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialCode:   DenialCodeAuditUnavailable,
			DenialReason: "control-plane audit is unavailable",
			Redacted:     true,
		})
		return
	}

	server.writeJSON(writer, http.StatusOK, validationResponse)
}

func (server *Server) handleMCPGatewayInvocationRequestApproval(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityMCPGatewayWrite) {
		return
	}
	requestBodyBytes, denialResponse, verified := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var invocationRequest MCPGatewayInvocationRequest
	if err := decodeJSONBytes(requestBodyBytes, &invocationRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialCode:   DenialCodeMalformedRequest,
			DenialReason: err.Error(),
			Redacted:     true,
		})
		return
	}

	validationResponse, err := server.buildMCPGatewayInvocationValidationResponse(invocationRequest)
	if err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialCode:   DenialCodeMalformedRequest,
			DenialReason: err.Error(),
			Redacted:     true,
		})
		return
	}

	if validationResponse.Decision != "needs_approval" || !validationResponse.RequiresApproval {
		if err := server.logEvent("mcp_gateway.invocation_checked", tokenClaims.ControlSessionID, buildMCPGatewayInvocationAuditData(tokenClaims, validationResponse)); err != nil {
			server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
				Status:       ResponseStatusError,
				DenialCode:   DenialCodeAuditUnavailable,
				DenialReason: "control-plane audit is unavailable",
				Redacted:     true,
			})
			return
		}
		server.writeJSON(writer, http.StatusOK, MCPGatewayInvocationApprovalResponse{
			ServerID:               validationResponse.ServerID,
			ToolName:               validationResponse.ToolName,
			Decision:               validationResponse.Decision,
			RequiresApproval:       validationResponse.RequiresApproval,
			ValidatedArgumentCount: validationResponse.ValidatedArgumentCount,
			ValidatedArgumentKeys:  validationResponse.ValidatedArgumentKeys,
			DenialCode:             validationResponse.DenialCode,
			DenialReason:           validationResponse.DenialReason,
		})
		return
	}

	approvalRequest, createdApprovalRequest, err := server.createOrReuseMCPGatewayApprovalRequest(tokenClaims, invocationRequest, validationResponse)
	switch {
	case err == nil:
	case errors.Is(err, errMCPGatewayApprovalStoreSaturated):
		deniedResponse := buildDeniedMCPGatewayInvocationApprovalResponse(validationResponse, DenialCodeControlPlaneStateSaturated, "control-plane approval store is at capacity")
		if auditErr := server.logEvent("mcp_gateway.invocation_checked", tokenClaims.ControlSessionID, buildMCPGatewayInvocationAuditData(tokenClaims, MCPGatewayInvocationValidationResponse{
			ServerID:               deniedResponse.ServerID,
			ToolName:               deniedResponse.ToolName,
			Decision:               deniedResponse.Decision,
			RequiresApproval:       deniedResponse.RequiresApproval,
			ValidatedArgumentCount: deniedResponse.ValidatedArgumentCount,
			ValidatedArgumentKeys:  deniedResponse.ValidatedArgumentKeys,
			DenialCode:             deniedResponse.DenialCode,
			DenialReason:           deniedResponse.DenialReason,
		})); auditErr != nil {
			server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
				Status:       ResponseStatusError,
				DenialCode:   DenialCodeAuditUnavailable,
				DenialReason: "control-plane audit is unavailable",
				Redacted:     true,
			})
			return
		}
		server.writeJSON(writer, http.StatusOK, deniedResponse)
		return
	case errors.Is(err, errMCPGatewayApprovalSessionLimitReached):
		deniedResponse := buildDeniedMCPGatewayInvocationApprovalResponse(validationResponse, DenialCodeMCPGatewayApprovalLimit, "pending approval limit reached for control session")
		if auditErr := server.logEvent("mcp_gateway.invocation_checked", tokenClaims.ControlSessionID, buildMCPGatewayInvocationAuditData(tokenClaims, MCPGatewayInvocationValidationResponse{
			ServerID:               deniedResponse.ServerID,
			ToolName:               deniedResponse.ToolName,
			Decision:               deniedResponse.Decision,
			RequiresApproval:       deniedResponse.RequiresApproval,
			ValidatedArgumentCount: deniedResponse.ValidatedArgumentCount,
			ValidatedArgumentKeys:  deniedResponse.ValidatedArgumentKeys,
			DenialCode:             deniedResponse.DenialCode,
			DenialReason:           deniedResponse.DenialReason,
		})); auditErr != nil {
			server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
				Status:       ResponseStatusError,
				DenialCode:   DenialCodeAuditUnavailable,
				DenialReason: "control-plane audit is unavailable",
				Redacted:     true,
			})
			return
		}
		server.writeJSON(writer, http.StatusOK, deniedResponse)
		return
	default:
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialCode:   DenialCodeExecutionFailed,
			DenialReason: "failed to prepare MCP gateway approval",
			Redacted:     true,
		})
		return
	}

	if createdApprovalRequest {
		if err := server.logEvent("approval.created", tokenClaims.ControlSessionID, buildMCPGatewayApprovalCreatedAuditData(approvalRequest)); err != nil {
			server.rollbackMCPGatewayApprovalRequestAfterAuditFailure(approvalRequest)
			server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
				Status:       ResponseStatusError,
				DenialCode:   DenialCodeAuditUnavailable,
				DenialReason: "control-plane audit is unavailable",
				Redacted:     true,
			})
			return
		}
	}

	server.writeJSON(writer, http.StatusOK, buildMCPGatewayInvocationApprovalResponse(validationResponse, approvalRequest))
}

func (server *Server) handleMCPGatewayInvocationDecideApproval(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityMCPGatewayWrite) {
		return
	}
	requestBodyBytes, denialResponse, verified := server.readAndVerifySignedBody(writer, request, maxApprovalBodyBytes, tokenClaims.ControlSessionID)
	if !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var decisionRequest MCPGatewayApprovalDecisionRequest
	if err := decodeJSONBytes(requestBodyBytes, &decisionRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialCode:   DenialCodeMalformedRequest,
			DenialReason: err.Error(),
			Redacted:     true,
		})
		return
	}
	if err := decisionRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialCode:   DenialCodeMalformedRequest,
			DenialReason: err.Error(),
			Redacted:     true,
		})
		return
	}

	approvalRequest, denialResponse, resolved := server.validateAndRecordMCPGatewayApprovalDecision(tokenClaims, decisionRequest)
	if !resolved {
		if strings.TrimSpace(denialResponse.DenialCode) != "" && denialResponse.DenialCode != DenialCodeAuditUnavailable {
			decisionAuditData := buildMCPGatewayApprovalDeniedAuditData(approvalRequest, denialResponse.DenialCode, denialResponse.DenialReason)
			if err := server.logEvent("approval.denied", tokenClaims.ControlSessionID, decisionAuditData); err != nil {
				server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
					RequestID:         strings.TrimSpace(decisionRequest.ApprovalRequestID),
					Status:            ResponseStatusError,
					DenialReason:      "control-plane audit is unavailable",
					DenialCode:        DenialCodeAuditUnavailable,
					ApprovalRequestID: strings.TrimSpace(decisionRequest.ApprovalRequestID),
					Redacted:          true,
				})
				return
			}
		}
		server.writeJSON(writer, approvalDecisionHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	server.writeJSON(writer, http.StatusOK, buildMCPGatewayApprovalDecisionResponse(approvalRequest, decisionRequest.Approved))
}

func (server *Server) handleMCPGatewayInvocationValidateExecution(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityMCPGatewayWrite) {
		return
	}
	requestBodyBytes, denialResponse, verified := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var executionRequest MCPGatewayExecutionRequest
	if err := decodeJSONBytes(requestBodyBytes, &executionRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialCode:   DenialCodeMalformedRequest,
			DenialReason: err.Error(),
			Redacted:     true,
		})
		return
	}
	if err := executionRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialCode:   DenialCodeMalformedRequest,
			DenialReason: err.Error(),
			Redacted:     true,
		})
		return
	}

	validationResponse, denialResponse, ok := server.validateMCPGatewayExecutionRequest(tokenClaims, executionRequest)
	if !ok {
		server.writeJSON(writer, approvalDecisionHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}
	if err := server.logEvent("mcp_gateway.execution_checked", tokenClaims.ControlSessionID, buildMCPGatewayExecutionAuditData(tokenClaims, validationResponse)); err != nil {
		server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
			RequestID:         validationResponse.ApprovalRequestID,
			Status:            ResponseStatusError,
			DenialReason:      "control-plane audit is unavailable",
			DenialCode:        DenialCodeAuditUnavailable,
			ApprovalRequestID: validationResponse.ApprovalRequestID,
			Redacted:          true,
		})
		return
	}

	server.writeJSON(writer, http.StatusOK, validationResponse)
}

func (server *Server) handleMCPGatewayInvocationExecute(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityMCPGatewayWrite) {
		return
	}
	requestBodyBytes, denialResponse, verified := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var executionRequest MCPGatewayExecutionRequest
	if err := decodeJSONBytes(requestBodyBytes, &executionRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialCode:   DenialCodeMalformedRequest,
			DenialReason: err.Error(),
			Redacted:     true,
		})
		return
	}
	if err := executionRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialCode:   DenialCodeMalformedRequest,
			DenialReason: err.Error(),
			Redacted:     true,
		})
		return
	}

	executionResponse, denialResponse, ok := server.executeMCPGatewayInvocation(request.Context(), tokenClaims, executionRequest)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}
	server.writeJSON(writer, http.StatusOK, executionResponse)
}
