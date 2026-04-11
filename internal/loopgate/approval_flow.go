package loopgate

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	policypkg "morph/internal/policy"
	"morph/internal/secrets"
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

// capabilityTokenForMorphlingApprovalFinalize builds a token for finalizeSpawnedMorphling after UI/API approval.
// It prefers the live control session (peer binding, expiry, tenancy) so execution matches the session that
// is still open when the operator approves; ExecutionContext carries a snapshot if the session is gone.
func (server *Server) capabilityTokenForMorphlingApprovalFinalize(pending pendingApproval) capabilityToken {
	server.mu.Lock()
	session, sessionFound := server.sessions[pending.ControlSessionID]
	server.mu.Unlock()
	token := capabilityToken{
		ControlSessionID:    pending.ExecutionContext.ControlSessionID,
		ActorLabel:          pending.ExecutionContext.ActorLabel,
		ClientSessionLabel:  pending.ExecutionContext.ClientSessionLabel,
		AllowedCapabilities: copyCapabilitySet(pending.ExecutionContext.AllowedCapabilities),
		TenantID:            pending.ExecutionContext.TenantID,
		UserID:              pending.ExecutionContext.UserID,
		ExpiresAt:           pending.ExpiresAt,
	}
	if sessionFound {
		token.PeerIdentity = session.PeerIdentity
		token.TenantID = session.TenantID
		token.UserID = session.UserID
		token.ExpiresAt = session.ExpiresAt
	}
	return token
}

func approvalTokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func (server *Server) authenticateApproval(writer http.ResponseWriter, request *http.Request) (controlSession, bool) {
	requestPeerIdentity, ok := peerIdentityFromContext(request.Context())
	if !ok {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "missing authenticated peer identity",
			DenialCode:   DenialCodeApprovalTokenInvalid,
		})
		return controlSession{}, false
	}

	approvalToken := strings.TrimSpace(request.Header.Get("X-Loopgate-Approval-Token"))
	if approvalToken == "" {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "missing approval token",
			DenialCode:   DenialCodeApprovalTokenMissing,
		})
		return controlSession{}, false
	}

	tokenHash := approvalTokenHash(approvalToken)

	server.mu.Lock()
	server.pruneExpiredLocked()
	controlSessionID, indexed := server.approvalTokenIndex[tokenHash]
	if !indexed {
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "invalid approval token",
			DenialCode:   DenialCodeApprovalTokenInvalid,
		})
		return controlSession{}, false
	}
	activeSession, sessionExists := server.sessions[controlSessionID]
	if !sessionExists {
		delete(server.approvalTokenIndex, tokenHash)
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "invalid approval token",
			DenialCode:   DenialCodeApprovalTokenInvalid,
		})
		return controlSession{}, false
	}

	// Constant-time comparison to prevent timing oracle on the raw token value.
	if subtle.ConstantTimeCompare([]byte(activeSession.ApprovalToken), []byte(approvalToken)) != 1 {
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "invalid approval token",
			DenialCode:   DenialCodeApprovalTokenInvalid,
		})
		return controlSession{}, false
	}

	if server.now().UTC().After(activeSession.ExpiresAt) {
		delete(server.sessions, controlSessionID)
		delete(server.approvalTokenIndex, tokenHash)
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "expired approval token",
			DenialCode:   DenialCodeApprovalTokenExpired,
		})
		return controlSession{}, false
	}
	if activeSession.PeerIdentity != requestPeerIdentity {
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "approval token peer binding mismatch",
			DenialCode:   DenialCodeApprovalTokenInvalid,
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
		return fmt.Sprintf("Grant write access to %s for 8 hours", grantRootPath)
	default:
		return policyDecision.Reason
	}
}

func buildApprovalGrantedAuditData(approvalID string, pendingApproval pendingApproval) map[string]interface{} {
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
	return auditData
}

func buildApprovalOperatorDeniedAuditData(approvalID string, pendingApproval pendingApproval) map[string]interface{} {
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
	return auditData
}

// validatePendingApprovalDecisionLocked performs session, expiry, nonce, and manifest checks
// for a pending approval without writing audit events or changing approval state.
// Must be called with server.mu held.
func (server *Server) validatePendingApprovalDecisionLocked(controlSession controlSession, approvalID string, decisionRequest ApprovalDecisionRequest) (pendingApproval, CapabilityResponse, bool) {
	server.pruneExpiredLocked()
	pendingApproval, found := server.approvals[approvalID]
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
		server.approvals[approvalID] = pendingApproval
		return pendingApproval, CapabilityResponse{
			RequestID:         pendingApproval.Request.RequestID,
			Status:            ResponseStatusDenied,
			DenialReason:      "approval request expired",
			DenialCode:        DenialCodeApprovalDenied,
			ApprovalRequestID: approvalID,
		}, false
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
	pendingApproval = backfillApprovalManifestLocked(server.approvals, approvalID, pendingApproval)

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

func (server *Server) validatePendingApprovalDecision(controlSession controlSession, approvalID string, decisionRequest ApprovalDecisionRequest) (pendingApproval, CapabilityResponse, bool) {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.validatePendingApprovalDecisionLocked(controlSession, approvalID, decisionRequest)
}

// commitApprovalGrantConsumed appends approval.granted and transitions the approval to consumed.
// Call after approval-scoped work succeeds (e.g. morphling spawn finalization) so a spawn failure
// does not leave a consumed approval with no morphling. If audit append fails after side effects,
// the operator may need manual recovery (rare).
func (server *Server) commitApprovalGrantConsumed(approvalID string, expectedDecisionNonce string) error {
	expectedDecisionNonce = strings.TrimSpace(expectedDecisionNonce)
	server.mu.Lock()
	defer server.mu.Unlock()

	pendingApproval, found := server.approvals[approvalID]
	if !found {
		return fmt.Errorf("approval request not found")
	}
	if pendingApproval.State != approvalStatePending {
		return fmt.Errorf("approval request is no longer pending")
	}
	if expectedDecisionNonce == "" || pendingApproval.DecisionNonce != expectedDecisionNonce {
		return fmt.Errorf("approval decision nonce mismatch")
	}

	grantAuditData := buildApprovalGrantedAuditData(approvalID, pendingApproval)
	if session, ok := server.sessions[pendingApproval.ControlSessionID]; ok {
		mergeAuditTenancyFromControlSession(grantAuditData, session)
	}
	grantRoot := strings.TrimSpace(interfaceStringValue(pendingApproval.Metadata["grant_root"]))
	var grantExpiresAt time.Time
	if grantRoot != "" {
		grantExpiresAt = server.now().UTC().Add(operatorMountWriteGrantTTL)
		grantAuditData["grant_expires_at_utc"] = grantExpiresAt.Format(time.RFC3339Nano)
	}
	if err := server.logEvent("approval.granted", pendingApproval.ControlSessionID, grantAuditData); err != nil {
		return err
	}
	if grantRoot != "" {
		controlSession := server.sessions[pendingApproval.ControlSessionID]
		if controlSession.OperatorMountWriteGrants == nil {
			controlSession.OperatorMountWriteGrants = make(map[string]time.Time)
		}
		controlSession.OperatorMountWriteGrants[grantRoot] = grantExpiresAt
		server.sessions[pendingApproval.ControlSessionID] = controlSession
	}
	pendingApproval.State = approvalStateConsumed
	pendingApproval.DecisionSubmittedAt = server.now().UTC()
	pendingApproval.DecisionNonce = ""
	server.approvals[approvalID] = pendingApproval
	return nil
}

func (server *Server) validateAndRecordApprovalDecision(controlSession controlSession, approvalID string, decisionRequest ApprovalDecisionRequest) (pendingApproval, CapabilityResponse, bool) {
	server.mu.Lock()
	defer server.mu.Unlock()

	pendingApproval, denialResponse, ok := server.validatePendingApprovalDecisionLocked(controlSession, approvalID, decisionRequest)
	if !ok {
		return pendingApproval, denialResponse, false
	}

	if decisionRequest.Approved {
		grantAuditData := buildApprovalGrantedAuditData(approvalID, pendingApproval)
		mergeAuditTenancyFromControlSession(grantAuditData, controlSession)
		if err := server.logEvent("approval.granted", pendingApproval.ControlSessionID, grantAuditData); err != nil {
			return pendingApproval, CapabilityResponse{
				RequestID:         pendingApproval.Request.RequestID,
				Status:            ResponseStatusError,
				DenialReason:      "control-plane audit is unavailable",
				DenialCode:        DenialCodeAuditUnavailable,
				ApprovalRequestID: approvalID,
			}, false
		}
		pendingApproval.State = approvalStateConsumed
	} else {
		deniedAuditData := buildApprovalOperatorDeniedAuditData(approvalID, pendingApproval)
		mergeAuditTenancyFromControlSession(deniedAuditData, controlSession)
		if err := server.logEvent("approval.denied", pendingApproval.ControlSessionID, deniedAuditData); err != nil {
			return pendingApproval, CapabilityResponse{
				RequestID:         pendingApproval.Request.RequestID,
				Status:            ResponseStatusError,
				DenialReason:      "control-plane audit is unavailable",
				DenialCode:        DenialCodeAuditUnavailable,
				ApprovalRequestID: approvalID,
			}, false
		}
		pendingApproval.State = approvalStateDenied
	}
	pendingApproval.DecisionSubmittedAt = server.now().UTC()
	pendingApproval.DecisionNonce = ""
	server.approvals[approvalID] = pendingApproval
	return pendingApproval, CapabilityResponse{}, true
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

	pendingApproval, found := server.approvals[approvalID]
	if !found {
		return
	}
	if executionStatus != ResponseStatusSuccess {
		pendingApproval.State = approvalStateExecutionFailed
	}
	pendingApproval.ExecutedAt = server.now().UTC()
	server.approvals[approvalID] = pendingApproval
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
