package troubleshoot

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
)

const defaultHookExplanationTimelineLimit = 5

var ErrHookBlockNotFound = errors.New("hook block not found in verified audit ledger")

// HookBlockQuery selects a blocked hook.pre_validate event from the verified ledger.
type HookBlockQuery struct {
	SessionID     string
	ToolUseID     string
	HookEventName string
}

// HookBlockExplanation is a human-readable operator summary derived from authoritative hook audit events.
// It is a troubleshooting view only; the verified ledger remains the source of truth.
type HookBlockExplanation struct {
	SessionID     string
	ToolUseID     string
	HookEventName string
	ToolName      string
	SurfaceClass  string
	HandlingMode  string
	Reason        string
	DenialCode    string
	MatchedAt     string
	MatchedLatest bool
	Timeline      []AuditLogLine
}

// ExplainHookBlock reads the verified audit ledger and summarizes one blocked hook.pre_validate event.
// When only SessionID is provided, it selects the most recent blocked hook event in that session.
func ExplainHookBlock(repoRoot string, runtimeConfig config.RuntimeConfig, query HookBlockQuery) (HookBlockExplanation, error) {
	normalizedQuery := HookBlockQuery{
		SessionID:     strings.TrimSpace(query.SessionID),
		ToolUseID:     strings.TrimSpace(query.ToolUseID),
		HookEventName: strings.TrimSpace(query.HookEventName),
	}
	if normalizedQuery.SessionID == "" {
		return HookBlockExplanation{}, fmt.Errorf("hook session id is required")
	}

	var (
		foundBlock         bool
		matchedExplanation HookBlockExplanation
		timelineRing       = newAuditLogRing(defaultHookExplanationTimelineLimit)
		matchedBlockCount  int
	)
	if err := visitVerifiedAuditEvents(repoRoot, runtimeConfig, func(parsedEvent ledger.Event) error {
		if parsedEvent.Type != "hook.pre_validate" {
			return nil
		}
		if strings.TrimSpace(parsedEvent.Session) != normalizedQuery.SessionID {
			return nil
		}
		if normalizedQuery.ToolUseID != "" && auditDataString(parsedEvent.Data, "tool_use_id") != normalizedQuery.ToolUseID {
			return nil
		}
		if normalizedQuery.HookEventName != "" && auditDataString(parsedEvent.Data, "hook_event_name") != normalizedQuery.HookEventName {
			return nil
		}

		renderedLine := renderAuditLogLine(parsedEvent)
		timelineRing.Append(renderedLine)

		if !strings.EqualFold(auditDataString(parsedEvent.Data, "decision"), "block") {
			return nil
		}
		foundBlock = true
		matchedBlockCount++
		matchedExplanation = HookBlockExplanation{
			SessionID:     normalizedQuery.SessionID,
			ToolUseID:     auditDataString(parsedEvent.Data, "tool_use_id"),
			HookEventName: auditDataString(parsedEvent.Data, "hook_event_name"),
			ToolName:      auditDataString(parsedEvent.Data, "tool_name"),
			SurfaceClass:  auditDataString(parsedEvent.Data, "hook_surface_class"),
			HandlingMode:  auditDataString(parsedEvent.Data, "hook_handling_mode"),
			Reason:        firstNonEmptyAuditData(parsedEvent.Data, "reason", "hook_reason"),
			DenialCode:    auditDataString(parsedEvent.Data, "denial_code"),
			MatchedAt:     renderedLine.Timestamp,
		}
		return nil
	}); err != nil {
		return HookBlockExplanation{}, err
	}
	if !foundBlock {
		return HookBlockExplanation{}, fmt.Errorf("%w: session_id=%s", ErrHookBlockNotFound, normalizedQuery.SessionID)
	}
	matchedExplanation.MatchedLatest = normalizedQuery.ToolUseID == "" && normalizedQuery.HookEventName == "" && matchedBlockCount > 1
	matchedExplanation.Timeline = timelineRing.Lines()
	return matchedExplanation, nil
}

// WriteHookBlockExplanation renders a plain-English explanation for one blocked hook event.
func WriteHookBlockExplanation(writer io.Writer, explanation HookBlockExplanation) error {
	if _, err := fmt.Fprintf(writer, "Hook session: %s\n", strings.TrimSpace(explanation.SessionID)); err != nil {
		return err
	}
	if explanation.MatchedLatest {
		if _, err := fmt.Fprintln(writer, "Selection: latest blocked hook event in this session"); err != nil {
			return err
		}
	}
	if strings.TrimSpace(explanation.MatchedAt) != "" {
		if _, err := fmt.Fprintf(writer, "Matched at: %s\n", strings.TrimSpace(explanation.MatchedAt)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(explanation.HookEventName) != "" {
		if _, err := fmt.Fprintf(writer, "Hook event: %s\n", strings.TrimSpace(explanation.HookEventName)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(explanation.ToolUseID) != "" {
		if _, err := fmt.Fprintf(writer, "Tool use id: %s\n", strings.TrimSpace(explanation.ToolUseID)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(explanation.ToolName) != "" {
		if _, err := fmt.Fprintf(writer, "Tool: %s\n", strings.TrimSpace(explanation.ToolName)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(explanation.SurfaceClass) != "" {
		if _, err := fmt.Fprintf(writer, "Surface class: %s\n", strings.TrimSpace(explanation.SurfaceClass)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(explanation.HandlingMode) != "" {
		if _, err := fmt.Fprintf(writer, "Handling mode: %s\n", strings.TrimSpace(explanation.HandlingMode)); err != nil {
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
