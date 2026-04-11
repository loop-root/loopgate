package loopgate

import (
	"net/http"
)

func (server *Server) handleQuarantineMetadata(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityQuarantineRead) {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxApprovalBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var metadataRequest QuarantineLookupRequest
	if err := decodeJSONBytes(requestBodyBytes, &metadataRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if err := metadataRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	metadataResponse, err := server.quarantineMetadata(metadataRequest.QuarantineRef)
	if err != nil {
		server.writeJSON(writer, quarantineHTTPStatus(err), CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: redactQuarantineError(err),
			DenialCode:   quarantineDenialCode(err),
			Redacted:     true,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, metadataResponse)
}

func (server *Server) handleQuarantineView(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityQuarantineRead) {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxApprovalBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var viewRequest QuarantineLookupRequest
	if err := decodeJSONBytes(requestBodyBytes, &viewRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if err := viewRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	viewResponse, err := server.viewQuarantinedPayload(viewRequest.QuarantineRef)
	if err != nil {
		server.writeJSON(writer, quarantineHTTPStatus(err), CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: redactQuarantineError(err),
			DenialCode:   quarantineDenialCode(err),
			Redacted:     true,
		})
		return
	}
	if err := server.logEvent("artifact.viewed", tokenClaims.ControlSessionID, map[string]interface{}{
		"quarantine_ref":       viewResponse.Metadata.QuarantineRef,
		"content_sha256":       viewResponse.Metadata.ContentSHA256,
		"blob_size_bytes":      viewResponse.Metadata.SizeBytes,
		"storage_state":        viewResponse.Metadata.StorageState,
		"content_type":         viewResponse.Metadata.ContentType,
		"control_session_id":   tokenClaims.ControlSessionID,
		"actor_label":          tokenClaims.ActorLabel,
		"client_session_label": tokenClaims.ClientSessionLabel,
	}); err != nil {
		server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "control-plane audit is unavailable",
			DenialCode:   DenialCodeAuditUnavailable,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, viewResponse)
}

func (server *Server) handleQuarantinePrune(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityQuarantineWrite) {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxApprovalBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var pruneRequest QuarantineLookupRequest
	if err := decodeJSONBytes(requestBodyBytes, &pruneRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if err := pruneRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	metadataResponse, err := server.pruneQuarantinedPayloadAndLoadMetadata(pruneRequest.QuarantineRef, "operator_requested")
	if err != nil {
		server.writeJSON(writer, quarantineHTTPStatus(err), CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: redactQuarantineError(err),
			DenialCode:   quarantineDenialCode(err),
			Redacted:     true,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, metadataResponse)
}
