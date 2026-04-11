package loopgate

import (
	"fmt"
	"strings"
)

func (server *Server) havenWakeStateSummaryText(tenantID string) (string, error) {
	wakeState, err := server.loadMemoryWakeState(tenantID)
	if err != nil {
		return "", err
	}
	if len(wakeState.RecentFacts) == 0 && len(wakeState.ActiveGoals) == 0 && len(wakeState.UnresolvedItems) == 0 {
		return "", nil
	}

	var summaryBuilder strings.Builder

	var factLines []string
	for _, recentFact := range wakeState.RecentFacts {
		if len(factLines) >= 12 {
			break
		}
		factLines = append(factLines, fmt.Sprintf("  %s: %v", recentFact.Name, recentFact.Value))
	}
	if len(factLines) > 0 {
		summaryBuilder.WriteString("REMEMBERED CONTINUITY (authoritative):\n")
		summaryBuilder.WriteString(strings.Join(factLines, "\n"))
	} else {
		summaryBuilder.WriteString("Wake-state is loaded with continuity metadata.")
	}

	if len(wakeState.ActiveGoals) > 0 {
		summaryBuilder.WriteString("\n\nACTIVE GOALS:\n")
		for _, activeGoal := range wakeState.ActiveGoals {
			summaryBuilder.WriteString(fmt.Sprintf("  - %s\n", activeGoal))
		}
	}

	if len(wakeState.UnresolvedItems) > 0 {
		summaryBuilder.WriteString("\n\nOPEN TASKS (task board — use todo.list to get IDs, todo.complete to close):\n")
		for _, unresolvedItem := range wakeState.UnresolvedItems {
			line := fmt.Sprintf("  [%s] %s", unresolvedItem.ID, unresolvedItem.Text)
			if unresolvedItem.ScheduledForUTC != "" {
				line += fmt.Sprintf(" (due: %s)", unresolvedItem.ScheduledForUTC)
			}
			if unresolvedItem.NextStep != "" {
				line += fmt.Sprintf(" — next: %s", unresolvedItem.NextStep)
			}
			summaryBuilder.WriteString(line + "\n")
		}
	}

	return summaryBuilder.String(), nil
}
