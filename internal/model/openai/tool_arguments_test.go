package openai

import (
	"encoding/json"
	"testing"
)

func TestExtractToolUseBlocks_ObjectShapedArguments(t *testing.T) {
	// Some OpenAI-compatible APIs return "arguments" as a JSON object, not a string.
	payload := []byte(`{
		"choices": [{
			"message": {
				"content": "",
				"tool_calls": [{
					"id": "call_1",
					"type": "function",
					"function": {
						"name": "fs_list",
						"arguments": {"path": "workspace", "recursive": false}
					}
				}]
			},
			"finish_reason": "tool_calls"
		}]
	}`)
	var resp chatCompletionResponse
	if err := json.Unmarshal(payload, &resp); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	blocks := extractToolUseBlocks(resp.Choices[0].Message.ToolCalls)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Name != "fs_list" {
		t.Fatalf("name: %q", blocks[0].Name)
	}
	if blocks[0].Input["path"] != "workspace" {
		t.Fatalf("path: %#v", blocks[0].Input["path"])
	}
	if blocks[0].Input["recursive"] != "false" {
		t.Fatalf("recursive: %#v", blocks[0].Input["recursive"])
	}
}

func TestExtractToolUseBlocks_StringArgumentsUnchanged(t *testing.T) {
	payload := []byte(`{
		"choices": [{
			"message": {
				"tool_calls": [{
					"id": "call_2",
					"type": "function",
					"function": {
						"name": "notes.write",
						"arguments": "{\"title\":\"t\",\"body\":\"b\"}"
					}
				}]
			},
			"finish_reason": "tool_calls"
		}]
	}`)
	var resp chatCompletionResponse
	if err := json.Unmarshal(payload, &resp); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	blocks := extractToolUseBlocks(resp.Choices[0].Message.ToolCalls)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Input["title"] != "t" || blocks[0].Input["body"] != "b" {
		t.Fatalf("unexpected input: %#v", blocks[0].Input)
	}
}

func TestExtractToolUseBlocks_ArrayArgumentBecomesJSONText(t *testing.T) {
	// Models often send plan_json as a native JSON array inside arguments; coerce to JSON text.
	payload := []byte(`{
		"choices": [{
			"message": {
				"tool_calls": [{
					"id": "call_3",
					"type": "function",
					"function": {
						"name": "host.organize.plan",
						"arguments": {
							"folder_name": "downloads",
							"plan_json": [{"kind":"mkdir","path":"x"}],
							"summary": "tidy"
						}
					}
				}]
			},
			"finish_reason": "tool_calls"
		}]
	}`)
	var resp chatCompletionResponse
	if err := json.Unmarshal(payload, &resp); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	blocks := extractToolUseBlocks(resp.Choices[0].Message.ToolCalls)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	planJSON := blocks[0].Input["plan_json"]
	if planJSON == "" || planJSON[0] != '[' {
		t.Fatalf("plan_json should be JSON array text, got %q", planJSON)
	}
	var decoded []map[string]string
	if err := json.Unmarshal([]byte(planJSON), &decoded); err != nil {
		t.Fatalf("decode plan_json: %v", err)
	}
	if len(decoded) != 1 || decoded[0]["kind"] != "mkdir" {
		t.Fatalf("decoded: %#v", decoded)
	}
}
