package loopgate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testRepoRoot(t *testing.T) string {
	t.Helper()
	_, currentFilePath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFilePath), "..", ".."))
}

func TestParseAndValidateRedirectURL_AllowsHTTPSAndLoopbackHTTPOnly(t *testing.T) {
	validRedirects := []string{
		"https://example.test/callback",
		"http://127.0.0.1/callback",
		"http://localhost:8080/callback",
	}
	for _, rawRedirectURL := range validRedirects {
		if _, err := parseAndValidateRedirectURL(rawRedirectURL); err != nil {
			t.Fatalf("expected redirect_url %q to be allowed, got %v", rawRedirectURL, err)
		}
	}
}

func TestParseAndValidateRedirectURL_DeniesCustomAndUnsafeSchemes(t *testing.T) {
	invalidRedirects := []string{
		"myapp:/callback",
		"myapp://callback",
		"file:///tmp/callback",
		"ssh://example.test/callback",
		"http://example.test/callback",
	}
	for _, rawRedirectURL := range invalidRedirects {
		_, err := parseAndValidateRedirectURL(rawRedirectURL)
		if err == nil {
			t.Fatalf("expected redirect_url %q to be denied", rawRedirectURL)
		}
		if !strings.Contains(err.Error(), "https or localhost http") {
			t.Fatalf("unexpected error for %q: %v", rawRedirectURL, err)
		}
	}
}

func TestLoadConfiguredConnectionFile_RequiresExplicitFieldSensitivity(t *testing.T) {
	repoRoot := t.TempDir()
	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: example\n" +
		"grant_type: client_credentials\n" +
		"subject: service-bot\n" +
		"client_id: example-client\n" +
		"token_url: https://example.test/oauth/token\n" +
		"api_base_url: https://example.test/api\n" +
		"allowed_hosts:\n" +
		"  - example.test\n" +
		"credential:\n" +
		"  id: example-client-secret\n" +
		"  backend: env\n" +
		"  account_name: LOOPGATE_EXAMPLE_SECRET\n" +
		"  scope: example.status_read\n" +
		"capabilities:\n" +
		"  - name: example.status_get\n" +
		"    description: Read example provider status.\n" +
		"    method: GET\n" +
		"    path: /status\n" +
		"    content_class: structured_json\n" +
		"    extractor: json_field_allowlist\n" +
		"    response_fields:\n" +
		"      - name: service\n" +
		"        max_inline_bytes: 256\n"
	configPath := filepath.Join(connectionDir, "example.yaml")
	if err := os.WriteFile(configPath, []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured connection yaml: %v", err)
	}

	_, _, err := loadConfiguredConnectionFile(configPath)
	if err == nil {
		t.Fatal("expected missing sensitivity to be denied")
	}
	if !strings.Contains(err.Error(), "sensitivity") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfiguredConnectionFile_PublicReadAllowsCredentiallessHTMLMetadata(t *testing.T) {
	repoRoot := t.TempDir()
	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: statuspage\n" +
		"grant_type: public_read\n" +
		"subject: stripe\n" +
		"api_base_url: https://status.example.test\n" +
		"allowed_hosts:\n" +
		"  - status.example.test\n" +
		"capabilities:\n" +
		"  - name: statuspage.summary_get\n" +
		"    description: Read public status page metadata.\n" +
		"    method: GET\n" +
		"    path: /\n" +
		"    content_class: html\n" +
		"    extractor: html_meta_allowlist\n" +
		"    response_fields:\n" +
		"      - name: page_title\n" +
		"        html_title: true\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 128\n"
	configPath := filepath.Join(connectionDir, "statuspage.yaml")
	if err := os.WriteFile(configPath, []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured connection yaml: %v", err)
	}

	loadedConnection, loadedCapabilities, err := loadConfiguredConnectionFile(configPath)
	if err != nil {
		t.Fatalf("expected public_read config to load, got %v", err)
	}
	if loadedConnection.Registration.GrantType != GrantTypePublicRead {
		t.Fatalf("unexpected grant type: %#v", loadedConnection.Registration)
	}
	if loadedConnection.TokenURL != nil || loadedConnection.AuthorizationURL != nil {
		t.Fatalf("public_read should not parse oauth endpoints, got %#v", loadedConnection)
	}
	if len(loadedCapabilities) != 1 {
		t.Fatalf("expected one public capability, got %#v", loadedCapabilities)
	}
}

func TestLoadConfiguredConnectionFile_CheckedInPublicStatusExampleLoads(t *testing.T) {
	configPath := filepath.Join(testRepoRoot(t), "docs", "setup", "examples", "public_status_github.yaml")

	loadedConnection, loadedCapabilities, err := loadConfiguredConnectionFile(configPath)
	if err != nil {
		t.Fatalf("load checked-in public status example: %v", err)
	}
	if loadedConnection.Registration.GrantType != GrantTypePublicRead {
		t.Fatalf("unexpected grant type: %#v", loadedConnection.Registration)
	}
	capabilityDefinition, found := loadedCapabilities["statuspage.summary_get"]
	if !found {
		t.Fatalf("expected checked-in status capability, got %#v", loadedCapabilities)
	}
	if capabilityDefinition.Extractor != extractorJSONNestedSelectorConfig {
		t.Fatalf("unexpected status extractor: %#v", capabilityDefinition)
	}
}

func TestLoadConfiguredConnectionFile_PublicReadAllowsNestedJSONSelectors(t *testing.T) {
	repoRoot := t.TempDir()
	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: statuspage\n" +
		"grant_type: public_read\n" +
		"subject: github\n" +
		"api_base_url: https://www.githubstatus.com/api/v2\n" +
		"allowed_hosts:\n" +
		"  - www.githubstatus.com\n" +
		"capabilities:\n" +
		"  - name: statuspage.summary_get\n" +
		"    description: Read public status summary fields.\n" +
		"    method: GET\n" +
		"    path: /status.json\n" +
		"    content_class: structured_json\n" +
		"    extractor: json_nested_selector\n" +
		"    response_fields:\n" +
		"      - name: status_description\n" +
		"        json_path: status.description\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 128\n" +
		"      - name: status_indicator\n" +
		"        json_path: status.indicator\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 64\n"
	configPath := filepath.Join(connectionDir, "statuspage.yaml")
	if err := os.WriteFile(configPath, []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured connection yaml: %v", err)
	}

	loadedConnection, loadedCapabilities, err := loadConfiguredConnectionFile(configPath)
	if err != nil {
		t.Fatalf("expected nested json public_read config to load, got %v", err)
	}
	if loadedConnection.Registration.GrantType != GrantTypePublicRead {
		t.Fatalf("unexpected grant type: %#v", loadedConnection.Registration)
	}
	capabilityDefinition, found := loadedCapabilities["statuspage.summary_get"]
	if !found {
		t.Fatalf("expected configured capability, got %#v", loadedCapabilities)
	}
	if capabilityDefinition.Extractor != extractorJSONNestedSelectorConfig {
		t.Fatalf("unexpected extractor: %#v", capabilityDefinition)
	}
	if capabilityDefinition.ResponseFields[0].JSONPath != "status.description" {
		t.Fatalf("unexpected json path: %#v", capabilityDefinition.ResponseFields[0])
	}
}

func TestLoadConfiguredConnectionFile_PublicReadAllowsJSONIssueListSelector(t *testing.T) {
	repoRoot := t.TempDir()
	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: repoapi\n" +
		"grant_type: public_read\n" +
		"subject: sample-repo\n" +
		"api_base_url: https://api.example.test\n" +
		"allowed_hosts:\n" +
		"  - api.example.test\n" +
		"capabilities:\n" +
		"  - name: repo.issues_list\n" +
		"    description: Read recent open repository issues.\n" +
		"    method: GET\n" +
		"    path: /repos/sample/issues\n" +
		"    content_class: structured_json\n" +
		"    extractor: json_object_list_selector\n" +
		"    response_fields:\n" +
		"      - name: issues\n" +
		"        json_path: issues.items\n" +
		"        json_list_item_fields:\n" +
		"          - number\n" +
		"          - title\n" +
		"          - state\n" +
		"          - updated_at\n" +
		"        max_items: 5\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 2048\n"
	configPath := filepath.Join(connectionDir, "issues.yaml")
	if err := os.WriteFile(configPath, []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured connection yaml: %v", err)
	}

	loadedConnection, loadedCapabilities, err := loadConfiguredConnectionFile(configPath)
	if err != nil {
		t.Fatalf("expected issue-list config to load, got %v", err)
	}
	if loadedConnection.Registration.GrantType != GrantTypePublicRead {
		t.Fatalf("unexpected grant type: %#v", loadedConnection.Registration)
	}
	capabilityDefinition, found := loadedCapabilities["repo.issues_list"]
	if !found {
		t.Fatalf("expected configured capability, got %#v", loadedCapabilities)
	}
	if capabilityDefinition.Extractor != extractorJSONObjectListSelector {
		t.Fatalf("unexpected extractor: %#v", capabilityDefinition)
	}
	if capabilityDefinition.ResponseFields[0].MaxItems != 5 {
		t.Fatalf("unexpected max_items: %#v", capabilityDefinition.ResponseFields[0])
	}
	if len(capabilityDefinition.ResponseFields[0].JSONListItemFields) != 4 {
		t.Fatalf("unexpected item fields: %#v", capabilityDefinition.ResponseFields[0])
	}
}

func TestLoadConfiguredConnectionFile_CheckedInPublicRepoIssuesExampleLoads(t *testing.T) {
	configPath := filepath.Join(testRepoRoot(t), "docs", "setup", "examples", "public_repo_issues_generic.yaml")

	loadedConnection, loadedCapabilities, err := loadConfiguredConnectionFile(configPath)
	if err != nil {
		t.Fatalf("load checked-in public repo issues example: %v", err)
	}
	if loadedConnection.Registration.GrantType != GrantTypePublicRead {
		t.Fatalf("unexpected grant type: %#v", loadedConnection.Registration)
	}
	capabilityDefinition, found := loadedCapabilities["repo.issues_list"]
	if !found {
		t.Fatalf("expected checked-in issues capability, got %#v", loadedCapabilities)
	}
	if capabilityDefinition.Extractor != extractorJSONObjectListSelector {
		t.Fatalf("unexpected issues extractor: %#v", capabilityDefinition)
	}
	if capabilityDefinition.ResponseFields[0].MaxItems != 5 {
		t.Fatalf("unexpected max_items: %#v", capabilityDefinition.ResponseFields[0])
	}
}

func TestLoadConfiguredConnectionFile_DeniesMalformedJSONObjectListSelector(t *testing.T) {
	repoRoot := t.TempDir()
	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: repoapi\n" +
		"grant_type: public_read\n" +
		"subject: sample-repo\n" +
		"api_base_url: https://api.example.test\n" +
		"allowed_hosts:\n" +
		"  - api.example.test\n" +
		"capabilities:\n" +
		"  - name: repo.issues_list\n" +
		"    description: Read recent open repository issues.\n" +
		"    method: GET\n" +
		"    path: /repos/sample/issues\n" +
		"    content_class: structured_json\n" +
		"    extractor: json_object_list_selector\n" +
		"    response_fields:\n" +
		"      - name: issues\n" +
		"        json_path: issues.items\n" +
		"        max_items: 5\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 2048\n"
	configPath := filepath.Join(connectionDir, "issues.yaml")
	if err := os.WriteFile(configPath, []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured connection yaml: %v", err)
	}

	_, _, err := loadConfiguredConnectionFile(configPath)
	if err == nil {
		t.Fatal("expected malformed json object list selector to be denied")
	}
	if !strings.Contains(err.Error(), "json_list_item_fields") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfiguredConnectionFile_DeniesMalformedJSONNestedSelector(t *testing.T) {
	repoRoot := t.TempDir()
	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: statuspage\n" +
		"grant_type: public_read\n" +
		"subject: github\n" +
		"api_base_url: https://www.githubstatus.com/api/v2\n" +
		"allowed_hosts:\n" +
		"  - www.githubstatus.com\n" +
		"capabilities:\n" +
		"  - name: statuspage.summary_get\n" +
		"    description: Read public status summary fields.\n" +
		"    method: GET\n" +
		"    path: /status.json\n" +
		"    content_class: structured_json\n" +
		"    extractor: json_nested_selector\n" +
		"    response_fields:\n" +
		"      - name: status_description\n" +
		"        json_path: status\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 128\n"
	configPath := filepath.Join(connectionDir, "statuspage.yaml")
	if err := os.WriteFile(configPath, []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured connection yaml: %v", err)
	}

	_, _, err := loadConfiguredConnectionFile(configPath)
	if err == nil {
		t.Fatal("expected malformed json_path to be denied")
	}
	if !strings.Contains(err.Error(), "json_path") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfiguredConnectionFile_PublicReadRejectsCredentialRef(t *testing.T) {
	repoRoot := t.TempDir()
	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: statuspage\n" +
		"grant_type: public_read\n" +
		"subject: stripe\n" +
		"api_base_url: https://status.example.test\n" +
		"allowed_hosts:\n" +
		"  - status.example.test\n" +
		"credential:\n" +
		"  id: should-not-exist\n" +
		"  backend: env\n" +
		"  account_name: LOOPGATE_STATUS_SECRET\n" +
		"  scope: status.read\n" +
		"capabilities:\n" +
		"  - name: statuspage.summary_get\n" +
		"    description: Read public status page metadata.\n" +
		"    method: GET\n" +
		"    path: /\n" +
		"    content_class: html\n" +
		"    extractor: html_meta_allowlist\n" +
		"    response_fields:\n" +
		"      - name: page_title\n" +
		"        html_title: true\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 128\n"
	configPath := filepath.Join(connectionDir, "statuspage.yaml")
	if err := os.WriteFile(configPath, []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured connection yaml: %v", err)
	}

	_, _, err := loadConfiguredConnectionFile(configPath)
	if err == nil {
		t.Fatal("expected public_read credential ref to be denied")
	}
	if !strings.Contains(err.Error(), "must not define a secret ref") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfiguredConnectionFile_RequiresExplicitFieldMaxInlineBytes(t *testing.T) {
	repoRoot := t.TempDir()
	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: example\n" +
		"grant_type: client_credentials\n" +
		"subject: service-bot\n" +
		"client_id: example-client\n" +
		"token_url: https://example.test/oauth/token\n" +
		"api_base_url: https://example.test/api\n" +
		"allowed_hosts:\n" +
		"  - example.test\n" +
		"credential:\n" +
		"  id: example-client-secret\n" +
		"  backend: env\n" +
		"  account_name: LOOPGATE_EXAMPLE_SECRET\n" +
		"  scope: example.status_read\n" +
		"capabilities:\n" +
		"  - name: example.status_get\n" +
		"    description: Read example provider status.\n" +
		"    method: GET\n" +
		"    path: /status\n" +
		"    content_class: structured_json\n" +
		"    extractor: json_field_allowlist\n" +
		"    response_fields:\n" +
		"      - name: service\n" +
		"        sensitivity: tainted_text\n"
	configPath := filepath.Join(connectionDir, "example.yaml")
	if err := os.WriteFile(configPath, []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured connection yaml: %v", err)
	}

	_, _, err := loadConfiguredConnectionFile(configPath)
	if err == nil {
		t.Fatal("expected missing max_inline_bytes to be denied")
	}
	if !strings.Contains(err.Error(), "max_inline_bytes") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfiguredConnectionFile_DeniesSuspiciousRemoteFieldNameByDefault(t *testing.T) {
	repoRoot := t.TempDir()
	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: example\n" +
		"grant_type: client_credentials\n" +
		"subject: service-bot\n" +
		"client_id: example-client\n" +
		"token_url: https://example.test/oauth/token\n" +
		"api_base_url: https://example.test/api\n" +
		"allowed_hosts:\n" +
		"  - example.test\n" +
		"credential:\n" +
		"  id: example-client-secret\n" +
		"  backend: env\n" +
		"  account_name: LOOPGATE_EXAMPLE_SECRET\n" +
		"  scope: example.status_read\n" +
		"capabilities:\n" +
		"  - name: example.status_get\n" +
		"    description: Read example provider status.\n" +
		"    method: GET\n" +
		"    path: /status\n" +
		"    content_class: structured_json\n" +
		"    extractor: json_field_allowlist\n" +
		"    response_fields:\n" +
		"      - name: instructions\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 256\n"
	configPath := filepath.Join(connectionDir, "example.yaml")
	if err := os.WriteFile(configPath, []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured connection yaml: %v", err)
	}

	_, _, err := loadConfiguredConnectionFile(configPath)
	if err == nil {
		t.Fatal("expected suspicious field name to be denied")
	}
	if !strings.Contains(err.Error(), "allow_suspicious_name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfiguredConnectionFile_AllowsSuspiciousRemoteFieldNameWhenExplicitlyPermitted(t *testing.T) {
	repoRoot := t.TempDir()
	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: example\n" +
		"grant_type: client_credentials\n" +
		"subject: service-bot\n" +
		"client_id: example-client\n" +
		"token_url: https://example.test/oauth/token\n" +
		"api_base_url: https://example.test/api\n" +
		"allowed_hosts:\n" +
		"  - example.test\n" +
		"credential:\n" +
		"  id: example-client-secret\n" +
		"  backend: env\n" +
		"  account_name: LOOPGATE_EXAMPLE_SECRET\n" +
		"  scope: example.status_read\n" +
		"capabilities:\n" +
		"  - name: example.status_get\n" +
		"    description: Read example provider status.\n" +
		"    method: GET\n" +
		"    path: /status\n" +
		"    content_class: structured_json\n" +
		"    extractor: json_field_allowlist\n" +
		"    response_fields:\n" +
		"      - name: instructions\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 256\n" +
		"        allow_suspicious_name: true\n"
	configPath := filepath.Join(connectionDir, "example.yaml")
	if err := os.WriteFile(configPath, []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured connection yaml: %v", err)
	}

	_, _, err := loadConfiguredConnectionFile(configPath)
	if err != nil {
		t.Fatalf("expected suspicious field name override to load, got %v", err)
	}
}

func TestLoadConfiguredConnectionFile_MarkdownFrontmatterRequiresFrontmatterKey(t *testing.T) {
	repoRoot := t.TempDir()
	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: docs\n" +
		"grant_type: client_credentials\n" +
		"subject: docs-bot\n" +
		"client_id: docs-client\n" +
		"token_url: https://example.test/oauth/token\n" +
		"api_base_url: https://example.test/api\n" +
		"allowed_hosts:\n" +
		"  - example.test\n" +
		"credential:\n" +
		"  id: docs-client-secret\n" +
		"  backend: env\n" +
		"  account_name: LOOPGATE_EXAMPLE_SECRET\n" +
		"  scope: docs.read\n" +
		"capabilities:\n" +
		"  - name: docs.release_get\n" +
		"    description: Read release metadata.\n" +
		"    method: GET\n" +
		"    path: /release.md\n" +
		"    content_class: markdown\n" +
		"    extractor: markdown_frontmatter_keys\n" +
		"    response_fields:\n" +
		"      - name: version\n" +
		"        sensitivity: benign\n" +
		"        max_inline_bytes: 64\n"
	configPath := filepath.Join(connectionDir, "docs.yaml")
	if err := os.WriteFile(configPath, []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured connection yaml: %v", err)
	}

	_, _, err := loadConfiguredConnectionFile(configPath)
	if err == nil {
		t.Fatal("expected missing frontmatter_key to be denied")
	}
	if !strings.Contains(err.Error(), "frontmatter_key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfiguredConnectionFile_MarkdownFrontmatterRejectsJSONField(t *testing.T) {
	repoRoot := t.TempDir()
	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: docs\n" +
		"grant_type: client_credentials\n" +
		"subject: docs-bot\n" +
		"client_id: docs-client\n" +
		"token_url: https://example.test/oauth/token\n" +
		"api_base_url: https://example.test/api\n" +
		"allowed_hosts:\n" +
		"  - example.test\n" +
		"credential:\n" +
		"  id: docs-client-secret\n" +
		"  backend: env\n" +
		"  account_name: LOOPGATE_EXAMPLE_SECRET\n" +
		"  scope: docs.read\n" +
		"capabilities:\n" +
		"  - name: docs.release_get\n" +
		"    description: Read release metadata.\n" +
		"    method: GET\n" +
		"    path: /release.md\n" +
		"    content_class: markdown\n" +
		"    extractor: markdown_frontmatter_keys\n" +
		"    response_fields:\n" +
		"      - name: version\n" +
		"        frontmatter_key: version\n" +
		"        json_field: version\n" +
		"        sensitivity: benign\n" +
		"        max_inline_bytes: 64\n"
	configPath := filepath.Join(connectionDir, "docs.yaml")
	if err := os.WriteFile(configPath, []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured connection yaml: %v", err)
	}

	_, _, err := loadConfiguredConnectionFile(configPath)
	if err == nil {
		t.Fatal("expected json_field on markdown extractor to be denied")
	}
	if !strings.Contains(err.Error(), "must not set json_field") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfiguredConnectionFile_MarkdownSectionRequiresHeadingPath(t *testing.T) {
	repoRoot := t.TempDir()
	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: docs\n" +
		"grant_type: client_credentials\n" +
		"subject: docs-bot\n" +
		"client_id: docs-client\n" +
		"token_url: https://example.test/oauth/token\n" +
		"api_base_url: https://example.test/api\n" +
		"allowed_hosts:\n" +
		"  - example.test\n" +
		"credential:\n" +
		"  id: docs-client-secret\n" +
		"  backend: env\n" +
		"  account_name: LOOPGATE_EXAMPLE_SECRET\n" +
		"  scope: docs.read\n" +
		"capabilities:\n" +
		"  - name: docs.section_get\n" +
		"    description: Read release section.\n" +
		"    method: GET\n" +
		"    path: /release.md\n" +
		"    content_class: markdown\n" +
		"    extractor: markdown_section_selector\n" +
		"    response_fields:\n" +
		"      - name: summary\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 64\n"
	configPath := filepath.Join(connectionDir, "docs.yaml")
	if err := os.WriteFile(configPath, []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured connection yaml: %v", err)
	}

	_, _, err := loadConfiguredConnectionFile(configPath)
	if err == nil {
		t.Fatal("expected missing heading_path to be denied")
	}
	if !strings.Contains(err.Error(), "heading_path") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfiguredConnectionFile_HTMLMetaAllowlistRequiresExactlyOneSelector(t *testing.T) {
	repoRoot := t.TempDir()
	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: docs\n" +
		"grant_type: client_credentials\n" +
		"subject: docs-bot\n" +
		"client_id: docs-client\n" +
		"token_url: https://example.test/oauth/token\n" +
		"api_base_url: https://example.test/api\n" +
		"allowed_hosts:\n" +
		"  - example.test\n" +
		"credential:\n" +
		"  id: docs-client-secret\n" +
		"  backend: env\n" +
		"  account_name: LOOPGATE_EXAMPLE_SECRET\n" +
		"  scope: docs.read\n" +
		"capabilities:\n" +
		"  - name: docs.page_get\n" +
		"    description: Read page metadata.\n" +
		"    method: GET\n" +
		"    path: /page.html\n" +
		"    content_class: html\n" +
		"    extractor: html_meta_allowlist\n" +
		"    response_fields:\n" +
		"      - name: page_title\n" +
		"        html_title: true\n" +
		"        meta_name: description\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 128\n"
	configPath := filepath.Join(connectionDir, "docs.yaml")
	if err := os.WriteFile(configPath, []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured html yaml: %v", err)
	}

	_, _, err := loadConfiguredConnectionFile(configPath)
	if err == nil {
		t.Fatal("expected invalid html selector combination to be denied")
	}
	if !strings.Contains(err.Error(), "exactly one of html_title, meta_name, or meta_property") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfiguredConnectionFile_HTMLMetaAllowlistRequiresTaintedTextSensitivity(t *testing.T) {
	repoRoot := t.TempDir()
	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: docs\n" +
		"grant_type: client_credentials\n" +
		"subject: docs-bot\n" +
		"client_id: docs-client\n" +
		"token_url: https://example.test/oauth/token\n" +
		"api_base_url: https://example.test/api\n" +
		"allowed_hosts:\n" +
		"  - example.test\n" +
		"credential:\n" +
		"  id: docs-client-secret\n" +
		"  backend: env\n" +
		"  account_name: LOOPGATE_EXAMPLE_SECRET\n" +
		"  scope: docs.read\n" +
		"capabilities:\n" +
		"  - name: docs.page_get\n" +
		"    description: Read page metadata.\n" +
		"    method: GET\n" +
		"    path: /page.html\n" +
		"    content_class: html\n" +
		"    extractor: html_meta_allowlist\n" +
		"    response_fields:\n" +
		"      - name: description\n" +
		"        meta_name: description\n" +
		"        sensitivity: benign\n" +
		"        max_inline_bytes: 128\n"
	configPath := filepath.Join(connectionDir, "docs.yaml")
	if err := os.WriteFile(configPath, []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured html yaml: %v", err)
	}

	_, _, err := loadConfiguredConnectionFile(configPath)
	if err == nil {
		t.Fatal("expected non-tainted html metadata sensitivity to be denied")
	}
	if !strings.Contains(err.Error(), "html metadata extraction must use sensitivity") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfiguredConnectionFile_MarkdownSectionRequiresTaintedTextSensitivity(t *testing.T) {
	repoRoot := t.TempDir()
	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: docs\n" +
		"grant_type: client_credentials\n" +
		"subject: docs-bot\n" +
		"client_id: docs-client\n" +
		"token_url: https://example.test/oauth/token\n" +
		"api_base_url: https://example.test/api\n" +
		"allowed_hosts:\n" +
		"  - example.test\n" +
		"credential:\n" +
		"  id: docs-client-secret\n" +
		"  backend: env\n" +
		"  account_name: LOOPGATE_EXAMPLE_SECRET\n" +
		"  scope: docs.read\n" +
		"capabilities:\n" +
		"  - name: docs.section_get\n" +
		"    description: Read release section.\n" +
		"    method: GET\n" +
		"    path: /release.md\n" +
		"    content_class: markdown\n" +
		"    extractor: markdown_section_selector\n" +
		"    response_fields:\n" +
		"      - name: summary\n" +
		"        heading_path:\n" +
		"          - Overview\n" +
		"        sensitivity: benign\n" +
		"        max_inline_bytes: 64\n"
	configPath := filepath.Join(connectionDir, "docs.yaml")
	if err := os.WriteFile(configPath, []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured connection yaml: %v", err)
	}

	_, _, err := loadConfiguredConnectionFile(configPath)
	if err == nil {
		t.Fatal("expected benign sensitivity to be denied for markdown section extraction")
	}
	if !strings.Contains(err.Error(), ResultFieldSensitivityTaintedText) {
		t.Fatalf("unexpected error: %v", err)
	}
}
