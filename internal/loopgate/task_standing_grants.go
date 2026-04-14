package loopgate

import (
	"fmt"
	"strings"
)

const (
	TaskExecutionClassApprovalRequired       = "approval_required"
	TaskExecutionClassLocalWorkspaceOrganize = "local_workspace_organize"
	TaskExecutionClassLocalDesktopOrganize   = "local_desktop_organize"
)

type taskExecutionClassDefinition struct {
	Class       string
	Label       string
	Description string
	SandboxOnly bool
}

var taskExecutionClassCatalog = map[string]taskExecutionClassDefinition{
	TaskExecutionClassApprovalRequired: {
		Class:       TaskExecutionClassApprovalRequired,
		Label:       "Ask Before Running",
		Description: "Use Loopgate approval before starting this task.",
	},
	TaskExecutionClassLocalWorkspaceOrganize: {
		Class:       TaskExecutionClassLocalWorkspaceOrganize,
		Label:       "Organize Haven Files",
		Description: "Rearrange or tidy files inside Morph's own Haven workspace only.",
		SandboxOnly: true,
	},
	TaskExecutionClassLocalDesktopOrganize: {
		Class:       TaskExecutionClassLocalDesktopOrganize,
		Label:       "Organize Haven Desktop",
		Description: "Rearrange desktop items and layout inside Haven only.",
		SandboxOnly: true,
	},
}

func normalizeTaskExecutionClass(rawExecutionClass string) string {
	normalizedExecutionClass := strings.TrimSpace(strings.ToLower(rawExecutionClass))
	if normalizedExecutionClass == "" {
		return TaskExecutionClassApprovalRequired
	}
	return normalizedExecutionClass
}

func validateTaskExecutionClass(executionClass string) error {
	classDefinition, found := taskExecutionClassCatalog[executionClass]
	if !found {
		return fmt.Errorf("execution_class %q is invalid", executionClass)
	}
	if strings.TrimSpace(classDefinition.Class) == "" {
		return fmt.Errorf("execution_class %q is invalid", executionClass)
	}
	return nil
}

func taskExecutionClassLabel(executionClass string) string {
	classDefinition, found := taskExecutionClassCatalog[normalizeTaskExecutionClass(executionClass)]
	if !found {
		return ""
	}
	return classDefinition.Label
}

func taskExecutionClassDescription(executionClass string) string {
	classDefinition, found := taskExecutionClassCatalog[normalizeTaskExecutionClass(executionClass)]
	if !found {
		return ""
	}
	return classDefinition.Description
}

func taskExecutionClassSandboxOnly(executionClass string) bool {
	classDefinition, found := taskExecutionClassCatalog[normalizeTaskExecutionClass(executionClass)]
	if !found {
		return false
	}
	return classDefinition.SandboxOnly
}
