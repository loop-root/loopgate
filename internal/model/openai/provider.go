package openai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// maxOpenAIRateLimitRetries is how many times we retry after HTTP 429 from an OpenAI-compatible API.
const maxOpenAIRateLimitRetries = 4

func openaiRateLimitWait(header http.Header, attemptZeroBased int) time.Duration {
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

// openAICompatiblePromptCacheKey is a stable key for OpenAI prompt-cache routing so requests
// that share the same persona/policy/prompt template and model hit the same cache bucket.
// See https://developers.openai.com/api/docs/guides/prompt-caching (prompt_cache_key).
func openAICompatiblePromptCacheKey(meta prompt.PromptMetadata, modelName string) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s|%s|%s|%s",
		strings.TrimSpace(meta.PersonaHash),
		strings.TrimSpace(meta.PolicyHash),
		strings.TrimSpace(meta.PromptHash),
		strings.TrimSpace(modelName),
	)
	return "morph_pc_" + hex.EncodeToString(h.Sum(nil)[:18])
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
	NoAuth          bool
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
	noAuth          bool
}

func NewProvider(config Config) (*Provider, error) {
	if strings.TrimSpace(config.BaseURL) == "" {
		return nil, fmt.Errorf("missing base URL")
	}
	if strings.TrimSpace(config.ModelName) == "" {
		return nil, fmt.Errorf("missing model name")
	}
	if !config.NoAuth && config.SecretStore == nil {
		return nil, fmt.Errorf("missing secret store")
	}
	if !config.NoAuth {
		if err := config.APIKeyRef.Validate(); err != nil {
			return nil, fmt.Errorf("validate api key ref: %w", err)
		}
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
		noAuth:          config.NoAuth,
	}, nil
}

func (provider *Provider) Generate(ctx context.Context, request model.Request) (model.Response, error) {
	generateStart := time.Now()

	promptCompileStart := time.Now()
	constrainedNativeTools := model.ConstrainedNativeToolsFromRuntimeFacts(request.RuntimeFacts)
	promptRuntimeFacts := model.StripInternalRuntimeFacts(request.RuntimeFacts)
	compiledPrompt, err := provider.compiler.Compile(prompt.Request{
		Persona:            request.Persona,
		Policy:             request.Policy,
		SessionID:          request.SessionID,
		TurnCount:          request.TurnCount,
		WakeState:          request.WakeState,
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

	var apiKeyBytes []byte
	secretResolveStart := time.Now()
	var secretResolveDuration time.Duration
	if !provider.noAuth {
		apiKeyBytes, _, err = provider.secretStore.Get(ctx, provider.apiKeyRef)
		secretResolveDuration = time.Since(secretResolveStart)
		if err != nil {
			return model.Response{
				ProviderName: "openai_compatible",
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
	}

	requestBody := chatCompletionRequest{
		Model:          provider.modelName,
		Temperature:    provider.temperature,
		Messages:       buildMessages(compiledPrompt, request.Conversation, request.Attachments),
		Tools:          buildOpenAITools(request.NativeToolDefs),
		PromptCacheKey: openAICompatiblePromptCacheKey(compiledPrompt.Metadata, provider.modelName),
	}
	if provider.maxOutputTokens > 0 {
		requestBody.MaxCompletionTokens = provider.maxOutputTokens
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

	requestURL := provider.baseURL + "/chat/completions"
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
		if !provider.noAuth {
			httpRequest.Header.Set("Authorization", "Bearer "+string(apiKeyBytes))
		}

		providerRoundTripStart := time.Now()
		httpResponse, err = provider.httpClient.Do(httpRequest)
		providerRoundTripDuration += time.Since(providerRoundTripStart)
		if err != nil {
			return model.Response{
				ProviderName:        "openai_compatible",
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
				ProviderName:        "openai_compatible",
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
				ProviderName:        "openai_compatible",
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

		if httpResponse.StatusCode == http.StatusTooManyRequests && attempt < maxOpenAIRateLimitRetries {
			wait := openaiRateLimitWait(httpResponse.Header, attempt)
			select {
			case <-time.After(wait):
			case <-requestContext.Done():
				return model.Response{
					ProviderName:        "openai_compatible",
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
				ProviderName:        "openai_compatible",
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
	var completionResponse chatCompletionResponse
	if err := json.Unmarshal(responseBytes, &completionResponse); err != nil {
		return model.Response{
			ProviderName:        "openai_compatible",
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
	if len(completionResponse.Choices) == 0 {
		return model.Response{
			ProviderName:        "openai_compatible",
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
		}, fmt.Errorf("provider response missing choices")
	}

	firstChoice := completionResponse.Choices[0]
	assistantText := strings.TrimSpace(firstChoice.Message.Content)
	toolUseBlocks := extractToolUseBlocks(firstChoice.Message.ToolCalls)

	cachedIn := 0
	if completionResponse.Usage.PromptTokensDetails != nil {
		cachedIn = completionResponse.Usage.PromptTokensDetails.CachedTokens
	}
	return model.Response{
		AssistantText:       assistantText,
		ProviderName:        "openai_compatible",
		ModelName:           completionResponse.Model,
		FinishReason:        firstChoice.FinishReason,
		RequestPayloadBytes: requestPayloadBytes,
		Usage: model.Usage{
			InputTokens:       completionResponse.Usage.PromptTokens,
			OutputTokens:      completionResponse.Usage.CompletionTokens,
			TotalTokens:       completionResponse.Usage.TotalTokens,
			CachedInputTokens: cachedIn,
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

type chatCompletionRequest struct {
	Model               string            `json:"model"`
	Messages            []chatMessage     `json:"messages"`
	Temperature         float64           `json:"temperature,omitempty"`
	MaxCompletionTokens int               `json:"max_completion_tokens,omitempty"`
	Tools               []openaiToolParam `json:"tools,omitempty"`
	// PromptCacheKey influences OpenAI prompt-cache routing for shared long prefixes.
	PromptCacheKey string `json:"prompt_cache_key,omitempty"`
}

// openaiToolParam is the OpenAI API tool definition shape.
type openaiToolParam struct {
	Type     string             `json:"type"`
	Function openaiToolFunction `json:"function"`
}

type openaiToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type chatMessage struct {
	Role string `json:"role"`
	// Content is either a plain string (text-only messages) or []openaiContentPart
	// (multimodal messages with images). The interface{} type lets json.Marshal
	// produce the correct shape for each case.
	Content    interface{}             `json:"content"`
	ToolCalls  []openaiToolCallRequest `json:"tool_calls,omitempty"`
	ToolCallID string                  `json:"tool_call_id,omitempty"`
	// Name is the function name for role=tool. Moonshot (Kimi) documents that
	// tool results should include name alongside tool_call_id so the model can
	// match results to the assistant tool_calls turn (OpenAI-compatible servers
	// may ignore this field).
	Name string `json:"name,omitempty"`
}

// openaiContentPart is one element of a multimodal content array.
type openaiContentPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *openaiImageURL `json:"image_url,omitempty"`
}

// openaiImageURL holds a data-URI for a base64-encoded image.
type openaiImageURL struct {
	URL string `json:"url"` // "data:<mime>;base64,<data>"
}

// openaiToolCallRequest is the tool_calls entry we send in chat completion requests.
// OpenAI documents function.arguments as a JSON string; Moonshot/Kimi reject a raw object.
type openaiToolCallRequest struct {
	ID       string                      `json:"id"`
	Type     string                      `json:"type"`
	Function openaiToolInvocationRequest `json:"function"`
}

type openaiToolInvocationRequest struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // stringified JSON object per OpenAI API
}

// openaiToolInvocation is the function payload when decoding API responses.
// json.RawMessage accepts both stringified JSON and a raw object (provider variance).
type openaiToolInvocation struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type openaiToolCall struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function openaiToolInvocation `json:"function"`
}

type chatCompletionResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content   string           `json:"content"`
			ToolCalls []openaiToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage chatCompletionUsage `json:"usage"`
}

type chatCompletionUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
}

func buildMessages(compiledPrompt prompt.CompiledPrompt, originalConversation []model.ConversationTurn, attachments []model.Attachment) []chatMessage {
	messages := []chatMessage{
		{
			Role:    "system",
			Content: compiledPrompt.SystemInstruction,
		},
	}

	// Build an index from prompt turns to original turns so we can access
	// ToolResults metadata that doesn't pass through the prompt compiler.
	originalByIndex := make(map[int]model.ConversationTurn)
	for i, turn := range originalConversation {
		originalByIndex[i] = turn
	}

	promptIndex := 0
	for _, conversationTurn := range compiledPrompt.Conversation {
		role := normalizeRole(conversationTurn.Role)

		// Check if the original turn has structured tool results.
		originalTurn, hasOriginal := originalByIndex[promptIndex]
		if hasOriginal && len(originalTurn.ToolResults) > 0 {
			// Emit each tool result as a separate "tool" role message.
			for _, tr := range originalTurn.ToolResults {
				toolMsg := chatMessage{
					Role:       "tool",
					Content:    tr.Content,
					ToolCallID: tr.ToolUseID,
				}
				if trimmedName := strings.TrimSpace(tr.ToolName); trimmedName != "" {
					toolMsg.Name = trimmedName
				}
				messages = append(messages, toolMsg)
			}
		} else if hasOriginal && len(originalTurn.ToolCalls) > 0 && role == "assistant" {
			// Assistant message with tool calls — include tool_calls array
			// so the OpenAI API accepts the subsequent "tool" messages.
			msg := chatMessage{
				Role:    "assistant",
				Content: conversationTurn.Content,
			}
			for _, tc := range originalTurn.ToolCalls {
				argsJSON, err := json.Marshal(tc.Input)
				if err != nil {
					argsJSON = []byte("{}")
				}
				msg.ToolCalls = append(msg.ToolCalls, openaiToolCallRequest{
					ID:   tc.ID,
					Type: "function",
					Function: openaiToolInvocationRequest{
						Name:      tc.Name,
						Arguments: string(argsJSON),
					},
				})
			}
			messages = append(messages, msg)
		} else if role != "" && strings.TrimSpace(conversationTurn.Content) != "" {
			messages = append(messages, chatMessage{
				Role:    role,
				Content: conversationTurn.Content,
			})
		}
		promptIndex++
	}

	if strings.TrimSpace(compiledPrompt.UserMessage) != "" {
		if len(attachments) > 0 {
			parts := make([]openaiContentPart, 0, 1+len(attachments))
			parts = append(parts, openaiContentPart{Type: "text", Text: compiledPrompt.UserMessage})
			for _, att := range attachments {
				if strings.HasPrefix(att.MimeType, "image/") {
					parts = append(parts, openaiContentPart{
						Type:     "image_url",
						ImageURL: &openaiImageURL{URL: fmt.Sprintf("data:%s;base64,%s", att.MimeType, att.Data)},
					})
				}
			}
			messages = append(messages, chatMessage{Role: "user", Content: parts})
		} else {
			messages = append(messages, chatMessage{Role: "user", Content: compiledPrompt.UserMessage})
		}
	}
	return messages
}

// buildOpenAITools converts NativeToolDef values into the OpenAI API tool
// definition shape. Returns nil if no tools are provided.
func buildOpenAITools(defs []model.NativeToolDef) []openaiToolParam {
	if len(defs) == 0 {
		return nil
	}
	params := make([]openaiToolParam, 0, len(defs))
	for _, def := range defs {
		params = append(params, openaiToolParam{
			Type: "function",
			Function: openaiToolFunction{
				Name:        def.Name,
				Description: def.Description,
				Parameters:  def.InputSchema,
			},
		})
	}
	return params
}

// extractToolUseBlocks converts OpenAI-format tool_calls into the common
// model.ToolUseBlock representation. Input values are coerced to strings.
func extractToolUseBlocks(toolCalls []openaiToolCall) []model.ToolUseBlock {
	if len(toolCalls) == 0 {
		return nil
	}
	blocks := make([]model.ToolUseBlock, 0, len(toolCalls))
	for _, tc := range toolCalls {
		if strings.TrimSpace(tc.ID) == "" || strings.TrimSpace(tc.Function.Name) == "" {
			continue
		}
		args := decodeOpenAIToolArguments(tc.Function.Arguments)
		blocks = append(blocks, model.ToolUseBlock{
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: args,
		})
	}
	return blocks
}

// decodeOpenAIToolArguments normalizes provider tool "arguments" into map[string]string.
func decodeOpenAIToolArguments(raw json.RawMessage) map[string]string {
	args := make(map[string]string)
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return args
	}
	// Standard shape: arguments is a JSON object.
	var asObject map[string]interface{}
	if err := json.Unmarshal(trimmed, &asObject); err == nil && asObject != nil {
		for k, v := range asObject {
			args[k] = stringifyJSONValue(v)
		}
		return args
	}
	// Some gateways double-encode: arguments is a JSON string containing JSON.
	var asString string
	if err := json.Unmarshal(trimmed, &asString); err == nil && strings.TrimSpace(asString) != "" {
		var inner map[string]interface{}
		if err := json.Unmarshal([]byte(asString), &inner); err == nil && inner != nil {
			for k, v := range inner {
				args[k] = stringifyJSONValue(v)
			}
		}
	}
	return args
}

func stringifyJSONValue(v interface{}) string {
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
	case "system":
		return "system"
	default:
		return "user"
	}
}
