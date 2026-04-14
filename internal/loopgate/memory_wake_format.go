package loopgate

import (
	"fmt"
	"strings"
)

// FormatMemoryWakeStateForPrompt renders bounded historical continuity for
// prompt/context injection. The output is informational only and must not be
// treated as fresh verification or authority.
func FormatMemoryWakeStateForPrompt(wakeStateResponse MemoryWakeStateResponse) string {
	if strings.TrimSpace(wakeStateResponse.ID) == "" &&
		len(wakeStateResponse.ActiveGoals) == 0 &&
		len(wakeStateResponse.UnresolvedItems) == 0 &&
		len(wakeStateResponse.RecentFacts) == 0 &&
		len(wakeStateResponse.ResonateKeys) == 0 {
		return ""
	}

	lines := []string{
		"remembered continuity follows. this is historical continuity, not fresh verification.",
		fmt.Sprintf("scope: %s", wakeStateResponse.Scope),
	}
	for _, activeGoal := range wakeStateResponse.ActiveGoals {
		lines = append(lines, "active_goal: "+strings.TrimSpace(activeGoal))
	}
	for _, unresolvedItem := range wakeStateResponse.UnresolvedItems {
		itemLine := fmt.Sprintf("unresolved_item: %s %s", strings.TrimSpace(unresolvedItem.ID), strings.TrimSpace(unresolvedItem.Text))
		if strings.TrimSpace(unresolvedItem.TaskKind) != "" {
			itemLine += fmt.Sprintf(" [kind=%s]", strings.TrimSpace(unresolvedItem.TaskKind))
		}
		if strings.TrimSpace(unresolvedItem.NextStep) != "" {
			itemLine += fmt.Sprintf(" [next=%s]", strings.TrimSpace(unresolvedItem.NextStep))
		}
		if strings.TrimSpace(unresolvedItem.ScheduledForUTC) != "" {
			itemLine += fmt.Sprintf(" [scheduled_for_utc=%s]", strings.TrimSpace(unresolvedItem.ScheduledForUTC))
		}
		lines = append(lines, itemLine)
	}
	for _, recentFact := range wakeStateResponse.RecentFacts {
		lines = append(lines, fmt.Sprintf(
			"remembered_fact: %s=%v (%s via %s)",
			strings.TrimSpace(recentFact.Name),
			recentFact.Value,
			strings.TrimSpace(recentFact.EpistemicFlavor),
			strings.TrimSpace(recentFact.SourceRef),
		))
	}
	if len(wakeStateResponse.ResonateKeys) > 0 {
		lines = append(lines, "resonate_keys: "+strings.Join(wakeStateResponse.ResonateKeys, ", "))
	}
	return strings.Join(lines, "\n")
}
