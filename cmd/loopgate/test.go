package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"loopgate/internal/loopgate"
	controlapipkg "loopgate/internal/loopgate/controlapi"
)

func runTest(args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(stderr)

	defaultRepoRoot, err := resolveLoopgateRepoRoot("")
	if err != nil {
		return err
	}

	repoRootFlag := fs.String("repo-root", defaultRepoRoot, "repository root containing Loopgate config and signed policy files")
	socketPathFlag := fs.String("socket", "", "Loopgate socket path (default: <repo>/runtime/state/loopgate.sock)")
	if err := fs.Parse(args); err != nil {
		return normalizeFlagParseError(err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}

	repoRoot, err := resolveLoopgateRepoRoot(strings.TrimSpace(*repoRootFlag))
	if err != nil {
		return err
	}
	socketPath := strings.TrimSpace(*socketPathFlag)
	if socketPath == "" {
		socketPath = filepath.Join(repoRoot, "runtime", "state", "loopgate.sock")
	}

	statusReport, err := collectOperatorStatusReport(repoRoot, resolveSetupClaudeDir(""), socketPath, false)
	if err != nil {
		return err
	}
	if !statusReport.Policy.Loaded {
		return fmt.Errorf("policy setup is not ready: %s", strings.TrimSpace(statusReport.Policy.Error))
	}
	if !statusReport.Signer.Verified {
		return fmt.Errorf("signer setup is not ready: %s", strings.TrimSpace(statusReport.Signer.Error))
	}

	daemonSource := "running"
	var temporaryHandle temporaryLoopgateHandle
	if !statusReport.Daemon.Healthy {
		temporaryHandle, err = startTemporaryLoopgateServer(repoRoot, socketPath)
		if err != nil {
			return formatLoopgateTestStartupError(err)
		}
		defer func() {
			_ = temporaryHandle.Shutdown()
		}()
		daemonSource = temporaryHandle.source
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := loopgate.NewClient(socketPath)
	defer client.CloseIdleConnections()
	client.ConfigureSession("loopgate-test", defaultCommandSessionID("test"), []string{"fs_list", "ui.read"})
	defer func() {
		_ = client.CloseSession(context.Background())
	}()

	capabilityResponse, err := client.ExecuteCapability(ctx, controlapipkg.CapabilityRequest{
		RequestID:  defaultCommandSessionID("test-request"),
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		return fmt.Errorf("execute governed fs_list smoke test: %w", err)
	}
	if capabilityResponse.Status != controlapipkg.ResponseStatusSuccess {
		return fmt.Errorf("governed fs_list smoke test failed with status %q (%s)", capabilityResponse.Status, capabilityResponse.DenialCode)
	}
	requestID := strings.TrimSpace(capabilityResponse.RequestID)
	if requestID == "" {
		return fmt.Errorf("governed fs_list smoke test returned an empty request_id")
	}

	uiEventFound, err := waitForRecentUIRequestID(client, requestID, 2*time.Second)
	if err != nil {
		return err
	}

	auditPath := filepath.Join(repoRoot, filepath.FromSlash(defaultAuditLedgerPathSuffix))
	auditEntryFound, err := auditLedgerContainsRequestID(auditPath, requestID)
	if err != nil {
		return err
	}
	if !auditEntryFound {
		return fmt.Errorf("audit ledger %s does not contain request_id %s", auditPath, requestID)
	}

	fmt.Fprintln(stdout, "test OK")
	fmt.Fprintf(stdout, "policy_profile: %s\n", statusReport.Policy.Profile)
	fmt.Fprintf(stdout, "daemon_source: %s\n", daemonSource)
	fmt.Fprintf(stdout, "capability: fs_list\n")
	fmt.Fprintf(stdout, "request_id: %s\n", requestID)
	fmt.Fprintf(stdout, "audit_ledger_path: %s\n", auditPath)
	fmt.Fprintf(stdout, "ui_event_found: %t\n", uiEventFound)
	fmt.Fprintf(stdout, "audit_entry_found: %t\n", auditEntryFound)
	fmt.Fprintln(stdout, "next_step: Everything is working. Try using Claude Code now.")
	return nil
}

func waitForRecentUIRequestID(client *loopgate.Client, requestID string, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), defaultOperatorCommandTimeout)
		recentEventsResponse, err := client.UIRecentEvents(ctx, "")
		cancel()
		if err != nil {
			return false, fmt.Errorf("load recent UI events: %w", err)
		}
		for _, recentEvent := range summarizeRecentEvents(recentEventsResponse.Events) {
			if recentEvent.RequestID == requestID {
				return true, nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false, fmt.Errorf("timed out waiting for request_id %s in recent UI events", requestID)
}

func auditLedgerContainsRequestID(auditPath string, requestID string) (bool, error) {
	auditFile, err := os.Open(auditPath)
	if err != nil {
		return false, fmt.Errorf("open audit ledger %s: %w", auditPath, err)
	}
	defer auditFile.Close()

	scanner := bufio.NewScanner(auditFile)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), requestID) {
			return true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("scan audit ledger %s: %w", auditPath, err)
	}
	return false, nil
}

func formatLoopgateTestStartupError(err error) error {
	if err == nil {
		return nil
	}
	trimmedError := strings.TrimSpace(err.Error())
	lowerError := strings.ToLower(trimmedError)
	if strings.Contains(lowerError, "keychain") ||
		strings.Contains(lowerError, "audit checkpoint secret") ||
		strings.Contains(lowerError, "secret backend unavailable") {
		return fmt.Errorf("start temporary loopgate daemon: %s\nhint: start ./bin/loopgate once from an interactive macOS login session so Keychain-backed audit setup can complete, then rerun ./bin/loopgate test", trimmedError)
	}
	return fmt.Errorf("start temporary loopgate daemon: %w", err)
}
