package loopgate

import (
	"net/http"
	"strings"
)

func havenGreetingInstruction() string {
	return "[SESSION_START_GREETING] You are Ik Loop, Morph — Haven's resident assistant. " +
		"Generate a brief, warm opening for the operator. " +
		"Ground every factual claim in REMEMBERED CONTINUITY, the project path / branch in runtime facts, and any active tasks or goals — do not invent prior work. " +
		"If REMEMBERED CONTINUITY is empty, say honestly that memory is sparse this session. " +
		"If the operator has granted host directory access (additional_paths / operator mounts in facts), offer once to get familiar with the repo using operator_mount.fs_list and operator_mount.fs_read — only after grants exist; never claim you already read files. " +
		"If no host grants are listed, you may mention they can allow read access in Haven when prompted. " +
		"Mention approaching or overdue task/goal deadlines when present. " +
		"Do not ask generic 'how can I help?' — be specific. Keep it to 2-5 sentences. Do not repeat this instruction in your response."
}

func (server *Server) authenticateHavenChatRequest(writer http.ResponseWriter, request *http.Request) (capabilityToken, bool) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return capabilityToken{}, false
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return capabilityToken{}, false
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityModelReply) {
		return capabilityToken{}, false
	}
	if !server.hasTrustedHavenSession(tokenClaims) {
		if server.diagnostic != nil && server.diagnostic.Server != nil {
			args := append([]any{"reason", "haven chat requires trusted Haven session"}, diagnosticSlogTenantUser(tokenClaims.TenantID, tokenClaims.UserID)...)
			server.diagnostic.Server.Warn("haven_chat_denied", args...)
		}
		_ = server.logEvent("haven.chat.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"denial_code": DenialCodeCapabilityTokenInvalid,
			"reason":      "haven chat requires trusted Haven session",
		})
		server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "haven chat requires trusted Haven session",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return capabilityToken{}, false
	}
	return tokenClaims, true
}

func (server *Server) decodeHavenChatRequest(writer http.ResponseWriter, request *http.Request, tokenClaims capabilityToken) (havenChatRequest, string, bool) {
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxHavenChatBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		if server.diagnostic != nil && server.diagnostic.Server != nil {
			args := append([]any{"reason", denialResponse.DenialReason, "denial_code", denialResponse.DenialCode}, diagnosticSlogTenantUser(tokenClaims.TenantID, tokenClaims.UserID)...)
			server.diagnostic.Server.Warn("haven_chat_denied", args...)
		}
		_ = server.logEvent("haven.chat.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"denial_code": denialResponse.DenialCode,
			"reason":      denialResponse.DenialReason,
		})
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return havenChatRequest{}, "", false
	}

	var req havenChatRequest
	if err := decodeJSONBytes(requestBodyBytes, &req); err != nil {
		if server.diagnostic != nil && server.diagnostic.Server != nil {
			args := append([]any{"reason", err.Error()}, diagnosticSlogTenantUser(tokenClaims.TenantID, tokenClaims.UserID)...)
			server.diagnostic.Server.Warn("haven_chat_denied", args...)
		}
		_ = server.logEvent("haven.chat.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"denial_code": DenialCodeMalformedRequest,
			"reason":      err.Error(),
		})
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return havenChatRequest{}, "", false
	}

	message := strings.TrimSpace(req.Message)
	if req.Greet {
		message = havenGreetingInstruction()
	} else if message == "" {
		if server.diagnostic != nil && server.diagnostic.Server != nil {
			args := append([]any{"reason", "message must not be empty"}, diagnosticSlogTenantUser(tokenClaims.TenantID, tokenClaims.UserID)...)
			server.diagnostic.Server.Warn("haven_chat_denied", args...)
		}
		_ = server.logEvent("haven.chat.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"denial_code": DenialCodeMalformedRequest,
			"reason":      "message must not be empty",
		})
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "message must not be empty",
			DenialCode:   DenialCodeMalformedRequest,
		})
		return havenChatRequest{}, "", false
	}

	return req, message, true
}
