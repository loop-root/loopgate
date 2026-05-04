package loopgate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
