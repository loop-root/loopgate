package model

import (
	"context"
	"time"

	"loopgate/internal/config"
)

type ConversationTurn struct {
	Role        string            `json:"Role"`
	Content     string            `json:"Content"`
	Timestamp   string            `json:"Timestamp"`
	ToolCalls   []ToolUseBlock    `json:"ToolCalls,omitempty"`
	ToolResults []ToolResultBlock `json:"ToolResults,omitempty"`
}

// ToolResultBlock is a structured tool result sent back to the model
// as part of a conversation turn on the native tool-use path.
type ToolResultBlock struct {
	ToolUseID string // Matches the ID from the model's tool_use block
	ToolName  string // Validated capability name (Moonshot/Kimi require this on role=tool messages)
	Content   string // Result content (output or error message)
	IsError   bool   // True if this result represents a failure
}

type ToolDefinition struct {
	Name        string
	Operation   string
	Description string
}

type CommandDefinition struct {
	Name        string
	Args        string
	Description string
}

// NativeToolDef is a tool definition sent to the model provider's native
// structured tool-calling API. This is defense-in-depth / UX shaping only —
// it does NOT grant authorization. Loopgate remains the sole authority.
type NativeToolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"` // JSON Schema object
}

type Request struct {
	Persona           config.Persona
	Policy            config.Policy
	SessionID         string
	TurnCount         int
	WakeState         string
	Conversation      []ConversationTurn
	UserMessage       string
	AvailableTools    []ToolDefinition
	AvailableCommands []CommandDefinition
	RuntimeFacts      []string
	NativeToolDefs    []NativeToolDef // Structured tool definitions for native provider API
	Attachments       []Attachment    // Optional file/image attachments for the user message
}

// Attachment is a file or image payload sent alongside a chat message.
// Data is the raw file content encoded as a standard base64 string.
// MimeType should be a valid MIME type (e.g. "image/jpeg", "text/plain").
type Attachment struct {
	Name     string
	MimeType string
	Data     string // base64-encoded content
}

type Settings struct {
	ProviderName    string
	ModelName       string
	Temperature     float64
	MaxOutputTokens int
	Timeout         time.Duration
}

type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	// CachedInputTokens is provider-reported prompt tokens served from a prefix cache
	// (e.g. OpenAI usage.prompt_tokens_details.cached_tokens). Zero when unknown or no cache hit.
	CachedInputTokens int `json:"cached_input_tokens,omitempty"`
}

type PromptMetadata struct {
	PersonaHash string
	PolicyHash  string
	PromptHash  string
}

type Timing struct {
	PromptCompile     time.Duration `json:"-"`
	SecretResolve     time.Duration `json:"-"`
	ProviderRoundTrip time.Duration `json:"-"`
	ResponseDecode    time.Duration `json:"-"`
	TotalGenerate     time.Duration `json:"-"`
}

// ToolUseBlock represents a structured tool invocation returned by the model
// via the provider's native tool-use API. The model is requesting, not commanding.
type ToolUseBlock struct {
	ID    string            // Provider-assigned tool-use block ID
	Name  string            // Tool name the model wants to invoke
	Input map[string]string // Parsed input arguments
}

type Response struct {
	AssistantText string
	ProviderName  string
	ModelName     string
	FinishReason  string
	Usage         Usage
	Prompt        PromptMetadata
	// RequestPayloadBytes is the on-wire JSON body size last sent to the provider
	// (approximate prompt/tool payload). Populated by providers when known; omit when zero.
	RequestPayloadBytes int            `json:"request_payload_bytes,omitempty"`
	Timing              Timing         `json:"-"`
	ToolUseBlocks       []ToolUseBlock // Structured tool-use blocks from native provider API
}

type Provider interface {
	Generate(ctx context.Context, request Request) (Response, error)
}
