package loopgate

import (
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	if loadedConnection.Registration.GrantType != controlapipkg.GrantTypePublicRead {
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
	if loadedConnection.Registration.GrantType != controlapipkg.GrantTypePublicRead {
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
	if loadedConnection.Registration.GrantType != controlapipkg.GrantTypePublicRead {
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
	if loadedConnection.Registration.GrantType != controlapipkg.GrantTypePublicRead {
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
	if loadedConnection.Registration.GrantType != controlapipkg.GrantTypePublicRead {
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
