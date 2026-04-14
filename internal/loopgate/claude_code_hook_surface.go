package loopgate

import "strings"

const (
	claudeCodeHookEventPreToolUse         = "PreToolUse"
	claudeCodeHookEventPermissionRequest  = "PermissionRequest"
	claudeCodeHookEventPostToolUse        = "PostToolUse"
	claudeCodeHookEventPostToolUseFailure = "PostToolUseFailure"
	claudeCodeHookEventSessionStart       = "SessionStart"
	claudeCodeHookEventSessionEnd         = "SessionEnd"
	claudeCodeHookEventUserPromptSubmit   = "UserPromptSubmit"

	claudeCodeHookSurfacePrimaryAuthority    = "primary_authority"
	claudeCodeHookSurfaceSecondaryGovernance = "secondary_governance"
	claudeCodeHookSurfaceObservability       = "observability"
	claudeCodeHookSurfaceUnknown             = "unknown"

	claudeCodeHookHandlingModeEnforced         = "enforced"
	claudeCodeHookHandlingModeAuditOnly        = "audit_only"
	claudeCodeHookHandlingModeContextInjection = "context_injection"
	claudeCodeHookHandlingModeStateTransition  = "state_transition"
)

var claudeCodeSecondaryGovernanceEvents = map[string]struct{}{
	claudeCodeHookEventPostToolUse:        {},
	claudeCodeHookEventPostToolUseFailure: {},
	claudeCodeHookEventPermissionRequest:  {},
	"TaskCreated":                         {},
	"TaskCompleted":                       {},
	"SubagentStart":                       {},
	"SubagentStop":                        {},
}

var claudeCodeObservabilityEvents = map[string]struct{}{
	"UserPromptSubmit":   {},
	"SessionStart":       {},
	"SessionEnd":         {},
	"InstructionsLoaded": {},
	"ConfigChange":       {},
	"FileChanged":        {},
	"Notification":       {},
	"Stop":               {},
	"StopFailure":        {},
}

func normalizedClaudeCodeHookEventName(rawHookEventName string) string {
	trimmedHookEventName := strings.TrimSpace(rawHookEventName)
	if trimmedHookEventName == "" {
		return claudeCodeHookEventPreToolUse
	}
	return trimmedHookEventName
}

func classifyClaudeCodeHookEvent(rawHookEventName string) string {
	hookEventName := normalizedClaudeCodeHookEventName(rawHookEventName)
	switch hookEventName {
	case claudeCodeHookEventPreToolUse:
		return claudeCodeHookSurfacePrimaryAuthority
	case claudeCodeHookEventSessionStart, claudeCodeHookEventSessionEnd, claudeCodeHookEventUserPromptSubmit:
		return claudeCodeHookSurfaceObservability
	}
	if _, found := claudeCodeSecondaryGovernanceEvents[hookEventName]; found {
		return claudeCodeHookSurfaceSecondaryGovernance
	}
	if _, found := claudeCodeObservabilityEvents[hookEventName]; found {
		return claudeCodeHookSurfaceObservability
	}
	return claudeCodeHookSurfaceUnknown
}

func hookHandlingModeForClaudeCodeHookEvent(rawHookEventName string) string {
	if classifyClaudeCodeHookEvent(rawHookEventName) == claudeCodeHookSurfaceObservability {
		return claudeCodeHookHandlingModeAuditOnly
	}
	return claudeCodeHookHandlingModeEnforced
}
