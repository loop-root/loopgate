package loopgate

import (
	"context"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"testing"
)

func TestExecuteCapabilityRequest_ShellCommandOutsideAllowlistIsDeniedBeforeApproval(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgateShellPolicyYAML(true, []string{"git"}))

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-shell-policy-deny",
		Capability: "shell_exec",
		Arguments: map[string]string{
			"command": "env",
		},
	})
	if err != nil {
		t.Fatalf("execute shell_exec: %v", err)
	}
	if response.Status != controlapipkg.ResponseStatusDenied {
		t.Fatalf("expected denied shell response, got %#v", response)
	}
	if response.DenialCode != controlapipkg.DenialCodePolicyDenied {
		t.Fatalf("expected policy denial code, got %#v", response)
	}
	if response.ApprovalRequired {
		t.Fatalf("expected allowlist denial before approval creation, got %#v", response)
	}
}
