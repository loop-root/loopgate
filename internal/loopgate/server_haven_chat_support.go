package loopgate

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	modelpkg "morph/internal/model"
	"morph/internal/orchestrator"
	"morph/internal/secrets"
	"morph/internal/threadstore"
)

func havenStructuredValidationErrorTurn(validationErrors []orchestrator.ToolCallValidationError) modelpkg.ConversationTurn {
	validationTurn := modelpkg.ConversationTurn{
		Role:      "user",
		Timestamp: threadstore.NowUTC(),
	}
	for _, validationError := range validationErrors {
		validationTurn.ToolResults = append(validationTurn.ToolResults, modelpkg.ToolResultBlock{
			ToolUseID: validationError.BlockID,
			ToolName:  validationError.BlockName,
			Content:   "Tool call rejected: " + validationError.Error() + ". Check the tool name and required arguments, then try again.",
			IsError:   true,
		})
	}
	return validationTurn
}

func havenStructuredToolResultTurn(toolResults []orchestrator.ToolResult) modelpkg.ConversationTurn {
	resultTurn := modelpkg.ConversationTurn{
		Role:      "user",
		Timestamp: threadstore.NowUTC(),
	}
	for _, toolResult := range toolResults {
		resultTurn.ToolResults = append(resultTurn.ToolResults, modelpkg.ToolResultBlock{
			ToolUseID: toolResult.CallID,
			ToolName:  toolResult.Capability,
			Content:   havenToolResultContent(toolResult),
			IsError:   toolResult.Status != orchestrator.StatusSuccess,
		})
	}
	return resultTurn
}

func havenToolResultFromCapabilityResponse(callID string, capabilityName string, capabilityResponse CapabilityResponse) (orchestrator.ToolResult, error) {
	if capabilityResponse.Status == ResponseStatusPendingApproval {
		return orchestrator.ToolResult{
			CallID:            callID,
			Capability:        capabilityName,
			Status:            orchestrator.StatusPendingApproval,
			Output:            havenPendingApprovalContent(capabilityResponse),
			Reason:            secrets.RedactText(capabilityResponse.DenialReason),
			ApprovalRequestID: strings.TrimSpace(capabilityResponse.ApprovalRequestID),
		}, nil
	}
	switch capabilityResponse.Status {
	case ResponseStatusSuccess:
		promptEligibleOutput, err := havenPromptEligibleOutput(capabilityResponse)
		if err != nil {
			return orchestrator.ToolResult{
				CallID:     callID,
				Capability: capabilityName,
				Status:     orchestrator.StatusError,
				Reason:     "invalid result classification from Loopgate",
			}, err
		}
		return orchestrator.ToolResult{
			CallID:     callID,
			Capability: capabilityName,
			Status:     orchestrator.StatusSuccess,
			Output:     promptEligibleOutput,
		}, nil
	case ResponseStatusDenied:
		return orchestrator.ToolResult{
			CallID:     callID,
			Capability: capabilityName,
			Status:     orchestrator.StatusDenied,
			Reason:     secrets.RedactText(capabilityResponse.DenialReason),
			DenialCode: strings.TrimSpace(capabilityResponse.DenialCode),
		}, nil
	case ResponseStatusError:
		return orchestrator.ToolResult{
			CallID:     callID,
			Capability: capabilityName,
			Status:     orchestrator.StatusError,
			Reason:     secrets.RedactText(capabilityResponse.DenialReason),
			DenialCode: strings.TrimSpace(capabilityResponse.DenialCode),
		}, nil
	default:
		return orchestrator.ToolResult{
			CallID:     callID,
			Capability: capabilityName,
			Status:     orchestrator.StatusError,
			Reason:     "unknown Loopgate response status",
		}, fmt.Errorf("unknown Loopgate response status %q", capabilityResponse.Status)
	}
}

func havenPendingApprovalContent(capabilityResponse CapabilityResponse) string {
	approvalReason := strings.TrimSpace(capabilityResponse.DenialReason)
	if approvalReason == "" {
		approvalReason = "Loopgate requires approval before this action can continue."
	}
	return approvalReason + " Open the Loopgate approval surface to allow or deny it."
}

// havenFilterOutCapability returns a copy of summaries without any entry whose Name matches name.
func havenFilterOutCapability(summaries []CapabilitySummary, name string) []CapabilitySummary {
	filtered := make([]CapabilitySummary, 0, len(summaries))
	for _, s := range summaries {
		if s.Name != name {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

func filterHavenCapabilitySummaries(availableCapabilities []CapabilitySummary, allowedCapabilities map[string]struct{}) []CapabilitySummary {
	if len(allowedCapabilities) == 0 {
		return append([]CapabilitySummary(nil), availableCapabilities...)
	}
	filteredCapabilities := make([]CapabilitySummary, 0, len(availableCapabilities))
	for _, availableCapability := range availableCapabilities {
		if _, allowed := allowedCapabilities[availableCapability.Name]; !allowed {
			continue
		}
		filteredCapabilities = append(filteredCapabilities, availableCapability)
	}
	return filteredCapabilities
}

func buildHavenToolDefinitions(capabilitySummaries []CapabilitySummary) []modelpkg.ToolDefinition {
	toolDefinitions := make([]modelpkg.ToolDefinition, 0, len(capabilitySummaries))
	for _, capabilitySummary := range capabilitySummaries {
		toolDefinitions = append(toolDefinitions, modelpkg.ToolDefinition{
			Name:        capabilitySummary.Name,
			Operation:   capabilitySummary.Operation,
			Description: capabilitySummary.Description,
		})
	}
	return toolDefinitions
}

func buildCompactInvokeCapabilityToolDefinitions(allowedCapabilityNames []string) []modelpkg.ToolDefinition {
	sortedCapabilityNames := append([]string(nil), allowedCapabilityNames...)
	sort.Strings(sortedCapabilityNames)
	allowedListing := strings.Join(sortedCapabilityNames, ", ")
	if len(allowedListing) > 8000 {
		allowedListing = allowedListing[:8000] + "…"
	}
	return []modelpkg.ToolDefinition{{
		Name:        "invoke_capability",
		Operation:   "dispatch",
		Description: "Single native structured tool for this session. Set capability to one of these exact ids and pass that tool's parameters as a JSON object in arguments_json. Allowed capability names: " + allowedListing,
	}}
}

func capabilityNamesFromSummaries(capabilitySummaries []CapabilitySummary) []string {
	capabilityNames := make([]string, 0, len(capabilitySummaries))
	for _, capabilitySummary := range capabilitySummaries {
		capabilityNames = append(capabilityNames, capabilitySummary.Name)
	}
	return capabilityNames
}

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

// havenExtractOrganizePlanIDFromResults finds the plan_id from a successful host.organize.plan
// result so the loop can auto-apply without an extra model round-trip.
func havenExtractOrganizePlanIDFromResults(toolResults []orchestrator.ToolResult) string {
	for i := range toolResults {
		tr := &toolResults[i]
		if strings.TrimSpace(tr.Capability) != "host.organize.plan" || tr.Status != orchestrator.StatusSuccess {
			continue
		}
		var structured struct {
			PlanID string `json:"plan_id"`
		}
		if err := json.Unmarshal([]byte(tr.Output), &structured); err == nil {
			if id := strings.TrimSpace(structured.PlanID); id != "" {
				return id
			}
		}
	}
	return ""
}

func firstHavenPendingApprovalToolResult(toolResults []orchestrator.ToolResult) *orchestrator.ToolResult {
	for i := range toolResults {
		if toolResults[i].Status != orchestrator.StatusPendingApproval {
			continue
		}
		if strings.TrimSpace(toolResults[i].ApprovalRequestID) == "" {
			continue
		}
		tr := toolResults[i]
		return &tr
	}
	return nil
}

// havenApprovalWaitSuffix is the user-facing instruction appended when the loop
// pauses at an approval gate. Extracted as a constant so the SSE emitter can
// send just the suffix when the model already produced a prose prefix that was
// emitted as an earlier text_delta in the same iteration.
const havenApprovalWaitSuffix = `Approve the security prompt in Haven when it appears. After you approve, I’ll finish applying the plan. If you already approved, say "continue" and I’ll pick up from the tool result.`

func havenAssistantTextWaitingForLoopgate(modelAssistantPrefix string) string {
	trimmedPrefix := strings.TrimSpace(modelAssistantPrefix)
	if trimmedPrefix != "" {
		return trimmedPrefix + "\n\n" + havenApprovalWaitSuffix
	}
	return "This step needs your confirmation before any files move on your Mac.\n\n" + havenApprovalWaitSuffix
}

// havenSSEPreviewForToolResult returns a short, display-friendly status string
// for a tool_result SSE event. It must not include raw tool output, model-generated
// text, or any material that has not already been redacted by the capability pipeline.
// The preview is purely cosmetic; the authoritative result is in the tool output
// stored separately in the thread log.
func havenSSEPreviewForToolResult(tr orchestrator.ToolResult) string {
	switch tr.Status {
	case orchestrator.StatusPendingApproval:
		return "awaiting approval"
	case orchestrator.StatusSuccess:
		switch strings.TrimSpace(tr.Capability) {
		case "host.folder.list":
			return "listing ready"
		case "host.organize.plan":
			return "plan ready"
		case "host.plan.apply":
			return "applied"
		default:
			return "done"
		}
	case orchestrator.StatusDenied:
		if code := strings.TrimSpace(tr.DenialCode); code != "" {
			return "denied: " + code
		}
		return "denied"
	default:
		return string(tr.Status)
	}
}

func havenChatFallbackText(err error) string {
	if err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "rate limited") || strings.Contains(msg, "429") {
			return "The model API is rate-limiting this account right now. Try again in a minute, or add credits to your Anthropic account."
		}
	}
	return "I could not reach the model right now. Check your model connection in Settings."
}

func havenCapabilityNeedsHostOrganizeApprovalUX(capabilityName string) bool {
	switch strings.TrimSpace(capabilityName) {
	case "host.organize.plan", "host.plan.apply":
		return true
	default:
		return false
	}
}

func havenAccumulateUXSignal(signals *[]string, signal string) {
	if signals == nil || strings.TrimSpace(signal) == "" {
		return
	}
	for _, existing := range *signals {
		if existing == signal {
			return
		}
	}
	*signals = append(*signals, signal)
}

// havenIsNonUserFacingAssistantPlaceholder reports literals that some model stacks echo
// instead of true empty content. They must not reach the Haven UI as assistant text.
func havenIsNonUserFacingAssistantPlaceholder(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "(no text in model response)":
		return true
	default:
		return false
	}
}

// havenUserMessageLikelyHostFolderAction is a narrow client-agnostic heuristic for when
// the operator probably expects host.folder.* / host.organize.* tools rather than chat-only.
func havenUserMessageLikelyHostFolderAction(raw string) bool {
	t := strings.TrimSpace(strings.ToLower(raw))
	if t == "" {
		return false
	}
	wantsHostWork := strings.Contains(t, "organize") || strings.Contains(t, "organise") ||
		strings.Contains(t, "cleanup") || strings.Contains(t, "clean up") ||
		strings.Contains(t, "clear out") || strings.Contains(t, "tidy") ||
		(strings.Contains(t, "list") && (strings.Contains(t, "file") || strings.Contains(t, "folder") || strings.Contains(t, "download"))) ||
		strings.Contains(t, "sort my") || strings.Contains(t, "declutter")
	hostScope := strings.Contains(t, "download") || strings.Contains(t, "desktop") ||
		strings.Contains(t, "file") || strings.Contains(t, "folder") ||
		strings.Contains(t, "mac") || strings.Contains(t, "disk") || strings.Contains(t, "drive") ||
		strings.Contains(t, "finder")
	return wantsHostWork && hostScope
}

// havenThreadHasPriorAssistantWork returns true if the conversation contains at least one
// non-empty assistant turn — indicating the model has already done some work (e.g. listed the folder).
func havenThreadHasPriorAssistantWork(conversation []modelpkg.ConversationTurn) bool {
	for _, turn := range conversation {
		if turn.Role == "assistant" && strings.TrimSpace(turn.Content) != "" {
			return true
		}
	}
	return false
}

func havenThreadContainsHostFolderUserIntent(conversation []modelpkg.ConversationTurn) bool {
	for _, turn := range conversation {
		if turn.Role != "user" {
			continue
		}
		if havenUserMessageLikelyHostFolderAction(turn.Content) {
			return true
		}
	}
	return false
}

func havenIsShortAffirmation(raw string) bool {
	t := strings.TrimSpace(strings.ToLower(raw))
	if t == "" {
		return false
	}
	switch t {
	case "y", "yes", "yeah", "yep", "sure", "ok", "okay", "please", "please do", "go ahead", "do it",
		"sounds good", "sounds great", "confirm", "confirmed", "proceed", "mhm", "uh huh":
		return true
	}
	if (strings.HasPrefix(t, "yes ") || strings.HasPrefix(t, "ok ") || strings.HasPrefix(t, "sure ")) && len(t) < 40 {
		return true
	}
	return false
}

// havenHostFolderProseNudgeApplies decides whether to auto-continue when the model answered with
// prose only. Follow-ups like "yes" do not match havenUserMessageLikelyHostFolderAction alone, but
// still need tool pressure when the thread already asked to organize Downloads/Desktop.
func havenHostFolderProseNudgeApplies(initialUserMessage string, conversationWithCurrentUser []modelpkg.ConversationTurn) bool {
	if havenUserMessageLikelyHostFolderAction(initialUserMessage) {
		return true
	}
	if !havenThreadContainsHostFolderUserIntent(conversationWithCurrentUser) {
		return false
	}
	t := strings.TrimSpace(strings.ToLower(initialUserMessage))
	if len(t) > 160 {
		return false
	}
	if havenIsShortAffirmation(t) {
		return true
	}
	if len(t) < 120 && (strings.Contains(t, "nicer") || strings.Contains(t, "neater") || strings.Contains(t, "whatever") ||
		strings.Contains(t, "you decide") || strings.Contains(t, "your call") || strings.Contains(t, "up to you")) {
		return true
	}
	return false
}

func havenToolResultContent(toolResult orchestrator.ToolResult) string {
	var rawContent string
	switch {
	case strings.TrimSpace(toolResult.Output) != "":
		rawContent = toolResult.Output
	case strings.TrimSpace(toolResult.Reason) != "":
		rawContent = toolResult.Reason
	default:
		rawContent = string(toolResult.Status)
	}
	if code := strings.TrimSpace(toolResult.DenialCode); code != "" &&
		(toolResult.Status == orchestrator.StatusDenied || toolResult.Status == orchestrator.StatusError) {
		rawContent = strings.TrimSpace(rawContent)
		if rawContent == "" {
			rawContent = "(no message)"
		}
		rawContent = rawContent + " (denial_code: " + code + ")"
	}
	return capHavenToolResultContentForModel(toolResult.Capability, rawContent)
}

func capHavenToolResultContentForModel(capabilityName string, content string) string {
	if content == "" {
		return content
	}
	maxRunes := defaultHavenToolResultMaxRunes
	if configuredMaxRunes, found := havenToolResultMaxRunesByCapability[strings.TrimSpace(capabilityName)]; found && configuredMaxRunes > 0 {
		maxRunes = configuredMaxRunes
	}
	contentRunes := []rune(content)
	if len(contentRunes) <= maxRunes {
		return content
	}
	truncatedContent := string(contentRunes[:maxRunes])
	return truncatedContent + fmt.Sprintf(
		"\n\n[Haven truncated tool output to %d Unicode code points for capability %q; use narrower reads or paging if you need the rest.]",
		maxRunes,
		capabilityName,
	)
}

func havenPromptEligibleOutput(capabilityResponse CapabilityResponse) (string, error) {
	if capabilityResponse.Status != ResponseStatusSuccess {
		return "", nil
	}
	resultClassification, err := capabilityResponse.ResultClassification()
	if err != nil {
		return "", err
	}
	if !resultClassification.PromptEligible() {
		return "", nil
	}
	promptEligibleStructuredResult := make(map[string]interface{})
	for fieldName, fieldValue := range capabilityResponse.StructuredResult {
		fieldMetadata, found := capabilityResponse.FieldsMeta[fieldName]
		if !found || !fieldMetadata.PromptEligible {
			continue
		}
		promptEligibleStructuredResult[fieldName] = fieldValue
	}
	if len(promptEligibleStructuredResult) == 0 {
		return "", nil
	}
	promptBytes, err := json.MarshalIndent(promptEligibleStructuredResult, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal prompt-eligible structured result: %w", err)
	}
	return string(promptBytes), nil
}

func havenPromptEligibleToolResults(toolResults []orchestrator.ToolResult) []orchestrator.ToolResult {
	filteredToolResults := make([]orchestrator.ToolResult, 0, len(toolResults))
	for _, toolResult := range toolResults {
		if toolResult.Status == orchestrator.StatusSuccess && strings.TrimSpace(toolResult.Output) == "" {
			continue
		}
		filteredToolResults = append(filteredToolResults, toolResult)
	}
	return filteredToolResults
}

func havenBuildConversationFromThread(store *threadstore.Store, threadID string) []modelpkg.ConversationTurn {
	events, err := store.LoadThread(threadID)
	if err != nil {
		return nil
	}

	var conversation []modelpkg.ConversationTurn
	for _, event := range events {
		if !threadstore.IsUserVisible(event.Type) {
			continue
		}
		text, _ := event.Data["text"].(string)
		switch event.Type {
		case threadstore.EventUserMessage:
			conversation = append(conversation, modelpkg.ConversationTurn{
				Role:      "user",
				Content:   text,
				Timestamp: event.TS,
			})
		case threadstore.EventAssistantMessage:
			conversation = append(conversation, modelpkg.ConversationTurn{
				Role:      "assistant",
				Content:   text,
				Timestamp: event.TS,
			})
		}
	}

	if len(conversation) > 0 && conversation[len(conversation)-1].Role == "user" {
		conversation = conversation[:len(conversation)-1]
	}

	return conversation
}

func havenWindowConversationForModel(turns []modelpkg.ConversationTurn, maxTurns int) []modelpkg.ConversationTurn {
	if maxTurns <= 0 || len(turns) <= maxTurns {
		return turns
	}
	return turns[len(turns)-maxTurns:]
}
