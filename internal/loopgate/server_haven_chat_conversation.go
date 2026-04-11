package loopgate

import (
	modelpkg "morph/internal/model"
	"morph/internal/threadstore"
)

func havenBuildConversationFromThread(store *threadstore.Store, threadID string) []modelpkg.ConversationTurn {
	events, err := store.LoadThread(threadID)
	if err != nil {
		return nil
	}

	var conversation []modelpkg.ConversationTurn
	for _, event := range events {
		if !threadstore.IsUserVisible(event.Type) {
			continue
		}
		text, _ := event.Data["text"].(string)
		switch event.Type {
		case threadstore.EventUserMessage:
			conversation = append(conversation, modelpkg.ConversationTurn{
				Role:      "user",
				Content:   text,
				Timestamp: event.TS,
			})
		case threadstore.EventAssistantMessage:
			conversation = append(conversation, modelpkg.ConversationTurn{
				Role:      "assistant",
				Content:   text,
				Timestamp: event.TS,
			})
		}
	}

	if len(conversation) > 0 && conversation[len(conversation)-1].Role == "user" {
		conversation = conversation[:len(conversation)-1]
	}

	return conversation
}

func havenWindowConversationForModel(turns []modelpkg.ConversationTurn, maxTurns int) []modelpkg.ConversationTurn {
	if maxTurns <= 0 || len(turns) <= maxTurns {
		return turns
	}
	return turns[len(turns)-maxTurns:]
}
