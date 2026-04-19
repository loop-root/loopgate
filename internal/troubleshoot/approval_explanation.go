package troubleshoot

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
)

var ErrApprovalRequestNotFound = errors.New("approval request not found in verified audit ledger")

// ApprovalExplanation is a human-readable operator summary derived from authoritative audit events.
// It is a troubleshooting view only; the verified ledger remains the source of truth.
type ApprovalExplanation struct {
	ApprovalRequestID string
	CurrentStatus     string
	ApprovalClass     string
	Action            string
	CreatedAt         string
	DecisionAt        string
	DenialCode        string
	Reason            string
	Timeline          []AuditLogLine
}

// ExplainApprovalRequest reads the verified audit ledger and summarizes the lifecycle of one
// approval request for operator troubleshooting.
func ExplainApprovalRequest(repoRoot string, runtimeConfig config.RuntimeConfig, approvalRequestID string) (ApprovalExplanation, error) {
	trimmedApprovalRequestID := strings.TrimSpace(approvalRequestID)
	if trimmedApprovalRequestID == "" {
		return ApprovalExplanation{}, fmt.Errorf("approval request id is required")
	}

	explanation := ApprovalExplanation{ApprovalRequestID: trimmedApprovalRequestID}
	found := false
	lastMatchedStatus := ""
	if err := visitVerifiedAuditEvents(repoRoot, runtimeConfig, func(parsedEvent ledger.Event) error {
		if !auditEventMatchesApprovalRequestID(parsedEvent, trimmedApprovalRequestID) {
			return nil
		}
		found = true

		renderedLine := renderAuditLogLine(parsedEvent)
		explanation.Timeline = append(explanation.Timeline, renderedLine)
		lastMatchedStatus = renderedLine.Status

		if explanation.Action == "" {
			if renderedAction := renderAuditAction(parsedEvent.Data); renderedAction != "" {
				explanation.Action = renderedAction
			}
		}
		if explanation.ApprovalClass == "" {
			explanation.ApprovalClass = auditDataString(parsedEvent.Data, "approval_class")
		}
		switch parsedEvent.Type {
		case "approval.created":
			if explanation.CreatedAt == "" {
				explanation.CreatedAt = renderedLine.Timestamp
			}
			if explanation.CurrentStatus == "" {
				explanation.CurrentStatus = renderedLine.Status
			}
			if explanation.Reason == "" {
				explanation.Reason = firstNonEmptyAuditData(parsedEvent.Data, "reason", "hook_reason")
			}
		case "approval.granted":
			explanation.CurrentStatus = renderedLine.Status
			explanation.DecisionAt = renderedLine.Timestamp
			if explanation.Reason == "" {
				explanation.Reason = firstNonEmptyAuditData(parsedEvent.Data, "reason", "approval_state")
			}
		case "approval.denied":
			explanation.CurrentStatus = renderedLine.Status
			explanation.DecisionAt = renderedLine.Timestamp
			explanation.DenialCode = auditDataString(parsedEvent.Data, "denial_code")
			explanation.Reason = firstNonEmptyAuditData(parsedEvent.Data, "reason", "denial_code")
		case "approval.cancelled":
			explanation.CurrentStatus = renderedLine.Status
			explanation.DecisionAt = renderedLine.Timestamp
			if explanation.Reason == "" {
				explanation.Reason = firstNonEmptyAuditData(parsedEvent.Data, "reason", "hook_reason")
			}
		case "hook.pre_validate":
			if explanation.Reason == "" {
				explanation.Reason = firstNonEmptyAuditData(parsedEvent.Data, "reason", "hook_reason")
			}
		}
		return nil
	}); err != nil {
		return ApprovalExplanation{}, err
	}
	if !found {
		return ApprovalExplanation{}, fmt.Errorf("%w: %s", ErrApprovalRequestNotFound, trimmedApprovalRequestID)
	}
	if explanation.CurrentStatus == "" {
		explanation.CurrentStatus = strings.TrimSpace(lastMatchedStatus)
	}
	if explanation.CurrentStatus == "" {
		explanation.CurrentStatus = "INFO"
	}
	if explanation.Action == "" && len(explanation.Timeline) > 0 {
		explanation.Action = explanation.Timeline[0].Summary
	}
	return explanation, nil
}

// WriteApprovalExplanation renders a plain-English explanation for one approval request.
func WriteApprovalExplanation(writer io.Writer, explanation ApprovalExplanation) error {
	if _, err := fmt.Fprintf(writer, "Approval request: %s\n", strings.TrimSpace(explanation.ApprovalRequestID)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Current status: %s\n", strings.TrimSpace(explanation.CurrentStatus)); err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(explanation.CurrentStatus), "DENIED") {
		if _, err := fmt.Fprintf(writer, "Note: approval request is currently %s, not denied.\n", strings.ToLower(strings.TrimSpace(explanation.CurrentStatus))); err != nil {
			return err
		}
	}
	if strings.TrimSpace(explanation.ApprovalClass) != "" {
		if _, err := fmt.Fprintf(writer, "Approval class: %s\n", strings.TrimSpace(explanation.ApprovalClass)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(explanation.Action) != "" {
		if _, err := fmt.Fprintf(writer, "Action: %s\n", strings.TrimSpace(explanation.Action)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(explanation.CreatedAt) != "" {
		if _, err := fmt.Fprintf(writer, "Created at: %s\n", strings.TrimSpace(explanation.CreatedAt)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(explanation.DecisionAt) != "" {
		if _, err := fmt.Fprintf(writer, "Decision at: %s\n", strings.TrimSpace(explanation.DecisionAt)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(explanation.DenialCode) != "" {
		if _, err := fmt.Fprintf(writer, "Denial code: %s\n", strings.TrimSpace(explanation.DenialCode)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(explanation.Reason) != "" {
		if _, err := fmt.Fprintf(writer, "Reason: %s\n", strings.TrimSpace(explanation.Reason)); err != nil {
			return err
		}
	}
	if len(explanation.Timeline) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(writer); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(writer, "Timeline:"); err != nil {
		return err
	}
	return WriteAuditLog(writer, explanation.Timeline)
}

func auditEventMatchesApprovalRequestID(parsedEvent ledger.Event, approvalRequestID string) bool {
	trimmedApprovalRequestID := strings.TrimSpace(approvalRequestID)
	if trimmedApprovalRequestID == "" {
		return false
	}
	return auditDataString(parsedEvent.Data, "approval_request_id") == trimmedApprovalRequestID ||
		auditDataString(parsedEvent.Data, "hook_approval_request_id") == trimmedApprovalRequestID
}
