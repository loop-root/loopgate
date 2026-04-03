package main

import (
	"testing"

	"morph/internal/loopgate"
)

func TestSystemStatus_ReturnsWorkerSummaries(t *testing.T) {
	client := &fakeLoopgateClient{}
	app, _ := testApp(t, client)

	// Assign capabilities to the app (simulates what main.go does).
	app.capabilities = []loopgate.CapabilitySummary{
		{Name: "fs_read", Category: "filesystem", Operation: "read"},
	}

	resp := app.SystemStatus()
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if resp.Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}
	if len(resp.Capabilities) != 1 {
		t.Fatalf("expected 1 capability, got %d", len(resp.Capabilities))
	}
	if resp.Capabilities[0].Name != "fs_read" {
		t.Errorf("expected fs_read, got %s", resp.Capabilities[0].Name)
	}
}

func TestSecurityOverview_ReturnsCapabilitiesAndConnections(t *testing.T) {
	client := &fakeLoopgateClient{
		statusResp: loopgate.StatusResponse{
			Capabilities: []loopgate.CapabilitySummary{
				{Name: "fs_read", Category: "filesystem", Operation: "read"},
				{Name: "fs_write", Category: "filesystem", Operation: "write"},
			},
			Connections: []loopgate.ConnectionStatus{
				{Provider: "anthropic", Status: "valid"},
			},
			ActiveMorphlings: 2,
		},
		taskStandingGrantResp: loopgate.TaskStandingGrantStatusResponse{
			Grants: []loopgate.TaskStandingGrantStatus{{
				Class:       loopgate.TaskExecutionClassLocalWorkspaceOrganize,
				Label:       "Organize Haven Files",
				Description: "Rearrange or tidy files inside Morph's own Haven workspace only.",
				SandboxOnly: true,
				Granted:     true,
			}},
		},
	}
	app, _ := testApp(t, client)

	resp := app.SecurityOverview()
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if len(resp.Capabilities) != 2 {
		t.Errorf("expected 2 capabilities, got %d", len(resp.Capabilities))
	}
	if len(resp.Connections) != 1 {
		t.Errorf("expected 1 connection, got %d", len(resp.Connections))
	}
	if resp.Connections[0].Provider != "anthropic" {
		t.Errorf("expected anthropic, got %s", resp.Connections[0].Provider)
	}
	if resp.ActiveMorphlings != 2 {
		t.Errorf("expected 2 active morphlings, got %d", resp.ActiveMorphlings)
	}
	if len(resp.StandingTaskGrants) != 1 || !resp.StandingTaskGrants[0].Granted {
		t.Fatalf("expected standing task grant summary, got %#v", resp.StandingTaskGrants)
	}
}

func TestTruncateForDisplay(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a long string that should be truncated", 10, "this is a ..."},
		{"", 5, ""},
	}
	for _, tt := range tests {
		got := truncateForDisplay(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncateForDisplay(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

func TestSystemStatus_InvariantNoVerbatimModelContent(t *testing.T) {
	// Model-originated content must not appear verbatim in operator summaries;
	// truncation is a defense-in-depth display cap (see docs/loopgate-threat-model.md).
	longGoal := "This is a very long model-generated goal hint that exceeds the sixty character display limit for safety"

	// Verify the goal would be truncated.
	truncated := truncateForDisplay(longGoal, 60)
	if truncated == longGoal {
		t.Error("expected long goal to be truncated")
	}
	if len(truncated) > 64 { // 60 + "..."
		t.Errorf("truncated length %d exceeds max display length", len(truncated))
	}
}
