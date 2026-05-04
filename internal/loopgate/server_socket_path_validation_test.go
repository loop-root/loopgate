package loopgate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewServerRejectsSocketPathOutsideAllowedRoots(t *testing.T) {
	repoRoot := t.TempDir()

	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))

	socketPath := filepath.Join(repoRoot, "loopgate.sock")
	if _, err := NewServer(repoRoot, socketPath); err == nil || !strings.Contains(err.Error(), "outside allowed runtime roots") {
		t.Fatalf("expected socket path validation error, got %v", err)
	}
}

func TestNewServerAllowsSocketPathUnderRepoRuntime(t *testing.T) {
	repoRoot := t.TempDir()

	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))

	socketPath := filepath.Join(repoRoot, "runtime", "memorybench-loopgate.sock")
	if _, err := NewServer(repoRoot, socketPath); err != nil {
		t.Fatalf("expected repo runtime socket path to be accepted, got %v", err)
	}
}

func TestServeRejectsDirectorySocketPathWithoutRemovingIt(t *testing.T) {
	repoRoot := t.TempDir()

	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))

	socketPath := filepath.Join(os.TempDir(), "loopgate-dir-target.sock")
	if err := os.RemoveAll(socketPath); err != nil {
		t.Fatalf("clear stale socket path: %v", err)
	}
	if err := os.MkdirAll(socketPath, 0o700); err != nil {
		t.Fatalf("mkdir socket path directory: %v", err)
	}
	defer func() { _ = os.RemoveAll(socketPath) }()

	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if err := server.Serve(context.Background()); err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("expected directory socket path error, got %v", err)
	}
	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("expected socket path directory to remain after failed serve, got %v", err)
	}
}
