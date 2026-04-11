package loopgate

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	modelpkg "morph/internal/model"
)

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
// havenChatRuntime.executeToolCallsConcurrent can stream individual tool_result events
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
// tool goroutines (see havenChatRuntime.executeToolCallsConcurrent) emit tool_result events.
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
