package loopgate

import (
	"context"
	"encoding/json"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"strings"
	"testing"
)

func TestCapabilityResponseJSONDoesNotExposeProviderTokenFields(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-json",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("execute fs_list: %v", err)
	}

	encodedResponse, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	lowerJSON := strings.ToLower(string(encodedResponse))
	for _, forbiddenField := range []string{"access_token", "refresh_token", "client_secret", "api_key"} {
		if strings.Contains(lowerJSON, forbiddenField) {
			t.Fatalf("response leaked forbidden token field %q: %s", forbiddenField, encodedResponse)
		}
	}
}
