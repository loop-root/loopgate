package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/model"
	"loopgate/internal/secrets"
)

func TestProvider_GenerateUsesSecretStoreAndAnthropicProtocol(t *testing.T) {
	var capturedAPIKey string
	var capturedVersion string
	var capturedPath string
	var capturedRequest messagesRequest

	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		capturedAPIKey = request.Header.Get("x-api-key")
		capturedVersion = request.Header.Get("anthropic-version")
		capturedPath = request.URL.Path

		if err := json.NewDecoder(request.Body).Decode(&capturedRequest); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"model": "claude-sonnet-4-5",
			"stop_reason": "end_turn",
			"content": [{"type": "text", "text": "hello from anthropic"}],
			"usage": {"input_tokens": 12, "output_tokens": 8}
		}`))
	}))
	defer testServer.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-anthropic-key")

	provider, err := NewProvider(Config{
		BaseURL:         testServer.URL,
		ModelName:       "claude-sonnet-4-5",
		Temperature:     0,
		MaxOutputTokens: 256,
		Timeout:         5 * time.Second,
		APIKeyRef: secrets.SecretRef{
			ID:          "anthropic-test",
			Backend:     secrets.BackendEnv,
			AccountName: "ANTHROPIC_API_KEY",
			Scope:       "model_inference",
		},
		SecretStore: secrets.NewEnvSecretStore(),
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	persona := config.Persona{}
	persona.Name = "Morph"
	persona.Description = "A helpful assistant."
	persona.Values = []string{"honesty", "security"}
	persona.Personality.Helpfulness = "high"
	persona.Personality.Honesty = "strict"
	persona.Personality.SafetyMindset = "high"
	persona.Personality.SecurityMindset = "high"
	persona.Personality.Directness = "high"
	persona.Personality.Warmth = "medium"
	persona.Personality.Humor = "low"
	persona.Personality.Pragmatism = "high"
	persona.Personality.Skepticism = "high"
	persona.Communication.Tone = "calm, direct"
	persona.Communication.Verbosity = "adaptive"
	persona.Communication.ExplanationDepth = "adaptive"
	persona.Trust.TreatModelOutputAsUntrusted = true
	persona.Trust.TreatToolOutputAsUntrusted = true
	persona.Trust.TreatFileContentAsUntrusted = true
	persona.Trust.TreatEnvironmentAsUntrusted = true
	persona.Trust.RequireValidationBeforeUse = true
	persona.HallucinationControls.AdmitUnknowns = true
	persona.HallucinationControls.RefuseToInventFacts = true

	response, err := provider.Generate(context.Background(), model.Request{
		Persona:     persona,
		SessionID:   "s-test",
		TurnCount:   1,
		UserMessage: "Hello, Claude",
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if response.AssistantText != "hello from anthropic" {
		t.Fatalf("unexpected assistant text: %q", response.AssistantText)
	}
	if response.ProviderName != "anthropic" {
		t.Fatalf("unexpected provider name: %q", response.ProviderName)
	}
	if capturedAPIKey != "test-anthropic-key" {
		t.Fatalf("unexpected x-api-key header: %q", capturedAPIKey)
	}
	if capturedVersion != anthropicVersion {
		t.Fatalf("unexpected anthropic-version header: %q", capturedVersion)
	}
	if capturedPath != "/messages" {
		t.Fatalf("unexpected request path: %q", capturedPath)
	}
	if capturedRequest.Model != "claude-sonnet-4-5" {
		t.Fatalf("unexpected request model: %#v", capturedRequest)
	}
	var systemBlocks []struct {
		Type         string            `json:"type"`
		Text         string            `json:"text"`
		CacheControl map[string]string `json:"cache_control"`
	}
	if err := json.Unmarshal(capturedRequest.System, &systemBlocks); err != nil {
		t.Fatalf("decode system json: %v", err)
	}
	if len(systemBlocks) != 1 || systemBlocks[0].Type != "text" || strings.TrimSpace(systemBlocks[0].Text) == "" {
		t.Fatalf("expected one cached system text block, got %#v", systemBlocks)
	}
	if systemBlocks[0].CacheControl["type"] != "ephemeral" {
		t.Fatalf("expected ephemeral system cache_control, got %#v", systemBlocks[0].CacheControl)
	}
	if len(capturedRequest.Messages) != 1 || capturedRequest.Messages[0].Role != "user" {
		t.Fatalf("unexpected request messages: %#v", capturedRequest.Messages)
	}
	if response.RequestPayloadBytes <= 0 {
		t.Fatalf("expected RequestPayloadBytes > 0, got %d", response.RequestPayloadBytes)
	}
}

func TestProvider_GenerateRetriesOn429(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping retry sleep in short mode")
	}
	var callCount int
	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		callCount++
		if callCount == 1 {
			writer.Header().Set("Retry-After", "1")
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = writer.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error"}}`))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"model": "claude-sonnet-4-5",
			"stop_reason": "end_turn",
			"content": [{"type": "text", "text": "after retry"}],
			"usage": {"input_tokens": 1, "output_tokens": 1}
		}`))
	}))
	defer testServer.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-anthropic-key")

	provider, err := NewProvider(Config{
		BaseURL:         testServer.URL,
		ModelName:       "claude-sonnet-4-5",
		Temperature:     0,
		MaxOutputTokens: 64,
		Timeout:         30 * time.Second,
		APIKeyRef: secrets.SecretRef{
			ID:          "anthropic-test",
			Backend:     secrets.BackendEnv,
			AccountName: "ANTHROPIC_API_KEY",
			Scope:       "model_inference",
		},
		SecretStore: secrets.NewEnvSecretStore(),
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	persona := config.Persona{Name: "Morph", Description: "test"}
	response, err := provider.Generate(context.Background(), model.Request{
		Persona:     persona,
		SessionID:   "s-retry",
		TurnCount:   1,
		UserMessage: "hi",
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 HTTP calls after 429 retry, got %d", callCount)
	}
	if response.AssistantText != "after retry" {
		t.Fatalf("unexpected text: %q", response.AssistantText)
	}
}
