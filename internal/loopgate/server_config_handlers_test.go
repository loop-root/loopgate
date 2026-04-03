package loopgate

import (
	"context"
	"net/http"
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

	writerDeniedClient := NewClient(client.socketPath)
	writerDeniedClient.ConfigureSession("config-writer", "config-write-denied", []string{controlCapabilityConfigRead})
	if _, err := writerDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure config.read token: %v", err)
	}
	var deniedResponse map[string]string
	err := writerDeniedClient.doJSON(context.Background(), http.MethodPut, "/v1/config/goal_aliases", writerDeniedClient.capabilityToken, config.GoalAliases{
		Version: "1",
		Aliases: map[string][]string{"workflow_followup": []string{"carry_forward"}},
	}, &deniedResponse, nil)
	if err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected config.write scope denial, got %v", err)
	}

	writerClient := NewClient(client.socketPath)
	writerClient.ConfigureSession("config-writer", "config-write-allowed", []string{controlCapabilityConfigWrite})
	if _, err := writerClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure config.write token: %v", err)
	}
	var okResponse map[string]string
	if err := writerClient.doJSON(context.Background(), http.MethodPut, "/v1/config/goal_aliases", writerClient.capabilityToken, config.GoalAliases{
		Version: "1",
		Aliases: map[string][]string{"workflow_followup": []string{"carry_forward"}},
	}, &okResponse, nil); err != nil {
		t.Fatalf("config.write put goal aliases: %v", err)
	}
	if okResponse["status"] != "ok" {
		t.Fatalf("unexpected config write response: %#v", okResponse)
	}
	if len(server.goalAliases.Aliases["workflow_followup"]) != 1 || server.goalAliases.Aliases["workflow_followup"][0] != "carry_forward" {
		t.Fatalf("expected updated goal aliases in memory, got %#v", server.goalAliases.Aliases)
	}
}
