package loopgate

import (
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"strings"
)

func (server *Server) handleCapabilityExecute(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}

	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var capabilityRequest controlapipkg.CapabilityRequest
	if err := decodeJSONBytes(requestBodyBytes, &capabilityRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   controlapipkg.DenialCodeMalformedRequest,
		})
		return
	}

	response := server.executeCapabilityRequest(request.Context(), tokenClaims, capabilityRequest, true)
	server.writeJSON(writer, httpStatusForResponse(response), response)
}

func (server *Server) handleApprovalDecision(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	controlSession, ok := server.authenticateApproval(writer, request)
	if !ok {
		return
	}

	approvalID := strings.TrimPrefix(request.URL.Path, "/v1/approvals/")
	approvalID = strings.TrimSuffix(approvalID, "/decision")
	if strings.TrimSpace(approvalID) == "" || strings.Contains(approvalID, "/") {
		server.writeJSON(writer, http.StatusBadRequest, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "invalid approval id",
			DenialCode:   controlapipkg.DenialCodeMalformedRequest,
		})
		return
	}

	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxApprovalBodyBytes, controlSession.ID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var decisionRequest controlapipkg.ApprovalDecisionRequest
	if err := decodeJSONBytes(requestBodyBytes, &decisionRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   controlapipkg.DenialCodeMalformedRequest,
		})
		return
	}
	if err := decisionRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   controlapipkg.DenialCodeMalformedRequest,
		})
		return
	}
	if decisionRequest.Approved {
		pendingApproval, denialResponse, ok := server.validatePendingApprovalDecision(controlSession, approvalID, decisionRequest)
		if !ok {
			if err := server.auditApprovalDecisionDenial(controlSession, approvalID, pendingApproval, denialResponse); err != nil {
				server.writeJSON(writer, http.StatusServiceUnavailable, controlapipkg.CapabilityResponse{
					RequestID:         approvalID,
					Status:            controlapipkg.ResponseStatusError,
					DenialReason:      "control-plane audit is unavailable",
					DenialCode:        controlapipkg.DenialCodeAuditUnavailable,
					ApprovalRequestID: approvalID,
				})
				return
			}
			server.writeJSON(writer, approvalDecisionHTTPStatus(denialResponse.DenialCode), denialResponse)
			return
		}
		if integrityDenial, integrityOK := server.verifyPendingApprovalStoredExecutionBody(pendingApproval); !integrityOK {
			server.writePendingApprovalExecutionIntegrityDenial(writer, controlSession, approvalID, pendingApproval, integrityDenial)
			return
		}
		if _, err := server.commitApprovalGrantConsumed(approvalID, decisionRequest.DecisionNonce, decisionRequest.Reason); err != nil {
			server.writeJSON(writer, http.StatusServiceUnavailable, controlapipkg.CapabilityResponse{
				RequestID:         pendingApproval.Request.RequestID,
				Status:            controlapipkg.ResponseStatusError,
				DenialReason:      "control-plane audit is unavailable",
				DenialCode:        controlapipkg.DenialCodeAuditUnavailable,
				ApprovalRequestID: approvalID,
			})
			return
		}
		executionToken, denialResponse, ok := server.approvedExecutionTokenForPendingApproval(approvalID, pendingApproval)
		if !ok {
			server.writeJSON(writer, approvalDecisionHTTPStatus(denialResponse.DenialCode), denialResponse)
			return
		}
		response := server.executeCapabilityRequest(request.Context(), executionToken, pendingApproval.Request, false)
		response.ApprovalRequestID = approvalID
		server.markApprovalExecutionResult(approvalID, response.Status)
		server.emitUIApprovalResolved(pendingApproval, approvalID, "approved", response.Status)
		server.writeJSON(writer, httpStatusForResponse(response), response)
		return
	}

	pendingApproval, denialResponse, _, ok := server.validateAndRecordApprovalDecision(controlSession, approvalID, decisionRequest)
	if !ok {
		if err := server.auditApprovalDecisionDenial(controlSession, approvalID, pendingApproval, denialResponse); err != nil {
			server.writeJSON(writer, http.StatusServiceUnavailable, controlapipkg.CapabilityResponse{
				RequestID:         approvalID,
				Status:            controlapipkg.ResponseStatusError,
				DenialReason:      "control-plane audit is unavailable",
				DenialCode:        controlapipkg.DenialCodeAuditUnavailable,
				ApprovalRequestID: approvalID,
			})
			return
		}
		server.writeJSON(writer, approvalDecisionHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}
	server.writeJSON(writer, http.StatusOK, controlapipkg.CapabilityResponse{
		RequestID:         pendingApproval.Request.RequestID,
		Status:            controlapipkg.ResponseStatusDenied,
		DenialReason:      "approval denied",
		DenialCode:        controlapipkg.DenialCodeApprovalDenied,
		ApprovalRequestID: approvalID,
	})
	server.emitUIApprovalResolved(pendingApproval, approvalID, "denied", controlapipkg.ResponseStatusDenied)
}
