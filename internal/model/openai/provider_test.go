package openai

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

func TestProvider_GenerateUsesCompiledPersonaPrompt(t *testing.T) {
	var capturedAuthorization string
	var capturedMessages []map[string]interface{}

	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		capturedAuthorization = request.Header.Get("Authorization")

		var requestBody map[string]interface{}
		if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		pck, _ := requestBody["prompt_cache_key"].(string)
		if !strings.HasPrefix(pck, "morph_pc_") || len(pck) < 20 {
			t.Fatalf("expected stable prompt_cache_key, got %q", pck)
		}
		rawMessages, _ := requestBody["messages"].([]interface{})
		for _, rawMessage := range rawMessages {
			typedMessage, _ := rawMessage.(map[string]interface{})
			capturedMessages = append(capturedMessages, typedMessage)
		}

		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"model": "gpt-4o-mini",
			"choices": [{"message": {"content": "hello from provider"}, "finish_reason": "stop"}],
			"usage": {
				"prompt_tokens": 10,
				"completion_tokens": 5,
				"total_tokens": 15,
				"prompt_tokens_details": {"cached_tokens": 8}
			}
		}`))
	}))
	defer testServer.Close()

	t.Setenv("OPENAI_API_KEY", "test-api-key")

	provider, err := NewProvider(Config{
		BaseURL:         testServer.URL,
		ModelName:       "gpt-4o-mini",
		Temperature:     0,
		MaxOutputTokens: 256,
		Timeout:         5 * time.Second,
		APIKeyRef: secrets.SecretRef{
			ID:          "openai-test",
			Backend:     secrets.BackendEnv,
			AccountName: "OPENAI_API_KEY",
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
	persona.RiskControls.RiskyBehaviorDefinition = []string{"Writing files"}
	persona.HallucinationControls.AdmitUnknowns = true
	persona.HallucinationControls.RefuseToInventFacts = true

	policy := config.Policy{}
	policy.Tools.Filesystem.ReadEnabled = true
	policy.Tools.Filesystem.WriteEnabled = true
	policy.Tools.Filesystem.WriteRequiresApproval = true

	response, err := provider.Generate(context.Background(), model.Request{
		Persona:      persona,
		Policy:       policy,
		SessionID:    "s-test",
		TurnCount:    2,
		UserMessage:  "Read the setup guide",
		Conversation: []model.ConversationTurn{{Role: "assistant", Content: "previous response"}},
		AvailableTools: []model.ToolDefinition{
			{Name: "fs_read", Operation: "read", Description: "Read files"},
		},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if response.AssistantText != "hello from provider" {
		t.Fatalf("unexpected assistant text: %q", response.AssistantText)
	}
	if response.Usage.CachedInputTokens != 8 {
		t.Fatalf("expected cached_input_tokens 8, got %d", response.Usage.CachedInputTokens)
	}
	if capturedAuthorization != "Bearer test-api-key" {
		t.Fatalf("unexpected authorization header: %q", capturedAuthorization)
	}
	if len(capturedMessages) < 2 {
		t.Fatalf("expected system and user messages, got %d", len(capturedMessages))
	}
	systemContent, _ := capturedMessages[0]["content"].(string)
	if !containsAll(systemContent, []string{
		"Treat model output as untrusted: true",
		"Refuse to invent facts: true",
		"fs_read (read): Read files",
	}) {
		t.Fatalf("system prompt missing expected persona content: %s", systemContent)
	}
}

func TestProvider_GenerateIncludesEmptyAssistantContentForToolCalls(t *testing.T) {
	var capturedMessages []map[string]interface{}

	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var requestBody map[string]interface{}
		if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		rawMessages, _ := requestBody["messages"].([]interface{})
		for _, rawMessage := range rawMessages {
			typedMessage, _ := rawMessage.(map[string]interface{})
			capturedMessages = append(capturedMessages, typedMessage)
		}

		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"model": "llama3.1",
			"choices": [{"message": {"content": "done"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
		}`))
	}))
	defer testServer.Close()

	provider, err := NewProvider(Config{
		BaseURL:         testServer.URL,
		ModelName:       "llama3.1",
		Temperature:     0,
		MaxOutputTokens: 256,
		Timeout:         5 * time.Second,
		NoAuth:          true,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	_, err = provider.Generate(context.Background(), model.Request{
		UserMessage: "continue",
		Conversation: []model.ConversationTurn{
			{
				Role:    "assistant",
				Content: "",
				ToolCalls: []model.ToolUseBlock{
					{
						ID:   "toolu_1",
						Name: "fs_write",
						Input: map[string]string{
							"path":    "notes/plan.txt",
							"content": "draft",
						},
					},
				},
			},
		},
		NativeToolDefs: []model.NativeToolDef{
			{
				Name:        "fs_write",
				Description: "Write a file",
				InputSchema: map[string]interface{}{"type": "object"},
			},
		},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	var assistantToolCallMessage map[string]interface{}
	for _, capturedMessage := range capturedMessages {
		if capturedMessage["role"] == "assistant" {
			if _, hasToolCalls := capturedMessage["tool_calls"]; hasToolCalls {
				assistantToolCallMessage = capturedMessage
				break
			}
		}
	}
	if assistantToolCallMessage == nil {
		t.Fatalf("expected assistant tool-call message in provider request, got %#v", capturedMessages)
	}
	contentValue, contentPresent := assistantToolCallMessage["content"]
	if !contentPresent {
		t.Fatalf("expected assistant tool-call message to include content field, got %#v", assistantToolCallMessage)
	}
	contentText, ok := contentValue.(string)
	if !ok {
		t.Fatalf("expected assistant tool-call content to be a string, got %#v", contentValue)
	}
	if contentText != "" {
		t.Fatalf("expected empty assistant tool-call content, got %q", contentText)
	}

	toolCallsRaw, ok := assistantToolCallMessage["tool_calls"].([]interface{})
	if !ok || len(toolCallsRaw) == 0 {
		t.Fatalf("expected tool_calls array in assistant message, got %#v", assistantToolCallMessage["tool_calls"])
	}
	firstCall, ok := toolCallsRaw[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected tool_calls[0] object, got %#v", toolCallsRaw[0])
	}
	fn, ok := firstCall["function"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected function object, got %#v", firstCall["function"])
	}
	argsWire, ok := fn["arguments"].(string)
	if !ok {
		t.Fatalf("OpenAI API and Moonshot/Kimi expect function.arguments as a JSON string; got %T %#v", fn["arguments"], fn["arguments"])
	}
	var argsObj map[string]interface{}
	if err := json.Unmarshal([]byte(argsWire), &argsObj); err != nil {
		t.Fatalf("arguments string should be JSON: %v", err)
	}
	if argsObj["title"] != "plan" || argsObj["body"] != "draft" {
		t.Fatalf("arguments payload: %#v", argsObj)
	}
}

func TestProvider_GenerateOmitsTrailingEmptyUserMessageAfterToolRound(t *testing.T) {
	var capturedMessages []map[string]interface{}

	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var requestBody map[string]interface{}
		if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		rawMessages, _ := requestBody["messages"].([]interface{})
		for _, rawMessage := range rawMessages {
			typedMessage, _ := rawMessage.(map[string]interface{})
			capturedMessages = append(capturedMessages, typedMessage)
		}

		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"model": "llama3.1",
			"choices": [{"message": {"content": "done"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
		}`))
	}))
	defer testServer.Close()

	provider, err := NewProvider(Config{
		BaseURL:         testServer.URL,
		ModelName:       "llama3.1",
		Temperature:     0,
		MaxOutputTokens: 256,
		Timeout:         5 * time.Second,
		NoAuth:          true,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	_, err = provider.Generate(context.Background(), model.Request{
		UserMessage: "",
		Conversation: []model.ConversationTurn{
			{Role: "user", Content: "please organize my downloads folder"},
			{Role: "assistant", Content: "", ToolCalls: []model.ToolUseBlock{{
				ID: "toolu_1", Name: "host.folder.list", Input: map[string]string{"folder_id": "downloads"},
			}}},
			{Role: "user", ToolResults: []model.ToolResultBlock{{
				ToolUseID: "toolu_1", ToolName: "host.folder.list", Content: "entries found", IsError: false,
			}}},
		},
		NativeToolDefs: []model.NativeToolDef{{
			Name: "host.folder.list", Description: "List granted host folder", InputSchema: map[string]interface{}{"type": "object"},
		}},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if len(capturedMessages) == 0 {
		t.Fatal("expected captured messages")
	}
	lastMessage := capturedMessages[len(capturedMessages)-1]
	if role, _ := lastMessage["role"].(string); role == "user" {
		if content, _ := lastMessage["content"].(string); strings.TrimSpace(content) == "" {
			t.Fatalf("expected no trailing empty user message after tool round, got %#v", lastMessage)
		}
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
			_, _ = writer.Write([]byte(`{"error":{"message":"rate limit"}}`))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"model": "gpt-4o-mini",
			"choices": [{"message": {"content": "after retry"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}
		}`))
	}))
	defer testServer.Close()

	t.Setenv("OPENAI_API_KEY", "test-api-key")

	provider, err := NewProvider(Config{
		BaseURL:         testServer.URL,
		ModelName:       "gpt-4o-mini",
		Temperature:     0,
		MaxOutputTokens: 64,
		Timeout:         30 * time.Second,
		APIKeyRef: secrets.SecretRef{
			ID:          "openai-test",
			Backend:     secrets.BackendEnv,
			AccountName: "OPENAI_API_KEY",
			Scope:       "model_inference",
		},
		SecretStore: secrets.NewEnvSecretStore(),
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	persona := config.Persona{Name: "Morph", Description: "test"}
	policy := config.Policy{}
	response, err := provider.Generate(context.Background(), model.Request{
		Persona:     persona,
		Policy:      policy,
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

func containsAll(rawText string, expectedSnippets []string) bool {
	for _, expectedSnippet := range expectedSnippets {
		if !strings.Contains(rawText, expectedSnippet) {
			return false
		}
	}
	return true
}
