package loopgate

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigPutPolicyReloadsSignedPolicyFromDisk(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))
	initialPolicyRuntime := server.currentPolicyRuntime()
	if !initialPolicyRuntime.policy.Tools.Filesystem.WriteRequiresApproval {
		t.Fatal("expected initial policy to require write approval")
	}

	writerClient := NewClient(client.socketPath)
	writerClient.ConfigureSession("config-writer", "config-write-policy-reload", []string{controlCapabilityConfigWrite})
	if _, err := writerClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure config.write token: %v", err)
	}

	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))

	var responseBody struct {
		Status               string `json:"status"`
		PreviousPolicySHA256 string `json:"previous_policy_sha256"`
		PolicySHA256         string `json:"policy_sha256"`
		PolicyChanged        bool   `json:"policy_changed"`
	}
	if err := writerClient.doJSON(context.Background(), http.MethodPut, "/v1/config/policy", writerClient.capabilityToken, nil, &responseBody, nil); err != nil {
		t.Fatalf("reload signed policy: %v", err)
	}
	if responseBody.Status != "ok" {
		t.Fatalf("unexpected reload response: %#v", responseBody)
	}
	if responseBody.PreviousPolicySHA256 != initialPolicyRuntime.policyContentSHA256 {
		t.Fatalf("expected previous policy sha %q, got %#v", initialPolicyRuntime.policyContentSHA256, responseBody)
	}
	if !responseBody.PolicyChanged {
		t.Fatalf("expected policy_changed=true, got %#v", responseBody)
	}
	if strings.TrimSpace(responseBody.PolicySHA256) == "" || responseBody.PolicySHA256 == initialPolicyRuntime.policyContentSHA256 {
		t.Fatalf("expected updated policy sha, got %#v", responseBody)
	}

	reloadedPolicyRuntime := server.currentPolicyRuntime()
	if reloadedPolicyRuntime.policy.Tools.Filesystem.WriteRequiresApproval {
		t.Fatal("expected reloaded policy to disable write approval")
	}
	if reloadedPolicyRuntime.policyContentSHA256 != responseBody.PolicySHA256 {
		t.Fatalf("expected runtime policy sha %q to match response %#v", reloadedPolicyRuntime.policyContentSHA256, responseBody)
	}
}

func TestConfigPutPolicyRejectsTamperedPolicyOnDisk(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))
	initialPolicyRuntime := server.currentPolicyRuntime()

	writerClient := NewClient(client.socketPath)
	writerClient.ConfigureSession("config-writer", "config-write-policy-conflict", []string{controlCapabilityConfigWrite})
	if _, err := writerClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure config.write token: %v", err)
	}

	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.WriteFile(policyPath, []byte(loopgatePolicyYAML(false)), 0o600); err != nil {
		t.Fatalf("tamper policy yaml: %v", err)
	}

	var responseBody map[string]interface{}
	err := writerClient.doJSON(context.Background(), http.MethodPut, "/v1/config/policy", writerClient.capabilityToken, nil, &responseBody, nil)
	if err == nil || !strings.Contains(err.Error(), "status 409") {
		t.Fatalf("expected config/policy PUT to fail closed on signature mismatch, got %v", err)
	}

	reloadedPolicyRuntime := server.currentPolicyRuntime()
	if !reloadedPolicyRuntime.policy.Tools.Filesystem.WriteRequiresApproval {
		t.Fatal("expected in-memory policy to remain unchanged after failed reload")
	}
	if reloadedPolicyRuntime.policyContentSHA256 != initialPolicyRuntime.policyContentSHA256 {
		t.Fatalf("expected policy hash to remain %q, got %q", initialPolicyRuntime.policyContentSHA256, reloadedPolicyRuntime.policyContentSHA256)
	}
}
