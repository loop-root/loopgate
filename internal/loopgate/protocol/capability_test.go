package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCloneCapabilityRequest_DeepCopiesArgumentsMap(t *testing.T) {
	sharedArguments := map[string]string{"path": "original.txt", "content": "a"}
	original := CapabilityRequest{
		RequestID:  "req-clone",
		Capability: "fs_write",
		Arguments:  sharedArguments,
	}
	cloned := CloneCapabilityRequest(original)
	sharedArguments["path"] = "mutated.txt"
	if cloned.Arguments["path"] != "original.txt" {
		t.Fatalf("clone should not observe mutations to the original arguments map, got path %q", cloned.Arguments["path"])
	}
	original.Arguments["content"] = "b"
	if cloned.Arguments["content"] != "a" {
		t.Fatalf("clone should not observe mutations via original struct field, got content %q", cloned.Arguments["content"])
	}
}

func TestCapabilityRequestMarshalJSON_StripsEchoFields(t *testing.T) {
	request := CapabilityRequest{
		RequestID:                 "req-1",
		Capability:                "fs_list",
		Arguments:                 map[string]string{"path": "."},
		EchoedNativeToolName:      "bad",
		EchoedNativeToolCallID:    "bad-call",
		EchoedNativeToolNameCamel: "badCamel",
	}
	encodedRequest, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal capability request: %v", err)
	}
	encodedLower := strings.ToLower(string(encodedRequest))
	for _, forbiddenField := range []string{"toolname", "tool_call_id", "toolcallid"} {
		if strings.Contains(encodedLower, forbiddenField) {
			t.Fatalf("marshal output leaked echoed tool metadata %q: %s", forbiddenField, encodedRequest)
		}
	}
}
