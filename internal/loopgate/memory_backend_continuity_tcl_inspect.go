package loopgate

import (
	"fmt"
	"strings"
	"time"
)

func (backend *continuityTCLMemoryBackend) inspectContinuityAuthoritatively(authenticatedSession capabilityToken, rawRequest ContinuityInspectRequest) (ContinuityInspectResponse, error) {
	validatedRequest, err := normalizeContinuityInspectRequest(rawRequest)
	if err != nil {
		return ContinuityInspectResponse{}, err
	}
	if err := validateContinuityInspectProvenance(authenticatedSession, validatedRequest); err != nil {
		return ContinuityInspectResponse{}, err
	}

	backend.server.memoryMu.Lock()
	existingInspection, foundExisting := backend.partition.state.Inspections[validatedRequest.InspectionID]
	if foundExisting {
		existingInspection = normalizeContinuityInspectionRecordMust(existingInspection)
		_ = backend.server.inspectionLineageSelectionDecisionLocked(backend.partition.state, existingInspection.InspectionID)
		backend.server.memoryMu.Unlock()
		return buildContinuityInspectResponse(existingInspection), nil
	}
	backend.server.memoryMu.Unlock()

	var inspectResponse ContinuityInspectResponse
	// Preserve the double-check inside the mutation closure so concurrent replay or
	// duplicate submissions cannot race the optimistic read above into divergent state.
	err = backend.server.mutateContinuityMemory(backend.partition.tenantID, authenticatedSession.ControlSessionID, "memory.continuity.inspected", func(workingState continuityMemoryState, nowUTC time.Time) (continuityMemoryState, map[string]interface{}, continuityMutationEvents, error) {
		if existingInspection, found := workingState.Inspections[validatedRequest.InspectionID]; found {
			existingInspection = normalizeContinuityInspectionRecordMust(existingInspection)
			_ = backend.server.inspectionLineageSelectionDecisionLocked(workingState, existingInspection.InspectionID)
			inspectResponse = buildContinuityInspectResponse(existingInspection)
			return workingState, nil, continuityMutationEvents{}, nil
		}

		inspectionRecord := continuityInspectionRecord{
			InspectionID:       validatedRequest.InspectionID,
			ThreadID:           validatedRequest.ThreadID,
			Scope:              validatedRequest.Scope,
			SubmittedAtUTC:     nowUTC.Format(time.RFC3339Nano),
			CompletedAtUTC:     nowUTC.Format(time.RFC3339Nano),
			EventCount:         len(validatedRequest.Events),
			ApproxPayloadBytes: actualContinuityPayloadBytes(validatedRequest.Events),
			ApproxPromptTokens: actualContinuityPromptTokens(validatedRequest.Events),
			Lineage: continuityInspectionLineage{
				Status:       continuityLineageStatusEligible,
				ChangedAtUTC: nowUTC.Format(time.RFC3339Nano),
				OperationID:  validatedRequest.InspectionID,
			},
		}
		inspectionRecord.DerivationOutcome = continuityInspectionOutcomeDerived
		if !backend.server.continuityThresholdReached(inspectionRecord) {
			inspectionRecord.DerivationOutcome = continuityInspectionOutcomeSkippedThreshold
		}

		var derivedDistillate continuityDistillateRecord
		var derivedResonateKey continuityResonateKeyRecord
		var hasDerivedArtifacts bool
		if inspectionRecord.DerivationOutcome == continuityInspectionOutcomeDerived {
			derivedDistillate = deriveContinuityDistillate(validatedRequest, inspectionRecord, nowUTC, backend.server.runtimeConfig, backend.server.goalAliases)
			if len(derivedDistillate.Facts) == 0 && len(derivedDistillate.GoalOps) == 0 && len(derivedDistillate.UnresolvedItemOps) == 0 {
				inspectionRecord.DerivationOutcome = continuityInspectionOutcomeNoArtifacts
			} else {
				derivedResonateKey = deriveContinuityResonateKey(derivedDistillate, nowUTC)
				hasDerivedArtifacts = true
			}
		}

		switch inspectionRecord.DerivationOutcome {
		case continuityInspectionOutcomeDerived:
			if backend.server.policy.Memory.ContinuityReviewRequired {
				inspectionRecord.Review = continuityInspectionReview{
					Status: continuityReviewStatusPendingReview,
				}
			} else {
				inspectionRecord.Review = continuityInspectionReview{
					Status:         continuityReviewStatusAccepted,
					DecisionSource: continuityReviewDecisionSourceAuto,
					ReviewedAtUTC:  nowUTC.Format(time.RFC3339Nano),
					OperationID:    validatedRequest.InspectionID,
				}
			}
		default:
			inspectionRecord.Review = continuityInspectionReview{
				Status:         continuityReviewStatusAccepted,
				DecisionSource: continuityReviewDecisionSourceAuto,
				ReviewedAtUTC:  nowUTC.Format(time.RFC3339Nano),
				OperationID:    validatedRequest.InspectionID,
			}
		}

		inspectionRecord.Outcome = inspectionRecord.DerivationOutcome
		if hasDerivedArtifacts {
			inspectionRecord.DerivedDistillateIDs = []string{derivedDistillate.DistillateID}
			inspectionRecord.DerivedResonateKeyIDs = []string{derivedResonateKey.KeyID}
			workingState.Distillates[derivedDistillate.DistillateID] = derivedDistillate
			workingState.ResonateKeys[derivedResonateKey.KeyID] = derivedResonateKey
		}
		workingState.Inspections[inspectionRecord.InspectionID] = inspectionRecord
		inspectResponse = buildContinuityInspectResponse(inspectionRecord)
		mutationEvents := continuityMutationEvents{
			Continuity: []continuityAuthoritativeEvent{{
				SchemaVersion: continuityMemorySchemaVersion,
				EventID:       "continuity_inspection_" + inspectionRecord.InspectionID,
				EventType:     "continuity_inspection_recorded",
				CreatedAtUTC:  nowUTC.Format(time.RFC3339Nano),
				Actor:         authenticatedSession.ControlSessionID,
				Scope:         inspectionRecord.Scope,
				InspectionID:  inspectionRecord.InspectionID,
				ThreadID:      inspectionRecord.ThreadID,
				GoalType:      derivedDistillate.GoalType,
				GoalFamilyID:  derivedDistillate.GoalFamilyID,
				Request:       &validatedRequest,
				Inspection:    ptrContinuityInspectionRecord(cloneContinuityInspectionRecord(inspectionRecord)),
				Distillate:    ptrContinuityDistillateRecord(cloneContinuityDistillateRecord(derivedDistillate)),
				ResonateKey:   ptrContinuityResonateKeyRecord(cloneContinuityResonateKeyRecord(derivedResonateKey)),
			}},
		}
		if !hasDerivedArtifacts {
			mutationEvents.Continuity[0].Distillate = nil
			mutationEvents.Continuity[0].ResonateKey = nil
			mutationEvents.Continuity[0].GoalType = ""
			mutationEvents.Continuity[0].GoalFamilyID = ""
		}
		if hasDerivedArtifacts {
			mutationEvents.Goal = append(mutationEvents.Goal, continuityGoalEvent{
				SchemaVersion:      continuityMemorySchemaVersion,
				EventID:            "goal_projection_" + inspectionRecord.InspectionID,
				EventType:          "goal_projection_updated",
				CreatedAtUTC:       nowUTC.Format(time.RFC3339Nano),
				Actor:              authenticatedSession.ControlSessionID,
				InspectionID:       inspectionRecord.InspectionID,
				ThreadID:           inspectionRecord.ThreadID,
				GoalType:           derivedDistillate.GoalType,
				GoalFamilyID:       derivedDistillate.GoalFamilyID,
				NeedsAliasCuration: strings.Contains(derivedDistillate.GoalFamilyID, ":fallback_"),
				GoalOps:            append([]continuityGoalOp(nil), derivedDistillate.GoalOps...),
				UnresolvedItemOps:  append([]continuityUnresolvedItemOp(nil), derivedDistillate.UnresolvedItemOps...),
			})
		}
		return workingState, map[string]interface{}{
			"inspection_id":          inspectionRecord.InspectionID,
			"thread_id":              inspectionRecord.ThreadID,
			"derivation_outcome":     inspectionRecord.DerivationOutcome,
			"review_status":          inspectionRecord.Review.Status,
			"lineage_status":         inspectionRecord.Lineage.Status,
			"event_count":            inspectionRecord.EventCount,
			"approx_payload_bytes":   inspectionRecord.ApproxPayloadBytes,
			"approx_prompt_tokens":   inspectionRecord.ApproxPromptTokens,
			"derived_distillate_ids": append([]string(nil), inspectionRecord.DerivedDistillateIDs...),
			"derived_resonate_keys":  append([]string(nil), inspectionRecord.DerivedResonateKeyIDs...),
		}, mutationEvents, nil
	})
	if err != nil {
		return ContinuityInspectResponse{}, err
	}
	return inspectResponse, nil
}

func validateContinuityInspectProvenance(authenticatedSession capabilityToken, validatedRequest ContinuityInspectRequest) error {
	allowedSessionIDs := map[string]struct{}{}
	if controlSessionID := strings.TrimSpace(authenticatedSession.ControlSessionID); controlSessionID != "" {
		allowedSessionIDs[controlSessionID] = struct{}{}
	}
	if clientSessionLabel := strings.TrimSpace(authenticatedSession.ClientSessionLabel); clientSessionLabel != "" {
		allowedSessionIDs[clientSessionLabel] = struct{}{}
	}
	if len(allowedSessionIDs) == 0 {
		return continuityGovernanceError{
			httpStatus:     401,
			responseStatus: ResponseStatusDenied,
			denialCode:     DenialCodeCapabilityTokenInvalid,
			reason:         "continuity inspect requires authenticated session binding",
		}
	}

	seenEventHashes := make(map[string]struct{}, len(validatedRequest.Events))
	var previousLedgerSequence int64
	for eventIndex, continuityEvent := range validatedRequest.Events {
		// Keep continuity inspect pinned to the authenticated request context so a caller cannot
		// smuggle another thread or session's events into durable memory just by shaping a valid
		// request body. The real authority is the authenticated control session, not the packet.
		if continuityEvent.ThreadID != validatedRequest.ThreadID {
			return continuityGovernanceError{
				httpStatus:     400,
				responseStatus: ResponseStatusDenied,
				denialCode:     DenialCodeMalformedRequest,
				reason:         fmt.Sprintf("continuity event %d thread_id must match request thread_id", eventIndex+1),
			}
		}
		if continuityEvent.Scope != validatedRequest.Scope {
			return continuityGovernanceError{
				httpStatus:     400,
				responseStatus: ResponseStatusDenied,
				denialCode:     DenialCodeMalformedRequest,
				reason:         fmt.Sprintf("continuity event %d scope must match request scope", eventIndex+1),
			}
		}
		if _, allowed := allowedSessionIDs[continuityEvent.SessionID]; !allowed {
			return continuityGovernanceError{
				httpStatus:     400,
				responseStatus: ResponseStatusDenied,
				denialCode:     DenialCodeMalformedRequest,
				reason:         fmt.Sprintf("continuity event %d session_id must match authenticated session", eventIndex+1),
			}
		}
		if _, duplicate := seenEventHashes[continuityEvent.EventHash]; duplicate {
			return continuityGovernanceError{
				httpStatus:     400,
				responseStatus: ResponseStatusDenied,
				denialCode:     DenialCodeMalformedRequest,
				reason:         fmt.Sprintf("continuity event %d event_hash must be unique within an inspection", eventIndex+1),
			}
		}
		seenEventHashes[continuityEvent.EventHash] = struct{}{}
		if eventIndex > 0 && continuityEvent.LedgerSequence <= previousLedgerSequence {
			return continuityGovernanceError{
				httpStatus:     400,
				responseStatus: ResponseStatusDenied,
				denialCode:     DenialCodeMalformedRequest,
				reason:         "continuity events must be strictly ordered by ledger_sequence",
			}
		}
		previousLedgerSequence = continuityEvent.LedgerSequence
	}
	return nil
}
