package loopgate

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"morph/internal/secrets"
)

const (
	maxHavenAgentWorkBodyBytes = 8 * 1024
	havenAgentTodoSourceKind   = "haven_agent"
)

func (server *Server) handleHavenAgentWorkItemEnsure(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(tokenClaims.ActorLabel), "haven") {
		server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "agent work-item ensure requires actor haven",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return
	}
	if !capabilityScopeAllowed(tokenClaims, "todo.add") {
		server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "capability todo.add is not granted to this session",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return
	}

	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxHavenAgentWorkBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var req HavenAgentWorkEnsureRequest
	if err := decodeJSONBytes(requestBodyBytes, &req); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	text := strings.TrimSpace(req.Text)
	if text == "" {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "text is required",
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	capReq := CapabilityRequest{
		RequestID: fmt.Sprintf("haven-agent-ensure-%d", time.Now().UTC().UnixNano()),
		SessionID: tokenClaims.ControlSessionID,
		Actor:     tokenClaims.ActorLabel,
		Capability: "todo.add",
		Arguments: map[string]string{
			"text":        text,
			"task_kind":   taskKindCarryOver,
			"source_kind": havenAgentTodoSourceKind,
			"next_step":   strings.TrimSpace(req.NextStep),
		},
	}

	ctx := request.Context()
	resp := server.executeCapabilityRequest(ctx, tokenClaims, capReq, true)
	if resp.Status != ResponseStatusSuccess {
		status := http.StatusBadRequest
		if resp.Status == ResponseStatusDenied {
			status = http.StatusForbidden
		}
		if resp.Status == ResponseStatusError {
			status = http.StatusInternalServerError
		}
		server.writeJSON(writer, status, resp)
		return
	}

	itemID, okID := havenStructuredResultString(resp.StructuredResult, "item_id")
	textOut, _ := havenStructuredResultString(resp.StructuredResult, "text")
	if !okID || strings.TrimSpace(itemID) == "" {
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "todo.add succeeded without item_id in structured result",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}
	already, _ := havenStructuredResultBool(resp.StructuredResult, "already_present")
	if textOut == "" {
		textOut = text
	}

	_ = server.logEvent("haven.agent_work_ensure", tokenClaims.ControlSessionID, map[string]interface{}{
		"item_id":              itemID,
		"already_present":      already,
		"control_session_id":   tokenClaims.ControlSessionID,
		"text_len":             len([]rune(text)),
	})

	server.writeJSON(writer, http.StatusOK, HavenAgentWorkItemResponse{
		ItemID:         itemID,
		Text:           textOut,
		AlreadyPresent: already,
	})
}

func (server *Server) handleHavenAgentWorkItemComplete(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(tokenClaims.ActorLabel), "haven") {
		server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "agent work-item complete requires actor haven",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return
	}
	if !capabilityScopeAllowed(tokenClaims, "todo.complete") {
		server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "capability todo.complete is not granted to this session",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return
	}

	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxHavenAgentWorkBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var req HavenAgentWorkCompleteRequest
	if err := decodeJSONBytes(requestBodyBytes, &req); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	itemID := strings.TrimSpace(req.ItemID)
	if itemID == "" {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "item_id is required",
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "haven_agent_work_completed"
	}

	capReq := CapabilityRequest{
		RequestID:  fmt.Sprintf("haven-agent-complete-%d", time.Now().UTC().UnixNano()),
		SessionID:  tokenClaims.ControlSessionID,
		Actor:      tokenClaims.ActorLabel,
		Capability: "todo.complete",
		Arguments: map[string]string{
			"item_id": itemID,
			"reason":  reason,
		},
	}

	ctx := request.Context()
	resp := server.executeCapabilityRequest(ctx, tokenClaims, capReq, true)
	if resp.Status != ResponseStatusSuccess {
		status := http.StatusBadRequest
		if resp.Status == ResponseStatusDenied {
			status = http.StatusForbidden
		}
		if resp.Status == ResponseStatusError {
			status = http.StatusInternalServerError
		}
		server.writeJSON(writer, status, resp)
		return
	}

	outID, _ := havenStructuredResultString(resp.StructuredResult, "item_id")
	textOut, _ := havenStructuredResultString(resp.StructuredResult, "text")
	if strings.TrimSpace(outID) == "" {
		outID = itemID
	}

	_ = server.logEvent("haven.agent_work_complete", tokenClaims.ControlSessionID, map[string]interface{}{
		"item_id":            outID,
		"control_session_id": tokenClaims.ControlSessionID,
		"reason":             secrets.RedactText(reason),
	})

	server.writeJSON(writer, http.StatusOK, HavenAgentWorkItemResponse{
		ItemID:         outID,
		Text:           textOut,
		AlreadyPresent: false,
	})
}

func havenStructuredResultString(m map[string]interface{}, key string) (string, bool) {
	if m == nil {
		return "", false
	}
	v, ok := m[key]
	if !ok || v == nil {
		return "", false
	}
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		return s, s != ""
	default:
		s := strings.TrimSpace(fmt.Sprint(t))
		return s, s != ""
	}
}

func havenStructuredResultBool(m map[string]interface{}, key string) (bool, bool) {
	if m == nil {
		return false, false
	}
	v, ok := m[key]
	if !ok || v == nil {
		return false, false
	}
	switch t := v.(type) {
	case bool:
		return t, true
	default:
		return false, false
	}
}
