package loopgate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
	modelpkg "loopgate/internal/model"
	modelruntime "loopgate/internal/modelruntime"
)

func TestModelReply_UsesLoopgateRuntime(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	var modelResponseAuditEvent ledger.Event

	appendAuditEvent := server.appendAuditEvent
	server.appendAuditEvent = func(path string, ledgerEvent ledger.Event) error {
		if ledgerEvent.Type == "model.response" {
			modelResponseAuditEvent = ledgerEvent
		}
		return appendAuditEvent(path, ledgerEvent)
	}

	modelResponse, err := client.ModelReply(context.Background(), modelpkg.Request{
		Persona:     config.Persona{Name: "Loopgate"},
		Policy:      status.Policy,
		SessionID:   "session-model",
		TurnCount:   1,
		UserMessage: "check the status",
	})
	if err != nil {
		t.Fatalf("model reply: %v", err)
	}
	if modelResponse.ProviderName != "stub" {
		t.Fatalf("expected stub provider, got %#v", modelResponse)
	}
	if !strings.Contains(modelResponse.AssistantText, "check the status") {
		t.Fatalf("unexpected assistant text: %#v", modelResponse)
	}
	requiredTimingFields := []string{
		"request_verify_ms",
		"runtime_config_load_ms",
		"model_client_init_ms",
		"model_generate_ms",
		"prompt_compile_ms",
		"secret_resolve_ms",
		"provider_roundtrip_ms",
		"response_decode_ms",
		"total_generate_ms",
	}
	for _, timingField := range requiredTimingFields {
		if _, found := modelResponseAuditEvent.Data[timingField]; !found {
			t.Fatalf("expected timing field %q on model.response audit event %#v", timingField, modelResponseAuditEvent)
		}
	}
}

func TestModelReply_UsesDedicatedModelTimeoutInsteadOfDefaultControlPlaneTimeout(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	client.defaultRequestTimeout = 10 * time.Millisecond
	client.modelReplyTimeout = 300 * time.Millisecond
	server.newModelClientFromConfig = func(runtimeConfig modelruntime.Config) (*modelpkg.Client, modelruntime.Config, error) {
		return modelpkg.NewClient(delayedModelProvider{delay: 100 * time.Millisecond}), modelruntime.Config{
			ProviderName: "delayed",
			ModelName:    "delayed",
			Timeout:      100 * time.Millisecond,
		}, nil
	}

	modelResponse, err := client.ModelReply(context.Background(), modelpkg.Request{
		Persona:     config.Persona{Name: "Loopgate"},
		Policy:      status.Policy,
		SessionID:   "session-model",
		TurnCount:   1,
		UserMessage: "check the status",
	})
	if err != nil {
		t.Fatalf("model reply with dedicated timeout: %v", err)
	}
	if modelResponse.ProviderName != "delayed" {
		t.Fatalf("unexpected delayed model response: %#v", modelResponse)
	}
}

func TestModelReply_FailsClosedWhenAuditUnavailable(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	appendAuditEvent := server.appendAuditEvent
	server.appendAuditEvent = func(path string, ledgerEvent ledger.Event) error {
		if ledgerEvent.Type == "model.response" {
			return errors.New("audit append unavailable")
		}
		return appendAuditEvent(path, ledgerEvent)
	}

	_, err := client.ModelReply(context.Background(), modelpkg.Request{
		Persona:     config.Persona{Name: "Loopgate"},
		Policy:      status.Policy,
		SessionID:   "session-model",
		TurnCount:   1,
		UserMessage: "check the status",
	})
	if err == nil || !strings.Contains(err.Error(), DenialCodeAuditUnavailable) {
		t.Fatalf("expected audit-unavailable model failure, got %v", err)
	}
}

func TestModelReply_LogsTimingOnModelError(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	var modelErrorAuditEvent ledger.Event

	server.newModelClientFromConfig = func(runtimeConfig modelruntime.Config) (*modelpkg.Client, modelruntime.Config, error) {
		return modelpkg.NewClient(failingModelProvider{}), modelruntime.Config{
			ProviderName: "failing",
			ModelName:    "failing",
			Timeout:      100 * time.Millisecond,
		}, nil
	}

	appendAuditEvent := server.appendAuditEvent
	server.appendAuditEvent = func(path string, ledgerEvent ledger.Event) error {
		if ledgerEvent.Type == "model.error" {
			modelErrorAuditEvent = ledgerEvent
		}
		return appendAuditEvent(path, ledgerEvent)
	}

	_, err := client.ModelReply(context.Background(), modelpkg.Request{
		Persona:     config.Persona{Name: "Loopgate"},
		Policy:      status.Policy,
		SessionID:   "session-model",
		TurnCount:   1,
		UserMessage: "check the status",
	})
	if err == nil || !strings.Contains(err.Error(), "synthetic model failure") {
		t.Fatalf("expected model failure, got %v", err)
	}

	requiredTimingFields := []string{
		"request_verify_ms",
		"runtime_config_load_ms",
		"model_client_init_ms",
		"model_generate_ms",
		"prompt_compile_ms",
		"secret_resolve_ms",
		"provider_roundtrip_ms",
		"response_decode_ms",
		"total_generate_ms",
	}
	for _, timingField := range requiredTimingFields {
		if _, found := modelErrorAuditEvent.Data[timingField]; !found {
			t.Fatalf("expected timing field %q on model.error audit event %#v", timingField, modelErrorAuditEvent)
		}
	}
}

func TestValidateModelConfig_UsesLoopgateRuntime(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	validatedConfig, err := client.ValidateModelConfig(context.Background(), modelruntime.Config{
		ProviderName: "stub",
		ModelName:    "stub",
	})
	if err != nil {
		t.Fatalf("validate model config: %v", err)
	}
	if validatedConfig.ProviderName != "stub" || validatedConfig.ModelName != "stub" {
		t.Fatalf("unexpected validated config: %#v", validatedConfig)
	}
}

func TestValidateModelConfig_FailsClosedWhenAuditUnavailable(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	appendAuditEvent := server.appendAuditEvent
	server.appendAuditEvent = func(path string, ledgerEvent ledger.Event) error {
		if ledgerEvent.Type == "model.config_validated" {
			return errors.New("audit append unavailable")
		}
		return appendAuditEvent(path, ledgerEvent)
	}

	_, err := client.ValidateModelConfig(context.Background(), modelruntime.Config{
		ProviderName: "stub",
		ModelName:    "stub",
	})
	if err == nil || !strings.Contains(err.Error(), DenialCodeAuditUnavailable) {
		t.Fatalf("expected audit-unavailable model validation failure, got %v", err)
	}
}

func TestDelegatedSessionClient_UsesProvidedCredentialsWithoutSessionOpen(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	client.mu.Lock()
	delegatedConfig := DelegatedSessionConfig{
		ControlSessionID: client.controlSessionID,
		CapabilityToken:  client.capabilityToken,
		ApprovalToken:    client.approvalToken,
		SessionMACKey:    client.sessionMACKey,
		ExpiresAt:        client.tokenExpiresAt,
	}
	client.mu.Unlock()

	delegatedClient, err := NewClientFromDelegatedSession(client.socketPath, delegatedConfig)
	if err != nil {
		t.Fatalf("new delegated client: %v", err)
	}

	uiStatus, err := delegatedClient.UIStatus(context.Background())
	if err != nil {
		t.Fatalf("delegated ui status: %v", err)
	}
	if uiStatus.ControlSessionID != delegatedConfig.ControlSessionID {
		t.Fatalf("expected delegated control session id %q, got %#v", delegatedConfig.ControlSessionID, uiStatus)
	}
}

func TestDelegatedSessionClient_ExpiredCredentialsFailClosed(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	client.mu.Lock()
	delegatedConfig := DelegatedSessionConfig{
		ControlSessionID: client.controlSessionID,
		CapabilityToken:  client.capabilityToken,
		ApprovalToken:    client.approvalToken,
		SessionMACKey:    client.sessionMACKey,
		ExpiresAt:        client.tokenExpiresAt,
	}
	client.mu.Unlock()

	delegatedClient, err := NewClientFromDelegatedSession(client.socketPath, delegatedConfig)
	if err != nil {
		t.Fatalf("new delegated client: %v", err)
	}

	delegatedClient.mu.Lock()
	delegatedClient.tokenExpiresAt = time.Now().UTC().Add(-1 * time.Minute)
	delegatedClient.mu.Unlock()

	_, err = delegatedClient.UIStatus(context.Background())
	if !errors.Is(err, ErrDelegatedSessionRefreshRequired) {
		t.Fatalf("expected delegated refresh-required error, got %v", err)
	}
}

func TestDelegatedSessionClient_RefreshSoonStillAllowsRequests(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	client.mu.Lock()
	delegatedConfig := DelegatedSessionConfig{
		ControlSessionID: client.controlSessionID,
		CapabilityToken:  client.capabilityToken,
		ApprovalToken:    client.approvalToken,
		SessionMACKey:    client.sessionMACKey,
		ExpiresAt:        time.Now().UTC().Add(90 * time.Second),
	}
	client.mu.Unlock()

	delegatedClient, err := NewClientFromDelegatedSession(client.socketPath, delegatedConfig)
	if err != nil {
		t.Fatalf("new delegated client: %v", err)
	}

	state, _, ok := delegatedClient.DelegatedSessionHealth(time.Now().UTC())
	if !ok {
		t.Fatal("expected delegated session health to be available")
	}
	if state != DelegatedSessionStateRefreshSoon {
		t.Fatalf("expected refresh_soon state, got %s", state)
	}

	if _, err := delegatedClient.UIStatus(context.Background()); err != nil {
		t.Fatalf("refresh-soon delegated client should still work before expiry: %v", err)
	}
}
