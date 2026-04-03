package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"morph/internal/haven/threadstore"
	"morph/internal/loopgate"
	"morph/internal/secrets"
)

const (
	havenAgentWorkKindHostFolderOrganize = "host_folder_organize"
	havenAgentHostOrganizeTaskTitle      = "Organize granted host folders"
	havenAgentOrganizeNextStep           = "Inspect granted folder → host.organize.plan → host.plan.apply after approval"
	// Short, fixed acknowledgment — not a long model-generated paragraph.
	havenAgentHostOrganizeAck = "Got it — I put that on your task list. Next I’ll inspect your granted folders, draft a plan, and wait for your approval before anything moves on disk."
	havenAgentHostOrganizeDoneAssistant = "Done — the organize plan was applied on disk. Open **Workspace** to review the mirror, or check the folder in Finder."
	havenAgentOrganizeCompleteReason      = "host_folder_organize_applied"
)

// havenMessageLooksLikeHostFolderOrganizeRequest is a narrow heuristic for the MVP workflow
// (no general classifier).
func havenMessageLooksLikeHostFolderOrganizeRequest(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}
	organize := strings.Contains(lower, "organize") || strings.Contains(lower, "organise") ||
		(strings.Contains(lower, "tidy") && (strings.Contains(lower, "file") || strings.Contains(lower, "folder") || strings.Contains(lower, "desktop") || strings.Contains(lower, "download")))
	if !organize {
		return false
	}
	scope := strings.Contains(lower, "file") || strings.Contains(lower, "folder") || strings.Contains(lower, "download") ||
		strings.Contains(lower, "desktop") || strings.Contains(lower, "disk") || strings.Contains(lower, "drive")
	return scope
}

func (app *HavenApp) emitAgentWorkPhase(threadID string, phase string, detail map[string]interface{}) {
	if app.emitter == nil || strings.TrimSpace(threadID) == "" {
		return
	}
	payload := map[string]interface{}{
		"thread_id": threadID,
		"phase":     strings.TrimSpace(phase),
	}
	for k, v := range detail {
		payload[k] = v
	}
	app.emitter.Emit("haven:agent_work_phase", payload)
}

func (app *HavenApp) appendVisibleAssistantMessage(threadID string, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	_ = app.threadStore.AppendEvent(threadID, threadstore.ConversationEvent{
		Type: threadstore.EventAssistantMessage,
		Data: map[string]interface{}{"text": text},
	})
	app.emitter.Emit("haven:assistant_message", map[string]interface{}{
		"thread_id": threadID,
		"text":      text,
	})
}

// maybeStartHostFolderOrganizeAgentWork ensures a Task Board item when the user message matches
// the narrow organize-files heuristic. Returns an error if ensure fails (fail-closed for this path).
func (app *HavenApp) maybeStartHostFolderOrganizeAgentWork(ctx context.Context, threadID, userMessage string, exec *threadExecution) error {
	if !havenMessageLooksLikeHostFolderOrganizeRequest(userMessage) {
		return nil
	}

	app.emitAgentWorkPhase(threadID, "planning", map[string]interface{}{
		"work_kind": havenAgentWorkKindHostFolderOrganize,
	})

	ensureCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	resp, err := app.loopgateClient.HavenAgentWorkItemEnsure(ensureCtx, loopgate.HavenAgentWorkEnsureRequest{
		Text:     havenAgentHostOrganizeTaskTitle,
		NextStep: havenAgentOrganizeNextStep,
	})
	if err != nil {
		app.emitAgentWorkPhase(threadID, "failed", map[string]interface{}{
			"detail": "work_item_ensure_failed",
		})
		return fmt.Errorf("haven agent work-item ensure: %w", err)
	}
	if strings.TrimSpace(resp.ItemID) == "" {
		app.emitAgentWorkPhase(threadID, "failed", map[string]interface{}{
			"detail": "work_item_ensure_missing_id",
		})
		return fmt.Errorf("haven agent work-item ensure returned empty item_id")
	}

	exec.setAgentWorkTracking(resp.ItemID, havenAgentWorkKindHostFolderOrganize)
	app.RefreshWakeState()

	app.emitAgentWorkPhase(threadID, "acting", map[string]interface{}{
		"work_kind": havenAgentWorkKindHostFolderOrganize,
		"item_id":   resp.ItemID,
	})
	app.appendVisibleAssistantMessage(threadID, havenAgentHostOrganizeAck)
	return nil
}

func (app *HavenApp) finalizeHostFolderOrganizeAgentWork(ctx context.Context, threadID string, exec *threadExecution, itemID string) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return
	}
	_, err := app.loopgateClient.HavenAgentWorkItemComplete(ctx, itemID, havenAgentOrganizeCompleteReason)
	if err != nil {
		if app.emitter != nil {
			app.emitter.Emit("haven:security_alert", map[string]interface{}{
				"thread_id": threadID,
				"type":      "agent_work_complete_failed",
				"message":   secrets.RedactText(err.Error()),
			})
		}
		return
	}
	exec.markAgentWorkItemClosed()
	app.RefreshWakeState()
	app.emitAgentWorkPhase(threadID, "completed", map[string]interface{}{
		"work_kind":     havenAgentWorkKindHostFolderOrganize,
		"take_me_there": "workspace",
	})
	app.appendVisibleAssistantMessage(threadID, havenAgentHostOrganizeDoneAssistant)
}
