package main

import (
	"context"
	"strings"
	"sync"

	"morph/internal/haven/threadstore"
)

// maxToolIterations is the hard limit on tool-loop iterations per SendMessage.
// After this many rounds of model→tool, the loop terminates with a failure.
// Some OpenAI-compatible models (e.g. certain Kimi endpoints) need more rounds
// before they emit a final non-tool reply; keep this high enough to be useful
// while chat.go still detects repeated identical tool batches and stops early.
const maxToolIterations = 24

// maxConsecutiveSingleCapabilityRounds stops models that call the same lone capability
// over and over with different arguments (fingerprint-based detection only catches
// identical argument maps). Some Kimi deployments loop on paint.save / fs_write this way.
const maxConsecutiveSingleCapabilityRounds = 5

// maxConsecutiveStructuredValidationBatches stops models (notably some Kimi endpoints)
// that emit the same invalid native tool name repeatedly (e.g. "list" instead of
// fs_list or host.folder.list). The validation-only retry path does not execute tools,
// so the usual fingerprint / single-capability loop detectors never fire without this.
const maxConsecutiveStructuredValidationBatches = 4

// useCompactNativeTools sends a single native tool definition (invoke_capability) and
// expands to real capability names in the orchestrator before Loopgate execution.
const useCompactNativeTools = true

// EventEmitter sends events to the frontend (UI).
// In production this wraps Wails runtime.EventsEmit;
// in tests it can be a recording mock.
type EventEmitter interface {
	Emit(eventName string, data interface{})
}

// approvalDecision carries the user's decision from DecideApproval
// to the blocked chat loop goroutine.
type approvalDecision struct {
	Approved bool
}

// threadExecution tracks the per-thread execution state.
//
// Invariant: at most one active execution per thread.
// Invariant: at most one pending approval per thread.
type threadExecution struct {
	mu                   sync.Mutex
	state                threadstore.ExecutionState
	cancelFn             context.CancelFunc
	pendingApprovalID    string
	approvalCh           chan approvalDecision
	doneCh               chan struct{} // closed when execution goroutine fully completes
	agentWorkItemID      string
	agentWorkKind        string // e.g. havenAgentWorkKindHostFolderOrganize — empty when no tracked agent work
	agentWorkItemClosed  bool   // true after successful work-item/complete for this run
}

// resetAgentWorkTrackingLocked clears per-run agent work state. Caller must hold exec.mu.
func (exec *threadExecution) resetAgentWorkTrackingLocked() {
	if exec == nil {
		return
	}
	exec.agentWorkItemID = ""
	exec.agentWorkKind = ""
	exec.agentWorkItemClosed = false
}

func (exec *threadExecution) setAgentWorkTracking(itemID, kind string) {
	if exec == nil {
		return
	}
	exec.mu.Lock()
	defer exec.mu.Unlock()
	exec.agentWorkItemID = strings.TrimSpace(itemID)
	exec.agentWorkKind = strings.TrimSpace(kind)
	exec.agentWorkItemClosed = false
}

func (exec *threadExecution) markAgentWorkItemClosed() {
	if exec == nil {
		return
	}
	exec.mu.Lock()
	defer exec.mu.Unlock()
	exec.agentWorkItemClosed = true
}

func (exec *threadExecution) snapshotAgentWork() (itemID, kind string, closed bool) {
	if exec == nil {
		return "", "", false
	}
	exec.mu.Lock()
	defer exec.mu.Unlock()
	return exec.agentWorkItemID, exec.agentWorkKind, exec.agentWorkItemClosed
}

// ChatResponse is returned by SendMessage to acknowledge the request.
type ChatResponse struct {
	ThreadID string `json:"thread_id"`
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty"`
}
