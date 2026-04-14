package loopgate

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	policypkg "loopgate/internal/policy"
	"loopgate/internal/secrets"
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

func (h hookToolInfo) Name() string                       { return h.name }
func (h hookToolInfo) Category() string                   { return h.category }
func (h hookToolInfo) Operation() policypkg.OperationType { return h.op }

// handleHookPreValidate handles POST /v1/hook/pre-validate.
//
// Auth model: Unix socket peer UID must match the server process UID. No session
// or MAC is required — the socket peer credential is the trust anchor, identical
// to the pattern used by /v1/health. This endpoint exists specifically to serve
// the Claude Code PreToolUse hook subprocess which does not have a control session.
//
// On policy allow: returns {"decision": "allow"} with HTTP 200.
// On policy approval ask: returns {"decision": "ask", ...} with HTTP 200.
// On policy block: returns {"decision": "block", ...} with HTTP 200.
//
//	The hook script must inspect the JSON body, not the HTTP status, to decide
//	whether to block. HTTP errors (4xx/5xx) indicate infrastructure problems.
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
	hookEventName := normalizedClaudeCodeHookEventName(req.HookEventName)
	hookSurfaceClass := classifyClaudeCodeHookEvent(hookEventName)
	hookHandlingMode := hookHandlingModeForClaudeCodeHookEvent(hookEventName)
	policyRuntime := server.currentPolicyRuntime()
	includeHookAuditPreviews := policyRuntime.policy.HookAuditProjectionIncludesPreviews()
	if hookEventName != claudeCodeHookEventPreToolUse {
		decision := "allow"
		reason := "observability-only hook event recorded without policy enforcement"
		denialCode := ""
		additionalContext := ""
		additionalContextBytes := 0
		hookApprovalRequestID := ""
		hookApprovalState := ""
		hookApprovalSurface := ""
		sessionEndAbandonedApprovals := 0

		var (
			claudeHookSessionRecord claudeHookSessionRecord
			hookApprovalFound       bool
			hookApprovalRecord      claudeHookApprovalRecord
			claudeHookSessionErr    error
		)
		switch hookEventName {
		case claudeCodeHookEventSessionEnd:
			var previousApprovalRecords map[string]claudeHookApprovalRecord
			sessionEndAbandonedApprovals, claudeHookSessionRecord, previousApprovalRecords, claudeHookSessionErr = server.abandonPendingClaudeHookApprovalsWithPrevious(req.SessionID, req.HookReason)
			if claudeHookSessionErr == nil && sessionEndAbandonedApprovals > 0 {
				approvalCancelledAuditData := mergeHookAuditProjection(map[string]interface{}{
					"approval_surface":       claudeHookApprovalSurfaceInlineClaude,
					"approval_class":         "claude_builtin_inline",
					"approval_state":         claudeHookApprovalStateAbandoned,
					"control_session_id":     req.SessionID,
					"client_session_label":   req.SessionID,
					"actor_ref":              formatHookAuditActor(peer.UID, req.SessionID),
					"reason":                 "session ended before inline Claude approval was consumed",
					"hook_reason":            req.HookReason,
					"abandoned_count":        sessionEndAbandonedApprovals,
					"claude_hook_session_id": claudeHookSessionRecord.SessionID,
				}, req, server.repoRoot, includeHookAuditPreviews)
				if err := server.logEvent("approval.cancelled", req.SessionID, approvalCancelledAuditData); err != nil {
					_ = server.restoreClaudeHookApprovalState(req.SessionID, previousApprovalRecords)
					http.Error(w, "audit unavailable: required append failed before hook decision", http.StatusInternalServerError)
					return
				}
			}
		case claudeCodeHookEventPostToolUse:
			var approvalChanged bool
			var previousApprovalRecords map[string]claudeHookApprovalRecord
			hookApprovalRecord, claudeHookSessionRecord, hookApprovalFound, approvalChanged, previousApprovalRecords, claudeHookSessionErr = server.transitionClaudeHookApproval(req, claudeHookApprovalStateExecuted, "Claude tool execution completed after local approval")
			if claudeHookSessionErr == nil && hookApprovalFound && approvalChanged {
				approvalGrantedAuditData := mergeHookAuditProjection(map[string]interface{}{
					"approval_request_id":     hookApprovalRecord.ApprovalRequestID,
					"approval_surface":        hookApprovalRecord.ApprovalSurface,
					"approval_class":          "claude_builtin_inline",
					"approval_state":          hookApprovalRecord.State,
					"capability":              req.ToolName,
					"tool_name":               req.ToolName,
					"tool_use_id":             req.ToolUseID,
					"control_session_id":      req.SessionID,
					"client_session_label":    req.SessionID,
					"actor_ref":               formatHookAuditActor(peer.UID, req.SessionID),
					"hook_event_name":         hookEventName,
					"post_execution_observed": true,
					"reason":                  hookApprovalRecord.Reason,
				}, req, server.repoRoot, includeHookAuditPreviews)
				if err := server.logEvent("approval.granted", req.SessionID, approvalGrantedAuditData); err != nil {
					_ = server.restoreClaudeHookApprovalState(req.SessionID, previousApprovalRecords)
					http.Error(w, "audit unavailable: required append failed before hook decision", http.StatusInternalServerError)
					return
				}
			}
		case claudeCodeHookEventPostToolUseFailure:
			resolutionReason := "Claude tool execution failed after local approval"
			if strings.TrimSpace(req.HookError) != "" {
				resolutionReason = req.HookError
			}
			var approvalChanged bool
			var previousApprovalRecords map[string]claudeHookApprovalRecord
			hookApprovalRecord, claudeHookSessionRecord, hookApprovalFound, approvalChanged, previousApprovalRecords, claudeHookSessionErr = server.transitionClaudeHookApproval(req, claudeHookApprovalStateExecutionFailed, resolutionReason)
			if claudeHookSessionErr == nil && hookApprovalFound && approvalChanged {
				approvalGrantedAuditData := mergeHookAuditProjection(map[string]interface{}{
					"approval_request_id":     hookApprovalRecord.ApprovalRequestID,
					"approval_surface":        hookApprovalRecord.ApprovalSurface,
					"approval_class":          "claude_builtin_inline",
					"approval_state":          hookApprovalRecord.State,
					"capability":              req.ToolName,
					"tool_name":               req.ToolName,
					"tool_use_id":             req.ToolUseID,
					"control_session_id":      req.SessionID,
					"client_session_label":    req.SessionID,
					"actor_ref":               formatHookAuditActor(peer.UID, req.SessionID),
					"hook_event_name":         hookEventName,
					"post_execution_observed": true,
					"reason":                  hookApprovalRecord.Reason,
					"execution_error":         secrets.RedactText(req.HookError),
					"hook_interrupted":        req.HookInterrupted,
				}, req, server.repoRoot, includeHookAuditPreviews)
				if err := server.logEvent("approval.granted", req.SessionID, approvalGrantedAuditData); err != nil {
					_ = server.restoreClaudeHookApprovalState(req.SessionID, previousApprovalRecords)
					http.Error(w, "audit unavailable: required append failed before hook decision", http.StatusInternalServerError)
					return
				}
			}
		case claudeCodeHookEventPermissionRequest:
			hookApprovalRecord, claudeHookSessionRecord, hookApprovalFound, claudeHookSessionErr = server.findClaudeHookApprovalByRequest(req)
			if claudeHookSessionErr == nil {
				if hookApprovalFound {
					hookApprovalRequestID = hookApprovalRecord.ApprovalRequestID
					hookApprovalState = hookApprovalRecord.State
					hookApprovalSurface = hookApprovalRecord.ApprovalSurface
					reason = "permission request matched pending Loopgate-tracked Claude approval"
				} else {
					reason = "permission request recorded with no matching Loopgate-tracked Claude approval"
				}
			}
		default:
			claudeHookSessionRecord, claudeHookSessionErr = server.ensureClaudeHookSessionBinding(req.SessionID, hookEventName, req.HookReason)
		}
		if claudeHookSessionErr != nil {
			http.Error(w, "claude hook session state unavailable: "+claudeHookSessionErr.Error(), http.StatusInternalServerError)
			return
		}
		if hookEventName == claudeCodeHookEventPostToolUse || hookEventName == claudeCodeHookEventPostToolUseFailure {
			if hookApprovalFound {
				hookHandlingMode = claudeCodeHookHandlingModeStateTransition
				hookApprovalRequestID = hookApprovalRecord.ApprovalRequestID
				hookApprovalState = hookApprovalRecord.State
				reason = "local Claude hook approval state updated from tool execution"
			} else {
				reason = "tool completion recorded with no pending local hook approval"
			}
		}
		if hookSurfaceClass == claudeCodeHookSurfaceSecondaryGovernance && hookEventName != claudeCodeHookEventPostToolUse && hookEventName != claudeCodeHookEventPostToolUseFailure && hookEventName != claudeCodeHookEventPermissionRequest {
			decision = "block"
			reason = "hook event is governance-relevant but not implemented in Loopgate yet"
			denialCode = DenialCodeHookEventUnimplemented
		} else if hookSurfaceClass == claudeCodeHookSurfaceUnknown {
			decision = "block"
			reason = "hook event is not recognized by Loopgate — denied by default"
			denialCode = DenialCodeHookUnknownEvent
		} else if hookEventName == claudeCodeHookEventSessionStart {
			reason = "session start recorded for local lifecycle audit"
		} else if hookEventName == claudeCodeHookEventUserPromptSubmit {
			reason = "user prompt recorded without automatic memory injection"
		} else if hookEventName == claudeCodeHookEventSessionEnd {
			if sessionEndAbandonedApprovals > 0 {
				hookHandlingMode = claudeCodeHookHandlingModeStateTransition
				reason = fmt.Sprintf("session end recorded and abandoned %d pending local hook approvals", sessionEndAbandonedApprovals)
			} else {
				reason = "session end recorded for local lifecycle audit"
			}
		}
		hookAuditData := mergeHookAuditProjection(map[string]interface{}{
			"decision":                      decision,
			"hook_event_name":               hookEventName,
			"hook_surface_class":            hookSurfaceClass,
			"hook_handling_mode":            hookHandlingMode,
			"hook_reason":                   req.HookReason,
			"actor_ref":                     formatHookAuditActor(peer.UID, req.SessionID),
			"tool_use_id":                   req.ToolUseID,
			"claude_hook_session_id":        claudeHookSessionRecord.SessionID,
			"claude_hook_session_state":     claudeHookSessionRecord.State,
			"continuity_thread_id":          claudeHookSessionRecord.CurrentThreadID,
			"continuity_next_thread_id":     claudeHookSessionRecord.NextThreadID,
			"continuity_previous_thread_id": claudeHookSessionRecord.PreviousThreadID,
			"hook_approval_request_id":      hookApprovalRequestID,
			"hook_approval_state":           hookApprovalState,
			"hook_approval_surface":         hookApprovalSurface,
			"tool_name":                     req.ToolName,
			"prompt_bytes":                  len(req.Prompt),
			"reason":                        reason,
			"additional_context_bytes":      additionalContextBytes,
			"peer_uid":                      peer.UID,
			"peer_pid":                      peer.PID,
		}, req, server.repoRoot, includeHookAuditPreviews)
		if err := server.logEvent("hook.pre_validate", req.SessionID, hookAuditData); err != nil {
			http.Error(w, "audit unavailable: required append failed before hook decision", http.StatusInternalServerError)
			return
		}
		if decision == "allow" {
			server.writeJSON(w, http.StatusOK, HookPreValidateResponse{
				Decision:          "allow",
				AdditionalContext: additionalContext,
			})
			return
		}
		server.writeJSON(w, http.StatusOK, HookPreValidateResponse{
			Decision:   "block",
			Reason:     reason,
			DenialCode: denialCode,
		})
		return
	}

	toolDef, known := claudeCodeToolMap[req.ToolName]
	if !known {
		decision := "block"
		reason := "tool not in governance map — denied by default"
		if !policyRuntime.policy.ClaudeCodeDenyUnknownTools() {
			decision = "allow"
			reason = "tool not in governance map — allowed by explicit policy override"
		}
		hookAuditData := mergeHookAuditProjection(map[string]interface{}{
			"decision":           decision,
			"hook_event_name":    hookEventName,
			"hook_surface_class": hookSurfaceClass,
			"hook_handling_mode": hookHandlingMode,
			"hook_reason":        req.HookReason,
			"actor_ref":          formatHookAuditActor(peer.UID, req.SessionID),
			"tool_name":          req.ToolName,
			"prompt_bytes":       len(req.Prompt),
			"reason":             reason,
			"peer_uid":           peer.UID,
			"peer_pid":           peer.PID,
		}, req, server.repoRoot, includeHookAuditPreviews)
		if err := server.logEvent("hook.pre_validate", req.SessionID, hookAuditData); err != nil {
			http.Error(w, "audit unavailable: required append failed before hook decision", http.StatusInternalServerError)
			return
		}
		if decision == "allow" {
			server.writeJSON(w, http.StatusOK, HookPreValidateResponse{Decision: "allow"})
			return
		}
		server.writeJSON(w, http.StatusOK, HookPreValidateResponse{
			Decision:   "block",
			Reason:     reason,
			DenialCode: DenialCodeHookUnknownTool,
		})
		return
	}

	result := server.evaluateClaudeCodeHookPolicy(req, toolDef)

	decision := "block"
	denialCode := DenialCodePolicyDenied
	hookApprovalRequestID := ""
	hookApprovalState := ""
	hookApprovalSurface := ""
	claudeHookSessionID := ""
	claudeHookSessionState := ""
	switch result.Decision {
	case policypkg.Allow:
		decision = "allow"
	case policypkg.NeedsApproval:
		hookApprovalRecord, claudeHookSessionRecord, approvalCreated, previousApprovalRecords, approvalErr := server.createClaudeHookApprovalRequest(req, result.Reason)
		if approvalErr != nil {
			result = policypkg.CheckResult{
				Decision: policypkg.Deny,
				Reason:   "failed to create local Claude hook approval: " + approvalErr.Error(),
			}
			denialCode = DenialCodeApprovalCreationFailed
			break
		}
		if approvalCreated {
			approvalCreatedAuditData := mergeHookAuditProjection(map[string]interface{}{
				"approval_request_id":    hookApprovalRecord.ApprovalRequestID,
				"approval_surface":       hookApprovalRecord.ApprovalSurface,
				"approval_class":         "claude_builtin_inline",
				"approval_state":         hookApprovalRecord.State,
				"capability":             req.ToolName,
				"tool_name":              req.ToolName,
				"tool_use_id":            req.ToolUseID,
				"control_session_id":     req.SessionID,
				"client_session_label":   req.SessionID,
				"actor_ref":              formatHookAuditActor(peer.UID, req.SessionID),
				"claude_hook_session_id": claudeHookSessionRecord.SessionID,
				"reason":                 hookApprovalRecord.Reason,
			}, req, server.repoRoot, includeHookAuditPreviews)
			if err := server.logEvent("approval.created", req.SessionID, approvalCreatedAuditData); err != nil {
				_ = server.restoreClaudeHookApprovalState(req.SessionID, previousApprovalRecords)
				http.Error(w, "audit unavailable: required append failed before hook decision", http.StatusInternalServerError)
				return
			}
		}
		decision = "ask"
		hookApprovalRequestID = hookApprovalRecord.ApprovalRequestID
		hookApprovalState = hookApprovalRecord.State
		hookApprovalSurface = hookApprovalRecord.ApprovalSurface
		claudeHookSessionID = claudeHookSessionRecord.SessionID
		claudeHookSessionState = claudeHookSessionRecord.State
	default:
		decision = "block"
	}

	hookAuditData := mergeHookAuditProjection(map[string]interface{}{
		"decision":                  decision,
		"hook_event_name":           hookEventName,
		"hook_surface_class":        hookSurfaceClass,
		"hook_handling_mode":        hookHandlingMode,
		"hook_reason":               req.HookReason,
		"actor_ref":                 formatHookAuditActor(peer.UID, req.SessionID),
		"tool_name":                 req.ToolName,
		"tool_use_id":               req.ToolUseID,
		"prompt_bytes":              len(req.Prompt),
		"category":                  toolDef.category,
		"operation":                 toolDef.operation,
		"claude_hook_session_id":    claudeHookSessionID,
		"claude_hook_session_state": claudeHookSessionState,
		"hook_approval_request_id":  hookApprovalRequestID,
		"hook_approval_state":       hookApprovalState,
		"hook_approval_surface":     hookApprovalSurface,
		"reason":                    result.Reason,
		"additional_context_bytes":  0,
		"peer_uid":                  peer.UID,
		"peer_pid":                  peer.PID,
	}, req, server.repoRoot, includeHookAuditPreviews)
	if err := server.logEvent("hook.pre_validate", req.SessionID, hookAuditData); err != nil {
		http.Error(w, "audit unavailable: required append failed before hook decision", http.StatusInternalServerError)
		return
	}

	if decision == "allow" {
		server.writeJSON(w, http.StatusOK, HookPreValidateResponse{Decision: "allow"})
		return
	}
	if decision == "ask" {
		server.writeJSON(w, http.StatusOK, HookPreValidateResponse{
			Decision:          "ask",
			Reason:            result.Reason,
			ApprovalRequestID: hookApprovalRequestID,
		})
		return
	}

	server.writeJSON(w, http.StatusOK, HookPreValidateResponse{
		Decision:   "block",
		Reason:     result.Reason,
		DenialCode: denialCode,
	})
}
