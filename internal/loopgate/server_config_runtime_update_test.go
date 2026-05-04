package loopgate

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loopgate/internal/config"
)

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
