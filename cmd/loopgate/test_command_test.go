package main

import (
	"bytes"
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/loopgate"
)

func TestRunTest_ReusesRunningDaemon(t *testing.T) {
	repoRoot := prepareOperatorTestRepo(t, "balanced")
	socketPath, stopServer := startOperatorTestServer(t, repoRoot)
	defer stopServer()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runTest([]string{
		"-repo-root", repoRoot,
		"-socket", socketPath,
	}, &stdout, &stderr); err != nil {
		t.Fatalf("runTest: %v stderr=%s", err, stderr.String())
	}

	renderedOutput := stdout.String()
	if !strings.Contains(renderedOutput, "test OK") {
		t.Fatalf("expected test OK output, got %q", renderedOutput)
	}
	if !strings.Contains(renderedOutput, "test_state: governed-path-verified") {
		t.Fatalf("expected test_state in output, got %q", renderedOutput)
	}
	if !strings.Contains(renderedOutput, "daemon_source: running") {
		t.Fatalf("expected running daemon source, got %q", renderedOutput)
	}
	if !strings.Contains(renderedOutput, "evidence_state: ui-and-audit-confirmed") {
		t.Fatalf("expected evidence_state in output, got %q", renderedOutput)
	}
	if !strings.Contains(renderedOutput, "audit_entry_found: true") {
		t.Fatalf("expected audit evidence confirmation, got %q", renderedOutput)
	}
	if !strings.Contains(renderedOutput, "next_steps:") {
		t.Fatalf("expected next_steps block, got %q", renderedOutput)
	}
	if !strings.Contains(renderedOutput, "./bin/loopgate install-hooks.") {
		t.Fatalf("expected hook-install guidance when hooks are missing, got %q", renderedOutput)
	}
	if !strings.Contains(renderedOutput, "./bin/loopgate test after the missing pieces are in place.") {
		t.Fatalf("expected rerun guidance when hooks are missing, got %q", renderedOutput)
	}
}

func TestRunTest_StartsTemporaryDaemonWhenNeeded(t *testing.T) {
	repoRoot := prepareOperatorTestRepo(t, "balanced")
	socketPath := newShortOperatorSocketPath(t)

	originalStartTemporary := startTemporaryLoopgateServer
	defer func() {
		startTemporaryLoopgateServer = originalStartTemporary
	}()
	startTemporaryLoopgateServer = func(repoRoot string, requestedSocketPath string) (temporaryLoopgateHandle, error) {
		server, err := loopgate.NewServerWithOptions(repoRoot, requestedSocketPath)
		if err != nil {
			return temporaryLoopgateHandle{}, err
		}
		serverContext, cancel := context.WithCancel(context.Background())
		serveDone := make(chan error, 1)
		go func() {
			serveDone <- server.Serve(serverContext)
		}()
		if err := waitForHealthyLoopgate(requestedSocketPath, 5*time.Second); err != nil {
			cancel()
			serveErr := <-serveDone
			server.CloseDiagnosticLogs()
			if serveErr != nil {
				return temporaryLoopgateHandle{}, serveErr
			}
			return temporaryLoopgateHandle{}, err
		}
		return temporaryLoopgateHandle{
			source: "spawned",
			shutdown: func() error {
				cancel()
				defer server.CloseDiagnosticLogs()
				if serveErr := <-serveDone; serveErr != nil {
					return serveErr
				}
				return nil
			},
		}, nil
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runTest([]string{
		"-repo-root", repoRoot,
		"-socket", socketPath,
	}, &stdout, &stderr); err != nil {
		t.Fatalf("runTest: %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "daemon_source: spawned") {
		t.Fatalf("expected spawned daemon source, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "daemon_mode_before_test: offline") {
		t.Fatalf("expected offline daemon_mode_before_test, got %q", stdout.String())
	}
	if runtime.GOOS == "darwin" && !strings.Contains(stdout.String(), "launch_agent_state: missing") {
		t.Fatalf("expected launch_agent_state in spawned output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "started a temporary daemon") {
		t.Fatalf("expected temporary-daemon guidance, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "./bin/loopgate install-hooks.") {
		t.Fatalf("expected hook-install guidance when hooks are missing, got %q", stdout.String())
	}
}

func TestRunTest_FailsWhenSignerSetupIsMissing(t *testing.T) {
	repoRoot := prepareOperatorTestRepo(t, "balanced")
	signatureFile, err := config.LoadPolicySignatureFile(repoRoot)
	if err != nil {
		t.Fatalf("LoadPolicySignatureFile: %v", err)
	}
	privateKeyPath, err := defaultOperatorPolicySigningPrivateKeyPath(signatureFile.KeyID)
	if err != nil {
		t.Fatalf("defaultOperatorPolicySigningPrivateKeyPath: %v", err)
	}
	if err := os.Remove(privateKeyPath); err != nil {
		t.Fatalf("os.Remove(%s): %v", privateKeyPath, err)
	}

	err = runTest([]string{"-repo-root", repoRoot}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected runTest to fail when signer setup is missing")
	}
	if !strings.Contains(err.Error(), "signer setup is not ready") {
		t.Fatalf("expected signer setup failure, got %v", err)
	}
}

func TestRunTest_InstalledHooksSuggestsTryingClaude(t *testing.T) {
	repoRoot := prepareOperatorTestRepo(t, "balanced")
	socketPath, stopServer := startOperatorTestServer(t, repoRoot)
	defer stopServer()

	claudeDir, err := defaultClaudeDir()
	if err != nil {
		t.Fatalf("defaultClaudeDir: %v", err)
	}
	if err := runInstallHooks([]string{
		"-repo", repoRoot,
		"-claude-dir", claudeDir,
	}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runInstallHooks: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runTest([]string{
		"-repo-root", repoRoot,
		"-socket", socketPath,
	}, &stdout, &stderr); err != nil {
		t.Fatalf("runTest: %v stderr=%s", err, stderr.String())
	}

	renderedOutput := stdout.String()
	if !strings.Contains(renderedOutput, "Loopgate is already running for this repo. Try using Claude Code now") {
		t.Fatalf("expected Claude-ready next step when hooks are installed, got %q", renderedOutput)
	}
	if strings.Contains(renderedOutput, "Install Claude Code hooks") {
		t.Fatalf("did not expect install-hooks guidance when hooks are installed, got %q", renderedOutput)
	}
}
