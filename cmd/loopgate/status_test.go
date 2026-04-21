package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunStatus_OfflineJSONIncludesSetupSummary(t *testing.T) {
	repoRoot := prepareOperatorTestRepo(t, "balanced")
	claudeDir := t.TempDir()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runStatus([]string{
		"-repo-root", repoRoot,
		"-claude-dir", claudeDir,
		"-json",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("runStatus: %v stderr=%s", err, stderr.String())
	}

	var report operatorStatusReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json.Unmarshal: %v\nstdout=%s", err, stdout.String())
	}
	if report.RepoRoot != repoRoot {
		t.Fatalf("repo_root = %q, want %q", report.RepoRoot, repoRoot)
	}
	if report.OperatorMode != "source-checkout" {
		t.Fatalf("operator_mode = %q, want source-checkout", report.OperatorMode)
	}
	if report.DaemonMode != "offline" {
		t.Fatalf("daemon_mode = %q, want offline", report.DaemonMode)
	}
	if report.Policy.Profile != "balanced" {
		t.Fatalf("policy profile = %q, want balanced", report.Policy.Profile)
	}
	if report.ClaudeHooks.State != "missing" {
		t.Fatalf("claude hook state = %q, want missing", report.ClaudeHooks.State)
	}
	if report.AuditLedgerPath != filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl") {
		t.Fatalf("audit_ledger_path = %q", report.AuditLedgerPath)
	}
	if report.Daemon.Healthy {
		t.Fatalf("expected offline status with no running daemon, got %#v", report.Daemon)
	}
}

func TestRunStatus_OfflineHumanOutputIncludesNextSteps(t *testing.T) {
	repoRoot := prepareOperatorTestRepo(t, "balanced")
	claudeDir := t.TempDir()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runStatus([]string{
		"-repo-root", repoRoot,
		"-claude-dir", claudeDir,
	}, &stdout, &stderr); err != nil {
		t.Fatalf("runStatus: %v stderr=%s", err, stderr.String())
	}

	renderedOutput := stdout.String()
	if !strings.Contains(renderedOutput, "next_steps:") {
		t.Fatalf("expected next_steps in human status output, got %q", renderedOutput)
	}
	if !strings.Contains(renderedOutput, "operator_mode: source-checkout") {
		t.Fatalf("expected operator mode in human status output, got %q", renderedOutput)
	}
	if !strings.Contains(renderedOutput, "daemon_mode: offline") {
		t.Fatalf("expected daemon mode in human status output, got %q", renderedOutput)
	}
	if !strings.Contains(renderedOutput, "claude_hooks_state: missing") {
		t.Fatalf("expected hook state in human status output, got %q", renderedOutput)
	}
	if !strings.Contains(renderedOutput, "./bin/loopgate install-hooks") {
		t.Fatalf("expected install-hooks guidance in human status output, got %q", renderedOutput)
	}
	if !strings.Contains(renderedOutput, "./bin/loopgate status and ./bin/loopgate test") {
		t.Fatalf("expected status/test rerun guidance in human status output, got %q", renderedOutput)
	}
	if !strings.Contains(renderedOutput, "./bin/loopgate-doctor report") {
		t.Fatalf("expected doctor guidance in human status output, got %q", renderedOutput)
	}
}

func TestRunStatus_ManagedInstallRootUsesBareCommandHints(t *testing.T) {
	repoRoot := prepareOperatorTestRepo(t, "balanced")
	claudeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, managedInstallRootMarkerFilename), []byte("version=test\n"), 0o600); err != nil {
		t.Fatalf("write managed install marker: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runStatus([]string{
		"-repo-root", repoRoot,
		"-claude-dir", claudeDir,
	}, &stdout, &stderr); err != nil {
		t.Fatalf("runStatus: %v stderr=%s", err, stderr.String())
	}

	renderedOutput := stdout.String()
	if !strings.Contains(renderedOutput, "operator_mode: managed-install") {
		t.Fatalf("expected managed install operator mode in status output, got %q", renderedOutput)
	}
	if !strings.Contains(renderedOutput, "daemon_mode: offline") {
		t.Fatalf("expected offline daemon mode in managed install status output, got %q", renderedOutput)
	}
	if !strings.Contains(renderedOutput, "loopgate install-hooks") || strings.Contains(renderedOutput, "./bin/loopgate install-hooks") {
		t.Fatalf("expected bare install-hooks guidance in managed install status output, got %q", renderedOutput)
	}
	if !strings.Contains(renderedOutput, "loopgate status and loopgate test") || strings.Contains(renderedOutput, "./bin/loopgate status and ./bin/loopgate test") {
		t.Fatalf("expected bare status/test rerun guidance in managed install status output, got %q", renderedOutput)
	}
	if !strings.Contains(renderedOutput, "loopgate-doctor report") || strings.Contains(renderedOutput, "./bin/loopgate-doctor report") {
		t.Fatalf("expected bare doctor guidance in managed install status output, got %q", renderedOutput)
	}
}

func TestRunStatus_PartialHooksShowRepairGuidance(t *testing.T) {
	repoRoot := prepareOperatorTestRepo(t, "balanced")
	claudeDir := t.TempDir()
	claudeHooksDir := filepath.Join(claudeDir, claudeHooksDirname)
	if err := os.MkdirAll(claudeHooksDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%s): %v", claudeHooksDir, err)
	}
	scriptPath := filepath.Join(claudeHooksDir, "loopgate_pretool.py")
	if err := os.WriteFile(scriptPath, []byte("#!/usr/bin/env python3\n"), 0o755); err != nil {
		t.Fatalf("os.WriteFile(%s): %v", scriptPath, err)
	}
	writeTestClaudeSettings(t, filepath.Join(claudeDir, claudeSettingsFilename), claudeSettings{
		Hooks: map[string][]claudeHookMatcherGroup{
			"PreToolUse": {
				{
					Matcher: "*",
					Hooks: []claudeHookAction{
						{
							Type:    "command",
							Command: "python3 " + shellQuoteHookCommandPath(scriptPath),
						},
					},
				},
			},
		},
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runStatus([]string{
		"-repo-root", repoRoot,
		"-claude-dir", claudeDir,
	}, &stdout, &stderr); err != nil {
		t.Fatalf("runStatus: %v stderr=%s", err, stderr.String())
	}

	renderedOutput := stdout.String()
	if !strings.Contains(renderedOutput, "claude_hooks_state: partial") {
		t.Fatalf("expected partial hook state, got %q", renderedOutput)
	}
	if !strings.Contains(renderedOutput, "repair Claude Code hooks with ./bin/loopgate install-hooks, or remove stale entries with ./bin/loopgate remove-hooks") {
		t.Fatalf("expected partial-hook repair guidance, got %q", renderedOutput)
	}
}

func TestRunStatus_LiveIncludesRecentEvents(t *testing.T) {
	repoRoot := prepareOperatorTestRepo(t, "strict")
	claudeDir := t.TempDir()
	socketPath, stopServer := startOperatorTestServer(t, repoRoot)
	defer stopServer()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runStatus([]string{
		"-repo-root", repoRoot,
		"-claude-dir", claudeDir,
		"-socket", socketPath,
		"-live",
		"-json",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("runStatus: %v stderr=%s", err, stderr.String())
	}

	var report operatorStatusReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json.Unmarshal: %v\nstdout=%s", err, stdout.String())
	}
	if !report.Daemon.Healthy {
		t.Fatalf("expected live status to see a healthy daemon, got %#v", report.Daemon)
	}
	if report.Live == nil {
		t.Fatalf("expected live section in status report, got %#v", report)
	}
	if report.DaemonMode != "foreground-or-manual" {
		t.Fatalf("daemon_mode = %q, want foreground-or-manual", report.DaemonMode)
	}
	if strings.TrimSpace(report.Live.ControlSessionID) == "" {
		t.Fatalf("expected control session id in live report, got %#v", report.Live)
	}
	if len(report.Live.RecentEvents) == 0 {
		t.Fatalf("expected recent events in live report, got %#v", report.Live)
	}
}
