package loopgate

import (
	"crypto/subtle"
	"fmt"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"strings"
	"time"

	approvalpkg "loopgate/internal/approvalruntime"
	policypkg "loopgate/internal/policy"
	"loopgate/internal/secrets"
)

type approvalExecutionContext = approvalpkg.ExecutionContext

type pendingApproval = approvalpkg.PendingApproval

const (
	approvalStatePending   = approvalpkg.StatePending
	approvalStateGranted   = approvalpkg.StateGranted
	approvalStateDenied    = approvalpkg.StateDenied
	approvalStateExpired   = approvalpkg.StateExpired
	approvalStateCancelled = approvalpkg.StateCancelled
	// approvalStateConsumed is set atomically when an approved decision is recorded, before
	// execution begins. A concurrent decision that finds this state returns
	// controlapipkg.DenialCodeApprovalStateConflict rather than controlapipkg.DenialCodeApprovalStateInvalid to distinguish
	// a lost execution race from a genuine state violation such as an expired or denied approval.
	approvalStateConsumed        = approvalpkg.StateConsumed
	approvalStateExecutionFailed = approvalpkg.StateExecutionFailed
)

func approvalTokenHash(token string) string {
	return approvalpkg.TokenHash(token)
}

func setApprovalStateLocked(approvalRecords map[string]pendingApproval, approvalID string, approval pendingApproval, nextState string) (pendingApproval, error) {
	if err := approvalpkg.ValidateStateTransition(approval.State, nextState); err != nil {
		return approval, err
	}
	approval.State = nextState
	approvalRecords[approvalID] = approval
	return approval, nil
}

func (server *Server) authenticateApproval(writer http.ResponseWriter, request *http.Request) (controlSession, bool) {
	requestPeerIdentity, ok := peerIdentityFromContext(request.Context())
	if !ok {
		server.writeAuditedAuthDenial(writer, request, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "missing authenticated peer identity",
			DenialCode:   controlapipkg.DenialCodeApprovalTokenInvalid,
		}, authDeniedAuditOptions{authKind: "approval_token"})
		return controlSession{}, false
	}

	approvalToken := strings.TrimSpace(request.Header.Get("X-Loopgate-Approval-Token"))
	if approvalToken == "" {
		server.writeAuditedAuthDenial(writer, request, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "missing approval token",
			DenialCode:   controlapipkg.DenialCodeApprovalTokenMissing,
		}, authDeniedAuditOptions{
			authKind:    "approval_token",
			requestPeer: &requestPeerIdentity,
		})
		return controlSession{}, false
	}

	tokenHash := approvalTokenHash(approvalToken)

	server.mu.Lock()
	server.pruneExpiredLocked()
	controlSessionID, indexed := server.approvalState.tokenIndex[tokenHash]
	if !indexed {
		server.mu.Unlock()
		server.writeAuditedAuthDenial(writer, request, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "invalid approval token",
			DenialCode:   controlapipkg.DenialCodeApprovalTokenInvalid,
		}, authDeniedAuditOptions{
			authKind:    "approval_token",
			requestPeer: &requestPeerIdentity,
		})
		return controlSession{}, false
	}
	activeSession, sessionExists := server.sessionState.sessions[controlSessionID]
	if !sessionExists {
		delete(server.approvalState.tokenIndex, tokenHash)
		server.mu.Unlock()
		server.writeAuditedAuthDenial(writer, request, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "invalid approval token",
			DenialCode:   controlapipkg.DenialCodeApprovalTokenInvalid,
		}, authDeniedAuditOptions{
			authKind:         "approval_token",
			controlSessionID: controlSessionID,
			requestPeer:      &requestPeerIdentity,
		})
		return controlSession{}, false
	}

	// Constant-time comparison to prevent timing oracle on the raw token value.
	if subtle.ConstantTimeCompare([]byte(activeSession.ApprovalToken), []byte(approvalToken)) != 1 {
		server.mu.Unlock()
		server.writeAuditedAuthDenial(writer, request, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "invalid approval token",
			DenialCode:   controlapipkg.DenialCodeApprovalTokenInvalid,
		}, authDeniedAuditOptions{
			authKind:           "approval_token",
			controlSessionID:   activeSession.ID,
			actorLabel:         activeSession.ActorLabel,
			clientSessionLabel: activeSession.ClientSessionLabel,
			tenantID:           activeSession.TenantID,
			userID:             activeSession.UserID,
			requestPeer:        &requestPeerIdentity,
		})
		return controlSession{}, false
	}

	if server.now().UTC().After(activeSession.ExpiresAt) {
		delete(server.sessionState.sessions, controlSessionID)
		delete(server.approvalState.tokenIndex, tokenHash)
		server.mu.Unlock()
		server.writeAuditedAuthDenial(writer, request, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "expired approval token",
			DenialCode:   controlapipkg.DenialCodeApprovalTokenExpired,
		}, authDeniedAuditOptions{
			authKind:           "approval_token",
			controlSessionID:   activeSession.ID,
			actorLabel:         activeSession.ActorLabel,
			clientSessionLabel: activeSession.ClientSessionLabel,
			tenantID:           activeSession.TenantID,
			userID:             activeSession.UserID,
			requestPeer:        &requestPeerIdentity,
		})
		return controlSession{}, false
	}
	if activeSession.PeerIdentity != requestPeerIdentity {
		server.mu.Unlock()
		server.writeAuditedAuthDenial(writer, request, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "approval token peer binding mismatch",
			DenialCode:   controlapipkg.DenialCodeApprovalTokenInvalid,
		}, authDeniedAuditOptions{
			authKind:           "approval_token",
			controlSessionID:   activeSession.ID,
			actorLabel:         activeSession.ActorLabel,
			clientSessionLabel: activeSession.ClientSessionLabel,
			tenantID:           activeSession.TenantID,
			userID:             activeSession.UserID,
			requestPeer:        &requestPeerIdentity,
		})
		return controlSession{}, false
	}
	server.mu.Unlock()
	return activeSession, true
}

func interfaceStringValue(rawValue interface{}) string {
	stringValue, ok := rawValue.(string)
	if !ok {
		return ""
	}
	return stringValue
}

func (server *Server) approvalMetadata(controlSessionID string, capabilityRequest controlapipkg.CapabilityRequest) map[string]interface{} {
	metadata := map[string]interface{}{
		"capability": capabilityRequest.Capability,
	}
	if approvalClass := server.approvalClassForCapability(capabilityRequest.Capability); approvalClass != "" {
		metadata["approval_class"] = approvalClass
	}
	if pathValue := strings.TrimSpace(capabilityRequest.Arguments["path"]); pathValue != "" {
		metadata["path"] = pathValue
	}
	if contentValue, hasContent := capabilityRequest.Arguments["content"]; hasContent {
		metadata["content_bytes"] = len(contentValue)
	}
	redactedArguments := secrets.RedactStringMap(capabilityRequest.Arguments)
	for argumentKey, argumentValue := range redactedArguments {
		if argumentKey == "path" || argumentKey == "content" {
			continue
		}
		metadata["arg_"+argumentKey] = argumentValue
	}
	if operatorMountGrant, _, err := operatorMountWriteGrantForRequest(server, controlSessionID, capabilityRequest); err == nil && strings.TrimSpace(operatorMountGrant.root) != "" {
		metadata["grant_root"] = operatorMountGrant.root
		metadata["grant_ttl_seconds"] = int(operatorMountWriteGrantTTL / time.Second)
	}
	if capabilityRequest.Capability == "host.plan.apply" {
		for k, v := range server.hostPlanApplyApprovalOperatorFields(capabilityRequest.Arguments["plan_id"]) {
			metadata[k] = v
		}
	}
	return metadata
}

func approvalReasonForCapability(policyDecision policypkg.CheckResult, metadata map[string]interface{}, capabilityRequest controlapipkg.CapabilityRequest) string {
	grantRootPath := strings.TrimSpace(stringMetadataValue(metadata, "grant_root"))
	if grantRootPath == "" {
		return policyDecision.Reason
	}
	switch capabilityRequest.Capability {
	case "operator_mount.fs_write", "operator_mount.fs_mkdir":
		return fmt.Sprintf("Grant write access to %s for %s", grantRootPath, operatorMountWriteGrantTTL)
	default:
		return policyDecision.Reason
	}
}

func buildApprovalGrantedAuditData(approvalID string, pendingApproval pendingApproval, decisionReason string) map[string]interface{} {
	auditData := map[string]interface{}{
		"approval_request_id":  approvalID,
		"capability":           pendingApproval.Request.Capability,
		"approval_class":       pendingApproval.Metadata["approval_class"],
		"approval_state":       approvalStateConsumed,
		"control_session_id":   pendingApproval.ControlSessionID,
		"actor_label":          pendingApproval.ExecutionContext.ActorLabel,
		"client_session_label": pendingApproval.ExecutionContext.ClientSessionLabel,
	}
	if approvalClass, ok := pendingApproval.Metadata["approval_class"].(string); ok && strings.TrimSpace(approvalClass) != "" {
		auditData["approval_class"] = approvalClass
	}
	if grantRoot, ok := pendingApproval.Metadata["grant_root"].(string); ok && strings.TrimSpace(grantRoot) != "" {
		auditData["grant_root"] = grantRoot
	}
	if grantTTLSeconds, found := pendingApproval.Metadata["grant_ttl_seconds"]; found {
		auditData["grant_ttl_seconds"] = grantTTLSeconds
	}
	if trimmedDecisionReason := strings.TrimSpace(decisionReason); trimmedDecisionReason != "" {
		auditData["operator_reason"] = secrets.RedactText(trimmedDecisionReason)
	}
	return auditData
}

func buildApprovalOperatorDeniedAuditData(approvalID string, pendingApproval pendingApproval, decisionReason string) map[string]interface{} {
	auditData := map[string]interface{}{
		"approval_request_id":  approvalID,
		"capability":           pendingApproval.Request.Capability,
		"approval_class":       pendingApproval.Metadata["approval_class"],
		"approval_state":       approvalStateDenied,
		"control_session_id":   pendingApproval.ControlSessionID,
		"actor_label":          pendingApproval.ExecutionContext.ActorLabel,
		"client_session_label": pendingApproval.ExecutionContext.ClientSessionLabel,
	}
	if approvalClass, ok := pendingApproval.Metadata["approval_class"].(string); ok && strings.TrimSpace(approvalClass) != "" {
		auditData["approval_class"] = approvalClass
	}
	if trimmedDecisionReason := strings.TrimSpace(decisionReason); trimmedDecisionReason != "" {
		auditData["operator_reason"] = secrets.RedactText(trimmedDecisionReason)
	}
	return auditData
}

func approvalDecisionValidationResponse(approvalID string, pendingApproval pendingApproval, validationError *approvalpkg.DecisionValidationError) controlapipkg.CapabilityResponse {
	requestID := pendingApproval.Request.RequestID
	denialCode := controlapipkg.DenialCodeApprovalStateInvalid
	switch validationError.Code {
	case approvalpkg.DecisionValidationNotFound:
		requestID = approvalID
		denialCode = controlapipkg.DenialCodeApprovalNotFound
	case approvalpkg.DecisionValidationOwnerMismatch:
		denialCode = controlapipkg.DenialCodeApprovalOwnerMismatch
	case approvalpkg.DecisionValidationStateConflict:
		denialCode = controlapipkg.DenialCodeApprovalStateConflict
	case approvalpkg.DecisionValidationStateInvalid:
		denialCode = controlapipkg.DenialCodeApprovalStateInvalid
	case approvalpkg.DecisionValidationNonceMissing:
		denialCode = controlapipkg.DenialCodeApprovalDecisionNonceMissing
	case approvalpkg.DecisionValidationNonceInvalid:
		denialCode = controlapipkg.DenialCodeApprovalDecisionNonceInvalid
	case approvalpkg.DecisionValidationManifestInvalid:
		denialCode = controlapipkg.DenialCodeApprovalManifestMismatch
	}
	return controlapipkg.CapabilityResponse{
		RequestID:         requestID,
		Status:            controlapipkg.ResponseStatusDenied,
		DenialReason:      validationError.Reason,
		DenialCode:        denialCode,
		ApprovalRequestID: approvalID,
	}
}

// validatePendingApprovalDecisionLocked performs session, expiry, nonce, and manifest checks
// for a pending approval without writing audit events or changing approval state.
// Must be called with server.mu held.
func (server *Server) loadPendingApprovalForDecisionLocked(approvalID string) (pendingApproval, controlapipkg.CapabilityResponse, bool) {
	server.pruneExpiredLocked()
	pendingApproval, found := server.approvalState.records[approvalID]
	if !found {
		return pendingApproval, controlapipkg.CapabilityResponse{
			RequestID:    approvalID,
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "approval request not found",
			DenialCode:   controlapipkg.DenialCodeApprovalNotFound,
		}, false
	}

	if pendingApproval.ExpiresAt.Before(server.now().UTC()) {
		expiredApproval, transitionErr := setApprovalStateLocked(server.approvalState.records, approvalID, pendingApproval, approvalStateExpired)
		if transitionErr != nil {
			return pendingApproval, controlapipkg.CapabilityResponse{
				RequestID:         pendingApproval.Request.RequestID,
				Status:            controlapipkg.ResponseStatusDenied,
				DenialReason:      "approval request is in an invalid state",
				DenialCode:        controlapipkg.DenialCodeApprovalStateInvalid,
				ApprovalRequestID: approvalID,
			}, false
		}
		return expiredApproval, controlapipkg.CapabilityResponse{
			RequestID:         pendingApproval.Request.RequestID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "approval request expired",
			DenialCode:        controlapipkg.DenialCodeApprovalDenied,
			ApprovalRequestID: approvalID,
		}, false
	}

	pendingApproval = backfillApprovalManifestLocked(server.approvalState.records, approvalID, pendingApproval)
	return pendingApproval, controlapipkg.CapabilityResponse{}, true
}

// validatePendingApprovalDecisionLocked performs session, expiry, nonce, and manifest checks
// for an approval-token decision path. Must be called with server.mu held.
func (server *Server) validatePendingApprovalDecisionLocked(controlSession controlSession, approvalID string, decisionRequest controlapipkg.ApprovalDecisionRequest) (pendingApproval, controlapipkg.CapabilityResponse, bool) {
	pendingApproval, denialResponse, ok := server.loadPendingApprovalForDecisionLocked(approvalID)
	if !ok {
		return pendingApproval, denialResponse, false
	}
	validationError := approvalpkg.ValidateDecisionRequest(pendingApproval, decisionRequest, approvalpkg.DecisionActor{
		ControlSessionID: controlSession.ID,
	})
	if validationError != nil {
		return pendingApproval, approvalDecisionValidationResponse(approvalID, pendingApproval, validationError), false
	}
	return pendingApproval, controlapipkg.CapabilityResponse{}, true
}

func (server *Server) validatePendingApprovalDecisionLockedForOperator(controlSession controlSession, approvalID string, decisionRequest controlapipkg.ApprovalDecisionRequest) (pendingApproval, controlapipkg.CapabilityResponse, bool) {
	pendingApproval, denialResponse, ok := server.loadPendingApprovalForDecisionLocked(approvalID)
	if !ok {
		return pendingApproval, denialResponse, false
	}
	validationError := approvalpkg.ValidateDecisionRequest(pendingApproval, decisionRequest, approvalpkg.DecisionActor{
		Operator: true,
		TenantID: controlSession.TenantID,
	})
	if validationError != nil {
		return pendingApproval, approvalDecisionValidationResponse(approvalID, pendingApproval, validationError), false
	}
	return pendingApproval, controlapipkg.CapabilityResponse{}, true
}

func (server *Server) validatePendingApprovalDecision(controlSession controlSession, approvalID string, decisionRequest controlapipkg.ApprovalDecisionRequest) (pendingApproval, controlapipkg.CapabilityResponse, bool) {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.validatePendingApprovalDecisionLocked(controlSession, approvalID, decisionRequest)
}

func (server *Server) validatePendingApprovalDecisionForOperator(controlSession controlSession, approvalID string, decisionRequest controlapipkg.ApprovalDecisionRequest) (pendingApproval, controlapipkg.CapabilityResponse, bool) {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.validatePendingApprovalDecisionLockedForOperator(controlSession, approvalID, decisionRequest)
}

// commitApprovalGrantConsumed appends approval.granted and transitions the approval to consumed.
// Call after approval-scoped work succeeds so a post-decision failure does not leave a consumed
// approval with no executed action. If audit append fails after side effects, the operator may
// need manual recovery (rare).
func (server *Server) commitApprovalGrantConsumed(approvalID string, expectedDecisionNonce string, decisionReason string) (string, error) {
	expectedDecisionNonce = strings.TrimSpace(expectedDecisionNonce)
	server.mu.Lock()
	defer server.mu.Unlock()

	pendingApproval, found := server.approvalState.records[approvalID]
	if !found {
		return "", fmt.Errorf("approval request not found")
	}
	if pendingApproval.State != approvalStatePending {
		return "", fmt.Errorf("approval request is no longer pending")
	}
	if expectedDecisionNonce == "" || pendingApproval.DecisionNonce != expectedDecisionNonce {
		return "", fmt.Errorf("approval decision nonce mismatch")
	}

	grantAuditData := buildApprovalGrantedAuditData(approvalID, pendingApproval, decisionReason)
	if session, ok := server.sessionState.sessions[pendingApproval.ControlSessionID]; ok {
		mergeAuditTenancyFromControlSession(grantAuditData, session)
	}
	grantRoot := strings.TrimSpace(interfaceStringValue(pendingApproval.Metadata["grant_root"]))
	var grantExpiresAt time.Time
	if grantRoot != "" {
		grantExpiresAt = server.now().UTC().Add(operatorMountWriteGrantTTL)
		grantAuditData["grant_expires_at_utc"] = grantExpiresAt.Format(time.RFC3339Nano)
	}
	auditEventHash, err := server.logEventWithHash("approval.granted", pendingApproval.ControlSessionID, grantAuditData)
	if err != nil {
		return "", err
	}
	if grantRoot != "" {
		controlSession := server.sessionState.sessions[pendingApproval.ControlSessionID]
		if controlSession.OperatorMountWriteGrants == nil {
			controlSession.OperatorMountWriteGrants = make(map[string]time.Time)
		}
		controlSession.OperatorMountWriteGrants[grantRoot] = grantExpiresAt
		server.sessionState.sessions[pendingApproval.ControlSessionID] = controlSession
	}
	pendingApproval, err = setApprovalStateLocked(server.approvalState.records, approvalID, pendingApproval, approvalStateConsumed)
	if err != nil {
		return "", err
	}
	pendingApproval.DecisionSubmittedAt = server.now().UTC()
	pendingApproval.DecisionNonce = ""
	server.approvalState.records[approvalID] = pendingApproval
	return auditEventHash, nil
}

func (server *Server) recordPendingApprovalDecisionLocked(controlSession controlSession, approvalID string, pendingApproval pendingApproval, decisionRequest controlapipkg.ApprovalDecisionRequest) (pendingApproval, controlapipkg.CapabilityResponse, string, bool) {
	if decisionRequest.Approved {
		grantAuditData := buildApprovalGrantedAuditData(approvalID, pendingApproval, decisionRequest.Reason)
		mergeAuditTenancyFromControlSession(grantAuditData, controlSession)
		auditEventHash, err := server.logEventWithHash("approval.granted", pendingApproval.ControlSessionID, grantAuditData)
		if err != nil {
			return pendingApproval, controlapipkg.CapabilityResponse{
				RequestID:         pendingApproval.Request.RequestID,
				Status:            controlapipkg.ResponseStatusError,
				DenialReason:      "control-plane audit is unavailable",
				DenialCode:        controlapipkg.DenialCodeAuditUnavailable,
				ApprovalRequestID: approvalID,
			}, "", false
		}
		pendingApproval, err = setApprovalStateLocked(server.approvalState.records, approvalID, pendingApproval, approvalStateConsumed)
		if err != nil {
			return pendingApproval, controlapipkg.CapabilityResponse{
				RequestID:         pendingApproval.Request.RequestID,
				Status:            controlapipkg.ResponseStatusDenied,
				DenialReason:      "approval request is in an invalid state",
				DenialCode:        controlapipkg.DenialCodeApprovalStateInvalid,
				ApprovalRequestID: approvalID,
			}, "", false
		}
		pendingApproval.DecisionSubmittedAt = server.now().UTC()
		pendingApproval.DecisionNonce = ""
		server.approvalState.records[approvalID] = pendingApproval
		return pendingApproval, controlapipkg.CapabilityResponse{}, auditEventHash, true
	}

	deniedAuditData := buildApprovalOperatorDeniedAuditData(approvalID, pendingApproval, decisionRequest.Reason)
	mergeAuditTenancyFromControlSession(deniedAuditData, controlSession)
	auditEventHash, err := server.logEventWithHash("approval.denied", pendingApproval.ControlSessionID, deniedAuditData)
	if err != nil {
		return pendingApproval, controlapipkg.CapabilityResponse{
			RequestID:         pendingApproval.Request.RequestID,
			Status:            controlapipkg.ResponseStatusError,
			DenialReason:      "control-plane audit is unavailable",
			DenialCode:        controlapipkg.DenialCodeAuditUnavailable,
			ApprovalRequestID: approvalID,
		}, "", false
	}
	pendingApproval, err = setApprovalStateLocked(server.approvalState.records, approvalID, pendingApproval, approvalStateDenied)
	if err != nil {
		return pendingApproval, controlapipkg.CapabilityResponse{
			RequestID:         pendingApproval.Request.RequestID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "approval request is in an invalid state",
			DenialCode:        controlapipkg.DenialCodeApprovalStateInvalid,
			ApprovalRequestID: approvalID,
		}, "", false
	}
	pendingApproval.DecisionSubmittedAt = server.now().UTC()
	pendingApproval.DecisionNonce = ""
	server.approvalState.records[approvalID] = pendingApproval
	return pendingApproval, controlapipkg.CapabilityResponse{}, auditEventHash, true
}

func (server *Server) validateAndRecordApprovalDecision(controlSession controlSession, approvalID string, decisionRequest controlapipkg.ApprovalDecisionRequest) (pendingApproval, controlapipkg.CapabilityResponse, string, bool) {
	server.mu.Lock()
	defer server.mu.Unlock()

	pendingApproval, denialResponse, ok := server.validatePendingApprovalDecisionLocked(controlSession, approvalID, decisionRequest)
	if !ok {
		return pendingApproval, denialResponse, "", false
	}
	return server.recordPendingApprovalDecisionLocked(controlSession, approvalID, pendingApproval, decisionRequest)
}

func (server *Server) validateAndRecordOperatorApprovalDecision(controlSession controlSession, approvalID string, decisionRequest controlapipkg.ApprovalDecisionRequest) (pendingApproval, controlapipkg.CapabilityResponse, string, bool) {
	server.mu.Lock()
	defer server.mu.Unlock()

	pendingApproval, denialResponse, ok := server.validatePendingApprovalDecisionLockedForOperator(controlSession, approvalID, decisionRequest)
	if !ok {
		return pendingApproval, denialResponse, "", false
	}
	return server.recordPendingApprovalDecisionLocked(controlSession, approvalID, pendingApproval, decisionRequest)
}

func (server *Server) approvedExecutionTokenForPendingApproval(approvalID string, pendingApproval pendingApproval) (capabilityToken, controlapipkg.CapabilityResponse, bool) {
	server.mu.Lock()
	defer server.mu.Unlock()

	server.pruneExpiredLocked()
	ownerSession, found := server.sessionState.sessions[pendingApproval.ExecutionContext.ControlSessionID]
	if !found {
		return capabilityToken{}, controlapipkg.CapabilityResponse{
			RequestID:         pendingApproval.Request.RequestID,
			Status:            controlapipkg.ResponseStatusDenied,
			DenialReason:      "approval owner session is no longer active",
			DenialCode:        controlapipkg.DenialCodeApprovalStateInvalid,
			ApprovalRequestID: approvalID,
		}, false
	}
	return capabilityToken{
		TokenID:             "approved:" + approvalID,
		ControlSessionID:    pendingApproval.ExecutionContext.ControlSessionID,
		ActorLabel:          pendingApproval.ExecutionContext.ActorLabel,
		ClientSessionLabel:  pendingApproval.ExecutionContext.ClientSessionLabel,
		AllowedCapabilities: copyCapabilitySet(pendingApproval.ExecutionContext.AllowedCapabilities),
		PeerIdentity:        ownerSession.PeerIdentity,
		TenantID:            ownerSession.TenantID,
		UserID:              ownerSession.UserID,
		ExpiresAt:           ownerSession.ExpiresAt,
		SingleUse:           true,
		ApprovedExecution:   true,
		BoundCapability:     pendingApproval.Request.Capability,
		BoundArgumentHash:   normalizedArgumentHash(pendingApproval.Request.Arguments),
	}, controlapipkg.CapabilityResponse{}, true
}

func (server *Server) auditApprovalDecisionDenial(controlSession controlSession, approvalID string, pendingApproval pendingApproval, denialResponse controlapipkg.CapabilityResponse) error {
	if strings.TrimSpace(denialResponse.DenialCode) == "" || denialResponse.DenialCode == controlapipkg.DenialCodeAuditUnavailable {
		return nil
	}
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
	return server.logEvent("approval.denied", controlSession.ID, approvalDeniedAuditData)
}

// backfillApprovalManifestLocked lazily computes and stores the approval manifest for any
// in-flight approval that was created before the manifest-binding change was deployed.
// Must be called with server.mu held.
func backfillApprovalManifestLocked(approvalRecords map[string]pendingApproval, approvalID string, approval pendingApproval) pendingApproval {
	return approvalpkg.BackfillPendingApprovalManifest(approvalRecords, approvalID, approval)
}

func (server *Server) markApprovalExecutionResult(approvalID string, executionStatus string) {
	server.mu.Lock()
	defer server.mu.Unlock()

	pendingApproval, found := server.approvalState.records[approvalID]
	if !found {
		return
	}
	if executionStatus != controlapipkg.ResponseStatusSuccess {
		updatedApproval, err := setApprovalStateLocked(server.approvalState.records, approvalID, pendingApproval, approvalStateExecutionFailed)
		if err != nil {
			if server.reportSecurityWarning != nil {
				server.reportSecurityWarning("approval_state_transition_invalid", err)
			}
		} else {
			pendingApproval = updatedApproval
		}
	}
	pendingApproval.ExecutedAt = server.now().UTC()
	server.approvalState.records[approvalID] = pendingApproval
}

func approvalDecisionHTTPStatus(denialCode string) int {
	switch denialCode {
	case controlapipkg.DenialCodeApprovalNotFound:
		return http.StatusNotFound
	case controlapipkg.DenialCodeApprovalTokenMissing, controlapipkg.DenialCodeApprovalTokenInvalid, controlapipkg.DenialCodeApprovalTokenExpired:
		return http.StatusUnauthorized
	default:
		return http.StatusForbidden
	}
}
