package threadstore

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// Schema version for conversation events and thread index.
const (
	ConversationEventSchemaVersion = "haven.conversation.v1"
	ThreadIndexSchemaVersion       = "haven.thread_index.v1"
)

// ExecutionState tracks the current state of a thread's chat execution loop.
type ExecutionState string

const (
	ExecutionIdle               ExecutionState = "idle"
	ExecutionRunning            ExecutionState = "running"
	ExecutionWaitingForApproval ExecutionState = "waiting_for_approval"
	ExecutionCompleted          ExecutionState = "completed"
	ExecutionFailed             ExecutionState = "failed"
	ExecutionCancelled          ExecutionState = "cancelled"
)

// AcceptsNewMessage returns true if the thread is in a state that allows
// a new SendMessage call. Running and waiting_for_approval are the only
// states that block new messages.
func (s ExecutionState) AcceptsNewMessage() bool {
	switch s {
	case ExecutionIdle, ExecutionCompleted, ExecutionFailed, ExecutionCancelled:
		return true
	default:
		return false
	}
}

// ConversationEvent is a single event in a thread's JSONL file.
// User-visible events use the "user_message" and "assistant_message" types.
// Tool-loop internals use the "orchestration.*" prefix.
type ConversationEvent struct {
	SchemaVersion string                 `json:"v"`
	TS            string                 `json:"ts"`
	ThreadID      string                 `json:"thread_id"`
	Type          string                 `json:"type"`
	Data          map[string]interface{} `json:"data,omitempty"`
}

// Event types — user-visible transcript.
const (
	EventUserMessage      = "user_message"
	EventAssistantMessage = "assistant_message"
)

// Event types — orchestration internals (tool loop).
const (
	EventOrchModelResponse     = "orchestration.model_response"
	EventOrchToolStarted       = "orchestration.tool_started"
	EventOrchToolResult        = "orchestration.tool_result"
	EventOrchToolDenied        = "orchestration.tool_denied"
	EventOrchApprovalRequested = "orchestration.approval_requested"
	EventOrchApprovalResolved  = "orchestration.approval_resolved"
	EventOrchExecutionState    = "orchestration.execution_state"
)

// ThreadSummary is a single entry in the thread index.
type ThreadSummary struct {
	ThreadID    string `json:"thread_id"`
	Title       string `json:"title"`
	Folder      string `json:"folder,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	EventCount  int    `json:"event_count"`
}

// ThreadIndex is the top-level structure of thread_index.json.
// It is a rebuildable derived view — if missing or corrupt, it can
// be reconstructed from the thread JSONL files.
type ThreadIndex struct {
	SchemaVersion string          `json:"schema_version"`
	Threads       []ThreadSummary `json:"threads"`
}

// MakeThreadID generates a unique thread identifier following the project's
// identifier pattern: t-{date}-{time}-{8hexbytes}.
func MakeThreadID() string {
	randomBytes := make([]byte, 8)
	_, _ = rand.Read(randomBytes)
	suffix := hex.EncodeToString(randomBytes)
	return fmt.Sprintf("t-%s-%s", time.Now().UTC().Format("20060102-150405"), suffix)
}

// NowUTC returns the current time in RFC3339Nano format.
func NowUTC() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// IsUserVisible returns true if the event type is part of the user-visible
// transcript rather than internal orchestration.
func IsUserVisible(eventType string) bool {
	return eventType == EventUserMessage || eventType == EventAssistantMessage
}
