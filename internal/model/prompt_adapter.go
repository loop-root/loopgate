package model

import "morph/internal/prompt"

func ToPromptConversationTurns(conversation []ConversationTurn) []prompt.ConversationTurn {
	promptConversation := make([]prompt.ConversationTurn, 0, len(conversation))
	for _, conversationTurn := range conversation {
		promptConversation = append(promptConversation, prompt.ConversationTurn{
			Role:    conversationTurn.Role,
			Content: conversationTurn.Content,
		})
	}
	return promptConversation
}

func ToPromptTools(availableTools []ToolDefinition) []prompt.ToolDefinition {
	promptTools := make([]prompt.ToolDefinition, 0, len(availableTools))
	for _, toolDefinition := range availableTools {
		promptTools = append(promptTools, prompt.ToolDefinition{
			Name:        toolDefinition.Name,
			Operation:   toolDefinition.Operation,
			Description: toolDefinition.Description,
		})
	}
	return promptTools
}

func ToPromptCommands(availableCommands []CommandDefinition) []prompt.CommandDefinition {
	promptCommands := make([]prompt.CommandDefinition, 0, len(availableCommands))
	for _, commandDefinition := range availableCommands {
		promptCommands = append(promptCommands, prompt.CommandDefinition{
			Name:        commandDefinition.Name,
			Args:        commandDefinition.Args,
			Description: commandDefinition.Description,
		})
	}
	return promptCommands
}
