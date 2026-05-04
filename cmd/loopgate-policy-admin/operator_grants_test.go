package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/loopgate"
)

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
	if !strings.Contains(grantStdout.String(), "operator grant applied") {
		t.Fatalf("expected grant success output, got %q", grantStdout.String())
	}
	if !strings.Contains(grantStdout.String(), "grant_class: repo_edit_safe") {
		t.Fatalf("expected repo_edit_safe grant output, got %q", grantStdout.String())
	}
	if !strings.Contains(grantStdout.String(), "grant_scope: permanent") {
		t.Fatalf("expected permanent grant scope output, got %q", grantStdout.String())
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

	var dryRunRevokeStdout bytes.Buffer
	var dryRunRevokeStderr bytes.Buffer
	exitCode = run([]string{"grants", "revoke", activeGrants[0].ID, "-repo", repoRoot, "-socket", socketPath, "-private-key-file", privateKeyPath, "-key-id", signerFixture.keyID(), "-dry-run"}, &dryRunRevokeStdout, &dryRunRevokeStderr)
	if exitCode != 0 {
		t.Fatalf("expected dry-run revoke exit code 0, got %d stdout=%s stderr=%s", exitCode, dryRunRevokeStdout.String(), dryRunRevokeStderr.String())
	}
	if !strings.Contains(dryRunRevokeStdout.String(), "operator grant revoke preview") || !strings.Contains(dryRunRevokeStdout.String(), "would_write: false") {
		t.Fatalf("expected dry-run revoke output, got %q", dryRunRevokeStdout.String())
	}
	afterDryRunRevoke, err := config.LoadOperatorOverrideDocumentWithHash(repoRoot)
	if err != nil {
		t.Fatalf("LoadOperatorOverrideDocumentWithHash after dry-run revoke: %v", err)
	}
	if active := config.ActiveOperatorOverrideGrants(afterDryRunRevoke.Document, config.OperatorOverrideClassRepoEditSafe); len(active) != 1 {
		t.Fatalf("expected dry-run revoke not to remove active grant, got %#v", afterDryRunRevoke.Document.Grants)
	}

	time.Sleep(600 * time.Millisecond)
	var revokeStdout bytes.Buffer
	var revokeStderr bytes.Buffer
	exitCode = run([]string{"grants", "revoke", activeGrants[0].ID, "-repo", repoRoot, "-socket", socketPath, "-private-key-file", privateKeyPath, "-key-id", signerFixture.keyID()}, &revokeStdout, &revokeStderr)
	if exitCode != 0 {
		t.Fatalf("expected revoke exit code 0, got %d stdout=%s stderr=%s", exitCode, revokeStdout.String(), revokeStderr.String())
	}
	if !strings.Contains(revokeStdout.String(), "operator grant") || !strings.Contains(revokeStdout.String(), "revoked") {
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
	if !strings.Contains(stderr.String(), "repo_edit_safe max_delegation=session does not allow permanent operator grants") {
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
	exitCode := run([]string{"grants", "add", config.OperatorOverrideClassRepoWriteSafe, "-repo", repoRoot, "-socket", socketPath, "-path", "docs", "-private-key-file", privateKeyPath, "-key-id", signerFixture.keyID()}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "operator grant applied") {
		t.Fatalf("expected grant success output, got %q", output)
	}
	if !strings.Contains(output, "grant_class: "+config.OperatorOverrideClassRepoWriteSafe) {
		t.Fatalf("expected repo_write_safe output, got %q", output)
	}
	if !strings.Contains(output, "grant_scope: permanent") {
		t.Fatalf("expected permanent grant scope output, got %q", output)
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

func TestRunGrantsList_PrintsActiveGrantState(t *testing.T) {
	repoRoot := t.TempDir()
	signerFixture := newTestPolicySignerFixture(t)
	signerFixture.writeSignedPolicy(t, repoRoot, mustPolicyPresetTemplate(t, "developer"))
	signerFixture.writeSignedOperatorOverrideDocument(t, repoRoot, config.OperatorOverrideDocument{
		Version: "1",
		Grants: []config.OperatorOverrideGrant{
			{
				ID:           "override-20260424010101-active1",
				Class:        config.OperatorOverrideClassRepoWriteSafe,
				State:        "active",
				PathPrefixes: []string{"docs"},
				CreatedAtUTC: "2026-04-24T01:01:01Z",
			},
			{
				ID:           "override-20260424020202-revoked1",
				Class:        config.OperatorOverrideClassRepoEditSafe,
				State:        "revoked",
				PathPrefixes: []string{"notes"},
				CreatedAtUTC: "2026-04-24T02:02:02Z",
				RevokedAtUTC: "2026-04-24T03:03:03Z",
			},
		},
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"grants", "list", "-repo", repoRoot}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	output := stdout.String()
	for _, expected := range []string{
		"active_grant_count: 1",
		"revoked_grant_count: 1",
		"grant.id: override-20260424010101-active1",
		"grant.class: " + config.OperatorOverrideClassRepoWriteSafe,
		"grant.state: active",
		"grant.scope: permanent",
		"grant.path_prefixes: docs",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected grants list output to contain %q, got %q", expected, output)
		}
	}
	if strings.Contains(output, "override-20260424020202-revoked1") {
		t.Fatalf("expected default grants list to omit revoked grants, got %q", output)
	}
}

func TestRunGrantsListAll_PrintsRevokedGrantHistory(t *testing.T) {
	repoRoot := t.TempDir()
	signerFixture := newTestPolicySignerFixture(t)
	signerFixture.writeSignedPolicy(t, repoRoot, mustPolicyPresetTemplate(t, "developer"))
	signerFixture.writeSignedOperatorOverrideDocument(t, repoRoot, config.OperatorOverrideDocument{
		Version: "1",
		Grants: []config.OperatorOverrideGrant{
			{
				ID:           "override-20260424020202-revoked1",
				Class:        config.OperatorOverrideClassRepoEditSafe,
				State:        "revoked",
				PathPrefixes: []string{"notes"},
				CreatedAtUTC: "2026-04-24T02:02:02Z",
				RevokedAtUTC: "2026-04-24T03:03:03Z",
			},
		},
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"grants", "list", "-repo", repoRoot, "-all"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	output := stdout.String()
	for _, expected := range []string{
		"active_grant_count: 0",
		"revoked_grant_count: 1",
		"active_grants: (none)",
		"grant.id: override-20260424020202-revoked1",
		"grant.class: " + config.OperatorOverrideClassRepoEditSafe,
		"grant.state: revoked",
		"grant.scope: permanent",
		"grant.path_prefixes: notes",
		"grant.revoked_at_utc: 2026-04-24T03:03:03Z",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected grants list -all output to contain %q, got %q", expected, output)
		}
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
	if !strings.Contains(output, "operator grant preview") || !strings.Contains(output, "grant_scope: permanent") || !strings.Contains(output, "would_write: false") {
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

func TestRunGrantsGrantEditPathAlias_DryRunDoesNotWriteOverrideDocument(t *testing.T) {
	repoRoot := t.TempDir()
	signerFixture := newTestPolicySignerFixture(t)
	signerFixture.writeSignedPolicy(t, repoRoot, delegatedRepoEditPolicyYAML())
	if err := os.MkdirAll(filepath.Join(repoRoot, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs dir: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"grants", "grant-edit-path", "-repo", repoRoot, "-path", "docs", "-dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "operator grant preview") || !strings.Contains(output, "grant_class: repo_edit_safe") {
		t.Fatalf("expected grant-edit-path alias preview output, got %q", output)
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
