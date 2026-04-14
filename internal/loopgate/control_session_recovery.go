package loopgate

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func (server *Server) retireDeadPeerSessionsForUID(peerUID uint32) error {
	if server.processExists == nil {
		return nil
	}

	server.mu.Lock()
	server.pruneExpiredLocked()
	candidateSessions := make([]controlSession, 0)
	for _, activeSession := range server.sessions {
		if activeSession.PeerIdentity.UID != peerUID {
			continue
		}
		candidateSessions = append(candidateSessions, activeSession)
	}
	server.mu.Unlock()

	sort.Slice(candidateSessions, func(leftIndex int, rightIndex int) bool {
		leftSession := candidateSessions[leftIndex]
		rightSession := candidateSessions[rightIndex]
		if !leftSession.CreatedAt.Equal(rightSession.CreatedAt) {
			return leftSession.CreatedAt.Before(rightSession.CreatedAt)
		}
		return leftSession.ID < rightSession.ID
	})

	for _, candidateSession := range candidateSessions {
		exists, err := server.processExists(candidateSession.PeerIdentity.PID)
		if err != nil {
			return fmt.Errorf("check peer pid %d for control session %s: %w", candidateSession.PeerIdentity.PID, candidateSession.ID, err)
		}
		if exists {
			continue
		}
		if err := server.retireOrphanedControlSession(candidateSession.ID); err != nil {
			return err
		}
	}

	return nil
}

func (server *Server) retireOrphanedControlSession(controlSessionID string) error {
	if err := server.terminateActiveMorphlingsForControlSession(controlSessionID, morphlingReasonParentSessionEnded); err != nil {
		return err
	}
	if err := server.cancelPendingApprovalsForControlSession(controlSessionID, "parent control session process is no longer alive"); err != nil {
		return err
	}

	retiredAtUTC := server.now().UTC()
	if err := server.retireControlSession(controlSessionID, retiredAtUTC, "session.orphan_retired", map[string]interface{}{
		"retirement_reason": "peer_process_missing",
		"retired_at_utc":    retiredAtUTC.Format(time.RFC3339Nano),
	}); err != nil {
		return err
	}
	return nil
}

func (server *Server) terminateActiveMorphlingsForControlSession(controlSessionID string, terminationReason string) error {
	server.morphlingsMu.Lock()
	morphlingIDs := make([]string, 0)
	for morphlingID, record := range server.morphlings {
		if record.ParentControlSessionID != controlSessionID {
			continue
		}
		if !morphlingStateConsumesCapacity(record.State) {
			continue
		}
		morphlingIDs = append(morphlingIDs, morphlingID)
	}
	server.morphlingsMu.Unlock()

	sort.Strings(morphlingIDs)
	for _, morphlingID := range morphlingIDs {
		if err := server.terminateMorphlingForControlSessionEnd(controlSessionID, morphlingID, terminationReason); err != nil {
			return err
		}
	}
	return nil
}

func (server *Server) terminateMorphlingForControlSessionEnd(controlSessionID string, morphlingID string, terminationReason string) error {
	nowUTC := server.now().UTC()

	server.morphlingsMu.Lock()
	record, found := server.morphlings[morphlingID]
	if !found {
		server.morphlingsMu.Unlock()
		return nil
	}
	if record.ParentControlSessionID != controlSessionID {
		server.morphlingsMu.Unlock()
		return nil
	}
	if !morphlingStateConsumesCapacity(record.State) {
		server.morphlingsMu.Unlock()
		return nil
	}

	previousRecord := cloneMorphlingRecord(record)
	terminatingRecord, err := server.transitionMorphlingLocked(morphlingID, morphlingEventBeginTermination, nowUTC, func(updatedRecord *morphlingRecord) error {
		updatedRecord.Outcome = morphlingOutcomeCancelled
		updatedRecord.TerminationReason = strings.TrimSpace(terminationReason)
		if updatedRecord.TerminationReason == "" {
			updatedRecord.TerminationReason = morphlingReasonParentSessionEnded
		}
		updatedRecord.ReviewDeadlineUTC = ""
		return nil
	})
	server.morphlingsMu.Unlock()
	if err != nil {
		return err
	}

	if err := server.logEvent("morphling.terminating", controlSessionID, map[string]interface{}{
		"morphling_id":       terminatingRecord.MorphlingID,
		"outcome":            terminatingRecord.Outcome,
		"termination_reason": terminatingRecord.TerminationReason,
		"control_session_id": controlSessionID,
	}); err != nil {
		if rollbackErr := server.rollbackMorphlingRecordAfterAuditFailure(terminatingRecord.MorphlingID, morphlingStateTerminating, previousRecord); rollbackErr != nil {
			return fmt.Errorf("%w: %v (rollback failed: %v)", errMorphlingAuditUnavailable, err, rollbackErr)
		}
		return fmt.Errorf("%w: %v", errMorphlingAuditUnavailable, err)
	}

	_, err = server.completeMorphlingTermination(controlSessionID, morphlingID)
	return err
}

func (server *Server) cancelPendingApprovalsForControlSession(controlSessionID string, cancellationReason string) error {
	server.mu.Lock()
	pendingApprovals := make([]pendingApproval, 0)
	for _, approvalRecord := range server.approvals {
		if approvalRecord.ControlSessionID != controlSessionID {
			continue
		}
		if approvalRecord.State != approvalStatePending {
			continue
		}
		pendingApprovals = append(pendingApprovals, approvalRecord)
	}
	server.mu.Unlock()

	sort.Slice(pendingApprovals, func(leftIndex int, rightIndex int) bool {
		return pendingApprovals[leftIndex].ID < pendingApprovals[rightIndex].ID
	})

	for _, pendingApproval := range pendingApprovals {
		if err := server.cancelPendingApproval(pendingApproval.ID, cancellationReason); err != nil {
			return err
		}
	}
	return nil
}

func (server *Server) cancelPendingApproval(approvalID string, cancellationReason string) error {
	cancelledAtUTC := server.now().UTC()

	server.mu.Lock()
	pendingApproval, found := server.approvals[approvalID]
	if !found {
		server.mu.Unlock()
		return nil
	}
	if pendingApproval.State != approvalStatePending {
		server.mu.Unlock()
		return nil
	}

	previousApproval := pendingApproval
	pendingApproval.State = approvalStateCancelled
	server.approvals[approvalID] = pendingApproval
	server.mu.Unlock()

	auditData := map[string]interface{}{
		"approval_request_id":  approvalID,
		"capability":           pendingApproval.Request.Capability,
		"approval_state":       approvalStateCancelled,
		"control_session_id":   pendingApproval.ControlSessionID,
		"actor_label":          pendingApproval.ExecutionContext.ActorLabel,
		"client_session_label": pendingApproval.ExecutionContext.ClientSessionLabel,
		"cancelled_at_utc":     cancelledAtUTC.Format(time.RFC3339Nano),
		"cancellation_reason":  strings.TrimSpace(cancellationReason),
	}
	if approvalClass, ok := pendingApproval.Metadata["approval_class"].(string); ok && strings.TrimSpace(approvalClass) != "" {
		auditData["approval_class"] = approvalClass
	}

	if err := server.logEvent("approval.cancelled", pendingApproval.ControlSessionID, auditData); err != nil {
		server.mu.Lock()
		currentApproval, currentFound := server.approvals[approvalID]
		if currentFound && currentApproval.State == approvalStateCancelled {
			server.approvals[approvalID] = previousApproval
		}
		server.mu.Unlock()
		return fmt.Errorf("%w: approval.cancelled audit append failed: %v", errMorphlingAuditUnavailable, err)
	}

	return nil
}

func (server *Server) retireControlSession(controlSessionID string, closedAtUTC time.Time, auditEventType string, extraAuditData map[string]interface{}) error {
	server.mu.Lock()
	activeSession, found := server.sessions[controlSessionID]
	if !found {
		server.mu.Unlock()
		return nil
	}

	sessionTokens := make(map[string]capabilityToken)
	for tokenString, activeTokenClaims := range server.tokens {
		if activeTokenClaims.ControlSessionID == controlSessionID {
			sessionTokens[tokenString] = activeTokenClaims
		}
	}
	approvalTokenHashValue := approvalTokenHash(activeSession.ApprovalToken)
	sessionReadCounts, hadSessionReadCounts := server.sessionReadCounts[controlSessionID]

	delete(server.sessions, controlSessionID)
	delete(server.approvalTokenIndex, approvalTokenHashValue)
	delete(server.sessionReadCounts, controlSessionID)
	for tokenString := range sessionTokens {
		delete(server.tokens, tokenString)
	}
	server.mu.Unlock()

	auditData := map[string]interface{}{
		"actor_label":            activeSession.ActorLabel,
		"client_session_label":   activeSession.ClientSessionLabel,
		"control_session_id":     controlSessionID,
		"requested_capabilities": len(activeSession.RequestedCapabilities),
		"retired_token_count":    len(sessionTokens),
		"peer_uid":               activeSession.PeerIdentity.UID,
		"peer_pid":               activeSession.PeerIdentity.PID,
		"peer_epid":              activeSession.PeerIdentity.EPID,
		"tenant_id":              activeSession.TenantID,
		"user_id":                activeSession.UserID,
	}
	if !activeSession.CreatedAt.IsZero() {
		auditData["lifetime_seconds"] = int(closedAtUTC.Sub(activeSession.CreatedAt).Round(time.Second) / time.Second)
	}
	for key, value := range extraAuditData {
		auditData[key] = value
	}

	if err := server.logEvent(auditEventType, controlSessionID, auditData); err != nil {
		server.mu.Lock()
		server.sessions[controlSessionID] = activeSession
		server.approvalTokenIndex[approvalTokenHashValue] = controlSessionID
		if hadSessionReadCounts {
			server.sessionReadCounts[controlSessionID] = sessionReadCounts
		}
		for tokenString, sessionTokenClaims := range sessionTokens {
			server.tokens[tokenString] = sessionTokenClaims
		}
		server.mu.Unlock()
		return err
	}

	return nil
}
