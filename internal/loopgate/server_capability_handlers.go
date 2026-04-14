package loopgate

import (
	"net/http"
	"strings"

	"loopgate/internal/secrets"
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

	var capabilityRequest CapabilityRequest
	if err := decodeJSONBytes(requestBodyBytes, &capabilityRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
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
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "invalid approval id",
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxApprovalBodyBytes, controlSession.ID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var decisionRequest ApprovalDecisionRequest
	if err := decodeJSONBytes(requestBodyBytes, &decisionRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if err := decisionRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if decisionRequest.Approved {
		pendingApproval, denialResponse, ok := server.validatePendingApprovalDecision(controlSession, approvalID, decisionRequest)
		if !ok {
			if strings.TrimSpace(denialResponse.DenialCode) != "" && denialResponse.DenialCode != DenialCodeAuditUnavailable {
				approvalDeniedAuditData := map[string]interface{}{
					"approval_request_id":  approvalID,
					"approval_class":       pendingApproval.Metadata["approval_class"],
					"reason":               secrets.RedactText(denialResponse.DenialReason),
					"denial_code":          denialResponse.DenialCode,
					"control_session_id":   controlSession.ID,
					"actor_label":          controlSession.ActorLabel,
					"client_session_label": controlSession.ClientSessionLabel,
				}
				if approvalClass, okClass := pendingApproval.Metadata["approval_class"].(string); okClass && strings.TrimSpace(approvalClass) != "" {
					approvalDeniedAuditData["approval_class"] = approvalClass
				}
				if err := server.logEvent("approval.denied", controlSession.ID, approvalDeniedAuditData); err != nil {
					server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
						RequestID:         approvalID,
						Status:            ResponseStatusError,
						DenialReason:      "control-plane audit is unavailable",
						DenialCode:        DenialCodeAuditUnavailable,
						ApprovalRequestID: approvalID,
					})
					return
				}
			}
			server.writeJSON(writer, approvalDecisionHTTPStatus(denialResponse.DenialCode), denialResponse)
			return
		}
		if integrityDenial, integrityOK := server.verifyPendingApprovalStoredExecutionBody(pendingApproval); !integrityOK {
			server.writePendingApprovalExecutionIntegrityDenial(writer, controlSession, approvalID, pendingApproval, integrityDenial)
			return
		}
		if err := server.commitApprovalGrantConsumed(approvalID, decisionRequest.DecisionNonce); err != nil {
			server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
				RequestID:         pendingApproval.Request.RequestID,
				Status:            ResponseStatusError,
				DenialReason:      "control-plane audit is unavailable",
				DenialCode:        DenialCodeAuditUnavailable,
				ApprovalRequestID: approvalID,
			})
			return
		}
		response := server.executeCapabilityRequest(request.Context(), capabilityToken{
			TokenID:             "approved:" + approvalID,
			ControlSessionID:    pendingApproval.ExecutionContext.ControlSessionID,
			ActorLabel:          pendingApproval.ExecutionContext.ActorLabel,
			ClientSessionLabel:  pendingApproval.ExecutionContext.ClientSessionLabel,
			AllowedCapabilities: copyCapabilitySet(pendingApproval.ExecutionContext.AllowedCapabilities),
			PeerIdentity:        controlSession.PeerIdentity,
			TenantID:            controlSession.TenantID,
			UserID:              controlSession.UserID,
			ExpiresAt:           controlSession.ExpiresAt,
			SingleUse:           true,
			ApprovedExecution:   true,
			BoundCapability:     pendingApproval.Request.Capability,
			BoundArgumentHash:   normalizedArgumentHash(pendingApproval.Request.Arguments),
		}, pendingApproval.Request, false)
		response.ApprovalRequestID = approvalID
		server.markApprovalExecutionResult(approvalID, response.Status)
		server.emitUIApprovalResolved(pendingApproval, approvalID, "approved", response.Status)
		server.writeJSON(writer, httpStatusForResponse(response), response)
		return
	}

	pendingApproval, denialResponse, ok := server.validateAndRecordApprovalDecision(controlSession, approvalID, decisionRequest)
	if !ok {
		if strings.TrimSpace(denialResponse.DenialCode) != "" && denialResponse.DenialCode != DenialCodeAuditUnavailable {
			approvalDeniedAuditData := map[string]interface{}{
				"approval_request_id":  approvalID,
				"approval_class":       pendingApproval.Metadata["approval_class"],
				"reason":               secrets.RedactText(denialResponse.DenialReason),
				"denial_code":          denialResponse.DenialCode,
				"control_session_id":   controlSession.ID,
				"actor_label":          controlSession.ActorLabel,
				"client_session_label": controlSession.ClientSessionLabel,
			}
			if approvalClass, okClass := pendingApproval.Metadata["approval_class"].(string); okClass && strings.TrimSpace(approvalClass) != "" {
				approvalDeniedAuditData["approval_class"] = approvalClass
			}
			if err := server.logEvent("approval.denied", controlSession.ID, approvalDeniedAuditData); err != nil {
				server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
					RequestID:         approvalID,
					Status:            ResponseStatusError,
					DenialReason:      "control-plane audit is unavailable",
					DenialCode:        DenialCodeAuditUnavailable,
					ApprovalRequestID: approvalID,
				})
				return
			}
		}
		server.writeJSON(writer, approvalDecisionHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}
	server.writeJSON(writer, http.StatusOK, CapabilityResponse{
		RequestID:         pendingApproval.Request.RequestID,
		Status:            ResponseStatusDenied,
		DenialReason:      "approval denied",
		DenialCode:        DenialCodeApprovalDenied,
		ApprovalRequestID: approvalID,
	})
	server.emitUIApprovalResolved(pendingApproval, approvalID, "denied", ResponseStatusDenied)
}

func ternaryApprovalDecision(approved bool) string {
	if approved {
		return "approved"
	}
	return "denied"
}
