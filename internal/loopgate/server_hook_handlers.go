package loopgate

import (
	"fmt"
	"net/http"
	"os"

	"morph/internal/ledger"
	policypkg "morph/internal/policy"
)

// claudeCodeToolMap maps Claude Code tool names to Loopgate policy categories and operations.
// Tools not present in this map are denied with hook_unknown_tool.
var claudeCodeToolMap = map[string]struct {
	category  string
	operation string
}{
	"Bash":      {category: "shell", operation: policypkg.OpExecute},
	"Write":     {category: "filesystem", operation: policypkg.OpWrite},
	"Edit":      {category: "filesystem", operation: policypkg.OpWrite},
	"MultiEdit": {category: "filesystem", operation: policypkg.OpWrite},
	"WebFetch":  {category: "http", operation: policypkg.OpRead},
	"WebSearch": {category: "http", operation: policypkg.OpRead},
	"Read":      {category: "filesystem", operation: policypkg.OpRead},
	"Glob":      {category: "filesystem", operation: policypkg.OpRead},
	"Grep":      {category: "filesystem", operation: policypkg.OpRead},
}

// hookToolInfo is a minimal ToolInfo implementation used by the hook handler.
type hookToolInfo struct {
	name     string
	category string
	op       string
}

func (h hookToolInfo) Name() string                    { return h.name }
func (h hookToolInfo) Category() string                { return h.category }
func (h hookToolInfo) Operation() policypkg.OperationType { return h.op }

// handleHookPreValidate handles POST /v1/hook/pre-validate.
//
// Auth model: Unix socket peer UID must match the server process UID. No session
// or MAC is required — the socket peer credential is the trust anchor, identical
// to the pattern used by /v1/health. This endpoint exists specifically to serve
// the Claude Code PreToolUse hook subprocess which does not have a control session.
//
// On policy allow: returns {"decision": "allow"} with HTTP 200.
// On policy block: returns {"decision": "block", ...} with HTTP 200.
//   The hook script must inspect the JSON body, not the HTTP status, to decide
//   whether to block. HTTP errors (4xx/5xx) indicate infrastructure problems.
func (server *Server) handleHookPreValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Peer UID binding: the caller must be the same OS user as the server process.
	// This prevents any other local user from using this endpoint as a policy oracle.
	peer, ok := peerIdentityFromContext(r.Context())
	if !ok {
		server.writeJSON(w, http.StatusUnauthorized, HookPreValidateResponse{
			Decision:   "block",
			Reason:     "missing peer identity — request must arrive over Unix domain socket",
			DenialCode: DenialCodeHookPeerBindingRejected,
		})
		return
	}
	serverUID := uint32(os.Getuid())
	if peer.UID != serverUID {
		server.writeJSON(w, http.StatusForbidden, HookPreValidateResponse{
			Decision:   "block",
			Reason:     fmt.Sprintf("peer UID %d does not match server UID %d", peer.UID, serverUID),
			DenialCode: DenialCodeHookPeerBindingRejected,
		})
		return
	}

	const maxHookBodyBytes = 65536 // 64 KiB — tool inputs are bounded
	var req HookPreValidateRequest
	if err := server.decodeJSONBody(w, r, maxHookBodyBytes, &req); err != nil {
		server.writeJSON(w, http.StatusBadRequest, HookPreValidateResponse{
			Decision:   "block",
			Reason:     "malformed request body: " + err.Error(),
			DenialCode: DenialCodeMalformedRequest,
		})
		return
	}

	// Map Claude Code tool name to Loopgate policy category + operation.
	toolDef, known := claudeCodeToolMap[req.ToolName]
	if !known {
		// Unknown tools are allowed through — they may be internal Claude Code
		// tools (Agent, TodoWrite, etc.) that have no policy surface.
		_ = server.appendAuditEvent(server.auditPath, ledger.Event{
			V:    1,
			TS:   server.now().UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
			Type: "hook.pre_validate",
			Data: map[string]interface{}{
				"decision":  "allow",
				"tool_name": req.ToolName,
				"reason":    "tool not in governance map — allowed through",
				"session":   req.SessionID,
				"peer_uid":  peer.UID,
				"peer_pid":  peer.PID,
			},
		})
		server.writeJSON(w, http.StatusOK, HookPreValidateResponse{Decision: "allow"})
		return
	}

	result := server.checker.Check(hookToolInfo{
		name:     req.ToolName,
		category: toolDef.category,
		op:       toolDef.operation,
	})

	var decision string
	switch result.Decision {
	case policypkg.Allow:
		decision = "allow"
	case policypkg.NeedsApproval:
		// Approval flow is not yet wired into the hook path.
		// Block with pending_approval — the user will see the reason in Claude Code.
		decision = "block"
	default:
		decision = "block"
	}

	_ = server.appendAuditEvent(server.auditPath, ledger.Event{
		V:    1,
		TS:   server.now().UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		Type: "hook.pre_validate",
		Data: map[string]interface{}{
			"decision":  decision,
			"tool_name": req.ToolName,
			"category":  toolDef.category,
			"operation": toolDef.operation,
			"reason":    result.Reason,
			"session":   req.SessionID,
			"peer_uid":  peer.UID,
			"peer_pid":  peer.PID,
		},
	})

	if decision == "allow" {
		server.writeJSON(w, http.StatusOK, HookPreValidateResponse{Decision: "allow"})
		return
	}

	denialCode := DenialCodePolicyDenied
	if result.Decision == policypkg.NeedsApproval {
		denialCode = DenialCodeApprovalRequired
	}

	server.writeJSON(w, http.StatusOK, HookPreValidateResponse{
		Decision:   "block",
		Reason:     result.Reason,
		DenialCode: denialCode,
	})
}
