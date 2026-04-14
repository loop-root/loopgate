package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loopgate/internal/testutil"
)

func TestResolvePolicySigningPrivateKeyPath_PrefersFlag(t *testing.T) {
	resolvedPath, source, err := resolvePolicySigningPrivateKeyPath(" /tmp/operator-key.pem ", "/tmp/env-key.pem", "loopgate-policy-root-2026-04")
	if err != nil {
		t.Fatalf("resolve private key path: %v", err)
	}
	if resolvedPath != "/tmp/operator-key.pem" {
		t.Fatalf("expected flag path, got %q", resolvedPath)
	}
	if source != "-private-key-file" {
		t.Fatalf("expected flag source, got %q", source)
	}
}

func TestResolvePolicySigningPrivateKeyPath_PrefersEnvOverDefault(t *testing.T) {
	resolvedPath, source, err := resolvePolicySigningPrivateKeyPath("", " /tmp/env-key.pem ", "loopgate-policy-root-2026-04")
	if err != nil {
		t.Fatalf("resolve private key path: %v", err)
	}
	if resolvedPath != "/tmp/env-key.pem" {
		t.Fatalf("expected env path, got %q", resolvedPath)
	}
	if source != policySigningPrivateKeyFileEnv {
		t.Fatalf("expected env source, got %q", source)
	}
}

func TestDefaultPolicySigningPrivateKeyPathForConfigDir(t *testing.T) {
	resolvedPath := defaultPolicySigningPrivateKeyPathForConfigDir("/Users/testuser/Library/Application Support", "loopgate-policy-root-2026-04")
	expectedPath := "/Users/testuser/Library/Application Support/Loopgate/policy-signing/loopgate-policy-root-2026-04.pem"
	if resolvedPath != expectedPath {
		t.Fatalf("expected %q, got %q", expectedPath, resolvedPath)
	}
}

func TestVerifyPolicySigningSetup_SucceedsWhenTrustAndSignerMatch(t *testing.T) {
	testSigner, err := testutil.NewPolicyTestSigner()
	if err != nil {
		t.Fatalf("new policy test signer: %v", err)
	}
	testSigner.ConfigureEnv(t.Setenv)

	repoRoot := t.TempDir()
	if err := testSigner.WriteSignedPolicyYAML(repoRoot, "version: \"1\"\n"); err != nil {
		t.Fatalf("write signed policy yaml: %v", err)
	}

	privateKeyPath := filepath.Join(t.TempDir(), testSigner.KeyID+".pem")
	writePEMEncodedEd25519PrivateKey(t, privateKeyPath, testSigner.PrivateKey, 0o600)

	verificationResult, err := verifyPolicySigningSetup(repoRoot, privateKeyPath, "-private-key-file", testSigner.KeyID)
	if err != nil {
		t.Fatalf("verify policy signing setup: %v", err)
	}
	if verificationResult.SignatureKeyID != testSigner.KeyID {
		t.Fatalf("expected signature key_id %q, got %q", testSigner.KeyID, verificationResult.SignatureKeyID)
	}
	if verificationResult.SignerKeyPermissions != 0o600 {
		t.Fatalf("expected signer key permissions 0600, got %04o", verificationResult.SignerKeyPermissions)
	}
}

func TestVerifyPolicySigningSetup_DetectsMismatchedSignerKey(t *testing.T) {
	trustedSigner, err := testutil.NewPolicyTestSigner()
	if err != nil {
		t.Fatalf("new trusted signer: %v", err)
	}
	trustedSigner.ConfigureEnv(t.Setenv)

	repoRoot := t.TempDir()
	if err := trustedSigner.WriteSignedPolicyYAML(repoRoot, "version: \"1\"\n"); err != nil {
		t.Fatalf("write signed policy yaml: %v", err)
	}

	_, mismatchedPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate mismatched private key: %v", err)
	}
	privateKeyPath := filepath.Join(t.TempDir(), "mismatched.pem")
	writePEMEncodedEd25519PrivateKey(t, privateKeyPath, mismatchedPrivateKey, 0o600)

	_, err = verifyPolicySigningSetup(repoRoot, privateKeyPath, "-private-key-file", trustedSigner.KeyID)
	if err == nil || !strings.Contains(err.Error(), "does not match trusted public key") {
		t.Fatalf("expected mismatched signer key error, got %v", err)
	}
}

func TestVerifyPolicySigningSetup_DetectsBroadPermissions(t *testing.T) {
	testSigner, err := testutil.NewPolicyTestSigner()
	if err != nil {
		t.Fatalf("new policy test signer: %v", err)
	}
	testSigner.ConfigureEnv(t.Setenv)

	repoRoot := t.TempDir()
	if err := testSigner.WriteSignedPolicyYAML(repoRoot, "version: \"1\"\n"); err != nil {
		t.Fatalf("write signed policy yaml: %v", err)
	}

	privateKeyPath := filepath.Join(t.TempDir(), testSigner.KeyID+".pem")
	writePEMEncodedEd25519PrivateKey(t, privateKeyPath, testSigner.PrivateKey, 0o644)

	_, err = verifyPolicySigningSetup(repoRoot, privateKeyPath, "-private-key-file", testSigner.KeyID)
	if err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("expected broad permissions error, got %v", err)
	}
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
