package loopgate

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/testutil"
)

func ageQuarantineRecordForPrune(t *testing.T, repoRoot string, quarantineRef string) {
	t.Helper()

	recordPath, err := quarantinePathFromRef(repoRoot, quarantineRef)
	if err != nil {
		t.Fatalf("quarantine path from ref: %v", err)
	}
	recordBytes, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read quarantine record: %v", err)
	}
	var sourceRecord quarantinedPayloadRecord
	if err := json.Unmarshal(recordBytes, &sourceRecord); err != nil {
		t.Fatalf("unmarshal quarantine record: %v", err)
	}
	sourceRecord.StoredAtUTC = time.Now().UTC().Add(-quarantineBlobRetentionPeriod - time.Hour).Format(time.RFC3339Nano)
	if err := writeQuarantinedPayloadRecord(recordPath, sourceRecord); err != nil {
		t.Fatalf("rewrite quarantine record: %v", err)
	}
}

func newShortLoopgateTestRepoRoot(t *testing.T) string {
	t.Helper()

	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	workspaceRoot := filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
	baseDir := filepath.Join(workspaceRoot, ".tmp-loopgate-tests")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatalf("mkdir short test base dir: %v", err)
	}
	repoRoot, err := os.MkdirTemp(baseDir, "rt-")
	if err != nil {
		t.Fatalf("mkdir short test repo root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(repoRoot) })
	return repoRoot
}

func newShortLoopgateSocketPath(t *testing.T) string {
	t.Helper()

	socketFile, err := os.CreateTemp(os.TempDir(), "loopgate-*.sock")
	if err != nil {
		t.Fatalf("create short socket file: %v", err)
	}
	socketPath := socketFile.Name()
	_ = socketFile.Close()
	_ = os.Remove(socketPath)
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	return socketPath
}

func startLoopgateServer(t *testing.T, repoRoot string, policyYAML string) (*Client, controlapipkg.StatusResponse, *Server) {
	return startLoopgateServerWithRuntime(t, repoRoot, policyYAML, nil, true)
}

func writeSignedTestPolicyYAML(t *testing.T, repoRoot string, policyYAML string) {
	t.Helper()

	policySigner, err := testutil.NewPolicyTestSigner()
	if err != nil {
		t.Fatalf("new test policy signer: %v", err)
	}
	policySigner.ConfigureEnv(t.Setenv)
	if err := policySigner.WriteSignedPolicyYAML(repoRoot, policyYAML); err != nil {
		t.Fatalf("write signed policy: %v", err)
	}
}

func writeSignedTestOperatorOverrideDocument(t *testing.T, repoRoot string, policySigner *testutil.PolicyTestSigner, document config.OperatorOverrideDocument) {
	t.Helper()

	policySigner.ConfigureEnv(t.Setenv)
	documentBytes, err := config.MarshalOperatorOverrideDocumentYAML(document)
	if err != nil {
		t.Fatalf("marshal operator override document: %v", err)
	}
	signatureFile, err := config.SignOperatorOverrideDocument(documentBytes, policySigner.KeyID, policySigner.PrivateKey)
	if err != nil {
		t.Fatalf("sign operator override document: %v", err)
	}
	if err := config.WriteOperatorOverrideDocumentYAML(repoRoot, document); err != nil {
		t.Fatalf("write operator override document: %v", err)
	}
	if err := config.WriteOperatorOverrideSignatureYAML(repoRoot, signatureFile); err != nil {
		t.Fatalf("write operator override signature: %v", err)
	}
}

func pinTestProcessAsExpectedClient(t *testing.T, server *Server) {
	t.Helper()

	testExecutablePath, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	normalizedExecutablePath := normalizeSessionExecutablePinPath(testExecutablePath)
	if strings.TrimSpace(normalizedExecutablePath) == "" {
		t.Fatal("expected normalized test executable path")
	}
	server.expectedClientPath = normalizedExecutablePath
}

// startLoopgateServerWithRuntime starts Loopgate in a temp repo. When runSessionBootstrap is false,
// the server is healthy but no control session is opened (for tests where session open must fail).
func startLoopgateServerWithRuntime(t *testing.T, repoRoot string, policyYAML string, runtimeCfg *config.RuntimeConfig, runSessionBootstrap bool) (*Client, controlapipkg.StatusResponse, *Server) {
	t.Helper()

	policySigner, err := testutil.NewPolicyTestSigner()
	if err != nil {
		t.Fatalf("new test policy signer: %v", err)
	}
	return startLoopgateServerWithSignerAndRuntime(t, repoRoot, policyYAML, policySigner, runtimeCfg, runSessionBootstrap)
}

func startLoopgateServerWithSignerAndRuntime(t *testing.T, repoRoot string, policyYAML string, policySigner *testutil.PolicyTestSigner, runtimeCfg *config.RuntimeConfig, runSessionBootstrap bool) (*Client, controlapipkg.StatusResponse, *Server) {
	t.Helper()

	policySigner.ConfigureEnv(t.Setenv)
	if err := policySigner.WriteSignedPolicyYAML(repoRoot, policyYAML); err != nil {
		t.Fatalf("write signed policy: %v", err)
	}
	if runtimeCfg != nil {
		if err := config.WriteRuntimeConfigYAML(repoRoot, *runtimeCfg); err != nil {
			t.Fatalf("write runtime config: %v", err)
		}
	}

	socketPath := newShortLoopgateSocketPath(t)
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	server.sessionOpenMinInterval = 0
	server.maxActiveSessionsPerUID = 64
	server.expirySweepMaxInterval = 0

	serverContext, cancel := context.WithCancel(context.Background())
	serverDone := make(chan struct{})
	serveErrCh := make(chan error, 1)
	go func() {
		defer close(serverDone)
		serveErrCh <- server.Serve(serverContext)
	}()
	t.Cleanup(func() {
		cancel()
		<-serverDone
	})

	client := NewClient(socketPath)
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, err = client.Health(context.Background())
		if err == nil {
			break
		}
		select {
		case serveErr := <-serveErrCh:
			t.Fatalf("loopgate serve exited before health check: %v", serveErr)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("wait for loopgate health: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}

	if !runSessionBootstrap {
		return client, controlapipkg.StatusResponse{}, server
	}

	client.ConfigureSession("test-actor", "test-session", []string{"fs_list"})
	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("bootstrap status after session: %v", err)
	}
	client.ConfigureSession("test-actor", "test-session", advertisedSessionCapabilityNames(status))
	status, err = client.Status(context.Background())
	if err != nil {
		t.Fatalf("final status after advertised session bootstrap: %v", err)
	}
	server.mu.Lock()
	server.sessionState.openByUID = make(map[uint32]time.Time)
	server.mu.Unlock()
	return client, status, server
}

func mustJSON(t *testing.T, value interface{}) []byte {
	t.Helper()
	encodedBytes, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal test json: %v", err)
	}
	return encodedBytes
}

func capabilityNames(capabilities []controlapipkg.CapabilitySummary) []string {
	names := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		names = append(names, capability.Name)
	}
	return names
}

func advertisedSessionCapabilityNames(status controlapipkg.StatusResponse) []string {
	advertisedCapabilities := capabilityNames(status.Capabilities)
	advertisedCapabilities = append(advertisedCapabilities, capabilityNames(status.ControlCapabilities)...)
	return advertisedCapabilities
}

func containsCapability(capabilities []controlapipkg.CapabilitySummary, capabilityName string) bool {
	for _, capability := range capabilities {
		if capability.Name == capabilityName {
			return true
		}
	}
	return false
}

func loopgatePolicyYAML(writeRequiresApproval bool) string {
	approvalValue := "false"
	if writeRequiresApproval {
		approvalValue = "true"
	}

	return "version: 0.1.0\n\n" +
		"tools:\n" +
		"  filesystem:\n" +
		"    allowed_roots:\n" +
		"      - \".\"\n" +
		"    denied_paths: []\n" +
		"    read_enabled: true\n" +
		"    write_enabled: true\n" +
		"    write_requires_approval: " + approvalValue + "\n" +
		"  http:\n" +
		"    enabled: false\n" +
		"    allowed_domains: []\n" +
		"    requires_approval: true\n" +
		"    timeout_seconds: 10\n" +
		"  shell:\n" +
		"    enabled: false\n" +
		"    allowed_commands: []\n" +
		"    requires_approval: true\n" +
		"logging:\n" +
		"  log_commands: true\n" +
		"  log_tool_calls: true\n" +
		"safety:\n" +
		"  allow_persona_modification: false\n" +
		"  allow_policy_modification: false\n"
}

func loopgateHTTPPolicyYAML(requiresApproval bool) string {
	approvalValue := "false"
	if requiresApproval {
		approvalValue = "true"
	}

	return "version: 0.1.0\n\n" +
		"tools:\n" +
		"  filesystem:\n" +
		"    allowed_roots:\n" +
		"      - \".\"\n" +
		"    denied_paths: []\n" +
		"    read_enabled: true\n" +
		"    write_enabled: true\n" +
		"    write_requires_approval: false\n" +
		"  http:\n" +
		"    enabled: true\n" +
		"    allowed_domains: []\n" +
		"    requires_approval: " + approvalValue + "\n" +
		"    timeout_seconds: 10\n" +
		"  shell:\n" +
		"    enabled: false\n" +
		"    allowed_commands: []\n" +
		"    requires_approval: true\n" +
		"logging:\n" +
		"  log_commands: true\n" +
		"  log_tool_calls: true\n" +
		"safety:\n" +
		"  allow_persona_modification: false\n" +
		"  allow_policy_modification: false\n"
}

func loopgateShellPolicyYAML(requiresApproval bool, allowedCommands []string) string {
	approvalValue := "false"
	if requiresApproval {
		approvalValue = "true"
	}
	quotedCommands := make([]string, 0, len(allowedCommands))
	for _, allowedCommand := range allowedCommands {
		quotedCommands = append(quotedCommands, fmt.Sprintf("      - %q\n", allowedCommand))
	}

	return "version: 0.1.0\n\n" +
		"tools:\n" +
		"  filesystem:\n" +
		"    allowed_roots:\n" +
		"      - \".\"\n" +
		"    denied_paths: []\n" +
		"    read_enabled: true\n" +
		"    write_enabled: true\n" +
		"    write_requires_approval: false\n" +
		"  http:\n" +
		"    enabled: false\n" +
		"    allowed_domains: []\n" +
		"    requires_approval: true\n" +
		"    timeout_seconds: 10\n" +
		"  shell:\n" +
		"    enabled: true\n" +
		"    allowed_commands:\n" +
		strings.Join(quotedCommands, "") +
		"    requires_approval: " + approvalValue + "\n" +
		"logging:\n" +
		"  log_commands: true\n" +
		"  log_tool_calls: true\n" +
		"safety:\n" +
		"  allow_persona_modification: false\n" +
		"  allow_policy_modification: false\n"
}

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

func readUIReplayEvents(t *testing.T, client *Client, lastEventID string) []controlapipkg.UIEventEnvelope {
	t.Helper()

	capabilityToken, err := client.ensureCapabilityToken(context.Background())
	if err != nil {
		t.Fatalf("ensure capability token for ui events: %v", err)
	}

	requestContext, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, client.baseURL+"/v1/ui/events", nil)
	if err != nil {
		t.Fatalf("build ui events request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+capabilityToken)
	if lastEventID != "" {
		request.Header.Set("Last-Event-ID", lastEventID)
	}
	if err := client.attachRequestSignature(request, "/v1/ui/events", nil); err != nil {
		t.Fatalf("attach ui events signature: %v", err)
	}

	httpResponse, err := client.httpClient.Do(request)
	if err != nil {
		t.Fatalf("do ui events request: %v", err)
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode != http.StatusOK {
		t.Fatalf("unexpected ui events status: %d", httpResponse.StatusCode)
	}

	reader := bufio.NewReader(httpResponse.Body)
	events := make([]controlapipkg.UIEventEnvelope, 0, 8)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var uiEvent controlapipkg.UIEventEnvelope
		if err := json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(line), "data: ")), &uiEvent); err != nil {
			t.Fatalf("decode ui event: %v", err)
		}
		events = append(events, uiEvent)
	}
	return events
}

func readUIRecentEvents(t *testing.T, client *Client, lastEventID string) []controlapipkg.UIEventEnvelope {
	t.Helper()

	capabilityToken, err := client.ensureCapabilityToken(context.Background())
	if err != nil {
		t.Fatalf("ensure capability token for recent ui events: %v", err)
	}

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, client.baseURL+"/v1/ui/events/recent", nil)
	if err != nil {
		t.Fatalf("build recent ui events request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+capabilityToken)
	if lastEventID != "" {
		request.Header.Set("Last-Event-ID", lastEventID)
	}
	if err := client.attachRequestSignature(request, "/v1/ui/events/recent", nil); err != nil {
		t.Fatalf("attach recent ui events signature: %v", err)
	}

	httpResponse, err := client.httpClient.Do(request)
	if err != nil {
		t.Fatalf("do recent ui events request: %v", err)
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode != http.StatusOK {
		t.Fatalf("unexpected recent ui events status: %d", httpResponse.StatusCode)
	}

	var response controlapipkg.UIRecentEventsResponse
	if err := json.NewDecoder(httpResponse.Body).Decode(&response); err != nil {
		t.Fatalf("decode recent ui events response: %v", err)
	}
	return response.Events
}

func containsUIEventType(events []controlapipkg.UIEventEnvelope, expectedType string) bool {
	for _, uiEvent := range events {
		if uiEvent.Type == expectedType {
			return true
		}
	}
	return false
}

func containsUICapabilityEvent(events []controlapipkg.UIEventEnvelope, capability string) bool {
	for _, uiEvent := range events {
		encodedEvent, err := json.Marshal(uiEvent)
		if err != nil {
			continue
		}
		if strings.Contains(string(encodedEvent), fmt.Sprintf("\"capability\":\"%s\"", capability)) {
			return true
		}
	}
	return false
}
