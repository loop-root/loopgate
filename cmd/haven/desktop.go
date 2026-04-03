package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"morph/internal/loopgate"
)

// --- System Status ---

// SystemStatus returns a snapshot of current system activity
// for the Haven desktop system status view.
//
// Security invariant: model-originated content does not appear verbatim in
// this response (see docs/loopgate-threat-model.md). Morphling goal/status text
// is count-projected, not passed through raw.
func (app *HavenApp) SystemStatus() SystemStatusResponse {
	status, err := app.loopgateClient.UIStatus(context.Background())
	if err != nil {
		return SystemStatusResponse{Error: fmt.Sprintf("activity unavailable: %v", err)}
	}

	// Morphling/worker status.
	var workers []WorkerSummary
	morphlingResp, morphErr := app.loopgateClient.MorphlingStatus(context.Background(), loopgate.MorphlingStatusRequest{})
	if morphErr == nil {
		for _, m := range morphlingResp.Morphlings {
			workers = append(workers, WorkerSummary{
				ID:    m.MorphlingID,
				Class: m.Class,
				State: m.State,
				// Loopgate MorphlingSummary no longer ships raw goal_hint / worker memory strings;
				// surface class + state + counts only (no raw model strings in summaries).
				Goal:                m.Class,
				ArtifactCount:       m.ArtifactCount,
				MemoryStringCount:   m.MemoryStringCount,
				PendingReview:       m.PendingReview,
				CapabilityCount:     len(m.GrantedCapabilities),
				TimeBudgetSeconds:   m.TimeBudgetSeconds,
			})
		}
	}

	// Capability access summary.
	capAccess := make([]CapabilityAccess, 0, len(app.capabilities))
	for _, cap := range app.capabilities {
		capAccess = append(capAccess, CapabilityAccess{
			Name:      cap.Name,
			Category:  cap.Category,
			Operation: cap.Operation,
			Granted:   true,
		})
	}

	return SystemStatusResponse{
		TurnCount:        status.TurnCount,
		PendingApprovals: status.PendingApprovals,
		ActiveWorkers:    len(workers),
		MaxWorkers:       morphlingResp.MaxActive,
		Workers:          workers,
		Capabilities:     capAccess,
		Policy: PolicyOverview{
			ReadEnabled:           status.Policy.ReadEnabled,
			WriteEnabled:          status.Policy.WriteEnabled,
			WriteRequiresApproval: status.Policy.WriteRequiresApproval,
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// --- Security Center ---

// SecurityOverview returns security posture information for the
// Haven security center panel.
//
// Security invariant: this data is derived from Loopgate's public status
// endpoints. No secrets, tokens, or raw audit content are exposed.
func (app *HavenApp) SecurityOverview() SecurityOverviewResponse {
	status, err := app.loopgateClient.Status(context.Background())
	if err != nil {
		return SecurityOverviewResponse{Error: fmt.Sprintf("security data unavailable: %v", err)}
	}

	// Capability inventory.
	caps := make([]CapabilityAccess, 0, len(status.Capabilities))
	for _, cap := range status.Capabilities {
		caps = append(caps, CapabilityAccess{
			Name:      cap.Name,
			Category:  cap.Category,
			Operation: cap.Operation,
			Granted:   true,
		})
	}

	// Connection status (redacted — no secrets).
	connections := make([]ConnectionSummary, 0, len(status.Connections))
	for _, conn := range status.Connections {
		connections = append(connections, ConnectionSummary{
			Provider:      conn.Provider,
			Status:        conn.Status,
			LastValidated: conn.LastValidatedAtUTC,
		})
	}

	// Pending approvals.
	var approvals []ApprovalSummary
	approvalsResp, approvalErr := app.loopgateClient.UIApprovals(context.Background())
	if approvalErr == nil {
		for _, a := range approvalsResp.Approvals {
			approvals = append(approvals, ApprovalSummary{
				ApprovalRequestID: a.ApprovalRequestID,
				Capability:        a.Capability,
				ExpiresAt:         a.ExpiresAtUTC,
				Redacted:          a.Redacted,
			})
		}
	}

	standingGrants := make([]StandingTaskGrantSummary, 0)
	taskGrantStatusResp, taskGrantErr := app.loopgateClient.TaskStandingGrantStatus(context.Background())
	if taskGrantErr == nil {
		for _, grantStatus := range taskGrantStatusResp.Grants {
			standingGrants = append(standingGrants, StandingTaskGrantSummary{
				Class:        grantStatus.Class,
				Label:        grantStatus.Label,
				Description:  grantStatus.Description,
				SandboxOnly:  grantStatus.SandboxOnly,
				DefaultGrant: grantStatus.DefaultGrant,
				Granted:      grantStatus.Granted,
			})
		}
	}

	return SecurityOverviewResponse{
		Capabilities:       caps,
		Connections:        connections,
		PendingApprovals:   approvals,
		StandingTaskGrants: standingGrants,
		Policy: PolicyOverview{
			ReadEnabled:           status.Policy.Tools.Filesystem.ReadEnabled,
			WriteEnabled:          status.Policy.Tools.Filesystem.WriteEnabled,
			WriteRequiresApproval: status.Policy.Tools.Filesystem.WriteRequiresApproval,
		},
		ActiveMorphlings: status.ActiveMorphlings,
	}
}

func (app *HavenApp) UpdateTaskStandingGrant(className string, granted bool) SecurityOverviewResponse {
	if _, err := app.loopgateClient.UpdateTaskStandingGrant(context.Background(), loopgate.TaskStandingGrantUpdateRequest{
		Class:   className,
		Granted: granted,
	}); err != nil {
		return SecurityOverviewResponse{Error: fmt.Sprintf("update task standing grant: %v", err)}
	}
	return app.SecurityOverview()
}

// --- Response types ---

type SystemStatusResponse struct {
	TurnCount        int                `json:"turn_count"`
	PendingApprovals int                `json:"pending_approvals"`
	ActiveWorkers    int                `json:"active_workers"`
	MaxWorkers       int                `json:"max_workers"`
	Workers          []WorkerSummary    `json:"workers"`
	Capabilities     []CapabilityAccess `json:"capabilities"`
	Policy           PolicyOverview     `json:"policy"`
	Timestamp        string             `json:"timestamp"`
	Error            string             `json:"error,omitempty"`
}

type WorkerSummary struct {
	ID                string `json:"id"`
	Class             string `json:"class"`
	State             string `json:"state"`
	Goal                string `json:"goal,omitempty"`
	ArtifactCount       int    `json:"artifact_count"`
	MemoryStringCount   int    `json:"memory_string_count,omitempty"`
	PendingReview       bool   `json:"pending_review"`
	CapabilityCount   int    `json:"capability_count"`
	TimeBudgetSeconds int    `json:"time_budget_seconds,omitempty"`
}

type CapabilityAccess struct {
	Name      string `json:"name"`
	Category  string `json:"category"`
	Operation string `json:"operation"`
	Granted   bool   `json:"granted"`
}

type PolicyOverview struct {
	ReadEnabled           bool `json:"read_enabled"`
	WriteEnabled          bool `json:"write_enabled"`
	WriteRequiresApproval bool `json:"write_requires_approval"`
}

type SecurityOverviewResponse struct {
	Capabilities       []CapabilityAccess         `json:"capabilities"`
	Connections        []ConnectionSummary        `json:"connections"`
	PendingApprovals   []ApprovalSummary          `json:"pending_approvals"`
	StandingTaskGrants []StandingTaskGrantSummary `json:"standing_task_grants,omitempty"`
	Policy             PolicyOverview             `json:"policy"`
	ActiveMorphlings   int                        `json:"active_morphlings"`
	Error              string                     `json:"error,omitempty"`
}

type ConnectionSummary struct {
	Provider      string `json:"provider"`
	Status        string `json:"status"`
	LastValidated string `json:"last_validated,omitempty"`
}

type ApprovalSummary struct {
	ApprovalRequestID string `json:"approval_request_id"`
	Capability        string `json:"capability"`
	ExpiresAt         string `json:"expires_at"`
	Redacted          bool   `json:"redacted"`
}

type StandingTaskGrantSummary struct {
	Class        string `json:"class"`
	Label        string `json:"label"`
	Description  string `json:"description"`
	SandboxOnly  bool   `json:"sandbox_only"`
	DefaultGrant bool   `json:"default_grant"`
	Granted      bool   `json:"granted"`
}

// truncateForDisplay truncates a string for safe display.
// This is used for morphling goal hints which may contain model-generated
// content that should not appear verbatim (UI projection / threat model).
func truncateForDisplay(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// --- Icon Positions ---

// IconPosition represents a desktop icon's position.
type IconPosition struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// IconPositionsResponse wraps the result of loading icon positions.
type IconPositionsResponse struct {
	Positions map[string]IconPosition `json:"positions"`
	Error     string                  `json:"error,omitempty"`
}

// LoadIconPositions reads Morph-organized icon positions from the state file.
// Returns an empty positions map if no state file exists (frontend uses defaults).
func (app *HavenApp) LoadIconPositions() IconPositionsResponse {
	statePath := filepath.Join(app.repoRoot, "runtime", "state", "haven_icon_positions.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return IconPositionsResponse{Positions: map[string]IconPosition{}}
		}
		return IconPositionsResponse{Error: fmt.Sprintf("read icon positions: %v", err)}
	}

	var state struct {
		Positions map[string]IconPosition `json:"positions"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return IconPositionsResponse{Error: fmt.Sprintf("parse icon positions: %v", err)}
	}
	if state.Positions == nil {
		state.Positions = map[string]IconPosition{}
	}
	return IconPositionsResponse{Positions: state.Positions}
}

// emitIconPositionsChanged reads the state file and emits a Wails event
// so the frontend can pick up Morph's icon layout changes.
func (app *HavenApp) emitIconPositionsChanged() {
	if app.emitter == nil {
		return
	}
	resp := app.LoadIconPositions()
	if resp.Error != "" {
		return
	}
	app.emitter.Emit("haven:icon_positions_changed", map[string]interface{}{
		"positions": resp.Positions,
	})
}
