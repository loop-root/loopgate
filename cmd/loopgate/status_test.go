package main

import (
	"bytes"
	"encoding/json"
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
	if report.Policy.Profile != "balanced" {
		t.Fatalf("policy profile = %q, want balanced", report.Policy.Profile)
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
	if !strings.Contains(renderedOutput, "./bin/loopgate install-hooks") {
		t.Fatalf("expected install-hooks guidance in human status output, got %q", renderedOutput)
	}
	if !strings.Contains(renderedOutput, "./bin/loopgate status and ./bin/loopgate test") {
		t.Fatalf("expected status/test rerun guidance in human status output, got %q", renderedOutput)
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
	if strings.TrimSpace(report.Live.ControlSessionID) == "" {
		t.Fatalf("expected control session id in live report, got %#v", report.Live)
	}
	if len(report.Live.RecentEvents) == 0 {
		t.Fatalf("expected recent events in live report, got %#v", report.Live)
	}
}
