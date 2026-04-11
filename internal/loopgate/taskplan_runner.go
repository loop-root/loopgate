package loopgate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// =============================================================================
// Morphling Runner — Minimal local-process executor for TaskPlan leases.
//
// The runner is a minimal task-plan execution helper that:
//   1. Receives a TaskPlanRunnerConfig (socket path, session credentials, lease details)
//   2. Calls POST /v1/task/execute (mediated capability call through Loopgate)
//   3. Calls POST /v1/task/complete (finalize lease consumption)
//   4. Exits cleanly with a result or error
//
// Important current constraint:
//   - This helper reuses delegated control-session credentials, so it only
//     works when subsequent requests preserve the same peer binding as the
//     original session. A distinct OS subprocess will be denied by peer
//     binding. Real cross-process morphling execution uses the dedicated
//     morphling worker launch/open flow instead.
//
// The runner does NOT:
//   - Execute providers directly (Loopgate mediates all capability execution)
//   - Supply capability or arguments to /execute (lease-bound)
//   - Stage artifacts (Loopgate stages provider output during /execute)
//   - Retry on failure (one-shot execution; parent handles recovery)
//
// This is a minimal vertical slice proving that a morphling can run as a
// separate local process. It does NOT implement:
//   - Real process isolation or sandboxing
//   - External provider integration (out-of-tree)
//   - Multi-step plan execution
//   - Lease renewal or session refresh
// =============================================================================

// TaskPlanRunnerConfig is the configuration passed to a morphling runner process.
// The parent process (Morph) obtains a lease from Loopgate and passes the
// resulting credentials and lease details to the runner via JSON on stdin.
type TaskPlanRunnerConfig struct {
	SocketPath       string `json:"socket_path"`
	ControlSessionID string `json:"control_session_id"`
	CapabilityToken  string `json:"capability_token"`
	ApprovalToken    string `json:"approval_token"`
	SessionMACKey    string `json:"session_mac_key"`
	SessionExpiresAt string `json:"session_expires_at"` // RFC3339Nano
	LeaseID          string `json:"lease_id"`
	MorphlingID      string `json:"morphling_id"`
	PlanID           string `json:"plan_id"`
}

// TaskPlanRunnerResult is the structured result from a morphling runner execution.
type TaskPlanRunnerResult struct {
	Status      string          `json:"status"` // "completed" | "failed"
	StepResult  *TaskStepResult `json:"step_result,omitempty"`
	ErrorReason string          `json:"error_reason,omitempty"`
}

// RunTaskPlanMorphling executes the morphling runner logic. This function is
// designed to be called from the same peer-bound process, or from a goroutine
// in integration tests. A distinct OS subprocess is expected to hit peer
// binding denial when using these delegated session credentials.
//
// The runner connects to Loopgate via the Unix socket, authenticates using the
// delegated session credentials, executes the lease-bound capability via
// /v1/task/execute, and finalizes via /v1/task/complete.
//
// Returns a structured result. The caller is responsible for process exit.
func RunTaskPlanMorphling(ctx context.Context, config TaskPlanRunnerConfig) TaskPlanRunnerResult {
	if err := validateRunnerConfig(config); err != nil {
		return TaskPlanRunnerResult{
			Status:      "failed",
			ErrorReason: fmt.Sprintf("invalid runner config: %v", err),
		}
	}

	expiresAt, err := time.Parse(time.RFC3339Nano, config.SessionExpiresAt)
	if err != nil {
		return TaskPlanRunnerResult{
			Status:      "failed",
			ErrorReason: fmt.Sprintf("parse session expiry: %v", err),
		}
	}

	client, err := NewClientFromDelegatedSession(config.SocketPath, DelegatedSessionConfig{
		ControlSessionID: config.ControlSessionID,
		CapabilityToken:  config.CapabilityToken,
		ApprovalToken:    config.ApprovalToken,
		SessionMACKey:    config.SessionMACKey,
		ExpiresAt:        expiresAt,
	})
	if err != nil {
		return TaskPlanRunnerResult{
			Status:      "failed",
			ErrorReason: fmt.Sprintf("create loopgate client: %v", err),
		}
	}

	// Phase 1: Execute the lease-bound capability via Loopgate mediation.
	// The runner does NOT supply capability or arguments — those are bound
	// by the lease from the validated plan.
	var executeResponse ExecuteTaskLeaseResponse
	err = client.doJSON(ctx, http.MethodPost, "/v1/task/execute", config.CapabilityToken,
		ExecuteTaskLeaseRequest{
			LeaseID:     config.LeaseID,
			MorphlingID: config.MorphlingID,
		}, &executeResponse, nil)
	if err != nil {
		return TaskPlanRunnerResult{
			Status:      "failed",
			ErrorReason: fmt.Sprintf("execute lease: %v", err),
		}
	}
	if executeResponse.Status != ResponseStatusSuccess {
		return TaskPlanRunnerResult{
			Status:      "failed",
			ErrorReason: fmt.Sprintf("execute denied: %s (%s)", executeResponse.DenialReason, executeResponse.DenialCode),
		}
	}

	// Phase 2: Finalize lease consumption.
	// Provider output was already staged by Loopgate during /execute.
	var completeResponse CompleteTaskLeaseResponse
	err = client.doJSON(ctx, http.MethodPost, "/v1/task/complete", config.CapabilityToken,
		CompleteTaskLeaseRequest{
			LeaseID:     config.LeaseID,
			MorphlingID: config.MorphlingID,
		}, &completeResponse, nil)
	if err != nil {
		return TaskPlanRunnerResult{
			Status:      "failed",
			ErrorReason: fmt.Sprintf("complete lease: %v", err),
		}
	}
	if completeResponse.Status != ResponseStatusSuccess {
		return TaskPlanRunnerResult{
			Status:      "failed",
			ErrorReason: fmt.Sprintf("complete denied: %s (%s)", completeResponse.DenialReason, completeResponse.DenialCode),
		}
	}

	return TaskPlanRunnerResult{
		Status:     "completed",
		StepResult: executeResponse.StepResult,
	}
}

func validateRunnerConfig(config TaskPlanRunnerConfig) error {
	if config.SocketPath == "" {
		return fmt.Errorf("missing socket_path")
	}
	if config.ControlSessionID == "" {
		return fmt.Errorf("missing control_session_id")
	}
	if config.CapabilityToken == "" {
		return fmt.Errorf("missing capability_token")
	}
	if config.ApprovalToken == "" {
		return fmt.Errorf("missing approval_token")
	}
	if config.SessionMACKey == "" {
		return fmt.Errorf("missing session_mac_key")
	}
	if config.SessionExpiresAt == "" {
		return fmt.Errorf("missing session_expires_at")
	}
	if config.LeaseID == "" {
		return fmt.Errorf("missing lease_id")
	}
	if config.MorphlingID == "" {
		return fmt.Errorf("missing morphling_id")
	}
	return nil
}

// RunMorphlingRunnerProcess is the entry point for the morphling-runner binary.
// It reads a TaskPlanRunnerConfig from the provided JSON bytes, executes the
// runner logic, and returns the result as JSON bytes.
func RunMorphlingRunnerProcess(ctx context.Context, configJSON []byte) ([]byte, error) {
	var config TaskPlanRunnerConfig
	if err := json.Unmarshal(configJSON, &config); err != nil {
		return nil, fmt.Errorf("parse runner config: %w", err)
	}
	result := RunTaskPlanMorphling(ctx, config)
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal runner result: %w", err)
	}
	return resultBytes, nil
}
