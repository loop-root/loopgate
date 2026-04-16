package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"loopgate/internal/model"
	"loopgate/internal/prompt"
	"loopgate/internal/secrets"
)

const anthropicVersion = "2023-06-01"

// maxAnthropicRateLimitRetries is how many times we retry after HTTP 429 from Anthropic.
// Keep this at 1 so a rate-limited call fails within one backoff cycle (~30s max) rather
// than burning through the constrained tool-loop context window with multiple long waits.
const maxAnthropicRateLimitRetries = 1

func anthropicRateLimitWait(header http.Header, attemptZeroBased int) time.Duration {
	if header != nil {
		if retryAfter := strings.TrimSpace(header.Get("Retry-After")); retryAfter != "" {
			if sec, err := strconv.ParseInt(retryAfter, 10, 64); err == nil && sec > 0 && sec <= 120 {
				return time.Duration(sec) * time.Second
			}
		}
	}
	shift := attemptZeroBased
	if shift > 4 {
		shift = 4
	}
	wait := time.Duration(1<<uint(shift)) * time.Second
	if wait > 30*time.Second {
		wait = 30 * time.Second
	}
	if wait < time.Second {
		wait = time.Second
	}
	return wait
}

type Config struct {
	BaseURL         string
	ModelName       string
	Temperature     float64
	MaxOutputTokens int
	Timeout         time.Duration
	APIKeyRef       secrets.SecretRef
	SecretStore     secrets.SecretStore
	HTTPClient      *http.Client
}

type Provider struct {
	baseURL         string
	modelName       string
	temperature     float64
	maxOutputTokens int
	timeout         time.Duration
	apiKeyRef       secrets.SecretRef
	secretStore     secrets.SecretStore
	httpClient      *http.Client
	compiler        *prompt.Compiler
}

func NewProvider(config Config) (*Provider, error) {
	if strings.TrimSpace(config.BaseURL) == "" {
		return nil, fmt.Errorf("missing base URL")
	}
	if strings.TrimSpace(config.ModelName) == "" {
		return nil, fmt.Errorf("missing model name")
	}
	if config.SecretStore == nil {
		return nil, fmt.Errorf("missing secret store")
	}
	if err := config.APIKeyRef.Validate(); err != nil {
		return nil, fmt.Errorf("validate api key ref: %w", err)
	}

	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: config.Timeout}
	}

	return &Provider{
		baseURL:         strings.TrimRight(config.BaseURL, "/"),
		modelName:       config.ModelName,
		temperature:     config.Temperature,
		maxOutputTokens: config.MaxOutputTokens,
		timeout:         config.Timeout,
		apiKeyRef:       config.APIKeyRef,
		secretStore:     config.SecretStore,
		httpClient:      httpClient,
		compiler:        prompt.NewCompiler(),
	}, nil
}

// sanitizeAnthropicModelRequest maps capability/tool names to Anthropic-safe identifiers
// everywhere we send them (tool definitions and tool_use history). Callers may carry
// dotted registry names (e.g. host.folder.list) in NativeToolDefs or replayed ToolCalls.
func sanitizeAnthropicModelRequest(request model.Request) model.Request {
	for i := range request.NativeToolDefs {
		request.NativeToolDefs[i].Name = model.MessagesAPIToolName(request.NativeToolDefs[i].Name)
	}
	for i := range request.Conversation {
		for j := range request.Conversation[i].ToolCalls {
			request.Conversation[i].ToolCalls[j].Name = model.MessagesAPIToolName(request.Conversation[i].ToolCalls[j].Name)
		}
	}
	return request
}

func (provider *Provider) Generate(ctx context.Context, request model.Request) (model.Response, error) {
	generateStart := time.Now()

	// Anthropic validates every tool name (definitions and tool_use blocks in messages)
	// against ^[a-zA-Z0-9_-]{1,128}$. Native defs use dotted capability ids; messages must
	// echo the same API-safe names as in the tools[] array.
	request = sanitizeAnthropicModelRequest(request)

	promptCompileStart := time.Now()
	constrainedNativeTools := model.ConstrainedNativeToolsFromRuntimeFacts(request.RuntimeFacts)
	promptRuntimeFacts := model.StripInternalRuntimeFacts(request.RuntimeFacts)
	compiledPrompt, err := provider.compiler.Compile(prompt.Request{
		Persona:            request.Persona,
		Policy:             request.Policy,
		SessionID:          request.SessionID,
		TurnCount:          request.TurnCount,
		RememberedContext:  request.RememberedContext,
		Conversation:       model.ToPromptConversationTurns(request.Conversation),
		UserMessage:        request.UserMessage,
		AvailableTools:     model.ToPromptTools(request.AvailableTools),
		AvailableCommands:  model.ToPromptCommands(request.AvailableCommands),
		RuntimeFacts:       promptRuntimeFacts,
		HasNativeTools:     len(request.NativeToolDefs) > 0,
		ConstrainedToolUse: constrainedNativeTools,
	})
	if err != nil {
		return model.Response{}, fmt.Errorf("compile prompt: %w", err)
	}
	promptCompileDuration := time.Since(promptCompileStart)

	secretResolveStart := time.Now()
	apiKeyBytes, _, err := provider.secretStore.Get(ctx, provider.apiKeyRef)
	secretResolveDuration := time.Since(secretResolveStart)
	if err != nil {
		return model.Response{
			ProviderName: "anthropic",
			ModelName:    provider.modelName,
			Prompt: model.PromptMetadata{
				PersonaHash: compiledPrompt.Metadata.PersonaHash,
				PolicyHash:  compiledPrompt.Metadata.PolicyHash,
				PromptHash:  compiledPrompt.Metadata.PromptHash,
			},
			Timing: model.Timing{
				PromptCompile: promptCompileDuration,
				SecretResolve: secretResolveDuration,
				TotalGenerate: time.Since(generateStart),
			},
		}, fmt.Errorf("resolve model api key: %w", err)
	}

	systemText := buildSystemPrompt(compiledPrompt)
	systemJSON, err := buildAnthropicCachedSystemJSON(systemText)
	if err != nil {
		return model.Response{}, fmt.Errorf("build anthropic system payload: %w", err)
	}
	requestBody := messagesRequest{
		Model:       provider.modelName,
		System:      systemJSON,
		Messages:    buildMessages(compiledPrompt, request.Conversation, request.Attachments),
		MaxTokens:   provider.maxOutputTokens,
		Temperature: provider.temperature,
		Tools:       buildNativeTools(request.NativeToolDefs),
	}
	requestBytes, err := json.Marshal(requestBody)
	if err != nil {
		return model.Response{}, fmt.Errorf("marshal provider request: %w", err)
	}
	requestPayloadBytes := len(requestBytes)

	requestContext := ctx
	if provider.timeout > 0 {
		var cancel context.CancelFunc
		requestContext, cancel = context.WithTimeout(ctx, provider.timeout)
		defer cancel()
	}

	requestURL := provider.baseURL + "/messages"
	var providerRoundTripDuration time.Duration
	var responseBytes []byte
	var httpResponse *http.Response
	for attempt := 0; ; attempt++ {
		httpRequest, err := http.NewRequestWithContext(
			requestContext,
			http.MethodPost,
			requestURL,
			bytes.NewReader(requestBytes),
		)
		if err != nil {
			return model.Response{}, fmt.Errorf("build provider request: %w", err)
		}
		httpRequest.Header.Set("Content-Type", "application/json")
		httpRequest.Header.Set("x-api-key", string(apiKeyBytes))
		httpRequest.Header.Set("anthropic-version", anthropicVersion)

		providerRoundTripStart := time.Now()
		httpResponse, err = provider.httpClient.Do(httpRequest)
		providerRoundTripDuration += time.Since(providerRoundTripStart)
		if err != nil {
			return model.Response{
				ProviderName:        "anthropic",
				ModelName:           provider.modelName,
				RequestPayloadBytes: requestPayloadBytes,
				Prompt: model.PromptMetadata{
					PersonaHash: compiledPrompt.Metadata.PersonaHash,
					PolicyHash:  compiledPrompt.Metadata.PolicyHash,
					PromptHash:  compiledPrompt.Metadata.PromptHash,
				},
				Timing: model.Timing{
					PromptCompile:     promptCompileDuration,
					SecretResolve:     secretResolveDuration,
					ProviderRoundTrip: providerRoundTripDuration,
					TotalGenerate:     time.Since(generateStart),
				},
			}, fmt.Errorf("provider request failed: %w", err)
		}

		responseBytes, err = io.ReadAll(io.LimitReader(httpResponse.Body, 2*1024*1024))
		closeErr := httpResponse.Body.Close()
		if err != nil {
			return model.Response{
				ProviderName:        "anthropic",
				ModelName:           provider.modelName,
				RequestPayloadBytes: requestPayloadBytes,
				Prompt: model.PromptMetadata{
					PersonaHash: compiledPrompt.Metadata.PersonaHash,
					PolicyHash:  compiledPrompt.Metadata.PolicyHash,
					PromptHash:  compiledPrompt.Metadata.PromptHash,
				},
				Timing: model.Timing{
					PromptCompile:     promptCompileDuration,
					SecretResolve:     secretResolveDuration,
					ProviderRoundTrip: providerRoundTripDuration,
					TotalGenerate:     time.Since(generateStart),
				},
			}, fmt.Errorf("read provider response: %w", err)
		}
		if closeErr != nil {
			return model.Response{
				ProviderName:        "anthropic",
				ModelName:           provider.modelName,
				RequestPayloadBytes: requestPayloadBytes,
				Prompt: model.PromptMetadata{
					PersonaHash: compiledPrompt.Metadata.PersonaHash,
					PolicyHash:  compiledPrompt.Metadata.PolicyHash,
					PromptHash:  compiledPrompt.Metadata.PromptHash,
				},
				Timing: model.Timing{
					PromptCompile:     promptCompileDuration,
					SecretResolve:     secretResolveDuration,
					ProviderRoundTrip: providerRoundTripDuration,
					TotalGenerate:     time.Since(generateStart),
				},
			}, fmt.Errorf("close provider response body: %w", closeErr)
		}

		if httpResponse.StatusCode == http.StatusTooManyRequests && attempt < maxAnthropicRateLimitRetries {
			wait := anthropicRateLimitWait(httpResponse.Header, attempt)
			select {
			case <-time.After(wait):
			case <-requestContext.Done():
				return model.Response{
					ProviderName:        "anthropic",
					ModelName:           provider.modelName,
					RequestPayloadBytes: requestPayloadBytes,
					Prompt: model.PromptMetadata{
						PersonaHash: compiledPrompt.Metadata.PersonaHash,
						PolicyHash:  compiledPrompt.Metadata.PolicyHash,
						PromptHash:  compiledPrompt.Metadata.PromptHash,
					},
					Timing: model.Timing{
						PromptCompile:     promptCompileDuration,
						SecretResolve:     secretResolveDuration,
						ProviderRoundTrip: providerRoundTripDuration,
						TotalGenerate:     time.Since(generateStart),
					},
				}, fmt.Errorf("provider rate limited (429): %w", requestContext.Err())
			}
			continue
		}

		if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
			redactedBody := secrets.RedactText(string(responseBytes))
			return model.Response{
				ProviderName:        "anthropic",
				ModelName:           provider.modelName,
				RequestPayloadBytes: requestPayloadBytes,
				Prompt: model.PromptMetadata{
					PersonaHash: compiledPrompt.Metadata.PersonaHash,
					PolicyHash:  compiledPrompt.Metadata.PolicyHash,
					PromptHash:  compiledPrompt.Metadata.PromptHash,
				},
				Timing: model.Timing{
					PromptCompile:     promptCompileDuration,
					SecretResolve:     secretResolveDuration,
					ProviderRoundTrip: providerRoundTripDuration,
					TotalGenerate:     time.Since(generateStart),
				},
			}, fmt.Errorf("provider returned status %d: %s", httpResponse.StatusCode, redactedBody)
		}
		break
	}

	responseDecodeStart := time.Now()
	var messageResponse messagesResponse
	if err := json.Unmarshal(responseBytes, &messageResponse); err != nil {
		return model.Response{
			ProviderName:        "anthropic",
			ModelName:           provider.modelName,
			RequestPayloadBytes: requestPayloadBytes,
			Prompt: model.PromptMetadata{
				PersonaHash: compiledPrompt.Metadata.PersonaHash,
				PolicyHash:  compiledPrompt.Metadata.PolicyHash,
				PromptHash:  compiledPrompt.Metadata.PromptHash,
			},
			Timing: model.Timing{
				PromptCompile:     promptCompileDuration,
				SecretResolve:     secretResolveDuration,
				ProviderRoundTrip: providerRoundTripDuration,
				ResponseDecode:    time.Since(responseDecodeStart),
				TotalGenerate:     time.Since(generateStart),
			},
		}, fmt.Errorf("decode provider response: %w", err)
	}
	responseDecodeDuration := time.Since(responseDecodeStart)

	assistantText := extractAssistantText(messageResponse.Content)
	toolUseBlocks := extractToolUseBlocks(messageResponse.Content)

	// A response must contain either text or tool-use blocks (or both).
	if strings.TrimSpace(assistantText) == "" && len(toolUseBlocks) == 0 {
		return model.Response{
			ProviderName:        "anthropic",
			ModelName:           provider.modelName,
			RequestPayloadBytes: requestPayloadBytes,
			Prompt: model.PromptMetadata{
				PersonaHash: compiledPrompt.Metadata.PersonaHash,
				PolicyHash:  compiledPrompt.Metadata.PolicyHash,
				PromptHash:  compiledPrompt.Metadata.PromptHash,
			},
			Timing: model.Timing{
				PromptCompile:     promptCompileDuration,
				SecretResolve:     secretResolveDuration,
				ProviderRoundTrip: providerRoundTripDuration,
				ResponseDecode:    responseDecodeDuration,
				TotalGenerate:     time.Since(generateStart),
			},
		}, fmt.Errorf("provider response missing text content")
	}

	return model.Response{
		AssistantText:       assistantText,
		ProviderName:        "anthropic",
		ModelName:           defaultString(messageResponse.Model, provider.modelName),
		FinishReason:        messageResponse.StopReason,
		RequestPayloadBytes: requestPayloadBytes,
		Usage: model.Usage{
			InputTokens:  messageResponse.Usage.InputTokens,
			OutputTokens: messageResponse.Usage.OutputTokens,
			TotalTokens:  messageResponse.Usage.InputTokens + messageResponse.Usage.OutputTokens,
		},
		Prompt: model.PromptMetadata{
			PersonaHash: compiledPrompt.Metadata.PersonaHash,
			PolicyHash:  compiledPrompt.Metadata.PolicyHash,
			PromptHash:  compiledPrompt.Metadata.PromptHash,
		},
		Timing: model.Timing{
			PromptCompile:     promptCompileDuration,
			SecretResolve:     secretResolveDuration,
			ProviderRoundTrip: providerRoundTripDuration,
			ResponseDecode:    responseDecodeDuration,
			TotalGenerate:     time.Since(generateStart),
		},
		ToolUseBlocks: toolUseBlocks,
	}, nil
}

type messagesRequest struct {
	Model       string            `json:"model"`
	System      json.RawMessage   `json:"system,omitempty"`
	Messages    []messageParam    `json:"messages"`
	MaxTokens   int               `json:"max_tokens"`
	Temperature float64           `json:"temperature,omitempty"`
	Tools       []nativeToolParam `json:"tools,omitempty"`
}

// anthropicCacheControl marks a prefix for Anthropic prompt caching (ephemeral breakpoint).
type anthropicCacheControl struct {
	Type string `json:"type"`
}

// nativeToolParam is the Anthropic API tool definition shape.
type nativeToolParam struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	InputSchema  map[string]interface{} `json:"input_schema"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

// buildAnthropicCachedSystemJSON returns a system parameter as a JSON array of text blocks
// with an ephemeral cache breakpoint when system text is non-empty. Omits when empty.
func buildAnthropicCachedSystemJSON(systemText string) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(systemText)
	if trimmed == "" {
		return nil, nil
	}
	block := struct {
		Type         string                `json:"type"`
		Text         string                `json:"text"`
		CacheControl anthropicCacheControl `json:"cache_control"`
	}{
		Type:         "text",
		Text:         trimmed,
		CacheControl: anthropicCacheControl{Type: "ephemeral"},
	}
	return json.Marshal([]interface{}{block})
}

type messageParam struct {
	Role    string        `json:"role"`
	Content []contentPart `json:"content"`
}

// contentPart is a polymorphic content block used in both requests and
// responses. Only the fields relevant to each block type are populated.
type contentPart struct {
	Type string `json:"type"`

	// text block fields
	Text string `json:"text,omitempty"`

	// image block fields (type="image")
	Source *anthropicImageSource `json:"source,omitempty"`

	// tool_use response block fields
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// tool_result request block fields
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

// anthropicImageSource is the source object for Anthropic image content blocks.
type anthropicImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/jpeg", "image/png", etc.
	Data      string `json:"data"`       // base64-encoded image bytes
}

type messagesResponse struct {
	Model      string        `json:"model"`
	StopReason string        `json:"stop_reason"`
	Content    []contentPart `json:"content"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func buildSystemPrompt(compiledPrompt prompt.CompiledPrompt) string {
	systemParts := make([]string, 0, 1+len(compiledPrompt.Conversation))
	if strings.TrimSpace(compiledPrompt.SystemInstruction) != "" {
		systemParts = append(systemParts, compiledPrompt.SystemInstruction)
	}
	for _, conversationTurn := range compiledPrompt.Conversation {
		if strings.EqualFold(strings.TrimSpace(conversationTurn.Role), "system") && strings.TrimSpace(conversationTurn.Content) != "" {
			systemParts = append(systemParts, conversationTurn.Content)
		}
	}
	return strings.Join(systemParts, "\n\n")
}

func buildMessages(compiledPrompt prompt.CompiledPrompt, originalConversation []model.ConversationTurn, attachments []model.Attachment) []messageParam {
	messages := make([]messageParam, 0, len(compiledPrompt.Conversation)+1)

	// Build an index from prompt turns to original turns so we can access
	// ToolResults metadata that doesn't pass through the prompt compiler.
	originalByIndex := make(map[int]model.ConversationTurn)
	for i, turn := range originalConversation {
		originalByIndex[i] = turn
	}

	promptIndex := 0
	for _, conversationTurn := range compiledPrompt.Conversation {
		role := normalizeRole(conversationTurn.Role)
		if role == "" {
			promptIndex++
			continue
		}

		// Check if the original turn has structured tool results.
		originalTurn, hasOriginal := originalByIndex[promptIndex]
		if hasOriginal && len(originalTurn.ToolResults) > 0 {
			// Emit as structured tool_result content blocks.
			parts := make([]contentPart, 0, len(originalTurn.ToolResults))
			for _, tr := range originalTurn.ToolResults {
				parts = append(parts, contentPart{
					Type:      "tool_result",
					ToolUseID: tr.ToolUseID,
					Content:   tr.Content,
					IsError:   tr.IsError,
				})
			}
			messages = append(messages, messageParam{
				Role:    "user",
				Content: parts,
			})
		} else if hasOriginal && len(originalTurn.ToolCalls) > 0 && role == "assistant" {
			// Assistant message with tool calls — emit as text + tool_use content blocks.
			parts := make([]contentPart, 0, 1+len(originalTurn.ToolCalls))
			if strings.TrimSpace(conversationTurn.Content) != "" {
				parts = append(parts, contentPart{
					Type: "text",
					Text: conversationTurn.Content,
				})
			}
			for _, tc := range originalTurn.ToolCalls {
				inputJSON, _ := json.Marshal(tc.Input)
				parts = append(parts, contentPart{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  model.MessagesAPIToolName(tc.Name),
					Input: json.RawMessage(inputJSON),
				})
			}
			messages = append(messages, messageParam{
				Role:    "assistant",
				Content: parts,
			})
		} else if strings.TrimSpace(conversationTurn.Content) != "" {
			messages = append(messages, messageParam{
				Role: role,
				Content: []contentPart{{
					Type: "text",
					Text: conversationTurn.Content,
				}},
			})
		}
		promptIndex++
	}
	if strings.TrimSpace(compiledPrompt.UserMessage) != "" {
		parts := []contentPart{{Type: "text", Text: compiledPrompt.UserMessage}}
		for _, att := range attachments {
			if strings.HasPrefix(att.MimeType, "image/") {
				parts = append(parts, contentPart{
					Type:   "image",
					Source: &anthropicImageSource{Type: "base64", MediaType: att.MimeType, Data: att.Data},
				})
			}
		}
		messages = append(messages, messageParam{Role: "user", Content: parts})
	}
	return messages
}

// buildNativeTools converts NativeToolDef values into the Anthropic API
// tool definition shape. Returns nil if no tools are provided.
func buildNativeTools(defs []model.NativeToolDef) []nativeToolParam {
	if len(defs) == 0 {
		return nil
	}
	params := make([]nativeToolParam, 0, len(defs))
	for _, def := range defs {
		params = append(params, nativeToolParam{
			// Anthropic requires tool names ^[a-zA-Z0-9_-]{1,128}$ (tools.*.custom.name).
			Name:        model.MessagesAPIToolName(def.Name),
			Description: def.Description,
			InputSchema: def.InputSchema,
		})
	}
	// Single cache breakpoint on the last tool definition caches the full tools[] prefix.
	params[len(params)-1].CacheControl = &anthropicCacheControl{Type: "ephemeral"}
	return params
}

// extractToolUseBlocks extracts structured tool_use content blocks from
// the Anthropic API response. Each tool_use block contains an id, name,
// and input object. The input values are coerced to strings to match
// the existing map[string]string tool argument convention.
func extractToolUseBlocks(contentBlocks []contentPart) []model.ToolUseBlock {
	var blocks []model.ToolUseBlock
	for _, block := range contentBlocks {
		if block.Type != "tool_use" {
			continue
		}
		if strings.TrimSpace(block.ID) == "" || strings.TrimSpace(block.Name) == "" {
			continue
		}

		// Parse the input JSON into map[string]string, coercing values.
		args := make(map[string]string)
		if len(block.Input) > 0 {
			var rawArgs map[string]interface{}
			if err := json.Unmarshal(block.Input, &rawArgs); err == nil {
				for k, v := range rawArgs {
					args[k] = stringifyAnthropicToolInputValue(v)
				}
			}
			// If input JSON is malformed, args stays empty and schema
			// validation in the orchestrator will catch missing required fields.
		}

		blocks = append(blocks, model.ToolUseBlock{
			ID:    block.ID,
			Name:  block.Name,
			Input: args,
		})
	}
	return blocks
}

// stringifyAnthropicToolInputValue maps tool_use input values to the map[string]string convention.
// Arrays and objects must become JSON text (not Go's %v) so capabilities like host.organize.plan
// can parse plan_json as a JSON array.
func stringifyAnthropicToolInputValue(v interface{}) string {
	switch typed := v.(type) {
	case string:
		return typed
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	case nil:
		return ""
	default:
		b, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprintf("%v", typed)
		}
		return string(b)
	}
}

func normalizeRole(rawRole string) string {
	switch strings.ToLower(strings.TrimSpace(rawRole)) {
	case "assistant":
		return "assistant"
	case "user":
		return "user"
	default:
		return ""
	}
}

func extractAssistantText(contentBlocks []contentPart) string {
	textParts := make([]string, 0, len(contentBlocks))
	for _, contentBlock := range contentBlocks {
		if strings.TrimSpace(contentBlock.Type) != "text" {
			continue
		}
		trimmedText := strings.TrimSpace(contentBlock.Text)
		if trimmedText == "" {
			continue
		}
		textParts = append(textParts, trimmedText)
	}
	return strings.Join(textParts, "\n")
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
