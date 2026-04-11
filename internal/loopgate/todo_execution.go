package loopgate

import (
	"errors"
	"fmt"
	"strings"

	"morph/internal/secrets"
)

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
		fieldsMetaError := fmt.Errorf("todo.add fields_meta: %w", fieldsMetaErr)
		if auditErr := server.logEvent("capability.error", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"error":                secrets.RedactText(fieldsMetaError.Error()),
			"operator_error_class": secrets.LoopgateOperatorErrorClass(fieldsMetaError),
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
	truncatedUnresolved, truncatedGoals, omittedTasks, omittedGoals := truncateTodoListForStructuredPayload(listResponse)
	structuredResult := map[string]interface{}{
		"content":          todoListContentText(listResponse),
		"item_count":       len(listResponse.UnresolvedItems),
		"goal_count":       len(listResponse.ActiveGoals),
		"unresolved_items": truncatedUnresolved,
		"active_goals":     truncatedGoals,
	}
	resultMetadata := map[string]interface{}{
		"prompt_eligible": true,
		"memory_eligible": false,
		"display_only":    false,
		"audit_only":      false,
		"quarantined":     false,
		"todo_scope":      listResponse.Scope,
	}
	if omittedTasks > 0 {
		resultMetadata["todo_struct_omitted_open_tasks"] = omittedTasks
	}
	if omittedGoals > 0 {
		resultMetadata["todo_struct_omitted_active_goals"] = omittedGoals
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
		"request_id":         capabilityRequest.RequestID,
		"capability":         capabilityRequest.Capability,
		"status":             ResponseStatusSuccess,
		"goal_id":            goalID,
		"actor_label":        tokenClaims.ActorLabel,
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
