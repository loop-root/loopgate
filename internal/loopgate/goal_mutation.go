package loopgate

import (
	"time"

	tclpkg "morph/internal/tcl"
)

func (server *Server) setGoal(tokenClaims capabilityToken, text string) (string, error) {
	var goalID string
	err := server.mutateContinuityMemory(tokenClaims.TenantID, tokenClaims.ControlSessionID, "memory.goal.opened", func(workingState continuityMemoryState, nowUTC time.Time) (continuityMemoryState, map[string]interface{}, continuityMutationEvents, error) {
		if err := server.consumeMemoryFactWriteBudgetLocked(tokenClaims.ControlSessionID, tokenClaims.PeerIdentity.UID, nowUTC); err != nil {
			return workingState, nil, continuityMutationEvents{}, err
		}

		suffix := makeEventPayloadID("goal_open", struct {
			Text   string `json:"text"`
			NowUTC string `json:"now_utc"`
		}{Text: text, NowUTC: nowUTC.Format(time.RFC3339Nano)})

		goalID = "goal_" + suffix
		inspectionID := "inspect_" + suffix
		distillateID := "dist_" + suffix
		resonateKeyID := "rk_" + suffix
		threadID := "thread_" + suffix
		userImportance := "somewhat_important"
		retentionScore := importanceBase(server.runtimeConfig, userImportance) + server.runtimeConfig.Memory.Scoring.ExplicitUserBonus
		effectiveHotness := hotnessBase(server.runtimeConfig, userImportance)

		inspectionRecord := continuityInspectionRecord{
			InspectionID:      inspectionID,
			ThreadID:          threadID,
			Scope:             memoryScopeGlobal,
			SubmittedAtUTC:    nowUTC.Format(time.RFC3339Nano),
			CompletedAtUTC:    nowUTC.Format(time.RFC3339Nano),
			Outcome:           continuityInspectionOutcomeDerived,
			DerivationOutcome: continuityInspectionOutcomeDerived,
			Review: continuityInspectionReview{
				Status:         continuityReviewStatusAccepted,
				DecisionSource: continuityReviewDecisionSourceOperator,
				ReviewedAtUTC:  nowUTC.Format(time.RFC3339Nano),
				Reason:         "goal_set",
				OperationID:    "goal_set_" + inspectionID,
			},
			Lineage: continuityInspectionLineage{
				Status:       continuityLineageStatusEligible,
				ChangedAtUTC: nowUTC.Format(time.RFC3339Nano),
				Reason:       "goal_set",
				OperationID:  "goal_set_" + inspectionID,
			},
			EventCount:            1,
			ApproxPayloadBytes:    len([]byte(text)),
			ApproxPromptTokens:    approximateLoopgateTokenCount(text),
			DerivedDistillateIDs:  []string{distillateID},
			DerivedResonateKeyIDs: []string{resonateKeyID},
		}
		distillateRecord := continuityDistillateRecord{
			SchemaVersion:        continuityMemorySchemaVersion,
			DerivationVersion:    continuityDerivationVersion,
			DistillateID:         distillateID,
			InspectionID:         inspectionID,
			ThreadID:             threadID,
			Scope:                memoryScopeGlobal,
			GoalType:             goalTypeWorkflowFollowup,
			GoalFamilyID:         goalTypeWorkflowFollowup + ":goal",
			NormalizationVersion: continuityNormalizationVersion,
			UserImportance:       userImportance,
			RetentionScore:       retentionScore,
			EffectiveHotness:     effectiveHotness,
			MemoryState:          deriveMemoryState(effectiveHotness, continuityLineageStatusEligible),
			DerivationSignature: makeEventPayloadID("goal_open_signature", struct {
				GoalID string `json:"goal_id"`
				Text   string `json:"text"`
			}{GoalID: goalID, Text: text}),
			CreatedAtUTC: nowUTC.Format(time.RFC3339Nano),
			Tags:         normalizeLoopgateMemoryTags([]string{"goal", goalID, text}),
			GoalOps: []continuityGoalOp{{
				GoalID:             goalID,
				Text:               text,
				Action:             "opened",
				SemanticProjection: deriveGoalOpSemanticProjection("opened", text, memorySourceChannelCapability, tclpkg.TrustSystemDerived),
			}},
		}
		resonateKeyRecord := continuityResonateKeyRecord{
			SchemaVersion:     continuityMemorySchemaVersion,
			DerivationVersion: continuityDerivationVersion,
			KeyID:             resonateKeyID,
			DistillateID:      distillateID,
			ThreadID:          threadID,
			Scope:             memoryScopeGlobal,
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
		return workingState, map[string]interface{}{"goal_id": goalID, "text": text}, continuityMutationEvents{}, nil
	})
	return goalID, err
}

func (server *Server) closeGoal(tokenClaims capabilityToken, goalID string) error {
	return server.mutateContinuityMemory(tokenClaims.TenantID, tokenClaims.ControlSessionID, "memory.goal.closed", func(workingState continuityMemoryState, nowUTC time.Time) (continuityMemoryState, map[string]interface{}, continuityMutationEvents, error) {
		suffix := makeEventPayloadID("goal_close", struct {
			GoalID string `json:"goal_id"`
			NowUTC string `json:"now_utc"`
		}{GoalID: goalID, NowUTC: nowUTC.Format(time.RFC3339Nano)})

		inspectionID := "inspect_" + suffix
		distillateID := "dist_" + suffix
		resonateKeyID := "rk_" + suffix
		threadID := "thread_" + suffix
		userImportance := "somewhat_important"
		retentionScore := importanceBase(server.runtimeConfig, userImportance)
		effectiveHotness := hotnessBase(server.runtimeConfig, userImportance)

		inspectionRecord := continuityInspectionRecord{
			InspectionID:      inspectionID,
			ThreadID:          threadID,
			Scope:             memoryScopeGlobal,
			SubmittedAtUTC:    nowUTC.Format(time.RFC3339Nano),
			CompletedAtUTC:    nowUTC.Format(time.RFC3339Nano),
			Outcome:           continuityInspectionOutcomeDerived,
			DerivationOutcome: continuityInspectionOutcomeDerived,
			Review: continuityInspectionReview{
				Status:         continuityReviewStatusAccepted,
				DecisionSource: continuityReviewDecisionSourceOperator,
				ReviewedAtUTC:  nowUTC.Format(time.RFC3339Nano),
				Reason:         "goal_close",
				OperationID:    "goal_close_" + inspectionID,
			},
			Lineage: continuityInspectionLineage{
				Status:       continuityLineageStatusEligible,
				ChangedAtUTC: nowUTC.Format(time.RFC3339Nano),
				Reason:       "goal_close",
				OperationID:  "goal_close_" + inspectionID,
			},
			EventCount:            1,
			ApproxPayloadBytes:    len(goalID),
			ApproxPromptTokens:    approximateLoopgateTokenCount(goalID),
			DerivedDistillateIDs:  []string{distillateID},
			DerivedResonateKeyIDs: []string{resonateKeyID},
		}
		distillateRecord := continuityDistillateRecord{
			SchemaVersion:        continuityMemorySchemaVersion,
			DerivationVersion:    continuityDerivationVersion,
			DistillateID:         distillateID,
			InspectionID:         inspectionID,
			ThreadID:             threadID,
			Scope:                memoryScopeGlobal,
			GoalType:             goalTypeWorkflowFollowup,
			GoalFamilyID:         goalTypeWorkflowFollowup + ":goal",
			NormalizationVersion: continuityNormalizationVersion,
			UserImportance:       userImportance,
			RetentionScore:       retentionScore,
			EffectiveHotness:     effectiveHotness,
			MemoryState:          deriveMemoryState(effectiveHotness, continuityLineageStatusEligible),
			DerivationSignature: makeEventPayloadID("goal_close_signature", struct {
				GoalID string `json:"goal_id"`
			}{GoalID: goalID}),
			CreatedAtUTC: nowUTC.Format(time.RFC3339Nano),
			Tags:         normalizeLoopgateMemoryTags([]string{"goal", goalID}),
			GoalOps: []continuityGoalOp{{
				GoalID:             goalID,
				Action:             "closed",
				SemanticProjection: deriveGoalOpSemanticProjection("closed", "", memorySourceChannelCapability, tclpkg.TrustSystemDerived),
			}},
		}
		resonateKeyRecord := continuityResonateKeyRecord{
			SchemaVersion:     continuityMemorySchemaVersion,
			DerivationVersion: continuityDerivationVersion,
			KeyID:             resonateKeyID,
			DistillateID:      distillateID,
			ThreadID:          threadID,
			Scope:             memoryScopeGlobal,
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
		return workingState, map[string]interface{}{"goal_id": goalID}, continuityMutationEvents{}, nil
	})
}
