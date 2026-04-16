package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultPolicySigningTrustDirForConfigDir(t *testing.T) {
	trustDir := defaultPolicySigningTrustDirForConfigDir("/Users/testuser/Library/Application Support")
	expectedDir := "/Users/testuser/Library/Application Support/Loopgate/policy-signing/trusted"
	if trustDir != expectedDir {
		t.Fatalf("expected trust dir %q, got %q", expectedDir, trustDir)
	}
}

func TestTrustedPolicySigningPublicKey_LoadsOperatorTrustAnchorFromDir(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate policy signing keypair: %v", err)
	}
	trustDir := t.TempDir()
	t.Setenv(policySigningTrustDirEnv, trustDir)

	if err := writePEMEncodedEd25519PublicKey(filepath.Join(trustDir, "loopgate-local-root.pub.pem"), publicKey); err != nil {
		t.Fatalf("write trust anchor: %v", err)
	}

	trustedPublicKey, err := TrustedPolicySigningPublicKey("loopgate-local-root")
	if err != nil {
		t.Fatalf("trusted policy signing public key: %v", err)
	}
	if !publicKeysEqual(trustedPublicKey, publicKey) {
		t.Fatal("trusted public key does not match operator trust anchor")
	}
}

func TestTrustedPolicySigningPublicKey_RejectsOperatorTrustAnchorConflictWithBuiltin(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate conflicting policy signing keypair: %v", err)
	}
	trustDir := t.TempDir()
	t.Setenv(policySigningTrustDirEnv, trustDir)

	conflictingPath := filepath.Join(trustDir, PolicySigningTrustAnchorKeyID+policySigningPublicKeySuffix)
	if err := writePEMEncodedEd25519PublicKey(conflictingPath, publicKey); err != nil {
		t.Fatalf("write conflicting trust anchor: %v", err)
	}

	_, err = TrustedPolicySigningPublicKey(PolicySigningTrustAnchorKeyID)
	if err == nil || !strings.Contains(err.Error(), "conflicts with an already trusted public key") {
		t.Fatalf("expected conflicting trust anchor error, got %v", err)
	}
}

func TestLoadPolicy_AcceptsOperatorTrustAnchorWithoutTestBypass(t *testing.T) {
	repoRoot := t.TempDir()
	trustDir := t.TempDir()
	t.Setenv(policySigningTrustDirEnv, trustDir)

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate operator policy signing keypair: %v", err)
	}

	if err := writePEMEncodedEd25519PublicKey(filepath.Join(trustDir, "loopgate-local-root.pub.pem"), publicKey); err != nil {
		t.Fatalf("write trust anchor: %v", err)
	}

	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir policy dir: %v", err)
	}
	rawPolicyBytes := []byte("version: 0.1.0\n")
	if err := os.WriteFile(policyPath, rawPolicyBytes, 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	signatureFile, err := SignPolicyDocument(rawPolicyBytes, "loopgate-local-root", privateKey)
	if err != nil {
		t.Fatalf("sign policy document: %v", err)
	}
	if err := WritePolicySignatureYAML(repoRoot, signatureFile); err != nil {
		t.Fatalf("write policy signature: %v", err)
	}

	if _, err := LoadPolicy(repoRoot); err != nil {
		t.Fatalf("load policy with operator trust anchor: %v", err)
	}
}

func writePEMEncodedEd25519PublicKey(path string, publicKey ed25519.PublicKey) error {
	publicKeyDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return err
	}
	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyDER,
	})
	return os.WriteFile(path, publicKeyPEM, 0o644)
}
