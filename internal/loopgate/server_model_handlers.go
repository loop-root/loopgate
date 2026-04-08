package loopgate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"morph/internal/identifiers"
	modelpkg "morph/internal/model"
	anthropicprovider "morph/internal/model/anthropic"
	openai "morph/internal/model/openai"
	modelruntime "morph/internal/modelruntime"
	"morph/internal/secrets"
)

func (server *Server) handleSessionOpen(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var openRequest OpenSessionRequest
	if err := server.decodeJSONBody(writer, request, maxOpenSessionBodyBytes, &openRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	openRequest.Actor = strings.TrimSpace(openRequest.Actor)
	openRequest.SessionID = strings.TrimSpace(openRequest.SessionID)
	normalizedCapabilities := normalizedCapabilityList(openRequest.RequestedCapabilities)
	openRequest.RequestedCapabilities = normalizedCapabilities
	if err := openRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if err := identifiers.ValidateSafeIdentifier("actor", defaultLabel(openRequest.Actor, "client")); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if err := identifiers.ValidateSafeIdentifier("session_id", defaultLabel(openRequest.SessionID, "session")); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if len(normalizedCapabilities) == 0 {
		server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "requested_capabilities must not be empty",
			DenialCode:   DenialCodeCapabilityScopeRequired,
		})
		return
	}

	// Security invariant: capability scope is server-granted, not client-declared.
	// The client's requested list is intersected with the server's registered capabilities.
	// Unknown capabilities are rejected; the client cannot escalate beyond what the server offers.
	grantedCapabilities, unknownCapabilities := server.filterGrantedCapabilities(normalizedCapabilities)
	if len(unknownCapabilities) > 0 {
		server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: fmt.Sprintf("unknown capabilities requested: %s", strings.Join(unknownCapabilities, ", ")),
			DenialCode:   DenialCodeCapabilityTokenScopeDenied,
		})
		return
	}
	if len(grantedCapabilities) == 0 {
		server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "no requested capabilities are available",
			DenialCode:   DenialCodeCapabilityScopeRequired,
		})
		return
	}

	requestPeerIdentity, ok := peerIdentityFromContext(request.Context())
	if !ok {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "missing authenticated peer identity",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return
	}

	if server.expectedClientPath != "" && server.resolveExePath != nil {
		exePath, exeErr := server.resolveExePath(requestPeerIdentity.PID)
		if exeErr != nil {
			server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
				Status:       ResponseStatusDenied,
				DenialReason: "cannot resolve connecting process executable",
				DenialCode:   DenialCodeProcessBindingRejected,
			})
			return
		}
		if normalizeSessionExecutablePinPath(exePath) != server.expectedClientPath {
			if server.reportSecurityWarning != nil {
				server.reportSecurityWarning("session_client_executable_mismatch", errors.New("executable path mismatch"))
			}
			server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
				Status:       ResponseStatusDenied,
				DenialReason: "connecting process does not match expected client executable",
				DenialCode:   DenialCodeProcessBindingRejected,
			})
			return
		}
	}

	nowUTC := server.now().UTC()
	server.mu.Lock()
	server.pruneExpiredLocked()

	// Idempotent re-open: if the same (UID, ClientSessionLabel) pair already has
	// an active session, close the old one before creating the replacement. This
	// prevents session accumulation from client retries, capability expansion, or
	// reconnects, while keeping audit logs unambiguous.
	clientLabel := defaultLabel(openRequest.SessionID, "session")
	for csID, existingSession := range server.sessions {
		if existingSession.PeerIdentity.UID == requestPeerIdentity.UID &&
			existingSession.ClientSessionLabel == clientLabel {
			// Revoke old session's tokens and clean up.
			for tokenString, tokenClaims := range server.tokens {
				if tokenClaims.ControlSessionID == csID {
					delete(server.tokens, tokenString)
				}
			}
			delete(server.approvalTokenIndex, approvalTokenHash(existingSession.ApprovalToken))
			delete(server.sessions, csID)
			break // at most one match per (UID, label)
		}
	}

	if server.maxTotalControlSessions > 0 && len(server.sessions) >= server.maxTotalControlSessions {
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusTooManyRequests, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "control-plane session store is at capacity",
			DenialCode:   DenialCodeControlPlaneStateSaturated,
		})
		return
	}

	if server.maxActiveSessionsPerUID > 0 && server.activeSessionsForPeerUIDLocked(requestPeerIdentity.UID) >= server.maxActiveSessionsPerUID {
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusTooManyRequests, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "active control session limit reached for this peer identity",
			DenialCode:   DenialCodeSessionActiveLimitReached,
		})
		return
	}
	if server.sessionOpenMinInterval > 0 {
		lastOpenedAtUTC := server.sessionOpenByUID[requestPeerIdentity.UID]
		if !lastOpenedAtUTC.IsZero() {
			elapsed := nowUTC.Sub(lastOpenedAtUTC)
			if elapsed < server.sessionOpenMinInterval {
				server.mu.Unlock()
				server.writeJSON(writer, http.StatusTooManyRequests, CapabilityResponse{
					Status:       ResponseStatusDenied,
					DenialReason: fmt.Sprintf("session open rate limit exceeded; retry after %s", (server.sessionOpenMinInterval - elapsed).Round(time.Millisecond)),
					DenialCode:   DenialCodeSessionOpenRateLimited,
				})
				return
			}
		}
	}

	controlSessionID, err := randomHex(16)
	if err != nil {
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "failed to create control session",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}
	capabilityTokenString, err := randomHex(24)
	if err != nil {
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "failed to mint capability token",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}
	approvalTokenString, err := randomHex(24)
	if err != nil {
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "failed to mint approval token",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}
	approvalTokenID, err := randomHex(8)
	if err != nil {
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "failed to mint approval token identifier",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}
	sessionMACKey, err := randomHex(32)
	if err != nil {
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "failed to mint session mac key",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}
	tokenID, err := randomHex(8)
	if err != nil {
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "failed to mint token identifier",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}
	expiresAt := nowUTC.Add(sessionTTL)
	deploymentTenantID := strings.TrimSpace(server.runtimeConfig.Tenancy.DeploymentTenantID)
	deploymentUserID := strings.TrimSpace(server.runtimeConfig.Tenancy.DeploymentUserID)
	tokenClaims := capabilityToken{
		TokenID:             tokenID,
		Token:               capabilityTokenString,
		ControlSessionID:    controlSessionID,
		ActorLabel:          defaultLabel(openRequest.Actor, "client"),
		ClientSessionLabel:  defaultLabel(openRequest.SessionID, "session"),
		AllowedCapabilities: capabilitySet(grantedCapabilities),
		PeerIdentity:        requestPeerIdentity,
		TenantID:            deploymentTenantID,
		UserID:              deploymentUserID,
		ExpiresAt:           expiresAt,
	}

	server.sessions[controlSessionID] = controlSession{
		ID:                    controlSessionID,
		ActorLabel:            tokenClaims.ActorLabel,
		ClientSessionLabel:    tokenClaims.ClientSessionLabel,
		WorkspaceID:           strings.TrimSpace(openRequest.WorkspaceID),
		RequestedCapabilities: capabilitySet(grantedCapabilities),
		ApprovalToken:         approvalTokenString,
		ApprovalTokenID:       approvalTokenID,
		SessionMACKey:         sessionMACKey,
		PeerIdentity:          requestPeerIdentity,
		TenantID:              deploymentTenantID,
		UserID:                deploymentUserID,
		ExpiresAt:             expiresAt,
		CreatedAt:             nowUTC,
	}
	server.tokens[capabilityTokenString] = tokenClaims
	server.approvalTokenIndex[approvalTokenHash(approvalTokenString)] = controlSessionID
	server.sessionOpenByUID[requestPeerIdentity.UID] = nowUTC
	server.noteExpiryCandidateLocked(expiresAt)
	server.mu.Unlock()

	if err := server.logEvent("session.opened", controlSessionID, map[string]interface{}{
		"actor_label":                tokenClaims.ActorLabel,
		"client_session_label":       tokenClaims.ClientSessionLabel,
		"control_session_id":         controlSessionID,
		"workspace_id":               strings.TrimSpace(openRequest.WorkspaceID),
		"requested_capability_count": len(normalizedCapabilities),
		"granted_capability_count":   len(grantedCapabilities),
		"token_id":                   tokenID,
		"approval_token_id":          approvalTokenID,
		"peer_uid":                   requestPeerIdentity.UID,
		"peer_pid":                   requestPeerIdentity.PID,
		"peer_epid":                  requestPeerIdentity.EPID,
		"expires_at_utc":             expiresAt.Format(time.RFC3339Nano),
	}); err != nil {
		server.mu.Lock()
		delete(server.sessions, controlSessionID)
		delete(server.tokens, capabilityTokenString)
		delete(server.approvalTokenIndex, approvalTokenHash(approvalTokenString))
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "control-plane audit is unavailable",
			DenialCode:   DenialCodeAuditUnavailable,
		})
		return
	}

	server.writeJSON(writer, http.StatusOK, OpenSessionResponse{
		ControlSessionID: controlSessionID,
		CapabilityToken:  capabilityTokenString,
		ApprovalToken:    approvalTokenString,
		SessionMACKey:    sessionMACKey,
		ExpiresAtUTC:     expiresAt.Format(time.RFC3339Nano),
	})
	personaName, personaVersion := server.loadPersonaDisplaySummary()
	server.emitUIEvent(controlSessionID, UIEventTypeSessionInfo, UIEventSessionInfo{
		ControlSessionID:   controlSessionID,
		ActorLabel:         tokenClaims.ActorLabel,
		ClientSessionLabel: tokenClaims.ClientSessionLabel,
		PersonaName:        personaName,
		PersonaVersion:     personaVersion,
	})
}

func (server *Server) handleModelReply(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}

	verifyRequestStart := time.Now()
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxModelReplyBodyBytes, tokenClaims.ControlSessionID)
	verifyRequestDuration := time.Since(verifyRequestStart)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var modelRequest modelpkg.Request
	if err := decodeJSONBytes(requestBodyBytes, &modelRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	loadRuntimeConfigStart := time.Now()
	runtimeConfig, err := modelruntime.LoadConfig(server.repoRoot)
	loadRuntimeConfigDuration := time.Since(loadRuntimeConfigStart)
	if err != nil {
		server.writeModelErrorResponse(writer, tokenClaims, runtimeConfig, modelpkg.Response{}, modelAuditTimings{
			RequestVerify:     verifyRequestDuration,
			RuntimeConfigLoad: loadRuntimeConfigDuration,
		}, fmt.Errorf("load model runtime config: %w", err))
		return
	}

	initializeModelClientStart := time.Now()
	modelClient, validatedRuntimeConfig, err := server.newModelClientFromConfig(runtimeConfig)
	initializeModelClientDuration := time.Since(initializeModelClientStart)
	if err != nil {
		server.writeModelErrorResponse(writer, tokenClaims, runtimeConfig, modelpkg.Response{}, modelAuditTimings{
			RequestVerify:     verifyRequestDuration,
			RuntimeConfigLoad: loadRuntimeConfigDuration,
			ModelClientInit:   initializeModelClientDuration,
		}, fmt.Errorf("initialize model runtime: %w", err))
		return
	}

	modelGenerateStart := time.Now()
	modelResponse, err := modelClient.Reply(request.Context(), modelRequest)
	modelGenerateDuration := time.Since(modelGenerateStart)
	if err != nil {
		server.writeModelErrorResponse(writer, tokenClaims, validatedRuntimeConfig, modelResponse, modelAuditTimings{
			RequestVerify:     verifyRequestDuration,
			RuntimeConfigLoad: loadRuntimeConfigDuration,
			ModelClientInit:   initializeModelClientDuration,
			ModelGenerate:     modelGenerateDuration,
		}, fmt.Errorf("model inference failed: %w", err))
		return
	}

	modelResponseAuditData := map[string]interface{}{
		"provider":              modelResponse.ProviderName,
		"model":                 modelResponse.ModelName,
		"finish_reason":         modelResponse.FinishReason,
		"persona_hash":          modelResponse.Prompt.PersonaHash,
		"policy_hash":           modelResponse.Prompt.PolicyHash,
		"prompt_hash":           modelResponse.Prompt.PromptHash,
		"input_tokens":          modelResponse.Usage.InputTokens,
		"output_tokens":         modelResponse.Usage.OutputTokens,
		"total_tokens":          modelResponse.Usage.TotalTokens,
		"cached_input_tokens":   modelResponse.Usage.CachedInputTokens,
		"request_payload_bytes": modelResponse.RequestPayloadBytes,
		"control_session_id":    tokenClaims.ControlSessionID,
		"actor_label":           tokenClaims.ActorLabel,
		"client_session_label":  tokenClaims.ClientSessionLabel,
	}
	modelTimings := modelAuditTimings{
		RequestVerify:     verifyRequestDuration,
		RuntimeConfigLoad: loadRuntimeConfigDuration,
		ModelClientInit:   initializeModelClientDuration,
		ModelGenerate:     modelGenerateDuration,
		PromptCompile:     modelResponse.Timing.PromptCompile,
		SecretResolve:     modelResponse.Timing.SecretResolve,
		ProviderRoundTrip: modelResponse.Timing.ProviderRoundTrip,
		ResponseDecode:    modelResponse.Timing.ResponseDecode,
		TotalGenerate:     modelResponse.Timing.TotalGenerate,
	}
	for timingKey, timingValue := range modelTimings.toAuditFields() {
		modelResponseAuditData[timingKey] = timingValue
	}
	if auditErr := server.logEvent("model.response", tokenClaims.ControlSessionID, modelResponseAuditData); auditErr != nil {
		server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "control-plane audit is unavailable",
			DenialCode:   DenialCodeAuditUnavailable,
		})
		return
	}

	server.writeJSON(writer, http.StatusOK, modelResponse)
}

func (server *Server) handleModelValidate(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}

	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxApprovalBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var validateRequest ModelValidateRequest
	if err := decodeJSONBytes(requestBodyBytes, &validateRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	validatedConfig, err := server.validateModelConfig(request.Context(), validateRequest.RuntimeConfig)
	if err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}

	if auditErr := server.logEvent("model.config_validated", tokenClaims.ControlSessionID, map[string]interface{}{
		"provider":             validatedConfig.ProviderName,
		"model":                validatedConfig.ModelName,
		"base_url":             validatedConfig.BaseURL,
		"model_connection_id":  validatedConfig.ModelConnectionID,
		"legacy_api_key_env":   validatedConfig.APIKeyEnvVar,
		"control_session_id":   tokenClaims.ControlSessionID,
		"actor_label":          tokenClaims.ActorLabel,
		"client_session_label": tokenClaims.ClientSessionLabel,
	}); auditErr != nil {
		server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "control-plane audit is unavailable",
			DenialCode:   DenialCodeAuditUnavailable,
		})
		return
	}

	server.writeJSON(writer, http.StatusOK, ModelValidateResponse{
		RuntimeConfig: validatedConfig,
	})
}

func (server *Server) handleModelConnectionStore(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxApprovalBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var storeRequest ModelConnectionStoreRequest
	if err := decodeJSONBytes(requestBodyBytes, &storeRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	connectionStatus, err := server.StoreModelConnection(request.Context(), storeRequest)
	if err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: redactSiteTrustError(err),
			DenialCode:   DenialCodeExecutionFailed,
			Redacted:     true,
		})
		return
	}

	if auditErr := server.logEvent("model.connection_validated", tokenClaims.ControlSessionID, map[string]interface{}{
		"connection_id":        connectionStatus.ConnectionID,
		"provider":             connectionStatus.ProviderName,
		"base_url":             connectionStatus.BaseURL,
		"secure_store_ref_id":  connectionStatus.SecureStoreRefID,
		"control_session_id":   tokenClaims.ControlSessionID,
		"actor_label":          tokenClaims.ActorLabel,
		"client_session_label": tokenClaims.ClientSessionLabel,
	}); auditErr != nil {
		server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "control-plane audit is unavailable",
			DenialCode:   DenialCodeAuditUnavailable,
		})
		return
	}

	server.writeJSON(writer, http.StatusOK, connectionStatus)
}

func (server *Server) validateModelConfig(ctx context.Context, runtimeConfig modelruntime.Config) (modelruntime.Config, error) {
	validatedConfig, err := modelruntime.ValidateConfig(ctx, runtimeConfig)
	if err != nil {
		return modelruntime.Config{}, err
	}
	if validatedConfig.ProviderName != "openai_compatible" && validatedConfig.ProviderName != "anthropic" {
		return validatedConfig, nil
	}
	if validatedConfig.ModelConnectionID != "" {
		if _, err := server.ValidateModelConnection(ctx, validatedConfig.ModelConnectionID); err != nil {
			return modelruntime.Config{}, err
		}
		return validatedConfig, nil
	}
	if validatedConfig.APIKeyEnvVar != "" {
		return validatedConfig, nil
	}
	if validatedConfig.ProviderName == "openai_compatible" && modelruntime.IsLoopbackModelBaseURL(validatedConfig.BaseURL) {
		return validatedConfig, nil
	}
	if validatedConfig.ProviderName == "anthropic" {
		return modelruntime.Config{}, fmt.Errorf("anthropic provider requires model_connection_id or legacy api_key env")
	}
	return modelruntime.Config{}, fmt.Errorf("openai_compatible provider requires model_connection_id or legacy api_key env for non-localhost base url")
}

func (server *Server) newModelClientFromRuntimeConfig(runtimeConfig modelruntime.Config) (*modelpkg.Client, modelruntime.Config, error) {
	validatedConfig, err := modelruntime.NormalizeConfig(runtimeConfig)
	if err != nil {
		return nil, modelruntime.Config{}, err
	}
	switch validatedConfig.ProviderName {
	case "stub":
		return modelpkg.NewClient(modelpkg.NewStubProvider()), validatedConfig, nil
	case "anthropic":
		if validatedConfig.ModelConnectionID != "" {
			modelConnectionRecord, err := server.resolveModelConnection(validatedConfig.ModelConnectionID)
			if err != nil {
				return nil, modelruntime.Config{}, err
			}
			secretStore, err := server.secretStoreForRef(modelConnectionRecord.Credential)
			if err != nil {
				return nil, modelruntime.Config{}, err
			}
			provider, err := anthropicprovider.NewProvider(anthropicprovider.Config{
				BaseURL:         validatedConfig.BaseURL,
				ModelName:       validatedConfig.ModelName,
				Temperature:     validatedConfig.Temperature,
				MaxOutputTokens: validatedConfig.MaxOutputTokens,
				Timeout:         validatedConfig.Timeout,
				APIKeyRef:       modelConnectionRecord.Credential,
				SecretStore:     secretStore,
			})
			if err != nil {
				return nil, modelruntime.Config{}, err
			}
			return modelpkg.NewClient(provider), validatedConfig, nil
		}
		return modelruntime.NewClientFromConfig(validatedConfig)
	case "openai_compatible":
		if validatedConfig.ModelConnectionID != "" {
			modelConnectionRecord, err := server.resolveModelConnection(validatedConfig.ModelConnectionID)
			if err != nil {
				return nil, modelruntime.Config{}, err
			}
			secretStore, err := server.secretStoreForRef(modelConnectionRecord.Credential)
			if err != nil {
				return nil, modelruntime.Config{}, err
			}
			provider, err := openai.NewProvider(openai.Config{
				BaseURL:         validatedConfig.BaseURL,
				ModelName:       validatedConfig.ModelName,
				Temperature:     validatedConfig.Temperature,
				MaxOutputTokens: validatedConfig.MaxOutputTokens,
				Timeout:         validatedConfig.Timeout,
				APIKeyRef:       modelConnectionRecord.Credential,
				SecretStore:     secretStore,
			})
			if err != nil {
				return nil, modelruntime.Config{}, err
			}
			return modelpkg.NewClient(provider), validatedConfig, nil
		}
		if modelruntime.IsLoopbackModelBaseURL(validatedConfig.BaseURL) && validatedConfig.APIKeyEnvVar == "" {
			provider, err := openai.NewProvider(openai.Config{
				BaseURL:         validatedConfig.BaseURL,
				ModelName:       validatedConfig.ModelName,
				Temperature:     validatedConfig.Temperature,
				MaxOutputTokens: validatedConfig.MaxOutputTokens,
				Timeout:         validatedConfig.Timeout,
				NoAuth:          true,
			})
			if err != nil {
				return nil, modelruntime.Config{}, err
			}
			return modelpkg.NewClient(provider), validatedConfig, nil
		}
		return modelruntime.NewClientFromConfig(validatedConfig)
	default:
		return nil, modelruntime.Config{}, fmt.Errorf("unsupported MORPH_MODEL_PROVIDER %q", validatedConfig.ProviderName)
	}
}

func (server *Server) writeModelErrorResponse(writer http.ResponseWriter, tokenClaims capabilityToken, runtimeConfig modelruntime.Config, partialResponse modelpkg.Response, timingBreakdown modelAuditTimings, modelErr error) {
	modelErrorAuditData := map[string]interface{}{
		"provider":             runtimeConfig.ProviderName,
		"model":                runtimeConfig.ModelName,
		"persona_hash":         partialResponse.Prompt.PersonaHash,
		"policy_hash":          partialResponse.Prompt.PolicyHash,
		"prompt_hash":          partialResponse.Prompt.PromptHash,
		"error":                secrets.RedactText(modelErr.Error()),
		"operator_error_class": secrets.LoopgateOperatorErrorClass(modelErr),
		"control_session_id":   tokenClaims.ControlSessionID,
		"actor_label":          tokenClaims.ActorLabel,
		"client_session_label": tokenClaims.ClientSessionLabel,
	}
	for timingKey, timingValue := range timingBreakdown.toAuditFields() {
		modelErrorAuditData[timingKey] = timingValue
	}
	if auditErr := server.logEvent("model.error", tokenClaims.ControlSessionID, modelErrorAuditData); auditErr != nil {
		server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "control-plane audit is unavailable",
			DenialCode:   DenialCodeAuditUnavailable,
		})
		return
	}

	server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
		Status:       ResponseStatusError,
		DenialReason: modelErr.Error(),
		DenialCode:   DenialCodeExecutionFailed,
	})
}

type modelAuditTimings struct {
	RequestVerify     time.Duration
	RuntimeConfigLoad time.Duration
	ModelClientInit   time.Duration
	ModelGenerate     time.Duration
	PromptCompile     time.Duration
	SecretResolve     time.Duration
	ProviderRoundTrip time.Duration
	ResponseDecode    time.Duration
	TotalGenerate     time.Duration
}

func (timingBreakdown modelAuditTimings) toAuditFields() map[string]interface{} {
	return map[string]interface{}{
		"request_verify_ms":      durationMilliseconds(timingBreakdown.RequestVerify),
		"runtime_config_load_ms": durationMilliseconds(timingBreakdown.RuntimeConfigLoad),
		"model_client_init_ms":   durationMilliseconds(timingBreakdown.ModelClientInit),
		"model_generate_ms":      durationMilliseconds(timingBreakdown.ModelGenerate),
		"prompt_compile_ms":      durationMilliseconds(timingBreakdown.PromptCompile),
		"secret_resolve_ms":      durationMilliseconds(timingBreakdown.SecretResolve),
		"provider_roundtrip_ms":  durationMilliseconds(timingBreakdown.ProviderRoundTrip),
		"response_decode_ms":     durationMilliseconds(timingBreakdown.ResponseDecode),
		"total_generate_ms":      durationMilliseconds(timingBreakdown.TotalGenerate),
	}
}

func durationMilliseconds(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	return duration.Milliseconds()
}
