package loopgate

import (
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	if !strings.Contains(err.Error(), controlapipkg.ResultFieldSensitivityTaintedText) {
		t.Fatalf("unexpected error: %v", err)
	}
}
