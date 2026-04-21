package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loopgate/internal/config"
)

func TestRunUninstall_DefaultPreservesRuntimeState(t *testing.T) {
	repoRoot := prepareOperatorTestRepo(t, "balanced")
	claudeDir := t.TempDir()
	runtimeMarkerPath := filepath.Join(repoRoot, "runtime", "state", "marker.txt")
	if err := os.MkdirAll(filepath.Dir(runtimeMarkerPath), 0o700); err != nil {
		t.Fatalf("MkdirAll runtime marker: %v", err)
	}
	if err := os.WriteFile(runtimeMarkerPath, []byte("keep"), 0o600); err != nil {
		t.Fatalf("WriteFile runtime marker: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runUninstall([]string{
		"-repo-root", repoRoot,
		"-claude-dir", claudeDir,
	}, &stdout, &stderr); err != nil {
		t.Fatalf("runUninstall: %v stderr=%s", err, stderr.String())
	}

	if _, err := os.Stat(runtimeMarkerPath); err != nil {
		t.Fatalf("expected runtime marker to remain after default uninstall: %v", err)
	}
	if !strings.Contains(stdout.String(), "purge: false") {
		t.Fatalf("expected purge=false output, got %q", stdout.String())
	}
}

func TestRunUninstall_PurgeRemovesLocalStateButLeavesTrackedPolicy(t *testing.T) {
	repoRoot := prepareOperatorTestRepo(t, "balanced")
	claudeDir := t.TempDir()

	runtimeMarkerPath := filepath.Join(repoRoot, "runtime", "state", "marker.txt")
	if err := os.MkdirAll(filepath.Dir(runtimeMarkerPath), 0o700); err != nil {
		t.Fatalf("MkdirAll runtime marker: %v", err)
	}
	if err := os.WriteFile(runtimeMarkerPath, []byte("remove"), 0o600); err != nil {
		t.Fatalf("WriteFile runtime marker: %v", err)
	}

	signatureFile, err := config.LoadPolicySignatureFile(repoRoot)
	if err != nil {
		t.Fatalf("LoadPolicySignatureFile: %v", err)
	}
	privateKeyPath, err := defaultOperatorPolicySigningPrivateKeyPath(signatureFile.KeyID)
	if err != nil {
		t.Fatalf("defaultOperatorPolicySigningPrivateKeyPath: %v", err)
	}
	publicKeyPath, err := defaultOperatorPolicySigningPublicKeyPath(signatureFile.KeyID)
	if err != nil {
		t.Fatalf("defaultOperatorPolicySigningPublicKeyPath: %v", err)
	}
	for _, installedBinaryPath := range mustDefaultInstalledBinaryPaths(t) {
		if err := os.MkdirAll(filepath.Dir(installedBinaryPath), 0o755); err != nil {
			t.Fatalf("MkdirAll install dir: %v", err)
		}
		if err := os.WriteFile(installedBinaryPath, []byte("binary"), 0o755); err != nil {
			t.Fatalf("WriteFile installed binary: %v", err)
		}
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runUninstall([]string{
		"-repo-root", repoRoot,
		"-claude-dir", claudeDir,
		"--purge",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("runUninstall: %v stderr=%s", err, stderr.String())
	}

	if _, err := os.Stat(filepath.Join(repoRoot, "runtime")); !os.IsNotExist(err) {
		t.Fatalf("expected runtime directory removed by purge, got %v", err)
	}
	if _, err := os.Stat(privateKeyPath); !os.IsNotExist(err) {
		t.Fatalf("expected signer private key removed by purge, got %v", err)
	}
	if _, err := os.Stat(publicKeyPath); !os.IsNotExist(err) {
		t.Fatalf("expected signer public key removed by purge, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "core", "policy", "policy.yaml")); err != nil {
		t.Fatalf("expected tracked policy file to remain, got %v", err)
	}
	if !strings.Contains(stdout.String(), "purge: true") {
		t.Fatalf("expected purge=true output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "removed_managed_install_root: false") {
		t.Fatalf("expected non-managed install root output, got %q", stdout.String())
	}
}

func mustDefaultInstalledBinaryPaths(t *testing.T) []string {
	t.Helper()
	paths, err := defaultInstalledBinaryPaths()
	if err != nil {
		t.Fatalf("defaultInstalledBinaryPaths: %v", err)
	}
	return paths
}

func TestRunUninstall_PurgeRemovesManagedInstallRoot(t *testing.T) {
	repoRoot := prepareOperatorTestRepo(t, "balanced")
	claudeDir := t.TempDir()
	markerPath := filepath.Join(repoRoot, managedInstallRootMarkerFilename)
	if err := os.WriteFile(markerPath, []byte("managed=true\n"), 0o600); err != nil {
		t.Fatalf("WriteFile install marker: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runUninstall([]string{
		"-repo-root", repoRoot,
		"-claude-dir", claudeDir,
		"--purge",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("runUninstall: %v stderr=%s", err, stderr.String())
	}

	if _, err := os.Stat(repoRoot); !os.IsNotExist(err) {
		t.Fatalf("expected managed install root removed by purge, got %v", err)
	}
	if !strings.Contains(stdout.String(), "removed_managed_install_root: true") {
		t.Fatalf("expected managed install root removal output, got %q", stdout.String())
	}
}
