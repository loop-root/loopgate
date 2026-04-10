package loopgate

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tclpkg "morph/internal/tcl"
)

func (backend *continuityTCLMemoryBackend) inspectContinuityAuthoritatively(authenticatedSession capabilityToken, rawRequest ContinuityInspectRequest) (ContinuityInspectResponse, error) {
	validatedRequest, err := normalizeContinuityInspectRequest(rawRequest)
	if err != nil {
		return ContinuityInspectResponse{}, err
	}
	if err := validateContinuityInspectProvenance(authenticatedSession, validatedRequest); err != nil {
		return ContinuityInspectResponse{}, err
	}
	observedPacket := buildObservedContinuityPacket(validatedRequest)

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
			derivedDistillate = backend.deriveContinuityDistillate(observedPacket, inspectionRecord, nowUTC)
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
				SchemaVersion:  continuityMemorySchemaVersion,
				EventID:        "continuity_inspection_" + inspectionRecord.InspectionID,
				EventType:      "continuity_inspection_recorded",
				CreatedAtUTC:   nowUTC.Format(time.RFC3339Nano),
				Actor:          authenticatedSession.ControlSessionID,
				Scope:          inspectionRecord.Scope,
				InspectionID:   inspectionRecord.InspectionID,
				ThreadID:       inspectionRecord.ThreadID,
				GoalType:       derivedDistillate.GoalType,
				GoalFamilyID:   derivedDistillate.GoalFamilyID,
				ObservedPacket: ptrContinuityObservedPacket(cloneContinuityObservedPacket(observedPacket)),
				Inspection:     ptrContinuityInspectionRecord(cloneContinuityInspectionRecord(inspectionRecord)),
				Distillate:     ptrContinuityDistillateRecord(cloneContinuityDistillateRecord(derivedDistillate)),
				ResonateKey:    ptrContinuityResonateKeyRecord(cloneContinuityResonateKeyRecord(derivedResonateKey)),
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

func buildObservedContinuityPacket(validatedRequest ContinuityInspectRequest) continuityObservedPacket {
	observedPacket := continuityObservedPacket{
		ThreadID:    validatedRequest.ThreadID,
		Scope:       validatedRequest.Scope,
		SealedAtUTC: validatedRequest.SealedAtUTC,
		Tags:        normalizeLoopgateMemoryTags(validatedRequest.Tags),
		Events:      make([]continuityObservedEventRecord, 0, len(validatedRequest.Events)),
	}
	for _, rawEvent := range validatedRequest.Events {
		observedPacket.Events = append(observedPacket.Events, buildObservedContinuityEventRecord(rawEvent))
	}
	return observedPacket
}

func buildObservedContinuityEventRecord(rawEvent ContinuityEventInput) continuityObservedEventRecord {
	observedEvent := continuityObservedEventRecord{
		TimestampUTC:    rawEvent.TimestampUTC,
		SessionID:       strings.TrimSpace(rawEvent.SessionID),
		Type:            strings.TrimSpace(rawEvent.Type),
		Scope:           strings.TrimSpace(rawEvent.Scope),
		ThreadID:        strings.TrimSpace(rawEvent.ThreadID),
		EpistemicFlavor: strings.TrimSpace(rawEvent.EpistemicFlavor),
		LedgerSequence:  rawEvent.LedgerSequence,
		EventHash:       strings.TrimSpace(rawEvent.EventHash),
	}
	if len(rawEvent.SourceRefs) != 0 {
		observedEvent.SourceRefs = make([]continuityArtifactSourceRef, 0, len(rawEvent.SourceRefs))
		for _, rawSourceRef := range rawEvent.SourceRefs {
			observedEvent.SourceRefs = append(observedEvent.SourceRefs, continuityArtifactSourceRef{
				Kind:   strings.TrimSpace(rawSourceRef.Kind),
				Ref:    strings.TrimSpace(rawSourceRef.Ref),
				SHA256: strings.TrimSpace(rawSourceRef.SHA256),
			})
		}
	}
	if observedPayload := buildObservedContinuityEventPayload(rawEvent.Payload); observedPayload != nil {
		observedEvent.Payload = observedPayload
	}
	return observedEvent
}

func buildObservedContinuityEventPayload(rawPayload map[string]interface{}) *continuityObservedEventPayload {
	if len(rawPayload) == 0 {
		return nil
	}
	observedPayload := continuityObservedEventPayload{
		Text:              continuityPayloadStringField(rawPayload, "text"),
		Output:            continuityPayloadStringField(rawPayload, "output"),
		GoalID:            continuityPayloadStringField(rawPayload, "goal_id"),
		ItemID:            continuityPayloadStringField(rawPayload, "item_id"),
		Capability:        continuityPayloadStringField(rawPayload, "capability"),
		Status:            continuityPayloadStringField(rawPayload, "status"),
		Reason:            continuityPayloadStringField(rawPayload, "reason"),
		DenialCode:        continuityPayloadStringField(rawPayload, "denial_code"),
		CallID:            continuityPayloadStringField(rawPayload, "call_id"),
		ApprovalRequestID: continuityPayloadStringField(rawPayload, "approval_request_id"),
	}
	if rawFacts, ok := rawPayload["facts"].(map[string]interface{}); ok && len(rawFacts) != 0 {
		factNames := make([]string, 0, len(rawFacts))
		for factName := range rawFacts {
			factNames = append(factNames, factName)
		}
		sort.Strings(factNames)
		for _, factName := range factNames {
			normalizedFactName := strings.TrimSpace(factName)
			if normalizedFactName == "" {
				continue
			}
			normalizedFactValue, ok := normalizeContinuityFactValueForPersistence(rawFacts[factName])
			if !ok {
				continue
			}
			observedPayload.Facts = append(observedPayload.Facts, continuityObservedFactRecord{
				Name:  normalizedFactName,
				Value: normalizedFactValue,
			})
		}
	}
	if strings.TrimSpace(observedPayload.Text) == "" &&
		strings.TrimSpace(observedPayload.Output) == "" &&
		strings.TrimSpace(observedPayload.GoalID) == "" &&
		strings.TrimSpace(observedPayload.ItemID) == "" &&
		strings.TrimSpace(observedPayload.Capability) == "" &&
		strings.TrimSpace(observedPayload.Status) == "" &&
		strings.TrimSpace(observedPayload.Reason) == "" &&
		strings.TrimSpace(observedPayload.DenialCode) == "" &&
		strings.TrimSpace(observedPayload.CallID) == "" &&
		strings.TrimSpace(observedPayload.ApprovalRequestID) == "" &&
		len(observedPayload.Facts) == 0 {
		return nil
	}
	return &observedPayload
}

func continuityPayloadStringField(rawPayload map[string]interface{}, fieldName string) string {
	fieldValue, _ := rawPayload[fieldName].(string)
	return strings.TrimSpace(fieldValue)
}

func (backend *continuityTCLMemoryBackend) deriveContinuityDistillate(observedPacket continuityObservedPacket, inspectionRecord continuityInspectionRecord, now time.Time) continuityDistillateRecord {
	distillateID := "dist_" + strings.TrimPrefix(observedPacket.ThreadID, "thread_")
	distillateRecord := continuityDistillateRecord{
		SchemaVersion:       continuityMemorySchemaVersion,
		DerivationVersion:   continuityDerivationVersion,
		DistillateID:        distillateID,
		InspectionID:        inspectionRecord.InspectionID,
		ThreadID:            observedPacket.ThreadID,
		Scope:               observedPacket.Scope,
		UserImportance:      "somewhat_important",
		CreatedAtUTC:        now.UTC().Format(time.RFC3339Nano),
		Tags:                append([]string(nil), observedPacket.Tags...),
		DerivationSignature: buildDerivationSignature(observedPacket),
	}

	discoveredTags := make(map[string]struct{}, len(observedPacket.Tags))
	for _, initialTag := range observedPacket.Tags {
		discoveredTags[initialTag] = struct{}{}
	}
	sourceRefSeen := map[string]struct{}{}
	for _, continuityEvent := range observedPacket.Events {
		eventSourceRef := continuityArtifactSourceRef{
			Kind:   "morph_ledger_event",
			Ref:    fmt.Sprintf("ledger_sequence:%d", continuityEvent.LedgerSequence),
			SHA256: continuityEvent.EventHash,
		}
		if _, seen := sourceRefSeen[eventSourceRef.Ref]; !seen {
			sourceRefSeen[eventSourceRef.Ref] = struct{}{}
			distillateRecord.SourceRefs = append(distillateRecord.SourceRefs, eventSourceRef)
		}
		switch continuityEvent.Type {
		case "provider_fact_observed":
			for _, observedFact := range continuityEvent.payloadFacts() {
				derivedFact, ok := backend.deriveContinuityDistillateFact(eventSourceRef.Ref, continuityEvent, observedFact)
				if !ok {
					continue
				}
				distillateRecord.Facts = append(distillateRecord.Facts, derivedFact)
				recordLoopgateMemoryTags(discoveredTags, derivedFact.Name, fmt.Sprint(derivedFact.Value))
			}
		case "goal_opened":
			goalID := continuityEvent.payloadGoalID()
			goalText := continuityEvent.payloadText()
			if strings.TrimSpace(goalID) != "" {
				distillateRecord.GoalOps = append(distillateRecord.GoalOps, continuityGoalOp{
					GoalID:             strings.TrimSpace(goalID),
					Text:               strings.TrimSpace(goalText),
					Action:             "opened",
					SemanticProjection: deriveGoalOpSemanticProjection("opened", strings.TrimSpace(goalText), "continuity_inspection", tclpkg.TrustInferred),
				})
				if distillateRecord.GoalFamilyID == "" {
					goalNormalization := normalizeGoalFamily(goalText, backend.server.goalAliases)
					distillateRecord.GoalType = goalNormalization.GoalType
					distillateRecord.GoalFamilyID = goalNormalization.GoalFamilyID
					distillateRecord.NormalizationVersion = goalNormalization.NormalizationVersion
				}
				recordLoopgateMemoryTags(discoveredTags, goalID, goalText)
			}
		case "goal_closed":
			goalID := continuityEvent.payloadGoalID()
			if strings.TrimSpace(goalID) != "" {
				distillateRecord.GoalOps = append(distillateRecord.GoalOps, continuityGoalOp{
					GoalID:             strings.TrimSpace(goalID),
					Action:             "closed",
					SemanticProjection: deriveGoalOpSemanticProjection("closed", "", "continuity_inspection", tclpkg.TrustInferred),
				})
				recordLoopgateMemoryTags(discoveredTags, goalID)
			}
		case "unresolved_item_opened":
			itemID := continuityEvent.payloadItemID()
			itemText := continuityEvent.payloadText()
			if strings.TrimSpace(itemID) != "" {
				distillateRecord.UnresolvedItemOps = append(distillateRecord.UnresolvedItemOps, continuityUnresolvedItemOp{
					ItemID:             strings.TrimSpace(itemID),
					Text:               strings.TrimSpace(itemText),
					Action:             "opened",
					SemanticProjection: deriveUnresolvedItemOpSemanticProjection("opened", "", strings.TrimSpace(itemText), "continuity_inspection", tclpkg.TrustInferred),
				})
				recordLoopgateMemoryTags(discoveredTags, itemID, itemText)
			}
		case "unresolved_item_resolved":
			itemID := continuityEvent.payloadItemID()
			if strings.TrimSpace(itemID) != "" {
				distillateRecord.UnresolvedItemOps = append(distillateRecord.UnresolvedItemOps, continuityUnresolvedItemOp{
					ItemID:             strings.TrimSpace(itemID),
					Action:             "closed",
					SemanticProjection: deriveUnresolvedItemOpSemanticProjection("closed", "", "", "continuity_inspection", tclpkg.TrustInferred),
				})
				recordLoopgateMemoryTags(discoveredTags, itemID)
			}
		}
	}

	sort.Slice(distillateRecord.Facts, func(leftIndex int, rightIndex int) bool {
		return distillateRecord.Facts[leftIndex].Name < distillateRecord.Facts[rightIndex].Name
	})
	distillateRecord.Tags = normalizeLoopgateMemoryTags(append([]string(nil), normalizedLoopgateTagSet(discoveredTags)...))
	if distillateRecord.GoalType == "" {
		goalNormalization := normalizeGoalFamily(strings.Join(distillateRecord.Tags, " "), backend.server.goalAliases)
		distillateRecord.GoalType = goalNormalization.GoalType
		distillateRecord.GoalFamilyID = goalNormalization.GoalFamilyID
		distillateRecord.NormalizationVersion = goalNormalization.NormalizationVersion
	}
	distillateRecord.RetentionScore = importanceBase(backend.server.runtimeConfig, distillateRecord.UserImportance) + backend.server.runtimeConfig.Memory.Scoring.ApprovedGoalAnchor
	distillateRecord.EffectiveHotness = hotnessBase(backend.server.runtimeConfig, distillateRecord.UserImportance)
	distillateRecord.MemoryState = deriveMemoryState(distillateRecord.EffectiveHotness, continuityLineageStatusEligible)
	return distillateRecord
}

func (continuityEvent continuityObservedEventRecord) payloadFacts() []continuityObservedFactRecord {
	if continuityEvent.Payload == nil {
		return nil
	}
	return append([]continuityObservedFactRecord(nil), continuityEvent.Payload.Facts...)
}

func (continuityEvent continuityObservedEventRecord) payloadGoalID() string {
	if continuityEvent.Payload == nil {
		return ""
	}
	return strings.TrimSpace(continuityEvent.Payload.GoalID)
}

func (continuityEvent continuityObservedEventRecord) payloadItemID() string {
	if continuityEvent.Payload == nil {
		return ""
	}
	return strings.TrimSpace(continuityEvent.Payload.ItemID)
}

func (continuityEvent continuityObservedEventRecord) payloadText() string {
	if continuityEvent.Payload == nil {
		return ""
	}
	return strings.TrimSpace(continuityEvent.Payload.Text)
}

func (backend *continuityTCLMemoryBackend) deriveContinuityDistillateFact(eventSourceRef string, continuityEvent continuityObservedEventRecord, observedFact continuityObservedFactRecord) (continuityDistillateFact, bool) {
	analyzedCandidate, ok := backend.analyzeContinuityFactCandidate(observedFact.Name, observedFact.Value)
	if !ok {
		return continuityDistillateFact{}, false
	}

	return continuityDistillateFact{
		Name:               analyzedCandidate.CanonicalFactKey,
		Value:              analyzedCandidate.CanonicalFactValue,
		SourceRef:          eventSourceRef,
		EpistemicFlavor:    continuityEvent.EpistemicFlavor,
		CertaintyScore:     certaintyScoreForEpistemicFlavor(continuityEvent.EpistemicFlavor),
		SemanticProjection: analyzedCandidate.SemanticProjection,
	}, true
}
