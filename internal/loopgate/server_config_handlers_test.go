package loopgate

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"morph/internal/config"
)

func TestConfigGetRequiresAuthenticatedScopedSignedRequest(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	var rawPolicy map[string]interface{}
	err := client.doJSON(context.Background(), http.MethodGet, "/v1/config/policy", "", nil, &rawPolicy, nil)
	if err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenMissing) {
		t.Fatalf("expected missing capability token denial, got %v", err)
	}

	client.ConfigureSession("config-reader", "config-reader-session", []string{"fs_read"})
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure scoped capability token: %v", err)
	}
	err = client.doJSON(context.Background(), http.MethodGet, "/v1/config/policy", client.capabilityToken, nil, &rawPolicy, nil)
	if err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected config.read scope denial, got %v", err)
	}

	readerClient := NewClient(client.socketPath)
	readerClient.ConfigureSession("config-reader", "config-reader-allowed-session", []string{controlCapabilityConfigRead})
	if _, err := readerClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure config.read capability token: %v", err)
	}
	if err := readerClient.doJSON(context.Background(), http.MethodGet, "/v1/config/policy", readerClient.capabilityToken, nil, &rawPolicy, nil); err != nil {
		t.Fatalf("config.read get policy: %v", err)
	}
	if len(rawPolicy) == 0 {
		t.Fatal("expected config policy payload")
	}
}

func TestConfigPutRequiresConfigWriteScope(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	runtimeConfigUpdate := config.DefaultRuntimeConfig()
	runtimeConfigUpdate.Memory.CandidatePanelSize = 9

	writerDeniedClient := NewClient(client.socketPath)
	writerDeniedClient.ConfigureSession("config-writer", "config-write-denied", []string{controlCapabilityConfigRead})
	if _, err := writerDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure config.read token: %v", err)
	}
	var deniedResponse map[string]string
	err := writerDeniedClient.doJSON(context.Background(), http.MethodPut, "/v1/config/runtime", writerDeniedClient.capabilityToken, runtimeConfigUpdate, &deniedResponse, nil)
	if err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected config.write scope denial, got %v", err)
	}

	writerClient := NewClient(client.socketPath)
	writerClient.ConfigureSession("config-writer", "config-write-allowed", []string{controlCapabilityConfigWrite})
	if _, err := writerClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure config.write token: %v", err)
	}
	var okResponse map[string]string
	if err := writerClient.doJSON(context.Background(), http.MethodPut, "/v1/config/runtime", writerClient.capabilityToken, runtimeConfigUpdate, &okResponse, nil); err != nil {
		t.Fatalf("config.write put runtime: %v", err)
	}
	if okResponse["status"] != "ok" {
		t.Fatalf("unexpected config write response: %#v", okResponse)
	}
	if server.runtimeConfig.Memory.CandidatePanelSize != runtimeConfigUpdate.Memory.CandidatePanelSize {
		t.Fatalf("expected updated runtime config in memory, got %#v", server.runtimeConfig.Memory.CandidatePanelSize)
	}
}

func TestConfigGoalAliasesRouteRetired(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, client.baseURL+"/v1/config/goal_aliases", nil)
	if err != nil {
		t.Fatalf("build retired config route request: %v", err)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		t.Fatalf("request retired config route: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected retired goal_aliases route to return 404, got %d", response.StatusCode)
	}
}

func TestConfigMorphlingClassesRouteRetired(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, client.baseURL+"/v1/config/morphling_classes", nil)
	if err != nil {
		t.Fatalf("build retired config route request: %v", err)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		t.Fatalf("request retired config route: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected retired morphling_classes route to return 404, got %d", response.StatusCode)
	}
}

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
