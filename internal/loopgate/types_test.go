package loopgate

import (
	"bytes"
	"encoding/json"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"testing"
)

func TestNormalizeCapabilityRequest_StripsEchoedProviderMetadata(t *testing.T) {
	raw := []byte(`{"request_id":"req_123","session_id":"control_123","actor":"operator","capability":"fs_list","arguments":{},"ToolName":"wrong.tool","tool_name":"also_wrong","toolName":"camel","tool_use_id":"u1","ToolUseID":"u2","tool_call_id":"c1","ToolCallID":"c2"}`)
	var decoded controlapipkg.CapabilityRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}

	normalized := normalizeCapabilityRequest(decoded)
	if normalized.EchoedNativeToolName != "" || normalized.EchoedNativeToolNameSnake != "" ||
		normalized.EchoedNativeToolNameCamel != "" || normalized.EchoedNativeToolUseID != "" ||
		normalized.EchoedNativeToolUseIDSnake != "" || normalized.EchoedNativeToolCallID != "" ||
		normalized.EchoedNativeToolCallIDAlt != "" {
		t.Fatalf("normalize should strip echoed tool metadata")
	}
	if normalized.Capability != "fs_list" {
		t.Fatalf("capability: %q", normalized.Capability)
	}
}
