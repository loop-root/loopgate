package loopgate

import (
	"context"
	"errors"
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

func TestDiagnosticReportRequiresSignedRequest(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	client.mu.Lock()
	client.sessionMACKey = ""
	client.mu.Unlock()

	var ignored map[string]interface{}
	err := client.FetchDiagnosticReport(context.Background(), &ignored)
	var denied RequestDeniedError
	if !errors.As(err, &denied) || denied.DenialCode != DenialCodeRequestSignatureMissing {
		t.Fatalf("expected request signature missing denial, got %v", err)
	}
}
