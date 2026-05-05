package loopgate

import (
	"context"
	"errors"
	"io"
	"loopgate/internal/ledger"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectSite_HTTPSReturnsCertificateInfo(t *testing.T) {
	repoRoot := t.TempDir()
	providerServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(writer, "<html><head><title>Status Page</title><meta name=\"description\" content=\"All systems operational\"></head><body>ok</body></html>")
	}))
	defer providerServer.Close()

	client, _, server := startLoopgateServer(t, repoRoot, loopgateHTTPPolicyYAML(false))
	server.httpClient = providerServer.Client()

	inspectionResponse, err := client.InspectSite(context.Background(), controlapipkg.SiteInspectionRequest{URL: providerServer.URL})
	if err != nil {
		t.Fatalf("inspect site: %v", err)
	}
	if !inspectionResponse.HTTPS {
		t.Fatalf("expected https inspection, got %#v", inspectionResponse)
	}
	if inspectionResponse.Certificate == nil || inspectionResponse.Certificate.Subject == "" {
		t.Fatalf("expected certificate details, got %#v", inspectionResponse)
	}
	if !inspectionResponse.TLSValid {
		t.Fatalf("expected trusted TLS inspection to validate certificate, got %#v", inspectionResponse)
	}
	if !inspectionResponse.TrustDraftAllowed {
		t.Fatalf("expected trusted TLS inspection to allow trust draft, got %#v", inspectionResponse)
	}
}

func TestInspectSite_UntrustedHTTPSReturnsNoDraftSuggestion(t *testing.T) {
	repoRoot := t.TempDir()
	providerServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(writer, "<html><head><title>Status Page</title><meta name=\"description\" content=\"tampered\"></head><body>tampered</body></html>")
	}))
	defer providerServer.Close()

	client, _, _ := startLoopgateServer(t, repoRoot, loopgateHTTPPolicyYAML(false))

	inspectionResponse, err := client.InspectSite(context.Background(), controlapipkg.SiteInspectionRequest{URL: providerServer.URL})
	if err != nil {
		t.Fatalf("inspect untrusted https site: %v", err)
	}
	if !inspectionResponse.HTTPS {
		t.Fatalf("expected https inspection, got %#v", inspectionResponse)
	}
	if inspectionResponse.Certificate != nil {
		t.Fatalf("expected invalid TLS inspection to omit certificate details, got %#v", inspectionResponse)
	}
	if inspectionResponse.TLSValid {
		t.Fatalf("expected untrusted test TLS to remain invalid, got %#v", inspectionResponse)
	}
	if inspectionResponse.TrustDraftAllowed {
		t.Fatalf("expected invalid TLS inspection to avoid trust draft, got %#v", inspectionResponse)
	}
	if inspectionResponse.DraftSuggestion != nil {
		t.Fatalf("expected invalid TLS inspection to omit draft suggestion, got %#v", inspectionResponse)
	}
}

func TestInspectSite_RejectsPrivateNetworkTarget(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgateHTTPPolicyYAML(false))

	_, err := client.InspectSite(context.Background(), controlapipkg.SiteInspectionRequest{URL: "https://10.0.0.1/status"})
	if err == nil {
		t.Fatal("expected private network target to be rejected")
	}
	if !strings.Contains(err.Error(), controlapipkg.DenialCodeSiteInspectionNetworkDenied) {
		t.Fatalf("expected network-denied error, got %v", err)
	}
}

func TestSiteInspectionIPAllowed(t *testing.T) {
	testCases := []struct {
		name    string
		rawIP   string
		allowed bool
	}{
		{name: "loopback", rawIP: "127.0.0.1", allowed: true},
		{name: "public", rawIP: "8.8.8.8", allowed: true},
		{name: "private", rawIP: "10.0.0.1", allowed: false},
		{name: "metadata link local", rawIP: "169.254.169.254", allowed: false},
		{name: "unspecified", rawIP: "0.0.0.0", allowed: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if allowed := siteInspectionIPAllowed(net.ParseIP(testCase.rawIP)); allowed != testCase.allowed {
				t.Fatalf("expected allowed=%v for %s, got %v", testCase.allowed, testCase.rawIP, allowed)
			}
		})
	}
}

func TestCreateTrustDraft_WritesLocalhostStatusDraft(t *testing.T) {
	repoRoot := t.TempDir()
	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"status":{"description":"All Systems Operational","indicator":"none"}}`)
	}))
	defer providerServer.Close()

	client, _, _ := startLoopgateServer(t, repoRoot, loopgateHTTPPolicyYAML(false))

	trustDraftResponse, err := client.CreateTrustDraft(context.Background(), controlapipkg.SiteTrustDraftRequest{URL: providerServer.URL})
	if err != nil {
		t.Fatalf("create trust draft: %v", err)
	}
	if !strings.Contains(trustDraftResponse.DraftPath, filepath.Join("loopgate", "connections", "drafts")) {
		t.Fatalf("expected draft under drafts dir, got %#v", trustDraftResponse)
	}
	draftBytes, err := os.ReadFile(trustDraftResponse.DraftPath)
	if err != nil {
		t.Fatalf("read draft file: %v", err)
	}
	draftText := string(draftBytes)
	if !strings.Contains(draftText, "grant_type: public_read") {
		t.Fatalf("expected public_read draft, got %q", draftText)
	}
	if !strings.Contains(draftText, "extractor: json_nested_selector") {
		t.Fatalf("expected nested json extractor draft, got %q", draftText)
	}
	if !strings.Contains(draftText, "json_path: status.description") {
		t.Fatalf("expected description selector in draft, got %q", draftText)
	}

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read loopgate event log: %v", err)
	}
	auditText := string(auditBytes)
	if !strings.Contains(auditText, "\"type\":\"site.trust_draft_created\"") {
		t.Fatalf("expected trust-draft event in audit log, got %s", auditText)
	}
	if strings.Contains(auditText, "\"type\":\"site.trust_draft_created\",\"session\":\"\"") {
		t.Fatalf("expected trust-draft event to carry a non-empty session, got %s", auditText)
	}
}

func TestCreateTrustDraft_DeniesOverwrite(t *testing.T) {
	repoRoot := t.TempDir()
	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"status":{"description":"All Systems Operational","indicator":"none"}}`)
	}))
	defer providerServer.Close()

	client, _, _ := startLoopgateServer(t, repoRoot, loopgateHTTPPolicyYAML(false))

	if _, err := client.CreateTrustDraft(context.Background(), controlapipkg.SiteTrustDraftRequest{URL: providerServer.URL}); err != nil {
		t.Fatalf("create first trust draft: %v", err)
	}
	_, err := client.CreateTrustDraft(context.Background(), controlapipkg.SiteTrustDraftRequest{URL: providerServer.URL})
	if err == nil {
		t.Fatal("expected second trust draft creation to fail")
	}
	if !strings.Contains(err.Error(), controlapipkg.DenialCodeSiteTrustDraftExists) {
		t.Fatalf("expected trust-draft-exists denial, got %v", err)
	}
}

func TestInspectSite_FailsClosedOnAuditFailure(t *testing.T) {
	repoRoot := t.TempDir()
	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"status":{"description":"All Systems Operational","indicator":"none"}}`)
	}))
	defer providerServer.Close()

	client, _, server := startLoopgateServer(t, repoRoot, loopgateHTTPPolicyYAML(false))
	server.appendAuditEvent = func(string, ledger.Event) error {
		return errors.New("audit down")
	}

	_, err := client.InspectSite(context.Background(), controlapipkg.SiteInspectionRequest{URL: providerServer.URL})
	if err == nil {
		t.Fatal("expected inspect audit failure")
	}
	if !strings.Contains(err.Error(), controlapipkg.DenialCodeAuditUnavailable) {
		t.Fatalf("expected audit unavailable denial, got %v", err)
	}
}
