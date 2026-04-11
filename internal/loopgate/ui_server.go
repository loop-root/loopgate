package loopgate

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"morph/internal/config"
	"morph/internal/secrets"
	statepkg "morph/internal/state"
)

const maxUIEventBuffer = 200

// havenTrustedSandboxAllowlistMode summarizes how capability names are filtered for Haven auto-allow (see config.Policy.HavenTrustedSandboxAutoAllowMatchesCapability).
func havenTrustedSandboxAllowlistMode(policy config.Policy) string {
	listPtr := policy.Safety.HavenTrustedSandboxAutoAllowCapabilities
	if listPtr == nil {
		return "all"
	}
	list := *listPtr
	if len(list) == 0 {
		return "none"
	}
	return "restricted"
}

type uiEventSubscriber struct {
	controlSessionID string
	id               int
	events           chan UIEventEnvelope
}

var errOperatorMountWriteGrantNotFound = errors.New("operator mount write grant not found")
var errOperatorMountWriteGrantRenewalRequiresApproval = errors.New("operator mount write grant renewal requires a fresh approval path")

func (server *Server) handleUIStatus(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	personaName, personaVersion := server.loadPersonaDisplaySummary()
	runtimeState := server.loadRuntimeStateDisplaySummary()

	server.mu.Lock()
	server.pruneExpiredLocked()
	nowUTC := server.now().UTC()
	pendingCount := 0
	writeGrants := make([]UIOperatorMountWriteGrant, 0)
	for _, pendingApproval := range server.approvals {
		if pendingApproval.ControlSessionID == tokenClaims.ControlSessionID && pendingApproval.State == approvalStatePending {
			pendingCount++
		}
	}
	if controlSession, found := server.sessions[tokenClaims.ControlSessionID]; found {
		writeGrants = uiOperatorMountWriteGrantsLocked(controlSession, nowUTC)
	}
	server.mu.Unlock()

	response := UIStatusResponse{
		Version:                  statusVersion,
		PersonaName:              personaName,
		PersonaVersion:           personaVersion,
		ControlSessionID:         tokenClaims.ControlSessionID,
		ActorLabel:               tokenClaims.ActorLabel,
		ClientSessionLabel:       tokenClaims.ClientSessionLabel,
		RuntimeSessionID:         runtimeState.SessionID,
		TurnCount:                runtimeState.TurnCount,
		DistillCursorLine:        runtimeState.DistillCursorLine,
		PendingApprovals:         pendingCount,
		ActiveMorphlings:         server.activeMorphlingCount(server.now().UTC()),
		CapabilityCount:          len(tokenClaims.AllowedCapabilities),
		ConnectionCount:          len(server.connectionStatuses()),
		OperatorMountWriteGrants: writeGrants,
		Policy: UIStatusPolicySummary{
			ReadEnabled:                      server.policy.Tools.Filesystem.ReadEnabled,
			WriteEnabled:                     server.policy.Tools.Filesystem.WriteEnabled,
			WriteRequiresApproval:            server.policy.Tools.Filesystem.WriteRequiresApproval,
			HavenTrustedSandboxAutoAllow:     server.policy.HavenTrustedSandboxAutoAllowEnabled(),
			HavenTrustedSandboxAllowlistMode: havenTrustedSandboxAllowlistMode(server.policy),
		},
	}
	server.writeJSON(writer, http.StatusOK, response)
}

func (server *Server) handleUIOperatorMountWriteGrants(writer http.ResponseWriter, request *http.Request) {
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityOperatorMountWriteGrant) {
		return
	}

	switch request.Method {
	case http.MethodPut:
		requestBodyBytes, denialResponse, verified := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
		if !verified {
			server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
			return
		}
		var updateRequest UIOperatorMountWriteGrantUpdateRequest
		if err := decodeJSONBytes(requestBodyBytes, &updateRequest); err != nil {
			server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
				Status:       ResponseStatusError,
				DenialReason: err.Error(),
				DenialCode:   DenialCodeMalformedRequest,
			})
			return
		}
		if err := updateRequest.Validate(); err != nil {
			server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
				Status:       ResponseStatusError,
				DenialReason: err.Error(),
				DenialCode:   DenialCodeMalformedRequest,
			})
			return
		}
		statusResponse, err := server.updateOperatorMountWriteGrant(tokenClaims, updateRequest)
		if err != nil {
			statusCode := http.StatusServiceUnavailable
			denialCode := DenialCodeExecutionFailed
			if errors.Is(err, errOperatorMountWriteGrantNotFound) {
				statusCode = http.StatusNotFound
				denialCode = DenialCodeMalformedRequest
			} else if errors.Is(err, errOperatorMountWriteGrantRenewalRequiresApproval) {
				statusCode = http.StatusForbidden
				denialCode = DenialCodeApprovalRequired
			}
			server.writeJSON(writer, statusCode, CapabilityResponse{
				Status:       ResponseStatusError,
				DenialReason: err.Error(),
				DenialCode:   denialCode,
			})
			return
		}
		server.writeJSON(writer, http.StatusOK, statusResponse)
	default:
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func uiOperatorMountWriteGrantsLocked(controlSession controlSession, nowUTC time.Time) []UIOperatorMountWriteGrant {
	writeGrants := make([]UIOperatorMountWriteGrant, 0, len(controlSession.OperatorMountWriteGrants))
	for grantRootPath, grantExpiresAtUTC := range controlSession.OperatorMountWriteGrants {
		if strings.TrimSpace(grantRootPath) == "" {
			continue
		}
		if !grantExpiresAtUTC.IsZero() && !grantExpiresAtUTC.After(nowUTC) {
			delete(controlSession.OperatorMountWriteGrants, grantRootPath)
			continue
		}
		writeGrant := UIOperatorMountWriteGrant{
			RootPath: strings.TrimSpace(grantRootPath),
		}
		if !grantExpiresAtUTC.IsZero() {
			writeGrant.ExpiresAtUTC = grantExpiresAtUTC.UTC().Format(time.RFC3339Nano)
		}
		writeGrants = append(writeGrants, writeGrant)
	}
	sort.Slice(writeGrants, func(leftIndex, rightIndex int) bool {
		return writeGrants[leftIndex].RootPath < writeGrants[rightIndex].RootPath
	})
	return writeGrants
}

func (server *Server) updateOperatorMountWriteGrant(tokenClaims capabilityToken, updateRequest UIOperatorMountWriteGrantUpdateRequest) (UIOperatorMountWriteGrantStatusResponse, error) {
	nowUTC := server.now().UTC()
	normalizedRootPath := filepath.Clean(strings.TrimSpace(updateRequest.RootPath))

	server.mu.Lock()
	defer server.mu.Unlock()

	server.pruneExpiredLocked()
	controlSession, found := server.sessions[tokenClaims.ControlSessionID]
	if !found {
		return UIOperatorMountWriteGrantStatusResponse{}, errOperatorMountWriteGrantNotFound
	}

	mountedRootFound := false
	for _, mountedRootPath := range controlSession.OperatorMountPaths {
		if mountedRootPath == normalizedRootPath {
			mountedRootFound = true
			break
		}
	}
	if !mountedRootFound {
		return UIOperatorMountWriteGrantStatusResponse{}, fmt.Errorf("%w: %s", errOperatorMountWriteGrantNotFound, normalizedRootPath)
	}

	currentGrantExpiresAtUTC, grantFound := controlSession.OperatorMountWriteGrants[normalizedRootPath]
	if !grantFound || (!currentGrantExpiresAtUTC.IsZero() && !currentGrantExpiresAtUTC.After(nowUTC)) {
		delete(controlSession.OperatorMountWriteGrants, normalizedRootPath)
		return UIOperatorMountWriteGrantStatusResponse{}, fmt.Errorf("%w: %s", errOperatorMountWriteGrantNotFound, normalizedRootPath)
	}

	updatedGrantExpiresAtUTC := currentGrantExpiresAtUTC
	switch strings.TrimSpace(updateRequest.Action) {
	case OperatorMountWriteGrantActionRevoke:
		delete(controlSession.OperatorMountWriteGrants, normalizedRootPath)
	case OperatorMountWriteGrantActionRenew:
		return UIOperatorMountWriteGrantStatusResponse{}, errOperatorMountWriteGrantRenewalRequiresApproval
	default:
		return UIOperatorMountWriteGrantStatusResponse{}, fmt.Errorf("unsupported operator mount write grant action %q", updateRequest.Action)
	}

	auditData := map[string]interface{}{
		"root_path":            normalizedRootPath,
		"action":               strings.TrimSpace(updateRequest.Action),
		"previous_expires_at":  currentGrantExpiresAtUTC.UTC().Format(time.RFC3339Nano),
		"control_session_id":   tokenClaims.ControlSessionID,
		"actor_label":          tokenClaims.ActorLabel,
		"client_session_label": tokenClaims.ClientSessionLabel,
		"tenant_id":            controlSession.TenantID,
		"user_id":              controlSession.UserID,
	}
	if strings.TrimSpace(updateRequest.Action) == OperatorMountWriteGrantActionRenew {
		auditData["expires_at_utc"] = updatedGrantExpiresAtUTC.UTC().Format(time.RFC3339Nano)
	}
	if err := server.logEvent("operator_mount.write_grant.updated", tokenClaims.ControlSessionID, auditData); err != nil {
		if strings.TrimSpace(updateRequest.Action) == OperatorMountWriteGrantActionRevoke {
			controlSession.OperatorMountWriteGrants[normalizedRootPath] = currentGrantExpiresAtUTC
		} else {
			controlSession.OperatorMountWriteGrants[normalizedRootPath] = currentGrantExpiresAtUTC
		}
		server.sessions[tokenClaims.ControlSessionID] = controlSession
		return UIOperatorMountWriteGrantStatusResponse{}, err
	}

	server.sessions[tokenClaims.ControlSessionID] = controlSession
	return UIOperatorMountWriteGrantStatusResponse{
		Grants: uiOperatorMountWriteGrantsLocked(controlSession, nowUTC),
	}, nil
}

func (server *Server) handleUIApprovals(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	controlSession, ok := server.authenticateApproval(writer, request)
	if !ok {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, controlSession.ID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	server.mu.Lock()
	server.pruneExpiredLocked()
	approvalSummaries := make([]UIApprovalSummary, 0, len(server.approvals))
	for _, pendingApproval := range server.approvals {
		if pendingApproval.ControlSessionID != controlSession.ID || pendingApproval.State != approvalStatePending {
			continue
		}
		approvalSummaries = append(approvalSummaries, uiApprovalSummaryFromPending(pendingApproval))
	}
	server.mu.Unlock()

	sort.Slice(approvalSummaries, func(leftIndex int, rightIndex int) bool {
		return approvalSummaries[leftIndex].ApprovalRequestID < approvalSummaries[rightIndex].ApprovalRequestID
	})
	server.writeJSON(writer, http.StatusOK, UIApprovalsResponse{Approvals: approvalSummaries})
}

func (server *Server) handleUIApprovalDecision(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	controlSession, ok := server.authenticateApproval(writer, request)
	if !ok {
		return
	}

	approvalID := strings.TrimPrefix(request.URL.Path, "/v1/ui/approvals/")
	approvalID = strings.TrimSuffix(approvalID, "/decision")
	if strings.TrimSpace(approvalID) == "" || strings.Contains(approvalID, "/") {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "invalid approval id",
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	requestBodyBytes, denialResponse, verified := server.readAndVerifySignedBody(writer, request, maxApprovalBodyBytes, controlSession.ID)
	if !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var uiDecisionRequest UIApprovalDecisionRequest
	if err := decodeJSONBytes(requestBodyBytes, &uiDecisionRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if err := uiDecisionRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if err := server.expirePendingMorphlingApprovals(); err != nil {
		server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
			RequestID:         approvalID,
			Status:            ResponseStatusError,
			DenialReason:      "control-plane audit is unavailable",
			DenialCode:        DenialCodeAuditUnavailable,
			ApprovalRequestID: approvalID,
		})
		return
	}

	decisionNonce, manifestSHA256, found := server.currentApprovalDecisionState(approvalID)
	if !found {
		server.writeJSON(writer, http.StatusNotFound, CapabilityResponse{
			RequestID:         approvalID,
			Status:            ResponseStatusDenied,
			DenialReason:      "approval request not found",
			DenialCode:        DenialCodeApprovalNotFound,
			ApprovalRequestID: approvalID,
		})
		return
	}

	approvalDecisionPayload := ApprovalDecisionRequest{
		Approved:               *uiDecisionRequest.Approved,
		DecisionNonce:          decisionNonce,
		ApprovalManifestSHA256: manifestSHA256,
	}

	if *uiDecisionRequest.Approved {
		pendingApproval, denialResponse, ok := server.validatePendingApprovalDecision(controlSession, approvalID, approvalDecisionPayload)
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
				server.emitUIApprovalResolved(pendingApproval, approvalID, "denied", ResponseStatusDenied)
			}
			server.writeJSON(writer, approvalDecisionHTTPStatus(denialResponse.DenialCode), denialResponse)
			return
		}
		if integrityDenial, integrityOK := server.verifyPendingApprovalStoredExecutionBody(pendingApproval); !integrityOK {
			server.writePendingApprovalExecutionIntegrityDenial(writer, controlSession, approvalID, pendingApproval, integrityDenial)
			return
		}
		if isMorphlingSpawnApproval(pendingApproval) {
			response, err := server.resolveMorphlingSpawnApproval(pendingApproval, true, decisionNonce)
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
			if response.Status != ResponseStatusSuccess {
				response.ApprovalRequestID = approvalID
				server.emitUIApprovalResolved(pendingApproval, approvalID, ternaryApprovalDecision(true), response.Status)
				server.writeJSON(writer, httpStatusForResponse(response), response)
				return
			}
			if err := server.commitApprovalGrantConsumed(approvalID, decisionNonce); err != nil {
				server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
					RequestID:         pendingApproval.Request.RequestID,
					Status:            ResponseStatusError,
					DenialReason:      "control-plane audit is unavailable",
					DenialCode:        DenialCodeAuditUnavailable,
					ApprovalRequestID: approvalID,
				})
				return
			}
			server.markApprovalExecutionResult(approvalID, ResponseStatusSuccess)
			response.ApprovalRequestID = approvalID
			server.emitUIApprovalResolved(pendingApproval, approvalID, "approved", response.Status)
			server.writeJSON(writer, httpStatusForResponse(response), response)
			return
		}
		if err := server.commitApprovalGrantConsumed(approvalID, decisionNonce); err != nil {
			server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
				RequestID:         pendingApproval.Request.RequestID,
				Status:            ResponseStatusError,
				DenialReason:      "control-plane audit is unavailable",
				DenialCode:        DenialCodeAuditUnavailable,
				ApprovalRequestID: approvalID,
			})
			return
		}
		// NOTE: This runs the full capability (e.g. host.plan.apply) before the HTTP response returns.
		// Haven and other UIs should show a long-running indicator on Approve — large plans can take many seconds.
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
		}, pendingApproval.Request, false)
		response.ApprovalRequestID = approvalID
		server.markApprovalExecutionResult(approvalID, response.Status)
		server.emitUIApprovalResolved(pendingApproval, approvalID, "approved", response.Status)
		server.writeJSON(writer, httpStatusForResponse(response), response)
		return
	}

	pendingApproval, denialResponse, validated := server.validateAndRecordApprovalDecision(controlSession, approvalID, approvalDecisionPayload)
	if !validated {
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
			if approvalClass, ok := pendingApproval.Metadata["approval_class"].(string); ok && strings.TrimSpace(approvalClass) != "" {
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
			server.emitUIApprovalResolved(pendingApproval, approvalID, "denied", ResponseStatusDenied)
		}
		server.writeJSON(writer, approvalDecisionHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}
	if isMorphlingSpawnApproval(pendingApproval) {
		response, err := server.resolveMorphlingSpawnApproval(pendingApproval, false, decisionNonce)
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
		response.ApprovalRequestID = approvalID
		server.emitUIApprovalResolved(pendingApproval, approvalID, "denied", response.Status)
		server.writeJSON(writer, httpStatusForResponse(response), response)
		return
	}

	server.emitUIApprovalResolved(pendingApproval, approvalID, "denied", ResponseStatusDenied)
	server.writeJSON(writer, http.StatusOK, CapabilityResponse{
		RequestID:         pendingApproval.Request.RequestID,
		Status:            ResponseStatusDenied,
		DenialReason:      "approval denied",
		DenialCode:        DenialCodeApprovalDenied,
		ApprovalRequestID: approvalID,
	})
}

func (server *Server) handleUIEvents(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	lastEventID := strings.TrimSpace(request.Header.Get("Last-Event-ID"))
	replayEvents := server.uiReplayEvents(tokenClaims.ControlSessionID, lastEventID)
	subscriber := server.addUISubscriber(tokenClaims.ControlSessionID)
	defer server.removeUISubscriber(subscriber.id)

	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")

	for _, replayEvent := range replayEvents {
		if err := writeSSEEvent(writer, replayEvent); err != nil {
			return
		}
		flusher.Flush()
	}

	for {
		select {
		case <-request.Context().Done():
			return
		case uiEventEnvelope := <-subscriber.events:
			if err := writeSSEEvent(writer, uiEventEnvelope); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeSSEEvent(writer http.ResponseWriter, uiEventEnvelope UIEventEnvelope) error {
	encodedEvent, err := json.Marshal(uiEventEnvelope)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "id: %s\n", uiEventEnvelope.ID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "event: %s\n", uiEventEnvelope.Type); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "data: %s\n\n", encodedEvent); err != nil {
		return err
	}
	return nil
}

func (server *Server) verifySignedRequestWithoutBody(request *http.Request, controlSessionID string) ([]byte, CapabilityResponse, bool) {
	requestBodyBytes := []byte{}
	denialResponse, verified := server.verifySignedRequest(request, requestBodyBytes, controlSessionID)
	if !verified {
		return nil, denialResponse, false
	}
	return requestBodyBytes, CapabilityResponse{}, true
}

// currentApprovalDecisionState returns the decision nonce and manifest SHA256 for a pending approval.
// The manifest SHA256 is included in the server-side decision path so that the UI approval handler
// can forward it to validateAndRecordApprovalDecision for manifest verification.
func (server *Server) currentApprovalDecisionState(approvalID string) (nonce, manifestSHA256 string, found bool) {
	server.mu.Lock()
	defer server.mu.Unlock()

	approval, found := server.approvals[approvalID]
	if !found {
		return "", "", false
	}
	approval = backfillApprovalManifestLocked(server.approvals, approvalID, approval)
	return approval.DecisionNonce, approval.ApprovalManifestSHA256, true
}

func (server *Server) loadPersonaDisplaySummary() (string, string) {
	persona, err := config.LoadPersona(server.repoRoot)
	if err != nil {
		return "Morph", "unknown"
	}
	return persona.Name, persona.Version
}

func (server *Server) loadRuntimeStateDisplaySummary() statepkg.RuntimeState {
	statePath := filepath.Join(server.repoRoot, "runtime", "state", "working_state.json")
	rawStateBytes, err := os.ReadFile(statePath)
	if err != nil {
		return statepkg.RuntimeState{}
	}

	var runtimeState statepkg.RuntimeState
	if err := json.Unmarshal(rawStateBytes, &runtimeState); err != nil {
		return statepkg.RuntimeState{}
	}
	return runtimeState
}

func uiApprovalSummaryFromPending(pendingApproval pendingApproval) UIApprovalSummary {
	preview := ""
	redacted := false
	if contentBytes, ok := pendingApproval.Metadata["content_bytes"].(int); ok && contentBytes > 0 {
		preview = "[content hidden]"
		redacted = true
	} else if contentBytesFloat, ok := pendingApproval.Metadata["content_bytes"].(float64); ok && int(contentBytesFloat) > 0 {
		preview = "[content hidden]"
		redacted = true
	}

	summary := UIApprovalSummary{
		ApprovalRequestID: pendingApproval.ID,
		Capability:        pendingApproval.Request.Capability,
		Path:              stringMetadataValue(pendingApproval.Metadata, "path"),
		ContentBytes:      intMetadataValue(pendingApproval.Metadata, "content_bytes"),
		Preview:           preview,
		Redacted:          redacted,
		Reason:            pendingApproval.Reason,
		ExpiresAtUTC:      pendingApproval.ExpiresAt.Format(timeLayoutRFC3339Nano),
	}
	if pendingApproval.Request.Capability == "host.plan.apply" {
		summary.OperatorIntentLine = stringMetadataValue(pendingApproval.Metadata, "operator_intent_line")
		summary.PlanSummary = stringMetadataValue(pendingApproval.Metadata, "plan_summary")
		summary.HostFolderDisplayName = stringMetadataValue(pendingApproval.Metadata, "host_folder_display_name")
		summary.PlanOperationCount = intMetadataValue(pendingApproval.Metadata, "plan_operation_count")
		summary.PlanMkdirCount = intMetadataValue(pendingApproval.Metadata, "plan_mkdir_count")
		summary.PlanMoveCount = intMetadataValue(pendingApproval.Metadata, "plan_move_count")
	}
	return summary
}

const timeLayoutRFC3339Nano = "2006-01-02T15:04:05.999999999Z07:00"

func stringMetadataValue(metadata map[string]interface{}, key string) string {
	rawValue, found := metadata[key]
	if !found {
		return ""
	}
	switch typedValue := rawValue.(type) {
	case string:
		return typedValue
	default:
		return fmt.Sprintf("%v", typedValue)
	}
}

func intMetadataValue(metadata map[string]interface{}, key string) int {
	rawValue, found := metadata[key]
	if !found {
		return 0
	}
	switch typedValue := rawValue.(type) {
	case int:
		return typedValue
	case int64:
		return int(typedValue)
	case float64:
		return int(typedValue)
	default:
		return 0
	}
}

func (server *Server) addUISubscriber(controlSessionID string) uiEventSubscriber {
	server.uiMu.Lock()
	defer server.uiMu.Unlock()

	server.nextUISubscriberID++
	subscriber := uiEventSubscriber{
		controlSessionID: controlSessionID,
		id:               server.nextUISubscriberID,
		events:           make(chan UIEventEnvelope, 16),
	}
	if server.uiSubscribers == nil {
		server.uiSubscribers = make(map[int]uiEventSubscriber)
	}
	server.uiSubscribers[subscriber.id] = subscriber
	return subscriber
}

func (server *Server) removeUISubscriber(subscriberID int) {
	server.uiMu.Lock()
	defer server.uiMu.Unlock()

	delete(server.uiSubscribers, subscriberID)
}

func (server *Server) uiReplayEvents(controlSessionID string, lastEventID string) []UIEventEnvelope {
	server.uiMu.Lock()
	defer server.uiMu.Unlock()

	if len(server.uiEvents) == 0 {
		return nil
	}

	if strings.TrimSpace(lastEventID) == "" {
		replayedEvents := make([]UIEventEnvelope, 0, len(server.uiEvents))
		for _, uiEventEnvelope := range server.uiEvents {
			if uiEventBelongsToControlSession(uiEventEnvelope, controlSessionID) {
				replayedEvents = append(replayedEvents, uiEventEnvelope)
			}
		}
		return replayedEvents
	}

	lastSeenSequence, err := strconv.ParseUint(lastEventID, 10, 64)
	if err != nil {
		return nil
	}

	replayedEvents := make([]UIEventEnvelope, 0, len(server.uiEvents))
	for _, uiEventEnvelope := range server.uiEvents {
		eventSequence, err := strconv.ParseUint(uiEventEnvelope.ID, 10, 64)
		if err != nil || eventSequence <= lastSeenSequence {
			continue
		}
		if uiEventBelongsToControlSession(uiEventEnvelope, controlSessionID) {
			replayedEvents = append(replayedEvents, uiEventEnvelope)
		}
	}
	return replayedEvents
}

func (server *Server) emitUIEvent(controlSessionID string, eventType string, eventData interface{}) {
	uiEventEnvelope := UIEventEnvelope{
		ControlSessionID: controlSessionID,
		Type:             eventType,
		TS:               server.now().UTC().Format(timeLayoutRFC3339Nano),
		Data:             eventData,
	}
	if err := validateUIEventEnvelope(UIEventEnvelope{
		ID:   "pending",
		Type: uiEventEnvelope.Type,
		TS:   uiEventEnvelope.TS,
		Data: uiEventEnvelope.Data,
	}); err != nil {
		return
	}

	server.uiMu.Lock()
	server.uiSequence++
	uiEventEnvelope.ID = strconv.FormatUint(server.uiSequence, 10)
	server.uiEvents = append(server.uiEvents, uiEventEnvelope)
	if len(server.uiEvents) > maxUIEventBuffer {
		server.uiEvents = append([]UIEventEnvelope(nil), server.uiEvents[len(server.uiEvents)-maxUIEventBuffer:]...)
	}
	subscribers := make([]uiEventSubscriber, 0, len(server.uiSubscribers))
	for _, subscriber := range server.uiSubscribers {
		subscribers = append(subscribers, subscriber)
	}
	server.uiMu.Unlock()

	for _, subscriber := range subscribers {
		if subscriber.controlSessionID != uiEventEnvelope.ControlSessionID {
			continue
		}
		select {
		case subscriber.events <- uiEventEnvelope:
		default:
		}
	}
}

func uiEventBelongsToControlSession(uiEventEnvelope UIEventEnvelope, controlSessionID string) bool {
	return uiEventEnvelope.ControlSessionID == controlSessionID
}

func buildUIToolResultEvent(capability string, capabilityResponse CapabilityResponse) UIEventToolResult {
	uiEventToolResult := UIEventToolResult{
		RequestID:  capabilityResponse.RequestID,
		Capability: capability,
	}
	if pathValue, ok := capabilityResponse.StructuredResult["path"].(string); ok {
		uiEventToolResult.Path = pathValue
	}
	if byteValue, found := capabilityResponse.StructuredResult["bytes"]; found {
		switch typedByteValue := byteValue.(type) {
		case int:
			uiEventToolResult.Bytes = typedByteValue
		case float64:
			uiEventToolResult.Bytes = int(typedByteValue)
		}
	}
	if messageValue, ok := capabilityResponse.StructuredResult["message"].(string); ok {
		uiEventToolResult.Message = messageValue
	}
	if contentValue, ok := capabilityResponse.StructuredResult["content"].(string); ok {
		uiEventToolResult.Content = contentValue
	}
	if entriesValue, ok := capabilityResponse.StructuredResult["entries"].([]interface{}); ok {
		uiEventToolResult.EntryCount = len(entriesValue)
	}
	if entriesValue, ok := capabilityResponse.StructuredResult["entries"].([]string); ok {
		uiEventToolResult.EntryCount = len(entriesValue)
	}

	classification, err := capabilityResponse.ResultClassification()
	if err == nil {
		uiEventToolResult.PromptEligible = classification.PromptEligible()
		uiEventToolResult.MemoryEligible = classification.MemoryEligible()
		uiEventToolResult.DisplayOnly = classification.DisplayOnly()
		uiEventToolResult.Quarantined = classification.Quarantined()
	}
	if capabilityResponse.QuarantineRef != "" || uiEventToolResult.Quarantined {
		uiEventToolResult.Quarantined = true
		uiEventToolResult.QuarantineNotice = "result quarantined by Loopgate"
		uiEventToolResult.Content = ""
		uiEventToolResult.Message = ""
	}
	return uiEventToolResult
}

func (server *Server) emitUIApprovalResolved(pendingApproval pendingApproval, approvalID string, decision string, status string) {
	server.emitUIEvent(pendingApproval.ControlSessionID, UIEventTypeApprovalResolved, UIEventApprovalResolved{
		ApprovalRequestID: approvalID,
		Capability:        pendingApproval.Request.Capability,
		Decision:          decision,
		Status:            status,
	})
}

func (server *Server) emitUIToolDenied(controlSessionID string, capabilityRequest CapabilityRequest, denialCode string, denialReason string) {
	server.emitUIEvent(controlSessionID, UIEventTypeToolDenied, UIEventToolDenied{
		RequestID:    capabilityRequest.RequestID,
		Capability:   capabilityRequest.Capability,
		DenialCode:   denialCode,
		DenialReason: denialReason,
	})
}

func (server *Server) emitUIToolResult(controlSessionID string, capabilityRequest CapabilityRequest, capabilityResponse CapabilityResponse) {
	server.emitUIEvent(controlSessionID, UIEventTypeToolResult, buildUIToolResultEvent(capabilityRequest.Capability, capabilityResponse))
}

func (server *Server) emitUIApprovalPending(pendingApproval pendingApproval) {
	server.emitUIEvent(pendingApproval.ControlSessionID, UIEventTypeApprovalPending, UIEventApprovalPending{
		ApprovalRequestID: pendingApproval.ID,
		Capability:        pendingApproval.Request.Capability,
		Path:              stringMetadataValue(pendingApproval.Metadata, "path"),
		ContentBytes:      intMetadataValue(pendingApproval.Metadata, "content_bytes"),
		Preview:           uiApprovalSummaryFromPending(pendingApproval).Preview,
		Redacted:          uiApprovalSummaryFromPending(pendingApproval).Redacted,
		Reason:            pendingApproval.Reason,
		ExpiresAtUTC:      pendingApproval.ExpiresAt.Format(timeLayoutRFC3339Nano),
	})
}
