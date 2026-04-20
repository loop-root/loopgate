package loopgate

import (
	"context"
	"errors"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
)

func TestConfigGetRequiresAuthenticatedScopedSignedRequest(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	var rawPolicy map[string]interface{}
	err := client.doJSON(context.Background(), http.MethodGet, "/v1/config/policy", "", nil, &rawPolicy, nil)
	if err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenMissing) {
		t.Fatalf("expected missing capability token denial, got %v", err)
	}

	client.ConfigureSession("config-reader", "config-reader-session", []string{"fs_read"})
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure scoped capability token: %v", err)
	}
	err = client.doJSON(context.Background(), http.MethodGet, "/v1/config/policy", client.capabilityToken, nil, &rawPolicy, nil)
	if err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
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
	runtimeConfigUpdate.Logging.AuditExport.MaxBatchEvents = 777

	writerDeniedClient := NewClient(client.socketPath)
	writerDeniedClient.ConfigureSession("config-writer", "config-write-denied", []string{controlCapabilityConfigRead})
	if _, err := writerDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure config.read token: %v", err)
	}
	var deniedResponse map[string]string
	err := writerDeniedClient.doJSON(context.Background(), http.MethodPut, "/v1/config/runtime", writerDeniedClient.capabilityToken, runtimeConfigUpdate, &deniedResponse, nil)
	if err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
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
	if server.runtimeConfig.Logging.AuditExport.MaxBatchEvents != runtimeConfigUpdate.Logging.AuditExport.MaxBatchEvents {
		t.Fatalf("expected updated runtime config in memory, got %#v", server.runtimeConfig.Logging.AuditExport.MaxBatchEvents)
	}
}

func TestConfigPutRuntimeRejectsUnknownFieldsAndLeavesStateUnchanged(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	writerClient := NewClient(client.socketPath)
	writerClient.ConfigureSession("config-writer", "config-write-runtime-unknown-field", []string{controlCapabilityConfigWrite})
	if _, err := writerClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure config.write token: %v", err)
	}

	previousRuntimeConfig := server.currentRuntimeConfigSnapshot()
	previousRuntimeConfigSHA256, err := configSHA256(previousRuntimeConfig)
	if err != nil {
		t.Fatalf("hash previous runtime config: %v", err)
	}

	var responseBody map[string]interface{}
	err = writerClient.doJSON(context.Background(), http.MethodPut, "/v1/config/runtime", writerClient.capabilityToken, map[string]interface{}{
		"unexpected_runtime_field": true,
	}, &responseBody, nil)
	if err == nil || !strings.Contains(err.Error(), "status 400") {
		t.Fatalf("expected strict decode failure for unknown runtime field, got %v", err)
	}

	currentRuntimeConfigSHA256, err := configSHA256(server.currentRuntimeConfigSnapshot())
	if err != nil {
		t.Fatalf("hash current runtime config: %v", err)
	}
	if currentRuntimeConfigSHA256 != previousRuntimeConfigSHA256 {
		t.Fatalf("expected in-memory runtime config to remain unchanged, got previous=%s current=%s", previousRuntimeConfigSHA256, currentRuntimeConfigSHA256)
	}

	reloadedRuntimeConfig, err := config.LoadRuntimeConfig(repoRoot)
	if err != nil {
		t.Fatalf("reload runtime config from disk: %v", err)
	}
	reloadedRuntimeConfigSHA256, err := configSHA256(reloadedRuntimeConfig)
	if err != nil {
		t.Fatalf("hash reloaded runtime config: %v", err)
	}
	if reloadedRuntimeConfigSHA256 != previousRuntimeConfigSHA256 {
		t.Fatalf("expected persisted runtime config to remain unchanged, got previous=%s persisted=%s", previousRuntimeConfigSHA256, reloadedRuntimeConfigSHA256)
	}
}

func TestConfigPutRuntimeWritesAuditEventAndUpdatesDerivedState(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	writerClient := NewClient(client.socketPath)
	writerClient.ConfigureSession("config-writer", "config-write-runtime-audited", []string{controlCapabilityConfigWrite})
	if _, err := writerClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure config.write token: %v", err)
	}

	runtimeConfigUpdate := server.currentRuntimeConfigSnapshot()
	runtimeConfigUpdate.Logging.AuditExport.MaxBatchEvents = 777
	runtimeConfigUpdate.Logging.AuditExport.StatePath = "runtime/state/audit-export/runtime-put.json"
	runtimeConfigUpdate.ControlPlane.ExpectedSessionClientExecutable = "/Applications/LoopgateClient.app/Contents/MacOS/LoopgateClient"

	var responseBody map[string]string
	if err := writerClient.doJSON(context.Background(), http.MethodPut, "/v1/config/runtime", writerClient.capabilityToken, runtimeConfigUpdate, &responseBody, nil); err != nil {
		t.Fatalf("config.write put runtime: %v", err)
	}
	if responseBody["status"] != "ok" {
		t.Fatalf("unexpected runtime update response: %#v", responseBody)
	}

	if server.runtimeConfig.Logging.AuditExport.MaxBatchEvents != runtimeConfigUpdate.Logging.AuditExport.MaxBatchEvents {
		t.Fatalf("expected updated runtime batch size, got %#v", server.runtimeConfig.Logging.AuditExport.MaxBatchEvents)
	}
	expectedClientPath := normalizeSessionExecutablePinPath(runtimeConfigUpdate.ControlPlane.ExpectedSessionClientExecutable)
	if server.expectedClientPath != expectedClientPath {
		t.Fatalf("expected client pin %q, got %q", expectedClientPath, server.expectedClientPath)
	}
	expectedAuditExportStatePath := filepath.Join(repoRoot, runtimeConfigUpdate.Logging.AuditExport.StatePath)
	if server.auditExportStatePath != expectedAuditExportStatePath {
		t.Fatalf("expected audit export state path %q, got %q", expectedAuditExportStatePath, server.auditExportStatePath)
	}

	auditBytes, err := os.ReadFile(server.auditPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if !strings.Contains(string(auditBytes), `"type":"config.runtime.updated"`) {
		t.Fatalf("expected config.runtime.updated audit event, got %s", string(auditBytes))
	}
}

func TestConfigPutConnectionsAllowsReplacingConfiguredCapabilityDefinitions(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	writerClient := NewClient(client.socketPath)
	writerClient.ConfigureSession("config-writer", "config-write-connections-replace", []string{controlCapabilityConfigWrite})
	if _, err := writerClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure config.write token: %v", err)
	}

	firstConfig := []connectionConfigFile{testPublicReadConnectionConfig("Read public status summary.", "/status.json")}
	secondConfig := []connectionConfigFile{testPublicReadConnectionConfig("Read public status summary (updated).", "/summary.json")}

	var firstResponse map[string]string
	if err := writerClient.doJSON(context.Background(), http.MethodPut, "/v1/config/connections", writerClient.capabilityToken, firstConfig, &firstResponse, nil); err != nil {
		t.Fatalf("first config.write put connections: %v", err)
	}
	if firstResponse["status"] != "ok" {
		t.Fatalf("unexpected first connections update response: %#v", firstResponse)
	}

	var secondResponse map[string]string
	if err := writerClient.doJSON(context.Background(), http.MethodPut, "/v1/config/connections", writerClient.capabilityToken, secondConfig, &secondResponse, nil); err != nil {
		t.Fatalf("second config.write put connections: %v", err)
	}
	if secondResponse["status"] != "ok" {
		t.Fatalf("unexpected second connections update response: %#v", secondResponse)
	}

	registeredTool := server.currentPolicyRuntime().registry.Get("statuspage.summary_get")
	configuredTool, ok := registeredTool.(*configuredCapabilityTool)
	if !ok {
		t.Fatalf("expected configured capability tool, got %#v", registeredTool)
	}
	if configuredTool.definition.Description != "Read public status summary (updated)." {
		t.Fatalf("expected updated configured capability description, got %#v", configuredTool.definition)
	}
	if configuredTool.definition.Path != "/summary.json" {
		t.Fatalf("expected updated configured capability path, got %#v", configuredTool.definition)
	}

	auditBytes, err := os.ReadFile(server.auditPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if strings.Count(string(auditBytes), `"type":"config.connections.updated"`) != 2 {
		t.Fatalf("expected two config.connections.updated audit events, got %s", string(auditBytes))
	}
}

func TestConfigPutConnectionsRollsBackPersistedStateWhenAuditFails(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	writerClient := NewClient(client.socketPath)
	writerClient.ConfigureSession("config-writer", "config-write-connections-audit-failure", []string{controlCapabilityConfigWrite})
	if _, err := writerClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure config.write token: %v", err)
	}

	server.appendAuditEvent = func(string, ledger.Event) error {
		return errors.New("audit unavailable for test")
	}

	var responseBody map[string]interface{}
	err := writerClient.doJSON(context.Background(), http.MethodPut, "/v1/config/connections", writerClient.capabilityToken, []connectionConfigFile{
		testPublicReadConnectionConfig("Read public status summary.", "/status.json"),
	}, &responseBody, nil)
	if err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeAuditUnavailable) {
		t.Fatalf("expected audit unavailable rollback failure, got %v", err)
	}

	if registeredTool := server.currentPolicyRuntime().registry.Get("statuspage.summary_get"); registeredTool != nil {
		t.Fatalf("expected registry to remain unchanged after audit failure, got %#v", registeredTool)
	}

	server.providerRuntime.mu.Lock()
	configuredConnections := len(server.providerRuntime.configuredConnections)
	configuredCapabilities := len(server.providerRuntime.configuredCapabilities)
	server.providerRuntime.mu.Unlock()
	if configuredConnections != 0 || configuredCapabilities != 0 {
		t.Fatalf("expected configured provider state to remain empty, got connections=%d capabilities=%d", configuredConnections, configuredCapabilities)
	}

	persistedConfigFiles, err := config.LoadJSONConfig[[]connectionConfigFile](server.configStateDir, "connections")
	if err != nil {
		t.Fatalf("load persisted connections config: %v", err)
	}
	if len(persistedConfigFiles) != 0 {
		t.Fatalf("expected persisted connections config rollback to empty, got %#v", persistedConfigFiles)
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

func testPublicReadConnectionConfig(description string, capabilityPath string) connectionConfigFile {
	return connectionConfigFile{
		Provider:     "statuspage",
		GrantType:    controlapipkg.GrantTypePublicRead,
		Subject:      "github",
		APIBaseURL:   "https://www.githubstatus.com/api/v2",
		AllowedHosts: []string{"www.githubstatus.com"},
		Capabilities: []connectionCapabilityConfig{
			{
				Name:         "statuspage.summary_get",
				Description:  description,
				Method:       http.MethodGet,
				Path:         capabilityPath,
				ContentClass: contentClassStructuredJSONConfig,
				Extractor:    extractorJSONNestedSelectorConfig,
				ResponseFields: []connectionCapabilityFieldConfig{
					{
						Name:           "status_description",
						JSONPath:       "status.description",
						Sensitivity:    controlapipkg.ResultFieldSensitivityTaintedText,
						MaxInlineBytes: 128,
					},
				},
			},
		},
	}
}
