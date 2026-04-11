package loopgate

import (
	"fmt"
	"strings"
)

func todoAddSuccessText(addResponse TodoAddResponse) string {
	if addResponse.AlreadyPresent {
		return fmt.Sprintf("You are already carrying %q on the task board.", addResponse.Text)
	}
	return fmt.Sprintf("Added %q to the task board.", addResponse.Text)
}

func todoCompleteSuccessText(completeResponse TodoCompleteResponse) string {
	if strings.TrimSpace(completeResponse.Text) == "" {
		return fmt.Sprintf("Marked %s as complete.", completeResponse.ItemID)
	}
	return fmt.Sprintf("Marked %q as complete.", completeResponse.Text)
}

// Limits for todo.list text echoed back into model prompts (TPM / context budget).
const (
	maxTodoListOpenTaskLinesInPrompt = 24
	maxTodoListGoalLinesInPrompt     = 16
	maxTodoListContentRunesInPrompt  = 6000
	maxTodoListStructuredItemsInJSON = 80
	maxTodoListSingleItemRunesInJSON = 400
)

func todoListContentText(listResponse TodoListResponse) string {
	if len(listResponse.UnresolvedItems) == 0 && len(listResponse.ActiveGoals) == 0 {
		return "The task board is clear right now."
	}

	lines := make([]string, 0, len(listResponse.UnresolvedItems)+len(listResponse.ActiveGoals)+4)
	omittedTasks := 0
	omittedGoals := 0
	if len(listResponse.UnresolvedItems) > 0 {
		lines = append(lines, "Open tasks:")
		limit := maxTodoListOpenTaskLinesInPrompt
		if len(listResponse.UnresolvedItems) > limit {
			omittedTasks = len(listResponse.UnresolvedItems) - limit
		}
		for taskIndex, unresolvedItem := range listResponse.UnresolvedItems {
			if taskIndex >= limit {
				break
			}
			itemText := strings.TrimSpace(unresolvedItem.Text)
			if itemText == "" {
				itemText = unresolvedItem.ID
			}
			if strings.TrimSpace(unresolvedItem.NextStep) != "" {
				lines = append(lines, "- "+itemText+" (next: "+strings.TrimSpace(unresolvedItem.NextStep)+")")
				continue
			}
			lines = append(lines, "- "+itemText)
		}
		if omittedTasks > 0 {
			lines = append(lines, fmt.Sprintf("… and %d more open task(s) not shown (use todo.list sparingly; complete or summarize tasks to reduce context).", omittedTasks))
		}
	}
	if len(listResponse.ActiveGoals) > 0 {
		lines = append(lines, "Active goals:")
		goalLimit := maxTodoListGoalLinesInPrompt
		if len(listResponse.ActiveGoals) > goalLimit {
			omittedGoals = len(listResponse.ActiveGoals) - goalLimit
		}
		for goalIndex, activeGoal := range listResponse.ActiveGoals {
			if goalIndex >= goalLimit {
				break
			}
			lines = append(lines, "- "+strings.TrimSpace(activeGoal))
		}
		if omittedGoals > 0 {
			lines = append(lines, fmt.Sprintf("… and %d more active goal(s) not shown.", omittedGoals))
		}
	}
	out := strings.Join(lines, "\n")
	if len([]rune(out)) > maxTodoListContentRunesInPrompt {
		runes := []rune(out)
		out = string(runes[:maxTodoListContentRunesInPrompt]) + "\n… (task list truncated for prompt size)"
	}
	return out
}

func truncateTodoListForStructuredPayload(listResponse TodoListResponse) (truncatedUnresolved []MemoryWakeStateOpenItem, truncatedGoals []string, omittedTasks int, omittedGoals int) {
	truncatedUnresolved = listResponse.UnresolvedItems
	if len(truncatedUnresolved) > maxTodoListStructuredItemsInJSON {
		omittedTasks = len(truncatedUnresolved) - maxTodoListStructuredItemsInJSON
		truncatedUnresolved = append([]MemoryWakeStateOpenItem(nil), truncatedUnresolved[:maxTodoListStructuredItemsInJSON]...)
	}
	for itemIndex := range truncatedUnresolved {
		textRunes := []rune(strings.TrimSpace(truncatedUnresolved[itemIndex].Text))
		if len(textRunes) > maxTodoListSingleItemRunesInJSON {
			truncatedUnresolved[itemIndex].Text = string(textRunes[:maxTodoListSingleItemRunesInJSON]) + "…"
		}
		nextRunes := []rune(strings.TrimSpace(truncatedUnresolved[itemIndex].NextStep))
		if len(nextRunes) > maxTodoListSingleItemRunesInJSON {
			truncatedUnresolved[itemIndex].NextStep = string(nextRunes[:maxTodoListSingleItemRunesInJSON]) + "…"
		}
	}
	truncatedGoals = listResponse.ActiveGoals
	if len(truncatedGoals) > maxTodoListStructuredItemsInJSON {
		omittedGoals = len(truncatedGoals) - maxTodoListStructuredItemsInJSON
		truncatedGoals = append([]string(nil), truncatedGoals[:maxTodoListStructuredItemsInJSON]...)
	}
	for goalIndex, goalText := range truncatedGoals {
		goalRunes := []rune(strings.TrimSpace(goalText))
		if len(goalRunes) > maxTodoListSingleItemRunesInJSON {
			truncatedGoals[goalIndex] = string(goalRunes[:maxTodoListSingleItemRunesInJSON]) + "…"
		}
	}
	return truncatedUnresolved, truncatedGoals, omittedTasks, omittedGoals
}
