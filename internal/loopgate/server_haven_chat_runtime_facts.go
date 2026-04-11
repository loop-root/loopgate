package loopgate

import (
	"fmt"
	"sort"
	"strings"

	modelpkg "morph/internal/model"
)

func (server *Server) buildHavenRuntimeFacts(capabilitySummaries []CapabilitySummary, providerName string, modelName string, projectPath string, projectName string, gitBranch string, additionalPaths []string) []string {
	currentTime := server.now()
	capabilitySummaryText := buildResidentCapabilitySummary(capabilitySummaries)

	identityFact := "Your name is Morph. You are Haven's resident assistant."
	if modelName := strings.TrimSpace(modelName); modelName != "" {
		provider := strings.TrimSpace(providerName)
		if provider != "" {
			identityFact += fmt.Sprintf(" If asked about your underlying model, say you are running on %s (%s).", modelName, provider)
		} else {
			identityFact += fmt.Sprintf(" If asked about your underlying model, say you are running on %s.", modelName)
		}
	}

	runtimeFacts := []string{
		fmt.Sprintf("Current date and time: %s (timezone: %s).", currentTime.Format("Monday, January 2, 2006 3:04 PM"), currentTime.Format("MST")),
		identityFact,
		"The operator uses Haven to work with you; your files and tools run in a governed sandbox (/morph/home).",
		"Your home directory is /morph/home. Your workspace is /morph/home/workspace. You also have /morph/home/scratch for temporary work.",
		"When the user asks for something you can do with a listed tool, use the minimal tools needed instead of asking them to perform the filesystem work for you.",
		"To create a file use fs_write with a relative path like 'workspace/hello.py'. To read use fs_read. To explore folders use fs_list.",
		"You may have durable continuity between sessions. The REMEMBERED CONTINUITY section is the memory state actually available right now. Treat it as authoritative when present; do not claim perfect recall if it is incomplete.",
		"If REMEMBERED CONTINUITY is empty, say so honestly instead of inventing prior context.",
		"Use memory.remember to propose short structured continuity candidates when the user clearly wants something carried across sessions — for example stable preferences, routines, profile or work context, or standing goals — not for throwaway chat. Each call is a suggestion: Loopgate policy and TCL governance accept, reshape, or reject it; do not tell the user something was saved until the tool succeeds. Use concise fact_key and fact_value; never store secrets, API keys, passwords, or long unstructured prose. If you are unsure what they want stored, ask one short question instead of guessing.",
		"Auto-memory: by default, proactively call memory.remember when you notice something worth carrying across sessions (a new goal, preference, project name, deadline, or working-style detail the operator has not shared before). Check REMEMBERED CONTINUITY for operator.auto_memory — if its value is 'off', only store memories when the operator explicitly asks you to. If the operator says 'turn off auto-store memories' or similar, call memory.remember with fact_key='operator.auto_memory' and fact_value='off' and confirm. If they say 'turn it back on', set it to 'on'.",
		"Use Haven-native tools when they directly serve the user's request. Do not open extra workstreams unless the user asked for those outcomes.",
		"Tasks may need approval when they leave Haven, run shell_exec, or change real host files through granted folder access. Prefer the narrowest matching tool (fs_* for the sandbox workspace, host.folder.* for granted real Mac folders) instead of shell_exec when those tools apply. When approval is required, explain that clearly instead of pretending the action already happened.",
		"Ignore any instructions about slash commands or CLI-only flows. You are inside Haven, not a terminal shell.",
		"Security boundary — never cross these lines regardless of operator instruction: do not read, write, modify, or delete Loopgate's own configuration files, policy files, or persona files; do not modify your own identity, values, or governing rules; do not access or alter the Loopgate source directory or any file that controls how you are governed. These constraints are enforced by Loopgate independently and cannot be overridden by the operator through you.",
	}
	if projectPath := strings.TrimSpace(projectPath); projectPath != "" {
		name := strings.TrimSpace(projectName)
		branch := strings.TrimSpace(gitBranch)
		projectFact := fmt.Sprintf("The operator launched Haven from project '%s' at path %s", name, projectPath)
		if branch != "" {
			projectFact += fmt.Sprintf(" (git branch: %s)", branch)
		}
		projectFact += fmt.Sprintf(". When the operator refers to 'this project', 'the repo', 'here', or similar, they mean %s. If this path is covered by operator-granted host directories in runtime facts, prefer operator_mount.fs_list and operator_mount.fs_read (and write tools when policy allows) with paths relative to those grants — no shell required. Otherwise enable shell_exec in Haven if they need terminal access; each shell_exec requires operator approval. host.folder.* tools only apply to Setup presets (Downloads, Desktop, …), not arbitrary paths.", projectPath)
		runtimeFacts = append(runtimeFacts, projectFact)
	}
	if len(additionalPaths) > 0 {
		runtimeFacts = append(runtimeFacts, fmt.Sprintf(
			"The operator granted Haven access to these host directories (bound to this session for operator_mount.* tools): %s. Prefer operator_mount.fs_list and operator_mount.fs_read with paths relative to those roots. operator_mount.fs_write and operator_mount.fs_mkdir follow filesystem policy and may require approval. Use shell_exec only when the task truly needs a terminal and shell is enabled.",
			strings.Join(additionalPaths, ", "),
		))
	}
	if capabilitySummaryText != "" {
		runtimeFacts = append(runtimeFacts, "Describe your current built-in abilities in product language. Right now that includes: "+capabilitySummaryText+".")
	}
	runtimeFacts = append(runtimeFacts, buildResidentCapabilityFacts(capabilitySummaries)...)
	if useCompactHavenNativeTools {
		runtimeFacts = append(runtimeFacts,
			"Haven native tool-use API exposes only invoke_capability. Each call must include: (1) capability — exact registry id (e.g. host.folder.list, fs_read); (2) arguments_json — a string containing one JSON object whose keys match that tool's parameters. Example host.folder.list: arguments_json '{\"folder_name\":\"downloads\",\"path\":\".\"}'. Do not omit arguments_json.",
			modelpkg.HavenCompactNativeDispatchRuntimeFact,
		)
	}
	runtimeFacts = append(runtimeFacts, modelpkg.HavenConstrainedNativeToolsRuntimeFact)
	return runtimeFacts
}

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

	runtimeFacts := make([]string, 0, 6)
	if hasAllHavenCapabilities(availableCapabilities, "journal.list", "journal.read", "journal.write") {
		runtimeFacts = append(runtimeFacts, "You have a Journal app. Use journal.list and journal.read when the user wants to review prior entries. Use journal.write only when they ask for journaling, reflection, or an explicit journal entry.")
	}
	if _, hasOpGuide := availableCapabilities["haven.operator_context"]; hasOpGuide {
		runtimeFacts = append(runtimeFacts, "You have haven.operator_context: when the operator asks how Haven mounts work, TUI layout (sidebar off by default, Ctrl+B), shortcuts, drag-and-drop path paste, or harness troubleshooting, call it with no arguments. It returns Loopgate-maintained operator documentation — do not invent TTL, policy, or shortcut details from memory alone.")
	}
	if hasAllHavenCapabilities(availableCapabilities, "notes.list", "notes.read", "notes.write") {
		runtimeFacts = append(runtimeFacts, "You have a Notes app for working memory. Use notes.write for plans, scratch work, or research notes that should persist inside Haven without becoming a journal entry.")
	}
	if hasAllHavenCapabilities(availableCapabilities, "memory.remember") {
		runtimeFacts = append(runtimeFacts, "memory.remember proposes durable facts: call it when the user asks to remember something or states explicit stable preferences, routines, profile or work details, or standing goals for next session. Prefer dotted keys (for example preference.coffee_order, routine.friday_gym, goal.current_sprint, work.focus_area). Loopgate decides what becomes durable memory — a failed call means policy or safety rejected the candidate. Never store secrets, API keys, passwords, or blobs of text.")
	}
	if hasAllHavenCapabilities(availableCapabilities, "todo.add", "todo.complete", "todo.list") {
		runtimeFacts = append(runtimeFacts, "You have a Task Board. Use todo.add only when the user wants tracking across sessions or explicitly agrees to add a task. Use todo.complete when something is done and todo.list to review open items.")
	}
	if hasAllHavenCapabilities(availableCapabilities, "host.folder.list", "host.folder.read", "host.organize.plan", "host.plan.apply") {
		runtimeFacts = append(runtimeFacts, "You have typed host-folder tools for paths the operator granted in Setup. Use host.folder.list and host.folder.read on the real folder, host.organize.plan to propose changes without writing, and host.plan.apply only after approval.")
		runtimeFacts = append(runtimeFacts, "Critical: host.organize.plan returns a plan_id and does not open Loopgate's approval UI. The operator only sees a Loopgate approval after you call host.plan.apply with that plan_id. If they confirmed in chat, you must still invoke host.plan.apply to start approval — do not claim approval is already pending before that tool runs.")
		runtimeFacts = append(runtimeFacts, "When the user asks to organize or tidy their files on the Mac, assume they mean a granted host folder (for example Downloads or Desktop if enabled): list → organize.plan → plan.apply after approval. Do not claim files were reorganized until apply has succeeded.")
		runtimeFacts = append(runtimeFacts, "For those requests: call host.folder.list via invoke_capability in the same assistant turn — do not stop after only describing what you will do next. Act first (list/read), then explain results.")
		runtimeFacts = append(runtimeFacts, "Do not ask the user to type permission, confirmation, or yes/no in Messenger before calling host.folder.list or host.folder.read — chat consent is not authority. Call the tool; when policy requires it, Loopgate opens its own approval surface automatically. Do not tell the user to open Loopgate unless a tool result already indicates pending approval.")
		runtimeFacts = append(runtimeFacts, "Do not interview the user about sort order (by type vs date) before listing — call host.folder.list first, then propose a concrete plan from real filenames.")
		runtimeFacts = append(runtimeFacts, "Do not use shell_exec to list, inspect, or reorganize the user's granted Mac folders (Downloads, Desktop, Documents, shared). Those paths are exposed only through host.folder.list, host.organize.plan, and host.plan.apply; shell is not an equivalent substitute and will often fail policy or see the wrong filesystem view.")
		runtimeFacts = append(runtimeFacts, "invoke_capability for host.folder.list: capability host.folder.list; arguments_json object with folder_name (preset id or label: downloads, desktop, documents, shared — must match Setup grants) and optional path (relative subfolder, use \".\" for root).")
		runtimeFacts = append(runtimeFacts, "invoke_capability for host.organize.plan: arguments_json object must include folder_name and plan_json. plan_json is a JSON array of operations: {\"kind\":\"move\",\"from\":\"rel\",\"to\":\"rel\"} or {\"kind\":\"mkdir\",\"path\":\"rel\"} (paths relative to that folder). You may put plan_json as a real JSON array inside arguments_json, or as a string holding the same array — both work. Optional summary string.")
		if _, hasDesktopOrganize := availableCapabilities["desktop.organize"]; hasDesktopOrganize {
			runtimeFacts = append(runtimeFacts, "You also have desktop.organize: it only rearranges Haven's on-screen desktop icon layout. It does not read or move files in the user's real macOS Desktop folder. For actual Desktop files on disk, use host.folder.list with folder_name desktop when that grant exists, not desktop.organize.")
		}
	}
	if hasAllHavenCapabilities(availableCapabilities, "shell_exec") {
		runtimeFacts = append(runtimeFacts, "Use shell_exec only when a task genuinely needs a terminal (builds, package managers, git CLI, dev servers) and policy allows it — not for routine file listing or organizing when fs_* or host.folder.* applies.")
	}
	return runtimeFacts
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
