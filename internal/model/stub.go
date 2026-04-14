package model

import (
	"context"
	"fmt"
	"strings"
	"time"

	"loopgate/internal/prompt"
)

type StubProvider struct {
	compiler *prompt.Compiler
}

func NewStubProvider() *StubProvider {
	return &StubProvider{compiler: prompt.NewCompiler()}
}

func (provider *StubProvider) Generate(ctx context.Context, request Request) (Response, error) {
	_ = ctx
	generateStart := time.Now()

	promptCompileStart := time.Now()
	constrainedNativeTools := ConstrainedNativeToolsFromRuntimeFacts(request.RuntimeFacts)
	promptRuntimeFacts := StripHavenInternalRuntimeFacts(request.RuntimeFacts)
	compiledPrompt, err := provider.compiler.Compile(prompt.Request{
		Persona:                 request.Persona,
		Policy:                  request.Policy,
		SessionID:               request.SessionID,
		TurnCount:               request.TurnCount,
		WakeState:               request.WakeState,
		Conversation:            ToPromptConversationTurns(request.Conversation),
		UserMessage:             request.UserMessage,
		AvailableTools:          ToPromptTools(request.AvailableTools),
		AvailableCommands:       ToPromptCommands(request.AvailableCommands),
		RuntimeFacts:            promptRuntimeFacts,
		HasNativeTools:          len(request.NativeToolDefs) > 0,
		HavenConstrainedToolUse: constrainedNativeTools,
	})
	if err != nil {
		return Response{}, err
	}
	promptCompileDuration := time.Since(promptCompileStart)

	trimmedMessage := strings.TrimSpace(request.UserMessage)
	if trimmedMessage == "" {
		trimmedMessage = "Say something and I'll respond. I'm not psychic (yet)."
	}

	replyTone := strings.TrimSpace(request.Persona.Defaults.Tone)
	if replyTone == "" {
		replyTone = strings.TrimSpace(request.Persona.Communication.Tone)
	}
	if replyTone == "" {
		replyTone = "helpful, honest, direct"
	}

	return Response{
		AssistantText: fmt.Sprintf("%s (%s): I heard you say: %q", request.Persona.Name, replyTone, trimmedMessage),
		ProviderName:  "stub",
		ModelName:     "stub",
		FinishReason:  "stop",
		Prompt: PromptMetadata{
			PersonaHash: compiledPrompt.Metadata.PersonaHash,
			PolicyHash:  compiledPrompt.Metadata.PolicyHash,
			PromptHash:  compiledPrompt.Metadata.PromptHash,
		},
		Timing: Timing{
			PromptCompile: promptCompileDuration,
			TotalGenerate: time.Since(generateStart),
		},
	}, nil
}
