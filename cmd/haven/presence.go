package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"morph/internal/loopgate"
)

// PresenceState represents Morph's visible state on the desktop.
type PresenceState string

const (
	PresenceIdle     PresenceState = "idle"
	PresenceWorking  PresenceState = "working"
	PresenceThinking PresenceState = "thinking"
	PresenceCreating PresenceState = "creating"
	PresenceReading  PresenceState = "reading"
	PresenceSleeping PresenceState = "sleeping"
	PresenceExcited  PresenceState = "excited"
)

// PresenceAnchor is the desktop area where Morph appears to be spending attention.
type PresenceAnchor string

const (
	PresenceAnchorDesk      PresenceAnchor = "desk"
	PresenceAnchorMorph     PresenceAnchor = "morph"
	PresenceAnchorWorkspace PresenceAnchor = "workspace"
	PresenceAnchorTodo      PresenceAnchor = "todo"
	PresenceAnchorJournal   PresenceAnchor = "journal"
	PresenceAnchorPaint     PresenceAnchor = "paint"
	PresenceAnchorLoopgate  PresenceAnchor = "loopgate"
	PresenceAnchorActivity  PresenceAnchor = "activity"
)

// PresenceManager tracks Morph's presence state and emits changes to the frontend.
type PresenceManager struct {
	mu              sync.Mutex
	state           PresenceState
	statusText      string
	detailText      string
	anchor          PresenceAnchor
	lastFocusAnchor PresenceAnchor
	lastActivity    time.Time
	emitter         EventEmitter
	stopCh          chan struct{}
	morphName       string
	stopOnce        sync.Once
	// snapshotPath, when set, is runtime/state/haven_presence.json under the repo root
	// so Loopgate GET /v1/ui/presence can read the same projection as the Wails shell.
	snapshotPath string
}

// PresenceResponse is returned to the frontend.
type PresenceResponse struct {
	State      string `json:"state"`
	StatusText string `json:"status_text"`
	DetailText string `json:"detail_text,omitempty"`
	Anchor     string `json:"anchor"`
}

// NewPresenceManager creates a new presence manager.
func NewPresenceManager(emitter EventEmitter, morphName string) *PresenceManager {
	if morphName == "" {
		morphName = "Morph"
	}
	presenceManager := &PresenceManager{
		state:           PresenceIdle,
		statusText:      morphName + " is getting settled in",
		detailText:      "finding a comfortable place in Haven",
		anchor:          PresenceAnchorDesk,
		lastFocusAnchor: PresenceAnchorDesk,
		lastActivity:    time.Now(),
		emitter:         emitter,
		stopCh:          make(chan struct{}),
		morphName:       morphName,
	}
	go presenceManager.idleWatcher()
	return presenceManager
}

// GetPresence returns the current presence state.
func (app *HavenApp) GetPresence() PresenceResponse {
	if app.presence == nil {
		return PresenceResponse{
			State:      string(PresenceIdle),
			StatusText: "Morph is idle",
			Anchor:     string(PresenceAnchorDesk),
		}
	}
	app.presence.mu.Lock()
	defer app.presence.mu.Unlock()
	return PresenceResponse{
		State:      string(app.presence.state),
		StatusText: app.presence.statusText,
		DetailText: app.presence.detailText,
		Anchor:     string(app.presence.anchor),
	}
}

// SetPresence is called internally to update Morph's presence.
func (presenceManager *PresenceManager) SetPresence(state PresenceState, statusText string) {
	presenceManager.applyPresence(state, statusText, "", PresenceAnchorDesk, true)
}

// SetPresenceWithContext updates presence with a richer desktop location and detail line.
func (presenceManager *PresenceManager) SetPresenceWithContext(state PresenceState, statusText string, detailText string, anchor PresenceAnchor) {
	presenceManager.applyPresence(state, statusText, detailText, anchor, true)
}

// NotifyToolStarted updates presence when a tool execution begins.
func (presenceManager *PresenceManager) NotifyToolStarted(capability string, arguments map[string]string) {
	state, statusText, detailText, anchor := presenceContextForCapability(presenceManager.morphName, capability, arguments)
	presenceManager.SetPresenceWithContext(state, statusText, detailText, anchor)
}

// NotifyThinking updates presence when the model is being called.
func (presenceManager *PresenceManager) NotifyThinking() {
	presenceManager.SetPresenceWithContext(PresenceThinking, presenceManager.morphName+" is thinking...", "lining up the next step", PresenceAnchorMorph)
}

// NotifyAwaitingApproval updates presence when Morph is blocked on a Loopgate approval.
func (presenceManager *PresenceManager) NotifyAwaitingApproval(capability string, arguments map[string]string) {
	_, _, detailText, _ := presenceContextForCapability(presenceManager.morphName, capability, arguments)
	if detailText == "" {
		detailText = "waiting for a decision from Security"
	}
	presenceManager.SetPresenceWithContext(PresenceWorking, presenceManager.morphName+" is waiting for you", detailText, PresenceAnchorLoopgate)
}

// NotifyCompleted updates presence when execution finishes.
func (presenceManager *PresenceManager) NotifyCompleted(completedWork *completedWorkTracker) {
	statusText, detailText, anchor := completionPresenceContext(presenceManager.morphName, completedWork)
	presenceManager.SetPresenceWithContext(PresenceExcited, statusText, detailText, anchor)
	go func() {
		time.Sleep(8 * time.Second)
		presenceManager.mu.Lock()
		if presenceManager.state == PresenceExcited {
			presenceManager.mu.Unlock()
			presenceManager.NotifyIdle()
		} else {
			presenceManager.mu.Unlock()
		}
	}()
}

// NotifyIdle resets to idle state with a time-aware status and visible home in Haven.
func (presenceManager *PresenceManager) NotifyIdle() {
	statusText, detailText, anchor := idlePresenceSnapshot(presenceManager.morphName, presenceManager.lastFocusAnchor, time.Now())
	presenceManager.applyPresence(PresenceIdle, statusText, detailText, anchor, true)
}

// NotifyFailed updates presence on error.
func (presenceManager *PresenceManager) NotifyFailed() {
	presenceManager.SetPresenceWithContext(PresenceIdle, presenceManager.morphName+" ran into a problem", "staying close in case you want another pass", PresenceAnchorMorph)
}

// NotifyContinuityLoaded updates startup presence from durable wake-state.
func (presenceManager *PresenceManager) NotifyContinuityLoaded(wakeState loopgate.MemoryWakeStateResponse) {
	presenceManager.mu.Lock()
	currentState := presenceManager.state
	presenceManager.mu.Unlock()
	if currentState != PresenceIdle && currentState != PresenceSleeping {
		return
	}

	switch {
	case len(wakeState.UnresolvedItems) > 0:
		detailText := strings.TrimSpace(wakeState.UnresolvedItems[0].Text)
		if detailText == "" {
			detailText = wakeState.UnresolvedItems[0].ID
		}
		presenceManager.applyPresence(PresenceIdle, presenceManager.morphName+" is picking back up", trimPresenceDetail(detailText), PresenceAnchorTodo, false)
	case len(wakeState.ActiveGoals) > 0:
		presenceManager.applyPresence(PresenceIdle, presenceManager.morphName+" is carrying work forward", trimPresenceDetail(wakeState.ActiveGoals[0]), PresenceAnchorTodo, false)
	case len(wakeState.RecentFacts) > 0:
		detailText := fmt.Sprintf("%s = %v", wakeState.RecentFacts[0].Name, wakeState.RecentFacts[0].Value)
		presenceManager.applyPresence(PresenceIdle, presenceManager.morphName+" remembers where things stand", trimPresenceDetail(detailText), PresenceAnchorDesk, false)
	default:
		presenceManager.applyPresence(PresenceIdle, presenceManager.morphName+" is getting settled in", "finding a comfortable place in Haven", PresenceAnchorDesk, false)
	}
}

// idleWatcher periodically checks for extended idle and updates ambient presence.
func (presenceManager *PresenceManager) idleWatcher() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-presenceManager.stopCh:
			return
		case <-ticker.C:
			presenceManager.mu.Lock()
			idle := presenceManager.state == PresenceIdle
			elapsed := time.Since(presenceManager.lastActivity)
			preferredAnchor := presenceManager.lastFocusAnchor
			morphName := presenceManager.morphName
			presenceManager.mu.Unlock()

			if idle && elapsed > 30*time.Minute {
				hour := time.Now().Hour()
				if hour >= 22 || hour < 6 {
					presenceManager.applyPresence(PresenceSleeping, morphName+" is resting", "the room has gone quiet", PresenceAnchorDesk, false)
					continue
				}
			}

			if idle && elapsed > 90*time.Second {
				statusText, detailText, anchor := idlePresenceSnapshot(morphName, preferredAnchor, time.Now())
				presenceManager.applyPresence(PresenceIdle, statusText, detailText, anchor, false)
			}
		}
	}
}

// Stop shuts down the idle watcher.
func (presenceManager *PresenceManager) Stop() {
	presenceManager.stopOnce.Do(func() {
		close(presenceManager.stopCh)
	})
}

func startsWith(s string, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func (presenceManager *PresenceManager) applyPresence(state PresenceState, statusText string, detailText string, anchor PresenceAnchor, markActivity bool) {
	normalizedAnchor := normalizePresenceAnchor(anchor)
	trimmedDetail := strings.TrimSpace(detailText)

	presenceManager.mu.Lock()
	changed := presenceManager.state != state ||
		presenceManager.statusText != statusText ||
		presenceManager.detailText != trimmedDetail ||
		presenceManager.anchor != normalizedAnchor
	presenceManager.state = state
	presenceManager.statusText = statusText
	presenceManager.detailText = trimmedDetail
	presenceManager.anchor = normalizedAnchor
	if normalizedAnchor != PresenceAnchorDesk || trimmedDetail != "" {
		presenceManager.lastFocusAnchor = normalizedAnchor
	}
	if markActivity {
		presenceManager.lastActivity = time.Now()
	} else if presenceManager.lastActivity.IsZero() {
		presenceManager.lastActivity = time.Now()
	}
	presenceManager.mu.Unlock()

	if changed && presenceManager.emitter != nil {
		presenceManager.emitter.Emit("haven:presence_changed", map[string]interface{}{
			"state":       string(state),
			"status_text": statusText,
			"detail_text": trimmedDetail,
			"anchor":      string(normalizedAnchor),
		})
	}
	if changed {
		presenceManager.persistPresenceSnapshot(PresenceResponse{
			State:      string(state),
			StatusText: statusText,
			DetailText: trimmedDetail,
			Anchor:     string(normalizedAnchor),
		})
	}
}

func (presenceManager *PresenceManager) persistPresenceSnapshot(snapshot PresenceResponse) {
	if presenceManager.snapshotPath == "" {
		return
	}
	jsonBytes, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return
	}
	if len(jsonBytes) == 0 || jsonBytes[len(jsonBytes)-1] != '\n' {
		jsonBytes = append(jsonBytes, '\n')
	}
	if err := os.MkdirAll(filepath.Dir(presenceManager.snapshotPath), 0o700); err != nil {
		return
	}
	tempPath := presenceManager.snapshotPath + ".tmp"
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	if _, err := file.Write(jsonBytes); err != nil {
		_ = file.Close()
		return
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return
	}
	if err := file.Close(); err != nil {
		return
	}
	if err := os.Rename(tempPath, presenceManager.snapshotPath); err != nil {
		return
	}
	if dir, err := os.Open(filepath.Dir(presenceManager.snapshotPath)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	_ = os.Chmod(presenceManager.snapshotPath, 0o600)
}

func normalizePresenceAnchor(anchor PresenceAnchor) PresenceAnchor {
	if anchor == "" {
		return PresenceAnchorDesk
	}
	return anchor
}

func presenceContextForCapability(morphName string, capability string, arguments map[string]string) (PresenceState, string, string, PresenceAnchor) {
	sandboxPath := strings.TrimSpace(arguments["path"])
	commandText := strings.TrimSpace(arguments["command"])
	presencePath := compactPresencePath(mapSandboxPathToHaven(sandboxPath))

	switch {
	case startsWith(capability, "fs_read"):
		if sandboxPath == "" {
			return PresenceReading, morphName + " is reading", "looking through Haven files", PresenceAnchorWorkspace
		}
		switch presenceAnchorForSandboxPath(sandboxPath) {
		case PresenceAnchorJournal:
			return PresenceReading, morphName + " is reading the journal", presencePath, PresenceAnchorJournal
		case PresenceAnchorPaint:
			return PresenceReading, morphName + " is checking the gallery", presencePath, PresenceAnchorPaint
		default:
			return PresenceReading, morphName + " is reading a file", presencePath, PresenceAnchorWorkspace
		}
	case startsWith(capability, "fs_write"):
		switch presenceAnchorForSandboxPath(sandboxPath) {
		case PresenceAnchorJournal:
			return PresenceCreating, morphName + " is writing in the journal", presencePath, PresenceAnchorJournal
		case PresenceAnchorPaint:
			return PresenceCreating, morphName + " is saving a painting", presencePath, PresenceAnchorPaint
		default:
			return PresenceWorking, morphName + " is changing the workspace", presencePath, PresenceAnchorWorkspace
		}
	case startsWith(capability, "fs_list"):
		switch presenceAnchorForSandboxPath(sandboxPath) {
		case PresenceAnchorJournal:
			return PresenceReading, morphName + " is looking through the journal", presencePathOrFallback(presencePath, "checking old pages"), PresenceAnchorJournal
		case PresenceAnchorPaint:
			return PresenceReading, morphName + " is browsing the gallery", presencePathOrFallback(presencePath, "checking recent sketches"), PresenceAnchorPaint
		default:
			return PresenceReading, morphName + " is browsing the workspace", presencePathOrFallback(presencePath, "checking the folders"), PresenceAnchorWorkspace
		}
	case startsWith(capability, "paint.") || startsWith(capability, "paint_"):
		if startsWith(capability, "paint.save") {
			return PresenceCreating, morphName + " is saving a painting", "working at the paint table", PresenceAnchorPaint
		}
		if startsWith(capability, "paint.list") {
			return PresenceReading, morphName + " is browsing the gallery", "checking recent sketches", PresenceAnchorPaint
		}
		return PresenceCreating, morphName + " is painting", "working at the paint table", PresenceAnchorPaint
	case startsWith(capability, "memory.remember"):
		return PresenceWorking, morphName + " is tucking something away", "adding it to durable continuity", PresenceAnchorDesk
	case startsWith(capability, "todo.list"):
		return PresenceReading, morphName + " is checking the carry-over list", "reviewing what is still open", PresenceAnchorTodo
	case startsWith(capability, "todo.add"):
		return PresenceWorking, morphName + " is adding something to carry-over", "keeping it visible for later", PresenceAnchorTodo
	case startsWith(capability, "todo.complete"):
		return PresenceWorking, morphName + " is checking something off", "closing out an open item", PresenceAnchorTodo
	case startsWith(capability, "browser_"):
		return PresenceReading, morphName + " is researching", "following a thread through the web", PresenceAnchorMorph
	case startsWith(capability, "shell_exec"):
		return PresenceWorking, morphName + " is running something small", compactCommand(commandText), PresenceAnchorWorkspace
	case startsWith(capability, "spawn_helper"):
		return PresenceWorking, morphName + " is coordinating help", "keeping an eye on the activity monitor", PresenceAnchorActivity
	default:
		return PresenceWorking, morphName + " is working", "moving through Haven", PresenceAnchorMorph
	}
}

func completionPresenceContext(morphName string, completedWork *completedWorkTracker) (string, string, PresenceAnchor) {
	if completedWork == nil || len(completedWork.writtenSandboxPaths) == 0 {
		return morphName + " finished! Take a look.", "ready when you are", PresenceAnchorMorph
	}

	firstPath := completedWork.writtenSandboxPaths[0]
	havenPath := compactPresencePath(mapSandboxPathToHaven(firstPath))
	switch presenceAnchorForSandboxPath(firstPath) {
	case PresenceAnchorPaint:
		return morphName + " finished a painting", havenPath, PresenceAnchorPaint
	case PresenceAnchorJournal:
		return morphName + " left something in the journal", havenPath, PresenceAnchorJournal
	default:
		if len(completedWork.writtenSandboxPaths) == 1 {
			return morphName + " finished something for you", havenPath, PresenceAnchorWorkspace
		}
		return morphName + " finished a few things", fmt.Sprintf("%s and %d more", havenPath, len(completedWork.writtenSandboxPaths)-1), PresenceAnchorWorkspace
	}
}

func idlePresenceSnapshot(morphName string, preferredAnchor PresenceAnchor, now time.Time) (string, string, PresenceAnchor) {
	hour := now.Hour()
	switch {
	case hour >= 5 && hour < 9:
		if preferredAnchor == PresenceAnchorTodo {
			return morphName + " is easing into the morning", idleDetailForAnchor(preferredAnchor), preferredAnchor
		}
		return morphName + " is easing into the morning", "keeping an eye on the desk", PresenceAnchorDesk
	case hour >= 9 && hour < 17:
		if preferredAnchor == PresenceAnchorWorkspace || preferredAnchor == PresenceAnchorPaint || preferredAnchor == PresenceAnchorTodo || preferredAnchor == PresenceAnchorActivity || preferredAnchor == PresenceAnchorJournal {
			return morphName + " is keeping quiet company", idleDetailForAnchor(preferredAnchor), preferredAnchor
		}
		return morphName + " is puttering around Haven", "staying close to the workspace", PresenceAnchorWorkspace
	case hour >= 17 && hour < 22:
		if preferredAnchor == PresenceAnchorWorkspace || preferredAnchor == PresenceAnchorPaint || preferredAnchor == PresenceAnchorTodo || preferredAnchor == PresenceAnchorJournal {
			return morphName + " is winding down", idleDetailForAnchor(preferredAnchor), preferredAnchor
		}
		return morphName + " is settling into the evening", "keeping the desk warm", PresenceAnchorDesk
	default:
		return morphName + " is in the quiet hours", "keeping the room calm", PresenceAnchorDesk
	}
}

func idleDetailForAnchor(anchor PresenceAnchor) string {
	switch anchor {
	case PresenceAnchorWorkspace:
		return "staying close to recent work"
	case PresenceAnchorTodo:
		return "keeping today's carry-over in view"
	case PresenceAnchorJournal:
		return "lingering near the journal"
	case PresenceAnchorPaint:
		return "letting the gallery settle"
	case PresenceAnchorLoopgate:
		return "keeping one eye on Security"
	case PresenceAnchorActivity:
		return "watching the activity monitor"
	default:
		return "keeping an eye on the desk"
	}
}

func presenceAnchorForSandboxPath(sandboxPath string) PresenceAnchor {
	switch {
	case strings.HasPrefix(sandboxPath, "scratch/journal/"):
		return PresenceAnchorJournal
	case strings.HasPrefix(sandboxPath, "outputs/paintings/"):
		return PresenceAnchorPaint
	default:
		return PresenceAnchorWorkspace
	}
}

func compactPresencePath(path string) string {
	cleanedPath := strings.TrimSpace(path)
	if cleanedPath == "" {
		return ""
	}
	pathParts := strings.Split(filepath.ToSlash(cleanedPath), "/")
	if len(pathParts) <= 3 {
		return cleanedPath
	}
	return pathParts[0] + "/.../" + pathParts[len(pathParts)-1]
}

func presencePathOrFallback(path string, fallback string) string {
	if strings.TrimSpace(path) == "" {
		return fallback
	}
	return path
}

func compactCommand(commandText string) string {
	trimmedCommand := strings.TrimSpace(commandText)
	if trimmedCommand == "" {
		return "working in the workspace"
	}
	if len(trimmedCommand) > 48 {
		return trimmedCommand[:45] + "..."
	}
	return trimmedCommand
}

func trimPresenceDetail(rawDetail string) string {
	trimmedDetail := strings.Join(strings.Fields(strings.TrimSpace(rawDetail)), " ")
	if len(trimmedDetail) <= 80 {
		return trimmedDetail
	}
	return trimmedDetail[:77] + "..."
}
