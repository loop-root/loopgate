package troubleshoot

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
)

const defaultAuditLogLimit = 50

// AuditLogLine is a human-readable operator projection of an authoritative audit event.
// It is derived content only; the JSONL ledger remains the source of truth.
type AuditLogLine struct {
	Timestamp string
	Status    string
	EventType string
	Summary   string
}

// ReadRecentAuditLog verifies the audit chain and returns a derived operator-friendly view
// of the most recent events across sealed segments and the active JSONL file.
func ReadRecentAuditLog(repoRoot string, runtimeConfig config.RuntimeConfig, limit int) ([]AuditLogLine, error) {
	if limit <= 0 {
		limit = defaultAuditLogLimit
	}
	lineRing := newAuditLogRing(limit)
	if err := visitVerifiedAuditEvents(repoRoot, runtimeConfig, func(parsedEvent ledger.Event) error {
		lineRing.Append(renderAuditLogLine(parsedEvent))
		return nil
	}); err != nil {
		return nil, err
	}
	return lineRing.Lines(), nil
}

// WriteAuditLog writes a table-like readable audit projection for operators and demos.
func WriteAuditLog(writer io.Writer, auditLogLines []AuditLogLine) error {
	if len(auditLogLines) == 0 {
		_, err := fmt.Fprintln(writer, "no audit events found")
		return err
	}
	if _, err := fmt.Fprintf(writer, "%-23s  %-10s  %-22s  %s\n", "TIME", "STATUS", "EVENT", "SUMMARY"); err != nil {
		return err
	}
	for _, auditLogLine := range auditLogLines {
		if _, err := fmt.Fprintf(
			writer,
			"%-23s  %-10s  %-22s  %s\n",
			strings.TrimSpace(auditLogLine.Timestamp),
			strings.TrimSpace(auditLogLine.Status),
			strings.TrimSpace(auditLogLine.EventType),
			strings.TrimSpace(auditLogLine.Summary),
		); err != nil {
			return err
		}
	}
	return nil
}

func visitVerifiedAuditEvents(repoRoot string, runtimeConfig config.RuntimeConfig, visit func(ledger.Event) error) error {
	activeAuditPath := ActiveAuditPath(repoRoot)
	rotationSettings := AuditRotationSettings(repoRoot, runtimeConfig)
	if _, _, err := ledger.ReadSegmentedChainState(activeAuditPath, "audit_sequence", rotationSettings); err != nil {
		return fmt.Errorf("verify audit ledger: %w", err)
	}

	auditPaths, err := ledger.OrderedSegmentedPaths(activeAuditPath, rotationSettings)
	if err != nil {
		return err
	}
	for _, auditPath := range auditPaths {
		if err := scanAuditFile(auditPath, visit); err != nil {
			return err
		}
	}
	return nil
}

func scanAuditFile(auditPath string, visit func(ledger.Event) error) error {
	auditHandle, err := os.Open(auditPath)
	if err != nil {
		return fmt.Errorf("open audit file %q: %w", auditPath, err)
	}
	defer auditHandle.Close()

	auditScanner := bufio.NewScanner(auditHandle)
	auditScanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for auditScanner.Scan() {
		parsedEvent, ok := ledger.ParseEvent(auditScanner.Bytes())
		if !ok {
			return fmt.Errorf("malformed audit line in %s", auditPath)
		}
		if err := visit(parsedEvent); err != nil {
			return err
		}
	}
	if err := auditScanner.Err(); err != nil {
		return fmt.Errorf("scan audit file %q: %w", auditPath, err)
	}
	return nil
}

func renderAuditLogLine(auditEvent ledger.Event) AuditLogLine {
	return AuditLogLine{
		Timestamp: displayAuditTimestamp(auditEvent.TS),
		Status:    renderAuditStatus(auditEvent),
		EventType: auditEvent.Type,
		Summary:   renderAuditSummary(auditEvent),
	}
}

func displayAuditTimestamp(rawTimestamp string) string {
	parsedTimestamp, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(rawTimestamp))
	if err != nil {
		return strings.TrimSpace(rawTimestamp)
	}
	return parsedTimestamp.In(time.Local).Format("2006-01-02 15:04:05 MST")
}

func renderAuditStatus(auditEvent ledger.Event) string {
	switch auditEvent.Type {
	case "hook.pre_validate":
		if decision := strings.ToUpper(auditDataString(auditEvent.Data, "decision")); decision != "" {
			return decision
		}
		return "INFO"
	case "approval.created":
		return "PENDING"
	case "approval.granted":
		switch strings.TrimSpace(auditDataString(auditEvent.Data, "approval_state")) {
		case "executed":
			return "EXECUTED"
		case "execution_failed":
			return "FAILED"
		default:
			return "GRANTED"
		}
	case "approval.denied":
		return "DENIED"
	case "approval.cancelled":
		return "CANCELLED"
	case "capability.requested":
		return "REQUESTED"
	case "capability.executed":
		if responseStatus := strings.ToUpper(auditDataString(auditEvent.Data, "status")); responseStatus != "" {
			return responseStatus
		}
		return "EXECUTED"
	case "capability.denied":
		return "DENIED"
	case "capability.error":
		return "ERROR"
	case "session.opened":
		return "OPENED"
	case "session.closed":
		return "CLOSED"
	default:
		return "INFO"
	}
}

func renderAuditSummary(auditEvent ledger.Event) string {
	switch auditEvent.Type {
	case "hook.pre_validate":
		return appendAuditReason(renderAuditAction(auditEvent.Data), auditDataString(auditEvent.Data, "reason"), auditEvent.Type)
	case "approval.created":
		return appendAuditReason(renderAuditAction(auditEvent.Data), "approval "+shortAuditID(auditDataString(auditEvent.Data, "approval_request_id"))+" created", auditEvent.Type)
	case "approval.granted":
		return appendAuditReason(renderAuditAction(auditEvent.Data), auditDataString(auditEvent.Data, "reason"), auditEvent.Type)
	case "approval.denied":
		return appendAuditReason(renderAuditAction(auditEvent.Data), firstNonEmptyAuditData(auditEvent.Data, "reason", "denial_code"), auditEvent.Type)
	case "approval.cancelled":
		return appendAuditReason(renderAuditAction(auditEvent.Data), firstNonEmptyAuditData(auditEvent.Data, "reason", "hook_reason"), auditEvent.Type)
	case "capability.requested":
		return renderAuditAction(auditEvent.Data)
	case "capability.executed":
		return appendAuditReason(renderAuditAction(auditEvent.Data), auditDataString(auditEvent.Data, "status"), auditEvent.Type)
	case "capability.denied":
		return appendAuditReason(renderAuditAction(auditEvent.Data), firstNonEmptyAuditData(auditEvent.Data, "reason", "denial_code"), auditEvent.Type)
	case "capability.error":
		return appendAuditReason(renderAuditAction(auditEvent.Data), firstNonEmptyAuditData(auditEvent.Data, "operator_error_class", "error"), auditEvent.Type)
	case "session.opened":
		return firstNonEmptyAuditData(auditEvent.Data, "actor_label", "client_session_label", "control_session_id")
	case "session.closed":
		return appendAuditReason(firstNonEmptyAuditData(auditEvent.Data, "actor_label", "client_session_label", "control_session_id"), auditDataString(auditEvent.Data, "reason"), auditEvent.Type)
	default:
		fallbackSummary := renderAuditAction(auditEvent.Data)
		if fallbackSummary != "" {
			return fallbackSummary
		}
		return firstNonEmptyAuditData(auditEvent.Data, "reason", "request_id", "approval_request_id")
	}
}

func renderAuditAction(auditData map[string]interface{}) string {
	commandPreview := auditDataString(auditData, "command_redacted_preview")
	if commandPreview != "" {
		return joinAuditParts(auditDataString(auditData, "tool_name"), commandPreview)
	}
	queryPreview := auditDataString(auditData, "query_redacted_preview")
	if queryPreview != "" {
		return joinAuditParts(auditDataString(auditData, "tool_name"), queryPreview)
	}
	urlPreview := auditDataString(auditData, "url_redacted_preview")
	if urlPreview != "" {
		return joinAuditParts(auditDataString(auditData, "tool_name"), urlPreview)
	}
	resolvedTargetPath := firstNonEmptyAuditData(auditData, "resolved_target_path", "target_path")
	if resolvedTargetPath != "" {
		return joinAuditParts(firstNonEmptyAuditData(auditData, "tool_name", "capability"), resolvedTargetPath)
	}
	requestHost := auditDataString(auditData, "request_host")
	if requestHost != "" {
		return joinAuditParts(auditDataString(auditData, "tool_name"), requestHost)
	}
	commandVerb := auditDataString(auditData, "command_verb")
	if commandVerb != "" {
		return joinAuditParts(auditDataString(auditData, "tool_name"), commandVerb)
	}
	return firstNonEmptyAuditData(auditData, "capability", "tool_name", "approval_request_id", "request_id")
}

func appendAuditReason(summary string, reason string, eventType string) string {
	trimmedSummary := strings.TrimSpace(summary)
	trimmedReason := strings.TrimSpace(reason)
	if trimmedReason == "" {
		return trimmedSummary
	}
	if trimmedSummary == "" {
		return trimmedReason
	}
	switch eventType {
	case "hook.pre_validate", "capability.denied", "capability.error", "approval.denied", "approval.cancelled", "session.closed":
		return trimmedSummary + " - " + trimmedReason
	case "approval.created":
		return trimmedSummary + " (" + trimmedReason + ")"
	case "capability.executed":
		if strings.EqualFold(trimmedReason, "success") {
			return trimmedSummary
		}
	}
	return trimmedSummary + " - " + trimmedReason
}

func shortAuditID(rawID string) string {
	trimmedID := strings.TrimSpace(rawID)
	if len(trimmedID) <= 8 {
		return trimmedID
	}
	return trimmedID[:8]
}

func joinAuditParts(parts ...string) string {
	trimmedParts := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		trimmedParts = append(trimmedParts, part)
	}
	return strings.Join(trimmedParts, " ")
}

func firstNonEmptyAuditData(auditData map[string]interface{}, fieldNames ...string) string {
	for _, fieldName := range fieldNames {
		if fieldValue := auditDataString(auditData, fieldName); fieldValue != "" {
			return fieldValue
		}
	}
	return ""
}

func auditDataString(auditData map[string]interface{}, fieldName string) string {
	if auditData == nil {
		return ""
	}
	rawFieldValue, ok := auditData[fieldName]
	if !ok || rawFieldValue == nil {
		return ""
	}
	switch typedFieldValue := rawFieldValue.(type) {
	case string:
		return strings.TrimSpace(typedFieldValue)
	case float64:
		return fmt.Sprintf("%.0f", typedFieldValue)
	case int:
		return fmt.Sprintf("%d", typedFieldValue)
	case int64:
		return fmt.Sprintf("%d", typedFieldValue)
	case bool:
		if typedFieldValue {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

type auditLogRing struct {
	lines []AuditLogLine
	start int
	count int
}

func newAuditLogRing(limit int) *auditLogRing {
	if limit <= 0 {
		limit = defaultAuditLogLimit
	}
	return &auditLogRing{
		lines: make([]AuditLogLine, limit),
	}
}

func (auditLogRing *auditLogRing) Append(auditLogLine AuditLogLine) {
	if len(auditLogRing.lines) == 0 {
		return
	}
	if auditLogRing.count < len(auditLogRing.lines) {
		auditLogRing.lines[auditLogRing.count] = auditLogLine
		auditLogRing.count++
		return
	}
	auditLogRing.lines[auditLogRing.start] = auditLogLine
	auditLogRing.start = (auditLogRing.start + 1) % len(auditLogRing.lines)
}

func (auditLogRing *auditLogRing) Lines() []AuditLogLine {
	if auditLogRing.count == 0 {
		return nil
	}
	orderedLines := make([]AuditLogLine, 0, auditLogRing.count)
	if auditLogRing.count < len(auditLogRing.lines) {
		return append(orderedLines, auditLogRing.lines[:auditLogRing.count]...)
	}
	for lineIndex := 0; lineIndex < auditLogRing.count; lineIndex++ {
		orderedLines = append(orderedLines, auditLogRing.lines[(auditLogRing.start+lineIndex)%len(auditLogRing.lines)])
	}
	return orderedLines
}
