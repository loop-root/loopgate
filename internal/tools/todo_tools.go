package tools

import (
	"context"
	"fmt"
)

// TodoAdd stores an operating task in durable continuity.
//
// Execution is routed through Loopgate's dedicated continuity pipeline rather
// than generic tool execution. The registry entry exists so the capability can
// be discovered, granted, documented, and schema-validated like other tools.
type TodoAdd struct{}

func (tool *TodoAdd) Name() string      { return "todo.add" }
func (tool *TodoAdd) Category() string  { return "filesystem" }
func (tool *TodoAdd) Operation() string { return OpWrite }

func (tool *TodoAdd) Schema() Schema {
	return Schema{
		Description: "Add a task to Haven's Task Board. Use this when the user gives you a task, reminder, or follow-up that should stay visible across sessions. Set task_kind to scheduled and provide scheduled_for_utc when the task should wait until a specific time.",
		Args: []ArgDef{
			{
				Name:        "text",
				Description: "Short single-line task text",
				Required:    true,
				Type:        "string",
				MaxLen:      200,
			},
			{
				Name:        "reason",
				Description: "Optional short rationale for why this task should stay in continuity",
				Required:    false,
				Type:        "string",
				MaxLen:      200,
			},
			{
				Name:        "task_kind",
				Description: "Optional task type such as carry_over, one_off, or scheduled",
				Required:    false,
				Type:        "string",
				MaxLen:      32,
			},
			{
				Name:        "source_kind",
				Description: "Optional source label such as user, continuity, or folder_signal",
				Required:    false,
				Type:        "string",
				MaxLen:      64,
			},
			{
				Name:        "next_step",
				Description: "Optional short next step that explains how the task should move forward",
				Required:    false,
				Type:        "string",
				MaxLen:      200,
			},
			{
				Name:        "scheduled_for_utc",
				Description: "Optional RFC3339 UTC time for scheduled tasks, such as 2026-03-21T15:30:00Z",
				Required:    false,
				Type:        "string",
				MaxLen:      64,
			},
			{
				Name:        "execution_class",
				Description: "Optional execution class. Leave blank for approval_required. Only use local_workspace_organize or local_desktop_organize for clearly sandbox-local organizing work inside Haven.",
				Required:    false,
				Type:        "string",
				MaxLen:      64,
			},
		},
	}
}

func (tool *TodoAdd) Execute(context.Context, map[string]string) (string, error) {
	return "", fmt.Errorf("todo.add must be executed through loopgate continuity handling")
}

// TodoComplete marks an existing task as done.
type TodoComplete struct{}

func (tool *TodoComplete) Name() string      { return "todo.complete" }
func (tool *TodoComplete) Category() string  { return "filesystem" }
func (tool *TodoComplete) Operation() string { return OpWrite }

func (tool *TodoComplete) Schema() Schema {
	return Schema{
		Description: "Mark a task as complete when it no longer needs attention.",
		Args: []ArgDef{
			{
				Name:        "item_id",
				Description: "The todo item identifier to complete",
				Required:    true,
				Type:        "string",
				MaxLen:      96,
			},
			{
				Name:        "reason",
				Description: "Optional short note about why the item is done",
				Required:    false,
				Type:        "string",
				MaxLen:      200,
			},
		},
	}
}

func (tool *TodoComplete) Execute(context.Context, map[string]string) (string, error) {
	return "", fmt.Errorf("todo.complete must be executed through loopgate continuity handling")
}

// TodoList lists the current open tasks and active goals.
type TodoList struct{}

func (tool *TodoList) Name() string      { return "todo.list" }
func (tool *TodoList) Category() string  { return "filesystem" }
func (tool *TodoList) Operation() string { return OpRead }

func (tool *TodoList) Schema() Schema {
	return Schema{
		Description: "List Haven's current open tasks, scheduled tasks, and active goals from durable continuity.",
	}
}

func (tool *TodoList) Execute(context.Context, map[string]string) (string, error) {
	return "", fmt.Errorf("todo.list must be executed through loopgate continuity handling")
}
