package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/loopgate"
	controlapipkg "loopgate/internal/loopgate/controlapi"
)

const (
	defaultOperatorCommandTimeout = 2 * time.Second
	defaultAuditLedgerPathSuffix  = "runtime/state/loopgate_events.jsonl"
)

var execCommandContext = exec.CommandContext
var execCommand = exec.Command
var startTemporaryLoopgateServer = startTemporaryLoopgate

type operatorPolicyStatus struct {
	Loaded         bool   `json:"loaded"`
	Profile        string `json:"profile"`
	ContentSHA256  string `json:"content_sha256,omitempty"`
	SignatureKeyID string `json:"signature_key_id,omitempty"`
	Error          string `json:"error,omitempty"`
}

type operatorSignerStatus struct {
	Verified         bool   `json:"verified"`
	KeyID            string `json:"key_id,omitempty"`
	PrivateKeyPath   string `json:"private_key_path,omitempty"`
	TrustedPublicKey string `json:"trusted_public_key_path,omitempty"`
	Error            string `json:"error,omitempty"`
}

type operatorHooksStatus struct {
	ClaudeDir         string   `json:"claude_dir"`
	State             string   `json:"state"`
	Installed         bool     `json:"installed"`
	SettingsPaths     []string `json:"settings_paths,omitempty"`
	ManagedEventCount int      `json:"managed_event_count"`
	CopiedScriptCount int      `json:"copied_script_count"`
	Error             string   `json:"error,omitempty"`
}

type operatorLaunchAgentStatus struct {
	Supported bool   `json:"supported"`
	Label     string `json:"label,omitempty"`
	PlistPath string `json:"plist_path,omitempty"`
	Installed bool   `json:"installed"`
	Loaded    bool   `json:"loaded"`
	Error     string `json:"error,omitempty"`
}

type operatorDaemonStatus struct {
	SocketExists bool   `json:"socket_exists"`
	Healthy      bool   `json:"healthy"`
	Version      string `json:"version,omitempty"`
	Error        string `json:"error,omitempty"`
}

type operatorRecentEvent struct {
	ID                string `json:"id"`
	Type              string `json:"type"`
	TS                string `json:"ts"`
	RequestID         string `json:"request_id,omitempty"`
	Capability        string `json:"capability,omitempty"`
	DenialCode        string `json:"denial_code,omitempty"`
	Message           string `json:"message,omitempty"`
	ApprovalRequestID string `json:"approval_request_id,omitempty"`
}

type operatorLiveStatus struct {
	ControlSessionID string                              `json:"control_session_id,omitempty"`
	PersonaName      string                              `json:"persona_name,omitempty"`
	PersonaVersion   string                              `json:"persona_version,omitempty"`
	RuntimeSessionID string                              `json:"runtime_session_id,omitempty"`
	TurnCount        int                                 `json:"turn_count"`
	PendingApprovals int                                 `json:"pending_approvals"`
	CapabilityCount  int                                 `json:"capability_count"`
	ConnectionCount  int                                 `json:"connection_count"`
	Policy           controlapipkg.UIStatusPolicySummary `json:"policy"`
	RecentEvents     []operatorRecentEvent               `json:"recent_events,omitempty"`
	Error            string                              `json:"error,omitempty"`
}

type operatorStatusReport struct {
	OK              bool                      `json:"ok"`
	OperatorMode    string                    `json:"operator_mode"`
	DaemonMode      string                    `json:"daemon_mode"`
	RepoRoot        string                    `json:"repo_root"`
	PolicyPath      string                    `json:"policy_path"`
	SignaturePath   string                    `json:"signature_path"`
	AuditLedgerPath string                    `json:"audit_ledger_path"`
	SocketPath      string                    `json:"socket_path"`
	Policy          operatorPolicyStatus      `json:"policy"`
	Signer          operatorSignerStatus      `json:"signer"`
	ClaudeHooks     operatorHooksStatus       `json:"claude_hooks"`
	LaunchAgent     operatorLaunchAgentStatus `json:"launch_agent"`
	Daemon          operatorDaemonStatus      `json:"daemon"`
	Live            *operatorLiveStatus       `json:"live,omitempty"`
}

func runStatus(args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)

	defaultRepoRoot, err := resolveLoopgateRepoRoot("")
	if err != nil {
		return err
	}
	defaultClaudeConfigDir, err := defaultClaudeDir()
	if err != nil {
		return err
	}

	repoRootFlag := fs.String("repo-root", defaultRepoRoot, "repository root containing Loopgate config and signed policy files")
	claudeDirFlag := fs.String("claude-dir", defaultClaudeConfigDir, "Claude config directory")
	socketPathFlag := fs.String("socket", "", "Loopgate socket path (default: <repo>/runtime/state/loopgate.sock)")
	liveFlag := fs.Bool("live", false, "include pending approvals and recent display-safe events from the running daemon")
	jsonFlag := fs.Bool("json", false, "print machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return normalizeFlagParseError(err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}

	report, err := collectOperatorStatusReport(strings.TrimSpace(*repoRootFlag), strings.TrimSpace(*claudeDirFlag), strings.TrimSpace(*socketPathFlag), *liveFlag)
	if err != nil {
		return err
	}

	if *jsonFlag {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}

	printOperatorStatusReport(stdout, report)
	return nil
}

func collectOperatorStatusReport(repoRootFlag string, claudeDirFlag string, socketPathFlag string, live bool) (operatorStatusReport, error) {
	repoRoot, err := resolveLoopgateRepoRoot(repoRootFlag)
	if err != nil {
		return operatorStatusReport{}, err
	}
	socketPath := strings.TrimSpace(socketPathFlag)
	if socketPath == "" {
		socketPath = filepath.Join(repoRoot, "runtime", "state", "loopgate.sock")
	}

	report := operatorStatusReport{
		OperatorMode:    operatorMode(repoRoot),
		RepoRoot:        repoRoot,
		PolicyPath:      filepath.Join(repoRoot, "core", "policy", "policy.yaml"),
		SignaturePath:   config.PolicySignaturePath(repoRoot),
		AuditLedgerPath: filepath.Join(repoRoot, filepath.FromSlash(defaultAuditLedgerPathSuffix)),
		SocketPath:      filepath.Clean(socketPath),
		Policy: operatorPolicyStatus{
			Profile: "custom",
		},
		ClaudeHooks: operatorHooksStatus{
			ClaudeDir: filepath.Clean(claudeDirFlag),
		},
		LaunchAgent: operatorLaunchAgentStatus{
			Supported: runtimeGOOS() == "darwin",
		},
	}

	loadResult, policyErr := config.LoadPolicyWithHash(repoRoot)
	if policyErr != nil {
		report.Policy.Error = policyErr.Error()
	} else {
		report.Policy.Loaded = true
		report.Policy.ContentSHA256 = loadResult.ContentSHA256
		report.Policy.Profile = config.DetectSetupPolicyTemplatePresetName(loadResult.Policy)
	}

	signatureFile, signatureErr := config.LoadPolicySignatureFile(repoRoot)
	if signatureErr != nil {
		if report.Policy.Error == "" {
			report.Policy.Error = signatureErr.Error()
		}
	} else {
		report.Policy.SignatureKeyID = signatureFile.KeyID
		report.Signer.KeyID = signatureFile.KeyID
		privateKeyPath, privateKeyErr := defaultOperatorPolicySigningPrivateKeyPath(signatureFile.KeyID)
		if privateKeyErr != nil {
			report.Signer.Error = privateKeyErr.Error()
		} else {
			report.Signer.PrivateKeyPath = privateKeyPath
			trustedPublicKeyPath, trustedPublicKeyErr := defaultOperatorPolicySigningPublicKeyPath(signatureFile.KeyID)
			if trustedPublicKeyErr != nil {
				report.Signer.Error = trustedPublicKeyErr.Error()
			} else {
				report.Signer.TrustedPublicKey = trustedPublicKeyPath
				if _, verifyErr := config.VerifyPolicySigningSetup(repoRoot, privateKeyPath, signatureFile.KeyID); verifyErr != nil {
					report.Signer.Error = verifyErr.Error()
				} else {
					report.Signer.Verified = true
				}
			}
		}
	}

	report.ClaudeHooks = inspectClaudeHooks(repoRoot, claudeDirFlag)
	report.LaunchAgent = inspectLaunchAgent(repoRoot)
	report.Daemon = inspectDaemon(report.SocketPath)
	report.DaemonMode = deriveOperatorDaemonMode(report.Daemon, report.LaunchAgent)

	if live {
		liveStatus := collectLiveOperatorStatus(report.SocketPath)
		report.Live = &liveStatus
		if strings.TrimSpace(liveStatus.Error) != "" {
			report.Daemon.Healthy = false
			if report.Daemon.Error == "" {
				report.Daemon.Error = liveStatus.Error
			}
		}
	}

	report.OK = report.Policy.Loaded &&
		report.Signer.Verified &&
		report.ClaudeHooks.Installed &&
		report.Daemon.Healthy
	return report, nil
}

func inspectClaudeHooks(repoRoot string, claudeDir string) operatorHooksStatus {
	status := operatorHooksStatus{
		ClaudeDir: filepath.Clean(claudeDir),
		State:     "missing",
	}
	settingsPaths := collectClaudeSettingsPaths(repoRoot, claudeDir)
	for _, settingsPath := range settingsPaths {
		settingsConfig, err := loadClaudeSettings(settingsPath)
		if err != nil {
			status.Error = err.Error()
			return status
		}
		if loopgateEventCount := countLoopgateConfiguredHookEvents(settingsConfig); loopgateEventCount > 0 {
			status.SettingsPaths = append(status.SettingsPaths, settingsPath)
			status.ManagedEventCount += loopgateEventCount
		}
	}

	claudeHooksDir := filepath.Join(filepath.Clean(claudeDir), claudeHooksDirname)
	for _, scriptName := range loopgateHookBundleFiles {
		scriptPath := filepath.Join(claudeHooksDir, scriptName)
		if _, err := os.Stat(scriptPath); err == nil {
			status.CopiedScriptCount++
		}
	}

	switch {
	case status.ManagedEventCount >= len(loopgateHookEvents) && status.CopiedScriptCount == len(loopgateHookBundleFiles):
		status.State = "installed"
	case status.ManagedEventCount > 0 || status.CopiedScriptCount > 0:
		status.State = "partial"
	default:
		status.State = "missing"
	}
	status.Installed = status.ManagedEventCount >= len(loopgateHookEvents) && status.CopiedScriptCount == len(loopgateHookBundleFiles)
	return status
}

func countLoopgateConfiguredHookEvents(settingsConfig claudeSettings) int {
	count := 0
	for _, hookSpec := range loopgateHookEvents {
		matcherGroups := settingsConfig.Hooks[hookSpec.EventName]
		for _, matcherGroup := range matcherGroups {
			for _, hookAction := range matcherGroup.Hooks {
				if isLoopgateHookCommand(hookAction.Command) {
					count++
					goto nextEvent
				}
			}
		}
	nextEvent:
	}
	return count
}

func inspectLaunchAgent(repoRoot string) operatorLaunchAgentStatus {
	status := operatorLaunchAgentStatus{
		Supported: runtimeGOOS() == "darwin",
	}
	if !status.Supported {
		return status
	}

	deps := defaultLaunchAgentDependencies()
	launchAgentsDir, err := resolveLaunchAgentsDir("", deps)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	label := defaultLoopgateLaunchAgentLabel(repoRoot)
	status.Label = label
	status.PlistPath = filepath.Join(launchAgentsDir, label+".plist")
	if _, err := os.Stat(status.PlistPath); err == nil {
		status.Installed = true
	} else if !os.IsNotExist(err) {
		status.Error = fmt.Sprintf("stat launch agent plist: %v", err)
		return status
	}

	serviceTarget := fmt.Sprintf("gui/%d/%s", deps.UserUID, label)
	command := execCommand("launchctl", "print", serviceTarget)
	if err := command.Run(); err == nil {
		status.Loaded = true
	} else {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			status.Error = fmt.Sprintf("check launch agent state: %v", err)
		}
	}
	return status
}

func inspectDaemon(socketPath string) operatorDaemonStatus {
	status := operatorDaemonStatus{}
	if _, err := os.Stat(socketPath); err == nil {
		status.SocketExists = true
	} else if os.IsNotExist(err) {
		status.Error = "socket not present"
		return status
	} else {
		status.Error = fmt.Sprintf("stat socket: %v", err)
		return status
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultOperatorCommandTimeout)
	defer cancel()

	client := loopgate.NewClient(socketPath)
	defer client.CloseIdleConnections()

	healthResponse, err := client.Health(ctx)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.Healthy = healthResponse.OK
	status.Version = healthResponse.Version
	if !healthResponse.OK {
		status.Error = "daemon reported unhealthy"
	}
	return status
}

func collectLiveOperatorStatus(socketPath string) operatorLiveStatus {
	liveStatus := operatorLiveStatus{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := loopgate.NewClient(socketPath)
	defer client.CloseIdleConnections()
	client.ConfigureSession("loopgate-status", defaultCommandSessionID("status"), []string{"ui.read"})
	defer func() {
		_ = client.CloseSession(context.Background())
	}()

	uiStatus, err := client.UIStatus(ctx)
	if err != nil {
		liveStatus.Error = err.Error()
		return liveStatus
	}
	recentEventsResponse, err := client.UIRecentEvents(ctx, "")
	if err != nil {
		liveStatus.Error = err.Error()
		return liveStatus
	}

	liveStatus.ControlSessionID = uiStatus.ControlSessionID
	liveStatus.PersonaName = uiStatus.PersonaName
	liveStatus.PersonaVersion = uiStatus.PersonaVersion
	liveStatus.RuntimeSessionID = uiStatus.RuntimeSessionID
	liveStatus.TurnCount = uiStatus.TurnCount
	liveStatus.PendingApprovals = uiStatus.PendingApprovals
	liveStatus.CapabilityCount = uiStatus.CapabilityCount
	liveStatus.ConnectionCount = uiStatus.ConnectionCount
	liveStatus.Policy = uiStatus.Policy
	liveStatus.RecentEvents = summarizeRecentEvents(recentEventsResponse.Events)
	return liveStatus
}

func summarizeRecentEvents(events []controlapipkg.UIEventEnvelope) []operatorRecentEvent {
	summaries := make([]operatorRecentEvent, 0, len(events))
	for _, eventEnvelope := range events {
		summary := operatorRecentEvent{
			ID:   eventEnvelope.ID,
			Type: eventEnvelope.Type,
			TS:   eventEnvelope.TS,
		}
		if dataMap, ok := eventEnvelope.Data.(map[string]interface{}); ok {
			summary.RequestID = stringInterfaceValue(dataMap, "request_id")
			summary.Capability = stringInterfaceValue(dataMap, "capability")
			summary.DenialCode = stringInterfaceValue(dataMap, "denial_code")
			summary.Message = stringInterfaceValue(dataMap, "message")
			summary.ApprovalRequestID = stringInterfaceValue(dataMap, "approval_request_id")
		}
		summaries = append(summaries, summary)
	}
	return summaries
}

func stringInterfaceValue(values map[string]interface{}, key string) string {
	if values == nil {
		return ""
	}
	rawValue, found := values[key]
	if !found {
		return ""
	}
	value, ok := rawValue.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func printOperatorStatusReport(output io.Writer, report operatorStatusReport) {
	statusLabel := "attention"
	if report.OK {
		statusLabel = "ok"
	}
	fmt.Fprintf(output, "status: %s\n", statusLabel)
	fmt.Fprintf(output, "operator_mode: %s\n", report.OperatorMode)
	fmt.Fprintf(output, "daemon_mode: %s\n", report.DaemonMode)
	fmt.Fprintf(output, "repo_root: %s\n", report.RepoRoot)
	fmt.Fprintf(output, "policy_profile: %s\n", report.Policy.Profile)
	fmt.Fprintf(output, "policy_path: %s\n", report.PolicyPath)
	fmt.Fprintf(output, "signature_path: %s\n", report.SignaturePath)
	fmt.Fprintf(output, "audit_ledger_path: %s\n", report.AuditLedgerPath)
	fmt.Fprintf(output, "socket_path: %s\n", report.SocketPath)
	if report.Policy.ContentSHA256 != "" {
		fmt.Fprintf(output, "policy_sha256: %s\n", report.Policy.ContentSHA256)
	}
	if report.Policy.SignatureKeyID != "" {
		fmt.Fprintf(output, "policy_key_id: %s\n", report.Policy.SignatureKeyID)
	}
	fmt.Fprintf(output, "signer_verified: %t\n", report.Signer.Verified)
	if report.Signer.PrivateKeyPath != "" {
		fmt.Fprintf(output, "signer_key_path: %s\n", report.Signer.PrivateKeyPath)
	}
	fmt.Fprintf(output, "claude_hooks_state: %s\n", report.ClaudeHooks.State)
	fmt.Fprintf(output, "claude_hooks_installed: %t\n", report.ClaudeHooks.Installed)
	fmt.Fprintf(output, "claude_dir: %s\n", report.ClaudeHooks.ClaudeDir)
	if len(report.ClaudeHooks.SettingsPaths) > 0 {
		fmt.Fprintf(output, "claude_settings_paths: %s\n", strings.Join(report.ClaudeHooks.SettingsPaths, ", "))
	}
	fmt.Fprintf(output, "daemon_healthy: %t\n", report.Daemon.Healthy)
	if report.Daemon.Version != "" {
		fmt.Fprintf(output, "daemon_version: %s\n", report.Daemon.Version)
	}
	if report.LaunchAgent.Supported {
		fmt.Fprintf(output, "launch_agent_installed: %t\n", report.LaunchAgent.Installed)
		fmt.Fprintf(output, "launch_agent_loaded: %t\n", report.LaunchAgent.Loaded)
		if report.LaunchAgent.PlistPath != "" {
			fmt.Fprintf(output, "launch_agent_plist: %s\n", report.LaunchAgent.PlistPath)
		}
	}
	for _, statusError := range []string{report.Policy.Error, report.Signer.Error, report.ClaudeHooks.Error, report.LaunchAgent.Error, report.Daemon.Error} {
		if strings.TrimSpace(statusError) != "" {
			fmt.Fprintf(output, "warning: %s\n", statusError)
		}
	}
	if report.Live != nil {
		if strings.TrimSpace(report.Live.Error) != "" {
			fmt.Fprintln(output, "live_status: unavailable")
			fmt.Fprintf(output, "warning: %s\n", report.Live.Error)
		} else {
			fmt.Fprintf(output, "live_pending_approvals: %d\n", report.Live.PendingApprovals)
			fmt.Fprintf(output, "live_capability_count: %d\n", report.Live.CapabilityCount)
			fmt.Fprintf(output, "live_connection_count: %d\n", report.Live.ConnectionCount)
			if len(report.Live.RecentEvents) > 0 {
				fmt.Fprintln(output, "recent_events:")
				for _, recentEvent := range report.Live.RecentEvents {
					fmt.Fprintf(output, "  - %s %s", recentEvent.ID, recentEvent.Type)
					if recentEvent.Capability != "" {
						fmt.Fprintf(output, " capability=%s", recentEvent.Capability)
					}
					if recentEvent.RequestID != "" {
						fmt.Fprintf(output, " request_id=%s", recentEvent.RequestID)
					}
					if recentEvent.ApprovalRequestID != "" {
						fmt.Fprintf(output, " approval_request_id=%s", recentEvent.ApprovalRequestID)
					}
					if recentEvent.DenialCode != "" {
						fmt.Fprintf(output, " denial_code=%s", recentEvent.DenialCode)
					}
					if recentEvent.Message != "" {
						fmt.Fprintf(output, " message=%q", recentEvent.Message)
					}
					fmt.Fprintln(output)
				}
			}
		}
	}
	nextSteps := operatorStatusNextSteps(report)
	if len(nextSteps) > 0 {
		fmt.Fprintln(output, "next_steps:")
		for _, nextStep := range nextSteps {
			fmt.Fprintf(output, "  - %s\n", nextStep)
		}
	}
}

func operatorStatusNextSteps(report operatorStatusReport) []string {
	nextSteps := make([]string, 0, 4)
	loopgateCmd := operatorCommandPath(report.RepoRoot, "loopgate")
	policyAdminCmd := operatorCommandPath(report.RepoRoot, "loopgate-policy-admin")
	policySignCmd := operatorCommandPath(report.RepoRoot, "loopgate-policy-sign")
	doctorCmd := operatorCommandPath(report.RepoRoot, "loopgate-doctor")
	if !report.Policy.Loaded {
		nextSteps = append(nextSteps, fmt.Sprintf("validate the signed policy with %s validate", policyAdminCmd))
	}
	if !report.Signer.Verified {
		nextSteps = append(nextSteps, fmt.Sprintf("repair signer setup with %s init or %s -verify-setup", loopgateCmd, policySignCmd))
	}
	if report.ClaudeHooks.State == "partial" {
		nextSteps = append(nextSteps, fmt.Sprintf("repair Claude Code hooks with %s install-hooks, or remove stale entries with %s remove-hooks", loopgateCmd, loopgateCmd))
	} else if !report.ClaudeHooks.Installed {
		nextSteps = append(nextSteps, fmt.Sprintf("install Claude Code hooks with %s install-hooks", loopgateCmd))
	}
	if !report.Daemon.Healthy {
		if report.LaunchAgent.Supported {
			switch {
			case report.LaunchAgent.Installed && !report.LaunchAgent.Loaded:
				nextSteps = append(nextSteps, fmt.Sprintf("load the existing LaunchAgent with %s install-launch-agent -load, or start Loopgate once with %s", loopgateCmd, loopgateCmd))
			case report.LaunchAgent.Installed && report.LaunchAgent.Loaded:
				nextSteps = append(nextSteps, fmt.Sprintf("restart the loaded LaunchAgent with %s install-launch-agent -load, or start Loopgate once with %s for foreground diagnostics", loopgateCmd, loopgateCmd))
			default:
				nextSteps = append(nextSteps, fmt.Sprintf("start Loopgate with %s, or install a background LaunchAgent with %s install-launch-agent -load", loopgateCmd, loopgateCmd))
			}
		} else {
			nextSteps = append(nextSteps, fmt.Sprintf("start Loopgate with %s", loopgateCmd))
		}
	}
	if len(nextSteps) > 0 {
		nextSteps = append(nextSteps, fmt.Sprintf("rerun %s status and %s test after the missing pieces are in place", loopgateCmd, loopgateCmd))
		nextSteps = append(nextSteps, fmt.Sprintf("run %s report if you need deeper diagnostics from derived local state", doctorCmd))
	}
	return nextSteps
}

func deriveOperatorDaemonMode(daemon operatorDaemonStatus, launchAgent operatorLaunchAgentStatus) string {
	if !daemon.Healthy {
		return "offline"
	}
	if launchAgent.Supported && launchAgent.Loaded {
		return "launch-agent-managed"
	}
	return "foreground-or-manual"
}

func defaultCommandSessionID(prefix string) string {
	return "loopgate-" + strings.TrimSpace(prefix) + "-" + fmt.Sprintf("%d", os.Getpid())
}

func runtimeGOOS() string {
	return runtime.GOOS
}

type temporaryLoopgateHandle struct {
	source   string
	shutdown func() error
}

func (handle temporaryLoopgateHandle) Shutdown() error {
	if handle.shutdown == nil {
		return nil
	}
	return handle.shutdown()
}

func startTemporaryLoopgate(repoRoot string, socketPath string) (temporaryLoopgateHandle, error) {
	deps := defaultLaunchAgentDependencies()
	binaryPath, err := resolveLoopgateExecutablePath("", deps)
	if err != nil {
		return temporaryLoopgateHandle{}, err
	}

	command := execCommandContext(context.Background(), binaryPath)
	command.Dir = repoRoot
	command.Env = append(os.Environ(),
		loopgateRepoRootEnv+"="+repoRoot,
		"LOOPGATE_SOCKET="+socketPath,
	)
	var stdoutBuffer strings.Builder
	var stderrBuffer strings.Builder
	command.Stdout = &stdoutBuffer
	command.Stderr = &stderrBuffer
	if err := command.Start(); err != nil {
		return temporaryLoopgateHandle{}, fmt.Errorf("start temporary loopgate: %w", err)
	}

	if err := waitForHealthyLoopgate(socketPath, 5*time.Second); err != nil {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
		trimmedStderr := strings.TrimSpace(stderrBuffer.String())
		if trimmedStderr != "" {
			return temporaryLoopgateHandle{}, fmt.Errorf("wait for temporary loopgate health: %w (%s)", err, trimmedStderr)
		}
		return temporaryLoopgateHandle{}, err
	}

	return temporaryLoopgateHandle{
		source: "spawned",
		shutdown: func() error {
			if command.Process == nil {
				return nil
			}
			_ = command.Process.Signal(os.Interrupt)
			done := make(chan error, 1)
			go func() {
				_, waitErr := command.Process.Wait()
				done <- waitErr
			}()
			select {
			case waitErr := <-done:
				if waitErr != nil && !isExpectedProcessExit(waitErr) {
					return waitErr
				}
				return nil
			case <-time.After(3 * time.Second):
				_ = command.Process.Kill()
				_, _ = command.Process.Wait()
				return nil
			}
		},
	}, nil
}

func waitForHealthyLoopgate(socketPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if inspectDaemon(socketPath).Healthy {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for loopgate health at %s", socketPath)
}

func isExpectedProcessExit(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
		return status.Signaled()
	}
	return exitErr.ExitCode() >= 0
}
