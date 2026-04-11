package loopgate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"morph/internal/config"
	modelpkg "morph/internal/model"
	modelruntime "morph/internal/modelruntime"
	"morph/internal/orchestrator"
	"morph/internal/secrets"
	"morph/internal/threadstore"
)

const (
	maxHavenChatBodyBytes  = 8 * 1024 * 1024 // 8 MB — allows image attachments (base64)
	maxHavenChatTurns      = 20
	maxHavenToolIterations = 12
	// When the model answers host-folder organize asks with prose only (no tools), re-prompt
	// it this many times before returning the last assistant text to the client.
	// Each nudge is an extra full model round-trip in the same HTTP request — high values
	// multiply latency (e.g. 4 here ⇒ up to 5 sequential Reply calls). Keep this small.
	maxHavenHostFolderProseOnlyNudges = 2
	// After host.organize.plan succeeds, the model often answers with prose claiming Loopgate
	// approval is pending — but approvals are only created when host.plan.apply runs. Nudge apply
	// this many times before giving up (each nudge is one extra model round-trip).
	maxHavenHostPlanApplyNudges   = 2
	openAICompatibleModelsTimeout = 4 * time.Second
	useCompactHavenNativeTools    = true
)

const havenToolLoopContinuationFact = "You are after tool results in the thread. Continue from the user's **latest** message; those outputs are authoritative. Do not re-run tools that already succeeded in this thread unless the user explicitly asks to redo or refresh. Do **not** call host.folder.*, host.organize.plan, or host.plan.apply unless (a) the user's latest message is clearly about listing/organizing a granted Mac folder, or (b) you are in the middle of that same workflow in **this** request (e.g. you just listed and must plan, or plan just returned plan_id and apply is next). If the user narrowed scope (e.g. only memory, or said organizing is not needed), answer with text or the tools that match—do not drag in host organize. Do not restart with a generic greeting or onboarding menu unless they changed topic."

// havenToolLoopSlimOrganizeFact is injected only while a host-folder organize workflow is active
// for this HTTP request (see runHavenChatToolLoop). It must not run on every follow-up iteration when
// the toolkit is merely available — that steered models to re-list/re-plan after unrelated tools (e.g. memory.remember).
//
// The full capability catalog is not re-sent after the first call to keep later iterations fast.
const havenToolLoopSlimOrganizeFact = "NEXT STEP (host folders): if the folder was just listed → call host.organize.plan NOW with a plan_json that includes EVERY non-folder entry from the listing — every single file must have a move or mkdir operation, do not leave any files unorganized in the root; if a plan_id was just returned → call host.plan.apply with that plan_id. Do not ask for confirmation — the Loopgate popup IS the approval step."

const havenToolFollowupUserNudge = "Continue from my prior request using the tool results in the thread above. Give the next concrete step or answer with no greeting, and do not ask me to restate the goal unless it was truly ambiguous. Do not start host-folder organize tools unless my latest message asked for that or you are mid list→plan→apply from this same turn."

// havenHostFolderActNowNudge is injected when the model hasn't listed the folder yet.
const havenHostFolderActNowNudge = `Do NOT describe what you will do — call invoke_capability RIGHT NOW. Use capability="host.folder.list" and arguments_json="{\"folder_name\":\"downloads\"}" (or the correct preset). arguments_json must be a JSON-encoded string. Emit a structured tool call only — no prose.`

// havenHostFolderPlanNowNudge is injected when the folder has already been listed
// (conversation has prior assistant analysis) but the model returned prose instead of calling host.organize.plan.
const havenHostFolderPlanNowNudge = `Folder contents are already in the thread above. Do NOT ask the user for confirmation — the Loopgate popup that appears after host.plan.apply IS the approval step. Call invoke_capability RIGHT NOW with capability="host.organize.plan". Your plan_json MUST include a move or mkdir operation for EVERY file in the listing — do not skip any files or leave them in the root. arguments_json must be a JSON-encoded string, e.g. "{\"folder_name\":\"downloads\",\"plan_json\":\"[{\\\"kind\\\":\\\"mkdir\\\",\\\"path\\\":\\\"Archives\\\"},{\\\"kind\\\":\\\"move\\\",\\\"from\\\":\\\"file.zip\\\",\\\"to\\\":\\\"Archives/file.zip\\\"}]\"}". Do not invent capabilities like host.folder.mkdir — all folder creation goes inside plan_json as {\"kind\":\"mkdir\",\"path\":\"Name\"}. Emit a tool call only.`

// havenHostPlanApplyActNowNudge is sent when host.organize.plan already returned a plan_id but the
// model answered with prose instead of calling host.plan.apply (the only step that enqueues Loopgate approval).
const havenHostPlanApplyActNowNudge = "host.organize.plan already returned a plan_id in the tool results above. Loopgate does not open an approval prompt until you call host.plan.apply with that plan_id. The operator already confirmed in Messenger — call invoke_capability for host.plan.apply now using the plan_id from the organize.plan result. Do not tell the user approval is waiting until host.plan.apply returns pending_approval."

const defaultHavenToolResultMaxRunes = 20000

// Optional UX hints for Haven clients (e.g. sidebar follow-ups). Not security-sensitive.
const (
	havenUXSignalHostOrganizeApprovalPending = "host_organize_approval_pending"
	havenUXSignalHostOrganizeApplied         = "host_organize_applied"
)

var havenToolResultMaxRunesByCapability = map[string]int{
	"fs_read":                 16000,
	"fs_list":                 12000,
	"operator_mount.fs_read":  16000,
	"operator_mount.fs_list":  12000,
	"operator_mount.fs_write": 16000,
	"operator_mount.fs_mkdir": 8000,
	"shell_exec":              12000,
	"haven.operator_context":  12000,
	"host.folder.list":        4000,
	"host.folder.read":        16000,
	"host.organize.plan":      20000,
	"host.plan.apply":         20000,
}

type havenCapabilityDescriptor struct {
	DisplayName string
	RuntimeHint string
}

var havenCapabilityCatalog = map[string]havenCapabilityDescriptor{
	"fs_list":                 {DisplayName: "Browse Files", RuntimeHint: "browse folders and see what is in your Haven sandbox workspace"},
	"fs_read":                 {DisplayName: "Read Documents", RuntimeHint: "read files inside your Haven sandbox workspace"},
	"fs_write":                {DisplayName: "Save Work", RuntimeHint: "create and update files in your Haven sandbox workspace"},
	"fs_mkdir":                {DisplayName: "Create Folders", RuntimeHint: "create new folders in your Haven sandbox workspace"},
	"operator_mount.fs_list":  {DisplayName: "Granted host project", RuntimeHint: "list files under operator-granted host directories (paths relative to each grant root)"},
	"operator_mount.fs_read":  {DisplayName: "Granted host project", RuntimeHint: "read files under operator-granted host directories"},
	"operator_mount.fs_write": {DisplayName: "Granted host project", RuntimeHint: "write files under operator-granted host directories (may require approval)"},
	"operator_mount.fs_mkdir": {DisplayName: "Granted host project", RuntimeHint: "create directories under operator-granted host paths (may require approval)"},
	"journal.list":            {DisplayName: "Journal", RuntimeHint: "review your private journal entries"},
	"journal.read":            {DisplayName: "Journal", RuntimeHint: "read a private journal entry"},
	"journal.write":           {DisplayName: "Journal", RuntimeHint: "write a private journal entry when the user asks for reflection or journaling"},
	"haven.operator_context":  {DisplayName: "Operator guide", RuntimeHint: "fetch authoritative Haven harness documentation for troubleshooting"},
	"notes.list":              {DisplayName: "Notes", RuntimeHint: "review your private working notes"},
	"notes.read":              {DisplayName: "Notes", RuntimeHint: "read a working note from your notebook"},
	"notes.write":             {DisplayName: "Notes", RuntimeHint: "save a working note for plans, scratch work, or research"},
	"memory.remember":         {DisplayName: "Remember Things", RuntimeHint: "propose short structured continuity (preferences, routines, profile, goals); Loopgate accepts or rejects; do not invent facts or store secrets"},
	"paint.list":              {DisplayName: "Paint", RuntimeHint: "review the paintings in your gallery"},
	"paint.save":              {DisplayName: "Paint", RuntimeHint: "create a painting from explicit strokes and save it to your gallery"},
	"note.create":             {DisplayName: "Sticky Notes", RuntimeHint: "leave a sticky note on the desktop for the user"},
	"desktop.organize":        {DisplayName: "Desktop Layout", RuntimeHint: "rearrange the desktop icons to tidy up Haven"},
	"todo.add":                {DisplayName: "Task Board", RuntimeHint: "add a task when the user wants a reminder or explicitly asks to track something across sessions"},
	"todo.complete":           {DisplayName: "Task Board", RuntimeHint: "mark a task as done when it no longer needs attention"},
	"todo.list":               {DisplayName: "Task Board", RuntimeHint: "review your open tasks and active goals"},
	"goal.set":                {DisplayName: "Goals", RuntimeHint: "set a named persistent goal for ongoing work or a multi-session objective the user wants to track"},
	"goal.close":              {DisplayName: "Goals", RuntimeHint: "close a goal when the objective has been achieved or the user no longer wants to track it"},
	"shell_exec":              {DisplayName: "Terminal Commands", RuntimeHint: "run terminal commands when a task genuinely requires the command line"},
	"host.folder.list":        {DisplayName: "Granted host folders", RuntimeHint: "list files in a user-granted folder on the real host filesystem"},
	"host.folder.read":        {DisplayName: "Granted host folders", RuntimeHint: "read a file under a granted host folder on disk"},
	"host.organize.plan":      {DisplayName: "Granted host folders", RuntimeHint: "draft a move or mkdir plan for a granted folder (no host writes until apply)"},
	"host.plan.apply":         {DisplayName: "Granted host folders", RuntimeHint: "execute an approved organization plan on the real host filesystem"},
	"invoke_capability":       {DisplayName: "Capability Dispatcher", RuntimeHint: "dispatch a single allowed Haven capability"},
}

type havenChatRequest struct {
	Message     string                `json:"message"`
	ThreadID    *string               `json:"thread_id,omitempty"`
	Attachments []havenChatAttachment `json:"attachments,omitempty"`
	// Greet instructs Loopgate to generate a session-opening greeting from Morph
	// rather than processing a user message. The Message field is ignored when
	// Greet is true. The greeting is generated from the operator's wake state,
	// current tasks/goals, and any time-sensitive items.
	Greet           bool     `json:"greet,omitempty"`
	ProjectPath     string   `json:"project_path,omitempty"`
	ProjectName     string   `json:"project_name,omitempty"`
	GitBranch       string   `json:"git_branch,omitempty"`
	AdditionalPaths []string `json:"additional_paths,omitempty"`
}

// havenChatAttachment carries a single file or image alongside a chat message.
// Data is standard base64-encoded file content (no data-URI prefix).
type havenChatAttachment struct {
	Name     string `json:"name"`
	MimeType string `json:"mime_type"`
	Data     string `json:"data"`
}

// HavenChatResponse is the JSON body for POST /v1/chat.
// The legacy /v1/haven/chat alias returns the same payload.
type HavenChatResponse struct {
	ThreadID           string   `json:"thread_id"`
	AssistantText      string   `json:"assistant_text"`
	Status             string   `json:"status,omitempty"`
	ApprovalID         string   `json:"approval_id,omitempty"`
	ApprovalCapability string   `json:"approval_capability,omitempty"`
	ProviderName       string   `json:"provider_name,omitempty"`
	ModelName          string   `json:"model_name,omitempty"`
	FinishReason       string   `json:"finish_reason,omitempty"`
	InputTokens        int      `json:"input_tokens,omitempty"`
	OutputTokens       int      `json:"output_tokens,omitempty"`
	TotalTokens        int      `json:"total_tokens,omitempty"`
	UXSignals          []string `json:"ux_signals,omitempty"`
}

// havenChatLoopOutcome is the result of the Haven HTTP chat tool loop.
type havenChatLoopOutcome struct {
	modelResponse      modelpkg.Response
	assistantText      string
	err                error
	approvalStatus     string
	approvalID         string
	approvalCapability string
	uxSignals          []string
}

// ---------------------------------------------------------------------------
// SSE streaming types — JSON keys must match ChatStreamEvent in the Swift client.
// ---------------------------------------------------------------------------

// havenSSEEvent is one event pushed over the SSE stream to Haven.
type havenSSEEvent struct {
	Type           string              `json:"type"`
	Content        string              `json:"content,omitempty"`
	ThreadID       string              `json:"thread_id,omitempty"`
	ToolCall       *havenSSEToolCall   `json:"tool_call,omitempty"`
	ToolResult     *havenSSEToolResult `json:"tool_result,omitempty"`
	ApprovalNeeded *havenSSEApproval   `json:"approval_needed,omitempty"`
	UXSignals      []string            `json:"ux_signals,omitempty"`
	Error          string              `json:"error,omitempty"`
	// Present only on turn_complete:
	FinishReason string `json:"finish_reason,omitempty"`
	InputTokens  int    `json:"input_tokens,omitempty"`
	OutputTokens int    `json:"output_tokens,omitempty"`
	TotalTokens  int    `json:"total_tokens,omitempty"`
	ProviderName string `json:"provider_name,omitempty"`
	ModelName    string `json:"model_name,omitempty"`
}

type havenSSEToolCall struct {
	CallID string `json:"call_id"`
	Name   string `json:"name"`
}

type havenSSEToolResult struct {
	CallID  string `json:"call_id"`
	Preview string `json:"preview,omitempty"`
	Status  string `json:"status"`
}

type havenSSEApproval struct {
	ApprovalID string `json:"approval_id"`
	Capability string `json:"capability"`
}

// havenSSEEmitter writes JSON-encoded SSE events ("data: {json}\n\n") to an
// http.ResponseWriter. Headers are committed on construction. All methods are
// safe to call on a nil receiver (no-op), so callers never need to nil-check.
//
// mu serialises concurrent emit() calls so that goroutines spawned by
// executeHavenToolCallsConcurrent can stream individual tool_result events
// as each tool finishes without corrupting the SSE frame boundaries.
// The single-goroutine serial path takes the lock on every call but never
// contends, so there is no measurable overhead in the common case.
type havenSSEEmitter struct {
	mu      sync.Mutex
	writer  http.ResponseWriter
	flusher http.Flusher
}

// newHavenSSEEmitter commits SSE headers (200 OK, text/event-stream) and
// returns an emitter. Returns nil if the writer does not implement http.Flusher,
// which should not happen with net/http's standard ResponseWriter.
func newHavenSSEEmitter(writer http.ResponseWriter) *havenSSEEmitter {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		return nil
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("X-Accel-Buffering", "no")
	writer.WriteHeader(http.StatusOK)
	flusher.Flush()
	return &havenSSEEmitter{writer: writer, flusher: flusher}
}

// emit marshals event to JSON and writes a "data: {json}\n\n" SSE line,
// flushing immediately. A nil receiver or a marshal failure is a silent no-op
// — the client sees a gap rather than a corrupt frame.
// emit is goroutine-safe: the mutex prevents frame interleaving when concurrent
// tool goroutines (see executeHavenToolCallsConcurrent) emit tool_result events.
func (e *havenSSEEmitter) emit(event havenSSEEvent) {
	if e == nil {
		return
	}
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	e.mu.Lock()
	fmt.Fprintf(e.writer, "data: %s\n\n", data)
	e.flusher.Flush()
	e.mu.Unlock()
}

// OllamaTagsResponse lists model ids from an OpenAI-compatible /models endpoint.
type OllamaTagsResponse struct {
	Models []OllamaModelTag `json:"models"`
}

// OllamaModelTag is a single model entry surfaced to Haven.
type OllamaModelTag struct {
	Name string `json:"name"`
	Size int64  `json:"size,omitempty"`
}

func (server *Server) handleHavenChat(writer http.ResponseWriter, request *http.Request) {
	// Declared before defer so the panic recovery can reach the emitter.
	// After SSE headers are committed (200 OK), http.Error writes raw bytes into
	// the already-open stream body — the client skips them as non-data lines and
	// the turn silently disappears. Emit proper SSE events instead.
	var emitter *havenSSEEmitter
	var diagTenantID, diagUserID, diagControlSessionID string
	defer func() {
		if r := recover(); r != nil {
			// Panic recovery guarantees the control plane fails safely. We emit an SSE error
			// so the user knows the turn failed, rather than leaving the stream indefinitely open.
			// Recovery MUST NOT swallow failures that must fail closed for security — by
			// recovering here, we abort the HTTP request entirely ensure no permissive state or
			// capability token is leaked via a half-completed execution.
			if server.diagnostic != nil && server.diagnostic.Server != nil {
				args := []any{
					"panic", fmt.Sprintf("%v", r),
					"control_session_id", diagControlSessionID,
				}
				args = append(args, diagnosticSlogTenantUser(diagTenantID, diagUserID)...)
				server.diagnostic.Server.Error("haven_chat_panic", args...)
			}
			if emitter != nil {
				emitter.emit(havenSSEEvent{Type: "error", Error: "internal error"})
				emitter.emit(havenSSEEvent{Type: "turn_complete"})
			} else {
				http.Error(writer, "internal error", http.StatusInternalServerError)
			}
		}
	}()

	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityModelReply) {
		return
	}
	diagControlSessionID = tokenClaims.ControlSessionID
	diagTenantID = tokenClaims.TenantID
	diagUserID = tokenClaims.UserID
	if !server.hasTrustedHavenSession(tokenClaims) {
		if server.diagnostic != nil && server.diagnostic.Server != nil {
			args := append([]any{"reason", "haven chat requires trusted Haven session"}, diagnosticSlogTenantUser(diagTenantID, diagUserID)...)
			server.diagnostic.Server.Warn("haven_chat_denied", args...)
		}
		_ = server.logEvent("haven.chat.denied", diagControlSessionID, map[string]interface{}{
			"denial_code": DenialCodeCapabilityTokenInvalid,
			"reason":      "haven chat requires trusted Haven session",
		})
		server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "haven chat requires trusted Haven session",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return
	}

	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxHavenChatBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		if server.diagnostic != nil && server.diagnostic.Server != nil {
			args := append([]any{"reason", denialResponse.DenialReason, "denial_code", denialResponse.DenialCode}, diagnosticSlogTenantUser(diagTenantID, diagUserID)...)
			server.diagnostic.Server.Warn("haven_chat_denied", args...)
		}
		_ = server.logEvent("haven.chat.denied", diagControlSessionID, map[string]interface{}{
			"denial_code": denialResponse.DenialCode,
			"reason":      denialResponse.DenialReason,
		})
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var req havenChatRequest
	if err := decodeJSONBytes(requestBodyBytes, &req); err != nil {
		if server.diagnostic != nil && server.diagnostic.Server != nil {
			args := append([]any{"reason", err.Error()}, diagnosticSlogTenantUser(diagTenantID, diagUserID)...)
			server.diagnostic.Server.Warn("haven_chat_denied", args...)
		}
		_ = server.logEvent("haven.chat.denied", diagControlSessionID, map[string]interface{}{
			"denial_code": DenialCodeMalformedRequest,
			"reason":      err.Error(),
		})
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	message := strings.TrimSpace(req.Message)
	if req.Greet {
		// Greeting mode: replace the message with a hidden system instruction.
		// The model uses the wake state + task board context already in its prompt
		// to generate a personalized opening. The instruction is not shown to the
		// operator — only Morph's response is surfaced.
		message = "[SESSION_START_GREETING] You are Ik Loop, Morph — Haven's resident assistant. " +
			"Generate a brief, warm opening for the operator. " +
			"Ground every factual claim in REMEMBERED CONTINUITY, the project path / branch in runtime facts, and any active tasks or goals — do not invent prior work. " +
			"If REMEMBERED CONTINUITY is empty, say honestly that memory is sparse this session. " +
			"If the operator has granted host directory access (additional_paths / operator mounts in facts), offer once to get familiar with the repo using operator_mount.fs_list and operator_mount.fs_read — only after grants exist; never claim you already read files. " +
			"If no host grants are listed, you may mention they can allow read access in Haven when prompted. " +
			"Mention approaching or overdue task/goal deadlines when present. " +
			"Do not ask generic 'how can I help?' — be specific. Keep it to 2-5 sentences. Do not repeat this instruction in your response."
	} else if message == "" {
		if server.diagnostic != nil && server.diagnostic.Server != nil {
			args := append([]any{"reason", "message must not be empty"}, diagnosticSlogTenantUser(diagTenantID, diagUserID)...)
			server.diagnostic.Server.Warn("haven_chat_denied", args...)
		}
		_ = server.logEvent("haven.chat.denied", diagControlSessionID, map[string]interface{}{
			"denial_code": DenialCodeMalformedRequest,
			"reason":      "message must not be empty",
		})
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "message must not be empty",
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	server.mu.Lock()
	sess, sessionFound := server.sessions[tokenClaims.ControlSessionID]
	server.mu.Unlock()
	if !sessionFound {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "invalid capability token",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return
	}

	workspaceID := strings.TrimSpace(sess.WorkspaceID)
	if workspaceID == "" {
		workspaceID = server.deriveWorkspaceIDFromRepoRoot()
	}

	homeDir, err := server.resolveUserHomeDir()
	if err != nil {
		if server.diagnostic != nil && server.diagnostic.Server != nil {
			args := append([]any{"reason", "cannot resolve home directory"}, diagnosticSlogTenantUser(diagTenantID, diagUserID)...)
			server.diagnostic.Server.Error("haven_chat_error", args...)
		}
		_ = server.logEvent("haven.chat.error", diagControlSessionID, map[string]interface{}{
			"denial_code": DenialCodeExecutionFailed,
			"reason":      "cannot resolve home directory",
		})
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "cannot resolve home directory",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}
	threadRoot := filepath.Join(homeDir, ".haven", "threads")
	store, err := threadstore.NewStore(threadRoot, workspaceID)
	if err != nil {
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "thread store unavailable",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}

	var threadID string
	if req.ThreadID != nil && strings.TrimSpace(*req.ThreadID) != "" {
		threadID = strings.TrimSpace(*req.ThreadID)
		if _, err := store.LoadThread(threadID); err != nil {
			if server.diagnostic != nil && server.diagnostic.Server != nil {
				args := append([]any{"reason", "unknown thread_id", "thread_id", threadID}, diagnosticSlogTenantUser(diagTenantID, diagUserID)...)
				server.diagnostic.Server.Warn("haven_chat_denied", args...)
			}
			server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
				Status:       ResponseStatusDenied,
				DenialReason: "unknown thread_id",
				DenialCode:   DenialCodeMalformedRequest,
			})
			return
		}
	} else {
		summary, err := store.NewThread()
		if err != nil {
			if server.diagnostic != nil && server.diagnostic.Server != nil {
				args := append([]any{"reason", "cannot create thread"}, diagnosticSlogTenantUser(diagTenantID, diagUserID)...)
				server.diagnostic.Server.Error("haven_chat_error", args...)
			}
			_ = server.logEvent("haven.chat.error", diagControlSessionID, map[string]interface{}{
				"denial_code": DenialCodeExecutionFailed,
				"reason":      "cannot create thread",
			})
			server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
				Status:       ResponseStatusError,
				DenialReason: "cannot create thread",
				DenialCode:   DenialCodeExecutionFailed,
			})
			return
		}
		threadID = summary.ThreadID
	}

	if err := store.AppendEvent(threadID, threadstore.ConversationEvent{
		Type: threadstore.EventUserMessage,
		Data: map[string]interface{}{"text": message},
	}); err != nil {
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "cannot persist user message",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}

	conversation := havenBuildConversationFromThread(store, threadID)
	windowed := havenWindowConversationForModel(conversation, maxHavenChatTurns)

	persona, err := config.LoadPersona(server.repoRoot)
	if err != nil {
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "persona unavailable",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}

	runtimeConfig, err := modelruntime.LoadConfig(server.repoRoot)
	if err != nil {
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: fmt.Sprintf("load model runtime config: %v", err),
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}

	modelClient, _, err := server.newModelClientFromConfig(runtimeConfig)
	if err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: fmt.Sprintf("initialize model runtime: %v", err),
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}

	wakeText, err := server.havenWakeStateSummaryText(tokenClaims.TenantID)
	if err != nil {
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "wake-state backend is unavailable",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}

	// Extend the timeout significantly for local models because CPU inference
	// or slow GPU offload can cause the first token to exceed default windows,
	// silently terminating the loop before the response generates.
	timeoutWindow := 60 * time.Second
	if runtimeConfig.ProviderName == "openai_compatible" || modelruntime.IsLoopbackModelBaseURL(runtimeConfig.BaseURL) {
		timeoutWindow = 5 * time.Minute
	}
	modelCtx, cancelModel := context.WithTimeout(request.Context(), timeoutWindow)
	defer cancelModel()

	allowedCapabilitySummaries := filterHavenCapabilitySummaries(server.capabilitySummaries(), tokenClaims.AllowedCapabilities)

	// shell_exec is off by default in Haven. It must be explicitly enabled via
	// Settings → Developer. This check is live — no Loopgate restart required.
	if shellDevEnabled, err := config.IsShellDevModeEnabled(server.repoRoot); err == nil && !shellDevEnabled {
		allowedCapabilitySummaries = havenFilterOutCapability(allowedCapabilitySummaries, "shell_exec")
	}

	availableToolDefs := buildHavenToolDefinitions(allowedCapabilitySummaries)
	nativeToolDefs := modelpkg.BuildNativeToolDefsForAllowedNamesWithOptions(server.registry, capabilityNamesFromSummaries(allowedCapabilitySummaries), modelpkg.NativeToolDefBuildOptions{
		HavenUserIntentGuards: true,
		CompactNativeTools:    useCompactHavenNativeTools,
	})
	if useCompactHavenNativeTools {
		availableToolDefs = buildCompactInvokeCapabilityToolDefinitions(capabilityNamesFromSummaries(allowedCapabilitySummaries))
	}
	runtimeFacts := server.buildHavenRuntimeFacts(allowedCapabilitySummaries, runtimeConfig.ProviderName, runtimeConfig.ModelName, req.ProjectPath, req.ProjectName, req.GitBranch, req.AdditionalPaths)
	allowedCapabilityNames := make(map[string]struct{}, len(allowedCapabilitySummaries))
	for _, summary := range allowedCapabilitySummaries {
		allowedCapabilityNames[summary.Name] = struct{}{}
	}
	hostFolderOrganizeToolkitAvailable := hasAllHavenCapabilities(allowedCapabilityNames,
		"host.folder.list", "host.folder.read", "host.organize.plan", "host.plan.apply")

	// Commit SSE headers before entering the loop. From this point forward the
	// response is streamed; error paths use SSE events rather than JSON bodies.
	emitter = newHavenSSEEmitter(writer)
	if emitter == nil {
		// net/http's ResponseWriter always implements Flusher — this path is a
		// safety guard only (e.g. if a test proxy strips it).
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "streaming not supported by this transport",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}
	if server.diagnostic != nil && server.diagnostic.Server != nil {
		args := []any{
			"thread_id", threadID,
			"control_session_id", tokenClaims.ControlSessionID,
		}
		args = append(args, diagnosticSlogTenantUser(tokenClaims.TenantID, tokenClaims.UserID)...)
		server.diagnostic.Server.Debug("haven_chat_sse_stream_start", args...)
	}

	var modelAttachments []modelpkg.Attachment
	for _, a := range req.Attachments {
		if strings.TrimSpace(a.Name) == "" || strings.TrimSpace(a.MimeType) == "" || strings.TrimSpace(a.Data) == "" {
			continue
		}
		modelAttachments = append(modelAttachments, modelpkg.Attachment{
			Name:     strings.TrimSpace(a.Name),
			MimeType: strings.ToLower(strings.TrimSpace(a.MimeType)),
			Data:     strings.TrimSpace(a.Data),
		})
	}

	havenChatWallStart := time.Now()
	loopOutcome := server.runHavenChatToolLoop(modelCtx, modelClient, store, threadID, tokenClaims, persona, wakeText, windowed, message, modelAttachments, availableToolDefs, nativeToolDefs, runtimeFacts, hostFolderOrganizeToolkitAvailable, emitter)
	havenChatWallMs := time.Since(havenChatWallStart).Milliseconds()

	if loopOutcome.err != nil {
		_ = server.logEvent("haven.chat", tokenClaims.ControlSessionID, map[string]interface{}{
			"thread_id":          threadID,
			"workspace_id":       workspaceID,
			"control_session_id": tokenClaims.ControlSessionID,
			"haven_chat_wall_ms": havenChatWallMs,
			"error":              loopOutcome.err.Error(),
		})
		fallbackText := havenChatFallbackText(loopOutcome.err)
		_ = store.AppendEvent(threadID, threadstore.ConversationEvent{
			Type: threadstore.EventAssistantMessage,
			Data: map[string]interface{}{"text": fallbackText},
		})
		// The loop did not emit a text_delta for error paths — emit the fallback
		// text now so the client always receives a visible message.
		emitter.emit(havenSSEEvent{Type: "text_delta", Content: fallbackText})
		emitter.emit(havenSSEEvent{Type: "turn_complete", ThreadID: threadID})
		return
	}

	if err := store.AppendEvent(threadID, threadstore.ConversationEvent{
		Type: threadstore.EventAssistantMessage,
		Data: map[string]interface{}{"text": loopOutcome.assistantText},
	}); err != nil {
		// Persistence failed after the loop already emitted its events. Emit an
		// error event so the client knows the turn did not complete cleanly.
		emitter.emit(havenSSEEvent{
			Type:     "error",
			Error:    "cannot persist assistant message",
			ThreadID: threadID,
		})
		return
	}

	logPayload := map[string]interface{}{
		"thread_id":          threadID,
		"workspace_id":       workspaceID,
		"provider":           loopOutcome.modelResponse.ProviderName,
		"model":              loopOutcome.modelResponse.ModelName,
		"finish_reason":      loopOutcome.modelResponse.FinishReason,
		"input_tokens":       loopOutcome.modelResponse.Usage.InputTokens,
		"output_tokens":      loopOutcome.modelResponse.Usage.OutputTokens,
		"control_session_id": tokenClaims.ControlSessionID,
		"haven_chat_wall_ms": havenChatWallMs,
	}
	if strings.TrimSpace(loopOutcome.approvalStatus) != "" {
		logPayload["chat_status"] = loopOutcome.approvalStatus
	}
	_ = server.logEvent("haven.chat", tokenClaims.ControlSessionID, logPayload)

	// turn_complete closes the turn. The loop already emitted all text_delta,
	// tool_start, tool_result, and approval_needed events; this is the final
	// bookkeeping event the client uses to capture threadID, uxSignals, and
	// token counts.
	emitter.emit(havenSSEEvent{
		Type:         "turn_complete",
		ThreadID:     threadID,
		UXSignals:    loopOutcome.uxSignals,
		FinishReason: loopOutcome.modelResponse.FinishReason,
		InputTokens:  loopOutcome.modelResponse.Usage.InputTokens,
		OutputTokens: loopOutcome.modelResponse.Usage.OutputTokens,
		TotalTokens:  loopOutcome.modelResponse.Usage.TotalTokens,
		ProviderName: loopOutcome.modelResponse.ProviderName,
		ModelName:    loopOutcome.modelResponse.ModelName,
	})
	if server.diagnostic != nil && server.diagnostic.Server != nil {
		args := []any{
			"thread_id", threadID,
			"control_session_id", tokenClaims.ControlSessionID,
			"haven_chat_wall_ms", havenChatWallMs,
		}
		args = append(args, diagnosticSlogTenantUser(tokenClaims.TenantID, tokenClaims.UserID)...)
		server.diagnostic.Server.Debug("haven_chat_sse_stream_done", args...)
	}
}

func (server *Server) handleOllamaTags(writer http.ResponseWriter, request *http.Request) {
	server.handleOpenAICompatibleModels(writer, request)
}

func (server *Server) handleOpenAICompatibleModels(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireTrustedHavenSession(writer, tokenClaims, "model listing requires trusted Haven session") {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityConnectionWrite) {
		return
	}

	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	baseURL := strings.TrimSpace(request.URL.Query().Get("base_url"))
	if baseURL == "" {
		baseURL = "http://127.0.0.1:11434/v1"
	}

	models, err := server.fetchHavenOpenAICompatibleModels(request.Context(), baseURL)
	if err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: secrets.RedactText(err.Error()),
			DenialCode:   DenialCodeExecutionFailed,
			Redacted:     true,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, OllamaTagsResponse{Models: models})
}

func havenOllamaAPIRootFromModelBase(baseURL string) string {
	trimmedBaseURL := strings.TrimSpace(baseURL)
	if trimmedBaseURL == "" {
		return "http://127.0.0.1:11434/v1"
	}
	return trimmedBaseURL
}

func (server *Server) fetchHavenOpenAICompatibleModels(ctx context.Context, baseURL string) ([]OllamaModelTag, error) {
	trimmedBaseURL := strings.TrimSpace(baseURL)
	if trimmedBaseURL == "" {
		trimmedBaseURL = "http://127.0.0.1:11434/v1"
	}
	if modelruntime.IsLoopbackModelBaseURL(trimmedBaseURL) {
		return fetchOpenAICompatibleModelTags(ctx, trimmedBaseURL, "")
	}

	runtimeConfig, err := modelruntime.LoadPersistedConfig(modelruntime.ConfigPath(server.repoRoot))
	if err != nil {
		return nil, fmt.Errorf("load model config: %w", err)
	}
	if strings.TrimSpace(runtimeConfig.ProviderName) != "openai_compatible" {
		return nil, fmt.Errorf("remote model listing requires an openai-compatible provider in current model settings")
	}
	if !strings.EqualFold(strings.TrimSpace(runtimeConfig.BaseURL), trimmedBaseURL) {
		return nil, fmt.Errorf("base_url must match the saved openai-compatible endpoint")
	}
	if strings.TrimSpace(runtimeConfig.ModelConnectionID) == "" {
		return nil, fmt.Errorf("save the openai-compatible connection first so Loopgate can use its stored credential")
	}

	modelConnectionRecord, err := server.resolveModelConnection(runtimeConfig.ModelConnectionID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(modelConnectionRecord.ProviderName) != "openai_compatible" {
		return nil, fmt.Errorf("saved model connection provider %q does not support openai-compatible model listing", modelConnectionRecord.ProviderName)
	}
	if !strings.EqualFold(strings.TrimSpace(modelConnectionRecord.BaseURL), trimmedBaseURL) {
		return nil, fmt.Errorf("saved model connection endpoint does not match base_url")
	}

	secretStore, err := server.secretStoreForRef(modelConnectionRecord.Credential)
	if err != nil {
		return nil, err
	}
	apiKeyBytes, _, err := secretStore.Get(ctx, modelConnectionRecord.Credential)
	if err != nil {
		return nil, fmt.Errorf("resolve model api key: %w", err)
	}
	return fetchOpenAICompatibleModelTags(ctx, trimmedBaseURL, strings.TrimSpace(string(apiKeyBytes)))
}

// fetchOllamaModelTags preserves the older loopback-only helper shape for existing Haven settings code.
func fetchOllamaModelTags(ctx context.Context, baseURL string) []OllamaModelTag {
	models, err := fetchOpenAICompatibleModelTags(ctx, baseURL, "")
	if err != nil {
		return nil
	}
	return models
}

// fetchOpenAICompatibleModelTags returns model ids from an OpenAI-compatible /models endpoint.
func fetchOpenAICompatibleModelTags(ctx context.Context, baseURL string, bearerToken string) ([]OllamaModelTag, error) {
	trimmedBaseURL := strings.TrimSpace(baseURL)
	if trimmedBaseURL == "" {
		trimmedBaseURL = "http://127.0.0.1:11434/v1"
	}

	modelsURL, err := url.Parse(trimmedBaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse model base url: %w", err)
	}
	if modelsURL.Scheme != "http" && modelsURL.Scheme != "https" {
		return nil, fmt.Errorf("model base url must use http or https")
	}
	modelsURL.Path = strings.TrimSuffix(modelsURL.Path, "/") + "/models"

	ctx, cancel := context.WithTimeout(ctx, openAICompatibleModelsTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build models request: %w", err)
	}
	if strings.TrimSpace(bearerToken) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearerToken))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		responseBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return nil, fmt.Errorf("models endpoint returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBytes)))
	}

	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode model list: %w", err)
	}

	models := make([]OllamaModelTag, 0, len(parsed.Data))
	for _, modelRecord := range parsed.Data {
		trimmedModelID := strings.TrimSpace(modelRecord.ID)
		if trimmedModelID == "" {
			continue
		}
		models = append(models, OllamaModelTag{Name: trimmedModelID})
	}
	return models, nil
}

func (server *Server) deriveWorkspaceIDFromRepoRoot() string {
	abs, err := filepath.Abs(server.repoRoot)
	if err != nil {
		abs = server.repoRoot
	}
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])
}

func (server *Server) havenWakeStateSummaryText(tenantID string) (string, error) {
	wake, err := server.loadMemoryWakeState(tenantID)
	if err != nil {
		return "", err
	}
	if len(wake.RecentFacts) == 0 && len(wake.ActiveGoals) == 0 && len(wake.UnresolvedItems) == 0 {
		return "", nil
	}

	var sb strings.Builder

	// Remembered facts (operator preferences, profile, routines).
	var factParts []string
	for _, f := range wake.RecentFacts {
		if len(factParts) >= 12 {
			break
		}
		factParts = append(factParts, fmt.Sprintf("  %s: %v", f.Name, f.Value))
	}
	if len(factParts) > 0 {
		sb.WriteString("REMEMBERED CONTINUITY (authoritative):\n")
		sb.WriteString(strings.Join(factParts, "\n"))
	} else {
		sb.WriteString("Wake-state is loaded with continuity metadata.")
	}

	// Active goals — persist across sessions.
	if len(wake.ActiveGoals) > 0 {
		sb.WriteString("\n\nACTIVE GOALS:\n")
		for _, g := range wake.ActiveGoals {
			sb.WriteString(fmt.Sprintf("  - %s\n", g))
		}
	}

	// Open tasks — the live task board the operator can track.
	if len(wake.UnresolvedItems) > 0 {
		sb.WriteString("\n\nOPEN TASKS (task board — use todo.list to get IDs, todo.complete to close):\n")
		for _, item := range wake.UnresolvedItems {
			line := fmt.Sprintf("  [%s] %s", item.ID, item.Text)
			if item.ScheduledForUTC != "" {
				line += fmt.Sprintf(" (due: %s)", item.ScheduledForUTC)
			}
			if item.NextStep != "" {
				line += fmt.Sprintf(" — next: %s", item.NextStep)
			}
			sb.WriteString(line + "\n")
		}
	}

	return sb.String(), nil
}

// runHavenChatToolLoop is Loopgate's supervised agent runtime for Haven turns.
//
// Runtime model (Loopgate terms):
//   - Morph (the resident assistant) operates within a bounded iteration budget
//     (maxHavenToolIterations). Loopgate owns the continuation decision; the model
//     only decides whether to call capabilities or return text within each iteration.
//   - Capability dispatch is policy-gated at every iteration via executeCapabilityRequest.
//     The model cannot bypass Loopgate authority by embedding capability names in text.
//   - Read-only capabilities may execute concurrently within a single iteration batch
//     (see executeHavenToolCallsConcurrent). Write and execute capabilities are always
//     dispatched serially to preserve observable ordering of side effects.
//   - All side-effect capabilities that require approval are held at the approval
//     boundary; the loop does not continue past a pending_approval result.
//
// Checkpoint gap (known limitation):
//
//	The conversation accumulates in memory across iterations. If the HTTP handler is
//	cancelled mid-loop (ctx cancellation, client disconnect, server restart) all
//	in-flight tool results are lost. A durable checkpoint would write each completed
//	tool-result turn to the threadstore before advancing to the next model call.
//	This is not yet implemented; recovery requires re-sending the user message.
//
// TODO(checkpoint): write completed tool-result turns to store after each iteration
// so that a cancelled or crashed request can be resumed without data loss.
func (server *Server) runHavenChatToolLoop(
	ctx context.Context,
	modelClient *modelpkg.Client,
	store *threadstore.Store,
	threadID string,
	tokenClaims capabilityToken,
	persona config.Persona,
	wakeState string,
	conversation []modelpkg.ConversationTurn,
	initialUserMessage string,
	initialAttachments []modelpkg.Attachment,
	availableTools []modelpkg.ToolDefinition,
	nativeToolDefs []modelpkg.NativeToolDef,
	baseRuntimeFacts []string,
	hostFolderOrganizeToolkitAvailable bool,
	emitter *havenSSEEmitter,
) havenChatLoopOutcome {
	userMessage := initialUserMessage
	var lastModelResponse modelpkg.Response
	var uxSignals []string
	proseOnlyHostFolderNudges := 0
	hostPlanApplyNudgeCount := 0
	sawHostFolderToolRound := false
	awaitingHostPlanApply := false

	for iteration := 0; iteration < maxHavenToolIterations; iteration++ {
		if ctx.Err() != nil {
			return havenChatLoopOutcome{err: ctx.Err()}
		}

		// On iteration 0 send the full capability catalog so the model knows what tools exist.
		// On subsequent iterations send only the lightweight set — the model already has tool
		// results in context and re-sending 2 000+ tokens of descriptions bloats input tokens
		// and adds measurable latency on every follow-up model call.
		var turnRuntimeFacts []string
		if iteration == 0 {
			turnRuntimeFacts = append([]string(nil), baseRuntimeFacts...)
		} else {
			turnRuntimeFacts = []string{
				havenToolLoopContinuationFact,
				modelpkg.HavenConstrainedNativeToolsRuntimeFact,
			}
			// Slim organize nudge is aggressive; inject it only while this request is already
			// executing host-folder tools or waiting on plan→apply — not whenever the toolkit exists.
			if hostFolderOrganizeToolkitAvailable && (sawHostFolderToolRound || awaitingHostPlanApply) {
				turnRuntimeFacts = append(turnRuntimeFacts, havenToolLoopSlimOrganizeFact)
			}
		}

		modelUserMessage := userMessage
		if iteration > 0 && strings.TrimSpace(modelUserMessage) == "" {
			modelUserMessage = havenToolFollowupUserNudge
		}

		windowedConversation := havenWindowConversationForModel(conversation, maxHavenChatTurns)
		// Attachments only apply to the first iteration — subsequent tool-loop iterations
		// are model↔tool exchanges that do not include the original user payload.
		var turnAttachments []modelpkg.Attachment
		if iteration == 0 {
			turnAttachments = initialAttachments
		}
		modelResponse, modelErr := modelClient.Reply(ctx, modelpkg.Request{
			Persona:        persona,
			Policy:         server.policy,
			SessionID:      tokenClaims.ControlSessionID,
			WakeState:      wakeState,
			Conversation:   windowedConversation,
			UserMessage:    modelUserMessage,
			Attachments:    turnAttachments,
			AvailableTools: availableTools,
			NativeToolDefs: nativeToolDefs,
			RuntimeFacts:   turnRuntimeFacts,
		})
		if modelErr != nil {
			return havenChatLoopOutcome{err: modelErr}
		}
		lastModelResponse = modelResponse
		replyText := strings.TrimSpace(modelResponse.AssistantText)
		if havenIsNonUserFacingAssistantPlaceholder(replyText) {
			replyText = ""
		}

		// If the model hit its output token limit the response is truncated — any
		// tool call JSON or plan_json will be malformed. Retrying with the same
		// (now longer) context only makes things worse and burns 40–50 s per
		// iteration. Return whatever text we have and let the user know.
		if strings.EqualFold(strings.TrimSpace(modelResponse.FinishReason), "max_tokens") ||
			strings.EqualFold(strings.TrimSpace(modelResponse.FinishReason), "length") {
			if replyText == "" {
				replyText = "My response was cut off — the output limit was reached. Try a shorter or more specific request."
			}
			emitter.emit(havenSSEEvent{Type: "text_delta", Content: replyText})
			return havenChatLoopOutcome{modelResponse: lastModelResponse, assistantText: replyText, uxSignals: uxSignals}
		}

		if userMessage != "" {
			conversation = append(conversation, modelpkg.ConversationTurn{
				Role:      "user",
				Content:   userMessage,
				Timestamp: threadstore.NowUTC(),
			})
		}

		useStructuredPath := len(modelResponse.ToolUseBlocks) > 0 && server.registry != nil
		var parsedCalls []orchestrator.ToolCall
		var validationErrors []orchestrator.ToolCallValidationError
		if useStructuredPath {
			parsedCalls, validationErrors = orchestrator.ExtractStructuredCalls(modelResponse.ToolUseBlocks, server.registry)
		} else {
			parser := orchestrator.NewParser()
			parser.Registry = server.registry
			parsedOutput := parser.Parse(replyText)
			parsedCalls = parsedOutput.Calls
		}

		if len(parsedCalls) == 0 && len(validationErrors) == 0 {
			if replyText == "" {
				replyText = "I didn't get a clear reply from the model. Try again in a moment."
			}
			// host.organize.plan does not enqueue Loopgate approval; host.plan.apply does. After any
			// host.* tool, sawHostFolderToolRound blocks the generic prose nudge — so without this,
			// the model can stop after organize.plan with "waiting for Loopgate" and Haven never
			// receives approval_required.
			if awaitingHostPlanApply &&
				hostFolderOrganizeToolkitAvailable &&
				hostPlanApplyNudgeCount < maxHavenHostPlanApplyNudges &&
				iteration+1 < maxHavenToolIterations {
				hostPlanApplyNudgeCount++
				conversation = append(conversation, modelpkg.ConversationTurn{
					Role:      "assistant",
					Content:   replyText,
					Timestamp: threadstore.NowUTC(),
				})
				userMessage = havenHostPlanApplyActNowNudge
				continue
			}
			if hostFolderOrganizeToolkitAvailable &&
				!sawHostFolderToolRound &&
				havenHostFolderProseNudgeApplies(initialUserMessage, conversation) &&
				proseOnlyHostFolderNudges < maxHavenHostFolderProseOnlyNudges &&
				iteration+1 < maxHavenToolIterations {
				proseOnlyHostFolderNudges++
				conversation = append(conversation, modelpkg.ConversationTurn{
					Role:      "assistant",
					Content:   replyText,
					Timestamp: threadstore.NowUTC(),
				})
				// If the thread already has a prior assistant turn, the folder has
				// been listed — push toward organize.plan. Otherwise push toward list.
				if havenThreadHasPriorAssistantWork(conversation) {
					userMessage = havenHostFolderPlanNowNudge
				} else {
					userMessage = havenHostFolderActNowNudge
				}
				continue
			}
			// Final return from this iteration — emit text now that we know no tool
			// calls follow (nudge continuations above would have called `continue`).
			emitter.emit(havenSSEEvent{Type: "text_delta", Content: replyText})
			return havenChatLoopOutcome{modelResponse: lastModelResponse, assistantText: replyText, uxSignals: uxSignals}
		}

		assistantTurn := modelpkg.ConversationTurn{
			Role:      "assistant",
			Content:   replyText,
			Timestamp: threadstore.NowUTC(),
		}
		if useStructuredPath {
			assistantTurn.ToolCalls = modelResponse.ToolUseBlocks
		}
		conversation = append(conversation, assistantTurn)

		if len(parsedCalls) == 0 && len(validationErrors) > 0 {
			conversation = append(conversation, havenStructuredValidationErrorTurn(validationErrors))
			userMessage = ""
			continue
		}

		// Emit tool_start for each valid call before execution so the client can
		// show progress immediately. Validation-error calls are not included here
		// because they were never dispatched — their tool_result would be orphaned.
		for _, call := range parsedCalls {
			emitter.emit(havenSSEEvent{Type: "tool_start", ToolCall: &havenSSEToolCall{
				CallID: call.ID,
				Name:   call.Name,
			}})
		}

		// executeHavenToolCallsConcurrent fans out read-only tools in parallel
		// and runs write/unknown tools serially. It also emits tool_result SSE
		// events inline as each tool finishes, so the operator sees live progress
		// rather than a single batch after all tools complete.
		toolResults := server.executeHavenToolCallsConcurrent(ctx, tokenClaims, parsedCalls, emitter)

		for _, validationError := range validationErrors {
			toolResults = append(toolResults, orchestrator.ToolResult{
				CallID:     validationError.BlockID,
				Capability: validationError.BlockName,
				Status:     orchestrator.StatusError,
				Output:     "Tool call rejected: " + validationError.Error() + ". Check the tool name and required arguments, then try again.",
			})
		}
		for _, parsedCall := range parsedCalls {
			if strings.HasPrefix(strings.TrimSpace(parsedCall.Name), "host.") {
				sawHostFolderToolRound = true
				break
			}
		}

		for _, tr := range toolResults {
			if tr.Status == orchestrator.StatusSuccess && tr.Capability == "host.plan.apply" {
				havenAccumulateUXSignal(&uxSignals, havenUXSignalHostOrganizeApplied)
			}
		}

		for _, tr := range toolResults {
			capName := strings.TrimSpace(tr.Capability)
			if capName == "host.organize.plan" && tr.Status == orchestrator.StatusSuccess {
				awaitingHostPlanApply = true
			}
			if capName == "host.plan.apply" {
				awaitingHostPlanApply = false
			}
		}

		// Auto-apply: when host.organize.plan just returned a plan_id, fire host.plan.apply
		// immediately without a model round-trip. This saves one full sequential model call
		// (~2–5 s) every time the organize flow completes successfully.
		// The pending-approval check below handles the resulting approval_required response
		// exactly as it would if the model had called host.plan.apply itself.
		if hostFolderOrganizeToolkitAvailable && awaitingHostPlanApply && ctx.Err() == nil {
			if planID := havenExtractOrganizePlanIDFromResults(toolResults); planID != "" {
				awaitingHostPlanApply = false
				autoCallID := "loopgate-auto-apply-" + planID
				emitter.emit(havenSSEEvent{Type: "tool_start", ToolCall: &havenSSEToolCall{
					CallID: autoCallID,
					Name:   "host.plan.apply",
				}})
				autoResults := server.executeHavenToolCalls(ctx, tokenClaims, []orchestrator.ToolCall{{
					ID:   autoCallID,
					Name: "host.plan.apply",
					Args: map[string]string{"plan_id": planID},
				}})
				for _, tr := range autoResults {
					if tr.Status == orchestrator.StatusSuccess {
						havenAccumulateUXSignal(&uxSignals, havenUXSignalHostOrganizeApplied)
					}
					emitter.emit(havenSSEEvent{Type: "tool_result", ToolResult: &havenSSEToolResult{
						CallID:  tr.CallID,
						Preview: havenSSEPreviewForToolResult(tr),
						Status:  string(tr.Status),
					}})
				}
				toolResults = append(toolResults, autoResults...)
			}
		}

		if pending := firstHavenPendingApprovalToolResult(toolResults); pending != nil {
			if havenCapabilityNeedsHostOrganizeApprovalUX(pending.Capability) {
				havenAccumulateUXSignal(&uxSignals, havenUXSignalHostOrganizeApprovalPending)
			}
			assistantText := havenAssistantTextWaitingForLoopgate(replyText)
			// Emit the approval preamble as text_delta. If the model produced prose
			// this iteration (replyText non-empty) it was NOT emitted earlier — tool
			// iterations hold text until the outcome is known. Emit the full combined
			// assistantText here so the client never receives a partial message.
			emitter.emit(havenSSEEvent{Type: "text_delta", Content: assistantText})
			emitter.emit(havenSSEEvent{Type: "approval_needed", ApprovalNeeded: &havenSSEApproval{
				ApprovalID: pending.ApprovalRequestID,
				Capability: pending.Capability,
			}})
			return havenChatLoopOutcome{
				modelResponse:      lastModelResponse,
				assistantText:      assistantText,
				approvalStatus:     "approval_required",
				approvalID:         pending.ApprovalRequestID,
				approvalCapability: pending.Capability,
				uxSignals:          uxSignals,
			}
		}

		if useStructuredPath {
			conversation = append(conversation, havenStructuredToolResultTurn(toolResults))
		} else {
			eligibleResults := havenPromptEligibleToolResults(toolResults)
			if len(eligibleResults) > 0 {
				conversation = append(conversation, modelpkg.ConversationTurn{
					Role:      "tool",
					Content:   orchestrator.FormatResults(eligibleResults),
					Timestamp: threadstore.NowUTC(),
				})
			}
		}

		userMessage = ""
	}

	timeoutText := "That took longer than expected and I had to stop mid-way. Try a smaller folder or ask again."
	emitter.emit(havenSSEEvent{Type: "text_delta", Content: timeoutText})
	return havenChatLoopOutcome{
		modelResponse: lastModelResponse,
		assistantText: timeoutText,
		uxSignals:     uxSignals,
	}
}

func (server *Server) executeHavenToolCalls(ctx context.Context, tokenClaims capabilityToken, parsedCalls []orchestrator.ToolCall) []orchestrator.ToolResult {
	toolResults := make([]orchestrator.ToolResult, 0, len(parsedCalls))
	for _, parsedCall := range parsedCalls {
		capabilityResponse := server.executeCapabilityRequest(ctx, tokenClaims, CapabilityRequest{
			RequestID:  parsedCall.ID,
			Actor:      tokenClaims.ActorLabel,
			SessionID:  tokenClaims.ControlSessionID,
			Capability: parsedCall.Name,
			Arguments:  parsedCall.Args,
		}, true)
		toolResult, toolResultErr := havenToolResultFromCapabilityResponse(parsedCall.ID, parsedCall.Name, capabilityResponse)
		if toolResultErr != nil {
			toolResult.Status = orchestrator.StatusError
			toolResult.Reason = "invalid Loopgate tool result"
		}
		toolResult.Capability = parsedCall.Name
		toolResults = append(toolResults, toolResult)
	}
	return toolResults
}

// executeHavenToolCallsConcurrent runs read-only tool calls in parallel and
// write/execute tool calls serially, preserving the original input order in
// the returned results slice.
//
// Why two phases:
//   - Read-only tools (OpRead) have no observable ordering constraints between
//     themselves; fanning them out reduces latency proportional to the count.
//   - Write and execute tools have side effects that may be visible to later
//     calls (e.g. fs_write followed by fs_read must see the new content).
//     They must remain serial.
//   - Unknown capabilities (not in registry) default to serial — fail-closed.
//
// The emitter is goroutine-safe (see havenSSEEmitter.mu) so each read goroutine
// can stream its own tool_result event as it finishes, giving the operator
// live feedback rather than a single batch after all reads complete.
//
// Concurrency invariants (AGENTS.md §7):
//   - All goroutines are scoped to this function; wg.Wait() ensures they all
//     complete before the function returns. No goroutine outlives the HTTP handler.
//   - Result slots are pre-allocated and each goroutine writes to its own index
//     (i), so there is no data race on the toolResults slice itself.
//   - Approval detection (checking results for StatusPendingApproval) happens
//     after wg.Wait(), so the caller always sees the complete, stable result set.
func (server *Server) executeHavenToolCallsConcurrent(ctx context.Context, tokenClaims capabilityToken, parsedCalls []orchestrator.ToolCall, emitter *havenSSEEmitter) []orchestrator.ToolResult {
	toolResults := make([]orchestrator.ToolResult, len(parsedCalls))

	// Partition into read-only (parallel) and serial (write/execute/unknown) groups.
	// We keep the original index so results can be written to the correct slot.
	type indexedCall struct {
		idx  int
		call orchestrator.ToolCall
	}
	var readGroup []indexedCall
	var serialGroup []indexedCall
	for i, call := range parsedCalls {
		cls := classifyCapability(server.registry, call.Name)
		if cls.readOnly {
			readGroup = append(readGroup, indexedCall{i, call})
		} else {
			serialGroup = append(serialGroup, indexedCall{i, call})
		}
	}

	// executeSingle runs one capability request and stores the result at toolResults[idx].
	// It also emits a tool_result SSE event immediately on completion so the operator
	// sees progress rather than a silent pause while tools run.
	executeSingle := func(idx int, call orchestrator.ToolCall) {
		capabilityResponse := server.executeCapabilityRequest(ctx, tokenClaims, CapabilityRequest{
			RequestID:  call.ID,
			Actor:      tokenClaims.ActorLabel,
			SessionID:  tokenClaims.ControlSessionID,
			Capability: call.Name,
			Arguments:  call.Args,
		}, true)
		result, err := havenToolResultFromCapabilityResponse(call.ID, call.Name, capabilityResponse)
		if err != nil {
			result.Status = orchestrator.StatusError
			result.Reason = "invalid Loopgate tool result"
		}
		result.Capability = call.Name
		toolResults[idx] = result
		// Emit immediately — emitter.emit is goroutine-safe.
		emitter.emit(havenSSEEvent{Type: "tool_result", ToolResult: &havenSSEToolResult{
			CallID:  result.CallID,
			Preview: havenSSEPreviewForToolResult(result),
			Status:  string(result.Status),
		}})
	}

	// Phase 1: Fan out all read-only calls concurrently.
	// Each goroutine writes to its own pre-allocated index — no mutex needed on toolResults.
	var wg sync.WaitGroup
	for _, ic := range readGroup {
		wg.Add(1)
		go func(idx int, call orchestrator.ToolCall) {
			defer wg.Done()
			executeSingle(idx, call)
		}(ic.idx, ic.call)
	}
	wg.Wait()

	// Phase 2: Execute write/unknown calls serially in original input order.
	// We must wait for all reads to complete first to preserve the invariant that
	// any write that could observe a prior read's side effects sees the final state.
	for _, ic := range serialGroup {
		executeSingle(ic.idx, ic.call)
	}

	return toolResults
}
