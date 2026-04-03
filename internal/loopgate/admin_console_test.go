package loopgate

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testAdminToken = "test-admin-token-for-tests-only-00"

func writeRuntimeYAMLForAdminTest(t *testing.T, repoRoot string, deploymentTenantID string) {
	t.Helper()
	configDir := filepath.Join(repoRoot, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	tenantIDYAML := `""`
	if deploymentTenantID != "" {
		tenantIDYAML = `"` + deploymentTenantID + `"`
	}
	raw := `version: "1"
tenancy:
  deployment_tenant_id: ` + tenantIDYAML + `
  deployment_user_id: ""
admin_console:
  enabled: true
  listen_addr: "127.0.0.1:0"
logging:
  audit_ledger:
    max_event_bytes: 262144
    rotate_at_bytes: 134217728
    segment_dir: "runtime/state/loopgate_event_segments"
    manifest_path: "runtime/state/loopgate_event_segments/manifest.jsonl"
memory:
  candidate_panel_size: 3
  decomposition_preference: "hybrid_schema_guided"
  review_preference: "risk_tiered"
  soft_morphling_concurrency: 3
  batching_preference: "pause_on_wave_failure"
`
	path := filepath.Join(configDir, "runtime.yaml")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write runtime.yaml: %v", err)
	}
}

func httptestClientNoRedirects(client *http.Client) *http.Client {
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &c
}

func newServerWithAdminTCP(t *testing.T, repoRoot string, deploymentTenantID string) *Server {
	t.Helper()
	t.Setenv("LOOPGATE_ADMIN_TOKEN", testAdminToken)

	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir policy dir: %v", err)
	}
	if err := os.WriteFile(policyPath, []byte(loopgatePolicyYAML(false)), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	writeTestMorphlingClassPolicy(t, repoRoot)
	writeRuntimeYAMLForAdminTest(t, repoRoot, deploymentTenantID)

	socketFile, err := os.CreateTemp("", "loopgate-admin-*.sock")
	if err != nil {
		t.Fatalf("temp socket: %v", err)
	}
	socketPath := socketFile.Name()
	_ = socketFile.Close()
	_ = os.Remove(socketPath)
	t.Cleanup(func() { _ = os.Remove(socketPath) })

	server, err := NewServerWithOptions(repoRoot, socketPath, false, true)
	if err != nil {
		t.Fatalf("NewServerWithOptions admin: %v", err)
	}
	return server
}

func TestAdminConsole_UnauthenticatedRedirect(t *testing.T) {
	repoRoot := t.TempDir()
	server := newServerWithAdminTCP(t, repoRoot, "")
	ts := httptest.NewServer(server.adminHTTPServer.Handler)
	t.Cleanup(ts.Close)

	response, err := httptestClientNoRedirects(ts.Client()).Get(ts.URL + "/admin/policy")
	if err != nil {
		t.Fatalf("GET policy: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected %d, got %d", http.StatusSeeOther, response.StatusCode)
	}
	loc := response.Header.Get("Location")
	if !strings.Contains(loc, "/admin/login") {
		t.Fatalf("unexpected Location: %q", loc)
	}
}

func TestAdminConsole_LoginAndPolicy(t *testing.T) {
	repoRoot := t.TempDir()
	server := newServerWithAdminTCP(t, repoRoot, "")
	ts := httptest.NewServer(server.adminHTTPServer.Handler)
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := ts.Client()
	client.Jar = jar
	noRedirectClient := httptestClientNoRedirects(client)

	loginURL := ts.URL + "/admin/login"
	postValues := url.Values{}
	postValues.Set("token", testAdminToken)
	loginResponse, err := noRedirectClient.PostForm(loginURL, postValues)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	_ = loginResponse.Body.Close()
	if loginResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("login expected redirect, got %d", loginResponse.StatusCode)
	}

	policyResponse, err := client.Get(ts.URL + "/admin/policy")
	if err != nil {
		t.Fatalf("GET policy: %v", err)
	}
	body, err := io.ReadAll(policyResponse.Body)
	_ = policyResponse.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if policyResponse.StatusCode != http.StatusOK {
		t.Fatalf("policy status %d body %s", policyResponse.StatusCode, string(body))
	}
	page := string(body)
	if !strings.Contains(page, "Active policy") || !strings.Contains(page, "Morphling classes") {
		t.Fatalf("unexpected policy page: %s", page)
	}
}

func TestAdminConsole_CSVRedactsSensitiveKeys(t *testing.T) {
	repoRoot := t.TempDir()
	server := newServerWithAdminTCP(t, repoRoot, "")
	auditDir := filepath.Dir(server.auditPath)
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		t.Fatalf("mkdir audit: %v", err)
	}
	line := `{"v":1,"ts":"2026-04-01T12:00:00Z","type":"test.event","session":"sess_x","data":{"tenant_id":"","user_id":"","api_key":"super-secret-value"}}` + "\n"
	if err := os.WriteFile(server.auditPath, []byte(line), 0o600); err != nil {
		t.Fatalf("write audit: %v", err)
	}

	ts := httptest.NewServer(server.adminHTTPServer.Handler)
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	client := ts.Client()
	client.Jar = jar
	postValues := url.Values{}
	postValues.Set("token", testAdminToken)
	loginResponse, err := client.PostForm(ts.URL+"/admin/login", postValues)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	_ = loginResponse.Body.Close()

	csvResponse, err := client.Get(ts.URL + "/admin/audit?format=csv&limit=10")
	if err != nil {
		t.Fatalf("csv: %v", err)
	}
	csvBody, err := io.ReadAll(csvResponse.Body)
	_ = csvResponse.Body.Close()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	if csvResponse.StatusCode != http.StatusOK {
		t.Fatalf("csv status %d: %s", csvResponse.StatusCode, string(csvBody))
	}
	if strings.Contains(string(csvBody), "super-secret-value") {
		t.Fatalf("csv leaked secret: %s", string(csvBody))
	}
	if !strings.Contains(string(csvBody), "[REDACTED]") {
		t.Fatalf("expected redaction placeholder in csv: %s", string(csvBody))
	}
}

func TestAdminConsole_AuditTenantFilter(t *testing.T) {
	repoRoot := t.TempDir()
	server := newServerWithAdminTCP(t, repoRoot, "tenant-a")
	auditDir := filepath.Dir(server.auditPath)
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		t.Fatalf("mkdir audit: %v", err)
	}
	lines := "" +
		`{"v":1,"ts":"2026-04-01T12:00:00Z","type":"keep","session":"s1","data":{"tenant_id":"tenant-a","user_id":"u1"}}` + "\n" +
		`{"v":1,"ts":"2026-04-01T12:00:01Z","type":"drop","session":"s2","data":{"tenant_id":"tenant-b","user_id":"u2"}}` + "\n"
	if err := os.WriteFile(server.auditPath, []byte(lines), 0o600); err != nil {
		t.Fatalf("write audit: %v", err)
	}

	ts := httptest.NewServer(server.adminHTTPServer.Handler)
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	client := ts.Client()
	client.Jar = jar
	postValues := url.Values{}
	postValues.Set("token", testAdminToken)
	_, _ = client.PostForm(ts.URL+"/admin/login", postValues)

	auditPage, err := client.Get(ts.URL + "/admin/audit?limit=50")
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	body, _ := io.ReadAll(auditPage.Body)
	_ = auditPage.Body.Close()
	page := string(body)
	if !strings.Contains(page, "keep") {
		t.Fatalf("expected matching event: %s", page)
	}
	if strings.Contains(page, "drop") {
		t.Fatalf("tenant-b event should be filtered: %s", page)
	}
}
