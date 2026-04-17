package loopgate

import (
	"net/http"
	"sort"
	"strings"
)

func (server *Server) handleControlApprovals(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityApprovalRead) {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	server.mu.Lock()
	server.pruneExpiredLocked()
	approvalSummaries := make([]OperatorApprovalSummary, 0, len(server.approvalState.records))
	for approvalID, pendingApproval := range server.approvalState.records {
		if pendingApproval.State != approvalStatePending {
			continue
		}
		if strings.TrimSpace(tokenClaims.TenantID) != "" && strings.TrimSpace(pendingApproval.ExecutionContext.TenantID) != "" && tokenClaims.TenantID != pendingApproval.ExecutionContext.TenantID {
			continue
		}
		pendingApproval = backfillApprovalManifestLocked(server.approvalState.records, approvalID, pendingApproval)
		approvalSummaries = append(approvalSummaries, operatorApprovalSummaryFromPending(pendingApproval))
	}
	server.mu.Unlock()

	sort.Slice(approvalSummaries, func(leftIndex int, rightIndex int) bool {
		return approvalSummaries[leftIndex].ApprovalRequestID < approvalSummaries[rightIndex].ApprovalRequestID
	})
	server.writeJSON(writer, http.StatusOK, OperatorApprovalsResponse{Approvals: approvalSummaries})
}

func (server *Server) handleControlApprovalDecision(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityApprovalWrite) {
		return
	}

	approvalID := strings.TrimPrefix(request.URL.Path, "/v1/control/approvals/")
	approvalID = strings.TrimSuffix(approvalID, "/decision")
	if strings.TrimSpace(approvalID) == "" || strings.Contains(approvalID, "/") {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "invalid approval id",
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	requestBodyBytes, denialResponse, verified := server.readAndVerifySignedBody(writer, request, maxApprovalBodyBytes, tokenClaims.ControlSessionID)
	if !verified {
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

	operatorSession := controlSession{
		ID:                 tokenClaims.ControlSessionID,
		ActorLabel:         tokenClaims.ActorLabel,
		ClientSessionLabel: tokenClaims.ClientSessionLabel,
		TenantID:           tokenClaims.TenantID,
		UserID:             tokenClaims.UserID,
	}

	if decisionRequest.Approved {
		pendingApproval, denialResponse, ok := server.validatePendingApprovalDecisionForOperator(operatorSession, approvalID, decisionRequest)
		if !ok {
			if err := server.auditApprovalDecisionDenial(operatorSession, approvalID, pendingApproval, denialResponse); err != nil {
				server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
					RequestID:         approvalID,
					Status:            ResponseStatusError,
					DenialReason:      "control-plane audit is unavailable",
					DenialCode:        DenialCodeAuditUnavailable,
					ApprovalRequestID: approvalID,
				})
				return
			}
			server.writeJSON(writer, approvalDecisionHTTPStatus(denialResponse.DenialCode), denialResponse)
			return
		}
		if integrityDenial, integrityOK := server.verifyPendingApprovalStoredExecutionBody(pendingApproval); !integrityOK {
			server.writePendingApprovalExecutionIntegrityDenial(writer, operatorSession, approvalID, pendingApproval, integrityDenial)
			return
		}
		auditEventHash, err := server.commitApprovalGrantConsumed(approvalID, decisionRequest.DecisionNonce, decisionRequest.Reason)
		if err != nil {
			server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
				RequestID:         pendingApproval.Request.RequestID,
				Status:            ResponseStatusError,
				DenialReason:      "control-plane audit is unavailable",
				DenialCode:        DenialCodeAuditUnavailable,
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
		server.writeJSON(writer, httpStatusForResponse(response), OperatorApprovalDecisionResponse{
			CapabilityResponse: response,
			AuditEventHash:     auditEventHash,
		})
		return
	}

	pendingApproval, denialResponse, auditEventHash, ok := server.validateAndRecordOperatorApprovalDecision(operatorSession, approvalID, decisionRequest)
	if !ok {
		if err := server.auditApprovalDecisionDenial(operatorSession, approvalID, pendingApproval, denialResponse); err != nil {
			server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
				RequestID:         approvalID,
				Status:            ResponseStatusError,
				DenialReason:      "control-plane audit is unavailable",
				DenialCode:        DenialCodeAuditUnavailable,
				ApprovalRequestID: approvalID,
			})
			return
		}
		server.writeJSON(writer, approvalDecisionHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}
	server.emitUIApprovalResolved(pendingApproval, approvalID, "denied", ResponseStatusDenied)
	server.writeJSON(writer, http.StatusOK, OperatorApprovalDecisionResponse{
		CapabilityResponse: CapabilityResponse{
			RequestID:         pendingApproval.Request.RequestID,
			Status:            ResponseStatusDenied,
			DenialReason:      "approval denied",
			DenialCode:        DenialCodeApprovalDenied,
			ApprovalRequestID: approvalID,
		},
		AuditEventHash: auditEventHash,
	})
}

func operatorApprovalSummaryFromPending(pendingApproval pendingApproval) OperatorApprovalSummary {
	return OperatorApprovalSummary{
		UIApprovalSummary:      uiApprovalSummaryFromPending(pendingApproval),
		DecisionNonce:          pendingApproval.DecisionNonce,
		ApprovalManifestSHA256: pendingApproval.ApprovalManifestSHA256,
	}
}
