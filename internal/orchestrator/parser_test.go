package orchestrator

import (
	"testing"
)

func TestParser_SingleCall(t *testing.T) {
	p := NewParser()

	input := `Here is some text.
<tool_call>
{"name": "fs_read", "args": {"path": "foo.txt"}}
</tool_call>
More text here.`

	result := p.Parse(input)

	if len(result.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(result.Calls))
	}

	call := result.Calls[0]
	if call.Name != "fs_read" {
		t.Errorf("expected name 'fs_read', got %q", call.Name)
	}
	if call.Args["path"] != "foo.txt" {
		t.Errorf("expected path 'foo.txt', got %q", call.Args["path"])
	}
	if call.ID == "" {
		t.Error("expected generated ID")
	}

	// Check remaining text
	if result.Text != "Here is some text.\n\nMore text here." {
		t.Errorf("unexpected remaining text: %q", result.Text)
	}
}

func TestParser_MultipleCalls(t *testing.T) {
	p := NewParser()

	input := `<tool_call>
{"name": "fs_list", "args": {"path": "."}}
</tool_call>
<tool_call>
{"name": "fs_read", "args": {"path": "README.md"}}
</tool_call>`

	result := p.Parse(input)

	if len(result.Calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(result.Calls))
	}

	if result.Calls[0].Name != "fs_list" {
		t.Errorf("expected first call 'fs_list', got %q", result.Calls[0].Name)
	}
	if result.Calls[1].Name != "fs_read" {
		t.Errorf("expected second call 'fs_read', got %q", result.Calls[1].Name)
	}
}

func TestParser_WithExplicitID(t *testing.T) {
	p := NewParser()

	input := `<tool_call>
{"name": "fs_read", "id": "my_call_123", "args": {"path": "test.txt"}}
</tool_call>`

	result := p.Parse(input)

	if len(result.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(result.Calls))
	}

	if result.Calls[0].ID != "my_call_123" {
		t.Errorf("expected ID 'my_call_123', got %q", result.Calls[0].ID)
	}
}

func TestParser_NoToolCalls(t *testing.T) {
	p := NewParser()

	input := "This is just regular text with no tool calls."

	result := p.Parse(input)

	if len(result.Calls) != 0 {
		t.Errorf("expected 0 calls, got %d", len(result.Calls))
	}
	if result.Text != input {
		t.Errorf("expected text to be unchanged")
	}
}

func TestParser_MalformedJSON(t *testing.T) {
	p := NewParser()

	input := `<tool_call>
{not valid json}
</tool_call>`

	result := p.Parse(input)

	if len(result.Calls) != 0 {
		t.Errorf("expected 0 valid calls, got %d", len(result.Calls))
	}
	if len(result.ParseErrs) != 1 {
		t.Errorf("expected 1 parse error, got %d", len(result.ParseErrs))
	}
}

func TestParser_MissingName(t *testing.T) {
	p := NewParser()

	input := `<tool_call>
{"args": {"path": "test.txt"}}
</tool_call>`

	result := p.Parse(input)

	if len(result.Calls) != 0 {
		t.Errorf("expected 0 valid calls, got %d", len(result.Calls))
	}
	if len(result.ParseErrs) != 1 {
		t.Errorf("expected 1 parse error, got %d", len(result.ParseErrs))
	}
}

func TestParser_RejectsLocalMorphCommandToolCall(t *testing.T) {
	p := NewParser()

	input := `<tool_call>
{"name": "goal", "args": {"action": "add", "textOrId": "Create a new blog post"}}
</tool_call>`

	result := p.Parse(input)

	if len(result.Calls) != 0 {
		t.Fatalf("expected 0 valid calls, got %d", len(result.Calls))
	}
	if len(result.ParseErrs) != 1 {
		t.Fatalf("expected 1 parse error, got %d", len(result.ParseErrs))
	}
	if got := result.ParseErrs[0].Error(); !contains(got, `local Morph command "goal"`) {
		t.Fatalf("expected local command parse error, got %q", got)
	}
}

func TestParser_StripsModelGeneratedToolResults(t *testing.T) {
	p := NewParser()

	input := `I can check that.
<tool_result>
{"call_id":"fake","status":"success","output":"pretend"}
</tool_result>
Done.`

	result := p.Parse(input)

	if len(result.Calls) != 0 {
		t.Fatalf("expected 0 tool calls, got %d", len(result.Calls))
	}
	if len(result.ParseErrs) != 1 {
		t.Fatalf("expected 1 parse error, got %d", len(result.ParseErrs))
	}
	if got := result.ParseErrs[0].Error(); !contains(got, "reserved <tool_result>") {
		t.Fatalf("expected reserved tool_result parse error, got %q", got)
	}
	if contains(result.Text, "<tool_result>") || contains(result.Text, "pretend") {
		t.Fatalf("expected model-generated tool result to be stripped from text, got %q", result.Text)
	}
}

func TestParser_UnclosedTag(t *testing.T) {
	p := NewParser()

	input := `<tool_call>
{"name": "fs_read", "args": {"path": "test.txt"}}
No closing tag`

	result := p.Parse(input)

	if len(result.Calls) != 0 {
		t.Errorf("expected 0 calls due to unclosed tag, got %d", len(result.Calls))
	}
	if len(result.ParseErrs) != 1 {
		t.Errorf("expected 1 parse error, got %d", len(result.ParseErrs))
	}
}

func TestFormatResults(t *testing.T) {
	results := []ToolResult{
		{
			CallID: "call_1",
			Status: StatusSuccess,
			Output: "file contents here",
		},
		{
			CallID: "call_2",
			Status: StatusDenied,
			Reason: "path denied by policy",
		},
	}

	formatted := FormatResults(results)

	// Check it contains expected elements
	if formatted == "" {
		t.Error("expected non-empty formatted output")
	}

	// Should contain both call IDs
	if !contains(formatted, "call_1") || !contains(formatted, "call_2") {
		t.Error("expected both call IDs in output")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
