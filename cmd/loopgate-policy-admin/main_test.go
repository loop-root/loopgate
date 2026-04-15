package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/loopgate"
)

type testPolicySignerFixture struct {
	publicKey  ed25519.PublicKey
	privateKey ed25519.PrivateKey
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

func TestRunValidate_ValidatesSignedRepoPolicy(t *testing.T) {
	repoRoot := t.TempDir()
	writeSignedPolicyFixture(t, repoRoot, strictMVPPresetTemplate)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"validate", "-repo", repoRoot}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "policy validation OK") {
		t.Fatalf("expected validation success output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "signature_verified: true") {
		t.Fatalf("expected signature verification output, got %q", stdout.String())
	}
}

func TestRunValidate_ValidatesUnsignedPolicyFile(t *testing.T) {
	repoRoot := t.TempDir()
	policyPath := filepath.Join(repoRoot, "policy.yaml")
	if err := os.WriteFile(policyPath, []byte(strictMVPPresetTemplate), 0o600); err != nil {
		t.Fatalf("write unsigned policy: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"validate", "-repo", repoRoot, "-policy-file", "policy.yaml"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "signature_verified: false") {
		t.Fatalf("expected unsigned validation output, got %q", stdout.String())
	}
}

func TestRunExplain_PrintsToolExplanation(t *testing.T) {
	repoRoot := t.TempDir()
	writeSignedPolicyFixture(t, repoRoot, developerPresetTemplate)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"explain", "-repo", repoRoot, "-tool", "Bash"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "[Bash]") {
		t.Fatalf("expected Bash section, got %q", output)
	}
	if !strings.Contains(output, "base_policy: approval_required (tools.shell.requires_approval=true)") {
		t.Fatalf("expected base policy explanation, got %q", output)
	}
	if !strings.Contains(output, "tool_policy.allowed_command_prefixes: ls, pwd, find, grep, cat, sed -n, head, tail, wc, sort, git status, git diff, go test, rg") {
		t.Fatalf("expected command prefixes in explanation, got %q", output)
	}
}

func TestRunRenderTemplate_RendersPreset(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"render-template", "-preset", "strict-mvp"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "deny_unknown_tools: true") {
		t.Fatalf("expected strict template output, got %q", output)
	}
	if !strings.Contains(output, "enabled: false") {
		t.Fatalf("expected strict template to disable at least one tool, got %q", output)
	}
}

func TestRunExplain_RejectsUnsupportedToolName(t *testing.T) {
	repoRoot := t.TempDir()
	writeSignedPolicyFixture(t, repoRoot, strictMVPPresetTemplate)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"explain", "-repo", repoRoot, "-tool", "NotARealTool"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "unsupported Claude Code tool") {
		t.Fatalf("expected unsupported tool error, got %q", stderr.String())
	}
}

func TestRunDiff_PrintsNormalizedPolicyDifferences(t *testing.T) {
	repoRoot := t.TempDir()
	writeSignedPolicyFixture(t, repoRoot, strictMVPPresetTemplate)

	rightPolicyPath := filepath.Join(repoRoot, "developer-policy.yaml")
	if err := os.WriteFile(rightPolicyPath, []byte(developerPresetTemplate), 0o600); err != nil {
		t.Fatalf("write right policy: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"diff", "-repo", repoRoot, "-right-policy-file", "developer-policy.yaml"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "normalized_policy_diff:") {
		t.Fatalf("expected diff header, got %q", output)
	}
	if !strings.Contains(output, "comparison_mode: normalized_effective_policy") {
		t.Fatalf("expected explicit comparison mode, got %q", output)
	}
	if !strings.Contains(output, "comparison_note: not a literal line-by-line source diff") {
		t.Fatalf("expected explicit comparison note, got %q", output)
	}
	if !strings.Contains(output, "tools.claude_code.tool_policies.Bash.enabled: false => true") {
		t.Fatalf("expected Bash enabled diff, got %q", output)
	}
	if !strings.Contains(output, "tools.http.enabled: false => true") {
		t.Fatalf("expected http enabled diff, got %q", output)
	}
}

func TestRunDiff_PrintsNoDiffForEquivalentPolicies(t *testing.T) {
	repoRoot := t.TempDir()
	writeSignedPolicyFixture(t, repoRoot, strictMVPPresetTemplate)

	rightPolicyPath := filepath.Join(repoRoot, "strict-copy.yaml")
	if err := os.WriteFile(rightPolicyPath, []byte(strictMVPPresetTemplate), 0o600); err != nil {
		t.Fatalf("write right policy: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"diff", "-repo", repoRoot, "-right-policy-file", "strict-copy.yaml"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "normalized_policy_diff: (none)") {
		t.Fatalf("expected no diff output, got %q", stdout.String())
	}
}

func TestRunApply_HotReloadsSignedPolicy(t *testing.T) {
	repoRoot := t.TempDir()
	signerFixture := newTestPolicySignerFixture(t)
	signerFixture.writeSignedPolicy(t, repoRoot, strictMVPPresetTemplate)
	writeTestMorphlingClassPolicyFixture(t, repoRoot)
	initialPolicy, err := loadPolicyDocument(repoRoot, "", "")
	if err != nil {
		t.Fatalf("load initial policy: %v", err)
	}

	socketPath := newTempSocketPath(t)
	startPolicyAdminTestServer(t, repoRoot, socketPath)

	signerFixture.writeSignedPolicy(t, repoRoot, developerPresetTemplate)
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
	signerFixture.writeSignedPolicy(t, repoRoot, strictMVPPresetTemplate)
	writeTestMorphlingClassPolicyFixture(t, repoRoot)
	initialPolicy, err := loadPolicyDocument(repoRoot, "", "")
	if err != nil {
		t.Fatalf("load initial policy: %v", err)
	}

	privateKeyPath := filepath.Join(t.TempDir(), signerFixture.keyID()+".pem")
	writePEMEncodedEd25519PrivateKey(t, privateKeyPath, signerFixture.privateKey, 0o600)

	socketPath := newTempSocketPath(t)
	startPolicyAdminTestServer(t, repoRoot, socketPath)

	signerFixture.writeSignedPolicy(t, repoRoot, developerPresetTemplate)
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

func TestRunApply_FailsWhenServerReloadsDifferentPolicy(t *testing.T) {
	repoRoot := t.TempDir()
	signerFixture := newTestPolicySignerFixture(t)
	signerFixture.writeSignedPolicy(t, repoRoot, strictMVPPresetTemplate)
	writeTestMorphlingClassPolicyFixture(t, repoRoot)

	serverRepoRoot := t.TempDir()
	signerFixture.writeSignedPolicy(t, serverRepoRoot, strictMVPPresetTemplate)
	writeTestMorphlingClassPolicyFixture(t, serverRepoRoot)

	socketPath := newTempSocketPath(t)
	startPolicyAdminTestServer(t, serverRepoRoot, socketPath)

	signerFixture.writeSignedPolicy(t, repoRoot, developerPresetTemplate)

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
	signerFixture.writeSignedPolicy(t, repoRoot, strictMVPPresetTemplate)

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

func startPolicyAdminTestServer(t *testing.T, repoRoot string, socketPath string) {
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
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("timed out waiting for loopgate test server health")
}

func writeTestMorphlingClassPolicyFixture(t *testing.T, repoRoot string) {
	t.Helper()
	_ = repoRoot
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

func defaultTestMorphlingClassPolicyYAML() string {
	return "version: \"1\"\n\n" +
		"classes:\n" +
		"  - name: reviewer\n" +
		"    description: \"Read-only analysis\"\n" +
		"    capabilities:\n" +
		"      allowed:\n" +
		"        - fs_list\n" +
		"        - fs_read\n" +
		"    sandbox:\n" +
		"      allowed_zones:\n" +
		"        - imports\n" +
		"        - scratch\n" +
		"        - workspace\n" +
		"    resource_limits:\n" +
		"      max_time_seconds: 300\n" +
		"      max_tokens: 50000\n" +
		"      max_disk_bytes: 52428800\n" +
		"    ttl:\n" +
		"      spawn_approval_ttl_seconds: 300\n" +
		"      capability_token_ttl_seconds: 360\n" +
		"      review_ttl_seconds: 86400\n" +
		"    spawn_requires_approval: false\n" +
		"    completion_requires_review: true\n" +
		"    max_concurrent: 3\n"
}
