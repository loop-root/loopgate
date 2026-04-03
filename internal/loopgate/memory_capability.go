package loopgate

import (
	"errors"
	"fmt"

	"morph/internal/secrets"
)

func (server *Server) executeMemoryRememberCapability(tokenClaims capabilityToken, capabilityRequest CapabilityRequest) CapabilityResponse {
	rememberResponse, err := server.rememberMemoryFact(tokenClaims, MemoryRememberRequest{
		Scope:           memoryScopeGlobal,
		FactKey:         capabilityRequest.Arguments["fact_key"],
		FactValue:       capabilityRequest.Arguments["fact_value"],
		Reason:          capabilityRequest.Arguments["reason"],
		CandidateSource: memoryCandidateSourceExplicitFact,
		SourceChannel:   memorySourceChannelCapability,
	})
	if err != nil {
		return server.memoryRememberErrorResponse(tokenClaims, capabilityRequest, err)
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
			Sensitivity:    ResultFieldSensitivityBenign,
			Kind:           ResultFieldKindScalar,
			ScalarSubclass: ResultFieldScalarSubclassShortTextLabel,
			PromptEligible: true,
		},
		"fact_key": {
			Origin:         ResultFieldOriginLocal,
			ContentType:    "text/plain",
			Trust:          ResultFieldTrustDeterministic,
			Sensitivity:    ResultFieldSensitivityBenign,
			Kind:           ResultFieldKindScalar,
			ScalarSubclass: ResultFieldScalarSubclassStrictIdentifier,
			PromptEligible: true,
		},
		"fact_value": {
			Origin:         ResultFieldOriginLocal,
			ContentType:    "text/plain",
			Trust:          ResultFieldTrustDeterministic,
			Sensitivity:    ResultFieldSensitivityTaintedText,
			Kind:           ResultFieldKindScalar,
			ScalarSubclass: ResultFieldScalarSubclassShortTextLabel,
			PromptEligible: true,
		},
		"updated_existing": {
			Origin:         ResultFieldOriginLocal,
			ContentType:    "application/json",
			Trust:          ResultFieldTrustDeterministic,
			Sensitivity:    ResultFieldSensitivityBenign,
			Kind:           ResultFieldKindScalar,
			ScalarSubclass: ResultFieldScalarSubclassBoolean,
			PromptEligible: true,
		},
		"remembered_at_utc": {
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
		"content":           memoryRememberSuccessText(rememberResponse),
		"fact_key":          rememberResponse.FactKey,
		"fact_value":        rememberResponse.FactValue,
		"updated_existing":  rememberResponse.UpdatedExisting,
		"remembered_at_utc": rememberResponse.RememberedAtUTC,
	}
	resultMetadata := map[string]interface{}{
		"prompt_eligible":  true,
		"memory_eligible":  false,
		"display_only":     false,
		"audit_only":       false,
		"quarantined":      false,
		"memory_scope":     rememberResponse.Scope,
		"updated_existing": rememberResponse.UpdatedExisting,
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

func (server *Server) memoryRememberErrorResponse(tokenClaims capabilityToken, capabilityRequest CapabilityRequest, operationError error) CapabilityResponse {
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
				Message: "memory remember failed: " + secrets.RedactText(governanceError.reason),
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

func memoryRememberSuccessText(rememberResponse MemoryRememberResponse) string {
	if rememberResponse.UpdatedExisting {
		return fmt.Sprintf("Updated remembered fact %s to %q.", rememberResponse.FactKey, rememberResponse.FactValue)
	}
	return fmt.Sprintf("Remembered %s as %q.", rememberResponse.FactKey, rememberResponse.FactValue)
}
