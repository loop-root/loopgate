package troubleshoot

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
)

var ErrCapabilityRequestNotFound = errors.New("capability request not found in verified audit ledger")

// CapabilityRequestExplanation is a human-readable operator summary derived from authoritative audit events.
// It is a troubleshooting view only; the verified ledger remains the source of truth.
type CapabilityRequestExplanation struct {
	RequestID          string
	CurrentStatus      string
	Capability         string
	Action             string
	RequestedAt        string
	FinalizedAt        string
	DenialCode         string
	OperatorErrorClass string
	Reason             string
	ApprovalRequestID  string
	Timeline           []AuditLogLine
}

// ExplainCapabilityRequest reads the verified audit ledger and summarizes the lifecycle of one
// capability request for operator troubleshooting.
func ExplainCapabilityRequest(repoRoot string, runtimeConfig config.RuntimeConfig, requestID string) (CapabilityRequestExplanation, error) {
	trimmedRequestID := strings.TrimSpace(requestID)
	if trimmedRequestID == "" {
		return CapabilityRequestExplanation{}, fmt.Errorf("request id is required")
	}

	explanation := CapabilityRequestExplanation{RequestID: trimmedRequestID}
	found := false
	lastMatchedStatus := ""
	if err := visitVerifiedAuditEvents(repoRoot, runtimeConfig, func(parsedEvent ledger.Event) error {
		if auditDataString(parsedEvent.Data, "request_id") != trimmedRequestID {
			return nil
		}
		found = true

		renderedLine := renderAuditLogLine(parsedEvent)
		explanation.Timeline = append(explanation.Timeline, renderedLine)
		lastMatchedStatus = renderedLine.Status

		if explanation.Capability == "" {
			explanation.Capability = auditDataString(parsedEvent.Data, "capability")
		}
		if explanation.Action == "" {
			if renderedAction := renderAuditAction(parsedEvent.Data); renderedAction != "" {
				explanation.Action = renderedAction
			}
		}
		if explanation.ApprovalRequestID == "" {
			explanation.ApprovalRequestID = auditDataString(parsedEvent.Data, "approval_request_id")
		}
		switch parsedEvent.Type {
		case "capability.requested":
			if explanation.RequestedAt == "" {
				explanation.RequestedAt = renderedLine.Timestamp
			}
			if explanation.CurrentStatus == "" {
				explanation.CurrentStatus = renderedLine.Status
			}
			if explanation.Reason == "" {
				explanation.Reason = auditDataString(parsedEvent.Data, "reason")
			}
		case "approval.created":
			explanation.CurrentStatus = "PENDING_APPROVAL"
			if explanation.ApprovalRequestID == "" {
				explanation.ApprovalRequestID = auditDataString(parsedEvent.Data, "approval_request_id")
			}
			if explanation.Reason == "" {
				explanation.Reason = auditDataString(parsedEvent.Data, "reason")
			}
		case "capability.denied":
			explanation.CurrentStatus = renderedLine.Status
			explanation.FinalizedAt = renderedLine.Timestamp
			explanation.DenialCode = auditDataString(parsedEvent.Data, "denial_code")
			explanation.Reason = firstNonEmptyAuditData(parsedEvent.Data, "reason", "denial_code")
		case "capability.error":
			explanation.CurrentStatus = renderedLine.Status
			explanation.FinalizedAt = renderedLine.Timestamp
			explanation.OperatorErrorClass = auditDataString(parsedEvent.Data, "operator_error_class")
			explanation.Reason = firstNonEmptyAuditData(parsedEvent.Data, "error", "operator_error_class")
		case "capability.executed":
			explanation.CurrentStatus = renderedLine.Status
			explanation.FinalizedAt = renderedLine.Timestamp
		}
		return nil
	}); err != nil {
		return CapabilityRequestExplanation{}, err
	}
	if !found {
		return CapabilityRequestExplanation{}, fmt.Errorf("%w: %s", ErrCapabilityRequestNotFound, trimmedRequestID)
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

// WriteCapabilityRequestExplanation renders a plain-English explanation for one capability request.
func WriteCapabilityRequestExplanation(writer io.Writer, explanation CapabilityRequestExplanation) error {
	if _, err := fmt.Fprintf(writer, "Request: %s\n", strings.TrimSpace(explanation.RequestID)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Current status: %s\n", strings.TrimSpace(explanation.CurrentStatus)); err != nil {
		return err
	}
	if strings.TrimSpace(explanation.Capability) != "" {
		if _, err := fmt.Fprintf(writer, "Capability: %s\n", strings.TrimSpace(explanation.Capability)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(explanation.Action) != "" {
		if _, err := fmt.Fprintf(writer, "Action: %s\n", strings.TrimSpace(explanation.Action)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(explanation.RequestedAt) != "" {
		if _, err := fmt.Fprintf(writer, "Requested at: %s\n", strings.TrimSpace(explanation.RequestedAt)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(explanation.FinalizedAt) != "" {
		if _, err := fmt.Fprintf(writer, "Finalized at: %s\n", strings.TrimSpace(explanation.FinalizedAt)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(explanation.DenialCode) != "" {
		if _, err := fmt.Fprintf(writer, "Denial code: %s\n", strings.TrimSpace(explanation.DenialCode)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(explanation.OperatorErrorClass) != "" {
		if _, err := fmt.Fprintf(writer, "Operator error class: %s\n", strings.TrimSpace(explanation.OperatorErrorClass)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(explanation.Reason) != "" {
		if _, err := fmt.Fprintf(writer, "Reason: %s\n", strings.TrimSpace(explanation.Reason)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(explanation.ApprovalRequestID) != "" {
		if _, err := fmt.Fprintf(writer, "Approval request id: %s\n", strings.TrimSpace(explanation.ApprovalRequestID)); err != nil {
			return err
		}
		if strings.EqualFold(strings.TrimSpace(explanation.CurrentStatus), "PENDING_APPROVAL") {
			if _, err := fmt.Fprintf(writer, "Next step: use loopgate-doctor explain-denial -approval-id %s for the approval timeline.\n", strings.TrimSpace(explanation.ApprovalRequestID)); err != nil {
				return err
			}
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
