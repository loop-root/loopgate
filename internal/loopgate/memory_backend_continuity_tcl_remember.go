package loopgate

import (
	"fmt"
	"strings"
	"time"
)

func (backend *continuityTCLMemoryBackend) rememberFactAuthoritatively(authenticatedSession capabilityToken, rawRequest MemoryRememberRequest) (MemoryRememberResponse, error) {
	analyzedCandidate, err := backend.analyzeRememberFactCandidate(authenticatedSession, rawRequest)
	if err != nil {
		return MemoryRememberResponse{}, err
	}
	validatedRequest := analyzedCandidate.ValidatedRequest
	validatedCandidateResult := analyzedCandidate.ValidatedCandidateResult
	validatedCandidate := analyzedCandidate.ValidatedCandidate

	denialCode, safeReason, shouldPersist := memoryRememberGovernanceDecision(validatedCandidate.Decision)
	if !shouldPersist {
		if auditErr := backend.server.logDeniedMemoryRememberCandidate(authenticatedSession.ControlSessionID, validatedRequest, denialCode, validatedCandidate.Decision.ReasonCode, validatedCandidateResult.AuditSummary); auditErr != nil {
			return MemoryRememberResponse{}, continuityGovernanceError{
				httpStatus:     503,
				responseStatus: ResponseStatusError,
				denialCode:     DenialCodeAuditUnavailable,
				reason:         "control-plane audit is unavailable",
			}
		}
		return MemoryRememberResponse{}, continuityGovernanceError{
			httpStatus:     403,
			responseStatus: ResponseStatusDenied,
			denialCode:     denialCode,
			reason:         safeReason,
		}
	}

	rememberedAnchorVersion := strings.TrimSpace(validatedCandidate.AnchorVersion)
	rememberedAnchorKey := strings.TrimSpace(validatedCandidate.AnchorKey)
	backend.server.memoryMu.Lock()
	existingFact, foundExisting := activeExplicitProfileFactByAnchorTuple(backend.partition.state, rememberedAnchorVersion, rememberedAnchorKey)
	backend.server.memoryMu.Unlock()
	if foundExisting && existingFact.FactValue == validatedCandidate.FactValue {
		return MemoryRememberResponse{
			Scope:           validatedRequest.Scope,
			FactKey:         validatedCandidate.CanonicalKey,
			FactValue:       existingFact.FactValue,
			InspectionID:    existingFact.InspectionID,
			DistillateID:    existingFact.DistillateID,
			ResonateKeyID:   existingFact.ResonateKeyID,
			RememberedAtUTC: existingFact.CreatedAtUTC,
			UpdatedExisting: false,
		}, nil
	}

	var rememberResponse MemoryRememberResponse
	// Use the backend's bound tenant rather than trusting the token field again after
	// partition binding has already been validated at the backend seam.
	err = backend.server.mutateContinuityMemory(backend.partition.tenantID, authenticatedSession.ControlSessionID, "memory.fact.remembered", func(workingState continuityMemoryState, nowUTC time.Time) (continuityMemoryState, map[string]interface{}, continuityMutationEvents, error) {
		if err := backend.server.consumeMemoryFactWriteBudgetLocked(authenticatedSession.ControlSessionID, authenticatedSession.PeerIdentity.UID, nowUTC); err != nil {
			return workingState, nil, continuityMutationEvents{}, err
		}

		existingFact, foundExisting := activeExplicitProfileFactByAnchorTuple(workingState, rememberedAnchorVersion, rememberedAnchorKey)
		var existingInspection continuityInspectionRecord
		var foundInspection bool
		if foundExisting {
			existingInspection, foundInspection = workingState.Inspections[existingFact.InspectionID]
			if !foundInspection {
				return workingState, nil, continuityMutationEvents{}, fmt.Errorf("existing remembered fact inspection %q not found", existingFact.InspectionID)
			}
		}

		rememberedThreadSuffix := makeEventPayloadID("memfact", struct {
			FactKey   string `json:"fact_key"`
			FactValue string `json:"fact_value"`
			NowUTC    string `json:"now_utc"`
		}{
			FactKey:   validatedCandidate.CanonicalKey,
			FactValue: validatedCandidate.FactValue,
			NowUTC:    nowUTC.Format(time.RFC3339Nano),
		})
		threadID := "thread_" + rememberedThreadSuffix
		inspectionID := "inspect_" + rememberedThreadSuffix
		distillateID := "dist_" + rememberedThreadSuffix
		resonateKeyID := "rk_" + rememberedThreadSuffix
		if foundExisting {
			existingInspection.Lineage = continuityInspectionLineage{
				Status:       continuityLineageStatusTombstoned,
				ChangedAtUTC: nowUTC.Format(time.RFC3339Nano),
				Reason:       "superseded_by_newer_explicit_profile_fact",
				OperationID: "remember_" + makeEventPayloadID("supersede", struct {
					FactKey string `json:"fact_key"`
					NowUTC  string `json:"now_utc"`
				}{
					FactKey: validatedCandidate.CanonicalKey,
					NowUTC:  nowUTC.Format(time.RFC3339Nano),
				}),
				SupersededByInspectionID:  inspectionID,
				SupersededByDistillateID:  distillateID,
				SupersededByResonateKeyID: resonateKeyID,
			}
			stampContinuityDerivedArtifactsExcluded(&workingState, existingInspection, nowUTC)
			workingState.Inspections[existingFact.InspectionID] = existingInspection
		}
		sourceRef := continuityArtifactSourceRef{
			Kind: explicitProfileFactSourceKind,
			Ref:  validatedCandidate.CanonicalKey,
		}
		userImportance := "somewhat_important"
		retentionScore := importanceBase(backend.server.runtimeConfig, userImportance) + backend.server.runtimeConfig.Memory.Scoring.ExplicitUserBonus
		effectiveHotness := hotnessBase(backend.server.runtimeConfig, userImportance)
		inspectionRecord := continuityInspectionRecord{
			InspectionID:      inspectionID,
			ThreadID:          threadID,
			Scope:             validatedRequest.Scope,
			SubmittedAtUTC:    nowUTC.Format(time.RFC3339Nano),
			CompletedAtUTC:    nowUTC.Format(time.RFC3339Nano),
			Outcome:           continuityInspectionOutcomeDerived,
			DerivationOutcome: continuityInspectionOutcomeDerived,
			Review: continuityInspectionReview{
				Status:         continuityReviewStatusAccepted,
				DecisionSource: continuityReviewDecisionSourceOperator,
				ReviewedAtUTC:  nowUTC.Format(time.RFC3339Nano),
				Reason:         "explicit_profile_fact_write",
				OperationID:    "remember_" + inspectionID,
			},
			Lineage: continuityInspectionLineage{
				Status:                 continuityLineageStatusEligible,
				ChangedAtUTC:           nowUTC.Format(time.RFC3339Nano),
				Reason:                 "explicit_profile_fact_write",
				OperationID:            "remember_" + inspectionID,
				SupersedesInspectionID: strings.TrimSpace(existingFact.InspectionID),
			},
			EventCount:            1,
			ApproxPayloadBytes:    len([]byte(validatedCandidate.FactValue)),
			ApproxPromptTokens:    approximateLoopgateTokenCount(validatedCandidate.FactValue),
			DerivedDistillateIDs:  []string{distillateID},
			DerivedResonateKeyIDs: []string{resonateKeyID},
		}
		distillateRecord := continuityDistillateRecord{
			SchemaVersion:        continuityMemorySchemaVersion,
			DerivationVersion:    continuityDerivationVersion,
			DistillateID:         distillateID,
			InspectionID:         inspectionID,
			ThreadID:             threadID,
			Scope:                validatedRequest.Scope,
			GoalType:             goalTypePreferenceOrConfigUpdate,
			GoalFamilyID:         goalTypePreferenceOrConfigUpdate + ":preference_change",
			NormalizationVersion: continuityNormalizationVersion,
			UserImportance:       userImportance,
			RetentionScore:       retentionScore,
			EffectiveHotness:     effectiveHotness,
			MemoryState:          deriveMemoryState(effectiveHotness, continuityLineageStatusEligible),
			DerivationSignature: makeEventPayloadID("remember_signature", struct {
				FactKey   string `json:"fact_key"`
				FactValue string `json:"fact_value"`
			}{
				FactKey:   validatedCandidate.CanonicalKey,
				FactValue: validatedCandidate.FactValue,
			}),
			CreatedAtUTC: nowUTC.Format(time.RFC3339Nano),
			SourceRefs:   []continuityArtifactSourceRef{sourceRef},
			Tags:         normalizeLoopgateMemoryTags([]string{validatedCandidate.CanonicalKey, validatedCandidate.FactValue}),
			Facts: []continuityDistillateFact{{
				Name:               validatedCandidate.CanonicalKey,
				Value:              validatedCandidate.FactValue,
				SourceRef:          sourceRef.Kind + ":" + sourceRef.Ref,
				EpistemicFlavor:    "remembered",
				CertaintyScore:     certaintyScoreForEpistemicFlavor("remembered"),
				SemanticProjection: cloneSemanticProjection(&validatedCandidate.Projection),
			}},
		}
		resonateKeyRecord := continuityResonateKeyRecord{
			SchemaVersion:     continuityMemorySchemaVersion,
			DerivationVersion: continuityDerivationVersion,
			KeyID:             resonateKeyID,
			DistillateID:      distillateID,
			ThreadID:          threadID,
			Scope:             validatedRequest.Scope,
			GoalType:          distillateRecord.GoalType,
			GoalFamilyID:      distillateRecord.GoalFamilyID,
			RetentionScore:    distillateRecord.RetentionScore,
			EffectiveHotness:  distillateRecord.EffectiveHotness,
			MemoryState:       distillateRecord.MemoryState,
			CreatedAtUTC:      nowUTC.Format(time.RFC3339Nano),
			Tags:              append([]string(nil), distillateRecord.Tags...),
		}

		workingState.Inspections[inspectionID] = inspectionRecord
		workingState.Distillates[distillateID] = distillateRecord
		workingState.ResonateKeys[resonateKeyID] = resonateKeyRecord

		rememberResponse = MemoryRememberResponse{
			Scope:           validatedRequest.Scope,
			FactKey:         validatedCandidate.CanonicalKey,
			FactValue:       validatedCandidate.FactValue,
			InspectionID:    inspectionID,
			DistillateID:    distillateID,
			ResonateKeyID:   resonateKeyID,
			RememberedAtUTC: nowUTC.Format(time.RFC3339Nano),
			UpdatedExisting: foundExisting,
		}
		if foundExisting {
			rememberResponse.SupersededFactValue = existingFact.FactValue
		}

		mutationEvents := continuityMutationEvents{
			Continuity: []continuityAuthoritativeEvent{{
				SchemaVersion: continuityMemorySchemaVersion,
				EventID:       "memory_fact_" + inspectionID,
				EventType:     "memory_fact_remembered",
				CreatedAtUTC:  nowUTC.Format(time.RFC3339Nano),
				Actor:         authenticatedSession.ControlSessionID,
				Scope:         validatedRequest.Scope,
				InspectionID:  inspectionID,
				ThreadID:      threadID,
				GoalType:      distillateRecord.GoalType,
				GoalFamilyID:  distillateRecord.GoalFamilyID,
				Inspection:    ptrContinuityInspectionRecord(cloneContinuityInspectionRecord(inspectionRecord)),
				Distillate:    ptrContinuityDistillateRecord(cloneContinuityDistillateRecord(distillateRecord)),
				ResonateKey:   ptrContinuityResonateKeyRecord(cloneContinuityResonateKeyRecord(resonateKeyRecord)),
			}},
		}
		if foundExisting {
			existingInspection := workingState.Inspections[existingFact.InspectionID]
			mutationEvents.Continuity = append(mutationEvents.Continuity, continuityAuthoritativeEvent{
				SchemaVersion: continuityMemorySchemaVersion,
				EventID:       "memory_fact_supersede_" + existingFact.InspectionID,
				EventType:     "continuity_inspection_lineage_updated",
				CreatedAtUTC:  nowUTC.Format(time.RFC3339Nano),
				Actor:         authenticatedSession.ControlSessionID,
				Scope:         existingInspection.Scope,
				InspectionID:  existingInspection.InspectionID,
				ThreadID:      existingInspection.ThreadID,
				GoalType:      distillateRecord.GoalType,
				GoalFamilyID:  distillateRecord.GoalFamilyID,
				Lineage:       ptrContinuityInspectionLineage(existingInspection.Lineage),
			})
		}
		return workingState, mergeMemoryTCLAuditSummary(map[string]interface{}{
			"fact_key":         validatedCandidate.CanonicalKey,
			"inspection_id":    inspectionID,
			"distillate_id":    distillateID,
			"resonate_key_id":  resonateKeyID,
			"updated_existing": foundExisting,
			"scope":            validatedRequest.Scope,
		}, validatedCandidateResult.AuditSummary), mutationEvents, nil
	})
	if err != nil {
		return MemoryRememberResponse{}, err
	}
	return rememberResponse, nil
}

func (backend *continuityTCLMemoryBackend) ensureAuthenticatedSessionMatchesBoundPartition(authenticatedSession capabilityToken) error {
	if backend.partition == nil {
		return fmt.Errorf("memory backend partition is not bound")
	}
	if memoryPartitionKey(authenticatedSession.TenantID) != backend.partition.partitionKey {
		return fmt.Errorf("authenticated session tenant does not match memory backend partition")
	}
	return nil
}
