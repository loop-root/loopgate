package loopgate

import (
	"errors"
	"fmt"
	"sort"
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

func (server *Server) executeTodoAddCapability(tokenClaims capabilityToken, capabilityRequest CapabilityRequest) CapabilityResponse {
	addResponse, err := server.addTodoItem(tokenClaims, TodoAddRequest{
		Scope:           memoryScopeGlobal,
		Text:            capabilityRequest.Arguments["text"],
		Reason:          capabilityRequest.Arguments["reason"],
		TaskKind:        capabilityRequest.Arguments["task_kind"],
		SourceKind:      capabilityRequest.Arguments["source_kind"],
		NextStep:        capabilityRequest.Arguments["next_step"],
		ScheduledForUTC: capabilityRequest.Arguments["scheduled_for_utc"],
		ExecutionClass:  capabilityRequest.Arguments["execution_class"],
	})
	if err != nil {
		return server.todoCapabilityErrorResponse(tokenClaims, capabilityRequest, err)
	}

	classification := ResultClassification{
		Exposure: ResultExposureDisplay,
		Eligibility: ResultEligibility{
			Prompt: true,
			Memory: false,
		},
	}
	structuredResult := map[string]interface{}{
		"content":           todoAddSuccessText(addResponse),
		"item_id":           addResponse.ItemID,
		"text":              addResponse.Text,
		"task_kind":         addResponse.TaskKind,
		"source_kind":       addResponse.SourceKind,
		"next_step":         addResponse.NextStep,
		"scheduled_for_utc": addResponse.ScheduledForUTC,
		"execution_class":   addResponse.ExecutionClass,
		"added_at_utc":      addResponse.AddedAtUTC,
		"already_present":   addResponse.AlreadyPresent,
	}
	fieldsMeta, fieldsMetaErr := fieldsMetadataForStructuredResult(structuredResult, ResultFieldOriginLocal, classification)
	if fieldsMetaErr != nil {
		fe := fmt.Errorf("todo.add fields_meta: %w", fieldsMetaErr)
		if auditErr := server.logEvent("capability.error", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"error":                secrets.RedactText(fe.Error()),
			"operator_error_class": secrets.LoopgateOperatorErrorClass(fe),
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
			"token_id":             tokenClaims.TokenID,
			"parent_token_id":      tokenClaims.ParentTokenID,
		}); auditErr != nil {
			return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
		}
		server.emitUIEvent(tokenClaims.ControlSessionID, UIEventTypeWarning, UIEventWarning{
			Message: capabilityRequest.Capability + " failed: internal result metadata error",
		})
		return CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusError,
			DenialReason: "internal result metadata error",
			DenialCode:   DenialCodeExecutionFailed,
			Redacted:     true,
		}
	}
	resultMetadata := map[string]interface{}{
		"prompt_eligible": true,
		"memory_eligible": false,
		"display_only":    false,
		"audit_only":      false,
		"quarantined":     false,
		"todo_scope":      addResponse.Scope,
		"already_present": addResponse.AlreadyPresent,
	}

	if err := server.logEvent("capability.executed", tokenClaims.ControlSessionID, map[string]interface{}{
		"request_id":            capabilityRequest.RequestID,
		"capability":            capabilityRequest.Capability,
		"status":                ResponseStatusSuccess,
		"result_classification": classification,
		"result_provenance":     resultMetadata,
		"actor_label":           tokenClaims.ActorLabel,
		"client_session_label":  tokenClaims.ClientSessionLabel,
		"control_session_id":    tokenClaims.ControlSessionID,
		"token_id":              tokenClaims.TokenID,
		"parent_token_id":       tokenClaims.ParentTokenID,
	}); err != nil {
		return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
	}

	successResponse := CapabilityResponse{
		RequestID:        capabilityRequest.RequestID,
		Status:           ResponseStatusSuccess,
		StructuredResult: structuredResult,
		FieldsMeta:       fieldsMeta,
		Classification:   classification,
		Metadata:         resultMetadata,
	}
	server.emitUIToolResult(tokenClaims.ControlSessionID, capabilityRequest, successResponse)
	return successResponse
}

func (server *Server) executeTodoCompleteCapability(tokenClaims capabilityToken, capabilityRequest CapabilityRequest) CapabilityResponse {
	completeResponse, err := server.completeTodoItem(tokenClaims, TodoCompleteRequest{
		Scope:  memoryScopeGlobal,
		ItemID: capabilityRequest.Arguments["item_id"],
		Reason: capabilityRequest.Arguments["reason"],
	})
	if err != nil {
		return server.todoCapabilityErrorResponse(tokenClaims, capabilityRequest, err)
	}

	classification := ResultClassification{
		Exposure: ResultExposureDisplay,
		Eligibility: ResultEligibility{
			Prompt: true,
			Memory: false,
		},
	}
	fieldsMeta := map[string]ResultFieldMetadata{
		"content": {
			Origin:         ResultFieldOriginLocal,
			ContentType:    "text/plain",
			Trust:          ResultFieldTrustDeterministic,
			Sensitivity:    ResultFieldSensitivityTaintedText,
			Kind:           ResultFieldKindScalar,
			ScalarSubclass: ResultFieldScalarSubclassShortTextLabel,
			PromptEligible: true,
		},
		"item_id": {
			Origin:         ResultFieldOriginLocal,
			ContentType:    "text/plain",
			Trust:          ResultFieldTrustDeterministic,
			Sensitivity:    ResultFieldSensitivityBenign,
			Kind:           ResultFieldKindScalar,
			ScalarSubclass: ResultFieldScalarSubclassStrictIdentifier,
			PromptEligible: true,
		},
		"text": {
			Origin:         ResultFieldOriginLocal,
			ContentType:    "text/plain",
			Trust:          ResultFieldTrustDeterministic,
			Sensitivity:    ResultFieldSensitivityTaintedText,
			Kind:           ResultFieldKindScalar,
			ScalarSubclass: ResultFieldScalarSubclassShortTextLabel,
			PromptEligible: true,
		},
		"completed_at_utc": {
			Origin:         ResultFieldOriginLocal,
			ContentType:    "text/plain",
			Trust:          ResultFieldTrustDeterministic,
			Sensitivity:    ResultFieldSensitivityBenign,
			Kind:           ResultFieldKindScalar,
			ScalarSubclass: ResultFieldScalarSubclassTimestamp,
			PromptEligible: true,
		},
	}
	structuredResult := map[string]interface{}{
		"content":          todoCompleteSuccessText(completeResponse),
		"item_id":          completeResponse.ItemID,
		"text":             completeResponse.Text,
		"completed_at_utc": completeResponse.CompletedAtUTC,
	}
	resultMetadata := map[string]interface{}{
		"prompt_eligible": true,
		"memory_eligible": false,
		"display_only":    false,
		"audit_only":      false,
		"quarantined":     false,
		"todo_scope":      completeResponse.Scope,
	}

	if err := server.logEvent("capability.executed", tokenClaims.ControlSessionID, map[string]interface{}{
		"request_id":            capabilityRequest.RequestID,
		"capability":            capabilityRequest.Capability,
		"status":                ResponseStatusSuccess,
		"result_classification": classification,
		"result_provenance":     resultMetadata,
		"actor_label":           tokenClaims.ActorLabel,
		"client_session_label":  tokenClaims.ClientSessionLabel,
		"control_session_id":    tokenClaims.ControlSessionID,
		"token_id":              tokenClaims.TokenID,
		"parent_token_id":       tokenClaims.ParentTokenID,
	}); err != nil {
		return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
	}

	successResponse := CapabilityResponse{
		RequestID:        capabilityRequest.RequestID,
		Status:           ResponseStatusSuccess,
		StructuredResult: structuredResult,
		FieldsMeta:       fieldsMeta,
		Classification:   classification,
		Metadata:         resultMetadata,
	}
	server.emitUIToolResult(tokenClaims.ControlSessionID, capabilityRequest, successResponse)
	return successResponse
}

func (server *Server) executeTodoListCapability(tokenClaims capabilityToken, capabilityRequest CapabilityRequest) CapabilityResponse {
	listResponse, err := server.listTodoItems(tokenClaims.TenantID)
	if err != nil {
		if auditErr := server.logEvent("capability.error", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"error":                secrets.RedactText(err.Error()),
			"operator_error_class": secrets.LoopgateOperatorErrorClass(err),
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
			"token_id":             tokenClaims.TokenID,
			"parent_token_id":      tokenClaims.ParentTokenID,
		}); auditErr != nil {
			return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
		}
		return CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusError,
			DenialReason: "wake-state backend is unavailable",
			DenialCode:   DenialCodeExecutionFailed,
			Redacted:     true,
		}
	}

	classification := ResultClassification{
		Exposure: ResultExposureDisplay,
		Eligibility: ResultEligibility{
			Prompt: true,
			Memory: false,
		},
	}
	fieldsMeta := map[string]ResultFieldMetadata{
		"content": {
			Origin:         ResultFieldOriginLocal,
			ContentType:    "text/plain",
			Trust:          ResultFieldTrustDeterministic,
			Sensitivity:    ResultFieldSensitivityTaintedText,
			Kind:           ResultFieldKindScalar,
			ScalarSubclass: ResultFieldScalarSubclassShortTextLabel,
			PromptEligible: true,
		},
		"item_count": {
			Origin:         ResultFieldOriginLocal,
			ContentType:    "application/json",
			Trust:          ResultFieldTrustDeterministic,
			Sensitivity:    ResultFieldSensitivityBenign,
			Kind:           ResultFieldKindScalar,
			ScalarSubclass: ResultFieldScalarSubclassValidatedNumber,
			PromptEligible: true,
		},
		"goal_count": {
			Origin:         ResultFieldOriginLocal,
			ContentType:    "application/json",
			Trust:          ResultFieldTrustDeterministic,
			Sensitivity:    ResultFieldSensitivityBenign,
			Kind:           ResultFieldKindScalar,
			ScalarSubclass: ResultFieldScalarSubclassValidatedNumber,
			PromptEligible: true,
		},
		"unresolved_items": {
			Origin:         ResultFieldOriginLocal,
			ContentType:    "application/json",
			Trust:          ResultFieldTrustDeterministic,
			Sensitivity:    ResultFieldSensitivityTaintedText,
			Kind:           ResultFieldKindArray,
			PromptEligible: false,
		},
		"active_goals": {
			Origin:         ResultFieldOriginLocal,
			ContentType:    "application/json",
			Trust:          ResultFieldTrustDeterministic,
			Sensitivity:    ResultFieldSensitivityTaintedText,
			Kind:           ResultFieldKindArray,
			PromptEligible: false,
		},
	}
	truncUnresolved, truncGoals, structOmittedTasks, structOmittedGoals := truncateTodoListForStructuredPayload(listResponse)
	structuredResult := map[string]interface{}{
		"content":          todoListContentText(listResponse),
		"item_count":       len(listResponse.UnresolvedItems),
		"goal_count":       len(listResponse.ActiveGoals),
		"unresolved_items": truncUnresolved,
		"active_goals":     truncGoals,
	}
	resultMetadata := map[string]interface{}{
		"prompt_eligible": true,
		"memory_eligible": false,
		"display_only":    false,
		"audit_only":      false,
		"quarantined":     false,
		"todo_scope":      listResponse.Scope,
	}
	if structOmittedTasks > 0 {
		resultMetadata["todo_struct_omitted_open_tasks"] = structOmittedTasks
	}
	if structOmittedGoals > 0 {
		resultMetadata["todo_struct_omitted_active_goals"] = structOmittedGoals
	}

	if err := server.logEvent("capability.executed", tokenClaims.ControlSessionID, map[string]interface{}{
		"request_id":            capabilityRequest.RequestID,
		"capability":            capabilityRequest.Capability,
		"status":                ResponseStatusSuccess,
		"result_classification": classification,
		"result_provenance":     resultMetadata,
		"actor_label":           tokenClaims.ActorLabel,
		"client_session_label":  tokenClaims.ClientSessionLabel,
		"control_session_id":    tokenClaims.ControlSessionID,
		"token_id":              tokenClaims.TokenID,
		"parent_token_id":       tokenClaims.ParentTokenID,
	}); err != nil {
		return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
	}

	successResponse := CapabilityResponse{
		RequestID:        capabilityRequest.RequestID,
		Status:           ResponseStatusSuccess,
		StructuredResult: structuredResult,
		FieldsMeta:       fieldsMeta,
		Classification:   classification,
		Metadata:         resultMetadata,
	}
	server.emitUIToolResult(tokenClaims.ControlSessionID, capabilityRequest, successResponse)
	return successResponse
}

func (server *Server) todoCapabilityErrorResponse(tokenClaims capabilityToken, capabilityRequest CapabilityRequest, operationError error) CapabilityResponse {
	var governanceError continuityGovernanceError
	if errors.As(operationError, &governanceError) {
		response := CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       governanceError.responseStatus,
			DenialReason: governanceError.reason,
			DenialCode:   governanceError.denialCode,
			Redacted:     true,
		}
		eventType := "capability.error"
		if response.Status == ResponseStatusDenied {
			eventType = "capability.denied"
		}
		if err := server.logEvent(eventType, tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"reason":               secrets.RedactText(governanceError.reason),
			"denial_code":          governanceError.denialCode,
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
			"token_id":             tokenClaims.TokenID,
			"parent_token_id":      tokenClaims.ParentTokenID,
		}); err != nil {
			return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
		}
		if response.Status == ResponseStatusDenied {
			server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, response.DenialCode, response.DenialReason)
		} else {
			server.emitUIEvent(tokenClaims.ControlSessionID, UIEventTypeWarning, UIEventWarning{
				Message: capabilityRequest.Capability + " failed: " + secrets.RedactText(governanceError.reason),
			})
		}
		return response
	}

	if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
		"request_id":           capabilityRequest.RequestID,
		"capability":           capabilityRequest.Capability,
		"reason":               secrets.RedactText(operationError.Error()),
		"denial_code":          DenialCodeInvalidCapabilityArguments,
		"actor_label":          tokenClaims.ActorLabel,
		"client_session_label": tokenClaims.ClientSessionLabel,
		"control_session_id":   tokenClaims.ControlSessionID,
		"token_id":             tokenClaims.TokenID,
		"parent_token_id":      tokenClaims.ParentTokenID,
	}); err != nil {
		return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
	}
	response := CapabilityResponse{
		RequestID:    capabilityRequest.RequestID,
		Status:       ResponseStatusError,
		DenialReason: operationError.Error(),
		DenialCode:   DenialCodeInvalidCapabilityArguments,
		Redacted:     true,
	}
	server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, response.DenialCode, response.DenialReason)
	return response
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
				Status:       continuityLineageStatusEligible,
				ChangedAtUTC: nowUTC.Format(time.RFC3339Nano),
				Reason:       "explicit_todo_complete",
				OperationID:  "todo_complete_" + inspectionID,
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

func goalTextsFromContinuityState(currentState continuityMemoryState) []string {
	goalSnapshot := buildGoalsCurrentSnapshot(currentState)
	rawGoals, ok := goalSnapshot["goals"].([]map[string]string)
	if !ok || len(rawGoals) == 0 {
		return nil
	}
	texts := make([]string, 0, len(rawGoals))
	for _, goalEntry := range rawGoals {
		texts = append(texts, goalEntry["text"])
	}
	return texts
}

func explicitTodoOpenDistillateForItemID(currentState continuityMemoryState, itemID string) (continuityDistillateRecord, bool) {
	distillates := make([]continuityDistillateRecord, 0, len(currentState.Distillates))
	for _, distillateRecord := range currentState.Distillates {
		distillates = append(distillates, distillateRecord)
	}
	sort.Slice(distillates, func(leftIndex int, rightIndex int) bool {
		if distillates[leftIndex].CreatedAtUTC != distillates[rightIndex].CreatedAtUTC {
			return distillates[leftIndex].CreatedAtUTC < distillates[rightIndex].CreatedAtUTC
		}
		return distillates[leftIndex].DistillateID < distillates[rightIndex].DistillateID
	})
	for _, distillateRecord := range distillates {
		if !isExplicitTodoDistillate(distillateRecord) {
			continue
		}
		for _, itemOp := range distillateRecord.UnresolvedItemOps {
			if itemOp.Action == "opened" && itemOp.ItemID == itemID {
				return distillateRecord, true
			}
		}
	}
	return continuityDistillateRecord{}, false
}

func isExplicitTodoCompletionDistillate(distillateRecord continuityDistillateRecord) bool {
	if !isExplicitTodoDistillate(distillateRecord) {
		return false
	}
	if len(distillateRecord.UnresolvedItemOps) != 1 {
		return false
	}
	return distillateRecord.UnresolvedItemOps[0].Action == "closed"
}

func recentExplicitTodoCompletionUIEntries(currentState continuityMemoryState, maxItems int) []UITasksItemEntry {
	if maxItems <= 0 {
		return nil
	}
	completions := make([]continuityDistillateRecord, 0)
	for _, distillateRecord := range currentState.Distillates {
		if isExplicitTodoCompletionDistillate(distillateRecord) {
			completions = append(completions, distillateRecord)
		}
	}
	sort.Slice(completions, func(leftIndex int, rightIndex int) bool {
		leftCreated := completions[leftIndex].CreatedAtUTC
		rightCreated := completions[rightIndex].CreatedAtUTC
		if leftCreated != rightCreated {
			return leftCreated > rightCreated
		}
		return completions[leftIndex].DistillateID > completions[rightIndex].DistillateID
	})
	if len(completions) > maxItems {
		completions = completions[:maxItems]
	}
	entries := make([]UITasksItemEntry, 0, len(completions))
	for _, completionDistillate := range completions {
		closeOp := completionDistillate.UnresolvedItemOps[0]
		itemID := closeOp.ItemID
		openDistillate, foundOpen := explicitTodoOpenDistillateForItemID(currentState, itemID)
		meta := explicitTodoTaskMetadataFromDistillate(completionDistillate)
		createdAt := completionDistillate.CreatedAtUTC
		if foundOpen {
			meta = explicitTodoTaskMetadataFromDistillate(openDistillate)
			createdAt = openDistillate.CreatedAtUTC
		}
		entries = append(entries, UITasksItemEntry{
			ID:           itemID,
			Text:         closeOp.Text,
			TaskKind:     meta.TaskKind,
			SourceKind:   meta.SourceKind,
			NextStep:     meta.NextStep,
			Status:       explicitTodoWorkflowStatusDone,
			CreatedAtUTC: createdAt,
		})
	}
	return entries
}

func buildUITasksResponseFromContinuityState(currentState continuityMemoryState) UITasksResponse {
	response := UITasksResponse{
		Goals: goalTextsFromContinuityState(currentState),
		Items: make([]UITasksItemEntry, 0, 8),
	}
	for _, activeRecord := range activeExplicitTodoItems(currentState) {
		status := activeRecord.Status
		if status == "" {
			status = explicitTodoWorkflowStatusTodo
		}
		response.Items = append(response.Items, UITasksItemEntry{
			ID:           activeRecord.ItemID,
			Text:         activeRecord.Text,
			TaskKind:     activeRecord.TaskKind,
			SourceKind:   activeRecord.SourceKind,
			NextStep:     activeRecord.NextStep,
			Status:       status,
			CreatedAtUTC: activeRecord.CreatedAtUTC,
		})
	}
	response.Items = append(response.Items, recentExplicitTodoCompletionUIEntries(currentState, maxUIRecentCompletedTodoItems)...)
	return response
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

func activeExplicitTodoItemByID(currentState continuityMemoryState, itemID string) (explicitTodoItemRecord, bool) {
	for _, activeTodoItem := range activeExplicitTodoItems(currentState) {
		if activeTodoItem.ItemID == itemID {
			return activeTodoItem, true
		}
	}
	return explicitTodoItemRecord{}, false
}

func activeExplicitTodoItemByText(currentState continuityMemoryState, text string) (explicitTodoItemRecord, bool) {
	normalizedText := normalizeTodoText(text)
	for _, activeTodoItem := range activeExplicitTodoItems(currentState) {
		if normalizeTodoText(activeTodoItem.Text) == normalizedText {
			return activeTodoItem, true
		}
	}
	return explicitTodoItemRecord{}, false
}

func activeExplicitTodoItems(currentState continuityMemoryState) []explicitTodoItemRecord {
	activeTodoByID := make(map[string]explicitTodoItemRecord)
	activeTodoOrder := make([]string, 0)

	for _, distillateRecord := range activeLoopgateDistillates(currentState) {
		if !isExplicitTodoDistillate(distillateRecord) {
			continue
		}
		resonateKeyID := resonateKeyIDForDistillate(currentState, distillateRecord.DistillateID)
		taskMetadata := explicitTodoTaskMetadataFromDistillate(distillateRecord)
		for _, itemOp := range distillateRecord.UnresolvedItemOps {
			switch itemOp.Action {
			case "opened":
				activeTodoByID[itemOp.ItemID] = explicitTodoItemRecord{
					InspectionID:    distillateRecord.InspectionID,
					DistillateID:    distillateRecord.DistillateID,
					ResonateKeyID:   resonateKeyID,
					ItemID:          itemOp.ItemID,
					Text:            itemOp.Text,
					TaskKind:        taskMetadata.TaskKind,
					SourceKind:      taskMetadata.SourceKind,
					NextStep:        taskMetadata.NextStep,
					ScheduledForUTC: taskMetadata.ScheduledForUTC,
					ExecutionClass:  taskMetadata.ExecutionClass,
					CreatedAtUTC:    distillateRecord.CreatedAtUTC,
					Status:          explicitTodoWorkflowStatusTodo,
				}
				activeTodoOrder = appendWithoutDuplicate(activeTodoOrder, itemOp.ItemID)
			case "closed":
				delete(activeTodoByID, itemOp.ItemID)
				activeTodoOrder = removeStringValue(activeTodoOrder, itemOp.ItemID)
			case todoItemOpStatusSet:
				if existingTodo, ok := activeTodoByID[itemOp.ItemID]; ok {
					if normalized := normalizeExplicitTodoWorkflowStatus(itemOp.Status); normalized != "" {
						existingTodo.Status = normalized
						activeTodoByID[itemOp.ItemID] = existingTodo
					}
				}
			}
		}
	}

	activeTodos := make([]explicitTodoItemRecord, 0, len(activeTodoOrder))
	for _, itemID := range activeTodoOrder {
		activeTodoItem, found := activeTodoByID[itemID]
		if !found {
			continue
		}
		activeTodos = append(activeTodos, activeTodoItem)
	}
	sort.Slice(activeTodos, func(leftIndex int, rightIndex int) bool {
		leftTodo := activeTodos[leftIndex]
		rightTodo := activeTodos[rightIndex]
		if leftTodo.CreatedAtUTC != rightTodo.CreatedAtUTC {
			return leftTodo.CreatedAtUTC < rightTodo.CreatedAtUTC
		}
		return leftTodo.ItemID < rightTodo.ItemID
	})
	return activeTodos
}

func resonateKeyIDForDistillate(currentState continuityMemoryState, distillateID string) string {
	for _, resonateKeyRecord := range currentState.ResonateKeys {
		if resonateKeyRecord.DistillateID == distillateID {
			return resonateKeyRecord.KeyID
		}
	}
	return ""
}

func isExplicitTodoDistillate(distillateRecord continuityDistillateRecord) bool {
	for _, sourceRef := range distillateRecord.SourceRefs {
		if sourceRef.Kind == explicitTodoSourceKind {
			return true
		}
	}
	return false
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

func explicitTodoTaskMetadataFromDistillate(distillateRecord continuityDistillateRecord) MemoryWakeStateOpenItem {
	taskMetadata := MemoryWakeStateOpenItem{
		TaskKind:       taskKindCarryOver,
		SourceKind:     taskSourceContinuity,
		ExecutionClass: TaskExecutionClassApprovalRequired,
		CreatedAtUTC:   distillateRecord.CreatedAtUTC,
	}
	if isExplicitTodoDistillate(distillateRecord) {
		taskMetadata.SourceKind = taskSourceUser
	}
	for _, factRecord := range distillateRecord.Facts {
		factValue, _ := factRecord.Value.(string)
		switch strings.TrimSpace(factRecord.Name) {
		case taskFactKind:
			normalizedTaskKind := normalizeTaskKind(factValue)
			if validateTaskKind(normalizedTaskKind) == nil {
				taskMetadata.TaskKind = normalizedTaskKind
			}
		case taskFactSourceKind:
			normalizedSourceKind := normalizeTaskSourceKind(factValue)
			if validateTaskSourceKind(normalizedSourceKind) == nil {
				taskMetadata.SourceKind = normalizedSourceKind
			}
		case taskFactNextStep:
			taskMetadata.NextStep = normalizeTodoText(factValue)
		case taskFactScheduledForUT:
			taskMetadata.ScheduledForUTC = strings.TrimSpace(factValue)
		case taskFactExecutionClass:
			normalizedExecutionClass := normalizeTaskExecutionClass(factValue)
			if validateTaskExecutionClass(normalizedExecutionClass) == nil {
				taskMetadata.ExecutionClass = normalizedExecutionClass
			}
		}
	}
	return taskMetadata
}

func todoAddSuccessText(addResponse TodoAddResponse) string {
	if addResponse.AlreadyPresent {
		return fmt.Sprintf("You are already carrying %q on the task board.", addResponse.Text)
	}
	return fmt.Sprintf("Added %q to the task board.", addResponse.Text)
}

func todoCompleteSuccessText(completeResponse TodoCompleteResponse) string {
	if strings.TrimSpace(completeResponse.Text) == "" {
		return fmt.Sprintf("Marked %s as complete.", completeResponse.ItemID)
	}
	return fmt.Sprintf("Marked %q as complete.", completeResponse.Text)
}

// Limits for todo.list text echoed back into model prompts (TPM / context budget).
const (
	maxTodoListOpenTaskLinesInPrompt = 24
	maxTodoListGoalLinesInPrompt     = 16
	maxTodoListContentRunesInPrompt  = 6000
	maxTodoListStructuredItemsInJSON = 80
	maxTodoListSingleItemRunesInJSON = 400
)

func todoListContentText(listResponse TodoListResponse) string {
	if len(listResponse.UnresolvedItems) == 0 && len(listResponse.ActiveGoals) == 0 {
		return "The task board is clear right now."
	}

	lines := make([]string, 0, len(listResponse.UnresolvedItems)+len(listResponse.ActiveGoals)+4)
	omittedTasks := 0
	omittedGoals := 0
	if len(listResponse.UnresolvedItems) > 0 {
		lines = append(lines, "Open tasks:")
		limit := maxTodoListOpenTaskLinesInPrompt
		if len(listResponse.UnresolvedItems) > limit {
			omittedTasks = len(listResponse.UnresolvedItems) - limit
		}
		for taskIndex, unresolvedItem := range listResponse.UnresolvedItems {
			if taskIndex >= limit {
				break
			}
			itemText := strings.TrimSpace(unresolvedItem.Text)
			if itemText == "" {
				itemText = unresolvedItem.ID
			}
			if strings.TrimSpace(unresolvedItem.NextStep) != "" {
				lines = append(lines, "- "+itemText+" (next: "+strings.TrimSpace(unresolvedItem.NextStep)+")")
				continue
			}
			lines = append(lines, "- "+itemText)
		}
		if omittedTasks > 0 {
			lines = append(lines, fmt.Sprintf("… and %d more open task(s) not shown (use todo.list sparingly; complete or summarize tasks to reduce context).", omittedTasks))
		}
	}
	if len(listResponse.ActiveGoals) > 0 {
		lines = append(lines, "Active goals:")
		goalLimit := maxTodoListGoalLinesInPrompt
		if len(listResponse.ActiveGoals) > goalLimit {
			omittedGoals = len(listResponse.ActiveGoals) - goalLimit
		}
		for goalIndex, activeGoal := range listResponse.ActiveGoals {
			if goalIndex >= goalLimit {
				break
			}
			lines = append(lines, "- "+strings.TrimSpace(activeGoal))
		}
		if omittedGoals > 0 {
			lines = append(lines, fmt.Sprintf("… and %d more active goal(s) not shown.", omittedGoals))
		}
	}
	out := strings.Join(lines, "\n")
	if len([]rune(out)) > maxTodoListContentRunesInPrompt {
		runes := []rune(out)
		out = string(runes[:maxTodoListContentRunesInPrompt]) + "\n… (task list truncated for prompt size)"
	}
	return out
}

func truncateTodoListForStructuredPayload(listResponse TodoListResponse) (truncatedUnresolved []MemoryWakeStateOpenItem, truncatedGoals []string, omittedTasks int, omittedGoals int) {
	truncatedUnresolved = listResponse.UnresolvedItems
	if len(truncatedUnresolved) > maxTodoListStructuredItemsInJSON {
		omittedTasks = len(truncatedUnresolved) - maxTodoListStructuredItemsInJSON
		truncatedUnresolved = append([]MemoryWakeStateOpenItem(nil), truncatedUnresolved[:maxTodoListStructuredItemsInJSON]...)
	}
	for itemIndex := range truncatedUnresolved {
		textRunes := []rune(strings.TrimSpace(truncatedUnresolved[itemIndex].Text))
		if len(textRunes) > maxTodoListSingleItemRunesInJSON {
			truncatedUnresolved[itemIndex].Text = string(textRunes[:maxTodoListSingleItemRunesInJSON]) + "…"
		}
		nextRunes := []rune(strings.TrimSpace(truncatedUnresolved[itemIndex].NextStep))
		if len(nextRunes) > maxTodoListSingleItemRunesInJSON {
			truncatedUnresolved[itemIndex].NextStep = string(nextRunes[:maxTodoListSingleItemRunesInJSON]) + "…"
		}
	}
	truncatedGoals = listResponse.ActiveGoals
	if len(truncatedGoals) > maxTodoListStructuredItemsInJSON {
		omittedGoals = len(truncatedGoals) - maxTodoListStructuredItemsInJSON
		truncatedGoals = append([]string(nil), truncatedGoals[:maxTodoListStructuredItemsInJSON]...)
	}
	for goalIndex, goalText := range truncatedGoals {
		goalRunes := []rune(strings.TrimSpace(goalText))
		if len(goalRunes) > maxTodoListSingleItemRunesInJSON {
			truncatedGoals[goalIndex] = string(goalRunes[:maxTodoListSingleItemRunesInJSON]) + "…"
		}
	}
	return truncatedUnresolved, truncatedGoals, omittedTasks, omittedGoals
}

// ---------------------------------------------------------------------------
// Goal capabilities
// ---------------------------------------------------------------------------

func (server *Server) executeGoalSetCapability(tokenClaims capabilityToken, capabilityRequest CapabilityRequest) CapabilityResponse {
	text := strings.TrimSpace(capabilityRequest.Arguments["text"])
	if text == "" {
		return CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusDenied,
			DenialReason: "goal text must not be empty",
			DenialCode:   DenialCodeMalformedRequest,
		}
	}

	goalID, err := server.setGoal(tokenClaims, text)
	if err != nil {
		return server.todoCapabilityErrorResponse(tokenClaims, capabilityRequest, err)
	}

	classification := ResultClassification{
		Exposure: ResultExposureDisplay,
		Eligibility: ResultEligibility{
			Prompt: true,
			Memory: false,
		},
	}
	structuredResult := map[string]interface{}{
		"content": fmt.Sprintf("Goal set: %s (goal_id=%s)", text, goalID),
		"goal_id": goalID,
		"text":    text,
	}
	fieldsMeta, fieldsMetaErr := fieldsMetadataForStructuredResult(structuredResult, ResultFieldOriginLocal, classification)
	if fieldsMetaErr != nil {
		server.emitUIEvent(tokenClaims.ControlSessionID, UIEventTypeWarning, UIEventWarning{
			Message: capabilityRequest.Capability + " failed: internal result metadata error",
		})
		return CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusError,
			DenialReason: "internal result metadata error",
			DenialCode:   DenialCodeExecutionFailed,
			Redacted:     true,
		}
	}
	if err := server.logEvent("capability.executed", tokenClaims.ControlSessionID, map[string]interface{}{
		"request_id":           capabilityRequest.RequestID,
		"capability":           capabilityRequest.Capability,
		"status":               ResponseStatusSuccess,
		"goal_id":              goalID,
		"actor_label":          tokenClaims.ActorLabel,
		"control_session_id":   tokenClaims.ControlSessionID,
	}); err != nil {
		return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
	}
	return CapabilityResponse{
		RequestID:        capabilityRequest.RequestID,
		Status:           ResponseStatusSuccess,
		StructuredResult: structuredResult,
		FieldsMeta:       fieldsMeta,
		Classification:   classification,
		Metadata:         map[string]interface{}{"goal_id": goalID},
	}
}

func (server *Server) executeGoalCloseCapability(tokenClaims capabilityToken, capabilityRequest CapabilityRequest) CapabilityResponse {
	goalID := strings.TrimSpace(capabilityRequest.Arguments["goal_id"])
	if goalID == "" {
		return CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusDenied,
			DenialReason: "goal_id must not be empty",
			DenialCode:   DenialCodeMalformedRequest,
		}
	}

	if err := server.closeGoal(tokenClaims, goalID); err != nil {
		return server.todoCapabilityErrorResponse(tokenClaims, capabilityRequest, err)
	}

	classification := ResultClassification{
		Exposure: ResultExposureDisplay,
		Eligibility: ResultEligibility{
			Prompt: true,
			Memory: false,
		},
	}
	structuredResult := map[string]interface{}{
		"content": fmt.Sprintf("Goal closed (goal_id=%s)", goalID),
		"goal_id": goalID,
	}
	fieldsMeta, fieldsMetaErr := fieldsMetadataForStructuredResult(structuredResult, ResultFieldOriginLocal, classification)
	if fieldsMetaErr != nil {
		return CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusError,
			DenialReason: "internal result metadata error",
			DenialCode:   DenialCodeExecutionFailed,
			Redacted:     true,
		}
	}
	if err := server.logEvent("capability.executed", tokenClaims.ControlSessionID, map[string]interface{}{
		"request_id":         capabilityRequest.RequestID,
		"capability":         capabilityRequest.Capability,
		"status":             ResponseStatusSuccess,
		"goal_id":            goalID,
		"control_session_id": tokenClaims.ControlSessionID,
	}); err != nil {
		return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
	}
	return CapabilityResponse{
		RequestID:        capabilityRequest.RequestID,
		Status:           ResponseStatusSuccess,
		StructuredResult: structuredResult,
		FieldsMeta:       fieldsMeta,
		Classification:   classification,
	}
}

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
			InspectionID:   inspectionID,
			ThreadID:       threadID,
			Scope:          memoryScopeGlobal,
			SubmittedAtUTC: nowUTC.Format(time.RFC3339Nano),
			CompletedAtUTC: nowUTC.Format(time.RFC3339Nano),
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
			InspectionID:   inspectionID,
			ThreadID:       threadID,
			Scope:          memoryScopeGlobal,
			SubmittedAtUTC: nowUTC.Format(time.RFC3339Nano),
			CompletedAtUTC: nowUTC.Format(time.RFC3339Nano),
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
