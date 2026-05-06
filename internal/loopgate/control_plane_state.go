package loopgate

import (
	"loopgate/internal/controlruntime"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"strings"
	"time"
)

type seenRequest = controlruntime.SeenRequest

type authNonceReplayStore = controlruntime.AuthNonceReplayStore

type usedToken = controlruntime.UsedToken

func (server *Server) pruneExpiredLocked() {
	nowUTC := server.now().UTC()
	if server.expirySweepMaxInterval > 0 && !server.nextExpirySweepAt.IsZero() && nowUTC.Before(server.nextExpirySweepAt) {
		return
	}

	earliestNextSweepAt := time.Time{}
	noteNextSweepCandidate := func(candidateTime time.Time) {
		if candidateTime.IsZero() {
			return
		}
		candidateTime = candidateTime.UTC()
		if earliestNextSweepAt.IsZero() || candidateTime.Before(earliestNextSweepAt) {
			earliestNextSweepAt = candidateTime
		}
	}

	for tokenString, tokenClaims := range server.sessionState.tokens {
		if nowUTC.After(tokenClaims.ExpiresAt) {
			delete(server.sessionState.tokens, tokenString)
			continue
		}
		noteNextSweepCandidate(tokenClaims.ExpiresAt)
	}
	for controlSessionID, activeSession := range server.sessionState.sessions {
		if nowUTC.After(activeSession.ExpiresAt) {
			delete(server.sessionState.sessions, controlSessionID)
			delete(server.approvalState.tokenIndex, approvalTokenHash(activeSession.ApprovalToken))
			continue
		}
		noteNextSweepCandidate(activeSession.ExpiresAt)
	}
	for approvalID, pendingApproval := range server.approvalState.records {
		if nowUTC.After(pendingApproval.ExpiresAt) {
			if pendingApproval.State == approvalStatePending {
				expiredApproval, transitionErr := setApprovalStateLocked(server.approvalState.records, approvalID, pendingApproval, approvalStateExpired)
				if transitionErr != nil {
					noteNextSweepCandidate(pendingApproval.ExpiresAt)
					continue
				}
				pendingApproval = expiredApproval
				noteNextSweepCandidate(pendingApproval.ExpiresAt.Add(requestReplayWindow))
				continue
			}
			if nowUTC.Sub(pendingApproval.ExpiresAt) > requestReplayWindow {
				delete(server.approvalState.records, approvalID)
				continue
			}
			noteNextSweepCandidate(pendingApproval.ExpiresAt.Add(requestReplayWindow))
			continue
		}
		noteNextSweepCandidate(pendingApproval.ExpiresAt)
	}
	for approvalRequestID, approvalRequest := range server.mcpGatewayApprovalRequests {
		if nowUTC.After(approvalRequest.ExpiresAt) {
			if approvalRequest.State == approvalStatePending {
				expiredApprovalRequest, transitionErr := setMCPGatewayApprovalStateLocked(server.mcpGatewayApprovalRequests, approvalRequestID, approvalRequest, approvalStateExpired)
				if transitionErr != nil {
					noteNextSweepCandidate(approvalRequest.ExpiresAt)
					continue
				}
				approvalRequest = expiredApprovalRequest
				noteNextSweepCandidate(approvalRequest.ExpiresAt.Add(requestReplayWindow))
				continue
			}
			if nowUTC.Sub(approvalRequest.ExpiresAt) > requestReplayWindow {
				delete(server.mcpGatewayApprovalRequests, approvalRequestID)
				continue
			}
			noteNextSweepCandidate(approvalRequest.ExpiresAt.Add(requestReplayWindow))
			continue
		}
		noteNextSweepCandidate(approvalRequest.ExpiresAt)
	}
	for requestKey, seenRequest := range server.replayState.seenRequests {
		if nowUTC.Sub(seenRequest.SeenAt) > requestReplayWindow {
			delete(server.replayState.seenRequests, requestKey)
			continue
		}
		noteNextSweepCandidate(seenRequest.SeenAt.Add(requestReplayWindow))
	}
	for nonceKey, seenNonce := range server.replayState.seenAuthNonces {
		if nowUTC.Sub(seenNonce.SeenAt) > requestReplayWindow {
			delete(server.replayState.seenAuthNonces, nonceKey)
			continue
		}
		noteNextSweepCandidate(seenNonce.SeenAt.Add(requestReplayWindow))
	}
	for tokenID, consumedToken := range server.replayState.usedTokens {
		if nowUTC.Sub(consumedToken.ConsumedAt) > requestReplayWindow {
			delete(server.replayState.usedTokens, tokenID)
			continue
		}
		noteNextSweepCandidate(consumedToken.ConsumedAt.Add(requestReplayWindow))
	}
	for controlSessionID, readTimestamps := range server.replayState.sessionReadCounts {
		prunedReadTimestamps := readTimestamps[:0]
		for _, readTimestamp := range readTimestamps {
			if nowUTC.Sub(readTimestamp) >= fsReadRateWindow {
				continue
			}
			prunedReadTimestamps = append(prunedReadTimestamps, readTimestamp)
			noteNextSweepCandidate(readTimestamp.Add(fsReadRateWindow))
		}
		if len(prunedReadTimestamps) == 0 {
			delete(server.replayState.sessionReadCounts, controlSessionID)
			continue
		}
		server.replayState.sessionReadCounts[controlSessionID] = prunedReadTimestamps
	}
	for burstKey, authDeniedBurst := range server.replayState.authDeniedBursts {
		if nowUTC.Sub(authDeniedBurst.LastSeenAt) >= authDeniedAuditBurstWindow {
			delete(server.replayState.authDeniedBursts, burstKey)
			continue
		}
		noteNextSweepCandidate(authDeniedBurst.LastSeenAt.Add(authDeniedAuditBurstWindow))
	}
	for peerUID, hookTimestamps := range server.replayState.hookPreValidateCounts {
		prunedHookTimestamps := hookTimestamps[:0]
		for _, hookTimestamp := range hookTimestamps {
			if nowUTC.Sub(hookTimestamp) >= server.hookPreValidateRateWindow {
				continue
			}
			prunedHookTimestamps = append(prunedHookTimestamps, hookTimestamp)
			noteNextSweepCandidate(hookTimestamp.Add(server.hookPreValidateRateWindow))
		}
		if len(prunedHookTimestamps) == 0 {
			delete(server.replayState.hookPreValidateCounts, peerUID)
			continue
		}
		server.replayState.hookPreValidateCounts[peerUID] = prunedHookTimestamps
	}
	for rateLimitKey, failureTimestamps := range server.replayState.hookPeerAuthFailureCounts {
		prunedFailureTimestamps := failureTimestamps[:0]
		for _, failureTimestamp := range failureTimestamps {
			if nowUTC.Sub(failureTimestamp) >= server.hookPeerAuthFailureWindow {
				continue
			}
			prunedFailureTimestamps = append(prunedFailureTimestamps, failureTimestamp)
			noteNextSweepCandidate(failureTimestamp.Add(server.hookPeerAuthFailureWindow))
		}
		if len(prunedFailureTimestamps) == 0 {
			delete(server.replayState.hookPeerAuthFailureCounts, rateLimitKey)
			continue
		}
		server.replayState.hookPeerAuthFailureCounts[rateLimitKey] = prunedFailureTimestamps
	}
	if server.expirySweepMaxInterval <= 0 {
		server.nextExpirySweepAt = time.Time{}
		return
	}

	maxScheduledSweepAt := nowUTC.Add(server.expirySweepMaxInterval)
	switch {
	case earliestNextSweepAt.IsZero():
		server.nextExpirySweepAt = time.Time{}
	case earliestNextSweepAt.Before(nowUTC):
		server.nextExpirySweepAt = nowUTC
	case earliestNextSweepAt.Before(maxScheduledSweepAt):
		server.nextExpirySweepAt = earliestNextSweepAt
	default:
		server.nextExpirySweepAt = maxScheduledSweepAt
	}
}

func (server *Server) noteExpiryCandidateLocked(candidateTime time.Time) {
	if server.expirySweepMaxInterval <= 0 || candidateTime.IsZero() {
		return
	}
	candidateTime = candidateTime.UTC()
	if server.nextExpirySweepAt.IsZero() || candidateTime.Before(server.nextExpirySweepAt) {
		server.nextExpirySweepAt = candidateTime
	}
}

func (server *Server) noteReplayWindowCandidateLocked(seenAt time.Time) {
	if seenAt.IsZero() {
		return
	}
	server.noteExpiryCandidateLocked(seenAt.UTC().Add(requestReplayWindow))
}

func (server *Server) currentNonceReplayStore() authNonceReplayStore {
	if server.nonceReplayStore != nil {
		return server.nonceReplayStore
	}
	return controlruntime.NewSnapshotNonceReplayStore(server.noncePath, requestReplayWindow)
}

func (server *Server) loadNonceReplayState() error {
	loadedNonces, err := server.currentNonceReplayStore().Load(server.now().UTC())
	if err != nil {
		return err
	}
	for nonceKey, seenNonce := range loadedNonces {
		server.replayState.seenAuthNonces[nonceKey] = seenNonce
	}
	return nil
}

func (server *Server) saveNonceReplayState() error {
	server.mu.Lock()
	stateSnapshot := controlruntime.CopySeenRequests(server.replayState.seenAuthNonces)
	server.mu.Unlock()
	return server.currentNonceReplayStore().Compact(stateSnapshot)
}

func (server *Server) countPendingApprovalsForSessionLocked(controlSessionID string) int {
	pendingCount := 0
	for _, pendingApproval := range server.approvalState.records {
		if pendingApproval.ControlSessionID != controlSessionID {
			continue
		}
		if pendingApproval.State == approvalStatePending {
			pendingCount++
		}
	}
	return pendingCount
}

func (server *Server) countPendingMCPGatewayApprovalRequestsForSessionLocked(controlSessionID string) int {
	pendingCount := 0
	for _, approvalRequest := range server.mcpGatewayApprovalRequests {
		if approvalRequest.ControlSessionID != controlSessionID {
			continue
		}
		if approvalRequest.State != approvalStatePending {
			continue
		}
		pendingCount++
	}
	return pendingCount
}

// recordRequest returns nil when the request_id is accepted for replay tracking, or a denial
// when duplicate or when the replay map is saturated (fail closed — no eviction).
func (server *Server) recordRequest(controlSessionID string, capabilityRequest controlapipkg.CapabilityRequest) *controlapipkg.CapabilityResponse {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.pruneExpiredLocked()
	recordedRequest, recordStatus := controlruntime.RecordSeenRequest(server.replayState.seenRequests, server.maxSeenRequestReplayEntries, controlSessionID, capabilityRequest.RequestID, server.now().UTC())
	switch recordStatus {
	case controlruntime.ReplayRecordDuplicate:
		return &controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "duplicate request_id was rejected",
			DenialCode:   controlapipkg.DenialCodeRequestReplayDetected,
		}
	case controlruntime.ReplayRecordSaturated:
		return &controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "request replay store is at capacity",
			DenialCode:   controlapipkg.DenialCodeReplayStateSaturated,
		}
	}
	server.noteReplayWindowCandidateLocked(recordedRequest.SeenAt)
	return nil
}

func (server *Server) consumeExecutionToken(tokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest) (controlapipkg.CapabilityResponse, bool) {
	if strings.TrimSpace(tokenClaims.BoundCapability) != "" && tokenClaims.BoundCapability != capabilityRequest.Capability {
		return controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "capability token binding does not match requested capability",
			DenialCode:   controlapipkg.DenialCodeCapabilityTokenBindingInvalid,
		}, true
	}
	if strings.TrimSpace(tokenClaims.BoundArgumentHash) != "" && tokenClaims.BoundArgumentHash != normalizedArgumentHash(capabilityRequest.Arguments) {
		return controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "capability token binding does not match normalized arguments",
			DenialCode:   controlapipkg.DenialCodeCapabilityTokenBindingInvalid,
		}, true
	}
	if !tokenClaims.SingleUse {
		return controlapipkg.CapabilityResponse{}, false
	}

	server.mu.Lock()
	defer server.mu.Unlock()

	server.pruneExpiredLocked()
	consumedToken, consumeStatus := controlruntime.ConsumeUsedToken(server.replayState.usedTokens, tokenClaims.TokenID, tokenClaims.ParentTokenID, tokenClaims.ControlSessionID, capabilityRequest.Capability, normalizedArgumentHash(capabilityRequest.Arguments), server.now().UTC())
	if consumeStatus == controlruntime.ReplayRecordDuplicate {
		return controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "single-use capability token was already consumed",
			DenialCode:   controlapipkg.DenialCodeCapabilityTokenReused,
		}, true
	}
	server.noteReplayWindowCandidateLocked(consumedToken.ConsumedAt)
	return controlapipkg.CapabilityResponse{}, false
}

// recordAuthNonce returns nil if the nonce is new and recorded, a denial for replay, or a
// denial when the nonce map is saturated (fail closed).
func (server *Server) recordAuthNonce(controlSessionID string, requestNonce string) *controlapipkg.CapabilityResponse {
	nonceKey := controlSessionID + ":" + requestNonce
	server.mu.Lock()
	server.pruneExpiredLocked()
	if _, found := server.replayState.seenAuthNonces[nonceKey]; found {
		server.mu.Unlock()
		return &controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "request nonce replay was rejected",
			DenialCode:   controlapipkg.DenialCodeRequestNonceReplayDetected,
		}
	}
	if len(server.replayState.seenAuthNonces) >= server.maxAuthNonceReplayEntries {
		server.mu.Unlock()
		return &controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "request nonce replay store is at capacity",
			DenialCode:   controlapipkg.DenialCodeReplayStateSaturated,
		}
	}
	server.replayState.seenAuthNonces[nonceKey] = seenRequest{
		ControlSessionID: controlSessionID,
		SeenAt:           server.now().UTC(),
	}
	recordedNonce := server.replayState.seenAuthNonces[nonceKey]
	server.noteReplayWindowCandidateLocked(recordedNonce.SeenAt)
	server.mu.Unlock()

	if err := server.currentNonceReplayStore().Record(nonceKey, recordedNonce); err != nil {
		server.mu.Lock()
		delete(server.replayState.seenAuthNonces, nonceKey)
		server.mu.Unlock()
		return &controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "nonce replay state is unavailable",
			DenialCode:   controlapipkg.DenialCodeAuditUnavailable,
		}
	}
	return nil
}
