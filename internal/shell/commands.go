package shell

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"morph/internal/loopgate"
	"morph/internal/loopgateresult"
	"morph/internal/memory"
	modelruntime "morph/internal/modelruntime"
	"morph/internal/safety"
	"morph/internal/sandbox"
	setuppkg "morph/internal/setup"
	"morph/internal/ui"

	"github.com/chzyer/readline"
)

// ToolLogger is used to log tool/policy events to the ledger (from main.go).
type ToolLogger func(eventType string, data map[string]interface{})

type CommandAuditEvent struct {
	Type string
	Data map[string]interface{}
}

type CommandResult struct {
	Output               string
	Handled              bool
	ToolEventSeen        bool
	RuntimeConfigChanged bool
	UpdatedRuntimeConfig modelruntime.Config
	RequiredAuditEvents  []CommandAuditEvent
}

// HandleCommand parses and executes Loopgate slash-commands.
func HandleCommand(commandContext CommandContext, input string, rl *readline.Instance, logTool ToolLogger) CommandResult {
	fields := strings.Fields(strings.TrimSpace(input))
	if len(fields) == 0 {
		return CommandResult{Handled: true}
	}

	cmd := strings.ToLower(fields[0])
	toolEventSeen := false

	wrapLog := func(eventType string, data map[string]interface{}) {
		toolEventSeen = true
		if logTool != nil {
			logTool(eventType, data)
		}
	}

	// Check for help flags on any command: /goal help, /todo --help, etc.
	if len(fields) >= 2 && isHelpRequest(strings.ToLower(fields[1])) {
		if manPage, found := LookupManPage(cmd); found {
			return CommandResult{Output: manPage, Handled: true, ToolEventSeen: toolEventSeen}
		}
	}

	switch cmd {
	case "/help":
		return CommandResult{Output: buildHelpText(), Handled: true, ToolEventSeen: toolEventSeen}

	case "/man":
		if len(fields) < 2 {
			return CommandResult{Output: "Usage: /man <command>  (e.g. /man goal)", Handled: true, ToolEventSeen: toolEventSeen}
		}
		target := strings.ToLower(fields[1])
		if !strings.HasPrefix(target, "/") {
			target = "/" + target
		}
		if manPage, found := LookupManPage(target); found {
			return CommandResult{Output: manPage, Handled: true, ToolEventSeen: toolEventSeen}
		}
		return CommandResult{Output: "No man page for " + fields[1] + ". Try /help for a command list.", Handled: true, ToolEventSeen: toolEventSeen}

	case "/pwd":
		return CommandResult{Output: commandContext.RepoRoot, Handled: true, ToolEventSeen: toolEventSeen}

	case "/agent":
		return CommandResult{Output: summarizeAgent(commandContext), Handled: true, ToolEventSeen: toolEventSeen}

	case "/persona":
		return CommandResult{Output: summarizePersona(commandContext.Persona), Handled: true, ToolEventSeen: toolEventSeen}

	case "/settings":
		return CommandResult{Output: summarizeSettings(commandContext), Handled: true, ToolEventSeen: toolEventSeen}

	case "/network":
		return CommandResult{Output: summarizeNetwork(commandContext), Handled: true, ToolEventSeen: toolEventSeen}

	case "/connections":
		if len(fields) == 1 {
			return CommandResult{Output: summarizeConnections(commandContext), Handled: true, ToolEventSeen: toolEventSeen}
		}
		switch strings.ToLower(fields[1]) {
		case "validate":
			if len(fields) < 4 {
				return CommandResult{Output: "Usage: /connections validate <provider> <subject>", Handled: true, ToolEventSeen: toolEventSeen}
			}
			connectionStatus, err := commandContext.LoopgateClient.ValidateConnection(context.Background(), fields[2], fields[3])
			if err != nil {
				return CommandResult{Output: "Error: " + err.Error(), Handled: true, ToolEventSeen: true}
			}
			return CommandResult{
				Output:        fmt.Sprintf("validated %s/%s: grant_type=%s status=%s secret_ref=%s", connectionStatus.Provider, defaultSummaryValue(connectionStatus.Subject, "default"), connectionStatus.GrantType, connectionStatus.Status, connectionStatus.SecureStoreRefID),
				Handled:       true,
				ToolEventSeen: true,
			}
		case "pkce-start":
			if len(fields) < 4 {
				return CommandResult{Output: "Usage: /connections pkce-start <provider> <subject>", Handled: true, ToolEventSeen: toolEventSeen}
			}
			pkceStartResponse, err := commandContext.LoopgateClient.StartPKCEConnection(context.Background(), loopgate.PKCEStartRequest{
				Provider: fields[2],
				Subject:  fields[3],
			})
			if err != nil {
				return CommandResult{Output: "Error: " + err.Error(), Handled: true, ToolEventSeen: true}
			}
			return CommandResult{
				Output: strings.Join([]string{
					fmt.Sprintf("pkce start: %s/%s", pkceStartResponse.Provider, defaultSummaryValue(pkceStartResponse.Subject, "default")),
					fmt.Sprintf("expires_at_utc: %s", pkceStartResponse.ExpiresAtUTC),
					fmt.Sprintf("state: %s", pkceStartResponse.State),
					fmt.Sprintf("authorization_url: %s", pkceStartResponse.AuthorizationURL),
				}, "\n"),
				Handled:       true,
				ToolEventSeen: true,
			}
		case "pkce-complete":
			if len(fields) < 6 {
				return CommandResult{Output: "Usage: /connections pkce-complete <provider> <subject> <state> <code>", Handled: true, ToolEventSeen: toolEventSeen}
			}
			connectionStatus, err := commandContext.LoopgateClient.CompletePKCEConnection(context.Background(), loopgate.PKCECompleteRequest{
				Provider: fields[2],
				Subject:  fields[3],
				State:    fields[4],
				Code:     fields[5],
			})
			if err != nil {
				return CommandResult{Output: "Error: " + err.Error(), Handled: true, ToolEventSeen: true}
			}
			return CommandResult{
				Output:        fmt.Sprintf("pkce completed for %s/%s: status=%s secret_ref=%s", connectionStatus.Provider, defaultSummaryValue(connectionStatus.Subject, "default"), connectionStatus.Status, connectionStatus.SecureStoreRefID),
				Handled:       true,
				ToolEventSeen: true,
			}
		default:
			return CommandResult{Output: "Usage: /connections [validate|pkce-start|pkce-complete]", Handled: true, ToolEventSeen: toolEventSeen}
		}

	case "/site":
		if len(fields) < 3 {
			return CommandResult{Output: "Usage: /site [inspect|trust-draft] <url>", Handled: true, ToolEventSeen: toolEventSeen}
		}
		siteURL := fields[2]
		switch strings.ToLower(fields[1]) {
		case "inspect":
			inspectionResponse, err := commandContext.LoopgateClient.InspectSite(context.Background(), loopgate.SiteInspectionRequest{
				URL: siteURL,
			})
			if err != nil {
				return CommandResult{Output: formatSiteCommandError(siteURL, err), Handled: true, ToolEventSeen: true}
			}
			return CommandResult{
				Output:        formatSiteInspectionResponse(inspectionResponse),
				Handled:       true,
				ToolEventSeen: true,
			}
		case "trust-draft":
			inspectionResponse, err := commandContext.LoopgateClient.InspectSite(context.Background(), loopgate.SiteInspectionRequest{
				URL: siteURL,
			})
			if err != nil {
				return CommandResult{Output: formatSiteCommandError(siteURL, err), Handled: true, ToolEventSeen: true}
			}
			if !inspectionResponse.TrustDraftAllowed || inspectionResponse.DraftSuggestion == nil {
				return CommandResult{
					Output:        formatSiteInspectionResponse(inspectionResponse),
					Handled:       true,
					ToolEventSeen: true,
				}
			}
			if rl == nil {
				return CommandResult{
					Output:        formatSiteInspectionResponse(inspectionResponse) + "\n\nDenied: trust-draft creation requires an interactive terminal prompt.",
					Handled:       true,
					ToolEventSeen: true,
				}
			}

			fmt.Println(formatSiteInspectionResponse(inspectionResponse))
			fmt.Println(ui.Approval(ui.ApprovalRequest{
				Tool:   "site trust draft",
				Class:  loopgate.ApprovalClassLabel(loopgate.ApprovalClassCreateTrustDraft),
				Path:   inspectionResponse.NormalizedURL,
				Reason: "create reviewable trust draft for exact source",
			}))
			oldPrompt := ui.Prompt(0)
			rl.SetPrompt(ui.ApprovalPrompt("site trust draft"))
			answer, readErr := rl.Readline()
			rl.SetPrompt(oldPrompt)
			rl.Refresh()
			if readErr != nil {
				return CommandResult{Output: "Denied: could not read trust-draft approval input.", Handled: true, ToolEventSeen: true}
			}
			if !strings.EqualFold(strings.TrimSpace(answer), "y") {
				return CommandResult{Output: "Denied: trust draft creation was declined.", Handled: true, ToolEventSeen: true}
			}

			trustDraftResponse, err := commandContext.LoopgateClient.CreateTrustDraft(context.Background(), loopgate.SiteTrustDraftRequest{
				URL: siteURL,
			})
			if err != nil {
				return CommandResult{Output: "Error: " + err.Error(), Handled: true, ToolEventSeen: true}
			}
			return CommandResult{
				Output: strings.Join([]string{
					fmt.Sprintf("trust draft created for %s", trustDraftResponse.NormalizedURL),
					fmt.Sprintf("draft_path: %s", trustDraftResponse.DraftPath),
					fmt.Sprintf("provider: %s", trustDraftResponse.Suggestion.Provider),
					fmt.Sprintf("subject: %s", trustDraftResponse.Suggestion.Subject),
					fmt.Sprintf("capability: %s", trustDraftResponse.Suggestion.CapabilityName),
				}, "\n"),
				Handled:       true,
				ToolEventSeen: true,
			}
		default:
			return CommandResult{Output: "Usage: /site [inspect|trust-draft] <url>", Handled: true, ToolEventSeen: toolEventSeen}
		}

	case "/sandbox":
		if commandContext.LoopgateClient == nil {
			return CommandResult{Output: "Denied: sandbox operations require Loopgate.", Handled: true, ToolEventSeen: true}
		}
		if len(fields) < 3 {
			return CommandResult{Output: "Usage: /sandbox [import|stage|metadata|export] ...", Handled: true, ToolEventSeen: toolEventSeen}
		}
		switch strings.ToLower(fields[1]) {
		case "import":
			hostSourcePath := strings.TrimSpace(fields[2])
			if hostSourcePath == "" {
				return CommandResult{Output: "Usage: /sandbox import <host-path> [destination-name]", Handled: true, ToolEventSeen: toolEventSeen}
			}
			destinationName := ""
			if len(fields) >= 4 {
				destinationName = strings.TrimSpace(fields[3])
			}
			if destinationName == "" {
				destinationName = filepath.Base(hostSourcePath)
			}
			sandboxResponse, err := commandContext.LoopgateClient.SandboxImport(context.Background(), loopgate.SandboxImportRequest{
				HostSourcePath:  hostSourcePath,
				DestinationName: destinationName,
			})
			if err != nil {
				return CommandResult{Output: "Error: " + err.Error(), Handled: true, ToolEventSeen: true}
			}
			return CommandResult{Output: formatSandboxOperationResponse(sandboxResponse), Handled: true, ToolEventSeen: true}
		case "stage":
			sandboxSourcePath := strings.TrimSpace(fields[2])
			if sandboxSourcePath == "" {
				return CommandResult{Output: "Usage: /sandbox stage <sandbox-path> [output-name]", Handled: true, ToolEventSeen: toolEventSeen}
			}
			outputName := ""
			if len(fields) >= 4 {
				outputName = strings.TrimSpace(fields[3])
			}
			if outputName == "" {
				outputName = filepath.Base(sandboxSourcePath)
			}
			sandboxResponse, err := commandContext.LoopgateClient.SandboxStage(context.Background(), loopgate.SandboxStageRequest{
				SandboxSourcePath: sandboxSourcePath,
				OutputName:        outputName,
			})
			if err != nil {
				return CommandResult{Output: "Error: " + err.Error(), Handled: true, ToolEventSeen: true}
			}
			return CommandResult{Output: formatSandboxOperationResponse(sandboxResponse), Handled: true, ToolEventSeen: true}
		case "metadata":
			sandboxSourcePath := strings.TrimSpace(fields[2])
			if sandboxSourcePath == "" {
				return CommandResult{Output: "Usage: /sandbox metadata <sandbox-output-path>", Handled: true, ToolEventSeen: toolEventSeen}
			}
			metadataResponse, err := commandContext.LoopgateClient.SandboxMetadata(context.Background(), loopgate.SandboxMetadataRequest{
				SandboxSourcePath: sandboxSourcePath,
			})
			if err != nil {
				return CommandResult{Output: "Error: " + err.Error(), Handled: true, ToolEventSeen: true}
			}
			return CommandResult{Output: formatSandboxArtifactMetadataResponse(metadataResponse), Handled: true, ToolEventSeen: true}
		case "export":
			if len(fields) < 4 {
				return CommandResult{Output: "Usage: /sandbox export <sandbox-output-path> <host-destination>", Handled: true, ToolEventSeen: toolEventSeen}
			}
			if rl == nil {
				return CommandResult{Output: "Denied: sandbox export requires an interactive terminal prompt.", Handled: true, ToolEventSeen: true}
			}
			sandboxSourcePath := strings.TrimSpace(fields[2])
			hostDestinationPath := strings.TrimSpace(fields[3])
			fmt.Println(ui.Approval(ui.ApprovalRequest{
				Tool:   "sandbox export",
				Class:  loopgate.ApprovalClassLabel(loopgate.ApprovalClassExportSandboxArt),
				Path:   displaySandboxPath(sandboxSourcePath),
				Reason: fmt.Sprintf("export staged sandbox artifact to %s", hostDestinationPath),
			}))
			oldPrompt := ui.Prompt(0)
			rl.SetPrompt(ui.ApprovalPrompt("sandbox export"))
			answer, readErr := rl.Readline()
			rl.SetPrompt(oldPrompt)
			rl.Refresh()
			if readErr != nil {
				return CommandResult{Output: "Denied: could not read sandbox export approval input.", Handled: true, ToolEventSeen: true}
			}
			if !strings.EqualFold(strings.TrimSpace(answer), "y") {
				return CommandResult{Output: "Denied: sandbox export was declined.", Handled: true, ToolEventSeen: true}
			}
			sandboxResponse, err := commandContext.LoopgateClient.SandboxExport(context.Background(), loopgate.SandboxExportRequest{
				SandboxSourcePath:   sandboxSourcePath,
				HostDestinationPath: hostDestinationPath,
			})
			if err != nil {
				return CommandResult{Output: "Error: " + err.Error(), Handled: true, ToolEventSeen: true}
			}
			return CommandResult{Output: formatSandboxOperationResponse(sandboxResponse), Handled: true, ToolEventSeen: true}
		default:
			return CommandResult{Output: "Usage: /sandbox [import|stage|metadata|export] ...", Handled: true, ToolEventSeen: toolEventSeen}
		}

	case "/quarantine":
		if len(fields) < 3 {
			return CommandResult{Output: "Usage: /quarantine [metadata|view|prune] <quarantine-ref>", Handled: true, ToolEventSeen: toolEventSeen}
		}
		switch strings.ToLower(fields[1]) {
		case "metadata":
			quarantineMetadataResponse, err := commandContext.LoopgateClient.QuarantineMetadata(context.Background(), fields[2])
			if err != nil {
				return CommandResult{Output: "Error: " + err.Error(), Handled: true, ToolEventSeen: true}
			}
			return CommandResult{
				Output: strings.Join([]string{
					fmt.Sprintf("quarantine_ref: %s", quarantineMetadataResponse.QuarantineRef),
					fmt.Sprintf("capability: %s", quarantineMetadataResponse.Capability),
					fmt.Sprintf("request_id: %s", quarantineMetadataResponse.RequestID),
					fmt.Sprintf("trust_state: %s", quarantineMetadataResponse.TrustState),
					fmt.Sprintf("content_availability: %s", quarantineMetadataResponse.ContentAvailability),
					fmt.Sprintf("stored_at_utc: %s", quarantineMetadataResponse.StoredAtUTC),
					fmt.Sprintf("storage_state: %s", quarantineMetadataResponse.StorageState),
					fmt.Sprintf("content_type: %s", quarantineMetadataResponse.ContentType),
					fmt.Sprintf("size_bytes: %d", quarantineMetadataResponse.SizeBytes),
					fmt.Sprintf("content_sha256: %s", quarantineMetadataResponse.ContentSHA256),
					fmt.Sprintf("prune_eligible: %t", quarantineMetadataResponse.PruneEligible),
					fmt.Sprintf("prune_eligible_at_utc: %s", quarantineMetadataResponse.PruneEligibleAtUTC),
					fmt.Sprintf("view_action: %s", quarantineViewActionLabel(quarantineMetadataResponse)),
					fmt.Sprintf("fresh_promotion_from_source: %s", quarantinePromotionActionLabel(quarantineMetadataResponse)),
				}, "\n"),
				Handled:       true,
				ToolEventSeen: true,
			}
		case "view":
			quarantineViewResponse, err := commandContext.LoopgateClient.ViewQuarantinedPayload(context.Background(), fields[2])
			if err != nil {
				return CommandResult{Output: "Error: " + err.Error(), Handled: true, ToolEventSeen: true}
			}
			return CommandResult{
				Output: strings.Join([]string{
					fmt.Sprintf("quarantine_ref: %s", quarantineViewResponse.Metadata.QuarantineRef),
					fmt.Sprintf("trust_state: %s", quarantineViewResponse.Metadata.TrustState),
					fmt.Sprintf("content_availability: %s", quarantineViewResponse.Metadata.ContentAvailability),
					fmt.Sprintf("storage_state: %s", quarantineViewResponse.Metadata.StorageState),
					fmt.Sprintf("content_type: %s", quarantineViewResponse.Metadata.ContentType),
					"",
					quarantineViewResponse.RawPayload,
				}, "\n"),
				Handled:       true,
				ToolEventSeen: true,
			}
		case "prune":
			quarantineMetadataResponse, err := commandContext.LoopgateClient.PruneQuarantinedPayload(context.Background(), fields[2])
			if err != nil {
				return CommandResult{Output: "Error: " + err.Error(), Handled: true, ToolEventSeen: true}
			}
			return CommandResult{
				Output: strings.Join([]string{
					fmt.Sprintf("quarantine_ref: %s", quarantineMetadataResponse.QuarantineRef),
					fmt.Sprintf("trust_state: %s", quarantineMetadataResponse.TrustState),
					fmt.Sprintf("content_availability: %s", quarantineMetadataResponse.ContentAvailability),
					fmt.Sprintf("storage_state: %s", quarantineMetadataResponse.StorageState),
					fmt.Sprintf("blob_pruned_at_utc: %s", defaultSummaryValue(quarantineMetadataResponse.BlobPrunedAtUTC, "not_pruned")),
					fmt.Sprintf("content_sha256: %s", quarantineMetadataResponse.ContentSHA256),
				}, "\n"),
				Handled:       true,
				ToolEventSeen: true,
			}
		default:
			return CommandResult{Output: "Usage: /quarantine [metadata|view|prune] <quarantine-ref>", Handled: true, ToolEventSeen: toolEventSeen}
		}

	case "/config":
		return CommandResult{Output: summarizeConfigPaths(commandContext), Handled: true, ToolEventSeen: toolEventSeen}

	case "/tools":
		return CommandResult{Output: summarizeTools(commandContext), Handled: true, ToolEventSeen: toolEventSeen}

	case "/memory":
		if commandContext.LoopgateClient == nil {
			return CommandResult{Output: "Denied: memory operations require Loopgate.", Handled: true, ToolEventSeen: true}
		}
		if len(fields) >= 2 {
			switch strings.ToLower(fields[1]) {
			case "recall":
				if len(fields) < 3 {
					return CommandResult{Output: "Usage: /memory recall <key-id>", Handled: true, ToolEventSeen: toolEventSeen}
				}
				recallResponse, err := commandContext.LoopgateClient.RecallMemory(context.Background(), loopgate.MemoryRecallRequest{
					RequestedKeys: []string{fields[2]},
				})
				if err != nil {
					return CommandResult{Output: "Error: " + err.Error(), Handled: true, ToolEventSeen: toolEventSeen}
				}
				return CommandResult{Output: loopgateresult.FormatMemoryRecallResponse(recallResponse), Handled: true, ToolEventSeen: true}
			case "discover":
				if len(fields) < 3 {
					return CommandResult{Output: "Usage: /memory discover <terms...>", Handled: true, ToolEventSeen: toolEventSeen}
				}
				discoveryResponse, err := commandContext.LoopgateClient.DiscoverMemory(context.Background(), loopgate.MemoryDiscoverRequest{
					Query: strings.Join(fields[2:], " "),
				})
				if err != nil {
					return CommandResult{Output: "Error: " + err.Error(), Handled: true, ToolEventSeen: toolEventSeen}
				}
				return CommandResult{Output: loopgateresult.FormatMemoryDiscoverResponse(discoveryResponse), Handled: true, ToolEventSeen: true}
			case "remember":
				if len(fields) < 4 {
					return CommandResult{Output: "Usage: /memory remember <fact-key> <value>", Handled: true, ToolEventSeen: toolEventSeen}
				}
				rememberResponse, err := commandContext.LoopgateClient.RememberMemoryFact(context.Background(), loopgate.MemoryRememberRequest{
					FactKey:         strings.TrimSpace(fields[2]),
					FactValue:       strings.TrimSpace(strings.Join(fields[3:], " ")),
					CandidateSource: "explicit_fact",
					SourceChannel:   "shell_command",
				})
				if err != nil {
					return CommandResult{Output: "Error: " + loopgate.SafeMemoryRememberErrorText(err), Handled: true, ToolEventSeen: true}
				}
				return CommandResult{Output: loopgateresult.FormatMemoryRememberResponse(rememberResponse), Handled: true, ToolEventSeen: true}
			}
		}
		return CommandResult{Output: summarizeMemory(commandContext), Handled: true, ToolEventSeen: toolEventSeen}

	case "/goal":
		if len(fields) < 2 {
			return CommandResult{Output: "Usage: /goal add <text> | /goal close [text-or-id] | /goal list", Handled: true, ToolEventSeen: toolEventSeen}
		}
		switch strings.ToLower(fields[1]) {
		case "list":
			continuityState, err := memory.LoadGlobalContinuityState(filepath.Join(commandContext.RepoRoot, "core", "memory", "ledger", "ledger.jsonl"))
			if err != nil {
				return CommandResult{Output: "Error: unable to load continuity state: " + err.Error(), Handled: true, ToolEventSeen: toolEventSeen}
			}
			return CommandResult{
				Output:        formatActiveGoals(continuityState.GoalsByID),
				Handled:       true,
				ToolEventSeen: toolEventSeen,
			}
		case "add":
			goalText := strings.TrimSpace(strings.Join(fields[2:], " "))
			if goalText == "" {
				return CommandResult{Output: "Usage: /goal add <text>", Handled: true, ToolEventSeen: toolEventSeen}
			}
			goalID := "goal_" + time.Now().UTC().Format("20060102T150405.000000000Z")
			return CommandResult{
				Output:        fmt.Sprintf("goal added: %s\ntext: %s", goalID, goalText),
				Handled:       true,
				ToolEventSeen: toolEventSeen,
				RequiredAuditEvents: []CommandAuditEvent{{
					Type: "memory.goal.opened",
					Data: memory.AnnotateContinuityEvent(
						map[string]interface{}{
							"goal_id": goalID,
							"text":    goalText,
						},
						"goal_opened",
						memory.MemoryScopeGlobal,
						memory.EpistemicFlavorRemembered,
						nil,
						map[string]interface{}{
							"goal_id": goalID,
							"text":    goalText,
						},
					),
				}},
			}
		case "close":
			goalTarget := strings.TrimSpace(strings.Join(fields[2:], " "))
			continuityState, err := memory.LoadGlobalContinuityState(filepath.Join(commandContext.RepoRoot, "core", "memory", "ledger", "ledger.jsonl"))
			if err != nil {
				return CommandResult{Output: "Error: unable to load continuity state: " + err.Error(), Handled: true, ToolEventSeen: toolEventSeen}
			}
			goalID, goalText, resolveErr := resolveGoalTarget(continuityState.GoalsByID, goalTarget)
			if resolveErr != nil {
				return CommandResult{Output: resolveErr.Error(), Handled: true, ToolEventSeen: toolEventSeen}
			}
			return CommandResult{
				Output:        fmt.Sprintf("goal closed: %s\ntext: %s", goalID, goalText),
				Handled:       true,
				ToolEventSeen: toolEventSeen,
				RequiredAuditEvents: []CommandAuditEvent{{
					Type: "memory.goal.closed",
					Data: memory.AnnotateContinuityEvent(
						map[string]interface{}{
							"goal_id": goalID,
						},
						"goal_closed",
						memory.MemoryScopeGlobal,
						memory.EpistemicFlavorRemembered,
						nil,
						map[string]interface{}{
							"goal_id": goalID,
						},
					),
				}},
			}
		default:
			return CommandResult{Output: "Usage: /goal add <text> | /goal close [text-or-id] | /goal list", Handled: true, ToolEventSeen: toolEventSeen}
		}

	case "/todo":
		if len(fields) < 3 {
			return CommandResult{Output: "Usage: /todo [add|resolve] <text-or-id>", Handled: true, ToolEventSeen: toolEventSeen}
		}
		switch strings.ToLower(fields[1]) {
		case "add":
			itemText := strings.TrimSpace(strings.Join(fields[2:], " "))
			if itemText == "" {
				return CommandResult{Output: "Usage: /todo add <text>", Handled: true, ToolEventSeen: toolEventSeen}
			}
			itemID := "todo_" + time.Now().UTC().Format("20060102T150405.000000000Z")
			return CommandResult{
				Output:        fmt.Sprintf("unresolved item added: %s\ntext: %s", itemID, itemText),
				Handled:       true,
				ToolEventSeen: toolEventSeen,
				RequiredAuditEvents: []CommandAuditEvent{{
					Type: "memory.unresolved_item.opened",
					Data: memory.AnnotateContinuityEvent(
						map[string]interface{}{
							"item_id": itemID,
							"text":    itemText,
						},
						"unresolved_item_opened",
						memory.MemoryScopeGlobal,
						memory.EpistemicFlavorRemembered,
						nil,
						map[string]interface{}{
							"item_id": itemID,
							"text":    itemText,
						},
					),
				}},
			}
		case "resolve":
			itemID := strings.TrimSpace(fields[2])
			if itemID == "" {
				return CommandResult{Output: "Usage: /todo resolve <item-id>", Handled: true, ToolEventSeen: toolEventSeen}
			}
			continuityState, err := memory.LoadGlobalContinuityState(filepath.Join(commandContext.RepoRoot, "core", "memory", "ledger", "ledger.jsonl"))
			if err != nil {
				return CommandResult{Output: "Error: unable to load continuity state: " + err.Error(), Handled: true, ToolEventSeen: toolEventSeen}
			}
			if _, itemFound := continuityState.UnresolvedItemsByID[itemID]; !itemFound {
				return CommandResult{Output: fmt.Sprintf("Error: unresolved item is not active or does not exist: %s", itemID), Handled: true, ToolEventSeen: toolEventSeen}
			}
			return CommandResult{
				Output:        fmt.Sprintf("unresolved item resolved: %s", itemID),
				Handled:       true,
				ToolEventSeen: toolEventSeen,
				RequiredAuditEvents: []CommandAuditEvent{{
					Type: "memory.unresolved_item.resolved",
					Data: memory.AnnotateContinuityEvent(
						map[string]interface{}{
							"item_id": itemID,
						},
						"unresolved_item_resolved",
						memory.MemoryScopeGlobal,
						memory.EpistemicFlavorRemembered,
						nil,
						map[string]interface{}{
							"item_id": itemID,
						},
					),
				}},
			}
		default:
			return CommandResult{Output: "Usage: /todo [add|resolve] <text-or-id>", Handled: true, ToolEventSeen: toolEventSeen}
		}

	case "/model":
		if len(fields) >= 2 {
			subcommand := strings.ToLower(fields[1])
			switch subcommand {
			case "setup":
				return handleSetupCommand(commandContext, rl)
			case "validate":
				return CommandResult{Output: validateModelConfig(commandContext.LoopgateClient, commandContext.CurrentRuntimeConfig), Handled: true, ToolEventSeen: toolEventSeen}
			default:
				return CommandResult{Output: "Usage: /model [setup|validate]", Handled: true, ToolEventSeen: toolEventSeen}
			}
		}
		return CommandResult{Output: summarizeModel(commandContext), Handled: true, ToolEventSeen: toolEventSeen}

	case "/policy":
		fsCfg := commandContext.Policy.Tools.Filesystem
		return CommandResult{Output: ui.Policy(ui.PolicyConfig{
			Version:               commandContext.Policy.Version,
			ReadEnabled:           fsCfg.ReadEnabled,
			WriteEnabled:          fsCfg.WriteEnabled,
			WriteRequiresApproval: fsCfg.WriteRequiresApproval,
			AllowedRoots:          fsCfg.AllowedRoots,
			DeniedPaths:           fsCfg.DeniedPaths,
			LogCommands:           commandContext.Policy.Logging.LogCommands,
			LogToolCalls:          commandContext.Policy.Logging.LogToolCalls,
			LogMemoryPromotions:   commandContext.Policy.Logging.LogMemoryPromotions,
			AutoDistillate:        commandContext.Policy.Memory.AutoDistillate,
		}), Handled: true, ToolEventSeen: toolEventSeen}

	case "/ls":
		path := "."
		if len(fields) >= 2 {
			path = fields[1]
		}
		return executeLoopgateCapability(commandContext, rl, wrapLog, loopgate.CapabilityRequest{
			Capability: "fs_list",
			Arguments:  map[string]string{"path": path},
		})

	case "/cat":
		if len(fields) < 2 {
			return CommandResult{Output: "Usage: /cat <file>", Handled: true, ToolEventSeen: toolEventSeen}
		}
		return executeLoopgateCapability(commandContext, rl, wrapLog, loopgate.CapabilityRequest{
			Capability: "fs_read",
			Arguments:  map[string]string{"path": fields[1]},
		})

	case "/write":
		if len(fields) < 3 {
			return CommandResult{Output: "Usage: /write <file> <text...>", Handled: true, ToolEventSeen: toolEventSeen}
		}
		return executeLoopgateCapability(commandContext, rl, wrapLog, loopgate.CapabilityRequest{
			Capability: "fs_write",
			Arguments: map[string]string{
				"path":    fields[1],
				"content": strings.Join(fields[2:], " "),
			},
		})

	case "/setup":
		return handleSetupCommand(commandContext, rl)

	case "/debug":
		// Debug helpers. Keep this read-only.
		if len(fields) < 2 {
			return CommandResult{Output: strings.Join([]string{
				"Usage:",
				"  /debug help",
				"  /debug safepath <path>",
			}, "\n"), Handled: true, ToolEventSeen: toolEventSeen}
		}

		sub := strings.ToLower(fields[1])
		switch sub {
		case "help":
			return CommandResult{Output: strings.Join([]string{
				"Debug commands:",
				"  /debug help",
				"  /debug safepath <path>    (prints SafePath decision trail)",
				"Notes:",
				"  /debug state and /debug cursor require main.go to pass state/paths into commands.go; not wired yet.",
			}, "\n"), Handled: true, ToolEventSeen: toolEventSeen}

		case "safepath":
			if len(fields) < 3 {
				return CommandResult{Output: "Usage: /debug safepath <path>", Handled: true, ToolEventSeen: toolEventSeen}
			}
			targetPath := fields[2]
			ex, err := safety.ExplainSafePath(commandContext.RepoRoot, commandContext.Policy.Tools.Filesystem.AllowedRoots, commandContext.Policy.Tools.Filesystem.DeniedPaths, targetPath)
			if err != nil {
				// ExplainSafePath includes the decision trail even on error.
				return CommandResult{Output: fmt.Sprintf("DENY (%v)\nrepo=%s\ninput=%s\ncandidate=%s\nresolved=%s\nallowed_roots=%v\nallowed_match=%s\ndenied_paths=%v\ndenied_match=%s\ndecision=%s",
					err,
					ex.RepoAbs,
					ex.Input,
					ex.CandidateAbs,
					ex.ResolvedAbs,
					ex.AllowedRoots,
					ex.AllowedMatch,
					ex.DeniedPaths,
					ex.DeniedMatch,
					ex.Decision,
				), Handled: true, ToolEventSeen: toolEventSeen}
			}
			return CommandResult{Output: fmt.Sprintf("ALLOW\nrepo=%s\ninput=%s\ncandidate=%s\nresolved=%s\nallowed_roots=%v\nallowed_match=%s\ndenied_paths=%v\ndenied_match=%s\ndecision=%s",
				ex.RepoAbs,
				ex.Input,
				ex.CandidateAbs,
				ex.ResolvedAbs,
				ex.AllowedRoots,
				ex.AllowedMatch,
				ex.DeniedPaths,
				ex.DeniedMatch,
				ex.Decision,
			), Handled: true, ToolEventSeen: toolEventSeen}

		default:
			return CommandResult{Output: "Unknown debug command. Try /debug help.", Handled: true, ToolEventSeen: toolEventSeen}
		}

	default:
		return CommandResult{Output: "Unknown command. Type /help.", Handled: true, ToolEventSeen: toolEventSeen}
	}
}

type readlineWizardPrompter struct {
	rl *readline.Instance
}

func handleSetupCommand(commandContext CommandContext, rl *readline.Instance) CommandResult {
	if rl == nil {
		return CommandResult{Output: "Denied: setup wizard requires an interactive terminal prompt.", Handled: true}
	}
	if commandContext.LoopgateClient == nil {
		return CommandResult{Output: "Denied: setup wizard requires Loopgate-backed model validation.", Handled: true}
	}

	fmt.Println(ui.WizardHeader())
	setupResult, err := setuppkg.RunModelWizard(context.Background(), commandContext.RepoRoot, commandContext.CurrentRuntimeConfig, commandContext.LoopgateClient.ValidateModelConfig, commandContext.LoopgateClient.StoreModelConnection, setuppkg.ProbeOpenAICompatibleModels, &readlineWizardPrompter{rl: rl})
	if err != nil {
		return CommandResult{Output: ui.Red("✗") + " " + err.Error(), Handled: true}
	}

	return CommandResult{
		Output:               ui.WizardSummary(strings.Split(setupResult.Summary, "\n")),
		Handled:              true,
		RuntimeConfigChanged: true,
		UpdatedRuntimeConfig: setupResult.RuntimeConfig,
	}
}

func executeLoopgateCapability(commandContext CommandContext, rl *readline.Instance, wrapLog func(string, map[string]interface{}), capabilityRequest loopgate.CapabilityRequest) CommandResult {
	if commandContext.LoopgateClient == nil {
		return CommandResult{Output: "Denied: loopgate client is not configured.", Handled: true}
	}

	capabilityResponse, err := commandContext.LoopgateClient.ExecuteCapability(context.Background(), capabilityRequest)
	if err != nil {
		wrapLog("loopgate.capability.error", map[string]interface{}{
			"capability": capabilityRequest.Capability,
			"error":      err.Error(),
		})
		return CommandResult{Output: "Error: " + err.Error(), Handled: true, ToolEventSeen: true}
	}

	if capabilityResponse.ApprovalRequired {
		if rl == nil {
			wrapLog("loopgate.approval.denied", map[string]interface{}{
				"capability":          capabilityRequest.Capability,
				"approval_request_id": capabilityResponse.ApprovalRequestID,
				"reason":              "approval_prompt_unavailable",
			})
			return CommandResult{Output: "Denied: loopgate requires approval, but no approval prompt is available.", Handled: true, ToolEventSeen: true}
		}

		approvalDisplayMetadata := sanitizeApprovalDisplayMetadata(capabilityResponse.Metadata)
		preview, hidden := approvalPreview(loopgateresult.StructuredDisplayText(approvalDisplayMetadata), 140)
		fmt.Println(ui.Approval(ui.ApprovalRequest{
			Tool:    capabilityRequest.Capability,
			Class:   approvalClassDisplayLabel(approvalDisplayMetadata),
			Path:    toString(approvalDisplayMetadata["path"]),
			Bytes:   toInt(approvalDisplayMetadata["content_bytes"]),
			Preview: preview,
			Hidden:  hidden,
			Reason:  toString(approvalDisplayMetadata["approval_reason"]),
		}))

		oldPrompt := ui.Prompt(0)
		rl.SetPrompt(ui.ApprovalPrompt(capabilityRequest.Capability))
		answer, readErr := rl.Readline()
		rl.SetPrompt(oldPrompt)
		rl.Refresh()
		if readErr != nil {
			return CommandResult{Output: "Denied: could not read approval input.", Handled: true, ToolEventSeen: true}
		}

		approved := strings.EqualFold(strings.TrimSpace(answer), "y")
		wrapLog("loopgate.approval.submitted", map[string]interface{}{
			"capability":          capabilityRequest.Capability,
			"approval_request_id": capabilityResponse.ApprovalRequestID,
			"approved":            approved,
		})

		capabilityResponse, err = commandContext.LoopgateClient.DecideApproval(context.Background(), capabilityResponse.ApprovalRequestID, approved)
		if err != nil {
			return CommandResult{Output: "Error: " + err.Error(), Handled: true, ToolEventSeen: true}
		}
	}

	wrapLog("loopgate.capability.result", map[string]interface{}{
		"capability": capabilityRequest.Capability,
		"status":     capabilityResponse.Status,
	})
	return CommandResult{
		Output:        formatCapabilityResponse(capabilityResponse),
		Handled:       true,
		ToolEventSeen: true,
	}
}

func sanitizeApprovalDisplayMetadata(rawMetadata map[string]interface{}) map[string]interface{} {
	return loopgateresult.SanitizedApprovalMetadata(rawMetadata)
}

func approvalClassDisplayLabel(approvalDisplayMetadata map[string]interface{}) string {
	rawApprovalClass := toString(approvalDisplayMetadata["approval_class"])
	return loopgate.ApprovalClassLabel(rawApprovalClass)
}

func formatCapabilityResponse(capabilityResponse loopgate.CapabilityResponse) string {
	return loopgateresult.FormatDisplayResponse(capabilityResponse)
}

func toString(value interface{}) string {
	stringValue, _ := value.(string)
	return stringValue
}

func toInt(value interface{}) int {
	switch typedValue := value.(type) {
	case int:
		return typedValue
	case float64:
		return int(typedValue)
	default:
		return 0
	}
}

func quarantineViewActionLabel(quarantineMetadataResponse loopgate.QuarantineMetadataResponse) string {
	if quarantineMetadataResponse.ContentAvailability == "blob_available" {
		return "explicit_view_allowed"
	}
	return "metadata_only"
}

func quarantinePromotionActionLabel(quarantineMetadataResponse loopgate.QuarantineMetadataResponse) string {
	if quarantineMetadataResponse.ContentAvailability == "blob_available" {
		return "source_bytes_available"
	}
	return "source_bytes_unavailable"
}

func formatSiteInspectionResponse(siteInspectionResponse loopgate.SiteInspectionResponse) string {
	lines := []string{
		fmt.Sprintf("normalized_url: %s", siteInspectionResponse.NormalizedURL),
		fmt.Sprintf("scheme: %s", siteInspectionResponse.Scheme),
		fmt.Sprintf("host: %s", siteInspectionResponse.Host),
		fmt.Sprintf("path: %s", siteInspectionResponse.Path),
		fmt.Sprintf("http_status_code: %d", siteInspectionResponse.HTTPStatusCode),
		fmt.Sprintf("content_type: %s", siteInspectionResponse.ContentType),
		fmt.Sprintf("https: %t", siteInspectionResponse.HTTPS),
		fmt.Sprintf("tls_valid: %t", siteInspectionResponse.TLSValid),
		fmt.Sprintf("trust_draft_allowed: %t", siteInspectionResponse.TrustDraftAllowed),
	}
	if siteInspectionResponse.Certificate != nil {
		lines = append(lines,
			fmt.Sprintf("certificate_subject: %s", defaultSummaryValue(siteInspectionResponse.Certificate.Subject, "unknown")),
			fmt.Sprintf("certificate_issuer: %s", defaultSummaryValue(siteInspectionResponse.Certificate.Issuer, "unknown")),
			fmt.Sprintf("certificate_not_after_utc: %s", defaultSummaryValue(siteInspectionResponse.Certificate.NotAfterUTC, "unknown")),
			fmt.Sprintf("certificate_fingerprint_sha256: %s", defaultSummaryValue(siteInspectionResponse.Certificate.FingerprintSHA256, "unknown")),
		)
	}
	if siteInspectionResponse.DraftSuggestion != nil {
		lines = append(lines,
			fmt.Sprintf("suggested_provider: %s", siteInspectionResponse.DraftSuggestion.Provider),
			fmt.Sprintf("suggested_subject: %s", siteInspectionResponse.DraftSuggestion.Subject),
			fmt.Sprintf("suggested_capability: %s", siteInspectionResponse.DraftSuggestion.CapabilityName),
			fmt.Sprintf("suggested_content_class: %s", siteInspectionResponse.DraftSuggestion.ContentClass),
			fmt.Sprintf("suggested_extractor: %s", siteInspectionResponse.DraftSuggestion.Extractor),
			fmt.Sprintf("suggested_path: %s", siteInspectionResponse.DraftSuggestion.CapabilityPath),
		)
	}
	return strings.Join(lines, "\n")
}

func formatSandboxOperationResponse(sandboxResponse loopgate.SandboxOperationResponse) string {
	lines := []string{
		fmt.Sprintf("action: %s", sandboxResponse.Action),
		fmt.Sprintf("entry_type: %s", sandboxResponse.EntryType),
		fmt.Sprintf("sandbox_root: %s", sandboxResponse.SandboxRoot),
		fmt.Sprintf("sandbox_relative_path: %s", sandboxResponse.SandboxRelativePath),
		fmt.Sprintf("sandbox_absolute_path: %s", sandboxResponse.SandboxAbsolutePath),
	}
	if sandboxResponse.SourceSandboxPath != "" {
		lines = append(lines, fmt.Sprintf("source_sandbox_path: %s", sandboxResponse.SourceSandboxPath))
	}
	if sandboxResponse.HostPath != "" {
		hostPathLabel := "host_path"
		switch sandboxResponse.Action {
		case "import":
			hostPathLabel = "host_source_path"
		case "export":
			hostPathLabel = "host_destination_path"
		}
		lines = append(lines, fmt.Sprintf("%s: %s", hostPathLabel, sandboxResponse.HostPath))
	}
	if sandboxResponse.ArtifactRef != "" {
		lines = append(lines,
			fmt.Sprintf("artifact_ref: %s", sandboxResponse.ArtifactRef),
			fmt.Sprintf("content_sha256: %s", sandboxResponse.ContentSHA256),
			fmt.Sprintf("size_bytes: %d", sandboxResponse.SizeBytes),
		)
	}
	return strings.Join(lines, "\n")
}

func formatSandboxArtifactMetadataResponse(metadataResponse loopgate.SandboxArtifactMetadataResponse) string {
	return strings.Join([]string{
		fmt.Sprintf("artifact_ref: %s", metadataResponse.ArtifactRef),
		fmt.Sprintf("entry_type: %s", metadataResponse.EntryType),
		fmt.Sprintf("sandbox_root: %s", metadataResponse.SandboxRoot),
		fmt.Sprintf("sandbox_relative_path: %s", metadataResponse.SandboxRelativePath),
		fmt.Sprintf("sandbox_absolute_path: %s", metadataResponse.SandboxAbsolutePath),
		fmt.Sprintf("source_sandbox_path: %s", metadataResponse.SourceSandboxPath),
		fmt.Sprintf("content_sha256: %s", metadataResponse.ContentSHA256),
		fmt.Sprintf("size_bytes: %d", metadataResponse.SizeBytes),
		fmt.Sprintf("staged_at_utc: %s", metadataResponse.StagedAtUTC),
		fmt.Sprintf("review_action: %s", metadataResponse.ReviewAction),
		fmt.Sprintf("export_action: %s", metadataResponse.ExportAction),
	}, "\n")
}

func displaySandboxPath(rawPath string) string {
	normalizedSandboxPath, err := sandbox.NormalizeHomePath(rawPath)
	if err != nil {
		return rawPath
	}
	return sandbox.VirtualizeRelativeHomePath(normalizedSandboxPath)
}

func (prompter *readlineWizardPrompter) Ask(promptLabel string, defaultValue string) (string, error) {
	// Style the prompt: "  ▸ label > "
	styledPrompt := "  " + ui.Teal("▸") + " " + promptLabel
	originalPrompt := prompter.rl.Config.Prompt
	prompter.rl.SetPrompt(styledPrompt)
	answer, err := prompter.rl.ReadlineWithDefault(defaultValue)
	prompter.rl.SetPrompt(originalPrompt)
	prompter.rl.Refresh()
	return strings.TrimSpace(answer), err
}

func (prompter *readlineWizardPrompter) AskSecret(promptLabel string) (string, error) {
	styledPrompt := "  " + ui.Amber("▸") + " " + promptLabel
	secretBytes, err := prompter.rl.ReadPassword(styledPrompt)
	if err != nil {
		return "", err
	}
	prompter.rl.Refresh()
	return strings.TrimSpace(string(secretBytes)), nil
}

func (prompter *readlineWizardPrompter) Select(title string, options []setuppkg.SelectOption, defaultIdx int) (string, error) {
	// Pause readline so huh can take over terminal input cleanly.
	prompter.rl.Clean()
	defer prompter.rl.Refresh()

	uiOptions := make([]ui.SelectOption, len(options))
	for i, opt := range options {
		uiOptions[i] = ui.SelectOption{Value: opt.Value, Label: opt.Label, Desc: opt.Desc}
	}
	return ui.SelectMenu(title, uiOptions, defaultIdx)
}

func approvalPreview(content string, maxLen int) (string, bool) {
	trimmed := strings.TrimSpace(content)
	lower := strings.ToLower(trimmed)
	sensitiveHints := []string{
		"password",
		"secret",
		"token",
		"apikey",
		"api_key",
		"authorization",
		"bearer ",
	}

	for _, hint := range sensitiveHints {
		if strings.Contains(lower, hint) {
			return "", true
		}
	}

	if maxLen > 0 && len(trimmed) > maxLen {
		return trimmed[:maxLen] + "... (truncated)", false
	}
	return trimmed, false
}

func formatActiveGoals(goalsByID map[string]string) string {
	if len(goalsByID) == 0 {
		return "active goals: none"
	}

	goalIDs := make([]string, 0, len(goalsByID))
	for goalID := range goalsByID {
		goalIDs = append(goalIDs, goalID)
	}
	sort.Strings(goalIDs)

	lines := []string{fmt.Sprintf("active goals: %d", len(goalIDs))}
	for _, goalID := range goalIDs {
		lines = append(lines, fmt.Sprintf("- %s: %s", goalID, goalsByID[goalID]))
	}
	return strings.Join(lines, "\n")
}

func resolveGoalTarget(goalsByID map[string]string, rawTarget string) (string, string, error) {
	trimmedTarget := strings.TrimSpace(rawTarget)
	if trimmedTarget == "" {
		if len(goalsByID) == 1 {
			for goalID, goalText := range goalsByID {
				return goalID, goalText, nil
			}
		}
		if len(goalsByID) == 0 {
			return "", "", fmt.Errorf("there are no active goals to close")
		}
		return "", "", fmt.Errorf("multiple active goals exist, use `/goal list` and close one by id or text")
	}

	if goalText, goalFound := goalsByID[trimmedTarget]; goalFound {
		return trimmedTarget, goalText, nil
	}

	normalizedTarget := strings.ToLower(trimmedTarget)
	exactTextMatches := make([]string, 0)
	substringMatches := make([]string, 0)
	for goalID, goalText := range goalsByID {
		normalizedGoalText := strings.ToLower(strings.TrimSpace(goalText))
		if normalizedGoalText == normalizedTarget {
			exactTextMatches = append(exactTextMatches, goalID)
			continue
		}
		if strings.Contains(strings.ToLower(goalID), normalizedTarget) || strings.Contains(normalizedGoalText, normalizedTarget) {
			substringMatches = append(substringMatches, goalID)
		}
	}

	if len(exactTextMatches) == 1 {
		goalID := exactTextMatches[0]
		return goalID, goalsByID[goalID], nil
	}
	if len(exactTextMatches) > 1 {
		return "", "", fmt.Errorf("goal target is ambiguous, matching active goals: %s", strings.Join(sortedCopy(exactTextMatches), ", "))
	}
	if len(substringMatches) == 1 {
		goalID := substringMatches[0]
		return goalID, goalsByID[goalID], nil
	}
	if len(substringMatches) > 1 {
		return "", "", fmt.Errorf("goal target is ambiguous, matching active goals: %s", strings.Join(sortedCopy(substringMatches), ", "))
	}
	return "", "", fmt.Errorf("goal is not active or does not exist: %s", trimmedTarget)
}

func sortedCopy(values []string) []string {
	sortedValues := append([]string(nil), values...)
	sort.Strings(sortedValues)
	return sortedValues
}

func formatSiteCommandError(rawSiteURL string, err error) string {
	trimmedSiteURL := strings.TrimSpace(rawSiteURL)
	if trimmedSiteURL != "" && !strings.Contains(trimmedSiteURL, "://") && strings.Contains(err.Error(), `site_url_invalid: unsupported scheme ""`) {
		return fmt.Sprintf("Error: expected a full URL with scheme. Try: /site inspect https://%s", trimmedSiteURL)
	}
	return "Error: " + err.Error()
}
