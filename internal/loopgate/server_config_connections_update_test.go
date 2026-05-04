package loopgate

import (
	"context"
	"errors"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"os"
	"strings"
	"testing"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
)

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
