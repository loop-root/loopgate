package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loopgate/internal/config"
	"loopgate/internal/testutil"
)

func TestRunPolicySignCLI_SignsPolicyFromWorkingDirectory(t *testing.T) {
	testSigner, err := testutil.NewPolicyTestSigner()
	if err != nil {
		t.Fatalf("new policy test signer: %v", err)
	}
	testSigner.ConfigureEnv(t.Setenv)

	repoRoot := t.TempDir()
	rawPolicyBytes := []byte("version: \"1\"\n")
	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir policy dir: %v", err)
	}
	if err := os.WriteFile(policyPath, rawPolicyBytes, 0o600); err != nil {
		t.Fatalf("write policy yaml: %v", err)
	}

	privateKeyPath := filepath.Join(t.TempDir(), testSigner.KeyID+".pem")
	writePEMEncodedEd25519PrivateKey(t, privateKeyPath, testSigner.PrivateKey, 0o600)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runPolicySignCLI(
		[]string{
			"-private-key-file", privateKeyPath,
			"-key-id", testSigner.KeyID,
			"-policy-file", filepath.Join("core", "policy", "policy.yaml"),
		},
		&stdout,
		&stderr,
		func() (string, error) { return repoRoot, nil },
		os.Getenv,
	)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d with stderr %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}

	signaturePath := filepath.Join(repoRoot, "core", "policy", "policy.yaml.sig")
	if !strings.Contains(stdout.String(), "Wrote "+signaturePath) {
		t.Fatalf("expected stdout to mention %q, got %q", signaturePath, stdout.String())
	}

	signatureFile, err := config.LoadPolicySignatureFile(repoRoot)
	if err != nil {
		t.Fatalf("load policy signature: %v", err)
	}
	if err := config.VerifyPolicyDocumentSignature(rawPolicyBytes, signatureFile); err != nil {
		t.Fatalf("verify signed policy: %v", err)
	}
}

func TestRunPolicySignCLI_VerifySetup_PrintsSummary(t *testing.T) {
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
	t.Setenv(policySigningPrivateKeyFileEnv, privateKeyPath)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runPolicySignCLI(
		[]string{
			"-repo-root", repoRoot,
			"-verify-setup",
			"-key-id", testSigner.KeyID,
		},
		&stdout,
		&stderr,
		func() (string, error) { return "", nil },
		os.Getenv,
	)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d with stderr %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}

	stdoutText := stdout.String()
	for _, expectedText := range []string{
		"Policy signing setup OK",
		"key_id: " + testSigner.KeyID,
		"policy_signature_key_id: " + testSigner.KeyID,
		"signer_key_source: " + policySigningPrivateKeyFileEnv,
		"signer_key_permissions: 0600",
	} {
		if !strings.Contains(stdoutText, expectedText) {
			t.Fatalf("expected stdout to contain %q, got %q", expectedText, stdoutText)
		}
	}
}

func TestRunPolicySignCLI_VerifySetup_DefaultsToCurrentSignedPolicyKeyID(t *testing.T) {
	testSigner, err := testutil.NewPolicyTestSigner()
	if err != nil {
		t.Fatalf("new policy test signer: %v", err)
	}
	testSigner.ConfigureEnv(t.Setenv)

	repoRoot := t.TempDir()
	if err := testSigner.WriteSignedPolicyYAML(repoRoot, "version: \"1\"\n"); err != nil {
		t.Fatalf("write signed policy yaml: %v", err)
	}

	t.Setenv(policySigningPrivateKeyFileEnv, "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	defaultKeyPath, err := defaultPolicySigningPrivateKeyPath(testSigner.KeyID)
	if err != nil {
		t.Fatalf("default private key path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(defaultKeyPath), 0o755); err != nil {
		t.Fatalf("mkdir default key dir: %v", err)
	}
	writePEMEncodedEd25519PrivateKey(t, defaultKeyPath, testSigner.PrivateKey, 0o600)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runPolicySignCLI(
		[]string{
			"-repo-root", repoRoot,
			"-verify-setup",
		},
		&stdout,
		&stderr,
		func() (string, error) { return "", nil },
		os.Getenv,
	)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d with stderr %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "key_id: "+testSigner.KeyID) {
		t.Fatalf("expected inferred key_id output, got %q", stdout.String())
	}
}

func TestRunPolicySignCLI_MissingDefaultPrivateKeyPrintsGuidance(t *testing.T) {
	repoRoot := t.TempDir()
	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir policy dir: %v", err)
	}
	if err := os.WriteFile(policyPath, []byte("version: \"1\"\n"), 0o600); err != nil {
		t.Fatalf("write policy yaml: %v", err)
	}

	t.Setenv(policySigningPrivateKeyFileEnv, "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	defaultKeyPath, err := defaultPolicySigningPrivateKeyPath(config.PolicySigningTrustAnchorKeyID)
	if err != nil {
		t.Fatalf("default private key path: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runPolicySignCLI(
		[]string{"-repo-root", repoRoot},
		&stdout,
		&stderr,
		func() (string, error) { return "", nil },
		os.Getenv,
	)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d with stderr %q", exitCode, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %q", stdout.String())
	}

	stderrText := stderr.String()
	if !strings.Contains(stderrText, "ERROR: read private key:") {
		t.Fatalf("expected read private key error, got %q", stderrText)
	}
	if !strings.Contains(stderrText, defaultKeyPath) {
		t.Fatalf("expected stderr to mention default key path %q, got %q", defaultKeyPath, stderrText)
	}
	if !strings.Contains(stderrText, "Create or move the signer key to") {
		t.Fatalf("expected default-path guidance, got %q", stderrText)
	}
	if !strings.Contains(stderrText, policySigningPrivateKeyFileEnv) {
		t.Fatalf("expected stderr to mention %s, got %q", policySigningPrivateKeyFileEnv, stderrText)
	}
}

func TestRunPolicySignCLI_InvalidFlagReturnsUsageError(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runPolicySignCLI(
		[]string{"-definitely-invalid"},
		&stdout,
		&stderr,
		func() (string, error) { return "", nil },
		os.Getenv,
	)
	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d with stderr %q", exitCode, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined") {
		t.Fatalf("expected flag parse error, got %q", stderr.String())
	}
}

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

func TestVerifyPolicySigningSetup_SucceedsWithOperatorTrustAnchor(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate operator signer: %v", err)
	}

	trustDir := t.TempDir()
	t.Setenv("LOOPGATE_POLICY_SIGNING_TRUST_DIR", trustDir)
	if err := writePEMEncodedEd25519PublicKey(filepath.Join(trustDir, "loopgate-local-root.pub.pem"), publicKey); err != nil {
		t.Fatalf("write trust anchor: %v", err)
	}

	repoRoot := t.TempDir()
	rawPolicyBytes := []byte("version: \"1\"\n")
	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir policy dir: %v", err)
	}
	if err := os.WriteFile(policyPath, rawPolicyBytes, 0o600); err != nil {
		t.Fatalf("write policy yaml: %v", err)
	}
	signatureFile, err := config.SignPolicyDocument(rawPolicyBytes, "loopgate-local-root", privateKey)
	if err != nil {
		t.Fatalf("sign policy yaml: %v", err)
	}
	if err := config.WritePolicySignatureYAML(repoRoot, signatureFile); err != nil {
		t.Fatalf("write policy signature yaml: %v", err)
	}

	privateKeyPath := filepath.Join(t.TempDir(), "loopgate-local-root.pem")
	writePEMEncodedEd25519PrivateKey(t, privateKeyPath, privateKey, 0o600)

	verificationResult, err := verifyPolicySigningSetup(repoRoot, privateKeyPath, "-private-key-file", "loopgate-local-root")
	if err != nil {
		t.Fatalf("verify policy signing setup: %v", err)
	}
	if verificationResult.SignatureKeyID != "loopgate-local-root" {
		t.Fatalf("expected signature key_id %q, got %q", "loopgate-local-root", verificationResult.SignatureKeyID)
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
