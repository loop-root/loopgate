package main

import (
	"strings"
	"testing"

	modelpkg "morph/internal/model"
)

func TestWindowConversationForModel_NoTrimWhenShort(t *testing.T) {
	turns := []modelpkg.ConversationTurn{
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b"},
	}
	got := windowConversationForModel(turns, 48)
	if len(got) != 2 {
		t.Fatalf("got %d turns, want 2", len(got))
	}
}

func TestWindowConversationForModel_TrimsOldest(t *testing.T) {
	var turns []modelpkg.ConversationTurn
	for i := 0; i < 60; i++ {
		turns = append(turns, modelpkg.ConversationTurn{
			Role:    "user",
			Content: string(rune('a' + (i % 26))),
		})
	}
	got := windowConversationForModel(turns, 48)
	if len(got) != 48 {
		t.Fatalf("got %d turns, want 48", len(got))
	}
	if got[0].Content != turns[12].Content {
		t.Fatalf("first kept turn should align with index 12, got %q want %q", got[0].Content, turns[12].Content)
	}
}

func TestWindowConversationForModel_PreservesNativeToolPair(t *testing.T) {
	prefix := make([]modelpkg.ConversationTurn, 50)
	for i := range prefix {
		prefix[i] = modelpkg.ConversationTurn{Role: "user", Content: "x"}
	}
	suffix := []modelpkg.ConversationTurn{
		{Role: "assistant", Content: "", ToolCalls: []modelpkg.ToolUseBlock{{ID: "tu_1", Name: "fs_list", Input: map[string]string{}}}},
		{Role: "user", ToolResults: []modelpkg.ToolResultBlock{{ToolUseID: "tu_1", ToolName: "fs_list", Content: "ok"}}},
	}
	turns := append(prefix, suffix...)
	// Tight window: only the last two turns, which must stay assistant+tool_result.
	got := windowConversationForModel(turns, 2)
	if len(got) != 2 {
		t.Fatalf("expected exactly 2 turns, got %d", len(got))
	}
	if !strings.EqualFold(got[0].Role, "assistant") || len(got[0].ToolCalls) == 0 {
		t.Fatalf("expected first kept turn to be assistant with tool calls, got %#v", got[0])
	}
	if len(got[1].ToolResults) == 0 {
		t.Fatalf("expected second turn to carry tool results, got %#v", got[1])
	}
}

func TestWindowConversationForModel_ExtendsBackForOrphanToolRole(t *testing.T) {
	prefix := make([]modelpkg.ConversationTurn, 50)
	for i := range prefix {
		prefix[i] = modelpkg.ConversationTurn{Role: "user", Content: "u"}
	}
	turns := append(prefix, []modelpkg.ConversationTurn{
		{Role: "assistant", Content: "calling tool"},
		{Role: "tool", Content: "tool output"},
	}...)
	// Window that would start on the bare "tool" turn must pull in the assistant before it.
	got := windowConversationForModel(turns, 1)
	if len(got) != 2 {
		t.Fatalf("want assistant+tool after expansion, got %d turns", len(got))
	}
	if !strings.EqualFold(got[0].Role, "assistant") || !strings.EqualFold(got[1].Role, "tool") {
		t.Fatalf("expected assistant then tool, got %#v then %#v", got[0], got[1])
	}
}
