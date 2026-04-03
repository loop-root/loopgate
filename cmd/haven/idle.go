package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"time"
)

// IdleBehavior represents a possible idle activity with weighted selection.
type IdleBehavior struct {
	Name        string
	Class       IdleBehaviorClass
	Description string        // Plain English for presence: "Morph is {Description}"
	Capability  string        // Loopgate capability needed (empty = always available)
	Weight      int           // Relative weight for random selection
	Presence    PresenceState // Presence state during execution
	DeskNote    func(morphName string) *DeskNoteDraft
	Execute     func(ctx context.Context, app *HavenApp) error
}

type IdleBehaviorClass string

const (
	IdleBehaviorUtility IdleBehaviorClass = "utility"
	IdleBehaviorAmbient IdleBehaviorClass = "ambient"
)

// IdleManager watches for idle periods and triggers autonomous Morph behavior.
type IdleManager struct {
	app            *HavenApp
	mu             sync.Mutex
	lastActivity   time.Time
	running        bool
	enabled        bool // whether resident behavior is active (toggled via settings)
	ambientEnabled bool
	stopCh         chan struct{}
	idleCancel     context.CancelFunc // cancel function for the active idle action

	// Configuration
	idleThreshold     time.Duration // How long before resident behavior becomes eligible
	utilityCooldown   time.Duration // Minimum time between utility actions
	ambientCooldown   time.Duration // Minimum time between ambient actions
	lastUtilityAction time.Time
	lastAmbientAction time.Time
	stopOnce          sync.Once
}

// NewIdleManager creates a new idle behavior manager.
func NewIdleManager(app *HavenApp) *IdleManager {
	im := &IdleManager{
		app:             app,
		lastActivity:    time.Now(),
		enabled:         true,
		ambientEnabled:  true,
		stopCh:          make(chan struct{}),
		idleThreshold:   3 * time.Minute,
		utilityCooldown: 4 * time.Minute,
		ambientCooldown: 15 * time.Minute,
	}
	go im.watch()
	return im
}

// NotifyActivity resets the idle timer and cancels any running idle action
// to free the Loopgate session for user-initiated work.
func (im *IdleManager) NotifyActivity() {
	im.mu.Lock()
	im.lastActivity = time.Now()
	cancelFn := im.idleCancel
	im.mu.Unlock()

	// Cancel running idle action so its ModelReply releases the session.
	if cancelFn != nil {
		cancelFn()
		// Wait briefly for the idle action to finish and release the session.
		for i := 0; i < 20; i++ {
			im.mu.Lock()
			stillRunning := im.running
			im.mu.Unlock()
			if !stillRunning {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// Stop shuts down the idle watcher.
func (im *IdleManager) Stop() {
	im.mu.Lock()
	cancelFn := im.idleCancel
	im.enabled = false
	im.mu.Unlock()

	if cancelFn != nil {
		cancelFn()
		for i := 0; i < 20; i++ {
			im.mu.Lock()
			stillRunning := im.running
			im.mu.Unlock()
			if !stillRunning {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	im.stopOnce.Do(func() {
		close(im.stopCh)
	})
}

// watch periodically checks if Morph has been idle long enough to trigger behavior.
func (im *IdleManager) watch() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-im.stopCh:
			return
		case <-ticker.C:
			// Refresh enabled/ambientEnabled from disk so settings UI changes
			// take effect without restarting the process.
			if saved := im.app.GetSettings(); true {
				im.mu.Lock()
				im.enabled = saved.IdleEnabled
				im.ambientEnabled = saved.AmbientEnabled
				im.mu.Unlock()
			}

			im.mu.Lock()
			elapsed := time.Since(im.lastActivity)
			running := im.running
			enabled := im.enabled
			im.mu.Unlock()

			if running || !enabled {
				continue
			}
			if elapsed < im.idleThreshold {
				continue
			}

			nextBehavior, ok := im.nextBehavior()
			if !ok {
				continue
			}
			if !im.cooldownReady(nextBehavior.Class) {
				continue
			}

			im.mu.Lock()
			im.running = true
			im.mu.Unlock()

			im.performIdleAction(nextBehavior)

			im.mu.Lock()
			im.running = false
			switch nextBehavior.Class {
			case IdleBehaviorUtility:
				im.lastUtilityAction = time.Now()
			case IdleBehaviorAmbient:
				im.lastAmbientAction = time.Now()
			}
			im.mu.Unlock()
		}
	}
}

func (im *IdleManager) cooldownReady(behaviorClass IdleBehaviorClass) bool {
	im.mu.Lock()
	defer im.mu.Unlock()

	switch behaviorClass {
	case IdleBehaviorUtility:
		return time.Since(im.lastUtilityAction) >= im.utilityCooldown
	case IdleBehaviorAmbient:
		return time.Since(im.lastAmbientAction) >= im.ambientCooldown
	default:
		return false
	}
}

func (im *IdleManager) nextBehavior() (IdleBehavior, bool) {
	im.app.RefreshWakeState()
	wakeStateSnapshot := im.app.currentWakeSnapshot()
	if countActionableWakeTasks(wakeStateSnapshot, time.Now().UTC()) > 0 || len(wakeStateSnapshot.ActiveGoals) > 0 {
		return carryForwardIdleBehavior, true
	}

	if len(wakeStateSnapshot.RecentFacts) > 0 && im.cooldownReady(IdleBehaviorUtility) {
		return reviewMemoryIdleBehavior, true
	}

	folderStatus := im.app.currentFolderAccessStatus()
	for _, folder := range folderStatus.Folders {
		if folder.ID == "downloads" && folder.Granted && folder.MirrorReady && folder.EntryCount > 0 {
			if im.cooldownReady(IdleBehaviorUtility) {
				return checkDownloadsIdleBehavior, true
			}
			break
		}
	}
	for _, folder := range folderStatus.Folders {
		if folder.Granted && folder.MirrorReady && folder.EntryCount > 0 {
			if im.cooldownReady(IdleBehaviorUtility) {
				return exploreWorkspaceIdleBehavior, true
			}
			break
		}
	}

	im.mu.Lock()
	ambientEnabled := im.ambientEnabled
	im.mu.Unlock()
	if !ambientEnabled {
		return IdleBehavior{}, false
	}

	behaviors := im.availableAmbientBehaviors()
	if len(behaviors) == 0 {
		return IdleBehavior{}, false
	}
	return weightedRandom(behaviors), true
}

// performIdleAction executes the selected resident behavior.
func (im *IdleManager) performIdleAction(behavior IdleBehavior) {
	name := im.morphName()

	// Update presence with behavior-specific state.
	if im.app.presence != nil {
		im.app.presence.SetPresenceWithContext(
			behavior.Presence,
			name+" is "+behavior.Description,
			idleBehaviorDetail(behavior.Name),
			idleBehaviorAnchor(behavior.Name),
		)
	}

	fmt.Fprintf(os.Stderr, "haven: idle behavior: %s\n", behavior.Name)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)

	im.mu.Lock()
	im.idleCancel = cancel
	im.mu.Unlock()

	err := behavior.Execute(ctx, im.app)
	cancel()

	im.mu.Lock()
	im.idleCancel = nil
	im.mu.Unlock()

	if err != nil {
		fmt.Fprintf(os.Stderr, "haven: idle behavior %s failed: %v\n", behavior.Name, err)
	} else {
		if behavior.DeskNote != nil {
			deskNoteDraft := behavior.DeskNote(name)
			if deskNoteDraft != nil {
				if _, noteErr := im.app.createDeskNote(*deskNoteDraft); noteErr != nil {
					fmt.Fprintf(os.Stderr, "haven: idle behavior %s note failed: %v\n", behavior.Name, noteErr)
				}
			}
		}
		im.app.EmitToast(name, name+" is "+behavior.Description, "info")
	}

	// Return to time-aware idle.
	if im.app.presence != nil {
		im.app.presence.NotifyIdle()
	}
}

// morphName returns the current Morph name from the presence manager.
func (im *IdleManager) morphName() string {
	if im.app.presence != nil {
		im.app.presence.mu.Lock()
		name := im.app.presence.morphName
		im.app.presence.mu.Unlock()
		return name
	}
	return "Morph"
}

// availableAmbientBehaviors returns optional self-directed behaviors that can be
// executed only when Haven is quiet and there is no carry-over work to pick up.
func (im *IdleManager) availableAmbientBehaviors() []IdleBehavior {
	capSet := make(map[string]bool)
	for _, cap := range im.app.capabilities {
		capSet[cap.Name] = true
	}

	var available []IdleBehavior
	for _, b := range ambientIdleBehaviors {
		if b.Capability == "" || capSet[b.Capability] {
			available = append(available, b)
		}
	}
	return available
}

var carryForwardIdleBehavior = IdleBehavior{
	Name:        "carry_forward",
	Class:       IdleBehaviorUtility,
	Description: "checking the carry-over list",
	Capability:  "",
	Weight:      1,
	Presence:    PresenceWorking,
	Execute:     idleCarryForward,
}

var reviewMemoryIdleBehavior = IdleBehavior{
	Name:        "review_memory",
	Class:       IdleBehaviorUtility,
	Description: "reviewing what it remembers",
	Capability:  "",
	Weight:      1,
	Presence:    PresenceWorking,
	Execute:     idleReviewMemory,
}

var exploreWorkspaceIdleBehavior = IdleBehavior{
	Name:        "explore_workspace",
	Class:       IdleBehaviorUtility,
	Description: "noticing shared folder updates",
	Capability:  "",
	Weight:      1,
	Presence:    PresenceWorking,
	Execute:     idleExploreWorkspace,
}

var checkDownloadsIdleBehavior = IdleBehavior{
	Name:        "check_downloads",
	Class:       IdleBehaviorUtility,
	Description: "checking your Downloads folder",
	Capability:  "",
	Weight:      1,
	Presence:    PresenceWorking,
	Execute:     idleCheckDownloads,
}

// ambientIdleBehaviors are self-directed optional actions reserved for quiet
// moments after Haven has already checked for unfinished work.
var ambientIdleBehaviors = []IdleBehavior{
	{
		Name:        "journal",
		Class:       IdleBehaviorAmbient,
		Description: "journaling",
		Capability:  "fs_write",
		Weight:      3,
		Presence:    PresenceCreating,
		DeskNote: func(_ string) *DeskNoteDraft {
			return &DeskNoteDraft{
				Kind:  "update",
				Title: "While you were away",
				Body:  "I spent a little quiet time journaling.",
			}
		},
		Execute: idleJournal,
	},
	{
		Name:        "create",
		Class:       IdleBehaviorAmbient,
		Description: "making something",
		Capability:  "fs_write",
		Weight:      2,
		Presence:    PresenceCreating,
		DeskNote: func(_ string) *DeskNoteDraft {
			return &DeskNoteDraft{
				Kind:  "update",
				Title: "While you were away",
				Body:  "I made something new. Open Workspace when you're curious.",
			}
		},
		Execute: idleCreate,
	},
}

// weightedRandom selects a behavior using weighted probability.
func weightedRandom(behaviors []IdleBehavior) IdleBehavior {
	total := 0
	for _, b := range behaviors {
		total += b.Weight
	}
	r := rand.Intn(total)
	for _, b := range behaviors {
		r -= b.Weight
		if r < 0 {
			return b
		}
	}
	return behaviors[len(behaviors)-1]
}

func idleBehaviorAnchor(behaviorName string) PresenceAnchor {
	switch behaviorName {
	case "carry_forward":
		return PresenceAnchorTodo
	case "review_memory":
		return PresenceAnchorDesk
	case "explore_workspace":
		return PresenceAnchorWorkspace
	case "check_downloads":
		return PresenceAnchorWorkspace
	case "journal":
		return PresenceAnchorJournal
	case "create":
		return PresenceAnchorPaint
	default:
		return PresenceAnchorDesk
	}
}

func idleBehaviorDetail(behaviorName string) string {
	switch behaviorName {
	case "carry_forward":
		return "checking what is still open"
	case "review_memory":
		return "thinking about what to keep in continuity"
	case "explore_workspace":
		return "noticing what landed in shared folders"
	case "check_downloads":
		return "glancing at Downloads clutter"
	case "journal":
		return "leaving a quiet page behind"
	case "create":
		return "making a small artifact for later"
	default:
		return ""
	}
}
