package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
	"loopgate/internal/loopgate"
)

type testPolicySignerFixture struct {
	publicKey  ed25519.PublicKey
	privateKey ed25519.PrivateKey
}

func mustPolicyPresetTemplate(t *testing.T, presetName string) string {
	t.Helper()
	preset, err := config.ResolvePolicyTemplatePreset(presetName)
	if err != nil {
		t.Fatalf("resolve policy preset %q: %v", presetName, err)
	}
	return preset.TemplateYAML
}

func (fixture testPolicySignerFixture) keyID() string {
	return "loopgate-test-policy-root"
}

func writeSignedPolicyFixture(t *testing.T, repoRoot string, rawPolicy string) {
	t.Helper()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate policy signing key: %v", err)
	}
	t.Setenv("LOOPGATE_TEST_POLICY_SIGNING_KEY_ID", "loopgate-test-policy-root")
	t.Setenv("LOOPGATE_TEST_POLICY_SIGNING_PUBLIC_KEY", base64.StdEncoding.EncodeToString(publicKey))

	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir policy dir: %v", err)
	}
	rawPolicyBytes := []byte(rawPolicy)
	if err := os.WriteFile(policyPath, rawPolicyBytes, 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	signatureFile, err := config.SignPolicyDocument(rawPolicyBytes, "loopgate-test-policy-root", privateKey)
	if err != nil {
		t.Fatalf("sign policy: %v", err)
	}
	if err := config.WritePolicySignatureYAML(repoRoot, signatureFile); err != nil {
		t.Fatalf("write signature: %v", err)
	}
}

func newTestPolicySignerFixture(t *testing.T) testPolicySignerFixture {
	t.Helper()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate policy signing key: %v", err)
	}
	return testPolicySignerFixture{
		publicKey:  publicKey,
		privateKey: privateKey,
	}
}

func (fixture testPolicySignerFixture) writeSignedPolicy(t *testing.T, repoRoot string, rawPolicy string) {
	t.Helper()

	t.Setenv("LOOPGATE_TEST_POLICY_SIGNING_KEY_ID", fixture.keyID())
	t.Setenv("LOOPGATE_TEST_POLICY_SIGNING_PUBLIC_KEY", base64.StdEncoding.EncodeToString(fixture.publicKey))

	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir policy dir: %v", err)
	}
	rawPolicyBytes := []byte(rawPolicy)
	if err := os.WriteFile(policyPath, rawPolicyBytes, 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	signatureFile, err := config.SignPolicyDocument(rawPolicyBytes, fixture.keyID(), fixture.privateKey)
	if err != nil {
		t.Fatalf("sign policy: %v", err)
	}
	if err := config.WritePolicySignatureYAML(repoRoot, signatureFile); err != nil {
		t.Fatalf("write signature: %v", err)
	}
}

func (fixture testPolicySignerFixture) writeSignedOperatorOverrideDocument(t *testing.T, repoRoot string, document config.OperatorOverrideDocument) {
	t.Helper()

	documentBytes, err := config.MarshalOperatorOverrideDocumentYAML(document)
	if err != nil {
		t.Fatalf("marshal operator override document: %v", err)
	}
	signatureFile, err := config.SignOperatorOverrideDocument(documentBytes, fixture.keyID(), fixture.privateKey)
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

func delegatedRepoEditPolicyYAML() string {
	return "version: 0.1.0\n\n" +
		"tools:\n" +
		"  claude_code:\n" +
		"    tool_policies:\n" +
		"      Edit:\n" +
		"        enabled: true\n" +
		"        requires_approval: true\n" +
		"        allowed_roots:\n" +
		"          - \"docs\"\n" +
		"      MultiEdit:\n" +
		"        enabled: true\n" +
		"        requires_approval: true\n" +
		"        allowed_roots:\n" +
		"          - \"docs\"\n" +
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
		"    enabled: false\n" +
		"    allowed_commands: []\n" +
		"    requires_approval: true\n" +
		"operator_overrides:\n" +
		"  classes:\n" +
		"    repo_edit_safe:\n" +
		"      max_delegation: persistent\n" +
		"logging:\n" +
		"  log_commands: true\n" +
		"  log_tool_calls: true\n" +
		"safety:\n" +
		"  allow_persona_modification: false\n" +
		"  allow_policy_modification: false\n"
}
func startPolicyAdminTestServer(t *testing.T, repoRoot string, socketPath string) string {
	t.Helper()

	server, err := loopgate.NewServerWithOptions(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("NewServerWithOptions: %v", err)
	}
	t.Cleanup(server.CloseDiagnosticLogs)

	serveContext, cancelServe := context.WithCancel(context.Background())
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- server.Serve(serveContext)
	}()
	t.Cleanup(func() {
		cancelServe()
		select {
		case serveErr := <-serveErrors:
			if serveErr != nil {
				t.Fatalf("serve loopgate: %v", serveErr)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out stopping loopgate test server")
		}
	})

	client := loopgate.NewClient(socketPath)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := client.Health(context.Background()); err == nil {
			return filepath.Join(repoRoot, "runtime", "sandbox", "root", "home")
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("timed out waiting for loopgate test server health")
	return ""
}

func newTempSocketPath(t *testing.T) string {
	t.Helper()

	socketFile, err := os.CreateTemp("", "loopgate-policy-admin-*.sock")
	if err != nil {
		t.Fatalf("create temp socket file: %v", err)
	}
	socketPath := socketFile.Name()
	_ = socketFile.Close()
	_ = os.Remove(socketPath)
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	return socketPath
}

func writePEMEncodedEd25519PrivateKey(t *testing.T, path string, privateKey ed25519.PrivateKey, permissions os.FileMode) {
	t.Helper()

	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privateKeyDER,
	})
	if err := os.WriteFile(path, privateKeyPEM, permissions); err != nil {
		t.Fatalf("write private key: %v", err)
	}
}

func readLastAuditEventOfType(t *testing.T, repoRoot string, eventType string) ledger.Event {
	t.Helper()

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(auditBytes)), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimSpace(lines[index])
		if line == "" {
			continue
		}
		var auditEvent ledger.Event
		if err := json.Unmarshal([]byte(line), &auditEvent); err != nil {
			t.Fatalf("decode audit event: %v", err)
		}
		if auditEvent.Type == eventType {
			return auditEvent
		}
	}

	t.Fatalf("expected audit event type %q", eventType)
	return ledger.Event{}
}
