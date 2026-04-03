package openai

import (
	"encoding/json"
	"testing"
)

func TestChatMessageToolRoleMarshalsNameField(t *testing.T) {
	// Moonshot/Kimi document role=tool messages with tool_call_id and name.
	msg := chatMessage{
		Role:       "tool",
		Content:    `{"status":"ok"}`,
		ToolCallID: "call_abc",
		Name:       "memory.remember",
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["name"] != "memory.remember" {
		t.Fatalf("expected name in JSON, got %v", decoded["name"])
	}
	if decoded["tool_call_id"] != "call_abc" {
		t.Fatalf("tool_call_id: %v", decoded["tool_call_id"])
	}
}
