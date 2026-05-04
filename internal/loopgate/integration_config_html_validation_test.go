package loopgate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
