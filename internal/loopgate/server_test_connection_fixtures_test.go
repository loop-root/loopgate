package loopgate

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfiguredConnectionYAML(t *testing.T, repoRoot string, providerBaseURL string) {
	t.Helper()

	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: example\n" +
		"grant_type: client_credentials\n" +
		"subject: service-bot\n" +
		"client_id: example-client\n" +
		"token_url: " + providerBaseURL + "/oauth/token\n" +
		"api_base_url: " + providerBaseURL + "/api\n" +
		"allowed_hosts:\n" +
		"  - 127.0.0.1\n" +
		"scopes:\n" +
		"  - status.read\n" +
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
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 256\n" +
		"      - name: healthy\n" +
		"        sensitivity: benign\n" +
		"        max_inline_bytes: 32\n"
	if err := os.WriteFile(filepath.Join(connectionDir, "example.yaml"), []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured connection yaml: %v", err)
	}
}

func writeConfiguredConnectionYAMLWithBlobFallback(t *testing.T, repoRoot string, providerBaseURL string) {
	t.Helper()

	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: example\n" +
		"grant_type: client_credentials\n" +
		"subject: service-bot\n" +
		"client_id: example-client\n" +
		"token_url: " + providerBaseURL + "/oauth/token\n" +
		"api_base_url: " + providerBaseURL + "/api\n" +
		"allowed_hosts:\n" +
		"  - 127.0.0.1\n" +
		"scopes:\n" +
		"  - status.read\n" +
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
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 256\n" +
		"        allow_blob_ref_fallback: true\n" +
		"      - name: healthy\n" +
		"        sensitivity: benign\n" +
		"        max_inline_bytes: 32\n"
	if err := os.WriteFile(filepath.Join(connectionDir, "example.yaml"), []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured connection yaml with blob fallback: %v", err)
	}
}

func writeConfiguredMarkdownFrontmatterYAML(t *testing.T, repoRoot string, providerBaseURL string) {
	t.Helper()

	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: docs\n" +
		"grant_type: client_credentials\n" +
		"subject: docs-bot\n" +
		"client_id: docs-client\n" +
		"token_url: " + providerBaseURL + "/oauth/token\n" +
		"api_base_url: " + providerBaseURL + "/api\n" +
		"allowed_hosts:\n" +
		"  - 127.0.0.1\n" +
		"scopes:\n" +
		"  - docs.read\n" +
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
		"        sensitivity: benign\n" +
		"        max_inline_bytes: 64\n" +
		"      - name: published\n" +
		"        frontmatter_key: published\n" +
		"        sensitivity: benign\n" +
		"        max_inline_bytes: 16\n"
	if err := os.WriteFile(filepath.Join(connectionDir, "docs.yaml"), []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured markdown frontmatter yaml: %v", err)
	}
}

func writeConfiguredMarkdownSectionYAML(t *testing.T, repoRoot string, providerBaseURL string) {
	t.Helper()

	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: docs\n" +
		"grant_type: client_credentials\n" +
		"subject: docs-bot\n" +
		"client_id: docs-client\n" +
		"token_url: " + providerBaseURL + "/oauth/token\n" +
		"api_base_url: " + providerBaseURL + "/api\n" +
		"allowed_hosts:\n" +
		"  - 127.0.0.1\n" +
		"scopes:\n" +
		"  - docs.read\n" +
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
		"          - Release Notes\n" +
		"          - Overview\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 256\n"
	if err := os.WriteFile(filepath.Join(connectionDir, "docs.yaml"), []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured markdown section yaml: %v", err)
	}
}

func writeConfiguredHTMLMetaYAML(t *testing.T, repoRoot string, providerBaseURL string) {
	t.Helper()

	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: docshtml\n" +
		"grant_type: client_credentials\n" +
		"subject: docs-bot\n" +
		"client_id: docs-client\n" +
		"token_url: " + providerBaseURL + "/oauth/token\n" +
		"api_base_url: " + providerBaseURL + "/api\n" +
		"allowed_hosts:\n" +
		"  - 127.0.0.1\n" +
		"scopes:\n" +
		"  - docs.read\n" +
		"credential:\n" +
		"  id: docs-client-secret\n" +
		"  backend: env\n" +
		"  account_name: LOOPGATE_EXAMPLE_SECRET\n" +
		"  scope: docs.read\n" +
		"capabilities:\n" +
		"  - name: docshtml.page_get\n" +
		"    description: Read HTML page metadata.\n" +
		"    method: GET\n" +
		"    path: /page.html\n" +
		"    content_class: html\n" +
		"    extractor: html_meta_allowlist\n" +
		"    response_fields:\n" +
		"      - name: page_title\n" +
		"        html_title: true\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 128\n" +
		"      - name: description\n" +
		"        meta_name: description\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 128\n" +
		"      - name: site_name\n" +
		"        meta_property: og:site_name\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 128\n"
	if err := os.WriteFile(filepath.Join(connectionDir, "docshtml.yaml"), []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured html metadata yaml: %v", err)
	}
}

func writeConfiguredPublicHTMLMetaYAML(t *testing.T, repoRoot string, providerBaseURL string) {
	t.Helper()

	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: statuspage\n" +
		"grant_type: public_read\n" +
		"subject: stripe\n" +
		"api_base_url: " + providerBaseURL + "\n" +
		"allowed_hosts:\n" +
		"  - 127.0.0.1\n" +
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
		"        max_inline_bytes: 128\n" +
		"      - name: description\n" +
		"        meta_name: description\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 128\n"
	if err := os.WriteFile(filepath.Join(connectionDir, "statuspage.yaml"), []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured public html yaml: %v", err)
	}
}

func writeConfiguredPublicJSONNestedYAML(t *testing.T, repoRoot string, providerBaseURL string) {
	t.Helper()

	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: statuspage\n" +
		"grant_type: public_read\n" +
		"subject: github\n" +
		"api_base_url: " + providerBaseURL + "\n" +
		"allowed_hosts:\n" +
		"  - 127.0.0.1\n" +
		"capabilities:\n" +
		"  - name: statuspage.summary_get\n" +
		"    description: Read public status summary fields.\n" +
		"    method: GET\n" +
		"    path: /\n" +
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
		"        max_inline_bytes: 32\n"
	if err := os.WriteFile(filepath.Join(connectionDir, "statuspage.yaml"), []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured public nested json yaml: %v", err)
	}
}

func writeConfiguredPublicJSONIssueListYAML(t *testing.T, repoRoot string, providerBaseURL string) {
	t.Helper()

	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: repoapi\n" +
		"grant_type: public_read\n" +
		"subject: sample-repo\n" +
		"api_base_url: " + providerBaseURL + "\n" +
		"allowed_hosts:\n" +
		"  - 127.0.0.1\n" +
		"capabilities:\n" +
		"  - name: repo.issues_list\n" +
		"    description: Read recent open repository issues.\n" +
		"    method: GET\n" +
		"    path: /\n" +
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
		"          - html_url\n" +
		"        max_items: 2\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 4096\n"
	if err := os.WriteFile(filepath.Join(connectionDir, "issues.yaml"), []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured public issue list yaml: %v", err)
	}
}

func writeConfiguredPKCEYAML(t *testing.T, repoRoot string, providerBaseURL string) {
	t.Helper()

	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: examplepkce\n" +
		"grant_type: pkce\n" +
		"subject: workspace-user\n" +
		"client_id: pkce-client\n" +
		"authorization_url: " + providerBaseURL + "/oauth/authorize\n" +
		"token_url: " + providerBaseURL + "/oauth/token\n" +
		"redirect_url: http://127.0.0.1/callback\n" +
		"api_base_url: " + providerBaseURL + "/api\n" +
		"allowed_hosts:\n" +
		"  - 127.0.0.1\n" +
		"scopes:\n" +
		"  - status.read\n" +
		"credential:\n" +
		"  id: pkce-refresh-token\n" +
		"  backend: secure\n" +
		"  account_name: loopgate.examplepkce.workspace-user\n" +
		"  scope: examplepkce.status_read\n" +
		"capabilities:\n" +
		"  - name: examplepkce.status_get\n" +
		"    description: Read example PKCE provider status.\n" +
		"    method: GET\n" +
		"    path: /status\n" +
		"    content_class: structured_json\n" +
		"    extractor: json_field_allowlist\n" +
		"    response_fields:\n" +
		"      - name: service\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 256\n" +
		"      - name: healthy\n" +
		"        sensitivity: benign\n" +
		"        max_inline_bytes: 32\n" +
		"      - name: generation\n" +
		"        sensitivity: benign\n" +
		"        max_inline_bytes: 32\n"
	if err := os.WriteFile(filepath.Join(connectionDir, "examplepkce.yaml"), []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured pkce yaml: %v", err)
	}
}
