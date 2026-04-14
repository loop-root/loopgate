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

func isExplicitTodoDistillate(distillateRecord continuityDistillateRecord) bool {
	for _, sourceRef := range distillateRecord.SourceRefs {
		if sourceRef.Kind == explicitTodoSourceKind {
			return true
		}
	}
	return false
}

// explicitTodoTaskMetadataFromDistillate remains as a legacy continuity reader so
// older task-shaped continuity records can still load deterministically after the
// task board feature retirement.
func explicitTodoTaskMetadataFromDistillate(distillateRecord continuityDistillateRecord) MemoryWakeStateOpenItem {
	taskMetadata := MemoryWakeStateOpenItem{
		TaskKind:       taskKindCarryOver,
		SourceKind:     taskSourceContinuity,
		ExecutionClass: TaskExecutionClassApprovalRequired,
		CreatedAtUTC:   distillateRecord.CreatedAtUTC,
	}
	if isExplicitTodoDistillate(distillateRecord) {
		taskMetadata.SourceKind = taskSourceUser
	}
	for _, factRecord := range distillateRecord.Facts {
		factValue, _ := factRecord.Value.(string)
		switch strings.TrimSpace(factRecord.Name) {
		case taskFactKind:
			normalizedTaskKind := normalizeTaskKind(factValue)
			if validateTaskKind(normalizedTaskKind) == nil {
				taskMetadata.TaskKind = normalizedTaskKind
			}
		case taskFactSourceKind:
			normalizedSourceKind := normalizeTaskSourceKind(factValue)
			if validateTaskSourceKind(normalizedSourceKind) == nil {
				taskMetadata.SourceKind = normalizedSourceKind
			}
		case taskFactNextStep:
			taskMetadata.NextStep = normalizeTodoText(factValue)
		case taskFactScheduledForUT:
			taskMetadata.ScheduledForUTC = strings.TrimSpace(factValue)
		case taskFactExecutionClass:
			normalizedExecutionClass := normalizeTaskExecutionClass(factValue)
			if validateTaskExecutionClass(normalizedExecutionClass) == nil {
				taskMetadata.ExecutionClass = normalizedExecutionClass
			}
		}
	}
	return taskMetadata
}
