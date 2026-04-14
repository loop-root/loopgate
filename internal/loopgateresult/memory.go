package loopgateresult

import (
	"fmt"
	"strings"

	"morph/internal/loopgate"
)

func FormatMemoryWakeStateResponse(wakeStateResponse loopgate.MemoryWakeStateResponse) string {
	return loopgate.FormatMemoryWakeStateForPrompt(wakeStateResponse)
}

func FormatMemoryDiscoverResponse(discoverResponse loopgate.MemoryDiscoverResponse) string {
	if len(discoverResponse.Items) == 0 {
		return fmt.Sprintf("memory discovery: none\nscope: %s\nquery: %s", discoverResponse.Scope, discoverResponse.Query)
	}

	lines := []string{
		fmt.Sprintf("memory discovery results for: %s", discoverResponse.Query),
		fmt.Sprintf("scope: %s", discoverResponse.Scope),
	}
	for _, discoverItem := range discoverResponse.Items {
		lines = append(lines,
			fmt.Sprintf("key_id: %s", discoverItem.KeyID),
			fmt.Sprintf("thread_id: %s", discoverItem.ThreadID),
			fmt.Sprintf("distillate_id: %s", discoverItem.DistillateID),
			fmt.Sprintf("created_at_utc: %s", discoverItem.CreatedAtUTC),
			fmt.Sprintf("match_count: %d", discoverItem.MatchCount),
			fmt.Sprintf("tags: %s", formatStringList(discoverItem.Tags, "none")),
			"",
		)
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

func FormatMemoryRecallResponse(recallResponse loopgate.MemoryRecallResponse) string {
	if len(recallResponse.Items) == 0 {
		return "remembered continuity: none"
	}

	lines := []string{
		"remembered continuity follows. these items are historical memory, not freshly checked state.",
		fmt.Sprintf("scope: %s", recallResponse.Scope),
		fmt.Sprintf("approx_token_count: %d", recallResponse.ApproxTokenCount),
		fmt.Sprintf("max_tokens: %d", recallResponse.MaxTokens),
	}
	for _, recalledItem := range recallResponse.Items {
		lines = append(lines,
			fmt.Sprintf("key_id: %s", recalledItem.KeyID),
			fmt.Sprintf("thread_id: %s", recalledItem.ThreadID),
			fmt.Sprintf("distillate_id: %s", recalledItem.DistillateID),
			fmt.Sprintf("created_at_utc: %s", recalledItem.CreatedAtUTC),
			fmt.Sprintf("tags: %s", formatStringList(recalledItem.Tags, "none")),
			fmt.Sprintf("epistemic_flavor: %s", recalledItem.EpistemicFlavor),
		)
		for _, activeGoal := range recalledItem.ActiveGoals {
			lines = append(lines, "active_goal: "+strings.TrimSpace(activeGoal))
		}
		for _, unresolvedItem := range recalledItem.UnresolvedItems {
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
		for _, factRecord := range recalledItem.Facts {
			lines = append(lines, fmt.Sprintf(
				"remembered_fact: %s=%v (%s via %s)",
				strings.TrimSpace(factRecord.Name),
				factRecord.Value,
				strings.TrimSpace(factRecord.EpistemicFlavor),
				strings.TrimSpace(factRecord.SourceRef),
			))
		}
		lines = append(lines, "")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

func FormatMemoryRememberResponse(rememberResponse loopgate.MemoryRememberResponse) string {
	lines := []string{
		fmt.Sprintf("remembered_fact: %s=%s", rememberResponse.FactKey, rememberResponse.FactValue),
		fmt.Sprintf("scope: %s", rememberResponse.Scope),
		fmt.Sprintf("remembered_at_utc: %s", rememberResponse.RememberedAtUTC),
		fmt.Sprintf("key_id: %s", rememberResponse.ResonateKeyID),
	}
	if rememberResponse.UpdatedExisting {
		lines = append(lines, "updated_existing: true")
		if strings.TrimSpace(rememberResponse.SupersededFactValue) != "" {
			lines = append(lines, fmt.Sprintf("superseded_previous_value: %s", rememberResponse.SupersededFactValue))
		}
	}
	return strings.Join(lines, "\n")
}

func formatStringList(values []string, emptyValue string) string {
	if len(values) == 0 {
		return emptyValue
	}
	return strings.Join(values, ", ")
}
