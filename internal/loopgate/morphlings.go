package loopgate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"morph/internal/sandbox"
)

func (server *Server) spawnMorphling(tokenClaims capabilityToken, spawnRequest MorphlingSpawnRequest) (MorphlingSpawnResponse, error) {
	if err := server.expirePendingMorphlingApprovals(); err != nil {
		return MorphlingSpawnResponse{}, err
	}
	if !server.policy.Tools.Morphlings.SpawnEnabled {
		return MorphlingSpawnResponse{
			RequestID:    strings.TrimSpace(spawnRequest.RequestID),
			Status:       ResponseStatusDenied,
			DenialCode:   DenialCodeMorphlingSpawnDisabled,
			DenialReason: redactMorphlingError(errMorphlingSpawnDisabled),
		}, nil
	}

	if strings.TrimSpace(spawnRequest.RequestID) == "" {
		requestIDSuffix, err := randomHex(8)
		if err != nil {
			return MorphlingSpawnResponse{}, fmt.Errorf("generate morphling request id: %w", err)
		}
		spawnRequest.RequestID = "req_" + requestIDSuffix
	}
	if strings.TrimSpace(spawnRequest.ParentSessionID) == "" {
		spawnRequest.ParentSessionID = tokenClaims.ControlSessionID
	}
	if strings.TrimSpace(spawnRequest.ParentSessionID) != tokenClaims.ControlSessionID {
		return MorphlingSpawnResponse{
			RequestID:    spawnRequest.RequestID,
			Status:       ResponseStatusDenied,
			DenialCode:   DenialCodeControlSessionBindingInvalid,
			DenialReason: "parent session id must match the authenticated control session",
		}, nil
	}

	validatedClass, found := server.morphlingClassPolicy.Class(spawnRequest.Class)
	if !found {
		return MorphlingSpawnResponse{
			RequestID:    spawnRequest.RequestID,
			Status:       ResponseStatusDenied,
			DenialCode:   DenialCodeMorphlingClassInvalid,
			DenialReason: redactMorphlingError(fmt.Errorf("%w: %s", errMorphlingClassInvalid, strings.TrimSpace(spawnRequest.Class))),
		}, nil
	}

	normalizedInputPaths := make([]string, 0, len(spawnRequest.Inputs))
	seenInputPaths := make(map[string]struct{}, len(spawnRequest.Inputs))
	for _, inputSpec := range spawnRequest.Inputs {
		_, sandboxRelativePath, err := server.sandboxPaths.ResolveHomePath(inputSpec.SandboxPath)
		if err != nil {
			return MorphlingSpawnResponse{
				RequestID:    spawnRequest.RequestID,
				Status:       ResponseStatusDenied,
				DenialCode:   DenialCodeMorphlingInputInvalid,
				DenialReason: redactMorphlingError(fmt.Errorf("%w: %v", errMorphlingInputInvalid, err)),
			}, nil
		}
		if zoneName := morphlingZoneForRelativePath(sandboxRelativePath); zoneName != "" && !validatedClass.AllowsZone(zoneName) {
			return MorphlingSpawnResponse{
				RequestID:    spawnRequest.RequestID,
				Status:       ResponseStatusDenied,
				DenialCode:   DenialCodeMorphlingInputInvalid,
				DenialReason: "morphling inputs fall outside the class sandbox policy",
			}, nil
		}
		if _, found := seenInputPaths[sandboxRelativePath]; found {
			continue
		}
		seenInputPaths[sandboxRelativePath] = struct{}{}
		normalizedInputPaths = append(normalizedInputPaths, sandboxRelativePath)
	}
	sort.Strings(normalizedInputPaths)

	grantedCapabilities := intersectMorphlingCapabilities(spawnRequest.RequestedCapabilities, validatedClass.AllowedCapabilities)
	if len(grantedCapabilities) == 0 {
		return MorphlingSpawnResponse{
			RequestID:    spawnRequest.RequestID,
			Status:       ResponseStatusDenied,
			DenialCode:   DenialCodePolicyDenied,
			DenialReason: "requested capabilities are not allowed by the morphling class",
		}, nil
	}

	requestedTimeBudgetSeconds := validatedClass.MaxTimeSeconds
	if spawnRequest.RequestedTimeBudgetSeconds > 0 && spawnRequest.RequestedTimeBudgetSeconds < requestedTimeBudgetSeconds {
		requestedTimeBudgetSeconds = spawnRequest.RequestedTimeBudgetSeconds
	}
	requestedTokenBudget := validatedClass.MaxTokens
	if spawnRequest.RequestedTokenBudget > 0 && spawnRequest.RequestedTokenBudget < requestedTokenBudget {
		requestedTokenBudget = spawnRequest.RequestedTokenBudget
	}

	goalHMAC, err := server.goalHMACForSession(tokenClaims.ControlSessionID, spawnRequest.Goal)
	if err != nil {
		return MorphlingSpawnResponse{}, err
	}
	if err := server.logEvent("morphling.spawn_requested", tokenClaims.ControlSessionID, map[string]interface{}{
		"request_id":           spawnRequest.RequestID,
		"class":                validatedClass.Name,
		"goal_hmac":            goalHMAC,
		"parent_session_id":    tokenClaims.ControlSessionID,
		"actor_label":          tokenClaims.ActorLabel,
		"client_session_label": tokenClaims.ClientSessionLabel,
	}); err != nil {
		return MorphlingSpawnResponse{}, fmt.Errorf("%w: %v", errMorphlingAuditUnavailable, err)
	}

	morphlingIDSuffix, err := randomHex(8)
	if err != nil {
		return MorphlingSpawnResponse{}, fmt.Errorf("generate morphling id: %w", err)
	}
	taskIDSuffix, err := randomHex(8)
	if err != nil {
		return MorphlingSpawnResponse{}, fmt.Errorf("generate morphling task id: %w", err)
	}
	morphlingID := "morphling-" + morphlingIDSuffix
	taskID := "task-" + taskIDSuffix
	nowUTC := server.now().UTC()
	requestedRecord := morphlingRecord{
		SchemaVersion:          "loopgate.morphling.v2",
		MorphlingID:            morphlingID,
		TaskID:                 taskID,
		RequestID:              spawnRequest.RequestID,
		ParentControlSessionID: tokenClaims.ControlSessionID,
		TenantID:               strings.TrimSpace(tokenClaims.TenantID),
		ActorLabel:             tokenClaims.ActorLabel,
		ClientSessionLabel:     tokenClaims.ClientSessionLabel,
		Class:                  validatedClass.Name,
		GoalText:               strings.TrimSpace(spawnRequest.Goal),
		GoalHMAC:               goalHMAC,
		GoalHint:               morphlingGoalHint(spawnRequest.Goal),
		State:                  morphlingStateRequested,
		RequestedCapabilities:  append([]string(nil), spawnRequest.RequestedCapabilities...),
		GrantedCapabilities:    append([]string(nil), grantedCapabilities...),
		InputRelativePaths:     normalizedInputPaths,
		RequiresReview:         validatedClass.CompletionRequiresReview,
		TimeBudgetSeconds:      requestedTimeBudgetSeconds,
		TokenBudget:            requestedTokenBudget,
		CreatedAtUTC:           nowUTC.Format(time.RFC3339Nano),
		LastEventAtUTC:         nowUTC.Format(time.RFC3339Nano),
	}

	server.morphlingsMu.Lock()
	previousRecords := cloneMorphlingRecords(server.morphlings)
	if activeMorphlingCountLocked(server.morphlings) >= server.policy.Tools.Morphlings.MaxActive {
		server.morphlingsMu.Unlock()
		return MorphlingSpawnResponse{
			RequestID:    spawnRequest.RequestID,
			Status:       ResponseStatusDenied,
			DenialCode:   DenialCodeMorphlingActiveLimitReached,
			DenialReason: redactMorphlingError(fmt.Errorf("%w: max_active=%d", errMorphlingActiveLimitReached, server.policy.Tools.Morphlings.MaxActive)),
		}, nil
	}
	if activeMorphlingCountForClassLocked(server.morphlings, validatedClass.Name) >= validatedClass.MaxConcurrent {
		server.morphlingsMu.Unlock()
		return MorphlingSpawnResponse{
			RequestID:    spawnRequest.RequestID,
			Status:       ResponseStatusDenied,
			DenialCode:   DenialCodeMorphlingActiveLimitReached,
			DenialReason: "morphling class active limit reached",
		}, nil
	}
	workingRecords := cloneMorphlingRecords(server.morphlings)
	workingRecords[requestedRecord.MorphlingID] = requestedRecord
	if err := saveMorphlingRecords(server.morphlingPath, workingRecords, server.morphlingStateKey); err != nil {
		server.morphlingsMu.Unlock()
		return MorphlingSpawnResponse{}, err
	}
	server.morphlings = workingRecords

	authorizingRecord, transitionErr := server.transitionMorphlingLocked(requestedRecord.MorphlingID, morphlingEventBeginAuthorization, nowUTC, func(updatedRecord *morphlingRecord) error {
		return nil
	})
	if transitionErr != nil {
		server.morphlingsMu.Unlock()
		return MorphlingSpawnResponse{}, transitionErr
	}
	server.morphlingsMu.Unlock()

	if err := server.logEvent("morphling.authorizing", tokenClaims.ControlSessionID, map[string]interface{}{
		"morphling_id":       authorizingRecord.MorphlingID,
		"class":              authorizingRecord.Class,
		"control_session_id": tokenClaims.ControlSessionID,
	}); err != nil {
		server.restoreMorphlingRecords(previousRecords)
		return MorphlingSpawnResponse{}, fmt.Errorf("%w: %v", errMorphlingAuditUnavailable, err)
	}

	if validatedClass.SpawnRequiresApproval {
		spawnResponse, err := server.createPendingMorphlingSpawnApproval(tokenClaims, validatedClass, authorizingRecord)
		if err != nil {
			_ = server.failMorphlingAfterAdmission(tokenClaims.ControlSessionID, authorizingRecord.MorphlingID, morphlingOutcomeFailed, morphlingReasonExecutionStartFailed)
			return spawnResponse, err
		}
		if spawnResponse.Status != ResponseStatusPendingApproval {
			_ = server.failMorphlingAfterAdmission(tokenClaims.ControlSessionID, authorizingRecord.MorphlingID, morphlingOutcomeFailed, morphlingReasonExecutionStartFailed)
		}
		return spawnResponse, nil
	}

	spawnResponse, err := server.finalizeSpawnedMorphling(tokenClaims, validatedClass, authorizingRecord, "")
	if err != nil {
		_ = server.failMorphlingAfterAdmission(tokenClaims.ControlSessionID, authorizingRecord.MorphlingID, morphlingOutcomeFailed, morphlingReasonExecutionStartFailed)
	}
	return spawnResponse, err
}

func (server *Server) createPendingMorphlingSpawnApproval(tokenClaims capabilityToken, validatedClass validatedMorphlingClass, authorizingRecord morphlingRecord) (MorphlingSpawnResponse, error) {
	approvalIDSuffix, err := randomHex(8)
	if err != nil {
		return MorphlingSpawnResponse{}, fmt.Errorf("generate morphling approval id: %w", err)
	}
	decisionNonce, err := randomHex(16)
	if err != nil {
		return MorphlingSpawnResponse{}, fmt.Errorf("generate morphling approval decision nonce: %w", err)
	}
	approvalID := "approval-" + approvalIDSuffix
	approvalDeadlineUTC := server.now().UTC().Add(time.Duration(validatedClass.SpawnApprovalTTLSeconds) * time.Second)

	spawnRequest := cloneCapabilityRequest(CapabilityRequest{
		RequestID:  authorizingRecord.RequestID,
		SessionID:  tokenClaims.ControlSessionID,
		Actor:      tokenClaims.ActorLabel,
		Capability: "morphling.spawn",
		Arguments: map[string]string{
			"class":        authorizingRecord.Class,
			"goal_hint":    authorizingRecord.GoalHint,
			"morphling_id": authorizingRecord.MorphlingID,
		},
	})
	// Same manifest/body binding as capability.execute approvals so operator decisions and
	// post-approval execution integrity checks apply uniformly (AMP RFC 0005 §6).
	manifestSHA256, bodySHA256, manifestErr := buildCapabilityApprovalManifest(spawnRequest, approvalDeadlineUTC.UnixMilli())
	if manifestErr != nil {
		return MorphlingSpawnResponse{}, fmt.Errorf("compute morphling spawn approval manifest: %w", manifestErr)
	}

	server.mu.Lock()
	server.pruneExpiredLocked()
	if len(server.approvals) >= server.maxTotalApprovalRecords {
		server.mu.Unlock()
		if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           authorizingRecord.RequestID,
			"capability":           "morphling.spawn",
			"reason":               "control-plane approval store is at capacity",
			"denial_code":          DenialCodeControlPlaneStateSaturated,
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
		}); err != nil {
			return MorphlingSpawnResponse{}, fmt.Errorf("%w: %v", errMorphlingAuditUnavailable, err)
		}
		return MorphlingSpawnResponse{
			RequestID:    authorizingRecord.RequestID,
			Status:       ResponseStatusDenied,
			DenialReason: "control-plane approval store is at capacity",
			DenialCode:   DenialCodeControlPlaneStateSaturated,
		}, nil
	}
	if server.countPendingApprovalsForSessionLocked(tokenClaims.ControlSessionID) >= server.maxPendingApprovalsPerControlSession {
		server.mu.Unlock()
		if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           authorizingRecord.RequestID,
			"capability":           "morphling.spawn",
			"reason":               "pending approval limit reached for control session",
			"denial_code":          DenialCodePendingApprovalLimitReached,
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
		}); err != nil {
			return MorphlingSpawnResponse{}, fmt.Errorf("%w: %v", errMorphlingAuditUnavailable, err)
		}
		return MorphlingSpawnResponse{
			RequestID:    authorizingRecord.RequestID,
			Status:       ResponseStatusDenied,
			DenialReason: "pending approval limit reached for control session",
			DenialCode:   DenialCodePendingApprovalLimitReached,
		}, nil
	}
	pendingApprovalRecord := pendingApproval{
		ID:        approvalID,
		Request:   spawnRequest,
		CreatedAt: server.now().UTC(),
		ExpiresAt: approvalDeadlineUTC,
		Metadata: map[string]interface{}{
			"approval_class": ApprovalClassLaunchMorphling,
			"approval_kind":  "morphling_spawn",
			"class":          authorizingRecord.Class,
			"goal_hint":      authorizingRecord.GoalHint,
			"morphling_id":   authorizingRecord.MorphlingID,
		},
		Reason:           "class requires spawn approval",
		ControlSessionID: tokenClaims.ControlSessionID,
		DecisionNonce:    decisionNonce,
		ExecutionContext: approvalExecutionContext{
			ControlSessionID:    tokenClaims.ControlSessionID,
			ActorLabel:          tokenClaims.ActorLabel,
			ClientSessionLabel:  tokenClaims.ClientSessionLabel,
			AllowedCapabilities: copyCapabilitySet(tokenClaims.AllowedCapabilities),
			TenantID:            tokenClaims.TenantID,
			UserID:              tokenClaims.UserID,
		},
		State:                  approvalStatePending,
		ApprovalManifestSHA256: manifestSHA256,
		ExecutionBodySHA256:    bodySHA256,
	}
	server.approvals[approvalID] = pendingApprovalRecord
	server.noteExpiryCandidateLocked(approvalDeadlineUTC)
	server.mu.Unlock()

	nowUTC := server.now().UTC()
	server.morphlingsMu.Lock()
	updatedRecord, err := server.transitionMorphlingLocked(authorizingRecord.MorphlingID, morphlingEventAwaitSpawnApproval, nowUTC, func(updatedRecord *morphlingRecord) error {
		updatedRecord.ApprovalID = approvalID
		updatedRecord.ApprovalDeadlineUTC = approvalDeadlineUTC.Format(time.RFC3339Nano)
		return nil
	})
	server.morphlingsMu.Unlock()
	if err != nil {
		server.mu.Lock()
		delete(server.approvals, approvalID)
		server.mu.Unlock()
		return MorphlingSpawnResponse{}, err
	}

	server.emitUIApprovalPending(pendingApprovalRecord)
	if err := server.logEvent("morphling.spawn_approval_pending", tokenClaims.ControlSessionID, map[string]interface{}{
		"morphling_id":          updatedRecord.MorphlingID,
		"approval_id":           approvalID,
		"approval_deadline_utc": updatedRecord.ApprovalDeadlineUTC,
		"control_session_id":    tokenClaims.ControlSessionID,
	}); err != nil {
		return MorphlingSpawnResponse{}, fmt.Errorf("%w: %v", errMorphlingAuditUnavailable, err)
	}

	return MorphlingSpawnResponse{
		RequestID:              updatedRecord.RequestID,
		Status:                 ResponseStatusPendingApproval,
		MorphlingID:            updatedRecord.MorphlingID,
		TaskID:                 updatedRecord.TaskID,
		State:                  updatedRecord.State,
		Class:                  updatedRecord.Class,
		ApprovalID:             approvalID,
		ApprovalDeadlineUTC:    updatedRecord.ApprovalDeadlineUTC,
		ApprovalManifestSHA256: manifestSHA256,
		ApprovalDecisionNonce:  decisionNonce,
	}, nil
}

func (server *Server) finalizeSpawnedMorphling(tokenClaims capabilityToken, validatedClass validatedMorphlingClass, currentRecord morphlingRecord, decisionNonce string) (MorphlingSpawnResponse, error) {
	workingDirAbsolutePath, workingDirRelativePath, err := server.sandboxPaths.BuildAgentWorkingDirectory(currentRecord.TaskID)
	if err != nil {
		return MorphlingSpawnResponse{}, err
	}
	if err := os.Mkdir(workingDirAbsolutePath, 0o700); err != nil {
		if os.IsExist(err) {
			return MorphlingSpawnResponse{}, fmt.Errorf("%w: %s", sandbox.ErrSandboxDestinationExists, workingDirAbsolutePath)
		}
		return MorphlingSpawnResponse{}, fmt.Errorf("create morphling working directory: %w", err)
	}

	allowedRelativePaths := append([]string{workingDirRelativePath}, currentRecord.InputRelativePaths...)
	sort.Strings(allowedRelativePaths)
	allowedRelativePaths = slices.Compact(allowedRelativePaths)
	nowUTC := server.now().UTC()
	tokenExpiryUTC := nowUTC.Add(time.Duration(validatedClass.CapabilityTokenTTLSeconds) * time.Second)

	server.morphlingsMu.Lock()
	updatedRecord, err := server.transitionMorphlingLocked(currentRecord.MorphlingID, morphlingEventSpawnSucceeded, nowUTC, func(updatedRecord *morphlingRecord) error {
		updatedRecord.WorkingDirRelativePath = workingDirRelativePath
		updatedRecord.AllowedRelativePaths = allowedRelativePaths
		updatedRecord.SpawnedAtUTC = nowUTC.Format(time.RFC3339Nano)
		updatedRecord.TokenExpiryUTC = tokenExpiryUTC.Format(time.RFC3339Nano)
		return nil
	})
	server.morphlingsMu.Unlock()
	if err != nil {
		_ = os.RemoveAll(workingDirAbsolutePath)
		return MorphlingSpawnResponse{}, err
	}

	if strings.TrimSpace(decisionNonce) != "" {
		if err := server.logEvent("morphling.spawn_approved", tokenClaims.ControlSessionID, map[string]interface{}{
			"morphling_id":   updatedRecord.MorphlingID,
			"approval_id":    updatedRecord.ApprovalID,
			"decision_nonce": decisionNonce,
		}); err != nil {
			return MorphlingSpawnResponse{}, fmt.Errorf("%w: %v", errMorphlingAuditUnavailable, err)
		}
	}
	if err := server.logEvent("morphling.spawned", tokenClaims.ControlSessionID, map[string]interface{}{
		"morphling_id":         updatedRecord.MorphlingID,
		"task_id":              updatedRecord.TaskID,
		"class":                updatedRecord.Class,
		"granted_capabilities": updatedRecord.GrantedCapabilities,
		"virtual_sandbox_path": sandbox.VirtualizeRelativeHomePath(updatedRecord.WorkingDirRelativePath),
		"token_expiry_utc":     updatedRecord.TokenExpiryUTC,
		"control_session_id":   tokenClaims.ControlSessionID,
		"actor_label":          tokenClaims.ActorLabel,
		"client_session_label": tokenClaims.ClientSessionLabel,
	}); err != nil {
		return MorphlingSpawnResponse{}, fmt.Errorf("%w: %v", errMorphlingAuditUnavailable, err)
	}

	return MorphlingSpawnResponse{
		RequestID:           updatedRecord.RequestID,
		Status:              ResponseStatusSuccess,
		MorphlingID:         updatedRecord.MorphlingID,
		TaskID:              updatedRecord.TaskID,
		State:               updatedRecord.State,
		Class:               updatedRecord.Class,
		GrantedCapabilities: append([]string(nil), updatedRecord.GrantedCapabilities...),
		VirtualSandboxPath:  sandbox.VirtualizeRelativeHomePath(updatedRecord.WorkingDirRelativePath),
		SpawnedAtUTC:        updatedRecord.SpawnedAtUTC,
		TokenExpiryUTC:      updatedRecord.TokenExpiryUTC,
	}, nil
}

func intersectMorphlingCapabilities(requestedCapabilities []string, allowedCapabilities []string) []string {
	allowedSet := make(map[string]struct{}, len(allowedCapabilities))
	for _, allowedCapability := range allowedCapabilities {
		allowedSet[allowedCapability] = struct{}{}
	}
	grantedCapabilities := make([]string, 0, len(requestedCapabilities))
	for _, requestedCapability := range requestedCapabilities {
		if _, allowed := allowedSet[requestedCapability]; allowed {
			grantedCapabilities = append(grantedCapabilities, requestedCapability)
		}
	}
	sort.Strings(grantedCapabilities)
	return slices.Compact(grantedCapabilities)
}

func (server *Server) morphlingStatus(tokenClaims capabilityToken, statusRequest MorphlingStatusRequest) (MorphlingStatusResponse, error) {
	if err := server.expirePendingMorphlingApprovals(); err != nil {
		return MorphlingStatusResponse{}, err
	}
	if err := server.expirePendingMorphlingReviews(); err != nil {
		return MorphlingStatusResponse{}, err
	}

	server.morphlingsMu.Lock()
	defer server.morphlingsMu.Unlock()

	if strings.TrimSpace(statusRequest.MorphlingID) != "" {
		morphlingRecord, found := server.morphlings[strings.TrimSpace(statusRequest.MorphlingID)]
		if !found {
			return MorphlingStatusResponse{}, errMorphlingNotFound
		}
		if morphlingRecord.ParentControlSessionID != tokenClaims.ControlSessionID {
			return MorphlingStatusResponse{}, errMorphlingNotFound
		}
		if morphlingTenantDenied(morphlingRecord, tokenClaims) {
			return MorphlingStatusResponse{}, errMorphlingNotFound
		}
		if !statusRequest.IncludeTerminated && morphlingRecord.State == morphlingStateTerminated {
			return MorphlingStatusResponse{
				SpawnEnabled: server.policy.Tools.Morphlings.SpawnEnabled,
				MaxActive:    server.policy.Tools.Morphlings.MaxActive,
				ActiveCount:  activeMorphlingCountLocked(server.morphlings),
				Morphlings:   []MorphlingSummary{},
			}, nil
		}
		return MorphlingStatusResponse{
			SpawnEnabled: server.policy.Tools.Morphlings.SpawnEnabled,
			MaxActive:    server.policy.Tools.Morphlings.MaxActive,
			ActiveCount:  activeMorphlingCountLocked(server.morphlings),
			Morphlings:   []MorphlingSummary{morphlingSummaryFromRecord(morphlingRecord)},
		}, nil
	}

	morphlingIDs := make([]string, 0, len(server.morphlings))
	for morphlingID := range server.morphlings {
		morphlingIDs = append(morphlingIDs, morphlingID)
	}
	sort.Strings(morphlingIDs)

	morphlingSummaries := make([]MorphlingSummary, 0, len(morphlingIDs))
	for _, morphlingID := range morphlingIDs {
		morphlingRecord := server.morphlings[morphlingID]
		if morphlingRecord.ParentControlSessionID != tokenClaims.ControlSessionID {
			continue
		}
		if morphlingTenantDenied(morphlingRecord, tokenClaims) {
			continue
		}
		if !statusRequest.IncludeTerminated && morphlingRecord.State == morphlingStateTerminated {
			continue
		}
		morphlingSummaries = append(morphlingSummaries, morphlingSummaryFromRecord(morphlingRecord))
	}
	return MorphlingStatusResponse{
		SpawnEnabled:       server.policy.Tools.Morphlings.SpawnEnabled,
		MaxActive:          server.policy.Tools.Morphlings.MaxActive,
		ActiveCount:        activeMorphlingCountLocked(server.morphlings),
		PendingReviewCount: pendingReviewCountLocked(server.morphlings, tokenClaims.ControlSessionID),
		Morphlings:         morphlingSummaries,
	}, nil
}

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

func (server *Server) transitionMorphlingLocked(morphlingID string, lifecycleEvent morphlingLifecycleEvent, eventTime time.Time, mutateRecord func(*morphlingRecord) error) (morphlingRecord, error) {
	currentRecord, found := server.morphlings[morphlingID]
	if !found {
		return morphlingRecord{}, errMorphlingNotFound
	}
	nextState, err := morphlingNextState(currentRecord.State, lifecycleEvent)
	if err != nil {
		return morphlingRecord{}, err
	}
	updatedRecord := cloneMorphlingRecord(currentRecord)
	updatedRecord.State = nextState
	updatedRecord.LastEventAtUTC = eventTime.UTC().Format(time.RFC3339Nano)
	if mutateRecord != nil {
		if err := mutateRecord(&updatedRecord); err != nil {
			return morphlingRecord{}, err
		}
	}
	updatedRecord.StatusText = morphlingStatusText(updatedRecord)
	if err := updatedRecord.Validate(); err != nil {
		return morphlingRecord{}, err
	}

	workingRecords := cloneMorphlingRecords(server.morphlings)
	workingRecords[morphlingID] = updatedRecord
	if err := saveMorphlingRecords(server.morphlingPath, workingRecords, server.morphlingStateKey); err != nil {
		return morphlingRecord{}, err
	}
	server.morphlings = workingRecords
	return updatedRecord, nil
}

func (server *Server) updateMorphlingRecordLocked(morphlingID string, eventTime time.Time, mutateRecord func(*morphlingRecord) error) (morphlingRecord, error) {
	currentRecord, found := server.morphlings[morphlingID]
	if !found {
		return morphlingRecord{}, errMorphlingNotFound
	}
	updatedRecord := cloneMorphlingRecord(currentRecord)
	updatedRecord.LastEventAtUTC = eventTime.UTC().Format(time.RFC3339Nano)
	if mutateRecord != nil {
		if err := mutateRecord(&updatedRecord); err != nil {
			return morphlingRecord{}, err
		}
	}
	updatedRecord.StatusText = morphlingStatusText(updatedRecord)
	if err := updatedRecord.Validate(); err != nil {
		return morphlingRecord{}, err
	}

	workingRecords := cloneMorphlingRecords(server.morphlings)
	workingRecords[morphlingID] = updatedRecord
	if err := saveMorphlingRecords(server.morphlingPath, workingRecords, server.morphlingStateKey); err != nil {
		return morphlingRecord{}, err
	}
	server.morphlings = workingRecords
	return updatedRecord, nil
}

func (server *Server) rollbackMorphlingRecordAfterAuditFailure(morphlingID string, expectedCurrentState string, previousRecord morphlingRecord) error {
	server.morphlingsMu.Lock()
	defer server.morphlingsMu.Unlock()

	currentRecord, found := server.morphlings[morphlingID]
	if !found {
		return errMorphlingNotFound
	}
	if currentRecord.State != expectedCurrentState {
		return fmt.Errorf("%w: morphling %s changed state from %s before audit rollback", errMorphlingStateInvalid, morphlingID, expectedCurrentState)
	}

	workingRecords := cloneMorphlingRecords(server.morphlings)
	workingRecords[morphlingID] = cloneMorphlingRecord(previousRecord)
	if err := saveMorphlingRecords(server.morphlingPath, workingRecords, server.morphlingStateKey); err != nil {
		return err
	}
	server.morphlings = workingRecords
	return nil
}

func (server *Server) restoreMorphlingRecords(previousRecords map[string]morphlingRecord) {
	server.morphlingsMu.Lock()
	defer server.morphlingsMu.Unlock()

	if err := saveMorphlingRecords(server.morphlingPath, previousRecords, server.morphlingStateKey); err != nil {
		return
	}
	server.morphlings = cloneMorphlingRecords(previousRecords)
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

func (server *Server) resolveMorphlingSpawnApproval(pendingApproval pendingApproval, approved bool, decisionNonce string) (CapabilityResponse, error) {
	morphlingID, _ := pendingApproval.Metadata["morphling_id"].(string)
	if strings.TrimSpace(morphlingID) == "" {
		return CapabilityResponse{
			RequestID:         pendingApproval.Request.RequestID,
			Status:            ResponseStatusError,
			DenialReason:      "morphling approval is missing morphling_id metadata",
			DenialCode:        DenialCodeApprovalStateInvalid,
			ApprovalRequestID: pendingApproval.ID,
		}, nil
	}
	if approved {
		server.morphlingsMu.Lock()
		record, found := server.morphlings[morphlingID]
		server.morphlingsMu.Unlock()
		if !found {
			return CapabilityResponse{
				RequestID:         pendingApproval.Request.RequestID,
				Status:            ResponseStatusDenied,
				DenialReason:      "morphling approval target was not found",
				DenialCode:        DenialCodeMorphlingNotFound,
				ApprovalRequestID: pendingApproval.ID,
			}, nil
		}
		if record.State != morphlingStatePendingSpawnApproval {
			denialReason := "approval request is no longer pending"
			denialCode := DenialCodeApprovalStateConflict
			if record.TerminationReason == morphlingReasonSpawnApprovalExpired {
				denialReason = "approval request expired"
				denialCode = DenialCodeApprovalDenied
			}
			return CapabilityResponse{
				RequestID:         pendingApproval.Request.RequestID,
				Status:            ResponseStatusDenied,
				DenialReason:      denialReason,
				DenialCode:        denialCode,
				ApprovalRequestID: pendingApproval.ID,
				Metadata: map[string]interface{}{
					"morphling_id": morphlingID,
					"state":        record.State,
				},
			}, nil
		}
		validatedClass, found := server.morphlingClassPolicy.Class(record.Class)
		if !found {
			return CapabilityResponse{
				RequestID:         pendingApproval.Request.RequestID,
				Status:            ResponseStatusDenied,
				DenialReason:      "morphling class is invalid",
				DenialCode:        DenialCodeMorphlingClassInvalid,
				ApprovalRequestID: pendingApproval.ID,
			}, nil
		}
		if _, err := server.finalizeSpawnedMorphling(server.capabilityTokenForMorphlingApprovalFinalize(pendingApproval), validatedClass, record, decisionNonce); err != nil {
			return CapabilityResponse{}, err
		}
		// Caller commits approval.granted + consumed and then markApprovalExecutionResult(Success).
		return CapabilityResponse{
			RequestID:         pendingApproval.Request.RequestID,
			Status:            ResponseStatusSuccess,
			ApprovalRequestID: pendingApproval.ID,
			Metadata: map[string]interface{}{
				"morphling_id": morphlingID,
				"state":        morphlingStateSpawned,
			},
		}, nil
	}

	nowUTC := server.now().UTC()
	server.morphlingsMu.Lock()
	terminatingRecord, err := server.transitionMorphlingLocked(morphlingID, morphlingEventBeginTermination, nowUTC, func(updatedRecord *morphlingRecord) error {
		updatedRecord.Outcome = morphlingOutcomeCancelled
		updatedRecord.TerminationReason = morphlingReasonSpawnDeniedByOperator
		updatedRecord.ReviewDeadlineUTC = ""
		return nil
	})
	server.morphlingsMu.Unlock()
	if err != nil {
		return CapabilityResponse{}, err
	}
	if err := server.logEvent("morphling.spawn_denied_by_operator", pendingApproval.ControlSessionID, map[string]interface{}{
		"morphling_id":   terminatingRecord.MorphlingID,
		"approval_id":    pendingApproval.ID,
		"decision_nonce": decisionNonce,
	}); err != nil {
		return CapabilityResponse{}, fmt.Errorf("%w: %v", errMorphlingAuditUnavailable, err)
	}
	if err := server.logEvent("morphling.terminating", pendingApproval.ControlSessionID, map[string]interface{}{
		"morphling_id":       terminatingRecord.MorphlingID,
		"outcome":            terminatingRecord.Outcome,
		"termination_reason": terminatingRecord.TerminationReason,
		"control_session_id": pendingApproval.ControlSessionID,
	}); err != nil {
		return CapabilityResponse{}, fmt.Errorf("%w: %v", errMorphlingAuditUnavailable, err)
	}
	if _, err := server.completeMorphlingTermination(pendingApproval.ControlSessionID, morphlingID); err != nil {
		return CapabilityResponse{}, err
	}
	server.markApprovalExecutionResult(pendingApproval.ID, ResponseStatusDenied)
	return CapabilityResponse{
		RequestID:         pendingApproval.Request.RequestID,
		Status:            ResponseStatusDenied,
		DenialReason:      "approval denied",
		DenialCode:        DenialCodeApprovalDenied,
		ApprovalRequestID: pendingApproval.ID,
		Metadata: map[string]interface{}{
			"morphling_id": morphlingID,
			"state":        morphlingStateTerminated,
		},
	}, nil
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

func hashMorphlingArtifactManifest(manifest interface{}) (string, error) {
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	return hashBytes(manifestBytes), nil
}

func hashBytes(payloadBytes []byte) string {
	payloadHash := sha256.Sum256(payloadBytes)
	return hex.EncodeToString(payloadHash[:])
}

func isMorphlingSpawnApproval(pendingApproval pendingApproval) bool {
	approvalKind, _ := pendingApproval.Metadata["approval_kind"].(string)
	return strings.TrimSpace(approvalKind) == "morphling_spawn"
}
