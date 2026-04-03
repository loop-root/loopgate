package orchestrator

import (
	"fmt"
	"time"

	"morph/internal/audit"
	"morph/internal/ledger"
	"morph/internal/policy"
	"morph/internal/secrets"
)

// LedgerLogger logs orchestrator events to the append-only ledger.
// This satisfies the system contract requirement that all tool calls
// must be recorded in the ledger with inputs, policy decision, and outputs.
type LedgerLogger struct {
	LedgerPath   string
	SessionID    string
	ReportError  func(error)
	ledgerWriter audit.LedgerWriter
}

// NewLedgerLogger creates a logger that writes to the given ledger file.
func NewLedgerLogger(ledgerPath, sessionID string) *LedgerLogger {
	return &LedgerLogger{
		LedgerPath:   ledgerPath,
		SessionID:    sessionID,
		ledgerWriter: audit.NewLedgerWriter(ledger.Append, nil),
	}
}

// LogToolCall records a tool call attempt and its policy decision.
func (l *LedgerLogger) LogToolCall(call ToolCall, decision policy.Decision, reason string) {
	eventType := "tool.call"

	// Use more specific event types based on decision
	switch decision {
	case policy.Deny:
		eventType = "tool.denied"
	case policy.NeedsApproval:
		eventType = "tool.approval_requested"
	case policy.Allow:
		eventType = "tool.allowed"
	}

	data := map[string]interface{}{
		"call_id":  call.ID,
		"tool":     call.Name,
		"decision": decision.String(),
	}

	if len(call.Args) > 0 {
		redactedArgs := secrets.RedactStringMap(call.Args)
		data["args"] = truncateStructuredStrings(redactedArgs, 1000)
	}

	if reason != "" {
		redactedReason := secrets.RedactText(reason)
		data["reason"] = truncateText(redactedReason, 1000)
	}

	evt := ledger.Event{
		TS:      time.Now().UTC().Format(time.RFC3339Nano),
		Type:    eventType,
		Session: l.SessionID,
		Data:    data,
	}

	if err := l.ledgerWriter.Record(l.LedgerPath, audit.ClassMustPersist, evt); err != nil {
		l.reportError(fmt.Errorf("append tool call event: %w", err))
	}
}

// LogToolResult records the outcome of a tool execution.
func (l *LedgerLogger) LogToolResult(call ToolCall, result ToolResult) {
	eventType := "tool.result"

	switch result.Status {
	case StatusSuccess:
		eventType = "tool.success"
	case StatusError:
		eventType = "tool.error"
	case StatusDenied:
		eventType = "tool.denied"
	}

	data := map[string]interface{}{
		"call_id": result.CallID,
		"tool":    call.Name,
		"status":  string(result.Status),
	}

	if result.Output != "" {
		redactedOutput := secrets.RedactText(result.Output)
		data["output"] = truncateText(redactedOutput, 10000)
	}

	if result.Reason != "" {
		redactedReason := secrets.RedactText(result.Reason)
		data["reason"] = truncateText(redactedReason, 1000)
	}

	evt := ledger.Event{
		TS:      time.Now().UTC().Format(time.RFC3339Nano),
		Type:    eventType,
		Session: l.SessionID,
		Data:    data,
	}

	if err := l.ledgerWriter.Record(l.LedgerPath, audit.ClassMustPersist, evt); err != nil {
		l.reportError(fmt.Errorf("append tool result event: %w", err))
	}
}

func (l *LedgerLogger) reportError(err error) {
	if err == nil {
		return
	}
	if l.ReportError != nil {
		l.ReportError(err)
	}
}

func truncateStructuredStrings(redactedFields map[string]interface{}, maxLen int) map[string]interface{} {
	if redactedFields == nil {
		return nil
	}
	truncated := make(map[string]interface{}, len(redactedFields))
	for key, value := range redactedFields {
		textValue, isText := value.(string)
		if !isText {
			truncated[key] = value
			continue
		}
		truncated[key] = truncateText(textValue, maxLen)
	}
	return truncated
}

func truncateText(redactedText string, maxLen int) string {
	if maxLen <= 0 || len(redactedText) <= maxLen {
		return redactedText
	}
	return redactedText[:maxLen] + "... (truncated)"
}

// NullLogger is a no-op logger for testing.
type NullLogger struct{}

func (NullLogger) LogToolCall(call ToolCall, decision policy.Decision, reason string) {}
func (NullLogger) LogToolResult(call ToolCall, result ToolResult)                     {}
