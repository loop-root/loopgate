package loopgate

import (
	"fmt"
	"strings"
	"time"

	"morph/internal/identifiers"
	"morph/internal/secrets"
	tclpkg "morph/internal/tcl"
)

const explicitTodoSourceKind = "explicit_todo_item"

const (
	todoItemOpStatusSet = "status_set"

	explicitTodoWorkflowStatusTodo       = "todo"
	explicitTodoWorkflowStatusInProgress = "in_progress"
	explicitTodoWorkflowStatusDone       = "done"

	maxUIRecentCompletedTodoItems = 20
)

const (
	taskKindCarryOver = "carry_over"
	taskKindOneOff    = "one_off"
	taskKindScheduled = "scheduled"

	taskSourceUser         = "user"
	taskSourceContinuity   = "continuity"
	taskFactKind           = "task.kind"
	taskFactSourceKind     = "task.source_kind"
	taskFactNextStep       = "task.next_step"
	taskFactScheduledForUT = "task.scheduled_for_utc"
	taskFactExecutionClass = "task.execution_class"
)

type explicitTodoItemRecord struct {
	InspectionID    string
	DistillateID    string
	ResonateKeyID   string
	ItemID          string
	Text            string
	TaskKind        string
	SourceKind      string
	NextStep        string
	ScheduledForUTC string
	ExecutionClass  string
	CreatedAtUTC    string
	// Status is "todo" or "in_progress" for open items (default "todo").
	Status string
}

func (server *Server) addTodoItem(tokenClaims capabilityToken, rawRequest TodoAddRequest) (TodoAddResponse, error) {
	validatedRequest, err := server.normalizeTodoAddRequest(rawRequest)
	if err != nil {
		return TodoAddResponse{}, err
	}

	server.memoryMu.Lock()
	partition, partitionErr := server.ensureMemoryPartitionLocked(tokenClaims.TenantID)
	if partitionErr != nil {
		server.memoryMu.Unlock()
		return TodoAddResponse{}, partitionErr
	}
	existingTodoItem, foundExisting := activeExplicitTodoItemByText(partition.state, validatedRequest.Text)
	server.memoryMu.Unlock()
	if foundExisting {
		return TodoAddResponse{
			Scope:           validatedRequest.Scope,
			ItemID:          existingTodoItem.ItemID,
			Text:            existingTodoItem.Text,
			TaskKind:        existingTodoItem.TaskKind,
			SourceKind:      existingTodoItem.SourceKind,
			NextStep:        existingTodoItem.NextStep,
			ScheduledForUTC: existingTodoItem.ScheduledForUTC,
			ExecutionClass:  existingTodoItem.ExecutionClass,
			InspectionID:    existingTodoItem.InspectionID,
			DistillateID:    existingTodoItem.DistillateID,
			ResonateKeyID:   existingTodoItem.ResonateKeyID,
			AddedAtUTC:      existingTodoItem.CreatedAtUTC,
			AlreadyPresent:  true,
		}, nil
	}

	var addResponse TodoAddResponse
	err = server.mutateContinuityMemory(tokenClaims.TenantID, tokenClaims.ControlSessionID, "memory.todo.added", func(workingState continuityMemoryState, nowUTC time.Time) (continuityMemoryState, map[string]interface{}, continuityMutationEvents, error) {
		if err := server.consumeMemoryFactWriteBudgetLocked(tokenClaims.ControlSessionID, tokenClaims.PeerIdentity.UID, nowUTC); err != nil {
			return workingState, nil, continuityMutationEvents{}, err
		}

		existingTodo, foundExisting := activeExplicitTodoItemByText(workingState, validatedRequest.Text)
		if foundExisting {
			addResponse = TodoAddResponse{
				Scope:           validatedRequest.Scope,
				ItemID:          existingTodo.ItemID,
				Text:            existingTodo.Text,
				TaskKind:        existingTodo.TaskKind,
				SourceKind:      existingTodo.SourceKind,
				NextStep:        existingTodo.NextStep,
				ScheduledForUTC: existingTodo.ScheduledForUTC,
				ExecutionClass:  existingTodo.ExecutionClass,
				InspectionID:    existingTodo.InspectionID,
				DistillateID:    existingTodo.DistillateID,
				ResonateKeyID:   existingTodo.ResonateKeyID,
				AddedAtUTC:      existingTodo.CreatedAtUTC,
				AlreadyPresent:  true,
			}
			return workingState, nil, continuityMutationEvents{}, nil
		}

		todoSuffix := makeEventPayloadID("todo_open", struct {
			Text   string `json:"text"`
			NowUTC string `json:"now_utc"`
		}{
			Text:   validatedRequest.Text,
			NowUTC: nowUTC.Format(time.RFC3339Nano),
		})
		itemID := "todo_" + todoSuffix
		threadID := "thread_" + todoSuffix
		inspectionID := "inspect_" + todoSuffix
		distillateID := "dist_" + todoSuffix
		resonateKeyID := "rk_" + todoSuffix
		sourceRef := continuityArtifactSourceRef{
			Kind: explicitTodoSourceKind,
			Ref:  itemID,
		}
		userImportance := "somewhat_important"
		retentionScore := importanceBase(server.runtimeConfig, userImportance) + server.runtimeConfig.Memory.Scoring.ExplicitUserBonus
		effectiveHotness := hotnessBase(server.runtimeConfig, userImportance)
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
				Reason:         "explicit_todo_add",
				OperationID:    "todo_add_" + inspectionID,
			},
			Lineage: continuityInspectionLineage{
				Status:       continuityLineageStatusEligible,
				ChangedAtUTC: nowUTC.Format(time.RFC3339Nano),
				Reason:       "explicit_todo_add",
				OperationID:  "todo_add_" + inspectionID,
			},
			EventCount:            1,
			ApproxPayloadBytes:    len([]byte(validatedRequest.Text)),
			ApproxPromptTokens:    approximateLoopgateTokenCount(validatedRequest.Text),
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
			GoalType:             goalTypeWorkflowFollowup,
			GoalFamilyID:         goalTypeWorkflowFollowup + ":explicit_todo",
			NormalizationVersion: continuityNormalizationVersion,
			UserImportance:       userImportance,
			RetentionScore:       retentionScore,
			EffectiveHotness:     effectiveHotness,
			MemoryState:          deriveMemoryState(effectiveHotness, continuityLineageStatusEligible),
			DerivationSignature: makeEventPayloadID("todo_open_signature", struct {
				ItemID string `json:"item_id"`
				Text   string `json:"text"`
			}{
				ItemID: itemID,
				Text:   validatedRequest.Text,
			}),
			CreatedAtUTC: nowUTC.Format(time.RFC3339Nano),
			SourceRefs:   []continuityArtifactSourceRef{sourceRef},
			Tags: normalizeLoopgateMemoryTags([]string{
				"todo",
				"task",
				itemID,
				validatedRequest.Text,
				validatedRequest.TaskKind,
				validatedRequest.SourceKind,
			}),
			Facts: server.buildExplicitTodoTaskFacts(itemID, validatedRequest),
			UnresolvedItemOps: []continuityUnresolvedItemOp{{
				ItemID:             itemID,
				Text:               validatedRequest.Text,
				Action:             "opened",
				SemanticProjection: deriveUnresolvedItemOpSemanticProjection("opened", "", validatedRequest.Text, memorySourceChannelCapability, tclpkg.TrustSystemDerived),
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

		addResponse = TodoAddResponse{
			Scope:           validatedRequest.Scope,
			ItemID:          itemID,
			Text:            validatedRequest.Text,
			TaskKind:        validatedRequest.TaskKind,
			SourceKind:      validatedRequest.SourceKind,
			NextStep:        validatedRequest.NextStep,
			ScheduledForUTC: validatedRequest.ScheduledForUTC,
			ExecutionClass:  validatedRequest.ExecutionClass,
			InspectionID:    inspectionID,
			DistillateID:    distillateID,
			ResonateKeyID:   resonateKeyID,
			AddedAtUTC:      nowUTC.Format(time.RFC3339Nano),
			AlreadyPresent:  false,
		}

		mutationEvents := continuityMutationEvents{
			Continuity: []continuityAuthoritativeEvent{{
				SchemaVersion: continuityMemorySchemaVersion,
				EventID:       "todo_add_" + inspectionID,
				EventType:     "todo_item_added",
				CreatedAtUTC:  nowUTC.Format(time.RFC3339Nano),
				Actor:         tokenClaims.ControlSessionID,
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
		return workingState, map[string]interface{}{
			"item_id":         itemID,
			"inspection_id":   inspectionID,
			"distillate_id":   distillateID,
			"resonate_key_id": resonateKeyID,
			"scope":           validatedRequest.Scope,
			"text":            secrets.RedactText(validatedRequest.Text),
		}, mutationEvents, nil
	})
	if err != nil {
		return TodoAddResponse{}, err
	}
	return addResponse, nil
}

func (server *Server) completeTodoItem(tokenClaims capabilityToken, rawRequest TodoCompleteRequest) (TodoCompleteResponse, error) {
	validatedRequest, err := server.normalizeTodoCompleteRequest(rawRequest)
	if err != nil {
		return TodoCompleteResponse{}, err
	}

	var completeResponse TodoCompleteResponse
	err = server.mutateContinuityMemory(tokenClaims.TenantID, tokenClaims.ControlSessionID, "memory.todo.completed", func(workingState continuityMemoryState, nowUTC time.Time) (continuityMemoryState, map[string]interface{}, continuityMutationEvents, error) {
		if err := server.consumeMemoryFactWriteBudgetLocked(tokenClaims.ControlSessionID, tokenClaims.PeerIdentity.UID, nowUTC); err != nil {
			return workingState, nil, continuityMutationEvents{}, err
		}

		activeTodoItem, found := activeExplicitTodoItemByID(workingState, validatedRequest.ItemID)
		if !found {
			return workingState, nil, continuityMutationEvents{}, continuityGovernanceError{
				httpStatus:     404,
				responseStatus: ResponseStatusDenied,
				denialCode:     DenialCodeTodoItemNotFound,
				reason:         "todo item not found",
			}
		}
		existingOpenInspection, found := workingState.Inspections[activeTodoItem.InspectionID]
		if !found {
			return workingState, nil, continuityMutationEvents{}, fmt.Errorf("active todo inspection %q not found", activeTodoItem.InspectionID)
		}

		todoSuffix := makeEventPayloadID("todo_close", struct {
			ItemID string `json:"item_id"`
			NowUTC string `json:"now_utc"`
		}{
			ItemID: validatedRequest.ItemID,
			NowUTC: nowUTC.Format(time.RFC3339Nano),
		})
		threadID := "thread_" + todoSuffix
		inspectionID := "inspect_" + todoSuffix
		distillateID := "dist_" + todoSuffix
		sourceRef := continuityArtifactSourceRef{
			Kind: explicitTodoSourceKind,
			Ref:  validatedRequest.ItemID,
		}
		userImportance := "somewhat_important"
		retentionScore := importanceBase(server.runtimeConfig, userImportance) + server.runtimeConfig.Memory.Scoring.ExplicitUserBonus
		effectiveHotness := hotnessBase(server.runtimeConfig, userImportance)
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
				Reason:         "explicit_todo_complete",
				OperationID:    "todo_complete_" + inspectionID,
			},
			Lineage: continuityInspectionLineage{
				Status:                 continuityLineageStatusEligible,
				ChangedAtUTC:           nowUTC.Format(time.RFC3339Nano),
				Reason:                 "explicit_todo_complete",
				OperationID:            "todo_complete_" + inspectionID,
				SupersedesInspectionID: activeTodoItem.InspectionID,
			},
			EventCount:            1,
			ApproxPayloadBytes:    len([]byte(activeTodoItem.Text)),
			ApproxPromptTokens:    approximateLoopgateTokenCount(activeTodoItem.Text),
			DerivedDistillateIDs:  []string{distillateID},
			DerivedResonateKeyIDs: nil,
		}
		distillateRecord := continuityDistillateRecord{
			SchemaVersion:        continuityMemorySchemaVersion,
			DerivationVersion:    continuityDerivationVersion,
			DistillateID:         distillateID,
			InspectionID:         inspectionID,
			ThreadID:             threadID,
			Scope:                validatedRequest.Scope,
			GoalType:             goalTypeWorkflowFollowup,
			GoalFamilyID:         goalTypeWorkflowFollowup + ":explicit_todo",
			NormalizationVersion: continuityNormalizationVersion,
			UserImportance:       userImportance,
			RetentionScore:       retentionScore,
			EffectiveHotness:     effectiveHotness,
			MemoryState:          deriveMemoryState(effectiveHotness, continuityLineageStatusEligible),
			DerivationSignature: makeEventPayloadID("todo_close_signature", struct {
				ItemID string `json:"item_id"`
				Text   string `json:"text"`
			}{
				ItemID: validatedRequest.ItemID,
				Text:   activeTodoItem.Text,
			}),
			CreatedAtUTC: nowUTC.Format(time.RFC3339Nano),
			SourceRefs:   []continuityArtifactSourceRef{sourceRef},
			Tags:         normalizeLoopgateMemoryTags([]string{"todo", validatedRequest.ItemID, activeTodoItem.Text}),
			UnresolvedItemOps: []continuityUnresolvedItemOp{{
				ItemID:             validatedRequest.ItemID,
				Text:               activeTodoItem.Text,
				Action:             "closed",
				SemanticProjection: deriveUnresolvedItemOpSemanticProjection("closed", "", activeTodoItem.Text, memorySourceChannelCapability, tclpkg.TrustSystemDerived),
			}},
		}

		// Completion is the current-state winner for an explicit todo item. Keep the
		// original open inspection in authoritative history, but tombstone its active
		// artifacts so discover/recall stop resurfacing stale blockers after closure.
		existingOpenInspection.Lineage = continuityInspectionLineage{
			Status:                   continuityLineageStatusTombstoned,
			ChangedAtUTC:             nowUTC.Format(time.RFC3339Nano),
			Reason:                   "superseded_by_todo_completion",
			OperationID:              "todo_complete_" + inspectionID,
			SupersededByInspectionID: inspectionID,
			SupersededByDistillateID: distillateID,
		}
		stampContinuityDerivedArtifactsExcluded(&workingState, existingOpenInspection, nowUTC)
		workingState.Inspections[activeTodoItem.InspectionID] = existingOpenInspection
		workingState.Inspections[inspectionID] = inspectionRecord
		workingState.Distillates[distillateID] = distillateRecord

		completeResponse = TodoCompleteResponse{
			Scope:          validatedRequest.Scope,
			ItemID:         validatedRequest.ItemID,
			Text:           activeTodoItem.Text,
			InspectionID:   inspectionID,
			DistillateID:   distillateID,
			CompletedAtUTC: nowUTC.Format(time.RFC3339Nano),
		}

		mutationEvents := continuityMutationEvents{
			Continuity: []continuityAuthoritativeEvent{{
				SchemaVersion: continuityMemorySchemaVersion,
				EventID:       "todo_complete_" + inspectionID,
				EventType:     "todo_item_completed",
				CreatedAtUTC:  nowUTC.Format(time.RFC3339Nano),
				Actor:         tokenClaims.ControlSessionID,
				Scope:         validatedRequest.Scope,
				InspectionID:  inspectionID,
				ThreadID:      threadID,
				GoalType:      distillateRecord.GoalType,
				GoalFamilyID:  distillateRecord.GoalFamilyID,
				Inspection:    ptrContinuityInspectionRecord(cloneContinuityInspectionRecord(inspectionRecord)),
				Distillate:    ptrContinuityDistillateRecord(cloneContinuityDistillateRecord(distillateRecord)),
			}},
		}
		mutationEvents.Continuity = append(mutationEvents.Continuity, continuityAuthoritativeEvent{
			SchemaVersion: continuityMemorySchemaVersion,
			EventID:       "todo_complete_supersede_" + activeTodoItem.InspectionID,
			EventType:     "continuity_inspection_lineage_updated",
			CreatedAtUTC:  nowUTC.Format(time.RFC3339Nano),
			Actor:         tokenClaims.ControlSessionID,
			Scope:         existingOpenInspection.Scope,
			InspectionID:  existingOpenInspection.InspectionID,
			ThreadID:      existingOpenInspection.ThreadID,
			GoalType:      distillateRecord.GoalType,
			GoalFamilyID:  distillateRecord.GoalFamilyID,
			Lineage:       ptrContinuityInspectionLineage(existingOpenInspection.Lineage),
		})
		return workingState, map[string]interface{}{
			"item_id":       validatedRequest.ItemID,
			"inspection_id": inspectionID,
			"distillate_id": distillateID,
			"scope":         validatedRequest.Scope,
		}, mutationEvents, nil
	})
	if err != nil {
		return TodoCompleteResponse{}, err
	}
	return completeResponse, nil
}

func normalizeExplicitTodoWorkflowStatus(raw string) string {
	normalized := strings.TrimSpace(strings.ToLower(raw))
	switch normalized {
	case explicitTodoWorkflowStatusTodo, explicitTodoWorkflowStatusInProgress:
		return normalized
	default:
		return ""
	}
}

func validatePutExplicitTodoWorkflowStatus(raw string) error {
	if normalizeExplicitTodoWorkflowStatus(raw) == "" {
		return fmt.Errorf("status must be %q or %q", explicitTodoWorkflowStatusTodo, explicitTodoWorkflowStatusInProgress)
	}
	return nil
}

func (server *Server) setExplicitTodoItemWorkflowStatus(tokenClaims capabilityToken, itemID string, requestedStatus string) error {
	validatedStatus := normalizeExplicitTodoWorkflowStatus(requestedStatus)
	if validatedStatus == "" {
		return fmt.Errorf("invalid todo workflow status")
	}
	if err := identifiers.ValidateSafeIdentifier("item_id", strings.TrimSpace(itemID)); err != nil {
		return err
	}

	return server.mutateContinuityMemory(tokenClaims.TenantID, tokenClaims.ControlSessionID, "memory.todo.status_changed", func(workingState continuityMemoryState, nowUTC time.Time) (continuityMemoryState, map[string]interface{}, continuityMutationEvents, error) {
		if err := server.consumeMemoryFactWriteBudgetLocked(tokenClaims.ControlSessionID, tokenClaims.PeerIdentity.UID, nowUTC); err != nil {
			return workingState, nil, continuityMutationEvents{}, err
		}

		activeTodoItem, found := activeExplicitTodoItemByID(workingState, itemID)
		if !found {
			return workingState, nil, continuityMutationEvents{}, continuityGovernanceError{
				httpStatus:     404,
				responseStatus: ResponseStatusDenied,
				denialCode:     DenialCodeTodoItemNotFound,
				reason:         "todo item not found",
			}
		}
		currentStatus := activeTodoItem.Status
		if currentStatus == "" {
			currentStatus = explicitTodoWorkflowStatusTodo
		}
		if currentStatus == validatedStatus {
			return workingState, nil, continuityMutationEvents{}, nil
		}

		todoSuffix := makeEventPayloadID("todo_status", struct {
			ItemID string `json:"item_id"`
			Status string `json:"status"`
			NowUTC string `json:"now_utc"`
		}{
			ItemID: itemID,
			Status: validatedStatus,
			NowUTC: nowUTC.Format(time.RFC3339Nano),
		})
		threadID := "thread_" + todoSuffix
		inspectionID := "inspect_" + todoSuffix
		distillateID := "dist_" + todoSuffix
		sourceRef := continuityArtifactSourceRef{
			Kind: explicitTodoSourceKind,
			Ref:  itemID,
		}
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
				Reason:         "explicit_todo_status",
				OperationID:    "todo_status_" + inspectionID,
			},
			Lineage: continuityInspectionLineage{
				Status:       continuityLineageStatusEligible,
				ChangedAtUTC: nowUTC.Format(time.RFC3339Nano),
				Reason:       "explicit_todo_status",
				OperationID:  "todo_status_" + inspectionID,
			},
			EventCount:           1,
			ApproxPayloadBytes:   len([]byte(activeTodoItem.Text)),
			ApproxPromptTokens:   approximateLoopgateTokenCount(activeTodoItem.Text),
			DerivedDistillateIDs: []string{distillateID},
		}
		distillateRecord := continuityDistillateRecord{
			SchemaVersion:        continuityMemorySchemaVersion,
			DerivationVersion:    continuityDerivationVersion,
			DistillateID:         distillateID,
			InspectionID:         inspectionID,
			ThreadID:             threadID,
			Scope:                memoryScopeGlobal,
			GoalType:             goalTypeWorkflowFollowup,
			GoalFamilyID:         goalTypeWorkflowFollowup + ":explicit_todo",
			NormalizationVersion: continuityNormalizationVersion,
			UserImportance:       userImportance,
			RetentionScore:       retentionScore,
			EffectiveHotness:     effectiveHotness,
			MemoryState:          deriveMemoryState(effectiveHotness, continuityLineageStatusEligible),
			DerivationSignature: makeEventPayloadID("todo_status_signature", struct {
				ItemID string `json:"item_id"`
				Status string `json:"status"`
			}{
				ItemID: itemID,
				Status: validatedStatus,
			}),
			CreatedAtUTC: nowUTC.Format(time.RFC3339Nano),
			SourceRefs:   []continuityArtifactSourceRef{sourceRef},
			Tags:         normalizeLoopgateMemoryTags([]string{"todo", itemID, activeTodoItem.Text}),
			UnresolvedItemOps: []continuityUnresolvedItemOp{{
				ItemID:             itemID,
				Text:               activeTodoItem.Text,
				Action:             todoItemOpStatusSet,
				Status:             validatedStatus,
				SemanticProjection: deriveUnresolvedItemOpSemanticProjection(todoItemOpStatusSet, validatedStatus, activeTodoItem.Text, memorySourceChannelCapability, tclpkg.TrustSystemDerived),
			}},
		}

		workingState.Inspections[inspectionID] = inspectionRecord
		workingState.Distillates[distillateID] = distillateRecord

		mutationEvents := continuityMutationEvents{
			Continuity: []continuityAuthoritativeEvent{{
				SchemaVersion: continuityMemorySchemaVersion,
				EventID:       "todo_status_" + inspectionID,
				EventType:     "todo_item_status_changed",
				CreatedAtUTC:  nowUTC.Format(time.RFC3339Nano),
				Actor:         tokenClaims.ControlSessionID,
				Scope:         memoryScopeGlobal,
				InspectionID:  inspectionID,
				ThreadID:      threadID,
				GoalType:      distillateRecord.GoalType,
				GoalFamilyID:  distillateRecord.GoalFamilyID,
				Inspection:    ptrContinuityInspectionRecord(cloneContinuityInspectionRecord(inspectionRecord)),
				Distillate:    ptrContinuityDistillateRecord(cloneContinuityDistillateRecord(distillateRecord)),
			}},
		}
		return workingState, map[string]interface{}{
			"item_id": itemID,
			"status":  validatedStatus,
			"scope":   memoryScopeGlobal,
			"text":    secrets.RedactText(activeTodoItem.Text),
		}, mutationEvents, nil
	})
}

func (server *Server) listTodoItems(tenantID string) (TodoListResponse, error) {
	wakeState, err := server.loadMemoryWakeState(tenantID)
	if err != nil {
		return TodoListResponse{}, err
	}
	return TodoListResponse{
		Scope:           wakeState.Scope,
		UnresolvedItems: append([]MemoryWakeStateOpenItem(nil), wakeState.UnresolvedItems...),
		ActiveGoals:     append([]string(nil), wakeState.ActiveGoals...),
	}, nil
}

func (server *Server) normalizeTodoAddRequest(rawRequest TodoAddRequest) (TodoAddRequest, error) {
	validatedRequest := rawRequest
	validatedRequest.Scope = strings.TrimSpace(validatedRequest.Scope)
	if validatedRequest.Scope == "" {
		validatedRequest.Scope = memoryScopeGlobal
	}
	validatedRequest.Text = normalizeTodoText(validatedRequest.Text)
	validatedRequest.TaskKind = normalizeTaskKind(validatedRequest.TaskKind)
	validatedRequest.SourceKind = normalizeTaskSourceKind(validatedRequest.SourceKind)
	validatedRequest.NextStep = normalizeTodoText(validatedRequest.NextStep)
	validatedRequest.ScheduledForUTC = strings.TrimSpace(validatedRequest.ScheduledForUTC)
	validatedRequest.ExecutionClass = normalizeTaskExecutionClass(validatedRequest.ExecutionClass)
	validatedRequest.Reason = strings.TrimSpace(validatedRequest.Reason)
	if err := validatedRequest.Validate(); err != nil {
		return TodoAddRequest{}, err
	}
	if validatedRequest.Scope != memoryScopeGlobal {
		return TodoAddRequest{}, fmt.Errorf("scope must be %q", memoryScopeGlobal)
	}
	if err := validateTaskKind(validatedRequest.TaskKind); err != nil {
		return TodoAddRequest{}, err
	}
	if err := validateTaskSourceKind(validatedRequest.SourceKind); err != nil {
		return TodoAddRequest{}, err
	}
	if err := validateTaskExecutionClass(validatedRequest.ExecutionClass); err != nil {
		return TodoAddRequest{}, err
	}
	return validatedRequest, nil
}

func (server *Server) normalizeTodoCompleteRequest(rawRequest TodoCompleteRequest) (TodoCompleteRequest, error) {
	validatedRequest := rawRequest
	validatedRequest.Scope = strings.TrimSpace(validatedRequest.Scope)
	if validatedRequest.Scope == "" {
		validatedRequest.Scope = memoryScopeGlobal
	}
	validatedRequest.ItemID = strings.TrimSpace(validatedRequest.ItemID)
	validatedRequest.Reason = strings.TrimSpace(validatedRequest.Reason)
	if err := validatedRequest.Validate(); err != nil {
		return TodoCompleteRequest{}, err
	}
	if validatedRequest.Scope != memoryScopeGlobal {
		return TodoCompleteRequest{}, fmt.Errorf("scope must be %q", memoryScopeGlobal)
	}
	if err := identifiers.ValidateSafeIdentifier("item_id", validatedRequest.ItemID); err != nil {
		return TodoCompleteRequest{}, err
	}
	return validatedRequest, nil
}

func normalizeTodoText(rawText string) string {
	trimmedText := strings.TrimSpace(rawText)
	if trimmedText == "" {
		return ""
	}
	return strings.Join(strings.Fields(trimmedText), " ")
}

func normalizeTaskKind(rawTaskKind string) string {
	normalizedTaskKind := strings.TrimSpace(strings.ToLower(rawTaskKind))
	if normalizedTaskKind == "" {
		return taskKindCarryOver
	}
	return normalizedTaskKind
}

func validateTaskKind(taskKind string) error {
	switch taskKind {
	case taskKindCarryOver, taskKindOneOff, taskKindScheduled:
		return nil
	default:
		return fmt.Errorf("task_kind %q is invalid", taskKind)
	}
}

func normalizeTaskSourceKind(rawSourceKind string) string {
	normalizedSourceKind := strings.TrimSpace(strings.ToLower(rawSourceKind))
	if normalizedSourceKind == "" {
		return taskSourceUser
	}
	return normalizedSourceKind
}

func validateTaskSourceKind(sourceKind string) error {
	if sourceKind == "" {
		return fmt.Errorf("source_kind is required")
	}
	if err := identifiers.ValidateSafeIdentifier("source_kind", sourceKind); err != nil {
		return err
	}
	return nil
}

func (server *Server) buildExplicitTodoTaskFacts(itemID string, validatedRequest TodoAddRequest) []continuityDistillateFact {
	sourceRef := explicitTodoSourceKind + ":" + itemID
	taskFacts := []continuityDistillateFact{
		server.newExplicitTodoTaskFact(sourceRef, taskFactKind, validatedRequest.TaskKind),
		server.newExplicitTodoTaskFact(sourceRef, taskFactSourceKind, validatedRequest.SourceKind),
		server.newExplicitTodoTaskFact(sourceRef, taskFactExecutionClass, validatedRequest.ExecutionClass),
	}
	if validatedRequest.NextStep != "" {
		taskFacts = append(taskFacts, server.newExplicitTodoTaskFact(sourceRef, taskFactNextStep, validatedRequest.NextStep))
	}
	if validatedRequest.ScheduledForUTC != "" {
		taskFacts = append(taskFacts, server.newExplicitTodoTaskFact(sourceRef, taskFactScheduledForUT, validatedRequest.ScheduledForUTC))
	}
	return taskFacts
}

func (server *Server) newExplicitTodoTaskFact(sourceRef string, factName string, factValue string) continuityDistillateFact {
	normalizedFactValue := strings.TrimSpace(factValue)
	return continuityDistillateFact{
		Name:               factName,
		Value:              normalizedFactValue,
		SourceRef:          sourceRef,
		EpistemicFlavor:    "freshly_checked",
		SemanticProjection: deriveExplicitTodoTaskFactSemanticProjection(factName, normalizedFactValue),
	}
}

func deriveExplicitTodoTaskFactSemanticProjection(factName string, factValue string) *tclpkg.SemanticProjection {
	normalizedFactName := strings.TrimSpace(factName)
	normalizedFactValue := strings.TrimSpace(factValue)
	if normalizedFactName == "" || normalizedFactValue == "" {
		return nil
	}
	return deriveMemoryCandidateSemanticProjection(tclpkg.MemoryCandidate{
		Source:              tclpkg.CandidateSourceTaskMetadata,
		SourceChannel:       memorySourceChannelCapability,
		NormalizedFactKey:   normalizedFactName,
		NormalizedFactValue: normalizedFactValue,
		Trust:               tclpkg.TrustSystemDerived,
		Actor:               tclpkg.ObjectSystem,
	})
}
