package loopgate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"morph/internal/config"
	modelpkg "morph/internal/model"
	modelruntime "morph/internal/modelruntime"
	"morph/internal/threadstore"
)

func TestHavenMemoryUIRoutesRequireScopedCapabilities(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	pinTestProcessAsExpectedClient(t, server)

	inventoryDeniedClient := NewClient(client.socketPath)
	inventoryDeniedClient.ConfigureSession("haven", "ui-memory-read-denied", []string{"fs_read"})
	if _, err := inventoryDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied inventory token: %v", err)
	}
	if _, err := inventoryDeniedClient.LoadHavenMemoryInventory(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected memory.read scope denial for ui inventory, got %v", err)
	}

	resetDeniedClient := NewClient(client.socketPath)
	resetDeniedClient.ConfigureSession("haven", "ui-memory-reset-denied", []string{controlCapabilityMemoryWrite})
	if _, err := resetDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied reset token: %v", err)
	}
	if _, err := resetDeniedClient.ResetHavenMemory(context.Background(), HavenMemoryResetRequest{
		OperationID: "ui-memory-reset-denied",
		Reason:      "scope check",
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected memory.reset scope denial for ui reset, got %v", err)
	}
}

func TestStatusOmitsConnectionsWithoutConnectionReadScope(t *testing.T) {
	repoRoot := t.TempDir()
	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"status":"ok"}`))
	}))
	defer providerServer.Close()
	writeConfiguredConnectionYAML(t, repoRoot, providerServer.URL)

	client, _, _ := startLoopgateServer(t, repoRoot, loopgateHTTPPolicyYAML(false))

	limitedClient := NewClient(client.socketPath)
	limitedClient.ConfigureSession("test-actor", "status-no-connection-read", []string{"fs_list"})
	if _, err := limitedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure limited token: %v", err)
	}
	status, err := limitedClient.Status(context.Background())
	if err != nil {
		t.Fatalf("status without connection.read: %v", err)
	}
	if len(status.Connections) != 0 {
		t.Fatalf("expected status to omit connection summaries without connection.read, got %#v", status.Connections)
	}
}

func TestConnectionRoutesRequireConnectionScopes(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgateHTTPPolicyYAML(false))

	readDeniedClient := NewClient(client.socketPath)
	readDeniedClient.ConfigureSession("test-actor", "connection-read-denied", []string{"fs_list"})
	if _, err := readDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied connection.read token: %v", err)
	}
	if _, err := readDeniedClient.ConnectionsStatus(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected connection.read scope denial, got %v", err)
	}

	writeDeniedClient := NewClient(client.socketPath)
	writeDeniedClient.ConfigureSession("test-actor", "connection-write-denied", []string{controlCapabilityConnectionRead})
	if _, err := writeDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied connection.write token: %v", err)
	}
	if _, err := writeDeniedClient.ValidateConnection(context.Background(), "missing", "subject"); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected connection.write scope denial for validate, got %v", err)
	}
	if _, err := writeDeniedClient.StartPKCEConnection(context.Background(), PKCEStartRequest{
		Provider: "examplepkce",
		Subject:  "workspace-user",
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected connection.write scope denial for pkce start, got %v", err)
	}
}

func TestSiteRoutesRequireScopedCapabilities(t *testing.T) {
	repoRoot := t.TempDir()
	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"status":{"description":"All Systems Operational","indicator":"none"}}`))
	}))
	defer providerServer.Close()

	client, _, _ := startLoopgateServer(t, repoRoot, loopgateHTTPPolicyYAML(false))

	inspectDeniedClient := NewClient(client.socketPath)
	inspectDeniedClient.ConfigureSession("test-actor", "site-inspect-denied", []string{controlCapabilityConnectionRead})
	if _, err := inspectDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied site.inspect token: %v", err)
	}
	if _, err := inspectDeniedClient.InspectSite(context.Background(), SiteInspectionRequest{URL: providerServer.URL}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected site.inspect scope denial, got %v", err)
	}

	trustDeniedClient := NewClient(client.socketPath)
	trustDeniedClient.ConfigureSession("test-actor", "site-trust-denied", []string{controlCapabilitySiteInspect})
	if _, err := trustDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied site.trust.write token: %v", err)
	}
	if _, err := trustDeniedClient.CreateTrustDraft(context.Background(), SiteTrustDraftRequest{URL: providerServer.URL}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected site.trust.write scope denial, got %v", err)
	}
}

func TestDiagnosticRouteRequiresScopedCapability(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	pinTestProcessAsExpectedClient(t, server)

	deniedClient := NewClient(client.socketPath)
	deniedClient.ConfigureSession("test-actor", "diagnostic-read-denied", []string{controlCapabilityConnectionRead})
	if _, err := deniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied diagnostic.read token: %v", err)
	}
	var deniedReport map[string]interface{}
	if err := deniedClient.FetchDiagnosticReport(context.Background(), &deniedReport); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected diagnostic.read scope denial, got %v", err)
	}

	allowedClient := NewClient(client.socketPath)
	allowedClient.ConfigureSession("test-actor", "diagnostic-read-allowed", []string{controlCapabilityDiagnosticRead})
	if _, err := allowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure diagnostic.read token: %v", err)
	}
	var allowedReport map[string]interface{}
	if err := allowedClient.FetchDiagnosticReport(context.Background(), &allowedReport); err != nil {
		t.Fatalf("fetch diagnostic report with diagnostic.read: %v", err)
	}
}

func TestModelRoutesRequireScopedCapabilities(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	modelRequest := modelpkg.Request{
		Persona:     config.Persona{Name: "Morph"},
		Policy:      status.Policy,
		SessionID:   "model-scope-session",
		TurnCount:   1,
		UserMessage: "check security scopes",
	}

	replyDeniedClient := NewClient(client.socketPath)
	replyDeniedClient.ConfigureSession("test-actor", "model-reply-denied", []string{controlCapabilityModelValidate})
	if _, err := replyDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied model.reply token: %v", err)
	}
	if _, err := replyDeniedClient.ModelReply(context.Background(), modelRequest); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected model.reply scope denial, got %v", err)
	}

	replyAllowedClient := NewClient(client.socketPath)
	replyAllowedClient.ConfigureSession("test-actor", "model-reply-allowed", []string{controlCapabilityModelReply})
	if _, err := replyAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure model.reply token: %v", err)
	}
	if _, err := replyAllowedClient.ModelReply(context.Background(), modelRequest); err != nil {
		t.Fatalf("model reply with model.reply: %v", err)
	}

	validateDeniedClient := NewClient(client.socketPath)
	validateDeniedClient.ConfigureSession("test-actor", "model-validate-denied", []string{controlCapabilityModelReply})
	if _, err := validateDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied model.validate token: %v", err)
	}
	if _, err := validateDeniedClient.ValidateModelConfig(context.Background(), modelruntime.Config{
		ProviderName: "stub",
		ModelName:    "stub",
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected model.validate scope denial, got %v", err)
	}

	validateAllowedClient := NewClient(client.socketPath)
	validateAllowedClient.ConfigureSession("test-actor", "model-validate-allowed", []string{controlCapabilityModelValidate})
	if _, err := validateAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure model.validate token: %v", err)
	}
	if _, err := validateAllowedClient.ValidateModelConfig(context.Background(), modelruntime.Config{
		ProviderName: "stub",
		ModelName:    "stub",
	}); err != nil {
		t.Fatalf("validate model config with model.validate: %v", err)
	}

	storeDeniedClient := NewClient(client.socketPath)
	storeDeniedClient.ConfigureSession("test-actor", "model-connection-store-denied", []string{controlCapabilityModelValidate})
	if _, err := storeDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied connection.write token: %v", err)
	}
	if _, err := storeDeniedClient.StoreModelConnection(context.Background(), ModelConnectionStoreRequest{
		ConnectionID: "scope_test_connection_denied",
		ProviderName: "openai_compatible",
		BaseURL:      "https://api.example.test/v1",
		SecretValue:  "sk-test-denied",
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected connection.write scope denial for model connection store, got %v", err)
	}

	storeAllowedClient := NewClient(client.socketPath)
	storeAllowedClient.ConfigureSession("test-actor", "model-connection-store-allowed", []string{controlCapabilityConnectionWrite})
	if _, err := storeAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure connection.write token: %v", err)
	}
	if _, err := storeAllowedClient.StoreModelConnection(context.Background(), ModelConnectionStoreRequest{
		ConnectionID: "scope_test_connection_allowed",
		ProviderName: "openai_compatible",
		BaseURL:      "https://api.example.test/v1",
		SecretValue:  "sk-test-allowed",
	}); err != nil {
		t.Fatalf("store model connection with connection.write: %v", err)
	}
}

func TestHavenModelRoutesRequireScopedCapabilities(t *testing.T) {
	repoRoot := t.TempDir()
	modelServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"data":[{"id":"phi4"},{"id":"llama3.3"}]}`))
	}))
	defer modelServer.Close()

	if err := modelruntime.SavePersistedConfig(modelruntime.ConfigPath(repoRoot), modelruntime.Config{
		ProviderName: "openai_compatible",
		ModelName:    "phi4",
		BaseURL:      modelServer.URL + "/v1",
	}); err != nil {
		t.Fatalf("seed persisted model config: %v", err)
	}

	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	pinTestProcessAsExpectedClient(t, server)

	settingsReadDeniedClient := NewClient(client.socketPath)
	settingsReadDeniedClient.ConfigureSession("haven", "model-settings-read-denied", []string{controlCapabilityConnectionWrite})
	if _, err := settingsReadDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied model.settings.read token: %v", err)
	}
	var deniedSettings HavenModelSettingsResponse
	if err := settingsReadDeniedClient.doJSON(context.Background(), http.MethodGet, "/v1/model/settings", settingsReadDeniedClient.capabilityToken, nil, &deniedSettings, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected model.settings.read scope denial, got %v", err)
	}

	settingsReadAllowedClient := NewClient(client.socketPath)
	settingsReadAllowedClient.ConfigureSession("haven", "model-settings-read-allowed", []string{controlCapabilityModelSettingsRead})
	if _, err := settingsReadAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure model.settings.read token: %v", err)
	}
	var allowedSettings HavenModelSettingsResponse
	if err := settingsReadAllowedClient.doJSON(context.Background(), http.MethodGet, "/v1/model/settings", settingsReadAllowedClient.capabilityToken, nil, &allowedSettings, nil); err != nil {
		t.Fatalf("get model settings with model.settings.read: %v", err)
	}

	settingsWriteDeniedClient := NewClient(client.socketPath)
	settingsWriteDeniedClient.ConfigureSession("haven", "model-settings-write-denied", []string{controlCapabilityModelSettingsRead})
	if _, err := settingsWriteDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied model.settings.write token: %v", err)
	}
	if err := settingsWriteDeniedClient.doJSON(context.Background(), http.MethodPost, "/v1/model/settings", settingsWriteDeniedClient.capabilityToken, havenModelSettingsPostRequest{
		Mode:         "local",
		ModelName:    "phi4-mini",
		LocalBaseURL: modelServer.URL + "/v1",
	}, &HavenModelSettingsResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected model.settings.write scope denial, got %v", err)
	}

	settingsWriteAllowedClient := NewClient(client.socketPath)
	settingsWriteAllowedClient.ConfigureSession("haven", "model-settings-write-allowed", []string{controlCapabilityModelSettingsWrite})
	if _, err := settingsWriteAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure model.settings.write token: %v", err)
	}
	var updatedSettings HavenModelSettingsResponse
	if err := settingsWriteAllowedClient.doJSON(context.Background(), http.MethodPost, "/v1/model/settings", settingsWriteAllowedClient.capabilityToken, havenModelSettingsPostRequest{
		Mode:         "local",
		ModelName:    "phi4-mini",
		LocalBaseURL: modelServer.URL + "/v1",
	}, &updatedSettings, nil); err != nil {
		t.Fatalf("update model settings with model.settings.write: %v", err)
	}

	remoteSettingsDeniedClient := NewClient(client.socketPath)
	remoteSettingsDeniedClient.ConfigureSession("haven", "model-settings-remote-store-denied", []string{controlCapabilityModelSettingsWrite})
	if _, err := remoteSettingsDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied remote connection.write token: %v", err)
	}
	if err := remoteSettingsDeniedClient.doJSON(context.Background(), http.MethodPost, "/v1/model/settings", remoteSettingsDeniedClient.capabilityToken, havenModelSettingsPostRequest{
		Mode:         "openai_compatible",
		ModelName:    "gpt-4.1",
		LocalBaseURL: "https://api.openai.com/v1",
		OpenAIAPIKey: "sk-remote-scope-denied",
	}, &HavenModelSettingsResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected connection.write scope denial for remote model settings secret store, got %v", err)
	}

	modelCatalogDeniedClient := NewClient(client.socketPath)
	modelCatalogDeniedClient.ConfigureSession("haven", "model-catalog-denied", []string{controlCapabilityModelSettingsRead})
	if _, err := modelCatalogDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied model catalog token: %v", err)
	}
	catalogPath := "/v1/model/openai/models?base_url=" + url.QueryEscape(modelServer.URL+"/v1")
	if err := modelCatalogDeniedClient.doJSON(context.Background(), http.MethodGet, catalogPath, modelCatalogDeniedClient.capabilityToken, nil, &OllamaTagsResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected connection.write scope denial for model catalog route, got %v", err)
	}

	modelCatalogAllowedClient := NewClient(client.socketPath)
	modelCatalogAllowedClient.ConfigureSession("haven", "model-catalog-allowed", []string{controlCapabilityConnectionWrite})
	if _, err := modelCatalogAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure connection.write token for model catalog: %v", err)
	}
	var catalogResponse OllamaTagsResponse
	if err := modelCatalogAllowedClient.doJSON(context.Background(), http.MethodGet, catalogPath, modelCatalogAllowedClient.capabilityToken, nil, &catalogResponse, nil); err != nil {
		t.Fatalf("load model catalog with connection.write: %v", err)
	}
	if len(catalogResponse.Models) != 2 {
		t.Fatalf("expected 2 model catalog entries, got %#v", catalogResponse)
	}

	chatDeniedClient := NewClient(client.socketPath)
	chatDeniedClient.ConfigureSession("haven", "haven-chat-model-reply-denied", []string{"fs_list"})
	if _, err := chatDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied haven chat token: %v", err)
	}
	if err := chatDeniedClient.doJSON(context.Background(), http.MethodPost, "/v1/chat", chatDeniedClient.capabilityToken, havenChatRequest{
		Message: "scope check",
	}, &CapabilityResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected model.reply scope denial for haven chat, got %v", err)
	}

	residentDeniedClient := NewClient(client.socketPath)
	residentDeniedClient.ConfigureSession("haven", "haven-journal-tick-denied", []string{"fs_write"})
	if _, err := residentDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied resident tick token: %v", err)
	}
	if err := residentDeniedClient.doJSON(context.Background(), http.MethodPost, "/v1/resident/journal-tick", residentDeniedClient.capabilityToken, nil, &HavenJournalResidentTickResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected model.reply scope denial for resident tick, got %v", err)
	}

	residentAllowedClient := NewClient(client.socketPath)
	residentAllowedClient.ConfigureSession("haven", "haven-journal-tick-allowed", []string{controlCapabilityModelReply})
	if _, err := residentAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure model.reply token for resident tick: %v", err)
	}
	var residentResponse HavenJournalResidentTickResponse
	if err := residentAllowedClient.doJSON(context.Background(), http.MethodPost, "/v1/resident/journal-tick", residentAllowedClient.capabilityToken, nil, &residentResponse, nil); err != nil {
		t.Fatalf("resident tick with model.reply: %v", err)
	}
	if residentResponse.Status != "skipped" || !strings.Contains(residentResponse.Reason, "fs_write") {
		t.Fatalf("expected resident tick to continue past scope gate and skip without fs_write, got %#v", residentResponse)
	}
}

func TestHavenRoutesRequireTrustedHavenSession(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	untrustedHavenClient := NewClient(client.socketPath)
	untrustedHavenClient.ConfigureSession("haven", "haven-untrusted-route-check", advertisedSessionCapabilityNames(status))
	if _, err := untrustedHavenClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure untrusted haven token: %v", err)
	}

	if err := untrustedHavenClient.doJSON(context.Background(), http.MethodGet, "/v1/settings/idle", untrustedHavenClient.capabilityToken, nil, &havenIdleSettingsResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenInvalid) {
		t.Fatalf("expected trusted Haven denial for idle settings, got %v", err)
	}
	if err := untrustedHavenClient.doJSON(context.Background(), http.MethodGet, "/v1/model/settings", untrustedHavenClient.capabilityToken, nil, &HavenModelSettingsResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenInvalid) {
		t.Fatalf("expected trusted Haven denial for model settings, got %v", err)
	}
	if err := untrustedHavenClient.doJSON(context.Background(), http.MethodPost, "/v1/agent/work-item/ensure", untrustedHavenClient.capabilityToken, HavenAgentWorkEnsureRequest{
		Text: "untrusted haven should not reach helper routes",
	}, &HavenAgentWorkItemResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenInvalid) {
		t.Fatalf("expected trusted Haven denial for agent work ensure, got %v", err)
	}
	if err := untrustedHavenClient.doJSON(context.Background(), http.MethodPost, "/v1/chat", untrustedHavenClient.capabilityToken, havenChatRequest{
		Message: "untrusted haven should not reach chat",
	}, &CapabilityResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenInvalid) {
		t.Fatalf("expected trusted Haven denial for chat, got %v", err)
	}
}

func TestHavenConvenienceRoutesRequireUIWriteScope(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	pinTestProcessAsExpectedClient(t, server)
	server.resolveUserHomeDir = func() (string, error) { return repoRoot, nil }

	workspaceID := server.deriveWorkspaceIDFromRepoRoot()
	threadStoreRoot := filepath.Join(repoRoot, ".haven", "threads")
	store, err := threadstore.NewStore(threadStoreRoot, workspaceID)
	if err != nil {
		t.Fatalf("thread store: %v", err)
	}
	summary, err := store.NewThread()
	if err != nil {
		t.Fatalf("new thread: %v", err)
	}
	_ = store.AppendEvent(summary.ThreadID, threadstore.ConversationEvent{
		Type: threadstore.EventUserMessage,
		Data: map[string]interface{}{"text": "route scope check"},
	})

	agentWorkDeniedClient := NewClient(client.socketPath)
	agentWorkDeniedClient.ConfigureSession("haven", "agent-work-ui-write-denied", []string{"todo.add"})
	if _, err := agentWorkDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied agent-work token: %v", err)
	}
	if err := agentWorkDeniedClient.doJSON(context.Background(), http.MethodPost, "/v1/agent/work-item/ensure", agentWorkDeniedClient.capabilityToken, HavenAgentWorkEnsureRequest{
		Text: "Route scope check",
	}, &HavenAgentWorkItemResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected ui.write scope denial for haven agent-work ensure, got %v", err)
	}

	continuityDeniedClient := NewClient(client.socketPath)
	continuityDeniedClient.SetWorkspaceID(workspaceID)
	continuityDeniedClient.ConfigureSession("haven", "continuity-ui-write-denied", []string{"memory.write"})
	if _, err := continuityDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied continuity token: %v", err)
	}
	if err := continuityDeniedClient.doJSON(context.Background(), http.MethodPost, "/v1/continuity/inspect-thread", continuityDeniedClient.capabilityToken, HavenContinuityInspectThreadRequest{
		ThreadID: summary.ThreadID,
	}, &HavenContinuityInspectThreadResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected ui.write scope denial for haven continuity inspect-thread, got %v", err)
	}

	agentWorkAllowedClient := NewClient(client.socketPath)
	agentWorkAllowedClient.ConfigureSession("haven", "agent-work-ui-write-allowed", []string{controlCapabilityUIWrite, "todo.add"})
	if _, err := agentWorkAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure allowed agent-work token: %v", err)
	}
	var ensureResponse HavenAgentWorkItemResponse
	if err := agentWorkAllowedClient.doJSON(context.Background(), http.MethodPost, "/v1/agent/work-item/ensure", agentWorkAllowedClient.capabilityToken, HavenAgentWorkEnsureRequest{
		Text: "Route scope allow check",
	}, &ensureResponse, nil); err != nil {
		t.Fatalf("haven agent-work ensure with ui.write + todo.add: %v", err)
	}
	if strings.TrimSpace(ensureResponse.ItemID) == "" {
		t.Fatalf("expected agent-work ensure to return item_id, got %#v", ensureResponse)
	}

	continuityAllowedClient := NewClient(client.socketPath)
	continuityAllowedClient.SetWorkspaceID(workspaceID)
	continuityAllowedClient.ConfigureSession("haven", "continuity-ui-write-allowed", []string{controlCapabilityUIWrite, "memory.write"})
	if _, err := continuityAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure allowed continuity token: %v", err)
	}
	if _, err := continuityAllowedClient.SubmitHavenContinuityInspectionForThread(context.Background(), summary.ThreadID); err != nil {
		t.Fatalf("haven continuity inspect-thread with ui.write + memory.write: %v", err)
	}
}

func TestFolderAccessRoutesRequireScopedCapabilities(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.resolveUserHomeDir = func() (string, error) { return repoRoot, nil }

	deniedClient := NewClient(client.socketPath)
	deniedClient.ConfigureSession("test-actor", "folder-access-denied", []string{"fs_list"})
	if _, err := deniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied folder_access token: %v", err)
	}
	if _, err := deniedClient.FolderAccessStatus(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected folder_access.read scope denial, got %v", err)
	}
	if _, err := deniedClient.SharedFolderStatus(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected shared-folder read scope denial, got %v", err)
	}
	if _, err := deniedClient.SyncFolderAccess(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected folder_access.write scope denial for sync, got %v", err)
	}
	if _, err := deniedClient.SyncSharedFolder(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected shared-folder write scope denial for sync, got %v", err)
	}

	readAllowedClient := NewClient(client.socketPath)
	readAllowedClient.ConfigureSession("test-actor", "folder-access-read-allowed", []string{controlCapabilityFolderAccessRead})
	if _, err := readAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure folder_access.read token: %v", err)
	}
	if _, err := readAllowedClient.FolderAccessStatus(context.Background()); err != nil {
		t.Fatalf("folder access status with folder_access.read: %v", err)
	}
	if _, err := readAllowedClient.SharedFolderStatus(context.Background()); err != nil {
		t.Fatalf("shared folder status with folder_access.read: %v", err)
	}

	writeAllowedClient := NewClient(client.socketPath)
	writeAllowedClient.ConfigureSession("test-actor", "folder-access-write-allowed", []string{controlCapabilityFolderAccessWrite})
	if _, err := writeAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure folder_access.write token: %v", err)
	}
	if _, err := writeAllowedClient.SyncFolderAccess(context.Background()); err != nil {
		t.Fatalf("sync folder access with folder_access.write: %v", err)
	}
	if _, err := writeAllowedClient.SyncSharedFolder(context.Background()); err != nil {
		t.Fatalf("sync shared folder with folder_access.write: %v", err)
	}
}

func TestTaskStandingGrantRoutesRequireScopedCapabilities(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	deniedClient := NewClient(client.socketPath)
	deniedClient.ConfigureSession("test-actor", "task-standing-grant-denied", []string{"fs_list"})
	if _, err := deniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied task standing grant token: %v", err)
	}
	if _, err := deniedClient.TaskStandingGrantStatus(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected task_standing_grant.read scope denial, got %v", err)
	}
	if _, err := deniedClient.UpdateTaskStandingGrant(context.Background(), TaskStandingGrantUpdateRequest{
		Class:   TaskExecutionClassLocalDesktopOrganize,
		Granted: false,
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected task_standing_grant.write scope denial, got %v", err)
	}

	readAllowedClient := NewClient(client.socketPath)
	readAllowedClient.ConfigureSession("test-actor", "task-standing-grant-read-allowed", []string{controlCapabilityTaskStandingGrantRead})
	if _, err := readAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure task_standing_grant.read token: %v", err)
	}
	if _, err := readAllowedClient.TaskStandingGrantStatus(context.Background()); err != nil {
		t.Fatalf("task standing grant status with read scope: %v", err)
	}

	writeAllowedClient := NewClient(client.socketPath)
	writeAllowedClient.ConfigureSession("test-actor", "task-standing-grant-write-allowed", []string{controlCapabilityTaskStandingGrantWrite})
	if _, err := writeAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure task_standing_grant.write token: %v", err)
	}
	if _, err := writeAllowedClient.UpdateTaskStandingGrant(context.Background(), TaskStandingGrantUpdateRequest{
		Class:   TaskExecutionClassLocalDesktopOrganize,
		Granted: false,
	}); err != nil {
		t.Fatalf("update task standing grant with write scope: %v", err)
	}
}

func TestTaskRoutesRequireScopedCapabilities(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	addResponse, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "task-route-scope-add",
		Capability: "todo.add",
		Arguments: map[string]string{
			"text": "review security route scopes",
		},
	})
	if err != nil {
		t.Fatalf("seed todo item: %v", err)
	}
	itemID, _ := addResponse.StructuredResult["item_id"].(string)
	if strings.TrimSpace(itemID) == "" {
		t.Fatalf("expected todo.add to return item_id, got %#v", addResponse.StructuredResult)
	}

	deniedClient := NewClient(client.socketPath)
	deniedClient.ConfigureSession("test-actor", "tasks-denied", []string{"fs_list"})
	if _, err := deniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied tasks token: %v", err)
	}
	if _, err := deniedClient.LoadTasks(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected tasks.read scope denial, got %v", err)
	}
	if err := deniedClient.SetExplicitTodoWorkflowStatus(context.Background(), itemID, explicitTodoWorkflowStatusInProgress); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected tasks.write scope denial, got %v", err)
	}

	readAllowedClient := NewClient(client.socketPath)
	readAllowedClient.ConfigureSession("test-actor", "tasks-read-allowed", []string{controlCapabilityTasksRead})
	if _, err := readAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure tasks.read token: %v", err)
	}
	if _, err := readAllowedClient.LoadTasks(context.Background()); err != nil {
		t.Fatalf("load tasks with tasks.read: %v", err)
	}

	writeAllowedClient := NewClient(client.socketPath)
	writeAllowedClient.ConfigureSession("test-actor", "tasks-write-allowed", []string{controlCapabilityTasksWrite})
	if _, err := writeAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure tasks.write token: %v", err)
	}
	if err := writeAllowedClient.SetExplicitTodoWorkflowStatus(context.Background(), itemID, explicitTodoWorkflowStatusInProgress); err != nil {
		t.Fatalf("update task status with tasks.write: %v", err)
	}
}

func TestTaskPlanRoutesRequireScopedCapabilities(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	readOnlyClient := NewClient(client.socketPath)
	readOnlyClient.ConfigureSession("test-actor", "task-plan-read-only", []string{controlCapabilityTaskPlanRead})
	if _, err := readOnlyClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure task_plan.read token: %v", err)
	}
	steps := []TaskPlanStep{
		{StepIndex: 0, Capability: "echo.generate_summary", Arguments: map[string]string{"input_text": "scope check"}},
	}
	submitRequest := SubmitTaskPlanRequest{
		GoalText:      "task-plan scope check",
		Steps:         steps,
		CanonicalHash: computeCanonicalHash("task-plan scope check", steps),
	}
	if err := readOnlyClient.doJSONWithHeaders(context.Background(), httpMethodPost, "/v1/task/plan", readOnlyClient.capabilityToken, submitRequest, &SubmitTaskPlanResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected task_plan.write scope denial for submit, got %v", err)
	}
	if err := readOnlyClient.doJSONWithHeaders(context.Background(), httpMethodPost, "/v1/task/execute", readOnlyClient.capabilityToken, ExecuteTaskLeaseRequest{
		LeaseID:     "lease-read-only",
		MorphlingID: "morphling-read-only",
	}, &ExecuteTaskLeaseResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected task_plan.write scope denial for execute, got %v", err)
	}

	writeOnlyClient := NewClient(client.socketPath)
	writeOnlyClient.ConfigureSession("test-actor", "task-plan-write-only", []string{controlCapabilityTaskPlanWrite})
	if _, err := writeOnlyClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure task_plan.write token: %v", err)
	}
	if err := writeOnlyClient.doJSONWithHeaders(context.Background(), httpMethodPost, "/v1/task/result", writeOnlyClient.capabilityToken, TaskPlanResultRequest{
		PlanID: "plan-write-only",
	}, &TaskPlanResultResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected task_plan.read scope denial for result, got %v", err)
	}

	readWriteClient := NewClient(client.socketPath)
	readWriteClient.ConfigureSession("test-actor", "task-plan-read-write", []string{controlCapabilityTaskPlanRead, controlCapabilityTaskPlanWrite})
	if _, err := readWriteClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure task_plan read/write token: %v", err)
	}
	planResponse := submitTaskPlanForTest(t, readWriteClient, "task-plan allowed")
	if planResponse.Status != taskPlanStateValidated {
		t.Fatalf("expected validated plan status, got %#v", planResponse)
	}
	resultRequest := TaskPlanResultRequest{PlanID: planResponse.PlanID}
	var resultResponse TaskPlanResultResponse
	if err := readWriteClient.doJSONWithHeaders(context.Background(), httpMethodPost, "/v1/task/result", readWriteClient.capabilityToken, resultRequest, &resultResponse, nil); err != nil {
		t.Fatalf("task/result with task_plan.read: %v", err)
	}
	if resultResponse.PlanID != planResponse.PlanID {
		t.Fatalf("expected task result for %q, got %#v", planResponse.PlanID, resultResponse)
	}
}

func TestQuarantineRoutesRequireScopedCapabilities(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	quarantineRef, err := server.storeQuarantinedPayload(CapabilityRequest{
		RequestID:  "req-quarantine-scope",
		Capability: "remote_fetch",
	}, "quarantined payload")
	if err != nil {
		t.Fatalf("store quarantined payload: %v", err)
	}

	readDeniedClient := NewClient(client.socketPath)
	readDeniedClient.ConfigureSession("test-actor", "quarantine-read-denied", []string{"fs_read"})
	if _, err := readDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied quarantine.read token: %v", err)
	}
	if _, err := readDeniedClient.QuarantineMetadata(context.Background(), quarantineRef); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected quarantine.read scope denial for metadata, got %v", err)
	}
	if _, err := readDeniedClient.ViewQuarantinedPayload(context.Background(), quarantineRef); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected quarantine.read scope denial for view, got %v", err)
	}

	writeDeniedClient := NewClient(client.socketPath)
	writeDeniedClient.ConfigureSession("test-actor", "quarantine-write-denied", []string{controlCapabilityQuarantineRead})
	if _, err := writeDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied quarantine.write token: %v", err)
	}
	if _, err := writeDeniedClient.PruneQuarantinedPayload(context.Background(), quarantineRef); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected quarantine.write scope denial for prune, got %v", err)
	}

	readAllowedClient := NewClient(client.socketPath)
	readAllowedClient.ConfigureSession("test-actor", "quarantine-read-allowed", []string{controlCapabilityQuarantineRead})
	if _, err := readAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure quarantine.read token: %v", err)
	}
	metadataResponse, err := readAllowedClient.QuarantineMetadata(context.Background(), quarantineRef)
	if err != nil {
		t.Fatalf("quarantine metadata with quarantine.read: %v", err)
	}
	if metadataResponse.QuarantineRef != quarantineRef {
		t.Fatalf("expected metadata for %q, got %#v", quarantineRef, metadataResponse)
	}
	viewResponse, err := readAllowedClient.ViewQuarantinedPayload(context.Background(), quarantineRef)
	if err != nil {
		t.Fatalf("quarantine view with quarantine.read: %v", err)
	}
	if viewResponse.Metadata.QuarantineRef != quarantineRef {
		t.Fatalf("expected view metadata for %q, got %#v", quarantineRef, viewResponse)
	}

	ageQuarantineRecordForPrune(t, repoRoot, quarantineRef)

	writeAllowedClient := NewClient(client.socketPath)
	writeAllowedClient.ConfigureSession("test-actor", "quarantine-write-allowed", []string{controlCapabilityQuarantineWrite})
	if _, err := writeAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure quarantine.write token: %v", err)
	}
	prunedMetadata, err := writeAllowedClient.PruneQuarantinedPayload(context.Background(), quarantineRef)
	if err != nil {
		t.Fatalf("quarantine prune with quarantine.write: %v", err)
	}
	if prunedMetadata.StorageState != quarantineStorageStateBlobPruned {
		t.Fatalf("expected blob_pruned storage state, got %#v", prunedMetadata)
	}
}

func TestMorphlingRoutesRequireScopedCapabilities(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgateMorphlingPolicyYAML(false, true, 5))

	writeAllowedClient := NewClient(client.socketPath)
	writeAllowedClient.ConfigureSession("test-actor", "morphling-write-allowed", []string{controlCapabilityMorphlingWrite})
	if _, err := writeAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure morphling.write token: %v", err)
	}

	seedSpawnResponse, err := writeAllowedClient.SpawnMorphling(context.Background(), MorphlingSpawnRequest{
		Class:                 "reviewer",
		Goal:                  "seed morphling scope checks",
		RequestedCapabilities: []string{"fs_list", "fs_read"},
	})
	if err != nil {
		t.Fatalf("spawn morphling with morphling.write: %v", err)
	}
	if seedSpawnResponse.Status != ResponseStatusSuccess {
		t.Fatalf("expected successful seed spawn, got %#v", seedSpawnResponse)
	}

	pendingReviewResponse := driveMorphlingToPendingReview(t, writeAllowedClient, server, "scope status", "scope memory")

	readDeniedClient := NewClient(client.socketPath)
	readDeniedClient.ConfigureSession("test-actor", "morphling-read-denied", []string{"fs_list"})
	if _, err := readDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied morphling.read token: %v", err)
	}
	if _, err := readDeniedClient.MorphlingStatus(context.Background(), MorphlingStatusRequest{MorphlingID: seedSpawnResponse.MorphlingID}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected morphling.read scope denial for status, got %v", err)
	}

	writeDeniedClient := NewClient(client.socketPath)
	writeDeniedClient.ConfigureSession("test-actor", "morphling-write-denied", []string{controlCapabilityMorphlingRead})
	if _, err := writeDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied morphling.write token: %v", err)
	}
	if _, err := writeDeniedClient.SpawnMorphling(context.Background(), MorphlingSpawnRequest{
		Class:                 "reviewer",
		Goal:                  "denied morphling spawn",
		RequestedCapabilities: []string{"fs_list", "fs_read"},
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected morphling.write scope denial for spawn, got %v", err)
	}
	if _, err := writeDeniedClient.TerminateMorphling(context.Background(), MorphlingTerminateRequest{
		MorphlingID: seedSpawnResponse.MorphlingID,
		Reason:      "scope denied terminate",
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected morphling.write scope denial for terminate, got %v", err)
	}
	if _, err := writeDeniedClient.LaunchMorphlingWorker(context.Background(), MorphlingWorkerLaunchRequest{
		MorphlingID: seedSpawnResponse.MorphlingID,
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected morphling.write scope denial for worker launch, got %v", err)
	}
	if _, err := writeDeniedClient.ReviewMorphling(context.Background(), MorphlingReviewRequest{
		MorphlingID: pendingReviewResponse.Morphling.MorphlingID,
		Approved:    true,
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected morphling.write scope denial for review, got %v", err)
	}

	readAllowedClient := NewClient(client.socketPath)
	readAllowedClient.ConfigureSession("test-actor", "morphling-read-allowed", []string{controlCapabilityMorphlingRead})
	if _, err := readAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure morphling.read token: %v", err)
	}
	statusResponse, err := readAllowedClient.MorphlingStatus(context.Background(), MorphlingStatusRequest{})
	if err != nil {
		t.Fatalf("morphling status with morphling.read: %v", err)
	}
	if len(statusResponse.Morphlings) != 0 {
		t.Fatalf("expected empty session-scoped status response for separate read-only session, got %#v", statusResponse)
	}

	reviewResponse, err := writeAllowedClient.ReviewMorphling(context.Background(), MorphlingReviewRequest{
		MorphlingID: pendingReviewResponse.Morphling.MorphlingID,
		Approved:    true,
	})
	if err != nil {
		t.Fatalf("review morphling with morphling.write: %v", err)
	}
	if reviewResponse.Morphling.State != morphlingStateTerminated {
		t.Fatalf("expected terminated morphling after allowed review, got %#v", reviewResponse)
	}
	terminateResponse, err := writeAllowedClient.TerminateMorphling(context.Background(), MorphlingTerminateRequest{
		MorphlingID: seedSpawnResponse.MorphlingID,
		Reason:      "scope allowed terminate",
	})
	if err != nil {
		t.Fatalf("terminate morphling with morphling.write: %v", err)
	}
	if terminateResponse.Morphling.State != morphlingStateTerminated {
		t.Fatalf("expected terminated seeded morphling, got %#v", terminateResponse)
	}
}

func TestSandboxRoutesRequireCapabilityScopes(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	pinTestProcessAsExpectedClient(t, server)
	if err := server.sandboxPaths.Ensure(); err != nil {
		t.Fatalf("ensure sandbox paths: %v", err)
	}

	workspaceFilePath := filepath.Join(server.sandboxPaths.Workspace, "scope-test.txt")
	if err := os.MkdirAll(filepath.Dir(workspaceFilePath), 0o755); err != nil {
		t.Fatalf("mkdir sandbox workspace: %v", err)
	}
	if err := os.WriteFile(workspaceFilePath, []byte("sandbox scope"), 0o600); err != nil {
		t.Fatalf("seed sandbox file: %v", err)
	}

	hostImportPath := filepath.Join(t.TempDir(), "import.txt")
	if err := os.WriteFile(hostImportPath, []byte("import me"), 0o600); err != nil {
		t.Fatalf("seed host import file: %v", err)
	}

	listDeniedClient := NewClient(client.socketPath)
	listDeniedClient.ConfigureSession("test-actor", "sandbox-list-denied", []string{"fs_read"})
	if _, err := listDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied fs_list token: %v", err)
	}
	if _, err := listDeniedClient.SandboxList(context.Background(), SandboxListRequest{SandboxPath: "workspace"}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected fs_list scope denial for sandbox list, got %v", err)
	}

	listAllowedClient := NewClient(client.socketPath)
	listAllowedClient.ConfigureSession("test-actor", "sandbox-list-allowed", []string{"fs_list"})
	if _, err := listAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure fs_list token: %v", err)
	}
	if _, err := listAllowedClient.SandboxList(context.Background(), SandboxListRequest{SandboxPath: "workspace"}); err != nil {
		t.Fatalf("sandbox list with fs_list: %v", err)
	}

	metadataDeniedClient := NewClient(client.socketPath)
	metadataDeniedClient.ConfigureSession("test-actor", "sandbox-metadata-denied", []string{"fs_list"})
	if _, err := metadataDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied fs_read token: %v", err)
	}

	importDeniedClient := NewClient(client.socketPath)
	importDeniedClient.SetOperatorMountPaths([]string{filepath.Dir(hostImportPath)}, filepath.Dir(hostImportPath))
	importDeniedClient.ConfigureSession("haven", "sandbox-import-denied", []string{"fs_read"})
	if _, err := importDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied fs_write token: %v", err)
	}
	if _, err := importDeniedClient.SandboxImport(context.Background(), SandboxImportRequest{
		HostSourcePath:  hostImportPath,
		DestinationName: "scope-import-denied.txt",
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected fs_write scope denial for sandbox import, got %v", err)
	}

	importAllowedClient := NewClient(client.socketPath)
	importAllowedClient.SetOperatorMountPaths([]string{filepath.Dir(hostImportPath)}, filepath.Dir(hostImportPath))
	importAllowedClient.ConfigureSession("haven", "sandbox-import-allowed", []string{"fs_write"})
	if _, err := importAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure fs_write token: %v", err)
	}
	if _, err := importAllowedClient.SandboxImport(context.Background(), SandboxImportRequest{
		HostSourcePath:  hostImportPath,
		DestinationName: "scope-import-allowed.txt",
	}); err != nil {
		t.Fatalf("sandbox import with fs_write: %v", err)
	}
	stageResponse, err := importAllowedClient.SandboxStage(context.Background(), SandboxStageRequest{
		SandboxSourcePath: "workspace/scope-test.txt",
		OutputName:        "scope-output.txt",
	})
	if err != nil {
		t.Fatalf("sandbox stage with fs_write: %v", err)
	}

	if _, err := metadataDeniedClient.SandboxMetadata(context.Background(), SandboxMetadataRequest{SandboxSourcePath: stageResponse.SandboxRelativePath}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected fs_read scope denial for sandbox metadata, got %v", err)
	}

	metadataAllowedClient := NewClient(client.socketPath)
	metadataAllowedClient.ConfigureSession("test-actor", "sandbox-metadata-allowed", []string{"fs_read"})
	if _, err := metadataAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure fs_read token: %v", err)
	}
	if _, err := metadataAllowedClient.SandboxMetadata(context.Background(), SandboxMetadataRequest{SandboxSourcePath: stageResponse.SandboxRelativePath}); err != nil {
		t.Fatalf("sandbox metadata with fs_read: %v", err)
	}
}

func TestHavenProjectionRoutesRequireCapabilityScopes(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if err := server.sandboxPaths.Ensure(); err != nil {
		t.Fatalf("ensure sandbox paths: %v", err)
	}

	journalFilePath := filepath.Join(server.sandboxPaths.Scratch, "journal", "today.md")
	if err := os.MkdirAll(filepath.Dir(journalFilePath), 0o755); err != nil {
		t.Fatalf("mkdir journal dir: %v", err)
	}
	if err := os.WriteFile(journalFilePath, []byte("# Today\n\nSecurity notes"), 0o600); err != nil {
		t.Fatalf("seed journal file: %v", err)
	}

	workingNotePath := filepath.Join(server.sandboxPaths.Scratch, "notes", "scope-note.md")
	if err := os.MkdirAll(filepath.Dir(workingNotePath), 0o755); err != nil {
		t.Fatalf("mkdir working notes dir: %v", err)
	}
	if err := os.WriteFile(workingNotePath, []byte("# Scope Note\n\nKeep UI projections bounded."), 0o600); err != nil {
		t.Fatalf("seed working note file: %v", err)
	}

	paintPath := filepath.Join(server.sandboxPaths.Outputs, "paintings", "20260410-120000-0001-scope.svg")
	if err := os.MkdirAll(filepath.Dir(paintPath), 0o755); err != nil {
		t.Fatalf("mkdir paint dir: %v", err)
	}
	if err := os.WriteFile(paintPath, []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>"), 0o600); err != nil {
		t.Fatalf("seed paint file: %v", err)
	}

	workspaceFilePath := filepath.Join(server.sandboxPaths.Workspace, "preview.txt")
	if err := os.WriteFile(workspaceFilePath, []byte("preview me"), 0o600); err != nil {
		t.Fatalf("seed workspace preview file: %v", err)
	}

	uiDeniedClient := NewClient(client.socketPath)
	uiDeniedClient.ConfigureSession("haven", "haven-ui-projection-denied", []string{"fs_list", "fs_read", "notes.write"})
	if _, err := uiDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied ui projection token: %v", err)
	}
	if err := uiDeniedClient.doJSON(context.Background(), http.MethodGet, "/v1/ui/journal/entries", uiDeniedClient.capabilityToken, nil, &HavenJournalEntriesResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected ui.read scope denial for journal entries, got %v", err)
	}
	if err := uiDeniedClient.doJSON(context.Background(), http.MethodGet, "/v1/ui/paint/gallery", uiDeniedClient.capabilityToken, nil, &HavenPaintGalleryResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected ui.read scope denial for paint gallery, got %v", err)
	}
	if err := uiDeniedClient.doJSON(context.Background(), http.MethodPost, "/v1/ui/workspace/list", uiDeniedClient.capabilityToken, HavenWorkspaceListRequest{}, &HavenWorkspaceListResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected ui.read scope denial for workspace list, got %v", err)
	}
	if err := uiDeniedClient.doJSON(context.Background(), http.MethodGet, "/v1/ui/workspace/host-layout", uiDeniedClient.capabilityToken, nil, &HavenWorkspaceHostLayoutResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected ui.read scope denial for workspace host layout, got %v", err)
	}
	if err := uiDeniedClient.doJSON(context.Background(), http.MethodPost, "/v1/ui/working-notes/save", uiDeniedClient.capabilityToken, HavenWorkingNoteSaveRequest{
		Title:   "Scope Test",
		Content: "Saved through notes.write",
	}, &HavenWorkingNoteSaveResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected ui.write scope denial for working note save, got %v", err)
	}

	listDeniedClient := NewClient(client.socketPath)
	listDeniedClient.ConfigureSession("haven", "haven-fs-list-denied", []string{controlCapabilityUIRead, "fs_read"})
	if _, err := listDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied haven fs_list token: %v", err)
	}
	if err := listDeniedClient.doJSON(context.Background(), http.MethodGet, "/v1/ui/journal/entries", listDeniedClient.capabilityToken, nil, &HavenJournalEntriesResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected fs_list scope denial for journal entries, got %v", err)
	}
	if err := listDeniedClient.doJSON(context.Background(), http.MethodPost, "/v1/ui/workspace/list", listDeniedClient.capabilityToken, HavenWorkspaceListRequest{}, &HavenWorkspaceListResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected fs_list scope denial for workspace list, got %v", err)
	}
	if err := listDeniedClient.doJSON(context.Background(), http.MethodGet, "/v1/ui/workspace/host-layout", listDeniedClient.capabilityToken, nil, &HavenWorkspaceHostLayoutResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected fs_list scope denial for workspace host layout, got %v", err)
	}

	listAllowedClient := NewClient(client.socketPath)
	listAllowedClient.ConfigureSession("haven", "haven-fs-list-allowed", []string{controlCapabilityUIRead, "fs_list", "fs_read"})
	if _, err := listAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure haven fs_list token: %v", err)
	}
	if err := listAllowedClient.doJSON(context.Background(), http.MethodGet, "/v1/ui/journal/entries", listAllowedClient.capabilityToken, nil, &HavenJournalEntriesResponse{}, nil); err != nil {
		t.Fatalf("journal entries with ui.read+fs_list: %v", err)
	}
	if err := listAllowedClient.doJSON(context.Background(), http.MethodGet, "/v1/ui/working-notes", listAllowedClient.capabilityToken, nil, &HavenWorkingNotesResponse{}, nil); err != nil {
		t.Fatalf("working notes with ui.read+fs_list: %v", err)
	}
	if err := listAllowedClient.doJSON(context.Background(), http.MethodGet, "/v1/ui/paint/gallery", listAllowedClient.capabilityToken, nil, &HavenPaintGalleryResponse{}, nil); err != nil {
		t.Fatalf("paint gallery with ui.read+fs_list+fs_read: %v", err)
	}
	if err := listAllowedClient.doJSON(context.Background(), http.MethodPost, "/v1/ui/workspace/list", listAllowedClient.capabilityToken, HavenWorkspaceListRequest{}, &HavenWorkspaceListResponse{}, nil); err != nil {
		t.Fatalf("workspace list with ui.read+fs_list: %v", err)
	}
	if err := listAllowedClient.doJSON(context.Background(), http.MethodGet, "/v1/ui/workspace/host-layout", listAllowedClient.capabilityToken, nil, &HavenWorkspaceHostLayoutResponse{}, nil); err != nil {
		t.Fatalf("workspace host layout with ui.read+fs_list: %v", err)
	}

	readDeniedClient := NewClient(client.socketPath)
	readDeniedClient.ConfigureSession("haven", "haven-fs-read-denied", []string{controlCapabilityUIRead, "fs_list"})
	if _, err := readDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied haven fs_read token: %v", err)
	}
	if err := readDeniedClient.doJSON(context.Background(), http.MethodGet, "/v1/ui/journal/entry?path=research/journal/today.md", readDeniedClient.capabilityToken, nil, &HavenJournalEntryResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected fs_read scope denial for journal entry, got %v", err)
	}
	if err := readDeniedClient.doJSON(context.Background(), http.MethodGet, "/v1/ui/working-notes/entry?path=research/notes/scope-note.md", readDeniedClient.capabilityToken, nil, &HavenWorkingNoteResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected fs_read scope denial for working note entry, got %v", err)
	}
	if err := readDeniedClient.doJSON(context.Background(), http.MethodPost, "/v1/ui/workspace/preview", readDeniedClient.capabilityToken, HavenWorkspacePreviewRequest{Path: "projects/preview.txt"}, &HavenWorkspacePreviewResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected fs_read scope denial for workspace preview, got %v", err)
	}
	if err := readDeniedClient.doJSON(context.Background(), http.MethodGet, "/v1/ui/paint/gallery", readDeniedClient.capabilityToken, nil, &HavenPaintGalleryResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected fs_read scope denial for paint gallery, got %v", err)
	}

	readAllowedClient := NewClient(client.socketPath)
	readAllowedClient.ConfigureSession("haven", "haven-fs-read-allowed", []string{controlCapabilityUIRead, "fs_read", "fs_list"})
	if _, err := readAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure haven fs_read token: %v", err)
	}
	if err := readAllowedClient.doJSON(context.Background(), http.MethodGet, "/v1/ui/journal/entry?path=research/journal/today.md", readAllowedClient.capabilityToken, nil, &HavenJournalEntryResponse{}, nil); err != nil {
		t.Fatalf("journal entry with ui.read+fs_read: %v", err)
	}
	if err := readAllowedClient.doJSON(context.Background(), http.MethodGet, "/v1/ui/working-notes/entry?path=research/notes/scope-note.md", readAllowedClient.capabilityToken, nil, &HavenWorkingNoteResponse{}, nil); err != nil {
		t.Fatalf("working note entry with ui.read+fs_read: %v", err)
	}
	if err := readAllowedClient.doJSON(context.Background(), http.MethodPost, "/v1/ui/workspace/preview", readAllowedClient.capabilityToken, HavenWorkspacePreviewRequest{Path: "projects/preview.txt"}, &HavenWorkspacePreviewResponse{}, nil); err != nil {
		t.Fatalf("workspace preview with ui.read+fs_read: %v", err)
	}

	notesDeniedClient := NewClient(client.socketPath)
	notesDeniedClient.ConfigureSession("haven", "haven-notes-write-denied", []string{controlCapabilityUIWrite, "fs_read"})
	if _, err := notesDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied notes.write token: %v", err)
	}
	if err := notesDeniedClient.doJSON(context.Background(), http.MethodPost, "/v1/ui/working-notes/save", notesDeniedClient.capabilityToken, HavenWorkingNoteSaveRequest{
		Title:   "Scope Test",
		Content: "Saved through notes.write",
	}, &HavenWorkingNoteSaveResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected notes.write scope denial for working note save, got %v", err)
	}

	notesAllowedClient := NewClient(client.socketPath)
	notesAllowedClient.ConfigureSession("haven", "haven-notes-write-allowed", []string{controlCapabilityUIWrite, "notes.write"})
	if _, err := notesAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure notes.write token: %v", err)
	}
	if err := notesAllowedClient.doJSON(context.Background(), http.MethodPost, "/v1/ui/working-notes/save", notesAllowedClient.capabilityToken, HavenWorkingNoteSaveRequest{
		Title:   "Scope Test",
		Content: "Saved through notes.write",
	}, &HavenWorkingNoteSaveResponse{}, nil); err != nil {
		t.Fatalf("working note save with ui.write+notes.write: %v", err)
	}

	uiReadDeniedClient := NewClient(client.socketPath)
	uiReadDeniedClient.ConfigureSession("haven", "haven-ui-read-denied", []string{"fs_list"})
	if _, err := uiReadDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied ui.read token: %v", err)
	}
	if _, err := uiReadDeniedClient.UIStatus(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected ui.read scope denial for ui status, got %v", err)
	}
	uiEventsDeniedRequest, err := http.NewRequestWithContext(context.Background(), http.MethodGet, uiReadDeniedClient.baseURL+"/v1/ui/events", nil)
	if err != nil {
		t.Fatalf("build denied ui events request: %v", err)
	}
	uiEventsDeniedRequest.Header.Set("Authorization", "Bearer "+uiReadDeniedClient.capabilityToken)
	if err := uiReadDeniedClient.attachRequestSignature(uiEventsDeniedRequest, "/v1/ui/events", nil); err != nil {
		t.Fatalf("attach denied ui events signature: %v", err)
	}
	uiEventsDeniedResponse, err := uiReadDeniedClient.httpClient.Do(uiEventsDeniedRequest)
	if err != nil {
		t.Fatalf("do denied ui events request: %v", err)
	}
	if uiEventsDeniedResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("expected ui.read scope denial HTTP status for ui events, got %d", uiEventsDeniedResponse.StatusCode)
	}
	_ = uiEventsDeniedResponse.Body.Close()
	uiRecentDeniedRequest, err := http.NewRequestWithContext(context.Background(), http.MethodGet, uiReadDeniedClient.baseURL+"/v1/ui/events/recent", nil)
	if err != nil {
		t.Fatalf("build denied recent ui events request: %v", err)
	}
	uiRecentDeniedRequest.Header.Set("Authorization", "Bearer "+uiReadDeniedClient.capabilityToken)
	if err := uiReadDeniedClient.attachRequestSignature(uiRecentDeniedRequest, "/v1/ui/events/recent", nil); err != nil {
		t.Fatalf("attach denied recent ui events signature: %v", err)
	}
	uiRecentDeniedResponse, err := uiReadDeniedClient.httpClient.Do(uiRecentDeniedRequest)
	if err != nil {
		t.Fatalf("do denied recent ui events request: %v", err)
	}
	if uiRecentDeniedResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("expected ui.read scope denial HTTP status for recent ui events, got %d", uiRecentDeniedResponse.StatusCode)
	}
	_ = uiRecentDeniedResponse.Body.Close()
	if err := uiReadDeniedClient.doJSON(context.Background(), http.MethodGet, "/v1/ui/desk-notes", uiReadDeniedClient.capabilityToken, nil, &HavenDeskNotesResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected ui.read scope denial for desk notes, got %v", err)
	}
	if err := uiReadDeniedClient.doJSON(context.Background(), http.MethodGet, "/v1/ui/presence", uiReadDeniedClient.capabilityToken, nil, &HavenPresenceResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected ui.read scope denial for presence, got %v", err)
	}
	if err := uiReadDeniedClient.doJSON(context.Background(), http.MethodGet, "/v1/ui/morph-sleep", uiReadDeniedClient.capabilityToken, nil, &HavenMorphSleepResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected ui.read scope denial for morph sleep, got %v", err)
	}

	uiReadAllowedClient := NewClient(client.socketPath)
	uiReadAllowedClient.ConfigureSession("haven", "haven-ui-read-allowed", []string{controlCapabilityUIRead})
	if _, err := uiReadAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure ui.read token: %v", err)
	}
	if _, err := uiReadAllowedClient.UIStatus(context.Background()); err != nil {
		t.Fatalf("ui status with ui.read: %v", err)
	}
	uiEventsAllowedRequest, err := http.NewRequestWithContext(context.Background(), http.MethodGet, uiReadAllowedClient.baseURL+"/v1/ui/events", nil)
	if err != nil {
		t.Fatalf("build allowed ui events request: %v", err)
	}
	uiEventsAllowedRequest.Header.Set("Authorization", "Bearer "+uiReadAllowedClient.capabilityToken)
	if err := uiReadAllowedClient.attachRequestSignature(uiEventsAllowedRequest, "/v1/ui/events", nil); err != nil {
		t.Fatalf("attach allowed ui events signature: %v", err)
	}
	uiEventsAllowedResponse, err := uiReadAllowedClient.httpClient.Do(uiEventsAllowedRequest)
	if err != nil {
		t.Fatalf("do allowed ui events request: %v", err)
	}
	if uiEventsAllowedResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected ui events success with ui.read, got %d", uiEventsAllowedResponse.StatusCode)
	}
	_ = uiEventsAllowedResponse.Body.Close()
	uiRecentAllowedRequest, err := http.NewRequestWithContext(context.Background(), http.MethodGet, uiReadAllowedClient.baseURL+"/v1/ui/events/recent", nil)
	if err != nil {
		t.Fatalf("build allowed recent ui events request: %v", err)
	}
	uiRecentAllowedRequest.Header.Set("Authorization", "Bearer "+uiReadAllowedClient.capabilityToken)
	if err := uiReadAllowedClient.attachRequestSignature(uiRecentAllowedRequest, "/v1/ui/events/recent", nil); err != nil {
		t.Fatalf("attach allowed recent ui events signature: %v", err)
	}
	uiRecentAllowedResponse, err := uiReadAllowedClient.httpClient.Do(uiRecentAllowedRequest)
	if err != nil {
		t.Fatalf("do allowed recent ui events request: %v", err)
	}
	if uiRecentAllowedResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected recent ui events success with ui.read, got %d", uiRecentAllowedResponse.StatusCode)
	}
	_ = uiRecentAllowedResponse.Body.Close()
	if err := uiReadAllowedClient.doJSON(context.Background(), http.MethodGet, "/v1/ui/desk-notes", uiReadAllowedClient.capabilityToken, nil, &HavenDeskNotesResponse{}, nil); err != nil {
		t.Fatalf("desk notes with ui.read: %v", err)
	}
	if err := uiReadAllowedClient.doJSON(context.Background(), http.MethodGet, "/v1/ui/presence", uiReadAllowedClient.capabilityToken, nil, &HavenPresenceResponse{}, nil); err != nil {
		t.Fatalf("presence with ui.read: %v", err)
	}
	if err := uiReadAllowedClient.doJSON(context.Background(), http.MethodGet, "/v1/ui/morph-sleep", uiReadAllowedClient.capabilityToken, nil, &HavenMorphSleepResponse{}, nil); err != nil {
		t.Fatalf("morph sleep with ui.read: %v", err)
	}

	uiWriteDeniedClient := NewClient(client.socketPath)
	uiWriteDeniedClient.ConfigureSession("haven", "haven-ui-write-denied", []string{controlCapabilityUIRead})
	if _, err := uiWriteDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied ui.write token: %v", err)
	}
	if err := uiWriteDeniedClient.doJSON(context.Background(), http.MethodPost, "/v1/ui/desk-notes/dismiss", uiWriteDeniedClient.capabilityToken, HavenDeskNoteDismissRequest{NoteID: "missing"}, &map[string]interface{}{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected ui.write scope denial for desk-note dismiss, got %v", err)
	}
}

func TestHavenSettingsRoutesRequireConfigScopes(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	pinTestProcessAsExpectedClient(t, server)

	readDeniedClient := NewClient(client.socketPath)
	readDeniedClient.ConfigureSession("haven", "haven-settings-read-denied", []string{"fs_list"})
	if _, err := readDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied config.read token: %v", err)
	}
	if err := readDeniedClient.doJSON(context.Background(), http.MethodGet, "/v1/settings/shell-dev", readDeniedClient.capabilityToken, nil, &havenShellDevResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected config.read scope denial for shell-dev settings, got %v", err)
	}
	if err := readDeniedClient.doJSON(context.Background(), http.MethodGet, "/v1/settings/idle", readDeniedClient.capabilityToken, nil, &havenIdleSettingsResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected config.read scope denial for idle settings, got %v", err)
	}

	readAllowedClient := NewClient(client.socketPath)
	readAllowedClient.ConfigureSession("haven", "haven-settings-read-allowed", []string{controlCapabilityConfigRead})
	if _, err := readAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure config.read token: %v", err)
	}
	if err := readAllowedClient.doJSON(context.Background(), http.MethodGet, "/v1/settings/shell-dev", readAllowedClient.capabilityToken, nil, &havenShellDevResponse{}, nil); err != nil {
		t.Fatalf("shell-dev settings with config.read: %v", err)
	}
	if err := readAllowedClient.doJSON(context.Background(), http.MethodGet, "/v1/settings/idle", readAllowedClient.capabilityToken, nil, &havenIdleSettingsResponse{}, nil); err != nil {
		t.Fatalf("idle settings with config.read: %v", err)
	}

	writeDeniedClient := NewClient(client.socketPath)
	writeDeniedClient.ConfigureSession("haven", "haven-settings-write-denied", []string{controlCapabilityConfigRead})
	if _, err := writeDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied config.write token: %v", err)
	}
	if err := writeDeniedClient.doJSON(context.Background(), http.MethodPost, "/v1/settings/shell-dev", writeDeniedClient.capabilityToken, havenShellDevUpdateRequest{Enabled: true}, &havenShellDevResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected config.write scope denial for shell-dev update, got %v", err)
	}
	if err := writeDeniedClient.doJSON(context.Background(), http.MethodPost, "/v1/settings/idle", writeDeniedClient.capabilityToken, havenIdleSettingsUpdateRequest{IdleEnabled: false, AmbientEnabled: false}, &havenIdleSettingsResponse{}, nil); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected config.write scope denial for idle settings update, got %v", err)
	}

	writeAllowedClient := NewClient(client.socketPath)
	writeAllowedClient.ConfigureSession("haven", "haven-settings-write-allowed", []string{controlCapabilityConfigWrite})
	if _, err := writeAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure config.write token: %v", err)
	}
	if err := writeAllowedClient.doJSON(context.Background(), http.MethodPost, "/v1/settings/shell-dev", writeAllowedClient.capabilityToken, havenShellDevUpdateRequest{Enabled: true}, &havenShellDevResponse{}, nil); err != nil {
		t.Fatalf("shell-dev settings update with config.write: %v", err)
	}
	if err := writeAllowedClient.doJSON(context.Background(), http.MethodPost, "/v1/settings/idle", writeAllowedClient.capabilityToken, havenIdleSettingsUpdateRequest{IdleEnabled: false, AmbientEnabled: false}, &havenIdleSettingsResponse{}, nil); err != nil {
		t.Fatalf("idle settings update with config.write: %v", err)
	}
}

func TestUIOperatorMountWriteGrantRouteRequiresScopeAndFreshApprovalForRenewal(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}

	server.mu.Lock()
	controlSession := server.sessions[client.controlSessionID]
	controlSession.OperatorMountPaths = []string{resolvedRepoRoot}
	controlSession.OperatorMountWriteGrants = map[string]time.Time{
		resolvedRepoRoot: server.now().UTC().Add(time.Hour),
	}
	server.sessions[client.controlSessionID] = controlSession
	server.mu.Unlock()

	deniedClient := NewClient(client.socketPath)
	deniedClient.ConfigureSession("test-actor", "operator-mount-grant-denied", []string{"fs_write"})
	if _, err := deniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied operator mount grant token: %v", err)
	}
	if _, err := deniedClient.UpdateUIOperatorMountWriteGrant(context.Background(), UIOperatorMountWriteGrantUpdateRequest{
		RootPath: resolvedRepoRoot,
		Action:   OperatorMountWriteGrantActionRevoke,
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected operator mount grant scope denial, got %v", err)
	}

	if _, err := client.UpdateUIOperatorMountWriteGrant(context.Background(), UIOperatorMountWriteGrantUpdateRequest{
		RootPath: resolvedRepoRoot,
		Action:   OperatorMountWriteGrantActionRenew,
	}); err == nil || !strings.Contains(err.Error(), DenialCodeApprovalRequired) {
		t.Fatalf("expected renew to require fresh approval, got %v", err)
	}
}

func TestNewServerRejectsSocketPathOutsideAllowedRoots(t *testing.T) {
	repoRoot := t.TempDir()

	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir policy dir: %v", err)
	}
	if err := os.WriteFile(policyPath, []byte(loopgatePolicyYAML(false)), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	writeTestMorphlingClassPolicy(t, repoRoot)

	socketPath := filepath.Join(repoRoot, "loopgate.sock")
	if _, err := NewServer(repoRoot, socketPath); err == nil || !strings.Contains(err.Error(), "outside allowed runtime roots") {
		t.Fatalf("expected socket path validation error, got %v", err)
	}
}

func TestNewServerAllowsSocketPathUnderRepoRuntime(t *testing.T) {
	repoRoot := t.TempDir()

	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir policy dir: %v", err)
	}
	if err := os.WriteFile(policyPath, []byte(loopgatePolicyYAML(false)), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	writeTestMorphlingClassPolicy(t, repoRoot)

	socketPath := filepath.Join(repoRoot, "runtime", "memorybench-loopgate.sock")
	if _, err := NewServer(repoRoot, socketPath); err != nil {
		t.Fatalf("expected repo runtime socket path to be accepted, got %v", err)
	}
}

func TestServeRejectsDirectorySocketPathWithoutRemovingIt(t *testing.T) {
	repoRoot := t.TempDir()

	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir policy dir: %v", err)
	}
	if err := os.WriteFile(policyPath, []byte(loopgatePolicyYAML(false)), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	writeTestMorphlingClassPolicy(t, repoRoot)

	socketPath := filepath.Join(os.TempDir(), "loopgate-dir-target.sock")
	if err := os.RemoveAll(socketPath); err != nil {
		t.Fatalf("clear stale socket path: %v", err)
	}
	if err := os.MkdirAll(socketPath, 0o700); err != nil {
		t.Fatalf("mkdir socket path directory: %v", err)
	}
	defer func() { _ = os.RemoveAll(socketPath) }()

	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if err := server.Serve(context.Background()); err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("expected directory socket path error, got %v", err)
	}
	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("expected socket path directory to remain after failed serve, got %v", err)
	}
}
