package loopgate

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"morph/internal/sandbox"
)

func (server *Server) terminateMorphling(tokenClaims capabilityToken, terminateRequest MorphlingTerminateRequest) (MorphlingTerminateResponse, error) {
	if err := server.expirePendingMorphlingApprovals(); err != nil {
		return MorphlingTerminateResponse{}, err
	}
	if err := server.expirePendingMorphlingReviews(); err != nil {
		return MorphlingTerminateResponse{}, err
	}

	nowUTC := server.now().UTC()
	server.morphlingsMu.Lock()
	record, found := server.morphlings[strings.TrimSpace(terminateRequest.MorphlingID)]
	if !found {
		server.morphlingsMu.Unlock()
		return MorphlingTerminateResponse{}, errMorphlingNotFound
	}
	previousRecord := cloneMorphlingRecord(record)
	if record.ParentControlSessionID != tokenClaims.ControlSessionID {
		server.morphlingsMu.Unlock()
		return MorphlingTerminateResponse{}, errMorphlingNotFound
	}
	if morphlingTenantDenied(record, tokenClaims) {
		server.morphlingsMu.Unlock()
		return MorphlingTerminateResponse{}, errMorphlingNotFound
	}
	if record.State == morphlingStateTerminating || record.State == morphlingStateTerminated {
		server.morphlingsMu.Unlock()
		return MorphlingTerminateResponse{}, errMorphlingStateInvalid
	}
	terminatingRecord, err := server.transitionMorphlingLocked(record.MorphlingID, morphlingEventBeginTermination, nowUTC, func(updatedRecord *morphlingRecord) error {
		updatedRecord.Outcome = morphlingOutcomeCancelled
		updatedRecord.TerminationReason = strings.TrimSpace(terminateRequest.Reason)
		if updatedRecord.TerminationReason == "" {
			updatedRecord.TerminationReason = morphlingReasonOperatorCancelled
		}
		updatedRecord.ReviewDeadlineUTC = ""
		return nil
	})
	server.morphlingsMu.Unlock()
	if err != nil {
		return MorphlingTerminateResponse{}, err
	}

	if err := server.logEvent("morphling.terminating", tokenClaims.ControlSessionID, map[string]interface{}{
		"morphling_id":       terminatingRecord.MorphlingID,
		"outcome":            terminatingRecord.Outcome,
		"termination_reason": terminatingRecord.TerminationReason,
		"control_session_id": tokenClaims.ControlSessionID,
	}); err != nil {
		if rollbackErr := server.rollbackMorphlingRecordAfterAuditFailure(terminatingRecord.MorphlingID, morphlingStateTerminating, previousRecord); rollbackErr != nil {
			return MorphlingTerminateResponse{}, fmt.Errorf("%w: %v (rollback failed: %v)", errMorphlingAuditUnavailable, err, rollbackErr)
		}
		return MorphlingTerminateResponse{}, fmt.Errorf("%w: %v", errMorphlingAuditUnavailable, err)
	}

	terminatedRecord, err := server.completeMorphlingTermination(tokenClaims.ControlSessionID, terminatingRecord.MorphlingID)
	if err != nil {
		return MorphlingTerminateResponse{}, err
	}
	return MorphlingTerminateResponse{
		Status:    ResponseStatusSuccess,
		Morphling: morphlingSummaryFromRecord(terminatedRecord),
	}, nil
}

func (server *Server) completeMorphlingTermination(controlSessionID string, morphlingID string) (morphlingRecord, error) {
	nowUTC := server.now().UTC()
	server.morphlingsMu.Lock()
	previousRecord, found := server.morphlings[morphlingID]
	if !found {
		server.morphlingsMu.Unlock()
		return morphlingRecord{}, errMorphlingNotFound
	}
	terminatedRecord, err := server.transitionMorphlingLocked(morphlingID, morphlingEventFinishTermination, nowUTC, func(updatedRecord *morphlingRecord) error {
		updatedRecord.TerminatedAtUTC = nowUTC.Format(time.RFC3339Nano)
		return nil
	})
	server.morphlingsMu.Unlock()
	if err != nil {
		return morphlingRecord{}, err
	}
	server.mu.Lock()
	server.revokeMorphlingWorkerAccessLocked(terminatedRecord.MorphlingID)
	server.mu.Unlock()
	auditPayload := map[string]interface{}{
		"morphling_id":            terminatedRecord.MorphlingID,
		"outcome":                 terminatedRecord.Outcome,
		"termination_reason":      terminatedRecord.TerminationReason,
		"preserved_artifact_refs": append([]string(nil), terminatedRecord.StagedArtifactRefs...),
		"control_session_id":      controlSessionID,
	}
	if strings.TrimSpace(terminatedRecord.WorkingDirRelativePath) != "" {
		auditPayload["virtual_evidence_path"] = sandbox.VirtualizeRelativeHomePath(terminatedRecord.WorkingDirRelativePath)
	}
	if err := server.logEvent("morphling.terminated", controlSessionID, auditPayload); err != nil {
		if rollbackErr := server.rollbackMorphlingRecordAfterAuditFailure(morphlingID, morphlingStateTerminated, previousRecord); rollbackErr != nil {
			return morphlingRecord{}, fmt.Errorf("%w: %v (rollback failed: %v)", errMorphlingAuditUnavailable, err, rollbackErr)
		}
		return morphlingRecord{}, fmt.Errorf("%w: %v", errMorphlingAuditUnavailable, err)
	}
	return terminatedRecord, nil
}

func (server *Server) expirePendingMorphlingApprovals() error {
	nowUTC := server.now().UTC()
	server.morphlingsMu.Lock()
	pendingMorphlingIDs := make([]string, 0)
	for morphlingID, record := range server.morphlings {
		if record.State != morphlingStatePendingSpawnApproval {
			continue
		}
		if strings.TrimSpace(record.ApprovalDeadlineUTC) == "" {
			continue
		}
		approvalDeadlineUTC, err := time.Parse(time.RFC3339Nano, record.ApprovalDeadlineUTC)
		if err != nil {
			server.morphlingsMu.Unlock()
			return err
		}
		if !nowUTC.Before(approvalDeadlineUTC) {
			pendingMorphlingIDs = append(pendingMorphlingIDs, morphlingID)
		}
	}
	server.morphlingsMu.Unlock()

	for _, morphlingID := range pendingMorphlingIDs {
		if err := server.expirePendingMorphlingApproval(morphlingID, nowUTC); err != nil {
			return err
		}
	}
	return nil
}

func (server *Server) expirePendingMorphlingApproval(morphlingID string, nowUTC time.Time) error {
	server.morphlingsMu.Lock()
	record, found := server.morphlings[morphlingID]
	if !found {
		server.morphlingsMu.Unlock()
		return nil
	}
	if record.State != morphlingStatePendingSpawnApproval {
		server.morphlingsMu.Unlock()
		return nil
	}
	terminatingRecord, err := server.transitionMorphlingLocked(morphlingID, morphlingEventBeginTermination, nowUTC, func(updatedRecord *morphlingRecord) error {
		updatedRecord.Outcome = morphlingOutcomeCancelled
		updatedRecord.TerminationReason = morphlingReasonSpawnApprovalExpired
		updatedRecord.ReviewDeadlineUTC = ""
		return nil
	})
	server.morphlingsMu.Unlock()
	if err != nil {
		return err
	}

	if err := server.logEvent("morphling.spawn_approval_expired", record.ParentControlSessionID, map[string]interface{}{
		"morphling_id":   terminatingRecord.MorphlingID,
		"approval_id":    terminatingRecord.ApprovalID,
		"expired_at_utc": nowUTC.Format(time.RFC3339Nano),
	}); err != nil {
		return fmt.Errorf("%w: %v", errMorphlingAuditUnavailable, err)
	}
	if err := server.logEvent("morphling.terminating", record.ParentControlSessionID, map[string]interface{}{
		"morphling_id":       terminatingRecord.MorphlingID,
		"outcome":            terminatingRecord.Outcome,
		"termination_reason": terminatingRecord.TerminationReason,
		"control_session_id": record.ParentControlSessionID,
	}); err != nil {
		return fmt.Errorf("%w: %v", errMorphlingAuditUnavailable, err)
	}
	_, err = server.completeMorphlingTermination(record.ParentControlSessionID, morphlingID)
	if err != nil {
		return err
	}
	server.mu.Lock()
	if approvalRecord, found := server.approvals[record.ApprovalID]; found && approvalRecord.State == approvalStatePending {
		approvalRecord.State = approvalStateExpired
		server.approvals[record.ApprovalID] = approvalRecord
	}
	server.mu.Unlock()
	return nil
}

func (server *Server) failMorphlingAfterAdmission(controlSessionID string, morphlingID string, outcome string, terminationReason string) error {
	nowUTC := server.now().UTC()
	server.morphlingsMu.Lock()
	record, found := server.morphlings[morphlingID]
	if !found {
		server.morphlingsMu.Unlock()
		return nil
	}
	if record.State == morphlingStateTerminating || record.State == morphlingStateTerminated {
		server.morphlingsMu.Unlock()
		return nil
	}
	terminatingRecord, err := server.transitionMorphlingLocked(morphlingID, morphlingEventBeginTermination, nowUTC, func(updatedRecord *morphlingRecord) error {
		updatedRecord.Outcome = outcome
		updatedRecord.TerminationReason = terminationReason
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
		return err
	}
	_, err = server.completeMorphlingTermination(controlSessionID, morphlingID)
	return err
}

func (server *Server) recoverMorphlings() error {
	morphlingIDs := make([]string, 0, len(server.morphlings))
	for morphlingID := range server.morphlings {
		morphlingIDs = append(morphlingIDs, morphlingID)
	}
	sort.Strings(morphlingIDs)

	for _, morphlingID := range morphlingIDs {
		record := server.morphlings[morphlingID]
		switch record.State {
		case morphlingStateRunning,
			morphlingStateCompleting,
			morphlingStatePendingReview,
			morphlingStateRequested,
			morphlingStateAuthorizing,
			morphlingStateSpawned,
			morphlingStatePendingSpawnApproval:
			nowUTC := server.now().UTC()
			server.morphlingsMu.Lock()
			terminatingRecord, err := server.transitionMorphlingLocked(morphlingID, morphlingEventBeginTermination, nowUTC, func(updatedRecord *morphlingRecord) error {
				updatedRecord.Outcome = morphlingOutcomeFailed
				if record.State == morphlingStatePendingSpawnApproval {
					updatedRecord.Outcome = morphlingOutcomeCancelled
				}
				updatedRecord.TerminationReason = morphlingReasonLoopgateRestart
				updatedRecord.ReviewDeadlineUTC = ""
				return nil
			})
			server.morphlingsMu.Unlock()
			if err != nil {
				return err
			}
			if err := server.logEvent("morphling.terminating", record.ParentControlSessionID, map[string]interface{}{
				"morphling_id":       terminatingRecord.MorphlingID,
				"outcome":            terminatingRecord.Outcome,
				"termination_reason": terminatingRecord.TerminationReason,
				"control_session_id": record.ParentControlSessionID,
			}); err != nil {
				return err
			}
			if _, err := server.completeMorphlingTermination(record.ParentControlSessionID, morphlingID); err != nil {
				return err
			}
		case morphlingStateTerminating:
			if _, err := server.completeMorphlingTermination(record.ParentControlSessionID, morphlingID); err != nil {
				return err
			}
		case morphlingStateTerminated:
		default:
			return fmt.Errorf("%w: unsupported morphling state %q during restart recovery", errMorphlingStateInvalid, record.State)
		}
	}
	return nil
}
