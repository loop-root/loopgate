package loopgateresult

// ToolCall is the display-layer summary of one model-requested capability call.
// It is not an authority object and exists only to summarize results coherently.
type ToolCall struct {
	ID   string
	Name string
	Args map[string]string
}

// ToolResult captures the rendered outcome of a governed capability call.
type ToolResult struct {
	CallID            string
	Capability        string
	Status            ToolStatus
	Output            string
	Reason            string
	DenialCode        string
	ApprovalRequestID string
}

// ToolStatus reports the outcome category for a governed capability call.
type ToolStatus string

const (
	StatusSuccess         ToolStatus = "success"
	StatusDenied          ToolStatus = "denied"
	StatusError           ToolStatus = "error"
	StatusPendingApproval ToolStatus = "pending_approval"
)
