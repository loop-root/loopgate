package loopgate

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	policypkg "loopgate/internal/policy"
	"loopgate/internal/secrets"
)

type approvalExecutionContext struct {
	ControlSessionID    string
	ActorLabel          string
	ClientSessionLabel  string
	AllowedCapabilities map[string]struct{}
	TenantID            string
	UserID              string
}

type pendingApproval struct {
	ID                  string
	Request             CapabilityRequest
	CreatedAt           time.Time
	ExpiresAt           time.Time
	Metadata            map[string]interface{}
	Reason              string
	ControlSessionID    string
	DecisionNonce       string
	DecisionSubmittedAt time.Time
	ExecutedAt          time.Time
	ExecutionContext    approvalExecutionContext
	State               string
	// ApprovalManifestSHA256 is the canonical approval manifest hash per AMP RFC 0005 §6,
	// computed at approval creation time from the action class, subject, execution method,
	// path, request body hash, scope, and expiry. Verified against the operator-submitted
	// hash at decision time to bind the decision to the exact approved action.
	ApprovalManifestSHA256 string
	// ExecutionBodySHA256 is the SHA256 of the serialized CapabilityRequest body, stored at
	// approval creation time. At execution time (PR 1b), the live request body hash is
	// verified against this value along with the method and path to confirm exact match.
	ExecutionBodySHA256 string
}

const (
	approvalStatePending   = "pending"
	approvalStateGranted   = "granted"
	approvalStateDenied    = "denied"
	approvalStateExpired   = "expired"
	approvalStateCancelled = "cancelled"
	// approvalStateConsumed is set atomically when an approved decision is recorded, before
	// execution begins. A concurrent decision that finds this state returns
	// DenialCodeApprovalStateConflict rather than DenialCodeApprovalStateInvalid to distinguish
	// a lost execution race from a genuine state violation such as an expired or denied approval.
	approvalStateConsumed        = "consumed"
	approvalStateExecutionFailed = "execution_failed"
)

func approvalTokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func (server *Server) authenticateApproval(writer http.ResponseWriter, request *http.Request) (controlSession, bool) {
	requestPeerIdentity, ok := peerIdentityFromContext(request.Context())
	if !ok {
		server.writeAuditedAuthDenial(writer, request, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "missing authenticated peer identity",
			DenialCode:   DenialCodeApprovalTokenInvalid,
		}, authDeniedAuditOptions{authKind: "approval_token"})
		return controlSession{}, false
	}

	approvalToken := strings.TrimSpace(request.Header.Get("X-Loopgate-Approval-Token"))
	if approvalToken == "" {
		server.writeAuditedAuthDenial(writer, request, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "missing approval token",
			DenialCode:   DenialCodeApprovalTokenMissing,
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
		server.writeAuditedAuthDenial(writer, request, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "invalid approval token",
			DenialCode:   DenialCodeApprovalTokenInvalid,
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
		server.writeAuditedAuthDenial(writer, request, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "invalid approval token",
			DenialCode:   DenialCodeApprovalTokenInvalid,
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
		server.writeAuditedAuthDenial(writer, request, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "invalid approval token",
			DenialCode:   DenialCodeApprovalTokenInvalid,
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
		server.writeAuditedAuthDenial(writer, request, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "expired approval token",
			DenialCode:   DenialCodeApprovalTokenExpired,
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
		server.writeAuditedAuthDenial(writer, request, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "approval token peer binding mismatch",
			DenialCode:   DenialCodeApprovalTokenInvalid,
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

func (server *Server) approvalMetadata(controlSessionID string, capabilityRequest CapabilityRequest) map[string]interface{} {
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

func approvalReasonForCapability(policyDecision policypkg.CheckResult, metadata map[string]interface{}, capabilityRequest CapabilityRequest) string {
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

// validatePendingApprovalDecisionLocked performs session, expiry, nonce, and manifest checks
// for a pending approval without writing audit events or changing approval state.
// Must be called with server.mu held.
func (server *Server) loadPendingApprovalForDecisionLocked(approvalID string) (pendingApproval, CapabilityResponse, bool) {
	server.pruneExpiredLocked()
	pendingApproval, found := server.approvalState.records[approvalID]
	if !found {
		return pendingApproval, CapabilityResponse{
			RequestID:    approvalID,
			Status:       ResponseStatusDenied,
			DenialReason: "approval request not found",
			DenialCode:   DenialCodeApprovalNotFound,
		}, false
	}

	if pendingApproval.ExpiresAt.Before(server.now().UTC()) {
		pendingApproval.State = approvalStateExpired
		server.approvalState.records[approvalID] = pendingApproval
		return pendingApproval, CapabilityResponse{
			RequestID:         pendingApproval.Request.RequestID,
			Status:            ResponseStatusDenied,
			DenialReason:      "approval request expired",
			DenialCode:        DenialCodeApprovalDenied,
			ApprovalRequestID: approvalID,
		}, false
	}

	pendingApproval = backfillApprovalManifestLocked(server.approvalState.records, approvalID, pendingApproval)
	return pendingApproval, CapabilityResponse{}, true
}

func validatePendingApprovalDecisionPayload(approvalID string, pendingApproval pendingApproval, decisionRequest ApprovalDecisionRequest) (pendingApproval, CapabilityResponse, bool) {
	if pendingApproval.State != approvalStatePending {
		denialCode := DenialCodeApprovalStateInvalid
		if pendingApproval.State == approvalStateConsumed || pendingApproval.State == approvalStateExecutionFailed {
			denialCode = DenialCodeApprovalStateConflict
		}
		return pendingApproval, CapabilityResponse{
			RequestID:         pendingApproval.Request.RequestID,
			Status:            ResponseStatusDenied,
			DenialReason:      "approval request is no longer pending",
			DenialCode:        denialCode,
			ApprovalRequestID: approvalID,
		}, false
	}

	decisionNonce := strings.TrimSpace(decisionRequest.DecisionNonce)
	if decisionNonce == "" {
		return pendingApproval, CapabilityResponse{
			RequestID:         pendingApproval.Request.RequestID,
			Status:            ResponseStatusDenied,
			DenialReason:      "approval decision nonce is required",
			DenialCode:        DenialCodeApprovalDecisionNonceMissing,
			ApprovalRequestID: approvalID,
		}, false
	}
	if decisionNonce != pendingApproval.DecisionNonce {
		return pendingApproval, CapabilityResponse{
			RequestID:         pendingApproval.Request.RequestID,
			Status:            ResponseStatusDenied,
			DenialReason:      "approval decision nonce is invalid",
			DenialCode:        DenialCodeApprovalDecisionNonceInvalid,
			ApprovalRequestID: approvalID,
		}, false
	}

	submittedManifest := strings.TrimSpace(decisionRequest.ApprovalManifestSHA256)
	if decisionRequest.Approved && pendingApproval.ApprovalManifestSHA256 != "" {
		if submittedManifest == "" {
			return pendingApproval, CapabilityResponse{
				RequestID:         pendingApproval.Request.RequestID,
				Status:            ResponseStatusDenied,
				DenialReason:      "approval manifest sha256 is required for this approval",
				DenialCode:        DenialCodeApprovalManifestMismatch,
				ApprovalRequestID: approvalID,
			}, false
		}
		if submittedManifest != pendingApproval.ApprovalManifestSHA256 {
			return pendingApproval, CapabilityResponse{
				RequestID:         pendingApproval.Request.RequestID,
				Status:            ResponseStatusDenied,
				DenialReason:      "approval manifest sha256 does not match the pending approval",
				DenialCode:        DenialCodeApprovalManifestMismatch,
				ApprovalRequestID: approvalID,
			}, false
		}
	}

	return pendingApproval, CapabilityResponse{}, true
}

// validatePendingApprovalDecisionLocked performs session, expiry, nonce, and manifest checks
// for an approval-token decision path. Must be called with server.mu held.
func (server *Server) validatePendingApprovalDecisionLocked(controlSession controlSession, approvalID string, decisionRequest ApprovalDecisionRequest) (pendingApproval, CapabilityResponse, bool) {
	pendingApproval, denialResponse, ok := server.loadPendingApprovalForDecisionLocked(approvalID)
	if !ok {
		return pendingApproval, denialResponse, false
	}
	if controlSession.ID != pendingApproval.ControlSessionID {
		return pendingApproval, CapabilityResponse{
			RequestID:         pendingApproval.Request.RequestID,
			Status:            ResponseStatusDenied,
			DenialReason:      "approval token does not match approval owner",
			DenialCode:        DenialCodeApprovalOwnerMismatch,
			ApprovalRequestID: approvalID,
		}, false
	}
	return validatePendingApprovalDecisionPayload(approvalID, pendingApproval, decisionRequest)
}

func (server *Server) validatePendingApprovalDecisionLockedForOperator(controlSession controlSession, approvalID string, decisionRequest ApprovalDecisionRequest) (pendingApproval, CapabilityResponse, bool) {
	pendingApproval, denialResponse, ok := server.loadPendingApprovalForDecisionLocked(approvalID)
	if !ok {
		return pendingApproval, denialResponse, false
	}
	if strings.TrimSpace(controlSession.TenantID) != "" && strings.TrimSpace(pendingApproval.ExecutionContext.TenantID) != "" && controlSession.TenantID != pendingApproval.ExecutionContext.TenantID {
		return pendingApproval, CapabilityResponse{
			RequestID:         approvalID,
			Status:            ResponseStatusDenied,
			DenialReason:      "approval request not found",
			DenialCode:        DenialCodeApprovalNotFound,
			ApprovalRequestID: approvalID,
		}, false
	}
	return validatePendingApprovalDecisionPayload(approvalID, pendingApproval, decisionRequest)
}

func (server *Server) validatePendingApprovalDecision(controlSession controlSession, approvalID string, decisionRequest ApprovalDecisionRequest) (pendingApproval, CapabilityResponse, bool) {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.validatePendingApprovalDecisionLocked(controlSession, approvalID, decisionRequest)
}

func (server *Server) validatePendingApprovalDecisionForOperator(controlSession controlSession, approvalID string, decisionRequest ApprovalDecisionRequest) (pendingApproval, CapabilityResponse, bool) {
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
	pendingApproval.State = approvalStateConsumed
	pendingApproval.DecisionSubmittedAt = server.now().UTC()
	pendingApproval.DecisionNonce = ""
	server.approvalState.records[approvalID] = pendingApproval
	return auditEventHash, nil
}

func (server *Server) recordPendingApprovalDecisionLocked(controlSession controlSession, approvalID string, pendingApproval pendingApproval, decisionRequest ApprovalDecisionRequest) (pendingApproval, CapabilityResponse, string, bool) {
	if decisionRequest.Approved {
		grantAuditData := buildApprovalGrantedAuditData(approvalID, pendingApproval, decisionRequest.Reason)
		mergeAuditTenancyFromControlSession(grantAuditData, controlSession)
		auditEventHash, err := server.logEventWithHash("approval.granted", pendingApproval.ControlSessionID, grantAuditData)
		if err != nil {
			return pendingApproval, CapabilityResponse{
				RequestID:         pendingApproval.Request.RequestID,
				Status:            ResponseStatusError,
				DenialReason:      "control-plane audit is unavailable",
				DenialCode:        DenialCodeAuditUnavailable,
				ApprovalRequestID: approvalID,
			}, "", false
		}
		pendingApproval.State = approvalStateConsumed
		pendingApproval.DecisionSubmittedAt = server.now().UTC()
		pendingApproval.DecisionNonce = ""
		server.approvalState.records[approvalID] = pendingApproval
		return pendingApproval, CapabilityResponse{}, auditEventHash, true
	}

	deniedAuditData := buildApprovalOperatorDeniedAuditData(approvalID, pendingApproval, decisionRequest.Reason)
	mergeAuditTenancyFromControlSession(deniedAuditData, controlSession)
	auditEventHash, err := server.logEventWithHash("approval.denied", pendingApproval.ControlSessionID, deniedAuditData)
	if err != nil {
		return pendingApproval, CapabilityResponse{
			RequestID:         pendingApproval.Request.RequestID,
			Status:            ResponseStatusError,
			DenialReason:      "control-plane audit is unavailable",
			DenialCode:        DenialCodeAuditUnavailable,
			ApprovalRequestID: approvalID,
		}, "", false
	}
	pendingApproval.State = approvalStateDenied
	pendingApproval.DecisionSubmittedAt = server.now().UTC()
	pendingApproval.DecisionNonce = ""
	server.approvalState.records[approvalID] = pendingApproval
	return pendingApproval, CapabilityResponse{}, auditEventHash, true
}

func (server *Server) validateAndRecordApprovalDecision(controlSession controlSession, approvalID string, decisionRequest ApprovalDecisionRequest) (pendingApproval, CapabilityResponse, string, bool) {
	server.mu.Lock()
	defer server.mu.Unlock()

	pendingApproval, denialResponse, ok := server.validatePendingApprovalDecisionLocked(controlSession, approvalID, decisionRequest)
	if !ok {
		return pendingApproval, denialResponse, "", false
	}
	return server.recordPendingApprovalDecisionLocked(controlSession, approvalID, pendingApproval, decisionRequest)
}

func (server *Server) validateAndRecordOperatorApprovalDecision(controlSession controlSession, approvalID string, decisionRequest ApprovalDecisionRequest) (pendingApproval, CapabilityResponse, string, bool) {
	server.mu.Lock()
	defer server.mu.Unlock()

	pendingApproval, denialResponse, ok := server.validatePendingApprovalDecisionLockedForOperator(controlSession, approvalID, decisionRequest)
	if !ok {
		return pendingApproval, denialResponse, "", false
	}
	return server.recordPendingApprovalDecisionLocked(controlSession, approvalID, pendingApproval, decisionRequest)
}

func (server *Server) approvedExecutionTokenForPendingApproval(approvalID string, pendingApproval pendingApproval) (capabilityToken, CapabilityResponse, bool) {
	server.mu.Lock()
	defer server.mu.Unlock()

	server.pruneExpiredLocked()
	ownerSession, found := server.sessionState.sessions[pendingApproval.ExecutionContext.ControlSessionID]
	if !found {
		return capabilityToken{}, CapabilityResponse{
			RequestID:         pendingApproval.Request.RequestID,
			Status:            ResponseStatusDenied,
			DenialReason:      "approval owner session is no longer active",
			DenialCode:        DenialCodeApprovalStateInvalid,
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
	}, CapabilityResponse{}, true
}

func (server *Server) auditApprovalDecisionDenial(controlSession controlSession, approvalID string, pendingApproval pendingApproval, denialResponse CapabilityResponse) error {
	if strings.TrimSpace(denialResponse.DenialCode) == "" || denialResponse.DenialCode == DenialCodeAuditUnavailable {
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
	if strings.TrimSpace(approval.ApprovalManifestSHA256) != "" {
		return approval
	}
	if strings.TrimSpace(approval.Request.Capability) == "" || approval.ExpiresAt.IsZero() {
		return approval
	}
	manifestSHA256, bodySHA256, err := buildCapabilityApprovalManifest(approval.Request, approval.ExpiresAt.UTC().UnixMilli())
	if err != nil {
		return approval
	}
	approval.ApprovalManifestSHA256 = manifestSHA256
	if strings.TrimSpace(approval.ExecutionBodySHA256) == "" {
		approval.ExecutionBodySHA256 = bodySHA256
	}
	approvalRecords[approvalID] = approval
	return approval
}

func (server *Server) markApprovalExecutionResult(approvalID string, executionStatus string) {
	server.mu.Lock()
	defer server.mu.Unlock()

	pendingApproval, found := server.approvalState.records[approvalID]
	if !found {
		return
	}
	if executionStatus != ResponseStatusSuccess {
		pendingApproval.State = approvalStateExecutionFailed
	}
	pendingApproval.ExecutedAt = server.now().UTC()
	server.approvalState.records[approvalID] = pendingApproval
}

func approvalDecisionHTTPStatus(denialCode string) int {
	switch denialCode {
	case DenialCodeApprovalNotFound:
		return http.StatusNotFound
	case DenialCodeApprovalTokenMissing, DenialCodeApprovalTokenInvalid, DenialCodeApprovalTokenExpired:
		return http.StatusUnauthorized
	default:
		return http.StatusForbidden
	}
}
