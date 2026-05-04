package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loopgate/internal/loopgate"
)

func TestRunApply_HotReloadsSignedPolicy(t *testing.T) {
	repoRoot := t.TempDir()
	signerFixture := newTestPolicySignerFixture(t)
	signerFixture.writeSignedPolicy(t, repoRoot, mustPolicyPresetTemplate(t, "strict"))
	initialPolicy, err := loadPolicyDocument(repoRoot, "", "")
	if err != nil {
		t.Fatalf("load initial policy: %v", err)
	}

	socketPath := newTempSocketPath(t)
	_ = startPolicyAdminTestServer(t, repoRoot, socketPath)

	signerFixture.writeSignedPolicy(t, repoRoot, mustPolicyPresetTemplate(t, "developer"))
	reloadedPolicy, err := loadPolicyDocument(repoRoot, "", "")
	if err != nil {
		t.Fatalf("load reloaded policy: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"apply", "-repo", repoRoot, "-socket", socketPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}

	output := stdout.String()
	if !strings.Contains(output, "policy hot-apply OK") {
		t.Fatalf("expected hot-apply success output, got %q", output)
	}
	if !strings.Contains(output, "normalized_running_policy_diff:") {
		t.Fatalf("expected running policy diff output, got %q", output)
	}
	if !strings.Contains(output, "tools.http.enabled: false => true") {
		t.Fatalf("expected http diff in output, got %q", output)
	}
	if !strings.Contains(output, "previous_policy_sha256: "+initialPolicy.ContentSHA256) {
		t.Fatalf("expected previous hash in output, got %q", output)
	}
	if !strings.Contains(output, "policy_sha256: "+reloadedPolicy.ContentSHA256) {
		t.Fatalf("expected reloaded hash in output, got %q", output)
	}
	if !strings.Contains(output, "policy_changed: true") {
		t.Fatalf("expected policy_changed output, got %q", output)
	}

	time.Sleep(600 * time.Millisecond)
	statusClient := loopgate.NewClient(socketPath)
	statusClient.ConfigureSession("policy-admin-status", "policy-admin-status", []string{"fs_list"})
	statusResponse, err := statusClient.Status(context.Background())
	if err != nil {
		t.Fatalf("status after apply: %v", err)
	}
	if !statusResponse.Policy.Tools.HTTP.Enabled {
		t.Fatalf("expected reloaded developer policy, got %#v", statusResponse.Policy.Tools.HTTP)
	}
}

func TestRunApply_WithVerifySetup_HotReloadsSignedPolicy(t *testing.T) {
	repoRoot := t.TempDir()
	signerFixture := newTestPolicySignerFixture(t)
	signerFixture.writeSignedPolicy(t, repoRoot, mustPolicyPresetTemplate(t, "strict"))
	initialPolicy, err := loadPolicyDocument(repoRoot, "", "")
	if err != nil {
		t.Fatalf("load initial policy: %v", err)
	}

	privateKeyPath := filepath.Join(t.TempDir(), signerFixture.keyID()+".pem")
	writePEMEncodedEd25519PrivateKey(t, privateKeyPath, signerFixture.privateKey, 0o600)

	socketPath := newTempSocketPath(t)
	_ = startPolicyAdminTestServer(t, repoRoot, socketPath)

	signerFixture.writeSignedPolicy(t, repoRoot, mustPolicyPresetTemplate(t, "developer"))
	reloadedPolicy, err := loadPolicyDocument(repoRoot, "", "")
	if err != nil {
		t.Fatalf("load reloaded policy: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"apply", "-repo", repoRoot, "-socket", socketPath, "-verify-setup", "-private-key-file", privateKeyPath, "-key-id", signerFixture.keyID()}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}

	output := stdout.String()
	if !strings.Contains(output, "policy signing setup OK") {
		t.Fatalf("expected setup verification output, got %q", output)
	}
	if !strings.Contains(output, "policy hot-apply OK") {
		t.Fatalf("expected hot-apply output, got %q", output)
	}
	if !strings.Contains(output, "normalized_running_policy_diff:") {
		t.Fatalf("expected running policy diff output, got %q", output)
	}
	if !strings.Contains(output, "tools.http.enabled: false => true") {
		t.Fatalf("expected http diff in output, got %q", output)
	}
	if !strings.Contains(output, "previous_policy_sha256: "+initialPolicy.ContentSHA256) {
		t.Fatalf("expected previous hash in output, got %q", output)
	}
	if !strings.Contains(output, "policy_sha256: "+reloadedPolicy.ContentSHA256) {
		t.Fatalf("expected reloaded hash in output, got %q", output)
	}
}

func TestRunApply_WithVerifySetup_DefaultsToCurrentSignedPolicyKeyID(t *testing.T) {
	repoRoot := t.TempDir()
	signerFixture := newTestPolicySignerFixture(t)
	signerFixture.writeSignedPolicy(t, repoRoot, mustPolicyPresetTemplate(t, "strict"))
	initialPolicy, err := loadPolicyDocument(repoRoot, "", "")
	if err != nil {
		t.Fatalf("load initial policy: %v", err)
	}

	t.Setenv(policySigningPrivateKeyFileEnv, "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	defaultKeyPath, err := defaultPolicySigningPrivateKeyPath(signerFixture.keyID())
	if err != nil {
		t.Fatalf("default private key path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(defaultKeyPath), 0o755); err != nil {
		t.Fatalf("mkdir default key dir: %v", err)
	}
	writePEMEncodedEd25519PrivateKey(t, defaultKeyPath, signerFixture.privateKey, 0o600)

	socketPath := newTempSocketPath(t)
	_ = startPolicyAdminTestServer(t, repoRoot, socketPath)

	signerFixture.writeSignedPolicy(t, repoRoot, mustPolicyPresetTemplate(t, "developer"))
	reloadedPolicy, err := loadPolicyDocument(repoRoot, "", "")
	if err != nil {
		t.Fatalf("load reloaded policy: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"apply", "-repo", repoRoot, "-socket", socketPath, "-verify-setup"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}

	output := stdout.String()
	if !strings.Contains(output, "policy signing setup OK") {
		t.Fatalf("expected setup verification output, got %q", output)
	}
	if !strings.Contains(output, "key_id: "+signerFixture.keyID()) {
		t.Fatalf("expected inferred key_id output, got %q", output)
	}
	if !strings.Contains(output, "previous_policy_sha256: "+initialPolicy.ContentSHA256) {
		t.Fatalf("expected previous hash in output, got %q", output)
	}
	if !strings.Contains(output, "policy_sha256: "+reloadedPolicy.ContentSHA256) {
		t.Fatalf("expected reloaded hash in output, got %q", output)
	}
}

func TestRunApply_FailsWhenServerReloadsDifferentPolicy(t *testing.T) {
	repoRoot := t.TempDir()
	signerFixture := newTestPolicySignerFixture(t)
	signerFixture.writeSignedPolicy(t, repoRoot, mustPolicyPresetTemplate(t, "strict"))
	serverRepoRoot := t.TempDir()
	signerFixture.writeSignedPolicy(t, serverRepoRoot, mustPolicyPresetTemplate(t, "strict"))
	socketPath := newTempSocketPath(t)
	_ = startPolicyAdminTestServer(t, serverRepoRoot, socketPath)

	signerFixture.writeSignedPolicy(t, repoRoot, mustPolicyPresetTemplate(t, "developer"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"apply", "-repo", repoRoot, "-socket", socketPath}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "reloaded policy sha mismatch") {
		t.Fatalf("expected sha mismatch error, got %q", stderr.String())
	}
}

func TestRunApply_WithVerifySetup_RejectsMismatchedSigner(t *testing.T) {
	repoRoot := t.TempDir()
	signerFixture := newTestPolicySignerFixture(t)
	signerFixture.writeSignedPolicy(t, repoRoot, mustPolicyPresetTemplate(t, "strict"))

	_, mismatchedPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate mismatched private key: %v", err)
	}
	privateKeyPath := filepath.Join(t.TempDir(), "mismatched.pem")
	writePEMEncodedEd25519PrivateKey(t, privateKeyPath, mismatchedPrivateKey, 0o600)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"apply", "-repo", repoRoot, "-verify-setup", "-private-key-file", privateKeyPath, "-key-id", signerFixture.keyID()}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "does not match trusted public key") {
		t.Fatalf("expected signer mismatch error, got %q", stderr.String())
	}
}
