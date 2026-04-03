package main

import (
	"context"
	"sync"
	"time"

	"morph/internal/config"
	"morph/internal/haven/threadstore"
	"morph/internal/loopgate"
	"morph/internal/tools"
)

// HavenApp is the core application for Haven Messenger.
// It manages threads, chat execution, and Loopgate integration.
type HavenApp struct {
	loopgateClient loopgate.ControlPlaneClient
	threadStore    *threadstore.Store
	toolRegistry   *tools.Registry
	persona        config.Persona
	policy         config.Policy
	capabilities   []loopgate.CapabilitySummary
	emitter        EventEmitter
	wailsCtx       context.Context
	originalsDir   string // directory for storing original imported file versions
	repoRoot       string // absolute repo root for preferences and state
	sandboxHome    string // Morph's sandbox home (virtual filesystem root)
	sessionID      string // session identifier for Loopgate audit/continuity
	wakeState      string // cached wake-state from Loopgate memory
	wakeSnapshot   loopgate.MemoryWakeStateResponse
	wakeDiagnostic loopgate.MemoryDiagnosticWakeResponse
	presence       *PresenceManager // Morph presence state machine
	idleManager    *IdleManager     // idle behavior system
	folderSync     *FolderSyncManager

	executionsMu   sync.Mutex
	executions     map[string]*threadExecution
	deskNotesMu    sync.Mutex
	memoryMu       sync.RWMutex
	folderAccessMu sync.RWMutex
	folderAccess   loopgate.FolderAccessStatusResponse
	shutdownOnce   sync.Once
}

// SetWailsContext stores the Wails runtime context, needed for native dialogs.
func (app *HavenApp) SetWailsContext(ctx context.Context) {
	app.wailsCtx = ctx
}

// NewHavenApp creates a new HavenApp with the given dependencies.
func NewHavenApp(
	loopgateClient loopgate.ControlPlaneClient,
	threadStore *threadstore.Store,
	toolRegistry *tools.Registry,
	persona config.Persona,
	policy config.Policy,
	capabilities []loopgate.CapabilitySummary,
	emitter EventEmitter,
	originalsDir string,
	repoRoot string,
) *HavenApp {
	return &HavenApp{
		loopgateClient: loopgateClient,
		threadStore:    threadStore,
		toolRegistry:   toolRegistry,
		persona:        persona,
		policy:         policy,
		capabilities:   capabilities,
		emitter:        emitter,
		originalsDir:   originalsDir,
		repoRoot:       repoRoot,
		executions:     make(map[string]*threadExecution),
	}
}

// getOrCreateExecution returns the threadExecution for the given thread,
// creating one in idle state if it doesn't exist.
func (app *HavenApp) getOrCreateExecution(threadID string) *threadExecution {
	app.executionsMu.Lock()
	defer app.executionsMu.Unlock()

	exec, ok := app.executions[threadID]
	if !ok {
		exec = &threadExecution{
			state: threadstore.ExecutionIdle,
		}
		app.executions[threadID] = exec
	}
	return exec
}

type idleConnectionsCloser interface {
	CloseIdleConnections()
}

// Shutdown stops background Haven managers, cancels in-flight executions, and
// releases idle Loopgate client connections. It is safe to call multiple times.
func (app *HavenApp) Shutdown() {
	app.shutdownOnce.Do(func() {
		if app.idleManager != nil {
			app.idleManager.Stop()
		}
		if app.folderSync != nil {
			app.folderSync.Stop()
		}

		doneChannels := app.cancelActiveExecutions()
		waitForExecutions(doneChannels, 2*time.Second)

		if app.presence != nil {
			app.presence.Stop()
		}

		if clientCloser, ok := app.loopgateClient.(idleConnectionsCloser); ok {
			clientCloser.CloseIdleConnections()
		}
	})
}

func (app *HavenApp) cancelActiveExecutions() []chan struct{} {
	app.executionsMu.Lock()
	executionSnapshots := make([]*threadExecution, 0, len(app.executions))
	for _, execution := range app.executions {
		executionSnapshots = append(executionSnapshots, execution)
	}
	app.executionsMu.Unlock()

	cancelFunctions := make([]context.CancelFunc, 0, len(executionSnapshots))
	doneChannels := make([]chan struct{}, 0, len(executionSnapshots))
	for _, execution := range executionSnapshots {
		execution.mu.Lock()
		if execution.state == threadstore.ExecutionRunning || execution.state == threadstore.ExecutionWaitingForApproval {
			if execution.cancelFn != nil {
				cancelFunctions = append(cancelFunctions, execution.cancelFn)
			}
			if execution.doneCh != nil {
				doneChannels = append(doneChannels, execution.doneCh)
			}
		}
		execution.mu.Unlock()
	}

	for _, cancelFunction := range cancelFunctions {
		cancelFunction()
	}

	return doneChannels
}

func waitForExecutions(doneChannels []chan struct{}, timeout time.Duration) {
	if len(doneChannels) == 0 {
		return
	}

	deadline := time.Now().Add(timeout)
	for _, doneChannel := range doneChannels {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return
		}
		select {
		case <-doneChannel:
		case <-time.After(remaining):
			return
		}
	}
}
