package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"morph/internal/haven/threadstore"
	"morph/internal/loopgate"
	"morph/internal/loopgateresult"
)

// LoadWakeState loads the durable wake-state from Loopgate memory.
// This is called at startup and periodically to keep Morph's memory current.
// The wake-state contains remembered facts, active goals, unresolved items,
// and resonate keys from previous sessions.
func (app *HavenApp) LoadWakeState() {
	if err := app.reloadMemoryState(10*time.Second, true); err != nil {
		fmt.Fprintf(os.Stderr, "haven: load wake-state: %v (memory will be empty)\n", err)
	} else {
		fmt.Fprintf(os.Stderr, "haven: wake-state loaded (%d bytes)\n", len(app.currentWakeStateText()))
	}
}

// RefreshWakeState reloads the wake-state from Loopgate.
// Called after significant memory events (thread completion, etc).
func (app *HavenApp) RefreshWakeState() {
	_ = app.reloadMemoryState(5*time.Second, false)
}

// GetMemoryStatus returns the current memory state for display in the frontend.
func (app *HavenApp) GetMemoryStatus() MemoryStatusResponse {
	app.memoryMu.RLock()
	wakeSnapshot := app.wakeSnapshot
	wakeDiagnostic := app.wakeDiagnostic
	wakeStateText := app.wakeState
	app.memoryMu.RUnlock()

	wakeStateSummary, currentFocus := summarizeWakeStateSnapshot(wakeSnapshot, wakeStateText)
	return MemoryStatusResponse{
		HasWakeState:            wakeStateText != "",
		WakeStateSummary:        wakeStateSummary,
		CurrentFocus:            currentFocus,
		RememberedFactCount:     len(wakeSnapshot.RecentFacts),
		RememberedFacts:         summarizeRememberedFacts(wakeSnapshot.RecentFacts),
		ActiveGoalCount:         len(wakeSnapshot.ActiveGoals),
		UnresolvedItemCount:     len(wakeSnapshot.UnresolvedItems),
		ActiveGoals:             append([]string(nil), wakeSnapshot.ActiveGoals...),
		UnresolvedItems:         append([]loopgate.MemoryWakeStateOpenItem(nil), wakeSnapshot.UnresolvedItems...),
		IncludedDiagnosticCount: wakeDiagnostic.IncludedCount,
		ExcludedDiagnosticCount: wakeDiagnostic.ExcludedCount,
		DiagnosticSummary:       summarizeWakeDiagnostic(wakeDiagnostic),
		LastUpdatedUTC:          firstNonEmptyString(wakeSnapshot.CreatedAtUTC, wakeDiagnostic.CreatedAtUTC),
	}
}

// MemoryStatusResponse contains the current memory state for the frontend.
type MemoryStatusResponse struct {
	HasWakeState            bool                               `json:"has_wake_state"`
	WakeStateSummary        string                             `json:"wake_state_summary"`
	CurrentFocus            string                             `json:"current_focus,omitempty"`
	RememberedFactCount     int                                `json:"remembered_fact_count"`
	RememberedFacts         []RememberedFactSummary            `json:"remembered_facts,omitempty"`
	ActiveGoalCount         int                                `json:"active_goal_count"`
	UnresolvedItemCount     int                                `json:"unresolved_item_count"`
	ActiveGoals             []string                           `json:"active_goals,omitempty"`
	UnresolvedItems         []loopgate.MemoryWakeStateOpenItem `json:"unresolved_items,omitempty"`
	IncludedDiagnosticCount int                                `json:"included_diagnostic_count"`
	ExcludedDiagnosticCount int                                `json:"excluded_diagnostic_count"`
	DiagnosticSummary       string                             `json:"diagnostic_summary,omitempty"`
	LastUpdatedUTC          string                             `json:"last_updated_utc,omitempty"`
}

type RememberedFactSummary struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func (app *HavenApp) reloadMemoryState(timeout time.Duration, logDiagnosticFailure bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	wakeStateResponse, err := app.loopgateClient.LoadMemoryWakeState(ctx)
	if err != nil {
		return err
	}

	wakeDiagnostic, diagnosticErr := app.loopgateClient.LoadMemoryDiagnosticWake(ctx)
	if diagnosticErr != nil {
		if logDiagnosticFailure {
			fmt.Fprintf(os.Stderr, "haven: load memory diagnostics: %v\n", diagnosticErr)
		}
		wakeDiagnostic = loopgate.MemoryDiagnosticWakeResponse{}
	}

	app.setMemoryState(wakeStateResponse, wakeDiagnostic)
	return nil
}

func (app *HavenApp) setMemoryState(wakeStateResponse loopgate.MemoryWakeStateResponse, wakeDiagnostic loopgate.MemoryDiagnosticWakeResponse) {
	formattedWakeState := loopgateresult.FormatMemoryWakeStateResponse(wakeStateResponse)

	app.memoryMu.Lock()
	app.wakeSnapshot = wakeStateResponse
	app.wakeDiagnostic = wakeDiagnostic
	app.wakeState = formattedWakeState
	app.memoryMu.Unlock()

	if app.presence != nil {
		app.presence.NotifyContinuityLoaded(wakeStateResponse)
	}
	if app.emitter != nil {
		app.emitter.Emit("haven:memory_updated", app.GetMemoryStatus())
	}
}

func (app *HavenApp) currentWakeStateText() string {
	app.memoryMu.RLock()
	defer app.memoryMu.RUnlock()
	return app.wakeState
}

func (app *HavenApp) currentWakeSnapshot() loopgate.MemoryWakeStateResponse {
	app.memoryMu.RLock()
	defer app.memoryMu.RUnlock()
	return app.wakeSnapshot
}

func summarizeWakeStateSnapshot(wakeSnapshot loopgate.MemoryWakeStateResponse, formattedWakeState string) (summary string, currentFocus string) {
	if formattedWakeState == "" {
		return "No durable continuity yet. Morph is starting fresh.", ""
	}
	actionableTask, hasActionableTask := firstActionableWakeTask(wakeSnapshot, time.Now().UTC())
	if hasActionableTask {
		currentFocus = trimMemoryPreview(actionableTask.Text)
		if currentFocus == "" {
			currentFocus = actionableTask.ID
		}
		return fmt.Sprintf("Carrying %d actionable task(s) forward.", countActionableWakeTasks(wakeSnapshot, time.Now().UTC())), currentFocus
	}
	if len(wakeSnapshot.ActiveGoals) > 0 {
		currentFocus = trimMemoryPreview(wakeSnapshot.ActiveGoals[0])
		return fmt.Sprintf("Holding onto %d active goal(s) from earlier work.", len(wakeSnapshot.ActiveGoals)), currentFocus
	}
	if scheduledTaskCount := countScheduledWakeTasks(wakeSnapshot, time.Now().UTC()); scheduledTaskCount > 0 {
		currentFocus = trimMemoryPreview(wakeSnapshot.UnresolvedItems[0].Text)
		if currentFocus == "" {
			currentFocus = wakeSnapshot.UnresolvedItems[0].ID
		}
		return fmt.Sprintf("Holding %d scheduled task(s) for later.", scheduledTaskCount), currentFocus
	}
	if len(wakeSnapshot.RecentFacts) > 0 {
		currentFocus = trimMemoryPreview(fmt.Sprintf("%s = %v", wakeSnapshot.RecentFacts[0].Name, wakeSnapshot.RecentFacts[0].Value))
		return fmt.Sprintf("Keeping %d remembered fact(s) close at hand.", len(wakeSnapshot.RecentFacts)), currentFocus
	}
	if len(wakeSnapshot.ResonateKeys) > 0 {
		return fmt.Sprintf("Wake-state is loaded with %d continuity key(s).", len(wakeSnapshot.ResonateKeys)), ""
	}
	return "Wake-state is loaded, but there is not much settled continuity yet.", ""
}

func summarizeWakeDiagnostic(wakeDiagnostic loopgate.MemoryDiagnosticWakeResponse) string {
	if strings.TrimSpace(wakeDiagnostic.ReportID) == "" {
		return "Wake diagnostics are not available yet."
	}
	if wakeDiagnostic.ExcludedCount > 0 {
		return fmt.Sprintf("%d item(s) are active in wake-state; %d were held back after ranking and trimming.", wakeDiagnostic.IncludedCount, wakeDiagnostic.ExcludedCount)
	}
	if wakeDiagnostic.IncludedCount > 0 {
		return fmt.Sprintf("%d memory item(s) are active in wake-state right now.", wakeDiagnostic.IncludedCount)
	}
	return "Wake diagnostics are present, but nothing is active yet."
}

func trimMemoryPreview(rawText string) string {
	trimmedText := strings.TrimSpace(rawText)
	if trimmedText == "" {
		return ""
	}
	trimmedText = strings.Join(strings.Fields(trimmedText), " ")
	if len(trimmedText) <= 80 {
		return trimmedText
	}
	return trimmedText[:77] + "..."
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func summarizeRememberedFacts(recentFacts []loopgate.MemoryWakeStateRecentFact) []RememberedFactSummary {
	if len(recentFacts) == 0 {
		return nil
	}
	summaries := make([]RememberedFactSummary, 0, len(recentFacts))
	for _, recentFact := range recentFacts {
		summaries = append(summaries, RememberedFactSummary{
			Name:  recentFact.Name,
			Value: trimMemoryPreview(fmt.Sprintf("%v", recentFact.Value)),
		})
	}
	return summaries
}

func firstActionableWakeTask(wakeSnapshot loopgate.MemoryWakeStateResponse, nowUTC time.Time) (loopgate.MemoryWakeStateOpenItem, bool) {
	for _, unresolvedItem := range wakeSnapshot.UnresolvedItems {
		if wakeTaskIsActionable(unresolvedItem, nowUTC) {
			return unresolvedItem, true
		}
	}
	return loopgate.MemoryWakeStateOpenItem{}, false
}

func countActionableWakeTasks(wakeSnapshot loopgate.MemoryWakeStateResponse, nowUTC time.Time) int {
	actionableTaskCount := 0
	for _, unresolvedItem := range wakeSnapshot.UnresolvedItems {
		if wakeTaskIsActionable(unresolvedItem, nowUTC) {
			actionableTaskCount++
		}
	}
	return actionableTaskCount
}

func countScheduledWakeTasks(wakeSnapshot loopgate.MemoryWakeStateResponse, nowUTC time.Time) int {
	scheduledTaskCount := 0
	for _, unresolvedItem := range wakeSnapshot.UnresolvedItems {
		if strings.TrimSpace(unresolvedItem.ScheduledForUTC) != "" && !wakeTaskIsActionable(unresolvedItem, nowUTC) {
			scheduledTaskCount++
		}
	}
	return scheduledTaskCount
}

func wakeTaskIsActionable(unresolvedItem loopgate.MemoryWakeStateOpenItem, nowUTC time.Time) bool {
	if strings.TrimSpace(unresolvedItem.ScheduledForUTC) == "" {
		return true
	}
	scheduledForUTC, err := time.Parse(time.RFC3339Nano, unresolvedItem.ScheduledForUTC)
	if err != nil {
		return true
	}
	return !scheduledForUTC.After(nowUTC)
}

// DistillThread submits a completed thread to Loopgate continuity inspection.
// Haven does not author durable memory here: it proposes attributable events;
// Loopgate policy, TCL governance, and the inspector decide what (if anything)
// becomes distillates, resonate keys, or wake-state inputs.
// Kept as DistillThread for Wails binding stability (see frontend wailsjs).
func (app *HavenApp) DistillThread(threadID string) {
	events, err := app.threadStore.LoadThread(threadID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "haven: continuity inspection: load thread %s: %v\n", threadID, err)
		return
	}

	// Only propose inspection when we have continuity-shaped events to send.
	continuityEvents := threadEventsToContinuityEvents(events, app.sessionID)
	if len(continuityEvents) == 0 {
		return
	}

	inspectionID := makeInspectionID()
	approxBytes := estimatePayloadBytes(events)
	approxTokens := approxBytes / 4 // rough estimate

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = app.loopgateClient.InspectContinuityThread(ctx, loopgate.ContinuityInspectRequest{
		InspectionID:       inspectionID,
		ThreadID:           threadID,
		Scope:              "global",
		SealedAtUTC:        time.Now().UTC().Format(time.RFC3339Nano),
		EventCount:         len(continuityEvents),
		ApproxPayloadBytes: approxBytes,
		ApproxPromptTokens: approxTokens,
		Tags:               []string{"haven", "conversation"},
		Events:             continuityEvents,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "haven: continuity inspection: thread %s inspect failed: %v\n", threadID, err)
		return
	}

	fmt.Fprintf(os.Stderr, "haven: continuity inspection: submitted thread %s (%d events) inspection_id=%s\n", threadID, len(continuityEvents), inspectionID)
}

// threadEventsToContinuityEvents converts Haven threadstore events into the
// ContinuityEventInput format that Loopgate expects for continuity inspection.
func threadEventsToContinuityEvents(events []threadstore.ConversationEvent, sessionID string) []loopgate.ContinuityEventInput {
	var result []loopgate.ContinuityEventInput

	for i, event := range events {
		// Map threadstore event types to continuity event types.
		continuityType := mapEventType(event.Type)
		if continuityType == "" {
			continue
		}

		payload := make(map[string]interface{}, len(event.Data))
		for k, v := range event.Data {
			payload[k] = v
		}

		result = append(result, loopgate.ContinuityEventInput{
			TimestampUTC:    event.TS,
			SessionID:       sessionID,
			Type:            continuityType,
			Scope:           "global",
			ThreadID:        event.ThreadID,
			EpistemicFlavor: "freshly_checked",
			LedgerSequence:  int64(i + 1),
			EventHash:       hashEvent(event),
			Payload:         payload,
		})
	}

	return result
}

// mapEventType converts Haven threadstore event types to continuity event types
// that Loopgate understands for distillation.
func mapEventType(eventType string) string {
	switch eventType {
	case threadstore.EventUserMessage:
		return "user_message"
	case threadstore.EventAssistantMessage:
		return "assistant_response"
	case threadstore.EventOrchToolResult:
		return "tool_executed"
	default:
		return "" // skip orchestration internals
	}
}

func makeInspectionID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("haven-insp-%s-%s", time.Now().UTC().Format("20060102-150405"), hex.EncodeToString(b))
}

func hashEvent(event threadstore.ConversationEvent) string {
	data, _ := json.Marshal(event)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:16]) // 128-bit prefix
}

func estimatePayloadBytes(events []threadstore.ConversationEvent) int {
	total := 0
	for _, event := range events {
		if text, ok := event.Data["text"].(string); ok {
			total += len(text)
		}
		if output, ok := event.Data["output"].(string); ok {
			total += len(output)
		}
	}
	return total
}
