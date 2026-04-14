package loopgate

import (
	"sort"
	"strings"
)

func buildResidentCapabilitySummary(capabilitySummaries []CapabilitySummary) string {
	displayNames := make([]string, 0, len(capabilitySummaries))
	seenDisplayNames := make(map[string]struct{}, len(capabilitySummaries))
	for _, capabilitySummary := range capabilitySummaries {
		descriptor, found := havenCapabilityCatalog[capabilitySummary.Name]
		displayName := capabilitySummary.Name
		if found && strings.TrimSpace(descriptor.DisplayName) != "" {
			displayName = descriptor.DisplayName
		}
		if _, alreadySeen := seenDisplayNames[displayName]; alreadySeen {
			continue
		}
		seenDisplayNames[displayName] = struct{}{}
		displayNames = append(displayNames, displayName)
	}
	sort.Strings(displayNames)
	return formatHavenHumanList(displayNames)
}

func buildResidentCapabilityFacts(capabilitySummaries []CapabilitySummary) []string {
	availableCapabilities := make(map[string]struct{}, len(capabilitySummaries))
	for _, capabilitySummary := range capabilitySummaries {
		availableCapabilities[capabilitySummary.Name] = struct{}{}
	}

	runtimeFacts := make([]string, 0, 12)
	runtimeFacts = append(runtimeFacts, buildHavenJournalCapabilityFacts(availableCapabilities)...)
	runtimeFacts = append(runtimeFacts, buildHavenOperatorContextFacts(availableCapabilities)...)
	runtimeFacts = append(runtimeFacts, buildHavenNotesCapabilityFacts(availableCapabilities)...)
	runtimeFacts = append(runtimeFacts, buildHavenMemoryCapabilityFacts(availableCapabilities)...)
	runtimeFacts = append(runtimeFacts, buildHavenHostFolderCapabilityFacts(availableCapabilities)...)
	runtimeFacts = append(runtimeFacts, buildHavenShellCapabilityFacts(availableCapabilities)...)
	return runtimeFacts
}

func buildHavenJournalCapabilityFacts(availableCapabilities map[string]struct{}) []string {
	if !hasAllHavenCapabilities(availableCapabilities, "journal.list", "journal.read", "journal.write") {
		return nil
	}
	return []string{
		"You have a Journal app. Use journal.list and journal.read when the user wants to review prior entries. Use journal.write only when they ask for journaling, reflection, or an explicit journal entry.",
	}
}

func buildHavenOperatorContextFacts(availableCapabilities map[string]struct{}) []string {
	if _, hasOperatorGuide := availableCapabilities["haven.operator_context"]; !hasOperatorGuide {
		return nil
	}
	return []string{
		"You have haven.operator_context: when the operator asks how Haven mounts work, TUI layout (sidebar off by default, Ctrl+B), shortcuts, drag-and-drop path paste, or harness troubleshooting, call it with no arguments. It returns Loopgate-maintained operator documentation — do not invent TTL, policy, or shortcut details from memory alone.",
	}
}

func buildHavenNotesCapabilityFacts(availableCapabilities map[string]struct{}) []string {
	if !hasAllHavenCapabilities(availableCapabilities, "notes.list", "notes.read", "notes.write") {
		return nil
	}
	return []string{
		"You have a Notes app for working memory. Use notes.write for plans, scratch work, or research notes that should persist inside Haven without becoming a journal entry.",
	}
}

func buildHavenMemoryCapabilityFacts(availableCapabilities map[string]struct{}) []string {
	if !hasAllHavenCapabilities(availableCapabilities, "memory.remember") {
		return nil
	}
	return []string{
		"memory.remember proposes durable facts: call it when the user asks to remember something or states explicit stable preferences, routines, profile or work details, or standing goals for next session. Prefer dotted keys (for example preference.coffee_order, routine.friday_gym, goal.current_sprint, work.focus_area). Loopgate decides what becomes durable memory — a failed call means policy or safety rejected the candidate. Never store secrets, API keys, passwords, or blobs of text.",
	}
}

func buildHavenHostFolderCapabilityFacts(availableCapabilities map[string]struct{}) []string {
	if !hasAllHavenCapabilities(availableCapabilities, "host.folder.list", "host.folder.read", "host.organize.plan", "host.plan.apply") {
		return nil
	}

	runtimeFacts := []string{
		"You have typed host-folder tools for paths the operator granted in Setup. Use host.folder.list and host.folder.read on the real folder, host.organize.plan to propose changes without writing, and host.plan.apply to execute the stored plan.",
		"Critical: host.organize.plan returns a plan_id and does not open Loopgate's approval UI. Only host.plan.apply can trigger execution. Higher-risk plans may open Loopgate approval; low-risk bounded plans may run immediately. Do not claim approval is pending unless the tool result actually says so.",
		"When the user asks to organize or tidy their files on the Mac, assume they mean a granted host folder (for example Downloads or Desktop if enabled): list -> organize.plan -> plan.apply. Do not claim files were reorganized until apply has succeeded.",
		"For those requests: call host.folder.list via invoke_capability in the same assistant turn — do not stop after only describing what you will do next. Act first (list/read), then explain results.",
		"Do not ask the user to type permission, confirmation, or yes/no in Messenger before calling host.folder.list or host.folder.read — chat consent is not authority. Call the tool; when policy requires it, Loopgate opens its own approval surface automatically. Do not tell the user to open Loopgate unless a tool result already indicates pending approval.",
		"Do not interview the user about sort order (by type vs date) before listing — call host.folder.list first, then propose a concrete plan from real filenames.",
		"Do not use shell_exec to list, inspect, or reorganize the user's granted Mac folders (Downloads, Desktop, Documents, shared). Those paths are exposed only through host.folder.list, host.organize.plan, and host.plan.apply; shell is not an equivalent substitute and will often fail policy or see the wrong filesystem view.",
		"invoke_capability for host.folder.list: capability host.folder.list; arguments_json object with folder_name (preset id or label: downloads, desktop, documents, shared — must match Setup grants) and optional path (relative subfolder, use \".\" for root).",
		"invoke_capability for host.organize.plan: arguments_json object must include folder_name and plan_json. plan_json is a JSON array of operations: {\"kind\":\"move\",\"from\":\"rel\",\"to\":\"rel\"} or {\"kind\":\"mkdir\",\"path\":\"rel\"} (paths relative to that folder). You may put plan_json as a real JSON array inside arguments_json, or as a string holding the same array — both work. Optional summary string.",
	}
	if _, hasDesktopOrganize := availableCapabilities["desktop.organize"]; hasDesktopOrganize {
		runtimeFacts = append(runtimeFacts, "You also have desktop.organize: it only rearranges Haven's on-screen desktop icon layout. It does not read or move files in the user's real macOS Desktop folder. For actual Desktop files on disk, use host.folder.list with folder_name desktop when that grant exists, not desktop.organize.")
	}
	return runtimeFacts
}

func buildHavenShellCapabilityFacts(availableCapabilities map[string]struct{}) []string {
	if !hasAllHavenCapabilities(availableCapabilities, "shell_exec") {
		return nil
	}
	return []string{
		"Use shell_exec only when a task genuinely needs a terminal (builds, package managers, git CLI, dev servers) and policy allows it — not for routine file listing or organizing when fs_* or host.folder.* applies.",
	}
}

func hasAllHavenCapabilities(availableCapabilities map[string]struct{}, requiredCapabilities ...string) bool {
	for _, requiredCapability := range requiredCapabilities {
		if _, found := availableCapabilities[requiredCapability]; !found {
			return false
		}
	}
	return true
}

func formatHavenHumanList(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + ", and " + items[len(items)-1]
	}
}
