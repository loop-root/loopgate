package threadstore

import (
	"strings"
	"testing"
)

func TestRedactEventData_NilData(t *testing.T) {
	result := redactEventData(EventUserMessage, nil)
	if result != nil {
		t.Fatalf("expected nil, got %v", result)
	}
}

func TestRedactEventData_UserMessagePassthrough(t *testing.T) {
	data := map[string]interface{}{
		"text": "hello world",
	}
	result := redactEventData(EventUserMessage, data)
	if result["text"] != "hello world" {
		t.Fatalf("expected user message text to pass through, got %v", result["text"])
	}
}

func TestRedactEventData_ModelResponseStripsRawPayload(t *testing.T) {
	data := map[string]interface{}{
		"model":        "claude-3",
		"stop_reason":  "end_turn",
		"raw_response": `{"huge": "payload with system prompt"}`,
		"raw_payload":  `{"another": "internal blob"}`,
		"api_response": `{"status": 200}`,
	}
	result := redactEventData(EventOrchModelResponse, data)

	if _, exists := result["raw_response"]; exists {
		t.Error("raw_response should be stripped")
	}
	if _, exists := result["raw_payload"]; exists {
		t.Error("raw_payload should be stripped")
	}
	if _, exists := result["api_response"]; exists {
		t.Error("api_response should be stripped")
	}
	if result["model"] != "claude-3" {
		t.Error("model field should be preserved")
	}
	if result["stop_reason"] != "end_turn" {
		t.Error("stop_reason field should be preserved")
	}
}

func TestRedactEventData_ToolResultTruncatesOutput(t *testing.T) {
	longOutput := strings.Repeat("x", 5000)
	data := map[string]interface{}{
		"tool_name": "fs_read",
		"status":    "success",
		"output":    longOutput,
	}
	result := redactEventData(EventOrchToolResult, data)

	output, ok := result["output"].(string)
	if !ok {
		t.Fatal("output should be a string")
	}
	if len(output) > maxToolOutputLen+100 {
		t.Errorf("output should be truncated, got length %d", len(output))
	}
	if !strings.Contains(output, "[threadstore: output truncated]") {
		t.Error("truncated output should contain redaction notice")
	}
	if result["tool_name"] != "fs_read" {
		t.Error("tool_name should be preserved")
	}
}

func TestRedactEventData_ToolResultStripsRawFields(t *testing.T) {
	data := map[string]interface{}{
		"tool_name":  "bash",
		"status":     "success",
		"output":     "ok",
		"raw_output": "verbose internal output",
		"stderr":     "debug info",
		"env":        "SECRET_KEY=abc123",
	}
	result := redactEventData(EventOrchToolResult, data)

	if _, exists := result["raw_output"]; exists {
		t.Error("raw_output should be stripped")
	}
	if _, exists := result["stderr"]; exists {
		t.Error("stderr should be stripped")
	}
	if _, exists := result["env"]; exists {
		t.Error("env should be stripped")
	}
}

func TestRedactEventData_ShortToolOutputPreserved(t *testing.T) {
	data := map[string]interface{}{
		"tool_name": "fs_list",
		"output":    "file1.txt\nfile2.txt",
	}
	result := redactEventData(EventOrchToolResult, data)
	if result["output"] != "file1.txt\nfile2.txt" {
		t.Error("short output should be preserved as-is")
	}
}

func TestScrubSecretPatterns_AnthropicKey(t *testing.T) {
	result := scrubSecretPatterns("my key is sk-ant-abc123xyz")
	if result != redactedMarker {
		t.Errorf("expected redacted, got %q", result)
	}
}

func TestScrubSecretPatterns_GitHubToken(t *testing.T) {
	result := scrubSecretPatterns("ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	if result != redactedMarker {
		t.Errorf("expected redacted, got %q", result)
	}
}

func TestScrubSecretPatterns_BearerToken(t *testing.T) {
	result := scrubSecretPatterns("Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.xxx")
	if result != redactedMarker {
		t.Errorf("expected redacted, got %q", result)
	}
}

func TestScrubSecretPatterns_AWSAccessKey(t *testing.T) {
	result := scrubSecretPatterns("AKIAIOSFODNN7EXAMPLE")
	if result != redactedMarker {
		t.Errorf("expected redacted, got %q", result)
	}
}

func TestScrubSecretPatterns_SafeString(t *testing.T) {
	safe := "just a normal string with no secrets"
	result := scrubSecretPatterns(safe)
	if result != safe {
		t.Errorf("safe string should not be modified, got %q", result)
	}
}

func TestScrubEnvLikeValues_OnlyAffectsStrings(t *testing.T) {
	data := map[string]interface{}{
		"text":  "sk-ant-leaked-key",
		"count": 42,
		"flag":  true,
	}
	result := scrubEnvLikeValues(data)
	if result["text"] != redactedMarker {
		t.Error("string with secret should be redacted")
	}
	if result["count"] != 42 {
		t.Error("non-string values should be preserved")
	}
	if result["flag"] != true {
		t.Error("non-string values should be preserved")
	}
}

func TestRedactEventData_DoesNotMutateOriginal(t *testing.T) {
	data := map[string]interface{}{
		"raw_response": "sensitive",
		"model":        "claude-3",
	}
	_ = redactEventData(EventOrchModelResponse, data)
	if _, exists := data["raw_response"]; !exists {
		t.Error("original map should not be mutated")
	}
}

func TestTruncateToolOutput_ExactBoundary(t *testing.T) {
	exact := strings.Repeat("x", maxToolOutputLen)
	result := truncateToolOutput(exact, maxToolOutputLen)
	if result != exact {
		t.Error("string at exact max length should not be truncated")
	}
}

func TestTruncateToolOutput_OneBeyond(t *testing.T) {
	over := strings.Repeat("x", maxToolOutputLen+1)
	result := truncateToolOutput(over, maxToolOutputLen)
	if !strings.HasSuffix(result, "[threadstore: output truncated]") {
		t.Error("string beyond max should be truncated with notice")
	}
	if len(result) != maxToolOutputLen+len("\n[threadstore: output truncated]") {
		t.Errorf("unexpected truncated length: %d", len(result))
	}
}
