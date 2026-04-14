package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/model"
	"loopgate/internal/secrets"
)

func testProvider(t *testing.T, serverURL string) *Provider {
	t.Helper()
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	provider, err := NewProvider(Config{
		BaseURL:         serverURL,
		ModelName:       "claude-sonnet-4-5",
		Temperature:     0,
		MaxOutputTokens: 256,
		Timeout:         5 * time.Second,
		APIKeyRef: secrets.SecretRef{
			ID:          "test",
			Backend:     secrets.BackendEnv,
			AccountName: "ANTHROPIC_API_KEY",
			Scope:       "model_inference",
		},
		SecretStore: secrets.NewEnvSecretStore(),
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	return provider
}

func testPersona() config.Persona {
	p := config.Persona{}
	p.Name = "Test"
	p.Description = "test"
	p.Values = []string{"test"}
	p.Personality.Helpfulness = "high"
	p.Personality.Honesty = "high"
	p.Personality.SafetyMindset = "high"
	p.Personality.SecurityMindset = "high"
	p.Personality.Directness = "high"
	p.Personality.Warmth = "medium"
	p.Personality.Humor = "low"
	p.Personality.Pragmatism = "high"
	p.Personality.Skepticism = "high"
	p.Communication.Tone = "calm"
	p.Communication.Verbosity = "low"
	p.Communication.ExplanationDepth = "adaptive"
	p.Trust.TreatModelOutputAsUntrusted = true
	p.Trust.TreatToolOutputAsUntrusted = true
	p.Trust.TreatFileContentAsUntrusted = true
	p.Trust.TreatEnvironmentAsUntrusted = true
	p.HallucinationControls.AdmitUnknowns = true
	p.HallucinationControls.RefuseToInventFacts = true
	return p
}

func TestProvider_StructuredToolUseBlock_Extracted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "claude-sonnet-4-5",
			"stop_reason": "tool_use",
			"content": [
				{"type": "text", "text": "Let me read that file."},
				{"type": "tool_use", "id": "toolu_01abc", "name": "fs_read", "input": {"path": "README.md"}}
			],
			"usage": {"input_tokens": 100, "output_tokens": 50}
		}`))
	}))
	defer server.Close()

	provider := testProvider(t, server.URL)
	resp, err := provider.Generate(context.Background(), model.Request{
		Persona:     testPersona(),
		SessionID:   "s-test",
		TurnCount:   1,
		UserMessage: "Read the readme",
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if resp.AssistantText != "Let me read that file." {
		t.Errorf("unexpected assistant text: %q", resp.AssistantText)
	}
	if len(resp.ToolUseBlocks) != 1 {
		t.Fatalf("expected 1 tool-use block, got %d", len(resp.ToolUseBlocks))
	}

	block := resp.ToolUseBlocks[0]
	if block.ID != "toolu_01abc" {
		t.Errorf("expected ID 'toolu_01abc', got %q", block.ID)
	}
	if block.Name != "fs_read" {
		t.Errorf("expected name 'fs_read', got %q", block.Name)
	}
	if block.Input["path"] != "README.md" {
		t.Errorf("expected path 'README.md', got %q", block.Input["path"])
	}
	if resp.FinishReason != "tool_use" {
		t.Errorf("expected finish_reason 'tool_use', got %q", resp.FinishReason)
	}
}

func TestProvider_ToolUseOnlyResponse_NoTextRequired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "claude-sonnet-4-5",
			"stop_reason": "tool_use",
			"content": [
				{"type": "tool_use", "id": "toolu_01xyz", "name": "fs_list", "input": {"path": "."}}
			],
			"usage": {"input_tokens": 80, "output_tokens": 30}
		}`))
	}))
	defer server.Close()

	provider := testProvider(t, server.URL)
	resp, err := provider.Generate(context.Background(), model.Request{
		Persona:     testPersona(),
		SessionID:   "s-test",
		TurnCount:   1,
		UserMessage: "List files",
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if resp.AssistantText != "" {
		t.Errorf("expected empty assistant text for tool-only response, got %q", resp.AssistantText)
	}
	if len(resp.ToolUseBlocks) != 1 {
		t.Fatalf("expected 1 tool-use block, got %d", len(resp.ToolUseBlocks))
	}
	if resp.ToolUseBlocks[0].Name != "fs_list" {
		t.Errorf("expected name 'fs_list', got %q", resp.ToolUseBlocks[0].Name)
	}
}

func TestProvider_TextOnlyResponse_NoToolBlocks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "claude-sonnet-4-5",
			"stop_reason": "end_turn",
			"content": [
				{"type": "text", "text": "Hello, how can I help?"}
			],
			"usage": {"input_tokens": 50, "output_tokens": 20}
		}`))
	}))
	defer server.Close()

	provider := testProvider(t, server.URL)
	resp, err := provider.Generate(context.Background(), model.Request{
		Persona:     testPersona(),
		SessionID:   "s-test",
		TurnCount:   1,
		UserMessage: "Hi",
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if resp.AssistantText != "Hello, how can I help?" {
		t.Errorf("unexpected assistant text: %q", resp.AssistantText)
	}
	if len(resp.ToolUseBlocks) != 0 {
		t.Errorf("expected 0 tool-use blocks for text-only response, got %d", len(resp.ToolUseBlocks))
	}
}

func TestProvider_NativeToolsSentInRequest(t *testing.T) {
	var capturedRequest messagesRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "claude-sonnet-4-5",
			"stop_reason": "end_turn",
			"content": [{"type": "text", "text": "ok"}],
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`))
	}))
	defer server.Close()

	provider := testProvider(t, server.URL)
	_, err := provider.Generate(context.Background(), model.Request{
		Persona:     testPersona(),
		SessionID:   "s-test",
		TurnCount:   1,
		UserMessage: "test",
		NativeToolDefs: []model.NativeToolDef{
			{
				Name:        "fs_read",
				Description: "Read a file",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "file path",
						},
					},
					"required": []string{"path"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if len(capturedRequest.Tools) != 1 {
		t.Fatalf("expected 1 tool in request, got %d", len(capturedRequest.Tools))
	}
	if capturedRequest.Tools[0].Name != "fs_read" {
		t.Errorf("expected tool name 'fs_read', got %q", capturedRequest.Tools[0].Name)
	}
	if capturedRequest.Tools[0].CacheControl == nil || capturedRequest.Tools[0].CacheControl.Type != "ephemeral" {
		t.Fatalf("expected ephemeral cache_control on tool, got %#v", capturedRequest.Tools[0].CacheControl)
	}
}

func TestProvider_OmitsTrailingEmptyUserMessageAfterToolRound(t *testing.T) {
	var capturedRequest messagesRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "claude-sonnet-4-5",
			"stop_reason": "end_turn",
			"content": [{"type": "text", "text": "ok"}],
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`))
	}))
	defer server.Close()

	provider := testProvider(t, server.URL)
	_, err := provider.Generate(context.Background(), model.Request{
		Persona:     testPersona(),
		SessionID:   "s-test",
		TurnCount:   2,
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

	if len(capturedRequest.Messages) == 0 {
		t.Fatal("expected captured messages")
	}
	// Dotted capability ids must not appear in outbound tool_use blocks (Anthropic name pattern).
	var assistantToolUse *contentPart
	for _, msg := range capturedRequest.Messages {
		if msg.Role != "assistant" {
			continue
		}
		for i := range msg.Content {
			if msg.Content[i].Type == "tool_use" {
				assistantToolUse = &msg.Content[i]
				break
			}
		}
		if assistantToolUse != nil {
			break
		}
	}
	if assistantToolUse == nil {
		t.Fatal("expected assistant message with tool_use block")
	}
	if assistantToolUse.Name != "host_folder_list" {
		t.Fatalf("expected sanitized tool_use name host_folder_list, got %q", assistantToolUse.Name)
	}
	if capturedRequest.Tools[0].Name != "host_folder_list" {
		t.Fatalf("expected tools[0].name host_folder_list, got %q", capturedRequest.Tools[0].Name)
	}
	lastMessage := capturedRequest.Messages[len(capturedRequest.Messages)-1]
	if lastMessage.Role == "user" && len(lastMessage.Content) == 1 && lastMessage.Content[0].Type == "text" && lastMessage.Content[0].Text == "" {
		t.Fatalf("expected no trailing empty user text message after tool round, got %#v", lastMessage)
	}
}

func TestProvider_NoNativeTools_OmittedFromRequest(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "claude-sonnet-4-5",
			"stop_reason": "end_turn",
			"content": [{"type": "text", "text": "ok"}],
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`))
	}))
	defer server.Close()

	provider := testProvider(t, server.URL)
	_, err := provider.Generate(context.Background(), model.Request{
		Persona:     testPersona(),
		SessionID:   "s-test",
		TurnCount:   1,
		UserMessage: "test",
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if _, hasTools := capturedBody["tools"]; hasTools {
		t.Error("expected 'tools' to be omitted from request when no NativeToolDefs provided")
	}
}

func TestProvider_MultipleToolUseBlocks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "claude-sonnet-4-5",
			"stop_reason": "tool_use",
			"content": [
				{"type": "tool_use", "id": "toolu_01", "name": "fs_list", "input": {"path": "."}},
				{"type": "tool_use", "id": "toolu_02", "name": "fs_read", "input": {"path": "main.go"}}
			],
			"usage": {"input_tokens": 100, "output_tokens": 60}
		}`))
	}))
	defer server.Close()

	provider := testProvider(t, server.URL)
	resp, err := provider.Generate(context.Background(), model.Request{
		Persona:     testPersona(),
		SessionID:   "s-test",
		TurnCount:   1,
		UserMessage: "list and read",
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if len(resp.ToolUseBlocks) != 2 {
		t.Fatalf("expected 2 tool-use blocks, got %d", len(resp.ToolUseBlocks))
	}
	if resp.ToolUseBlocks[0].Name != "fs_list" {
		t.Errorf("expected first block 'fs_list', got %q", resp.ToolUseBlocks[0].Name)
	}
	if resp.ToolUseBlocks[1].Name != "fs_read" {
		t.Errorf("expected second block 'fs_read', got %q", resp.ToolUseBlocks[1].Name)
	}
}

func TestProvider_MixedTextAndToolUse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "claude-sonnet-4-5",
			"stop_reason": "tool_use",
			"content": [
				{"type": "text", "text": "I'll check the directory for you."},
				{"type": "tool_use", "id": "toolu_mixed", "name": "fs_list", "input": {"path": "src/"}}
			],
			"usage": {"input_tokens": 80, "output_tokens": 40}
		}`))
	}))
	defer server.Close()

	provider := testProvider(t, server.URL)
	resp, err := provider.Generate(context.Background(), model.Request{
		Persona:     testPersona(),
		SessionID:   "s-test",
		TurnCount:   1,
		UserMessage: "list src",
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if resp.AssistantText != "I'll check the directory for you." {
		t.Errorf("unexpected assistant text: %q", resp.AssistantText)
	}
	if len(resp.ToolUseBlocks) != 1 {
		t.Fatalf("expected 1 tool-use block, got %d", len(resp.ToolUseBlocks))
	}
	if resp.ToolUseBlocks[0].Name != "fs_list" {
		t.Errorf("expected name 'fs_list', got %q", resp.ToolUseBlocks[0].Name)
	}
}

func TestExtractToolUseBlocks_PlanJSONArrayCoercedToJSONText(t *testing.T) {
	blocks := extractToolUseBlocks([]contentPart{{
		Type:  "tool_use",
		ID:    "toolu_plan",
		Name:  "host.organize.plan",
		Input: json.RawMessage(`{"folder_name":"downloads","plan_json":[{"kind":"mkdir","path":"x"}]}`),
	}})
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks", len(blocks))
	}
	planJSON := blocks[0].Input["plan_json"]
	if len(planJSON) < 2 || planJSON[0] != '[' {
		t.Fatalf("expected JSON array text, got %q", planJSON)
	}
	var ops []map[string]string
	if err := json.Unmarshal([]byte(planJSON), &ops); err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || ops[0]["kind"] != "mkdir" {
		t.Fatalf("ops %#v", ops)
	}
}
