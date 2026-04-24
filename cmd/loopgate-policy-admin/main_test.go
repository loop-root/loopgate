package main

import (
	"bytes"
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
	controlapipkg "loopgate/internal/loopgate/controlapi"
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

func TestRunValidate_ValidatesSignedRepoPolicy(t *testing.T) {
	repoRoot := t.TempDir()
	writeSignedPolicyFixture(t, repoRoot, mustPolicyPresetTemplate(t, "strict"))

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
	if err := os.WriteFile(policyPath, []byte(mustPolicyPresetTemplate(t, "strict")), 0o600); err != nil {
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
	writeSignedPolicyFixture(t, repoRoot, mustPolicyPresetTemplate(t, "developer"))

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
	if !strings.Contains(output, "operator_override.class: repo_bash_safe") {
		t.Fatalf("expected operator override class in explanation, got %q", output)
	}
	if !strings.Contains(output, "operator_override.max_delegation: persistent") {
		t.Fatalf("expected operator override delegation in explanation, got %q", output)
	}
	if !strings.Contains(output, "tool_policy.allowed_command_prefixes: ls, pwd, find, grep, cat, sed -n, head, tail, wc, sort, git status, git diff, go test, rg") {
		t.Fatalf("expected command prefixes in explanation, got %q", output)
	}
}

func TestRunRenderTemplate_RendersPreset(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"render-template", "-preset", "strict"}, &stdout, &stderr)
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

func TestRunRenderTemplate_RendersBalancedPreset(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"render-template", "-preset", "balanced"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "enabled: true") {
		t.Fatalf("expected balanced template to enable at least one guarded tool, got %q", output)
	}
	if !strings.Contains(output, "timeout_seconds: 10") {
		t.Fatalf("expected balanced template to retain explicit HTTP timeout, got %q", output)
	}
}

func TestRunRenderTemplate_RendersReadOnlyPreset(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"render-template", "-preset", "read-only"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "shell:\n    enabled: false") {
		t.Fatalf("expected read-only template to disable shell, got %q", output)
	}
	if !strings.Contains(output, "Edit:\n        enabled: false") {
		t.Fatalf("expected read-only template to disable Claude Edit, got %q", output)
	}
}

func TestRunExplain_RejectsUnsupportedToolName(t *testing.T) {
	repoRoot := t.TempDir()
	writeSignedPolicyFixture(t, repoRoot, mustPolicyPresetTemplate(t, "strict"))

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
	writeSignedPolicyFixture(t, repoRoot, mustPolicyPresetTemplate(t, "strict"))

	rightPolicyPath := filepath.Join(repoRoot, "developer-policy.yaml")
	if err := os.WriteFile(rightPolicyPath, []byte(mustPolicyPresetTemplate(t, "developer")), 0o600); err != nil {
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
	writeSignedPolicyFixture(t, repoRoot, mustPolicyPresetTemplate(t, "strict"))

	rightPolicyPath := filepath.Join(repoRoot, "strict-copy.yaml")
	if err := os.WriteFile(rightPolicyPath, []byte(mustPolicyPresetTemplate(t, "strict")), 0o600); err != nil {
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

func TestRunOverridesGrantEditPath_WritesAndReloadsSignedOverride(t *testing.T) {
	repoRoot := t.TempDir()
	signerFixture := newTestPolicySignerFixture(t)
	signerFixture.writeSignedPolicy(t, repoRoot, delegatedRepoEditPolicyYAML())
	if err := os.MkdirAll(filepath.Join(repoRoot, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs dir: %v", err)
	}

	privateKeyPath := filepath.Join(t.TempDir(), signerFixture.keyID()+".pem")
	writePEMEncodedEd25519PrivateKey(t, privateKeyPath, signerFixture.privateKey, 0o600)

	socketPath := newTempSocketPath(t)
	_ = startPolicyAdminTestServer(t, repoRoot, socketPath)

	var grantStdout bytes.Buffer
	var grantStderr bytes.Buffer
	exitCode := run([]string{"overrides", "grant-edit-path", "-repo", repoRoot, "-socket", socketPath, "-path", "docs", "-private-key-file", privateKeyPath, "-key-id", signerFixture.keyID()}, &grantStdout, &grantStderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stdout=%s stderr=%s", exitCode, grantStdout.String(), grantStderr.String())
	}
	if !strings.Contains(grantStdout.String(), "operator override grant applied") {
		t.Fatalf("expected grant success output, got %q", grantStdout.String())
	}
	if !strings.Contains(grantStdout.String(), "grant_class: repo_edit_safe") {
		t.Fatalf("expected repo_edit_safe grant output, got %q", grantStdout.String())
	}
	if !strings.Contains(grantStdout.String(), "path_prefix: docs") {
		t.Fatalf("expected docs path prefix output, got %q", grantStdout.String())
	}

	loadResult, err := config.LoadOperatorOverrideDocumentWithHash(repoRoot)
	if err != nil {
		t.Fatalf("LoadOperatorOverrideDocumentWithHash after grant: %v", err)
	}
	if !loadResult.Present {
		t.Fatal("expected signed operator override document after grant")
	}
	if loadResult.SignatureKeyID != signerFixture.keyID() {
		t.Fatalf("expected signature key id %q, got %q", signerFixture.keyID(), loadResult.SignatureKeyID)
	}
	activeGrants := config.ActiveOperatorOverrideGrants(loadResult.Document, config.OperatorOverrideClassRepoEditSafe)
	if len(activeGrants) != 1 {
		t.Fatalf("expected one active repo_edit_safe grant, got %#v", loadResult.Document.Grants)
	}
	if got := activeGrants[0].PathPrefixes; len(got) != 1 || got[0] != "docs" {
		t.Fatalf("expected docs path prefix, got %#v", got)
	}

	configClient := loopgate.NewClient(socketPath)
	configClient.ConfigureSession("policy-admin-overrides", "policy-admin-overrides", []string{"config.read"})
	time.Sleep(600 * time.Millisecond)
	runningOverrideDocument, err := configClient.LoadOperatorOverrideConfig(context.Background())
	if err != nil {
		t.Fatalf("LoadOperatorOverrideConfig after grant: %v", err)
	}
	if active := config.ActiveOperatorOverrideGrants(runningOverrideDocument, config.OperatorOverrideClassRepoEditSafe); len(active) != 1 {
		t.Fatalf("expected running server override runtime to expose one active grant, got %#v", runningOverrideDocument.Grants)
	}

	time.Sleep(600 * time.Millisecond)
	var revokeStdout bytes.Buffer
	var revokeStderr bytes.Buffer
	exitCode = run([]string{"overrides", "revoke", activeGrants[0].ID, "-repo", repoRoot, "-socket", socketPath, "-private-key-file", privateKeyPath, "-key-id", signerFixture.keyID()}, &revokeStdout, &revokeStderr)
	if exitCode != 0 {
		t.Fatalf("expected revoke exit code 0, got %d stdout=%s stderr=%s", exitCode, revokeStdout.String(), revokeStderr.String())
	}
	if !strings.Contains(revokeStdout.String(), "revoked") {
		t.Fatalf("expected revoke output, got %q", revokeStdout.String())
	}

	time.Sleep(600 * time.Millisecond)
	reloadedDocument, err := configClient.LoadOperatorOverrideConfig(context.Background())
	if err != nil {
		t.Fatalf("LoadOperatorOverrideConfig after revoke: %v", err)
	}
	if active := config.ActiveOperatorOverrideGrants(reloadedDocument, config.OperatorOverrideClassRepoEditSafe); len(active) != 0 {
		t.Fatalf("expected no active grants after revoke, got %#v", reloadedDocument.Grants)
	}
}

func TestRunOverridesGrantEditPath_RejectsNonPersistentParentDelegation(t *testing.T) {
	repoRoot := t.TempDir()
	signerFixture := newTestPolicySignerFixture(t)
	signerFixture.writeSignedPolicy(t, repoRoot, mustPolicyPresetTemplate(t, "strict"))

	privateKeyPath := filepath.Join(t.TempDir(), signerFixture.keyID()+".pem")
	writePEMEncodedEd25519PrivateKey(t, privateKeyPath, signerFixture.privateKey, 0o600)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"overrides", "grant-edit-path", "-repo", repoRoot, "-path", "docs", "-private-key-file", privateKeyPath, "-key-id", signerFixture.keyID()}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "repo_edit_safe max_delegation=session does not allow persistent operator override grants") {
		t.Fatalf("expected delegation rejection error, got %q", stderr.String())
	}
}

func TestRunOverridesGrant_WritesGenericPathScopedGrant(t *testing.T) {
	repoRoot := t.TempDir()
	signerFixture := newTestPolicySignerFixture(t)
	signerFixture.writeSignedPolicy(t, repoRoot, mustPolicyPresetTemplate(t, "developer"))
	if err := os.MkdirAll(filepath.Join(repoRoot, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs dir: %v", err)
	}

	privateKeyPath := filepath.Join(t.TempDir(), signerFixture.keyID()+".pem")
	writePEMEncodedEd25519PrivateKey(t, privateKeyPath, signerFixture.privateKey, 0o600)

	socketPath := newTempSocketPath(t)
	_ = startPolicyAdminTestServer(t, repoRoot, socketPath)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"overrides", "grant", config.OperatorOverrideClassRepoWriteSafe, "-repo", repoRoot, "-socket", socketPath, "-path", "docs", "-private-key-file", privateKeyPath, "-key-id", signerFixture.keyID()}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "operator override grant applied") {
		t.Fatalf("expected grant success output, got %q", output)
	}
	if !strings.Contains(output, "grant_class: "+config.OperatorOverrideClassRepoWriteSafe) {
		t.Fatalf("expected repo_write_safe output, got %q", output)
	}
	if !strings.Contains(output, "path_prefix: docs") {
		t.Fatalf("expected docs path output, got %q", output)
	}

	loadResult, err := config.LoadOperatorOverrideDocumentWithHash(repoRoot)
	if err != nil {
		t.Fatalf("LoadOperatorOverrideDocumentWithHash after grant: %v", err)
	}
	activeGrants := config.ActiveOperatorOverrideGrants(loadResult.Document, config.OperatorOverrideClassRepoWriteSafe)
	if len(activeGrants) != 1 {
		t.Fatalf("expected one active repo_write_safe grant, got %#v", loadResult.Document.Grants)
	}
	if got := activeGrants[0].PathPrefixes; len(got) != 1 || got[0] != "docs" {
		t.Fatalf("expected docs path prefix, got %#v", got)
	}
}

func TestRunOverridesGrant_DryRunDoesNotWriteOverrideDocument(t *testing.T) {
	repoRoot := t.TempDir()
	signerFixture := newTestPolicySignerFixture(t)
	signerFixture.writeSignedPolicy(t, repoRoot, delegatedRepoEditPolicyYAML())
	if err := os.MkdirAll(filepath.Join(repoRoot, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs dir: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"overrides", "grant", config.OperatorOverrideClassRepoEditSafe, "-repo", repoRoot, "-path", "docs", "-dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "operator override grant preview") || !strings.Contains(output, "would_write: false") {
		t.Fatalf("expected dry-run preview output, got %q", output)
	}
	loadResult, err := config.LoadOperatorOverrideDocumentWithHash(repoRoot)
	if err != nil {
		t.Fatalf("LoadOperatorOverrideDocumentWithHash after dry run: %v", err)
	}
	if loadResult.Present {
		t.Fatalf("expected dry-run not to write operator override document, got %#v", loadResult)
	}
}

func TestRunOverridesGrant_RejectsUnsupportedPathScopedClass(t *testing.T) {
	repoRoot := t.TempDir()
	signerFixture := newTestPolicySignerFixture(t)
	signerFixture.writeSignedPolicy(t, repoRoot, mustPolicyPresetTemplate(t, "developer"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"overrides", "grant", config.OperatorOverrideClassWebAccessTrusted, "-repo", repoRoot, "-path", "docs"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "unsupported path-scoped operator override class") {
		t.Fatalf("expected unsupported class error, got %q", stderr.String())
	}
}

func TestRunApprovalsList_PrintsPendingApprovals(t *testing.T) {
	repoRoot := t.TempDir()
	signerFixture := newTestPolicySignerFixture(t)
	signerFixture.writeSignedPolicy(t, repoRoot, mustPolicyPresetTemplate(t, "strict"))

	socketPath := newTempSocketPath(t)
	_ = startPolicyAdminTestServer(t, repoRoot, socketPath)

	requestClient := loopgate.NewClient(socketPath)
	requestClient.ConfigureSession("approval-requester", "approval-requester-session", []string{"fs_write"})
	pendingResponse, err := requestClient.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-policy-admin-list",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "pending.txt",
			"content": "hello",
		},
	})
	if err != nil {
		t.Fatalf("execute pending approval: %v", err)
	}
	if !pendingResponse.ApprovalRequired {
		t.Fatalf("expected approval required response, got %#v", pendingResponse)
	}
	time.Sleep(600 * time.Millisecond)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"approvals", "list", "-repo", repoRoot, "-socket", socketPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}

	output := stdout.String()
	for _, expected := range []string{"APPROVAL ID", pendingResponse.ApprovalRequestID, "approval-requester", "fs_write"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected approvals list output to contain %q, got %q", expected, output)
		}
	}
}

func TestRunApprovalsApprove_CompletesApprovalAndWritesAuditReason(t *testing.T) {
	repoRoot := t.TempDir()
	signerFixture := newTestPolicySignerFixture(t)
	signerFixture.writeSignedPolicy(t, repoRoot, mustPolicyPresetTemplate(t, "strict"))

	socketPath := newTempSocketPath(t)
	workspaceRoot := startPolicyAdminTestServer(t, repoRoot, socketPath)

	requestClient := loopgate.NewClient(socketPath)
	requestClient.ConfigureSession("approval-requester", "approval-requester-session", []string{"fs_write"})
	pendingResponse, err := requestClient.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-policy-admin-approve",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "approved.txt",
			"content": "hello from approval",
		},
	})
	if err != nil {
		t.Fatalf("execute pending approval: %v", err)
	}
	if !pendingResponse.ApprovalRequired {
		t.Fatalf("expected approval required response, got %#v", pendingResponse)
	}
	time.Sleep(600 * time.Millisecond)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"approvals", "approve", pendingResponse.ApprovalRequestID, "-repo", repoRoot, "-socket", socketPath, "-reason", "ship it"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}

	writtenBytes, err := os.ReadFile(filepath.Join(workspaceRoot, "approved.txt"))
	if err != nil {
		t.Fatalf("read approved file: %v", err)
	}
	if string(writtenBytes) != "hello from approval" {
		t.Fatalf("unexpected approved file contents: %q", string(writtenBytes))
	}

	grantedEvent := readLastAuditEventOfType(t, repoRoot, "approval.granted")
	if got := grantedEvent.Data["operator_reason"]; got != "ship it" {
		t.Fatalf("expected operator_reason %q, got %#v", "ship it", got)
	}
	grantedEventHash, _ := grantedEvent.Data["event_hash"].(string)
	expectedOutput := "approval " + pendingResponse.ApprovalRequestID + " approved audit_event_hash=" + grantedEventHash
	if strings.TrimSpace(stdout.String()) != expectedOutput {
		t.Fatalf("unexpected approve output: %q", stdout.String())
	}
}

func TestRunApprovalsDeny_RecordsAuditReason(t *testing.T) {
	repoRoot := t.TempDir()
	signerFixture := newTestPolicySignerFixture(t)
	signerFixture.writeSignedPolicy(t, repoRoot, mustPolicyPresetTemplate(t, "strict"))

	socketPath := newTempSocketPath(t)
	workspaceRoot := startPolicyAdminTestServer(t, repoRoot, socketPath)

	requestClient := loopgate.NewClient(socketPath)
	requestClient.ConfigureSession("approval-requester", "approval-requester-session", []string{"fs_write"})
	pendingResponse, err := requestClient.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-policy-admin-deny",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "denied.txt",
			"content": "hello from denied approval",
		},
	})
	if err != nil {
		t.Fatalf("execute pending approval: %v", err)
	}
	if !pendingResponse.ApprovalRequired {
		t.Fatalf("expected approval required response, got %#v", pendingResponse)
	}
	time.Sleep(600 * time.Millisecond)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"approvals", "deny", pendingResponse.ApprovalRequestID, "-repo", repoRoot, "-socket", socketPath, "-reason", "not safe yet"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}

	if _, err := os.Stat(filepath.Join(workspaceRoot, "denied.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected denied file to stay absent, stat err=%v", err)
	}

	deniedEvent := readLastAuditEventOfType(t, repoRoot, "approval.denied")
	if got := deniedEvent.Data["operator_reason"]; got != "not safe yet" {
		t.Fatalf("expected operator_reason %q, got %#v", "not safe yet", got)
	}
	deniedEventHash, _ := deniedEvent.Data["event_hash"].(string)
	expectedOutput := "approval " + pendingResponse.ApprovalRequestID + " denied audit_event_hash=" + deniedEventHash
	if strings.TrimSpace(stdout.String()) != expectedOutput {
		t.Fatalf("unexpected deny output: %q", stdout.String())
	}
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
