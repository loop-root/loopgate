package loopgate

import (
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"

	"loopgate/internal/secrets"
)

func (server *Server) handleHealth(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	server.writeJSON(writer, http.StatusOK, controlapipkg.HealthResponse{
		Version: statusVersion,
		OK:      true,
	})
}

func (server *Server) handleSessionMACKeys(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	response := server.buildSessionMACKeysResponse(tokenClaims.ControlSessionID)
	server.writeJSON(writer, http.StatusOK, response)
}

func (server *Server) handleStatus(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	server.mu.Lock()
	server.pruneExpiredLocked()
	pendingCount := 0
	for _, pendingApproval := range server.approvalState.records {
		if pendingApproval.State == "pending" {
			pendingCount++
		}
	}
	server.mu.Unlock()

	response := controlapipkg.StatusResponse{
		Version: statusVersion,
		Policy:  server.currentPolicyRuntime().policy,
		// Keep control-plane route scopes separate from executable tool capabilities.
		// Mixing them together makes session bootstrap and operator inspection harder to reason about.
		Capabilities:        server.capabilitySummaries(),
		ControlCapabilities: controlCapabilitySummaries(),
		PendingApprovals:    pendingCount,
	}
	if capabilityScopeAllowed(tokenClaims, controlCapabilityConnectionRead) {
		response.Connections = server.connectionStatuses()
	}
	server.writeJSON(writer, http.StatusOK, response)
}

func (server *Server) handleConnectionsStatus(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityConnectionRead) {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	server.writeJSON(writer, http.StatusOK, controlapipkg.ConnectionsStatusResponse{
		Connections: server.connectionStatuses(),
	})
}

func (server *Server) handleConnectionValidate(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityConnectionWrite) {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxApprovalBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var validateRequest controlapipkg.ConnectionKeyRequest
	if err := decodeJSONBytes(requestBodyBytes, &validateRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   controlapipkg.DenialCodeMalformedRequest,
		})
		return
	}
	connectionStatus, err := server.ValidateConnection(request.Context(), validateRequest.Provider, validateRequest.Subject)
	if err != nil {
		server.writeJSON(writer, http.StatusBadRequest, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: secrets.RedactText(err.Error()),
			DenialCode:   controlapipkg.DenialCodeExecutionFailed,
			Redacted:     true,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, connectionStatus)
}

func (server *Server) handleConnectionPKCEStart(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityConnectionWrite) {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxApprovalBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var startRequest controlapipkg.PKCEStartRequest
	if err := decodeJSONBytes(requestBodyBytes, &startRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   controlapipkg.DenialCodeMalformedRequest,
		})
		return
	}
	startResponse, err := server.startPKCEConnection(request.Context(), tokenClaims, startRequest)
	if err != nil {
		server.writeJSON(writer, http.StatusBadRequest, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: secrets.RedactText(err.Error()),
			DenialCode:   controlapipkg.DenialCodeExecutionFailed,
			Redacted:     true,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, startResponse)
}

func (server *Server) handleConnectionPKCEComplete(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityConnectionWrite) {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var completeRequest controlapipkg.PKCECompleteRequest
	if err := decodeJSONBytes(requestBodyBytes, &completeRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   controlapipkg.DenialCodeMalformedRequest,
		})
		return
	}
	connectionStatus, err := server.completePKCEConnection(request.Context(), tokenClaims, completeRequest)
	if err != nil {
		server.writeJSON(writer, http.StatusBadRequest, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: secrets.RedactText(err.Error()),
			DenialCode:   controlapipkg.DenialCodeExecutionFailed,
			Redacted:     true,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, connectionStatus)
}

func (server *Server) handleSiteInspect(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilitySiteInspect) {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxApprovalBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var inspectionRequest controlapipkg.SiteInspectionRequest
	if err := decodeJSONBytes(requestBodyBytes, &inspectionRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   controlapipkg.DenialCodeMalformedRequest,
		})
		return
	}
	if err := inspectionRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   controlapipkg.DenialCodeMalformedRequest,
		})
		return
	}

	inspectionResponse, err := server.inspectSite(request.Context(), inspectionRequest.URL)
	if err != nil {
		server.writeJSON(writer, siteInspectionHTTPStatus(err), controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: redactSiteTrustError(err),
			DenialCode:   siteTrustDenialCode(err),
			Redacted:     true,
		})
		return
	}
	if err := server.logEvent("site.inspected", tokenClaims.ControlSessionID, map[string]interface{}{
		"normalized_url":       inspectionResponse.NormalizedURL,
		"scheme":               inspectionResponse.Scheme,
		"host":                 inspectionResponse.Host,
		"path":                 inspectionResponse.Path,
		"http_status_code":     inspectionResponse.HTTPStatusCode,
		"content_type":         inspectionResponse.ContentType,
		"https":                inspectionResponse.HTTPS,
		"tls_valid":            inspectionResponse.TLSValid,
		"trust_draft_allowed":  inspectionResponse.TrustDraftAllowed,
		"control_session_id":   tokenClaims.ControlSessionID,
		"actor_label":          tokenClaims.ActorLabel,
		"client_session_label": tokenClaims.ClientSessionLabel,
	}); err != nil {
		server.writeJSON(writer, http.StatusServiceUnavailable, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "control-plane audit is unavailable",
			DenialCode:   controlapipkg.DenialCodeAuditUnavailable,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, inspectionResponse)
}

func (server *Server) handleSiteTrustDraft(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilitySiteTrustWrite) {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxApprovalBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var trustDraftRequest controlapipkg.SiteTrustDraftRequest
	if err := decodeJSONBytes(requestBodyBytes, &trustDraftRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   controlapipkg.DenialCodeMalformedRequest,
		})
		return
	}
	if err := trustDraftRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   controlapipkg.DenialCodeMalformedRequest,
		})
		return
	}

	trustDraftResponse, err := server.createSiteTrustDraft(request.Context(), tokenClaims, trustDraftRequest.URL)
	if err != nil {
		server.writeJSON(writer, siteInspectionHTTPStatus(err), controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: redactSiteTrustError(err),
			DenialCode:   siteTrustDenialCode(err),
			Redacted:     true,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, trustDraftResponse)
}
