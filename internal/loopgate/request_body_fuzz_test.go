package loopgate

import (
	"encoding/json"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"strings"
	"testing"
)

func FuzzDecodeJSONBytesCapabilityRequest(f *testing.F) {
	f.Add([]byte(`{"request_id":"req-1","session_id":"session-1","actor":"operator","capability":"fs_read","arguments":{"path":"README.md"}}`))
	f.Add([]byte(`{"request_id":"req-echo","session_id":"session-1","actor":"operator","capability":"fs_read","arguments":{"path":"README.md"},"ToolName":"Read","tool_name":"Read","toolName":"Read","ToolUseID":"use-1","tool_use_id":"use-1","tool_call_id":"call-1","ToolCallID":"call-1"}`))
	f.Add([]byte(`{"request_id":"req-unknown","capability":"fs_read","arguments":{"path":"README.md"},"unknown_field":true}`))
	f.Add([]byte(`{"request_id":"req-trailing","capability":"fs_read"} {"second":true}`))

	f.Fuzz(func(t *testing.T, rawBytes []byte) {
		var capabilityRequest controlapipkg.CapabilityRequest
		if err := decodeJSONBytes(rawBytes, &capabilityRequest); err != nil {
			return
		}

		_ = capabilityRequest.Validate()

		canonicalBytes, err := json.Marshal(capabilityRequest)
		if err != nil {
			t.Fatalf("marshal accepted capability request: %v", err)
		}

		canonicalJSON := string(canonicalBytes)
		for _, forbiddenField := range []string{
			`"ToolName"`,
			`"tool_name"`,
			`"toolName"`,
			`"ToolUseID"`,
			`"tool_use_id"`,
			`"tool_call_id"`,
			`"ToolCallID"`,
		} {
			if strings.Contains(canonicalJSON, forbiddenField) {
				t.Fatalf("canonical capability JSON leaked provider echo field %s: %s", forbiddenField, canonicalJSON)
			}
		}

		var reparsed controlapipkg.CapabilityRequest
		if err := decodeJSONBytes(canonicalBytes, &reparsed); err != nil {
			t.Fatalf("reparse canonical capability request: %v", err)
		}
		if reparsed.EchoedNativeToolName != "" ||
			reparsed.EchoedNativeToolNameSnake != "" ||
			reparsed.EchoedNativeToolNameCamel != "" ||
			reparsed.EchoedNativeToolUseID != "" ||
			reparsed.EchoedNativeToolUseIDSnake != "" ||
			reparsed.EchoedNativeToolCallID != "" ||
			reparsed.EchoedNativeToolCallIDAlt != "" {
			t.Fatalf("reparsed canonical capability request retained provider echo metadata: %#v", reparsed)
		}
	})
}
