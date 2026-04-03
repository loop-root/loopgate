package orchestrator

// ToolCall represents a parsed tool invocation from model output.
type ToolCall struct {
	ID   string            `json:"id"`   // Unique ID for tracking
	Name string            `json:"name"` // e.g., "fs_read", "fs_write"
	Args map[string]string `json:"args"` // Tool-specific arguments
}

// ToolResult captures the outcome of a tool execution.
type ToolResult struct {
	CallID              string `json:"call_id"`
	Capability          string `json:"capability,omitempty"` // Validated Loopgate capability name (for provider replay)
	Status              Status `json:"status"`
	Output              string `json:"output"` // Tool output or error message
	Reason              string `json:"reason"` // For denials/errors: why
	DenialCode          string `json:"denial_code,omitempty"`
	ApprovalRequestID   string `json:"approval_request_id,omitempty"`
}

// Status represents the outcome of a tool call.
type Status string

const (
	StatusSuccess         Status = "success"
	StatusDenied          Status = "denied"
	StatusError           Status = "error"
	StatusPendingApproval Status = "pending_approval"
)

