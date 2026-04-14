package loopgate

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"morph/internal/sandbox"
)

const morphlingWorkerLaunchTTL = 2 * time.Minute

type morphlingWorkerLaunch struct {
	LaunchToken            string
	MorphlingID            string
	ParentControlSessionID string
	ExpiresAt              time.Time
	CreatedAt              time.Time
}

type morphlingWorkerSession struct {
	WorkerToken            string
	ControlSessionID       string
	MorphlingID            string
	ParentControlSessionID string
	PeerIdentity           peerIdentity
	SessionMACKey          string
	ExpiresAt              time.Time
	CreatedAt              time.Time
}

func normalizeMorphlingMemoryStrings(memoryStrings []string) []string {
	normalizedMemoryStrings := make([]string, 0, len(memoryStrings))
	seenMemoryStrings := make(map[string]struct{}, len(memoryStrings))
	for _, rawMemoryString := range memoryStrings {
		memoryString := strings.TrimSpace(rawMemoryString)
		if memoryString == "" {
			continue
		}
		if _, exists := seenMemoryStrings[memoryString]; exists {
			continue
		}
		seenMemoryStrings[memoryString] = struct{}{}
		normalizedMemoryStrings = append(normalizedMemoryStrings, memoryString)
	}
	sort.Strings(normalizedMemoryStrings)
	return normalizedMemoryStrings
}

func normalizedMorphlingStatusText(statusText string) string {
	return strings.TrimSpace(statusText)
}

func morphlingProjectionAuditFields(statusText string, memoryStrings []string) map[string]interface{} {
	fields := map[string]interface{}{
		"memory_string_count": len(memoryStrings),
	}
	if strings.TrimSpace(statusText) != "" {
		fields["status_text_hash"] = hashBytes([]byte(strings.TrimSpace(statusText)))
	}
	if len(memoryStrings) > 0 {
		fields["memory_strings_hash"] = hashBytes([]byte(strings.Join(memoryStrings, "\n")))
	}
	return fields
}

func morphlingRelativePathWithinRoot(rootRelativePath string, candidateRelativePath string) bool {
	normalizedRoot := strings.TrimSpace(rootRelativePath)
	normalizedCandidate := strings.TrimSpace(candidateRelativePath)
	if normalizedRoot == "" || normalizedCandidate == "" {
		return false
	}
	return normalizedCandidate == normalizedRoot || strings.HasPrefix(normalizedCandidate, normalizedRoot+"/")
}

func morphlingArtifactOutputName(taskID string, artifactIndex int, artifactRelativePath string) string {
	artifactBaseName := path.Base(strings.TrimSpace(artifactRelativePath))
	artifactPathHash := hashBytes([]byte(strings.TrimSpace(artifactRelativePath)))
	if len(artifactPathHash) > 8 {
		artifactPathHash = artifactPathHash[:8]
	}
	return fmt.Sprintf("%s-%02d-%s-%s", taskID, artifactIndex+1, artifactPathHash, artifactBaseName)
}

func (server *Server) createMorphlingWorkerLaunch(tokenClaims capabilityToken, launchRequest MorphlingWorkerLaunchRequest) (MorphlingWorkerLaunchResponse, error) {
	if err := server.expirePendingMorphlingApprovals(); err != nil {
		return MorphlingWorkerLaunchResponse{}, err
	}
	if err := server.expirePendingMorphlingReviews(); err != nil {
		return MorphlingWorkerLaunchResponse{}, err
	}

	server.morphlingsMu.Lock()
	record, found := server.morphlings[strings.TrimSpace(launchRequest.MorphlingID)]
	server.morphlingsMu.Unlock()
	if !found {
		return MorphlingWorkerLaunchResponse{}, errMorphlingNotFound
	}
	if record.ParentControlSessionID != tokenClaims.ControlSessionID {
		return MorphlingWorkerLaunchResponse{}, errMorphlingNotFound
	}
	if morphlingTenantDenied(record, tokenClaims) {
		return MorphlingWorkerLaunchResponse{}, errMorphlingNotFound
	}
	if record.State != morphlingStateSpawned && record.State != morphlingStateRunning {
		return MorphlingWorkerLaunchResponse{}, errMorphlingStateInvalid
	}
	expiresAtUTC, err := time.Parse(time.RFC3339Nano, record.TokenExpiryUTC)
	if err != nil {
		return MorphlingWorkerLaunchResponse{}, fmt.Errorf("%w: invalid morphling token expiry", errMorphlingStateInvalid)
	}
	launchExpiresAtUTC := server.now().UTC().Add(morphlingWorkerLaunchTTL)
	if expiresAtUTC.Before(launchExpiresAtUTC) {
		launchExpiresAtUTC = expiresAtUTC
	}
	if !launchExpiresAtUTC.After(server.now().UTC()) {
		return MorphlingWorkerLaunchResponse{}, fmt.Errorf("%w: morphling token already expired", errMorphlingStateInvalid)
	}

	launchToken, err := randomHex(24)
	if err != nil {
		return MorphlingWorkerLaunchResponse{}, fmt.Errorf("generate morphling worker launch token: %w", err)
	}

	server.mu.Lock()
	server.pruneExpiredLocked()
	server.revokeMorphlingWorkerAccessLocked(record.MorphlingID)
	server.morphlingWorkerLaunches[launchToken] = morphlingWorkerLaunch{
		LaunchToken:            launchToken,
		MorphlingID:            record.MorphlingID,
		ParentControlSessionID: record.ParentControlSessionID,
		ExpiresAt:              launchExpiresAtUTC,
		CreatedAt:              server.now().UTC(),
	}
	server.noteExpiryCandidateLocked(launchExpiresAtUTC)
	server.mu.Unlock()

	if err := server.logEvent("morphling.worker_launch_created", record.ParentControlSessionID, map[string]interface{}{
		"morphling_id":       record.MorphlingID,
		"task_id":            record.TaskID,
		"expires_at_utc":     launchExpiresAtUTC.Format(time.RFC3339Nano),
		"control_session_id": record.ParentControlSessionID,
	}); err != nil {
		server.mu.Lock()
		delete(server.morphlingWorkerLaunches, launchToken)
		server.mu.Unlock()
		return MorphlingWorkerLaunchResponse{}, fmt.Errorf("%w: %v", errMorphlingAuditUnavailable, err)
	}

	return MorphlingWorkerLaunchResponse{
		MorphlingID:  record.MorphlingID,
		LaunchToken:  launchToken,
		ExpiresAtUTC: launchExpiresAtUTC.Format(time.RFC3339Nano),
	}, nil
}

func (server *Server) openMorphlingWorkerSession(requestPeerIdentity peerIdentity, openRequest MorphlingWorkerOpenRequest) (MorphlingWorkerSessionResponse, error) {
	// Hold server.mu continuously from launch lookup through session issuance so two
	// concurrent openers cannot both observe the same single-use launch token.
	server.mu.Lock()
	server.pruneExpiredLocked()
	launch, found := server.morphlingWorkerLaunches[strings.TrimSpace(openRequest.LaunchToken)]
	if !found {
		server.mu.Unlock()
		return MorphlingWorkerSessionResponse{}, errMorphlingWorkerLaunchInvalid
	}
	if !launch.ExpiresAt.After(server.now().UTC()) {
		server.mu.Unlock()
		return MorphlingWorkerSessionResponse{}, errMorphlingWorkerLaunchInvalid
	}

	server.morphlingsMu.Lock()
	record, morphlingFound := server.morphlings[launch.MorphlingID]
	server.morphlingsMu.Unlock()
	if !morphlingFound {
		server.mu.Unlock()
		return MorphlingWorkerSessionResponse{}, errMorphlingNotFound
	}
	if record.ParentControlSessionID != launch.ParentControlSessionID {
		server.mu.Unlock()
		return MorphlingWorkerSessionResponse{}, errMorphlingWorkerLaunchInvalid
	}
	parentSession, parentFound := server.sessions[record.ParentControlSessionID]
	if !parentFound || morphlingParentTenantInconsistent(record.TenantID, parentSession) {
		server.mu.Unlock()
		return MorphlingWorkerSessionResponse{}, errMorphlingWorkerLaunchInvalid
	}
	if record.State != morphlingStateSpawned && record.State != morphlingStateRunning {
		server.mu.Unlock()
		return MorphlingWorkerSessionResponse{}, errMorphlingStateInvalid
	}
	expiresAtUTC, err := time.Parse(time.RFC3339Nano, record.TokenExpiryUTC)
	if err != nil {
		server.mu.Unlock()
		return MorphlingWorkerSessionResponse{}, fmt.Errorf("%w: invalid morphling token expiry", errMorphlingStateInvalid)
	}
	if !expiresAtUTC.After(server.now().UTC()) {
		server.mu.Unlock()
		return MorphlingWorkerSessionResponse{}, errMorphlingStateInvalid
	}

	controlSessionSuffix, err := randomHex(8)
	if err != nil {
		server.mu.Unlock()
		return MorphlingWorkerSessionResponse{}, fmt.Errorf("generate morphling worker control session id: %w", err)
	}
	workerToken, err := randomHex(24)
	if err != nil {
		server.mu.Unlock()
		return MorphlingWorkerSessionResponse{}, fmt.Errorf("generate morphling worker token: %w", err)
	}
	workerControlSessionID := "worker-" + controlSessionSuffix

	if server.maxMorphlingWorkerSessions > 0 && len(server.morphlingWorkerSessions) >= server.maxMorphlingWorkerSessions {
		server.mu.Unlock()
		return MorphlingWorkerSessionResponse{}, errMorphlingWorkerSessionsSaturated
	}

	delete(server.morphlingWorkerLaunches, strings.TrimSpace(openRequest.LaunchToken))
	server.revokeMorphlingWorkerAccessLocked(record.MorphlingID)
	sessionMACKey := server.sessionMACKeyForControlSessionAtEpoch(workerControlSessionID, server.currentSessionMACEpochIndex())
	server.morphlingWorkerSessions[workerToken] = morphlingWorkerSession{
		WorkerToken:            workerToken,
		ControlSessionID:       workerControlSessionID,
		MorphlingID:            record.MorphlingID,
		ParentControlSessionID: record.ParentControlSessionID,
		PeerIdentity:           requestPeerIdentity,
		SessionMACKey:          sessionMACKey,
		ExpiresAt:              expiresAtUTC,
		CreatedAt:              server.now().UTC(),
	}
	server.noteExpiryCandidateLocked(expiresAtUTC)
	server.mu.Unlock()

	if err := server.logEvent("morphling.worker_session_opened", record.ParentControlSessionID, map[string]interface{}{
		"morphling_id":              record.MorphlingID,
		"worker_control_session_id": workerControlSessionID,
		"expires_at_utc":            expiresAtUTC.Format(time.RFC3339Nano),
		"peer_uid":                  requestPeerIdentity.UID,
		"peer_pid":                  requestPeerIdentity.PID,
		"peer_epid":                 requestPeerIdentity.EPID,
		"control_session_id":        record.ParentControlSessionID,
	}); err != nil {
		server.mu.Lock()
		delete(server.morphlingWorkerSessions, workerToken)
		server.mu.Unlock()
		return MorphlingWorkerSessionResponse{}, fmt.Errorf("%w: %v", errMorphlingAuditUnavailable, err)
	}

	return MorphlingWorkerSessionResponse{
		MorphlingID:      record.MorphlingID,
		ControlSessionID: workerControlSessionID,
		WorkerToken:      workerToken,
		SessionMACKey:    sessionMACKey,
		ExpiresAtUTC:     expiresAtUTC.Format(time.RFC3339Nano),
	}, nil
}

func (server *Server) startMorphlingWorker(workerSession morphlingWorkerSession, startRequest MorphlingWorkerStartRequest) (MorphlingWorkerActionResponse, error) {
	nowUTC := server.now().UTC()
	normalizedStatusText := normalizedMorphlingStatusText(startRequest.StatusText)
	normalizedMemoryStrings := normalizeMorphlingMemoryStrings(startRequest.MemoryStrings)

	server.morphlingsMu.Lock()
	previousRecord, found := server.morphlings[workerSession.MorphlingID]
	if !found {
		server.morphlingsMu.Unlock()
		return MorphlingWorkerActionResponse{}, errMorphlingNotFound
	}
	updatedRecord, err := server.transitionMorphlingLocked(workerSession.MorphlingID, morphlingEventExecutionStarted, nowUTC, func(updatedRecord *morphlingRecord) error {
		updatedRecord.StatusText = normalizedStatusText
		updatedRecord.MemoryStrings = normalizedMemoryStrings
		return nil
	})
	server.morphlingsMu.Unlock()
	if err != nil {
		return MorphlingWorkerActionResponse{}, err
	}

	auditFields := morphlingProjectionAuditFields(normalizedStatusText, normalizedMemoryStrings)
	auditFields["morphling_id"] = updatedRecord.MorphlingID
	auditFields["task_id"] = updatedRecord.TaskID
	if err := server.logEvent("morphling.execution_started", workerSession.ParentControlSessionID, auditFields); err != nil {
		if rollbackErr := server.rollbackMorphlingRecordAfterAuditFailure(workerSession.MorphlingID, morphlingStateRunning, previousRecord); rollbackErr != nil {
			return MorphlingWorkerActionResponse{}, fmt.Errorf("%w: %v (rollback failed: %v)", errMorphlingAuditUnavailable, err, rollbackErr)
		}
		return MorphlingWorkerActionResponse{}, fmt.Errorf("%w: %v", errMorphlingAuditUnavailable, err)
	}

	return MorphlingWorkerActionResponse{
		Status:    ResponseStatusSuccess,
		Morphling: morphlingSummaryFromRecord(updatedRecord),
	}, nil
}

func (server *Server) updateMorphlingWorker(workerSession morphlingWorkerSession, updateRequest MorphlingWorkerUpdateRequest) (MorphlingWorkerActionResponse, error) {
	nowUTC := server.now().UTC()
	normalizedStatusText := normalizedMorphlingStatusText(updateRequest.StatusText)
	normalizedMemoryStrings := normalizeMorphlingMemoryStrings(updateRequest.MemoryStrings)

	server.morphlingsMu.Lock()
	record, found := server.morphlings[workerSession.MorphlingID]
	if !found {
		server.morphlingsMu.Unlock()
		return MorphlingWorkerActionResponse{}, errMorphlingNotFound
	}
	previousRecord := cloneMorphlingRecord(record)
	if record.State != morphlingStateRunning {
		server.morphlingsMu.Unlock()
		return MorphlingWorkerActionResponse{}, errMorphlingStateInvalid
	}
	updatedRecord, err := server.updateMorphlingRecordLocked(workerSession.MorphlingID, nowUTC, func(updatedRecord *morphlingRecord) error {
		updatedRecord.StatusText = normalizedStatusText
		updatedRecord.MemoryStrings = normalizedMemoryStrings
		return nil
	})
	server.morphlingsMu.Unlock()
	if err != nil {
		return MorphlingWorkerActionResponse{}, err
	}

	auditFields := morphlingProjectionAuditFields(normalizedStatusText, normalizedMemoryStrings)
	auditFields["morphling_id"] = updatedRecord.MorphlingID
	if err := server.logEvent("morphling.progress_updated", workerSession.ParentControlSessionID, auditFields); err != nil {
		if rollbackErr := server.rollbackMorphlingRecordAfterAuditFailure(workerSession.MorphlingID, morphlingStateRunning, previousRecord); rollbackErr != nil {
			return MorphlingWorkerActionResponse{}, fmt.Errorf("%w: %v (rollback failed: %v)", errMorphlingAuditUnavailable, err, rollbackErr)
		}
		return MorphlingWorkerActionResponse{}, fmt.Errorf("%w: %v", errMorphlingAuditUnavailable, err)
	}

	return MorphlingWorkerActionResponse{
		Status:    ResponseStatusSuccess,
		Morphling: morphlingSummaryFromRecord(updatedRecord),
	}, nil
}

func (server *Server) completeMorphlingWorker(workerSession morphlingWorkerSession, completeRequest MorphlingWorkerCompleteRequest) (MorphlingWorkerActionResponse, error) {
	nowUTC := server.now().UTC()
	normalizedStatusText := normalizedMorphlingStatusText(completeRequest.StatusText)
	normalizedMemoryStrings := normalizeMorphlingMemoryStrings(completeRequest.MemoryStrings)
	exitReason := strings.TrimSpace(completeRequest.ExitReason)
	if exitReason == "" {
		exitReason = "completed"
	}

	server.morphlingsMu.Lock()
	record, found := server.morphlings[workerSession.MorphlingID]
	if !found {
		server.morphlingsMu.Unlock()
		return MorphlingWorkerActionResponse{}, errMorphlingNotFound
	}
	previousRunningRecord := cloneMorphlingRecord(record)
	if record.State != morphlingStateRunning {
		server.morphlingsMu.Unlock()
		return MorphlingWorkerActionResponse{}, errMorphlingStateInvalid
	}
	completingRecord, err := server.transitionMorphlingLocked(workerSession.MorphlingID, morphlingEventExecutionCompleted, nowUTC, func(updatedRecord *morphlingRecord) error {
		updatedRecord.StatusText = normalizedStatusText
		updatedRecord.MemoryStrings = normalizedMemoryStrings
		return nil
	})
	server.morphlingsMu.Unlock()
	if err != nil {
		return MorphlingWorkerActionResponse{}, err
	}

	executionCompletedAuditFields := morphlingProjectionAuditFields(normalizedStatusText, normalizedMemoryStrings)
	executionCompletedAuditFields["morphling_id"] = completingRecord.MorphlingID
	executionCompletedAuditFields["exit_reason"] = exitReason
	if err := server.logEvent("morphling.execution_completed", workerSession.ParentControlSessionID, executionCompletedAuditFields); err != nil {
		if rollbackErr := server.rollbackMorphlingRecordAfterAuditFailure(workerSession.MorphlingID, morphlingStateCompleting, previousRunningRecord); rollbackErr != nil {
			return MorphlingWorkerActionResponse{}, fmt.Errorf("%w: %v (rollback failed: %v)", errMorphlingAuditUnavailable, err, rollbackErr)
		}
		return MorphlingWorkerActionResponse{}, fmt.Errorf("%w: %v", errMorphlingAuditUnavailable, err)
	}

	stagedArtifactRefs, artifactManifestHash, artifactStageErr := server.stageMorphlingCompletionArtifacts(workerSession, completingRecord, completeRequest.ArtifactPaths)
	if artifactStageErr != nil {
		_ = server.failMorphlingAfterAdmission(workerSession.ParentControlSessionID, workerSession.MorphlingID, morphlingOutcomeFailed, morphlingReasonStagingFailed)
		return MorphlingWorkerActionResponse{}, artifactStageErr
	}

	if completingRecord.RequiresReview {
		policyRuntime := server.currentPolicyRuntime()
		classPolicy, found := policyRuntime.morphlingClassPolicy.Class(completingRecord.Class)
		if !found {
			_ = server.failMorphlingAfterAdmission(workerSession.ParentControlSessionID, workerSession.MorphlingID, morphlingOutcomeFailed, morphlingReasonStagingFailed)
			return MorphlingWorkerActionResponse{}, errMorphlingClassInvalid
		}
		reviewDeadlineUTC := server.now().UTC().Add(time.Duration(classPolicy.ReviewTTLSeconds) * time.Second)

		server.morphlingsMu.Lock()
		pendingReviewRecord, err := server.transitionMorphlingLocked(workerSession.MorphlingID, morphlingEventAwaitReview, server.now().UTC(), func(updatedRecord *morphlingRecord) error {
			updatedRecord.ArtifactCount = len(stagedArtifactRefs)
			updatedRecord.StagedArtifactRefs = append([]string(nil), stagedArtifactRefs...)
			updatedRecord.ArtifactManifestHash = artifactManifestHash
			updatedRecord.ReviewDeadlineUTC = reviewDeadlineUTC.Format(time.RFC3339Nano)
			if strings.TrimSpace(updatedRecord.StatusText) == "" {
				updatedRecord.StatusText = "pending review"
			}
			return nil
		})
		server.morphlingsMu.Unlock()
		if err != nil {
			return MorphlingWorkerActionResponse{}, err
		}
		if err := server.logEvent("morphling.artifacts_staged", workerSession.ParentControlSessionID, map[string]interface{}{
			"morphling_id":           pendingReviewRecord.MorphlingID,
			"artifact_count":         pendingReviewRecord.ArtifactCount,
			"artifact_manifest_hash": pendingReviewRecord.ArtifactManifestHash,
			"review_deadline_utc":    pendingReviewRecord.ReviewDeadlineUTC,
		}); err != nil {
			if rollbackErr := server.rollbackMorphlingRecordAfterAuditFailure(workerSession.MorphlingID, morphlingStatePendingReview, completingRecord); rollbackErr != nil {
				return MorphlingWorkerActionResponse{}, fmt.Errorf("%w: %v (rollback failed: %v)", errMorphlingAuditUnavailable, err, rollbackErr)
			}
			return MorphlingWorkerActionResponse{}, fmt.Errorf("%w: %v", errMorphlingAuditUnavailable, err)
		}
		return MorphlingWorkerActionResponse{
			Status:    ResponseStatusSuccess,
			Morphling: morphlingSummaryFromRecord(pendingReviewRecord),
		}, nil
	}

	server.morphlingsMu.Lock()
	terminatingRecord, err := server.transitionMorphlingLocked(workerSession.MorphlingID, morphlingEventBeginTermination, server.now().UTC(), func(updatedRecord *morphlingRecord) error {
		updatedRecord.ArtifactCount = len(stagedArtifactRefs)
		updatedRecord.StagedArtifactRefs = append([]string(nil), stagedArtifactRefs...)
		updatedRecord.ArtifactManifestHash = artifactManifestHash
		updatedRecord.Outcome = morphlingOutcomeApproved
		updatedRecord.TerminationReason = morphlingReasonNormalCompletion
		updatedRecord.ReviewDeadlineUTC = ""
		return nil
	})
	server.morphlingsMu.Unlock()
	if err != nil {
		return MorphlingWorkerActionResponse{}, err
	}
	if err := server.logEvent("morphling.terminating", workerSession.ParentControlSessionID, map[string]interface{}{
		"morphling_id":       terminatingRecord.MorphlingID,
		"outcome":            terminatingRecord.Outcome,
		"termination_reason": terminatingRecord.TerminationReason,
		"control_session_id": workerSession.ParentControlSessionID,
	}); err != nil {
		if rollbackErr := server.rollbackMorphlingRecordAfterAuditFailure(workerSession.MorphlingID, morphlingStateTerminating, completingRecord); rollbackErr != nil {
			return MorphlingWorkerActionResponse{}, fmt.Errorf("%w: %v (rollback failed: %v)", errMorphlingAuditUnavailable, err, rollbackErr)
		}
		return MorphlingWorkerActionResponse{}, fmt.Errorf("%w: %v", errMorphlingAuditUnavailable, err)
	}
	terminatedRecord, err := server.completeMorphlingTermination(workerSession.ParentControlSessionID, workerSession.MorphlingID)
	if err != nil {
		return MorphlingWorkerActionResponse{}, err
	}
	return MorphlingWorkerActionResponse{
		Status:    ResponseStatusSuccess,
		Morphling: morphlingSummaryFromRecord(terminatedRecord),
	}, nil
}

func (server *Server) stageMorphlingCompletionArtifacts(workerSession morphlingWorkerSession, completingRecord morphlingRecord, artifactPaths []string) ([]string, string, error) {
	normalizedArtifactPaths := make([]string, 0, len(artifactPaths))
	seenArtifactPaths := make(map[string]struct{}, len(artifactPaths))
	for _, rawArtifactPath := range artifactPaths {
		normalizedArtifactPath, err := sandbox.NormalizeHomePath(rawArtifactPath)
		if err != nil {
			return nil, "", fmt.Errorf("%w: %v", errMorphlingArtifactInvalid, err)
		}
		if _, exists := seenArtifactPaths[normalizedArtifactPath]; exists {
			continue
		}
		seenArtifactPaths[normalizedArtifactPath] = struct{}{}
		normalizedArtifactPaths = append(normalizedArtifactPaths, normalizedArtifactPath)
	}
	sort.Strings(normalizedArtifactPaths)

	server.mu.Lock()
	parentSession, parentOk := server.sessions[workerSession.ParentControlSessionID]
	server.mu.Unlock()
	stageTenantID, stageUserID := "", ""
	if parentOk {
		stageTenantID = parentSession.TenantID
		stageUserID = parentSession.UserID
	}
	stageTokenClaims := capabilityToken{
		ControlSessionID:   workerSession.ParentControlSessionID,
		ActorLabel:         "morphling_worker",
		ClientSessionLabel: workerSession.ControlSessionID,
		PeerIdentity:       workerSession.PeerIdentity,
		TenantID:           stageTenantID,
		UserID:             stageUserID,
	}

	manifestArtifacts := make([]map[string]interface{}, 0, len(normalizedArtifactPaths))
	stagedArtifactRefs := make([]string, 0, len(normalizedArtifactPaths))
	for artifactIndex, normalizedArtifactPath := range normalizedArtifactPaths {
		_, artifactRelativePath, err := server.sandboxPaths.ResolveHomePath(normalizedArtifactPath)
		if err != nil {
			return nil, "", fmt.Errorf("%w: %v", errMorphlingArtifactInvalid, err)
		}
		if !morphlingRelativePathWithinRoot(completingRecord.WorkingDirRelativePath, artifactRelativePath) {
			return nil, "", fmt.Errorf("%w: artifact path %s must stay inside the morphling working directory", errMorphlingArtifactInvalid, normalizedArtifactPath)
		}
		stageResponse, err := server.stageSandboxArtifact(stageTokenClaims, SandboxStageRequest{
			SandboxSourcePath: normalizedArtifactPath,
			OutputName:        morphlingArtifactOutputName(completingRecord.TaskID, artifactIndex, artifactRelativePath),
		})
		if err != nil {
			return nil, "", err
		}
		stagedArtifactRefs = append(stagedArtifactRefs, stageResponse.ArtifactRef)
		manifestArtifacts = append(manifestArtifacts, map[string]interface{}{
			"artifact_ref":        stageResponse.ArtifactRef,
			"content_sha256":      stageResponse.ContentSHA256,
			"size_bytes":          stageResponse.SizeBytes,
			"sandbox_output_path": stageResponse.SandboxAbsolutePath,
			"source_sandbox_path": stageResponse.SourceSandboxPath,
		})
	}
	sort.Slice(manifestArtifacts, func(leftIndex int, rightIndex int) bool {
		return manifestArtifacts[leftIndex]["artifact_ref"].(string) < manifestArtifacts[rightIndex]["artifact_ref"].(string)
	})

	artifactManifestHash, err := hashMorphlingArtifactManifest(map[string]interface{}{
		"morphling_id":   completingRecord.MorphlingID,
		"task_id":        completingRecord.TaskID,
		"artifact_count": len(manifestArtifacts),
		"artifacts":      manifestArtifacts,
	})
	if err != nil {
		return nil, "", err
	}
	return stagedArtifactRefs, artifactManifestHash, nil
}

func (server *Server) reviewMorphling(tokenClaims capabilityToken, reviewRequest MorphlingReviewRequest) (MorphlingReviewResponse, error) {
	if err := server.expirePendingMorphlingApprovals(); err != nil {
		return MorphlingReviewResponse{}, err
	}
	if err := server.expirePendingMorphlingReviews(); err != nil {
		return MorphlingReviewResponse{}, err
	}

	decisionNonce, err := randomHex(16)
	if err != nil {
		return MorphlingReviewResponse{}, fmt.Errorf("generate morphling review decision nonce: %w", err)
	}

	server.morphlingsMu.Lock()
	record, found := server.morphlings[strings.TrimSpace(reviewRequest.MorphlingID)]
	if !found {
		server.morphlingsMu.Unlock()
		return MorphlingReviewResponse{}, errMorphlingNotFound
	}
	if record.ParentControlSessionID != tokenClaims.ControlSessionID {
		server.morphlingsMu.Unlock()
		return MorphlingReviewResponse{}, errMorphlingNotFound
	}
	if morphlingTenantDenied(record, tokenClaims) {
		server.morphlingsMu.Unlock()
		return MorphlingReviewResponse{}, errMorphlingNotFound
	}
	if record.State != morphlingStatePendingReview {
		server.morphlingsMu.Unlock()
		return MorphlingReviewResponse{}, errMorphlingReviewInvalid
	}

	outcome := morphlingOutcomeRejected
	decision := "rejected"
	if reviewRequest.Approved {
		outcome = morphlingOutcomeApproved
		decision = "approved"
	}

	terminatingRecord, err := server.transitionMorphlingLocked(record.MorphlingID, morphlingEventBeginTermination, server.now().UTC(), func(updatedRecord *morphlingRecord) error {
		updatedRecord.Outcome = outcome
		updatedRecord.TerminationReason = morphlingReasonNormalCompletion
		updatedRecord.ReviewDeadlineUTC = ""
		return nil
	})
	server.morphlingsMu.Unlock()
	if err != nil {
		return MorphlingReviewResponse{}, err
	}

	if err := server.logEvent("morphling.review_decision", tokenClaims.ControlSessionID, map[string]interface{}{
		"morphling_id":           terminatingRecord.MorphlingID,
		"decision":               decision,
		"decision_nonce":         decisionNonce,
		"artifact_manifest_hash": terminatingRecord.ArtifactManifestHash,
		"control_session_id":     tokenClaims.ControlSessionID,
	}); err != nil {
		return MorphlingReviewResponse{}, fmt.Errorf("%w: %v", errMorphlingAuditUnavailable, err)
	}
	if err := server.logEvent("morphling.terminating", tokenClaims.ControlSessionID, map[string]interface{}{
		"morphling_id":       terminatingRecord.MorphlingID,
		"outcome":            terminatingRecord.Outcome,
		"termination_reason": terminatingRecord.TerminationReason,
		"control_session_id": tokenClaims.ControlSessionID,
	}); err != nil {
		return MorphlingReviewResponse{}, fmt.Errorf("%w: %v", errMorphlingAuditUnavailable, err)
	}

	terminatedRecord, err := server.completeMorphlingTermination(tokenClaims.ControlSessionID, terminatingRecord.MorphlingID)
	if err != nil {
		return MorphlingReviewResponse{}, err
	}
	return MorphlingReviewResponse{
		Status:        ResponseStatusSuccess,
		DecisionNonce: decisionNonce,
		Morphling:     morphlingSummaryFromRecord(terminatedRecord),
	}, nil
}

func (server *Server) expirePendingMorphlingReviews() error {
	nowUTC := server.now().UTC()
	server.morphlingsMu.Lock()
	expiredReviewMorphlingIDs := make([]string, 0)
	for morphlingID, record := range server.morphlings {
		if record.State != morphlingStatePendingReview {
			continue
		}
		if strings.TrimSpace(record.ReviewDeadlineUTC) == "" {
			continue
		}
		reviewDeadlineUTC, err := time.Parse(time.RFC3339Nano, record.ReviewDeadlineUTC)
		if err != nil {
			server.morphlingsMu.Unlock()
			return err
		}
		if !nowUTC.Before(reviewDeadlineUTC) {
			expiredReviewMorphlingIDs = append(expiredReviewMorphlingIDs, morphlingID)
		}
	}
	server.morphlingsMu.Unlock()

	for _, morphlingID := range expiredReviewMorphlingIDs {
		if err := server.expirePendingMorphlingReview(morphlingID, nowUTC); err != nil {
			return err
		}
	}
	return nil
}

func (server *Server) expirePendingMorphlingReview(morphlingID string, nowUTC time.Time) error {
	server.morphlingsMu.Lock()
	record, found := server.morphlings[morphlingID]
	if !found {
		server.morphlingsMu.Unlock()
		return nil
	}
	if record.State != morphlingStatePendingReview {
		server.morphlingsMu.Unlock()
		return nil
	}
	terminatingRecord, err := server.transitionMorphlingLocked(morphlingID, morphlingEventBeginTermination, nowUTC, func(updatedRecord *morphlingRecord) error {
		updatedRecord.Outcome = morphlingOutcomeCancelled
		updatedRecord.TerminationReason = morphlingReasonReviewTTLExpired
		updatedRecord.ReviewDeadlineUTC = ""
		return nil
	})
	server.morphlingsMu.Unlock()
	if err != nil {
		return err
	}

	if err := server.logEvent("morphling.review_expired", record.ParentControlSessionID, map[string]interface{}{
		"morphling_id":   terminatingRecord.MorphlingID,
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
	return err
}

func (server *Server) revokeMorphlingWorkerAccessLocked(morphlingID string) {
	for launchToken, workerLaunch := range server.morphlingWorkerLaunches {
		if workerLaunch.MorphlingID == morphlingID {
			delete(server.morphlingWorkerLaunches, launchToken)
		}
	}
	for workerToken, workerSession := range server.morphlingWorkerSessions {
		if workerSession.MorphlingID == morphlingID {
			delete(server.morphlingWorkerSessions, workerToken)
		}
	}
}
