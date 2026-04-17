package loopgate

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"loopgate/internal/identifiers"
)

func (server *Server) handleSessionOpen(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var openRequest OpenSessionRequest
	if err := server.decodeJSONBody(writer, request, maxOpenSessionBodyBytes, &openRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	openRequest.Actor = strings.TrimSpace(openRequest.Actor)
	openRequest.SessionID = strings.TrimSpace(openRequest.SessionID)
	normalizedCapabilities := normalizedCapabilityList(openRequest.RequestedCapabilities)
	openRequest.RequestedCapabilities = normalizedCapabilities
	if err := openRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if err := identifiers.ValidateSafeIdentifier("actor", defaultLabel(openRequest.Actor, "client")); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if err := identifiers.ValidateSafeIdentifier("session_id", defaultLabel(openRequest.SessionID, "session")); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	normalizedOperatorMounts, mountErr := normalizeOperatorMountPathsForSession(openRequest.Actor, openRequest.OperatorMountPaths)
	if mountErr != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: mountErr.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	normalizedPrimaryOperatorMount, primaryMountErr := normalizePrimaryOperatorMountPathForSession(openRequest.Actor, openRequest.PrimaryOperatorMountPath, normalizedOperatorMounts)
	if primaryMountErr != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: primaryMountErr.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if len(normalizedCapabilities) == 0 {
		server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "requested_capabilities must not be empty",
			DenialCode:   DenialCodeCapabilityScopeRequired,
		})
		return
	}

	// Security invariant: capability scope is server-granted, not client-declared.
	// The client's requested list is intersected with the server's registered capabilities.
	// Unknown capabilities are rejected; the client cannot escalate beyond what the server offers.
	grantedCapabilities, unknownCapabilities := server.filterGrantedCapabilities(normalizedCapabilities)
	if len(unknownCapabilities) > 0 {
		server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: fmt.Sprintf("unknown capabilities requested: %s", strings.Join(unknownCapabilities, ", ")),
			DenialCode:   DenialCodeCapabilityTokenScopeDenied,
		})
		return
	}
	if len(grantedCapabilities) == 0 {
		server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "no requested capabilities are available",
			DenialCode:   DenialCodeCapabilityScopeRequired,
		})
		return
	}

	requestPeerIdentity, ok := peerIdentityFromContext(request.Context())
	if !ok {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "missing authenticated peer identity",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return
	}
	if len(normalizedOperatorMounts) > 0 && strings.TrimSpace(server.expectedClientPath) == "" {
		server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "operator mount binding requires expected client executable pinning",
			DenialCode:   DenialCodeControlSessionBindingInvalid,
		})
		return
	}

	if server.expectedClientPath != "" {
		if server.resolveExePath == nil {
			server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
				Status:       ResponseStatusDenied,
				DenialReason: "cannot resolve connecting process executable",
				DenialCode:   DenialCodeProcessBindingRejected,
			})
			return
		}
		exePath, exeErr := server.resolveExePath(requestPeerIdentity.PID)
		if exeErr != nil {
			server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
				Status:       ResponseStatusDenied,
				DenialReason: "cannot resolve connecting process executable",
				DenialCode:   DenialCodeProcessBindingRejected,
			})
			return
		}
		if normalizeSessionExecutablePinPath(exePath) != server.expectedClientPath {
			if server.reportSecurityWarning != nil {
				server.reportSecurityWarning("session_client_executable_mismatch", errors.New("executable path mismatch"))
			}
			server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
				Status:       ResponseStatusDenied,
				DenialReason: "connecting process does not match expected client executable",
				DenialCode:   DenialCodeProcessBindingRejected,
			})
			return
		}
	}

	nowUTC := server.now().UTC()
	authoritativeWorkspaceID := server.deriveWorkspaceIDFromRepoRoot()
	requestedWorkspaceID := strings.TrimSpace(openRequest.WorkspaceID)
	if requestedWorkspaceID != "" && requestedWorkspaceID != authoritativeWorkspaceID {
		server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "workspace binding does not match this Loopgate runtime",
			DenialCode:   DenialCodeControlSessionBindingInvalid,
		})
		return
	}
	if err := server.retireDeadPeerSessionsForUID(requestPeerIdentity.UID); err != nil {
		server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "control-plane orphan session recovery failed",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}
	server.mu.Lock()
	server.pruneExpiredLocked()

	var replacedSessionID string
	var replacedSession controlSession
	replacedSessionTokens := make(map[string]capabilityToken)
	hadReplacedSession := false
	previousSessionOpenAtUTC, hadPreviousSessionOpenAt := server.sessionState.openByUID[requestPeerIdentity.UID]

	// Idempotent re-open: if the same (UID, ClientSessionLabel) pair already has
	// an active session, replace it once the new session is ready. This prevents
	// session accumulation from client retries, capability expansion, or
	// reconnects, while keeping later denial paths from destroying the still-live
	// authoritative session.
	clientLabel := defaultLabel(openRequest.SessionID, "session")
	for csID, existingSession := range server.sessionState.sessions {
		if existingSession.PeerIdentity.UID == requestPeerIdentity.UID &&
			existingSession.ClientSessionLabel == clientLabel {
			replacedSessionID = csID
			replacedSession = existingSession
			hadReplacedSession = true
			for tokenString, tokenClaims := range server.sessionState.tokens {
				if tokenClaims.ControlSessionID == csID {
					replacedSessionTokens[tokenString] = tokenClaims
				}
			}
			break // at most one match per (UID, label)
		}
	}

	activeSessionCountForUID := server.activeSessionsForPeerUIDLocked(requestPeerIdentity.UID)
	totalSessionCount := len(server.sessionState.sessions)
	if hadReplacedSession {
		activeSessionCountForUID--
		totalSessionCount--
	}

	if server.maxTotalControlSessions > 0 && totalSessionCount >= server.maxTotalControlSessions {
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusTooManyRequests, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "control-plane session store is at capacity",
			DenialCode:   DenialCodeControlPlaneStateSaturated,
		})
		return
	}

	if server.maxActiveSessionsPerUID > 0 && activeSessionCountForUID >= server.maxActiveSessionsPerUID {
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusTooManyRequests, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "active control session limit reached for this peer identity",
			DenialCode:   DenialCodeSessionActiveLimitReached,
		})
		return
	}
	if server.sessionOpenMinInterval > 0 {
		lastOpenedAtUTC := server.sessionState.openByUID[requestPeerIdentity.UID]
		if !lastOpenedAtUTC.IsZero() {
			elapsed := nowUTC.Sub(lastOpenedAtUTC)
			if elapsed < server.sessionOpenMinInterval {
				server.mu.Unlock()
				server.writeJSON(writer, http.StatusTooManyRequests, CapabilityResponse{
					Status:       ResponseStatusDenied,
					DenialReason: fmt.Sprintf("session open rate limit exceeded; retry after %s", (server.sessionOpenMinInterval - elapsed).Round(time.Millisecond)),
					DenialCode:   DenialCodeSessionOpenRateLimited,
				})
				return
			}
		}
	}

	controlSessionID, err := randomHex(16)
	if err != nil {
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "failed to create control session",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}
	capabilityTokenString, err := randomHex(24)
	if err != nil {
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "failed to mint capability token",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}
	approvalTokenString, err := randomHex(24)
	if err != nil {
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "failed to mint approval token",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}
	approvalTokenID, err := randomHex(8)
	if err != nil {
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "failed to mint approval token identifier",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}
	tokenID, err := randomHex(8)
	if err != nil {
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "failed to mint token identifier",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}
	expiresAt := nowUTC.Add(sessionTTL)
	sessionMACKey := server.sessionMACKeyForControlSessionAtEpoch(controlSessionID, server.currentSessionMACEpochIndex())
	deploymentTenantID := strings.TrimSpace(server.runtimeConfig.Tenancy.DeploymentTenantID)
	deploymentUserID := strings.TrimSpace(server.runtimeConfig.Tenancy.DeploymentUserID)
	tokenClaims := capabilityToken{
		TokenID:             tokenID,
		Token:               capabilityTokenString,
		ControlSessionID:    controlSessionID,
		ActorLabel:          defaultLabel(openRequest.Actor, "client"),
		ClientSessionLabel:  defaultLabel(openRequest.SessionID, "session"),
		AllowedCapabilities: capabilitySet(grantedCapabilities),
		PeerIdentity:        requestPeerIdentity,
		TenantID:            deploymentTenantID,
		UserID:              deploymentUserID,
		ExpiresAt:           expiresAt,
	}

	if hadReplacedSession {
		for replacedTokenString := range replacedSessionTokens {
			delete(server.sessionState.tokens, replacedTokenString)
		}
		delete(server.approvalState.tokenIndex, approvalTokenHash(replacedSession.ApprovalToken))
		delete(server.sessionState.sessions, replacedSessionID)
	}
	server.sessionState.sessions[controlSessionID] = controlSession{
		ID:                       controlSessionID,
		ActorLabel:               tokenClaims.ActorLabel,
		ClientSessionLabel:       tokenClaims.ClientSessionLabel,
		WorkspaceID:              authoritativeWorkspaceID,
		OperatorMountPaths:       normalizedOperatorMounts,
		PrimaryOperatorMountPath: normalizedPrimaryOperatorMount,
		RequestedCapabilities:    capabilitySet(grantedCapabilities),
		ApprovalToken:            approvalTokenString,
		ApprovalTokenID:          approvalTokenID,
		SessionMACKey:            sessionMACKey,
		PeerIdentity:             requestPeerIdentity,
		TenantID:                 deploymentTenantID,
		UserID:                   deploymentUserID,
		ExpiresAt:                expiresAt,
		CreatedAt:                nowUTC,
	}
	server.sessionState.tokens[capabilityTokenString] = tokenClaims
	server.approvalState.tokenIndex[approvalTokenHash(approvalTokenString)] = controlSessionID
	server.sessionState.openByUID[requestPeerIdentity.UID] = nowUTC
	server.noteExpiryCandidateLocked(expiresAt)
	server.mu.Unlock()

	if err := server.logEvent("session.opened", controlSessionID, map[string]interface{}{
		"actor_label":                tokenClaims.ActorLabel,
		"client_session_label":       tokenClaims.ClientSessionLabel,
		"control_session_id":         controlSessionID,
		"workspace_id":               authoritativeWorkspaceID,
		"operator_mount_count":       len(normalizedOperatorMounts),
		"requested_capability_count": len(normalizedCapabilities),
		"granted_capability_count":   len(grantedCapabilities),
		"token_id":                   tokenID,
		"approval_token_id":          approvalTokenID,
		"peer_uid":                   requestPeerIdentity.UID,
		"peer_pid":                   requestPeerIdentity.PID,
		"peer_epid":                  requestPeerIdentity.EPID,
		"expires_at_utc":             expiresAt.Format(time.RFC3339Nano),
	}); err != nil {
		server.mu.Lock()
		delete(server.sessionState.sessions, controlSessionID)
		delete(server.sessionState.tokens, capabilityTokenString)
		delete(server.approvalState.tokenIndex, approvalTokenHash(approvalTokenString))
		if hadReplacedSession {
			server.sessionState.sessions[replacedSessionID] = replacedSession
			for restoredTokenString, restoredTokenClaims := range replacedSessionTokens {
				server.sessionState.tokens[restoredTokenString] = restoredTokenClaims
			}
			server.approvalState.tokenIndex[approvalTokenHash(replacedSession.ApprovalToken)] = replacedSessionID
		}
		if hadPreviousSessionOpenAt {
			server.sessionState.openByUID[requestPeerIdentity.UID] = previousSessionOpenAtUTC
		} else {
			delete(server.sessionState.openByUID, requestPeerIdentity.UID)
		}
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "control-plane audit is unavailable",
			DenialCode:   DenialCodeAuditUnavailable,
		})
		return
	}

	server.writeJSON(writer, http.StatusOK, OpenSessionResponse{
		ControlSessionID: controlSessionID,
		CapabilityToken:  capabilityTokenString,
		ApprovalToken:    approvalTokenString,
		SessionMACKey:    sessionMACKey,
		ExpiresAtUTC:     expiresAt.Format(time.RFC3339Nano),
	})
	personaName, personaVersion := server.loadPersonaDisplaySummary()
	server.emitUIEvent(controlSessionID, UIEventTypeSessionInfo, UIEventSessionInfo{
		ControlSessionID:   controlSessionID,
		ActorLabel:         tokenClaims.ActorLabel,
		ClientSessionLabel: tokenClaims.ClientSessionLabel,
		PersonaName:        personaName,
		PersonaVersion:     personaVersion,
	})
}

func (server *Server) handleSessionClose(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
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

	closedAtUTC := server.now().UTC()
	server.mu.Lock()
	server.pruneExpiredLocked()

	if _, found := server.sessionState.sessions[tokenClaims.ControlSessionID]; !found {
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "invalid capability token",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return
	}

	pendingApprovalCount := 0
	for _, pendingApproval := range server.approvalState.records {
		if pendingApproval.ControlSessionID == tokenClaims.ControlSessionID &&
			pendingApproval.State == approvalStatePending {
			pendingApprovalCount++
		}
	}
	if pendingApprovalCount > 0 {
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusConflict, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: fmt.Sprintf("control session has %d pending approvals; resolve or wait for them before closing the session", pendingApprovalCount),
			DenialCode:   DenialCodeSessionCloseBlocked,
		})
		return
	}

	server.mu.Unlock()

	auditData := map[string]interface{}{
		"closed_at_utc": closedAtUTC.Format(time.RFC3339Nano),
	}
	if err := server.retireControlSession(tokenClaims.ControlSessionID, closedAtUTC, "session.closed", auditData); err != nil {
		server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "control-plane audit is unavailable",
			DenialCode:   DenialCodeAuditUnavailable,
		})
		return
	}

	server.writeJSON(writer, http.StatusOK, CloseSessionResponse{
		Status:           ResponseStatusSuccess,
		ControlSessionID: tokenClaims.ControlSessionID,
		ClosedAtUTC:      closedAtUTC.Format(time.RFC3339Nano),
	})
}
