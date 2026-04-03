package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"morph/internal/loopgate"
)

type TodoActionResponse struct {
	Applied bool   `json:"applied"`
	ItemID  string `json:"item_id,omitempty"`
	Error   string `json:"error,omitempty"`
}

type TaskDraft struct {
	Text            string `json:"text"`
	NextStep        string `json:"next_step,omitempty"`
	ScheduledForUTC string `json:"scheduled_for_utc,omitempty"`
	ExecutionClass  string `json:"execution_class,omitempty"`
}

func (app *HavenApp) AddTask(draft TaskDraft) TodoActionResponse {
	normalizedText := strings.Join(strings.Fields(strings.TrimSpace(draft.Text)), " ")
	if normalizedText == "" {
		return TodoActionResponse{Error: "task text is required"}
	}
	normalizedNextStep := strings.Join(strings.Fields(strings.TrimSpace(draft.NextStep)), " ")
	normalizedScheduledForUTC := strings.TrimSpace(draft.ScheduledForUTC)
	normalizedExecutionClass := strings.TrimSpace(draft.ExecutionClass)
	if normalizedScheduledForUTC != "" {
		if _, err := time.Parse(time.RFC3339Nano, normalizedScheduledForUTC); err != nil {
			return TodoActionResponse{Error: fmt.Sprintf("scheduled task time is invalid: %v", err)}
		}
	}

	if app.idleManager != nil {
		app.idleManager.NotifyActivity()
	}

	taskKind := "carry_over"
	if normalizedScheduledForUTC != "" {
		taskKind = "scheduled"
	}

	response, err := app.loopgateClient.ExecuteCapability(context.Background(), loopgate.CapabilityRequest{
		RequestID:  fmt.Sprintf("todo-add-%d", time.Now().UTC().UnixNano()),
		Actor:      "haven",
		Capability: "todo.add",
		Arguments: map[string]string{
			"text":              normalizedText,
			"task_kind":         taskKind,
			"source_kind":       "user",
			"next_step":         normalizedNextStep,
			"scheduled_for_utc": normalizedScheduledForUTC,
			"execution_class":   normalizedExecutionClass,
		},
	})
	if err != nil {
		return TodoActionResponse{Error: fmt.Sprintf("add task: %v", err)}
	}
	if response.Status != loopgate.ResponseStatusSuccess {
		denialReason := response.DenialReason
		if denialReason == "" {
			denialReason = "task could not be added"
		}
		return TodoActionResponse{Error: denialReason}
	}

	app.RefreshWakeState()
	itemID, _ := response.StructuredResult["item_id"].(string)
	return TodoActionResponse{
		Applied: true,
		ItemID:  itemID,
	}
}

func (app *HavenApp) AddTodo(text string) TodoActionResponse {
	return app.AddTask(TaskDraft{Text: text})
}

func (app *HavenApp) CompleteTodo(itemID string) TodoActionResponse {
	validatedItemID := strings.TrimSpace(itemID)
	if validatedItemID == "" {
		return TodoActionResponse{Error: "item id is required"}
	}
	if app.idleManager != nil {
		app.idleManager.NotifyActivity()
	}

	response, err := app.loopgateClient.ExecuteCapability(context.Background(), loopgate.CapabilityRequest{
		RequestID:  fmt.Sprintf("todo-complete-%d", time.Now().UTC().UnixNano()),
		Actor:      "haven",
		Capability: "todo.complete",
		Arguments: map[string]string{
			"item_id": validatedItemID,
		},
	})
	if err != nil {
		return TodoActionResponse{Error: fmt.Sprintf("complete todo: %v", err)}
	}
	if response.Status != loopgate.ResponseStatusSuccess {
		denialReason := response.DenialReason
		if denialReason == "" {
			denialReason = "todo item could not be completed"
		}
		return TodoActionResponse{Error: denialReason}
	}

	app.RefreshWakeState()
	return TodoActionResponse{
		Applied: true,
		ItemID:  validatedItemID,
	}
}
