package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPolicyWithHash_ReturnsConsistentHash(t *testing.T) {
	repoRoot := t.TempDir()
	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeSignedPolicyForConfigTest(t, repoRoot, testPolicyYAML())

	result1, err := LoadPolicyWithHash(repoRoot)
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	result2, err := LoadPolicyWithHash(repoRoot)
	if err != nil {
		t.Fatalf("load policy again: %v", err)
	}
	if result1.ContentSHA256 != result2.ContentSHA256 {
		t.Fatalf("expected consistent hash, got %q and %q", result1.ContentSHA256, result2.ContentSHA256)
	}
	if result1.ContentSHA256 == "" {
		t.Fatal("hash should not be empty")
	}
}

func TestLoadPolicy_RejectsMissingDetachedSignature(t *testing.T) {
	repoRoot := t.TempDir()
	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(policyPath, []byte(testPolicyYAML()), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	if _, err := LoadPolicy(repoRoot); err == nil || !strings.Contains(err.Error(), "policy signature") {
		t.Fatalf("expected unsigned policy to fail closed, got %v", err)
	}
}

func TestLoadPolicy_RejectsTamperedSignature(t *testing.T) {
	repoRoot := t.TempDir()
	writeSignedPolicyForConfigTest(t, repoRoot, testPolicyYAML())

	signaturePath := filepath.Join(repoRoot, "core", "policy", "policy.yaml.sig")
	rawSignatureBytes, err := os.ReadFile(signaturePath)
	if err != nil {
		t.Fatalf("read signature: %v", err)
	}
	rawSignatureBytes[len(rawSignatureBytes)-2] ^= 0x01
	if err := os.WriteFile(signaturePath, rawSignatureBytes, 0o600); err != nil {
		t.Fatalf("rewrite tampered signature: %v", err)
	}

	if _, err := LoadPolicy(repoRoot); err == nil || (!strings.Contains(err.Error(), "verification failed") && !strings.Contains(err.Error(), "decode policy signature")) {
		t.Fatalf("expected tampered policy signature to fail, got %v", err)
	}
}
