package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/loopgate"
)

func prepareOperatorTestRepo(t *testing.T, profile string) string {
	t.Helper()
	repoRoot := makeSetupTestRepo(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv(policySigningTrustDirEnv, filepath.Join(t.TempDir(), "trusted"))

	err := runSetup([]string{
		"-repo-root", repoRoot,
		"-profile", profile,
		"-skip-hooks",
		"-skip-launch-agent",
	}, strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("runSetup: %v", err)
	}
	runtimeConfig := config.DefaultRuntimeConfig()
	runtimeConfig.Logging.AuditLedger.HMACCheckpoint.Enabled = false
	if err := config.WriteRuntimeConfigYAML(repoRoot, runtimeConfig); err != nil {
		t.Fatalf("WriteRuntimeConfigYAML: %v", err)
	}
	return repoRoot
}

func startOperatorTestServer(t *testing.T, repoRoot string) (string, func()) {
	t.Helper()
	socketPath := newShortOperatorSocketPath(t)
	server, err := loopgate.NewServerWithOptions(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("NewServerWithOptions: %v", err)
	}

	serverContext, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(serverContext)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for daemonStatus := inspectDaemon(socketPath); !daemonStatus.Healthy; daemonStatus = inspectDaemon(socketPath) {
		select {
		case serveErr := <-serveDone:
			cancel()
			t.Fatalf("server exited before health check: %v", serveErr)
		default:
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("waitForHealthyLoopgate: timed out waiting for %s", socketPath)
		}
		time.Sleep(25 * time.Millisecond)
	}
	return socketPath, func() {
		cancel()
		if err := <-serveDone; err != nil {
			t.Fatalf("server shutdown: %v", err)
		}
		server.CloseDiagnosticLogs()
	}
}

func newShortOperatorSocketPath(t *testing.T) string {
	t.Helper()

	socketFile, err := os.CreateTemp(os.TempDir(), "loopgate-cmd-*.sock")
	if err != nil {
		t.Fatalf("CreateTemp socket path: %v", err)
	}
	socketPath := socketFile.Name()
	if err := socketFile.Close(); err != nil {
		t.Fatalf("Close temp socket file: %v", err)
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		t.Fatalf("Remove temp socket placeholder: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	return socketPath
}
