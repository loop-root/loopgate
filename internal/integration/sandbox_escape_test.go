package integration_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"morph/internal/config"
	"morph/internal/loopgate"
)

func TestSandboxImportRejectsHostDirectoryWithSymlinkOverRealSocket(t *testing.T) {
	skipIfSymlinkUnsupported(t)

	testExecutablePath, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	harness := newLoopgateHarnessWithSetup(t, integrationPolicyYAML(true), func(repoRoot string) error {
		runtimeConfig := config.DefaultRuntimeConfig()
		runtimeConfig.ControlPlane.ExpectedSessionClientExecutable = testExecutablePath
		return config.WriteRuntimeConfigYAML(repoRoot, runtimeConfig)
	})
	status := harness.waitForStatus(t)
	hostDirectory := filepath.Join(t.TempDir(), "import-dir")
	client := harness.newClient("haven", "integration-sandbox-import", capabilityNames(status.Capabilities))
	client.SetOperatorMountPaths([]string{hostDirectory}, hostDirectory)

	outsidePath := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outsidePath, []byte("outside"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	if err := os.MkdirAll(hostDirectory, 0o700); err != nil {
		t.Fatalf("mkdir host directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hostDirectory, "notes.txt"), []byte("notes"), 0o600); err != nil {
		t.Fatalf("write host file: %v", err)
	}
	if err := os.Symlink(outsidePath, filepath.Join(hostDirectory, "escape-link.txt")); err != nil {
		t.Fatalf("create hostile symlink: %v", err)
	}

	_, err = client.SandboxImport(context.Background(), loopgate.SandboxImportRequest{
		HostSourcePath:  hostDirectory,
		DestinationName: "hostile-import",
	})
	if err == nil {
		t.Fatal("expected sandbox import with hostile symlink to be denied")
	}
	if !strings.Contains(err.Error(), loopgate.DenialCodeSandboxSymlinkNotAllowed) {
		t.Fatalf("expected sandbox symlink denial, got %v", err)
	}

	importDestinationPath := filepath.Join(harness.repoRoot, "runtime", "sandbox", "root", "home", "imports", "hostile-import")
	if _, statErr := os.Stat(importDestinationPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected hostile sandbox import destination to remain absent, got err=%v", statErr)
	}

	_, auditBytes := harness.readAuditEvents(t)
	if strings.Contains(string(auditBytes), "\"type\":\"sandbox.imported\"") {
		t.Fatalf("did not expect sandbox.imported audit event after denied hostile import, got %s", auditBytes)
	}
}

func TestSandboxStageRejectsSandboxSymlinkEscapeOverRealSocket(t *testing.T) {
	skipIfSymlinkUnsupported(t)

	harness := newLoopgateHarness(t, integrationPolicyYAML(true))
	status := harness.waitForStatus(t)
	client := harness.newClient("integration-actor", "integration-sandbox-stage", capabilityNames(status.Capabilities))

	outsidePath := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outsidePath, []byte("outside"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	sandboxSymlinkPath := filepath.Join(harness.repoRoot, "runtime", "sandbox", "root", "home", "imports", "escape.txt")
	if err := os.Symlink(outsidePath, sandboxSymlinkPath); err != nil {
		t.Fatalf("create sandbox symlink: %v", err)
	}

	_, err := client.SandboxStage(context.Background(), loopgate.SandboxStageRequest{
		SandboxSourcePath: "/morph/home/imports/escape.txt",
		OutputName:        "escaped.txt",
	})
	if err == nil {
		t.Fatal("expected sandbox stage from escaping symlink to be denied")
	}
	if !strings.Contains(err.Error(), loopgate.DenialCodeSandboxPathInvalid) {
		t.Fatalf("expected sandbox path invalid denial, got %v", err)
	}

	outputPath := filepath.Join(harness.repoRoot, "runtime", "sandbox", "root", "home", "outputs", "escaped.txt")
	if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected denied sandbox stage to leave output absent, got err=%v", statErr)
	}

	_, auditBytes := harness.readAuditEvents(t)
	if strings.Contains(string(auditBytes), "\"type\":\"sandbox.staged\"") {
		t.Fatalf("did not expect sandbox.staged audit event after denied symlink stage, got %s", auditBytes)
	}
}

func skipIfSymlinkUnsupported(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symlink test skipped on windows")
	}
}
