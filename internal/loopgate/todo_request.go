package loopgate

import (
	"fmt"
	"strings"

	"morph/internal/identifiers"
)

func normalizeExplicitTodoWorkflowStatus(raw string) string {
	normalized := strings.TrimSpace(strings.ToLower(raw))
	switch normalized {
	case explicitTodoWorkflowStatusTodo, explicitTodoWorkflowStatusInProgress:
		return normalized
	default:
		return ""
	}
}

func validatePutExplicitTodoWorkflowStatus(raw string) error {
	if normalizeExplicitTodoWorkflowStatus(raw) == "" {
		return fmt.Errorf("status must be %q or %q", explicitTodoWorkflowStatusTodo, explicitTodoWorkflowStatusInProgress)
	}
	return nil
}

func (server *Server) normalizeTodoAddRequest(rawRequest TodoAddRequest) (TodoAddRequest, error) {
	validatedRequest := rawRequest
	validatedRequest.Scope = strings.TrimSpace(validatedRequest.Scope)
	if validatedRequest.Scope == "" {
		validatedRequest.Scope = memoryScopeGlobal
	}
	validatedRequest.Text = normalizeTodoText(validatedRequest.Text)
	validatedRequest.TaskKind = normalizeTaskKind(validatedRequest.TaskKind)
	validatedRequest.SourceKind = normalizeTaskSourceKind(validatedRequest.SourceKind)
	validatedRequest.NextStep = normalizeTodoText(validatedRequest.NextStep)
	validatedRequest.ScheduledForUTC = strings.TrimSpace(validatedRequest.ScheduledForUTC)
	validatedRequest.ExecutionClass = normalizeTaskExecutionClass(validatedRequest.ExecutionClass)
	validatedRequest.Reason = strings.TrimSpace(validatedRequest.Reason)
	if err := validatedRequest.Validate(); err != nil {
		return TodoAddRequest{}, err
	}
	if validatedRequest.Scope != memoryScopeGlobal {
		return TodoAddRequest{}, fmt.Errorf("scope must be %q", memoryScopeGlobal)
	}
	if err := validateTaskKind(validatedRequest.TaskKind); err != nil {
		return TodoAddRequest{}, err
	}
	if err := validateTaskSourceKind(validatedRequest.SourceKind); err != nil {
		return TodoAddRequest{}, err
	}
	if err := validateTaskExecutionClass(validatedRequest.ExecutionClass); err != nil {
		return TodoAddRequest{}, err
	}
	return validatedRequest, nil
}

func (server *Server) normalizeTodoCompleteRequest(rawRequest TodoCompleteRequest) (TodoCompleteRequest, error) {
	validatedRequest := rawRequest
	validatedRequest.Scope = strings.TrimSpace(validatedRequest.Scope)
	if validatedRequest.Scope == "" {
		validatedRequest.Scope = memoryScopeGlobal
	}
	validatedRequest.ItemID = strings.TrimSpace(validatedRequest.ItemID)
	validatedRequest.Reason = strings.TrimSpace(validatedRequest.Reason)
	if err := validatedRequest.Validate(); err != nil {
		return TodoCompleteRequest{}, err
	}
	if validatedRequest.Scope != memoryScopeGlobal {
		return TodoCompleteRequest{}, fmt.Errorf("scope must be %q", memoryScopeGlobal)
	}
	if err := identifiers.ValidateSafeIdentifier("item_id", validatedRequest.ItemID); err != nil {
		return TodoCompleteRequest{}, err
	}
	return validatedRequest, nil
}

func normalizeTodoText(rawText string) string {
	trimmedText := strings.TrimSpace(rawText)
	if trimmedText == "" {
		return ""
	}
	return strings.Join(strings.Fields(trimmedText), " ")
}

func normalizeTaskKind(rawTaskKind string) string {
	normalizedTaskKind := strings.TrimSpace(strings.ToLower(rawTaskKind))
	if normalizedTaskKind == "" {
		return taskKindCarryOver
	}
	return normalizedTaskKind
}

func validateTaskKind(taskKind string) error {
	switch taskKind {
	case taskKindCarryOver, taskKindOneOff, taskKindScheduled:
		return nil
	default:
		return fmt.Errorf("task_kind %q is invalid", taskKind)
	}
}

func normalizeTaskSourceKind(rawSourceKind string) string {
	normalizedSourceKind := strings.TrimSpace(strings.ToLower(rawSourceKind))
	if normalizedSourceKind == "" {
		return taskSourceUser
	}
	return normalizedSourceKind
}

func validateTaskSourceKind(sourceKind string) error {
	if sourceKind == "" {
		return fmt.Errorf("source_kind is required")
	}
	if err := identifiers.ValidateSafeIdentifier("source_kind", sourceKind); err != nil {
		return err
	}
	return nil
}
