package loopgate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDiagnosticReport_UnauthenticatedWithoutPeer(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, srv := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	req := httptest.NewRequest(http.MethodGet, "/v1/diagnostic/report", nil)
	rec := httptest.NewRecorder()
	srv.handleDiagnosticReport(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without peer, got %d", rec.Code)
	}
}

func TestClientFetchDiagnosticReport_OK(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	var decoded map[string]interface{}
	if err := client.FetchDiagnosticReport(context.Background(), &decoded); err != nil {
		t.Fatalf("fetch diagnostic report: %v", err)
	}
	if _, ok := decoded["ledger_verify"]; !ok {
		t.Fatalf("expected ledger_verify in response, got %#v", decoded)
	}
}
