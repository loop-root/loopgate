package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func writeSignedPolicyForConfigTest(t *testing.T, repoRoot string, rawPolicy string) {
	t.Helper()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("new test policy signer: %v", err)
	}
	t.Setenv(testPolicySigningKeyIDEnv, defaultTestPolicySigningKeyID)
	t.Setenv(testPolicySigningPublicKeyEnv, base64.StdEncoding.EncodeToString(publicKey))

	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir policy dir: %v", err)
	}
	rawPolicyBytes := []byte(rawPolicy)
	if err := os.WriteFile(policyPath, rawPolicyBytes, 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	signatureFile, err := SignPolicyDocument(rawPolicyBytes, defaultTestPolicySigningKeyID, privateKey)
	if err != nil {
		t.Fatalf("sign policy: %v", err)
	}
	if err := WritePolicySignatureYAML(repoRoot, signatureFile); err != nil {
		t.Fatalf("write policy signature: %v", err)
	}
}

func testPolicyYAML() string {
	return `version: 0.1.0
tools:
  filesystem:
    allowed_roots: ["."]
    denied_paths: []
    read_enabled: true
    write_enabled: false
    write_requires_approval: true
  http:
    enabled: false
    allowed_domains: []
    requires_approval: true
    timeout_seconds: 10
  shell:
    enabled: false
    allowed_commands: []
    requires_approval: true
logging:
  log_commands: true
  log_tool_calls: true
safety:
  allow_persona_modification: false
  allow_policy_modification: false
`
}
