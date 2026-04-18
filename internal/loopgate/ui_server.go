package loopgate

import (
	"encoding/json"
	"errors"
	"fmt"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"loopgate/internal/config"
	statepkg "loopgate/internal/state"
)

const maxUIEventBuffer = 200

type uiEventSubscriber struct {
	controlSessionID string
	id               int
	events           chan controlapipkg.UIEventEnvelope
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
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityUIRead) {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	personaName, personaVersion := server.loadPersonaDisplaySummary()
	runtimeState := server.loadRuntimeStateDisplaySummary()
	policyRuntime := server.currentPolicyRuntime()

	server.mu.Lock()
	server.pruneExpiredLocked()
	nowUTC := server.now().UTC()
	pendingCount := 0
	writeGrants := make([]controlapipkg.UIOperatorMountWriteGrant, 0)
	for _, pendingApproval := range server.approvalState.records {
		if pendingApproval.ControlSessionID == tokenClaims.ControlSessionID && pendingApproval.State == approvalStatePending {
			pendingCount++
		}
	}
	if controlSession, found := server.sessionState.sessions[tokenClaims.ControlSessionID]; found {
		writeGrants = uiOperatorMountWriteGrantsLocked(controlSession, nowUTC)
	}
	server.mu.Unlock()

	response := controlapipkg.UIStatusResponse{
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
		CapabilityCount:          len(tokenClaims.AllowedCapabilities),
		ConnectionCount:          len(server.connectionStatuses()),
		OperatorMountWriteGrants: writeGrants,
		Policy: controlapipkg.UIStatusPolicySummary{
			ReadEnabled:           policyRuntime.policy.Tools.Filesystem.ReadEnabled,
			WriteEnabled:          policyRuntime.policy.Tools.Filesystem.WriteEnabled,
			WriteRequiresApproval: policyRuntime.policy.Tools.Filesystem.WriteRequiresApproval,
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
		var updateRequest controlapipkg.UIOperatorMountWriteGrantUpdateRequest
		if err := decodeJSONBytes(requestBodyBytes, &updateRequest); err != nil {
			server.writeJSON(writer, http.StatusBadRequest, controlapipkg.CapabilityResponse{
				Status:       controlapipkg.ResponseStatusError,
				DenialReason: err.Error(),
				DenialCode:   controlapipkg.DenialCodeMalformedRequest,
			})
			return
		}
		if err := updateRequest.Validate(); err != nil {
			server.writeJSON(writer, http.StatusBadRequest, controlapipkg.CapabilityResponse{
				Status:       controlapipkg.ResponseStatusError,
				DenialReason: err.Error(),
				DenialCode:   controlapipkg.DenialCodeMalformedRequest,
			})
			return
		}
		statusResponse, err := server.updateOperatorMountWriteGrant(tokenClaims, updateRequest)
		if err != nil {
			statusCode := http.StatusServiceUnavailable
			denialCode := controlapipkg.DenialCodeExecutionFailed
			if errors.Is(err, errOperatorMountWriteGrantNotFound) {
				statusCode = http.StatusNotFound
				denialCode = controlapipkg.DenialCodeMalformedRequest
			} else if errors.Is(err, errOperatorMountWriteGrantRenewalRequiresApproval) {
				statusCode = http.StatusForbidden
				denialCode = controlapipkg.DenialCodeApprovalRequired
			}
			server.writeJSON(writer, statusCode, controlapipkg.CapabilityResponse{
				Status:       controlapipkg.ResponseStatusError,
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

func uiOperatorMountWriteGrantsLocked(controlSession controlSession, nowUTC time.Time) []controlapipkg.UIOperatorMountWriteGrant {
	writeGrants := make([]controlapipkg.UIOperatorMountWriteGrant, 0, len(controlSession.OperatorMountWriteGrants))
	for grantRootPath, grantExpiresAtUTC := range controlSession.OperatorMountWriteGrants {
		if strings.TrimSpace(grantRootPath) == "" {
			continue
		}
		if !grantExpiresAtUTC.IsZero() && !grantExpiresAtUTC.After(nowUTC) {
			delete(controlSession.OperatorMountWriteGrants, grantRootPath)
			continue
		}
		writeGrant := controlapipkg.UIOperatorMountWriteGrant{
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

func (server *Server) updateOperatorMountWriteGrant(tokenClaims capabilityToken, updateRequest controlapipkg.UIOperatorMountWriteGrantUpdateRequest) (controlapipkg.UIOperatorMountWriteGrantStatusResponse, error) {
	nowUTC := server.now().UTC()
	normalizedRootPath := filepath.Clean(strings.TrimSpace(updateRequest.RootPath))

	server.mu.Lock()
	defer server.mu.Unlock()

	server.pruneExpiredLocked()
	controlSession, found := server.sessionState.sessions[tokenClaims.ControlSessionID]
	if !found {
		return controlapipkg.UIOperatorMountWriteGrantStatusResponse{}, errOperatorMountWriteGrantNotFound
	}

	mountedRootFound := false
	for _, mountedRootPath := range controlSession.OperatorMountPaths {
		if mountedRootPath == normalizedRootPath {
			mountedRootFound = true
			break
		}
	}
	if !mountedRootFound {
		return controlapipkg.UIOperatorMountWriteGrantStatusResponse{}, fmt.Errorf("%w: %s", errOperatorMountWriteGrantNotFound, normalizedRootPath)
	}

	currentGrantExpiresAtUTC, grantFound := controlSession.OperatorMountWriteGrants[normalizedRootPath]
	if !grantFound || (!currentGrantExpiresAtUTC.IsZero() && !currentGrantExpiresAtUTC.After(nowUTC)) {
		delete(controlSession.OperatorMountWriteGrants, normalizedRootPath)
		return controlapipkg.UIOperatorMountWriteGrantStatusResponse{}, fmt.Errorf("%w: %s", errOperatorMountWriteGrantNotFound, normalizedRootPath)
	}

	updatedGrantExpiresAtUTC := currentGrantExpiresAtUTC
	switch strings.TrimSpace(updateRequest.Action) {
	case controlapipkg.OperatorMountWriteGrantActionRevoke:
		delete(controlSession.OperatorMountWriteGrants, normalizedRootPath)
	case controlapipkg.OperatorMountWriteGrantActionRenew:
		return controlapipkg.UIOperatorMountWriteGrantStatusResponse{}, errOperatorMountWriteGrantRenewalRequiresApproval
	default:
		return controlapipkg.UIOperatorMountWriteGrantStatusResponse{}, fmt.Errorf("unsupported operator mount write grant action %q", updateRequest.Action)
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
	if strings.TrimSpace(updateRequest.Action) == controlapipkg.OperatorMountWriteGrantActionRenew {
		auditData["expires_at_utc"] = updatedGrantExpiresAtUTC.UTC().Format(time.RFC3339Nano)
	}
	if err := server.logEvent("operator_mount.write_grant.updated", tokenClaims.ControlSessionID, auditData); err != nil {
		if strings.TrimSpace(updateRequest.Action) == controlapipkg.OperatorMountWriteGrantActionRevoke {
			controlSession.OperatorMountWriteGrants[normalizedRootPath] = currentGrantExpiresAtUTC
		} else {
			controlSession.OperatorMountWriteGrants[normalizedRootPath] = currentGrantExpiresAtUTC
		}
		server.sessionState.sessions[tokenClaims.ControlSessionID] = controlSession
		return controlapipkg.UIOperatorMountWriteGrantStatusResponse{}, err
	}

	server.sessionState.sessions[tokenClaims.ControlSessionID] = controlSession
	return controlapipkg.UIOperatorMountWriteGrantStatusResponse{
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
	approvalSummaries := make([]controlapipkg.UIApprovalSummary, 0, len(server.approvalState.records))
	for _, pendingApproval := range server.approvalState.records {
		if pendingApproval.ControlSessionID != controlSession.ID || pendingApproval.State != approvalStatePending {
			continue
		}
		approvalSummaries = append(approvalSummaries, uiApprovalSummaryFromPending(pendingApproval))
	}
	server.mu.Unlock()

	sort.Slice(approvalSummaries, func(leftIndex int, rightIndex int) bool {
		return approvalSummaries[leftIndex].ApprovalRequestID < approvalSummaries[rightIndex].ApprovalRequestID
	})
	server.writeJSON(writer, http.StatusOK, controlapipkg.UIApprovalsResponse{Approvals: approvalSummaries})
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
		server.writeJSON(writer, http.StatusBadRequest, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "invalid approval id",
			DenialCode:   controlapipkg.DenialCodeMalformedRequest,
		})
		return
	}

	requestBodyBytes, denialResponse, verified := server.readAndVerifySignedBody(writer, request, maxApprovalBodyBytes, controlSession.ID)
	if !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var uiDecisionRequest controlapipkg.UIApprovalDecisionRequest
	if err := decodeJSONBytes(requestBodyBytes, &uiDecisionRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   controlapipkg.DenialCodeMalformedRequest,
		})
		return
	}
	if err := uiDecisionRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   controlapipkg.DenialCodeMalformedRequest,
		})
		return
	}
	decisionNonce, manifestSHA256, found := server.currentApprovalDecisionState(approvalID)
	if !found {
		server.writeJSON(writer, http.StatusNotFound, controlapipkg.CapabilityResponse{
			RequestID:         approvalID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "approval request not found",
			DenialCode:        controlapipkg.DenialCodeApprovalNotFound,
			ApprovalRequestID: approvalID,
		})
		return
	}

	approvalDecisionPayload := controlapipkg.ApprovalDecisionRequest{
		Approved:               *uiDecisionRequest.Approved,
		Reason:                 strings.TrimSpace(uiDecisionRequest.Reason),
		DecisionNonce:          decisionNonce,
		ApprovalManifestSHA256: manifestSHA256,
	}

	if *uiDecisionRequest.Approved {
		pendingApproval, denialResponse, ok := server.validatePendingApprovalDecision(controlSession, approvalID, approvalDecisionPayload)
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
			server.emitUIApprovalResolved(pendingApproval, approvalID, "denied", controlapipkg.ResponseStatusDenied)
			server.writeJSON(writer, approvalDecisionHTTPStatus(denialResponse.DenialCode), denialResponse)
			return
		}
		if integrityDenial, integrityOK := server.verifyPendingApprovalStoredExecutionBody(pendingApproval); !integrityOK {
			server.writePendingApprovalExecutionIntegrityDenial(writer, controlSession, approvalID, pendingApproval, integrityDenial)
			return
		}
		if _, err := server.commitApprovalGrantConsumed(approvalID, decisionNonce, approvalDecisionPayload.Reason); err != nil {
			server.writeJSON(writer, http.StatusServiceUnavailable, controlapipkg.CapabilityResponse{
				RequestID:         pendingApproval.Request.RequestID,
				Status:            controlapipkg.ResponseStatusError,
				DenialReason:      "control-plane audit is unavailable",
				DenialCode:        controlapipkg.DenialCodeAuditUnavailable,
				ApprovalRequestID: approvalID,
			})
			return
		}
		// NOTE: This runs the full capability (e.g. host.plan.apply) before the HTTP response returns.
		// UI clients should show a long-running indicator on Approve — large plans can take many seconds.
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

	pendingApproval, denialResponse, _, validated := server.validateAndRecordApprovalDecision(controlSession, approvalID, approvalDecisionPayload)
	if !validated {
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
		server.emitUIApprovalResolved(pendingApproval, approvalID, "denied", controlapipkg.ResponseStatusDenied)
		server.writeJSON(writer, approvalDecisionHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}
	server.emitUIApprovalResolved(pendingApproval, approvalID, "denied", controlapipkg.ResponseStatusDenied)
	server.writeJSON(writer, http.StatusOK, controlapipkg.CapabilityResponse{
		RequestID:         pendingApproval.Request.RequestID,
		Status:            controlapipkg.ResponseStatusDenied,
		DenialReason:      "approval denied",
		DenialCode:        controlapipkg.DenialCodeApprovalDenied,
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
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityUIRead) {
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

func (server *Server) handleUIRecentEvents(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityUIRead) {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	lastEventID := strings.TrimSpace(request.Header.Get("Last-Event-ID"))
	server.writeJSON(writer, http.StatusOK, controlapipkg.UIRecentEventsResponse{
		Events: server.uiReplayEvents(tokenClaims.ControlSessionID, lastEventID),
	})
}

func writeSSEEvent(writer http.ResponseWriter, uiEventEnvelope controlapipkg.UIEventEnvelope) error {
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

func (server *Server) verifySignedRequestWithoutBody(request *http.Request, controlSessionID string) ([]byte, controlapipkg.CapabilityResponse, bool) {
	requestBodyBytes := []byte{}
	denialResponse, verified := server.verifySignedRequest(request, requestBodyBytes, controlSessionID)
	if !verified {
		return nil, denialResponse, false
	}
	return requestBodyBytes, controlapipkg.CapabilityResponse{}, true
}

// currentApprovalDecisionState returns the decision nonce and manifest SHA256 for a pending approval.
// The manifest SHA256 is included in the server-side decision path so that the UI approval handler
// can forward it to validateAndRecordApprovalDecision for manifest verification.
func (server *Server) currentApprovalDecisionState(approvalID string) (nonce, manifestSHA256 string, found bool) {
	server.mu.Lock()
	defer server.mu.Unlock()

	approval, found := server.approvalState.records[approvalID]
	if !found {
		return "", "", false
	}
	approval = backfillApprovalManifestLocked(server.approvalState.records, approvalID, approval)
	return approval.DecisionNonce, approval.ApprovalManifestSHA256, true
}

func (server *Server) loadPersonaDisplaySummary() (string, string) {
	persona, err := config.LoadPersona(server.repoRoot)
	if err != nil {
		return "Loopgate", "unknown"
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

func uiApprovalSummaryFromPending(pendingApproval pendingApproval) controlapipkg.UIApprovalSummary {
	preview := ""
	redacted := false
	if contentBytes, ok := pendingApproval.Metadata["content_bytes"].(int); ok && contentBytes > 0 {
		preview = "[content hidden]"
		redacted = true
	} else if contentBytesFloat, ok := pendingApproval.Metadata["content_bytes"].(float64); ok && int(contentBytesFloat) > 0 {
		preview = "[content hidden]"
		redacted = true
	}

	summary := controlapipkg.UIApprovalSummary{
		ApprovalRequestID: pendingApproval.ID,
		ControlSessionID:  pendingApproval.ControlSessionID,
		Requester:         pendingApproval.ExecutionContext.ActorLabel,
		Capability:        pendingApproval.Request.Capability,
		CreatedAtUTC:      pendingApproval.CreatedAt.Format(timeLayoutRFC3339Nano),
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
	server.ui.mu.Lock()
	defer server.ui.mu.Unlock()

	server.ui.nextSubscriberID++
	subscriber := uiEventSubscriber{
		controlSessionID: controlSessionID,
		id:               server.ui.nextSubscriberID,
		events:           make(chan controlapipkg.UIEventEnvelope, 16),
	}
	if server.ui.subscribers == nil {
		server.ui.subscribers = make(map[int]uiEventSubscriber)
	}
	server.ui.subscribers[subscriber.id] = subscriber
	return subscriber
}

func (server *Server) removeUISubscriber(subscriberID int) {
	server.ui.mu.Lock()
	defer server.ui.mu.Unlock()

	delete(server.ui.subscribers, subscriberID)
}

func (server *Server) uiReplayEvents(controlSessionID string, lastEventID string) []controlapipkg.UIEventEnvelope {
	server.ui.mu.Lock()
	defer server.ui.mu.Unlock()

	if len(server.ui.events) == 0 {
		return nil
	}

	if strings.TrimSpace(lastEventID) == "" {
		replayedEvents := make([]controlapipkg.UIEventEnvelope, 0, len(server.ui.events))
		for _, uiEventEnvelope := range server.ui.events {
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

	replayedEvents := make([]controlapipkg.UIEventEnvelope, 0, len(server.ui.events))
	for _, uiEventEnvelope := range server.ui.events {
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
	uiEventEnvelope := controlapipkg.UIEventEnvelope{
		ControlSessionID: controlSessionID,
		Type:             eventType,
		TS:               server.now().UTC().Format(timeLayoutRFC3339Nano),
		Data:             eventData,
	}
	if err := controlapipkg.ValidateUIEventEnvelope(controlapipkg.UIEventEnvelope{
		ID:   "pending",
		Type: uiEventEnvelope.Type,
		TS:   uiEventEnvelope.TS,
		Data: uiEventEnvelope.Data,
	}); err != nil {
		return
	}

	server.ui.mu.Lock()
	server.ui.sequence++
	uiEventEnvelope.ID = strconv.FormatUint(server.ui.sequence, 10)
	server.ui.events = append(server.ui.events, uiEventEnvelope)
	if len(server.ui.events) > maxUIEventBuffer {
		server.ui.events = append([]controlapipkg.UIEventEnvelope(nil), server.ui.events[len(server.ui.events)-maxUIEventBuffer:]...)
	}
	subscribers := make([]uiEventSubscriber, 0, len(server.ui.subscribers))
	for _, subscriber := range server.ui.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	server.ui.mu.Unlock()

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

func uiEventBelongsToControlSession(uiEventEnvelope controlapipkg.UIEventEnvelope, controlSessionID string) bool {
	return uiEventEnvelope.ControlSessionID == controlSessionID
}

func buildUIToolResultEvent(capability string, capabilityResponse controlapipkg.CapabilityResponse) controlapipkg.UIEventToolResult {
	uiEventToolResult := controlapipkg.UIEventToolResult{
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
	server.emitUIEvent(pendingApproval.ControlSessionID, controlapipkg.UIEventTypeApprovalResolved, controlapipkg.UIEventApprovalResolved{
		ApprovalRequestID: approvalID,
		Capability:        pendingApproval.Request.Capability,
		Decision:          decision,
		Status:            status,
	})
}

func (server *Server) emitUIToolDenied(controlSessionID string, capabilityRequest controlapipkg.CapabilityRequest, denialCode string, denialReason string) {
	server.emitUIEvent(controlSessionID, controlapipkg.UIEventTypeToolDenied, controlapipkg.UIEventToolDenied{
		RequestID:    capabilityRequest.RequestID,
		Capability:   capabilityRequest.Capability,
		DenialCode:   denialCode,
		DenialReason: denialReason,
	})
}

func (server *Server) emitUIToolResult(controlSessionID string, capabilityRequest controlapipkg.CapabilityRequest, capabilityResponse controlapipkg.CapabilityResponse) {
	server.emitUIEvent(controlSessionID, controlapipkg.UIEventTypeToolResult, buildUIToolResultEvent(capabilityRequest.Capability, capabilityResponse))
}

func (server *Server) emitUIApprovalPending(pendingApproval pendingApproval) {
	server.emitUIEvent(pendingApproval.ControlSessionID, controlapipkg.UIEventTypeApprovalPending, controlapipkg.UIEventApprovalPending{
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
