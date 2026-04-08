package loopgate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// =============================================================================
// TaskPlan / TaskLease / TaskExecution — Minimal Vertical Slice
//
// This file defines the core types and state machine for the
// TaskPlan → validation → lease → mediated execution → staged result flow.
//
// What this slice proves:
//   - Morph can submit a plan and have it validated against capability policy
//   - Loopgate issues a single-use lease binding a logical morphling identity
//   - The morphling invokes mediated execution through Loopgate (not direct)
//   - Loopgate validates lease/capability/args and calls the provider internally
//   - Loopgate stages provider output (morphling output is untrusted)
//   - The morphling finalizes lease consumption via /v1/task/complete
//
// What this slice does NOT prove:
//   - Full morphling process isolation (morphling is a logical identity only)
//   - Real external provider integration (echo provider is local/fake; in-tree MCP removed — ADR 0010)
//   - Multi-step plan execution (Steps[] exists but only step 0 is used)
//   - Durable persistence across server restarts (in-memory only)
//   - Lease revocation or plan cancellation
//
// Lock ownership and ordering:
//   - server.mu protects auth/session/token/approval/policy state
//   - taskPlansMu protects taskPlans, taskLeases, and taskExecutions maps
//   - auditMu protects the append-only audit ledger
//   - Acquisition order: server.mu → (release) → taskPlansMu → (release) → auditMu
//   - server.mu and taskPlansMu are NEVER held simultaneously; authentication
//     completes and releases server.mu before taskPlansMu is acquired
//   - taskPlansMu and auditMu are NEVER held simultaneously; state transitions
//     complete and release taskPlansMu before logEvent acquires auditMu
// =============================================================================

// --- Plan states ---
// submitted → validated | denied
// validated → lease_issued
// lease_issued → executing
// executing → completed | failed
const (
	taskPlanStateSubmitted   = "submitted"
	taskPlanStateValidated   = "validated"
	taskPlanStateDenied      = "denied"
	taskPlanStateLeaseIssued = "lease_issued"
	taskPlanStateExecuting   = "executing"
	taskPlanStateCompleted   = "completed"
	taskPlanStateFailed      = "failed"
)

// --- Lease states ---
// issued → executing → consumed | expired
const (
	taskLeaseStateIssued    = "issued"
	taskLeaseStateExecuting = "executing"
	taskLeaseStateConsumed  = "consumed"
	taskLeaseStateExpired   = "expired"
)

const taskLeaseTTL = 2 * time.Minute

const (
	taskPlanArtifactRefPrefix = "taskplan://artifacts/"
	taskPlanStagingRefPrefix  = "taskplan://staging/"
)

// knownTaskCapabilities is the registry of capabilities available for task plans.
// Fail-closed: capabilities not listed here are denied at validation time.
var knownTaskCapabilities = map[string]bool{
	"echo.generate_summary": true,
}

// --- Core types ---

// taskPlanRecord represents a validated task plan submitted by Morph.
// Plan state tracks the lifecycle from submission through completion.
// Execution-specific state is kept in taskExecutionRecord.
type taskPlanRecord struct {
	PlanID         string
	SessionID      string
	ActorLabel     string
	GoalText       string
	Steps          []TaskPlanStep
	CanonicalHash  string
	State          string
	DenialReason   string
	LeaseID        string // set when lease is issued
	ExecutionID    string // set when execution begins
	CreatedAtUTC   time.Time
	ValidatedAtUTC *time.Time
	CompletedAtUTC *time.Time
}

// TaskPlanStep represents a single step in a task plan.
type TaskPlanStep struct {
	StepIndex  int               `json:"step_index"`
	Capability string            `json:"capability"`
	Arguments  map[string]string `json:"arguments"`
}

// taskLeaseRecord represents a single-use lease issued by Loopgate for executing
// a plan step. The lease binds the exact approved capability and arguments from the
// plan — callers cannot override these at execution time.
//
// The morphling_id is a logical identity (not a process ID). This slice does not
// implement morphling process isolation; the morphling_id exists to establish the
// authorization binding pattern that will map onto real isolation later.
type taskLeaseRecord struct {
	LeaseID      string
	PlanID       string
	PlanHash     string // must match plan's canonical hash at issuance
	StepIndex    int
	MorphlingID  string
	Capability   string
	Arguments    map[string]string
	StagingDir   string
	State        string
	ExpiresAtUTC time.Time
	CreatedAtUTC time.Time
}

// taskExecutionRecord tracks runtime execution state for a single lease execution.
// This is kept separate from taskPlanRecord so plan lifecycle state and execution
// runtime state are not overloaded onto the same struct.
type taskExecutionRecord struct {
	ExecutionID    string
	LeaseID        string
	PlanID         string
	Capability     string
	State          string // "running" | "succeeded" | "failed"
	ProviderOutput json.RawMessage
	OutputHash     string // SHA256 of ProviderOutput bytes
	ArtifactRef    string
	StartedAtUTC   time.Time
	CompletedAtUTC *time.Time
	ErrorMessage   string
}

func taskPlanArtifactRef(planID string, leaseID string) string {
	return taskPlanArtifactRefPrefix + planID + "/" + leaseID + "/result.json"
}

func taskPlanStagingRef(planID string, leaseID string) string {
	return taskPlanStagingRefPrefix + planID + "/" + leaseID
}

// TaskStepResult is the structured envelope for a completed step result.
// OutputData is canonical JSON from the provider, stored as raw bytes to
// preserve deterministic hashing and avoid re-serialization drift.
type TaskStepResult struct {
	StepIndex    int             `json:"step_index"`
	Capability   string          `json:"capability"`
	ProviderName string          `json:"provider_name"`
	OutputData   json.RawMessage `json:"output_data"`
	OutputHash   string          `json:"output_hash"`
}

// EchoProviderOutput is the typed output envelope for echo.generate_summary.
type EchoProviderOutput struct {
	Summary     string `json:"summary"`
	InputLength int    `json:"input_length"`
	Provider    string `json:"provider"`
}

// --- Canonical hash ---

// computeCanonicalHash computes a deterministic SHA256 hash of goal text and steps.
// Steps are sorted by step_index; argument keys are sorted by json.Marshal's default
// behavior (Go maps are serialized with sorted keys).
func computeCanonicalHash(goalText string, steps []TaskPlanStep) string {
	type canonicalStep struct {
		StepIndex  int               `json:"step_index"`
		Capability string            `json:"capability"`
		Arguments  map[string]string `json:"arguments"`
	}

	sortedSteps := make([]canonicalStep, len(steps))
	for i, step := range steps {
		args := make(map[string]string, len(step.Arguments))
		for k, v := range step.Arguments {
			args[k] = v
		}
		sortedSteps[i] = canonicalStep{
			StepIndex:  step.StepIndex,
			Capability: step.Capability,
			Arguments:  args,
		}
	}
	sort.Slice(sortedSteps, func(i, j int) bool {
		return sortedSteps[i].StepIndex < sortedSteps[j].StepIndex
	})

	payload := struct {
		GoalText string          `json:"goal_text"`
		Steps    []canonicalStep `json:"steps"`
	}{
		GoalText: goalText,
		Steps:    sortedSteps,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(payloadBytes)
	return hex.EncodeToString(hash[:])
}

// --- State machine ---

func validTaskPlanTransition(from, to string) bool {
	switch from {
	case taskPlanStateSubmitted:
		return to == taskPlanStateValidated || to == taskPlanStateDenied
	case taskPlanStateValidated:
		return to == taskPlanStateLeaseIssued
	case taskPlanStateLeaseIssued:
		return to == taskPlanStateExecuting
	case taskPlanStateExecuting:
		return to == taskPlanStateCompleted || to == taskPlanStateFailed
	default:
		return false
	}
}

func validTaskLeaseTransition(from, to string) bool {
	switch from {
	case taskLeaseStateIssued:
		return to == taskLeaseStateExecuting || to == taskLeaseStateExpired
	case taskLeaseStateExecuting:
		return to == taskLeaseStateConsumed || to == taskLeaseStateExpired
	default:
		return false
	}
}

// transitionTaskPlanState validates and applies a plan state transition.
// Caller must hold taskPlansMu.
func transitionTaskPlanState(plan *taskPlanRecord, newState string) error {
	if !validTaskPlanTransition(plan.State, newState) {
		return fmt.Errorf("invalid plan state transition: %s → %s", plan.State, newState)
	}
	plan.State = newState
	return nil
}

// transitionTaskLeaseState validates and applies a lease state transition.
// Caller must hold taskPlansMu.
func transitionTaskLeaseState(lease *taskLeaseRecord, newState string) error {
	if !validTaskLeaseTransition(lease.State, newState) {
		return fmt.Errorf("invalid lease state transition: %s → %s", lease.State, newState)
	}
	lease.State = newState
	return nil
}
