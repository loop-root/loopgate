package loopgate

import (
	"context"
	"net/http"
	"testing"
)

func TestConfigGoalAliasesRouteRetired(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, client.baseURL+"/v1/config/goal_aliases", nil)
	if err != nil {
		t.Fatalf("build retired config route request: %v", err)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		t.Fatalf("request retired config route: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected retired goal_aliases route to return 404, got %d", response.StatusCode)
	}
}

func TestConfigMorphlingClassesRouteRetired(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, client.baseURL+"/v1/config/morphling_classes", nil)
	if err != nil {
		t.Fatalf("build retired config route request: %v", err)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		t.Fatalf("request retired config route: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected retired morphling_classes route to return 404, got %d", response.StatusCode)
	}
}
