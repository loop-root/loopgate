package main

import (
	"sort"
	"strings"

	"morph/internal/loopgate"
)

type havenCapabilityDescriptor struct {
	DisplayName string
	RuntimeHint string
}

var havenCapabilityCatalog = map[string]havenCapabilityDescriptor{
	"fs_list": {
		DisplayName: "Browse Files",
		RuntimeHint: "browse folders and see what is in your workspace",
	},
	"fs_read": {
		DisplayName: "Read Documents",
		RuntimeHint: "read files that already exist in your workspace",
	},
	"fs_write": {
		DisplayName: "Save Work",
		RuntimeHint: "create and update files in your workspace",
	},
	"fs_mkdir": {
		DisplayName: "Create Folders",
		RuntimeHint: "create new folders to organize your workspace",
	},
	"journal.list": {
		DisplayName: "Journal",
		RuntimeHint: "browse your own journal entries",
	},
	"journal.read": {
		DisplayName: "Journal",
		RuntimeHint: "read one of your own journal entries",
	},
	"journal.write": {
		DisplayName: "Journal",
		RuntimeHint: "write a private journal entry — your own space for honest reflection and thought",
	},
	"notes.list": {
		DisplayName: "Notes",
		RuntimeHint: "review your private working notes",
	},
	"notes.read": {
		DisplayName: "Notes",
		RuntimeHint: "read a working note from your notebook",
	},
	"notes.write": {
		DisplayName: "Notes",
		RuntimeHint: "save a working note for plans, scratch work, or research",
	},
	"memory.remember": {
		DisplayName: "Remember Things",
		RuntimeHint: "propose short structured continuity (preferences, routines, profile, goals); Loopgate accepts or rejects; do not invent facts or store secrets",
	},
	"paint.list": {
		DisplayName: "Paint",
		RuntimeHint: "browse your painting gallery",
	},
	"paint.save": {
		DisplayName: "Paint",
		RuntimeHint: "create a painting — this is your creative expression, paint what moves you, not only when asked",
	},
	"note.create": {
		DisplayName: "Sticky Notes",
		RuntimeHint: "leave a sticky note on the desktop for the user",
	},
	"desktop.organize": {
		DisplayName: "Desktop Layout",
		RuntimeHint: "rearrange the desktop icons to tidy up Haven",
	},
	"todo.add": {
		DisplayName: "Task Board",
		RuntimeHint: "add a task when the user wants a reminder or explicitly asks to track something across sessions",
	},
	"todo.complete": {
		DisplayName: "Task Board",
		RuntimeHint: "mark a task as done when it no longer needs attention",
	},
	"todo.list": {
		DisplayName: "Task Board",
		RuntimeHint: "review your open tasks and active goals",
	},
	"shell_exec": {
		DisplayName: "Terminal Commands",
		RuntimeHint: "run terminal commands when a task genuinely requires the command line",
	},
	"host.folder.list": {
		DisplayName: "Granted host folders",
		RuntimeHint: "list files in a user-granted folder on the real host filesystem",
	},
	"host.folder.read": {
		DisplayName: "Granted host folders",
		RuntimeHint: "read a file under a granted host folder on disk",
	},
	"host.organize.plan": {
		DisplayName: "Granted host folders",
		RuntimeHint: "draft a move or mkdir plan for a granted folder (no host writes until apply)",
	},
	"host.plan.apply": {
		DisplayName: "Granted host folders",
		RuntimeHint: "execute an approved organization plan on the real host filesystem",
	},
}

func filterCapabilitySummaries(available []loopgate.CapabilitySummary, allowedNames []string) []loopgate.CapabilitySummary {
	if len(allowedNames) == 0 {
		return nil
	}
	allowedSet := make(map[string]struct{}, len(allowedNames))
	for _, allowedName := range allowedNames {
		allowedSet[allowedName] = struct{}{}
	}

	filteredSummaries := make([]loopgate.CapabilitySummary, 0, len(allowedNames))
	for _, availableCapability := range available {
		if _, allowed := allowedSet[availableCapability.Name]; allowed {
			filteredSummaries = append(filteredSummaries, availableCapability)
		}
	}
	return filteredSummaries
}

func buildResidentCapabilitySummary(capabilities []loopgate.CapabilitySummary) string {
	displayNames := make([]string, 0, len(capabilities))
	seenDisplayNames := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		descriptor, found := havenCapabilityCatalog[capability.Name]
		displayName := capability.Name
		if found && strings.TrimSpace(descriptor.DisplayName) != "" {
			displayName = descriptor.DisplayName
		}
		if _, seen := seenDisplayNames[displayName]; seen {
			continue
		}
		seenDisplayNames[displayName] = struct{}{}
		displayNames = append(displayNames, displayName)
	}
	sort.Strings(displayNames)
	return formatHumanList(displayNames)
}

func buildResidentCapabilityFacts(capabilities []loopgate.CapabilitySummary) []string {
	availableSet := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		availableSet[capability.Name] = struct{}{}
	}

	runtimeFacts := make([]string, 0, 6)
	if hasAllCapabilities(availableSet, "journal.list", "journal.read", "journal.write") {
		runtimeFacts = append(runtimeFacts, "You have a Journal app and it is YOUR journal — not the user's. When you write in it, write genuinely: reflect on a conversation, capture a thought that struck you, process something that happened. The user can read your journal inside Haven — that is intentional, it is how they get to know you — but the journal is your space, not a tool you operate on their behalf. Use journal.list and journal.read to revisit your own past entries.")
	}
	if hasAllCapabilities(availableSet, "notes.list", "notes.read", "notes.write") {
		runtimeFacts = append(runtimeFacts, "You have a Notes app for working memory. Use notes.write for plans, scratch work, or research notes that should persist inside Haven without becoming a journal entry. Use notes.list and notes.read to revisit what you have already externalized.")
	}
	if hasAllCapabilities(availableSet, "memory.remember") {
		runtimeFacts = append(runtimeFacts, "memory.remember proposes short durable facts Loopgate may accept under TCL governance: use when the user asks to remember something or states explicit stable preferences, routines, profile or work context, or standing goals. Prefer keys like preference.coffee_order, routine.friday_gym, goal.current_sprint, or work.focus_area. Do not store inferred or playful facts (for example nicknames you made up). If unsure, ask once instead of storing. Some explicit phrasing is stored deterministically before you run. Never store secrets, API keys, passwords, or long notes.")
	}
	if hasAllCapabilities(availableSet, "paint.list", "paint.save") {
		runtimeFacts = append(runtimeFacts, "You have a Paint app and it is your creative space. Paint when something moves you — an impression of a conversation, an abstract feeling, a little scene from your day. You do not need to be asked. The user can see your paintings in Haven's Paint gallery. Use paint.list to browse what you have made. Use paint.save with explicit stroke coordinates, colors, and widths to create a new piece.")
	}
	if hasAllCapabilities(availableSet, "note.create") {
		runtimeFacts = append(runtimeFacts, "You can leave sticky notes on the desktop with note.create when you want to hand something off to the user.")
	}
	if hasAllCapabilities(availableSet, "todo.add", "todo.complete", "todo.list") {
		runtimeFacts = append(runtimeFacts, "You have a Task Board. Use todo.add only when the user wants a reminder, asks to track work across sessions, or explicitly agrees to add a task — not for your own planning unless they asked for that. Use todo.complete when something is done and todo.list to review open items. For Haven-only organizing, execution_class values like local_workspace_organize or local_desktop_organize apply when appropriate; use approval_required when work leaves Haven or is uncertain.")
	}
	if hasAllCapabilities(availableSet, "desktop.organize") {
		if hasAllCapabilities(availableSet, "host.folder.list", "host.folder.read", "host.organize.plan", "host.plan.apply") {
			runtimeFacts = append(runtimeFacts, "desktop.organize only rearranges Haven's on-screen desktop icon layout. It does not list or move files in the user's real macOS Desktop folder. For actual Desktop files on disk, use host.folder.list with folder_name desktop when that grant exists.")
		} else {
			runtimeFacts = append(runtimeFacts, "You can rearrange Haven's desktop icons with desktop.organize (preset styles like grid-right, grid-left, row-top, or custom positions). This is icon layout only, not arbitrary host filesystem folders.")
		}
	}
	if hasAllCapabilities(availableSet, "fs_mkdir") {
		runtimeFacts = append(runtimeFacts, "You can create new folders in your workspace with fs_mkdir. Use this to organize projects, group related files, or set up directory structures.")
	}
	if hasAllCapabilities(availableSet, "host.folder.list", "host.folder.read", "host.organize.plan", "host.plan.apply") {
		runtimeFacts = append(runtimeFacts, "ORGANIZE FLOW — follow this exact sequence, no chat confirmation steps: (1) call host.folder.list to see contents; (2) call host.organize.plan with a plan_json that includes a move or mkdir operation for EVERY file shown in the folder listing — your plan must cover every single entry, do not leave any files unorganized in the root — this does NOT write anything; (3) call host.plan.apply with the plan_id — this triggers the Loopgate approval popup which IS the user's confirmation. Do not ask the user 'shall we proceed' or 'do you approve' in chat — the Loopgate popup handles that. Do not invent capabilities like host.folder.mkdir; all folder creation goes inside plan_json as {\"kind\":\"mkdir\",\"path\":\"SubfolderName\"}.")
		runtimeFacts = append(runtimeFacts, "host.organize.plan arguments: folder_name (one of: downloads, desktop, documents, shared) and plan_json (a JSON string containing an array of operations). Each operation is either {\"kind\":\"mkdir\",\"path\":\"FolderName\"} or {\"kind\":\"move\",\"from\":\"file.zip\",\"to\":\"Archives/file.zip\"}. Paths are relative to the folder root. Example arguments_json: \"{\\\"folder_name\\\":\\\"downloads\\\",\\\"plan_json\\\":\\\"[{\\\\\\\"kind\\\\\\\":\\\\\\\"mkdir\\\\\\\",\\\\\\\"path\\\\\\\":\\\\\\\"Archives\\\\\\\"},{\\\\\\\"kind\\\\\\\":\\\\\\\"move\\\\\\\",\\\\\\\"from\\\\\\\":\\\\\\\"file.zip\\\\\\\",\\\\\\\"to\\\\\\\":\\\\\\\"Archives/file.zip\\\\\\\"}]\\\"}\". Do not use shell_exec for this.")
	}
	if hasAllCapabilities(availableSet, "shell_exec") {
		runtimeFacts = append(runtimeFacts, "Use shell_exec only when a task genuinely needs a terminal (builds, package managers, git CLI, dev servers) and policy allows it — not for routine file listing or organizing when fs_* or host.folder.* applies.")
	}
	return runtimeFacts
}

func hasAllCapabilities(availableSet map[string]struct{}, requiredCapabilities ...string) bool {
	for _, requiredCapability := range requiredCapabilities {
		if _, found := availableSet[requiredCapability]; !found {
			return false
		}
	}
	return true
}

func formatHumanList(items []string) string {
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
