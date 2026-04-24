// Command loopgate-doctor produces operator diagnostic bundles, offline JSON reports,
// and live trust preflight checks for local troubleshooting.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"loopgate/internal/config"
	"loopgate/internal/loopgate"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"loopgate/internal/troubleshoot"
)

var checkLoopgateHealth = func(socketPath string) (controlapipkg.HealthResponse, error) {
	client := loopgate.NewClient(socketPath)
	return client.Health(context.Background())
}

var checkAuditExportTrust = func(socketPath string) (controlapipkg.AuditExportTrustCheckResponse, error) {
	client := loopgate.NewClient(socketPath)
	client.ConfigureSession("loopgate-doctor", defaultDoctorSessionID("trust-check"), []string{"diagnostic.read"})
	return client.CheckAuditExportTrust(context.Background())
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) < 1 {
		printUsage(stderr)
		return 2
	}
	switch args[0] {
	case "setup-check":
		return runSetupCheck(args[1:], stdout, stderr)
	case "report":
		return runReport(args[1:], stdout, stderr)
	case "bundle":
		return runBundle(args[1:], stdout, stderr)
	case "explain-denial":
		return runExplainDenial(args[1:], stdout, stderr)
	case "trust-check":
		return runTrustCheck(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stderr)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown subcommand %q\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintf(w, `Usage:
  loopgate-doctor setup-check [-repo DIR] [-socket PATH] [-claude-dir DIR] [-json]  Print setup readiness
  loopgate-doctor report      [-repo DIR]                        Print offline JSON diagnostic report to stdout
  loopgate-doctor bundle      [-repo DIR] -out DIR [-log-lines N]   Write report.json + diagnostic log tails
  loopgate-doctor explain-denial [-repo DIR] (-approval-id ID | -request-id ID | -hook-session-id ID [-tool-use-id ID] [-hook-event-name NAME])    Explain one approval, request, or hook block outcome from the verified audit ledger
  loopgate-doctor trust-check [-repo DIR] [-socket PATH]            Query the running local Loopgate audit-export trust preflight

-repo defaults to the current working directory.
trust-check defaults to LOOPGATE_SOCKET or <repo>/runtime/state/loopgate.sock.
Effective runtime config for offline report/bundle matches Loopgate: config/runtime.yaml (plus optional diagnostic override JSON).
`)
}

type setupCheckReport struct {
	RepoRoot          string                           `json:"repo_root"`
	SocketPath        string                           `json:"socket_path"`
	Policy            setupCheckPolicyStatus           `json:"policy"`
	OperatorOverrides setupCheckOperatorOverrideStatus `json:"operator_overrides"`
	Daemon            setupCheckDaemonStatus           `json:"daemon"`
	ClaudeHooks       setupCheckClaudeHooksStatus      `json:"claude_hooks"`
	SampleDecisions   []setupCheckSampleDecision       `json:"sample_decisions"`
	NextSteps         []string                         `json:"next_steps,omitempty"`
}

type setupCheckPolicyStatus struct {
	Loaded         bool   `json:"loaded"`
	Profile        string `json:"profile"`
	ContentSHA256  string `json:"content_sha256,omitempty"`
	SignatureKeyID string `json:"signature_key_id,omitempty"`
	Error          string `json:"error,omitempty"`
}

type setupCheckOperatorOverrideStatus struct {
	Present          bool   `json:"present"`
	ActiveGrantCount int    `json:"active_grant_count"`
	ContentSHA256    string `json:"content_sha256,omitempty"`
	SignatureKeyID   string `json:"signature_key_id,omitempty"`
	Error            string `json:"error,omitempty"`
}

type setupCheckDaemonStatus struct {
	Healthy bool   `json:"healthy"`
	Version string `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
}

type setupCheckClaudeHooksStatus struct {
	ClaudeDir         string   `json:"claude_dir"`
	State             string   `json:"state"`
	Installed         bool     `json:"installed"`
	SettingsPaths     []string `json:"settings_paths,omitempty"`
	ConfiguredEntries int      `json:"configured_entries"`
	CopiedScriptCount int      `json:"copied_script_count"`
	Error             string   `json:"error,omitempty"`
}

type setupCheckSampleDecision struct {
	Label           string   `json:"label"`
	Decision        string   `json:"decision,omitempty"`
	ReasonCode      string   `json:"reason_code,omitempty"`
	DenialCode      string   `json:"denial_code,omitempty"`
	ApprovalOwner   string   `json:"approval_owner,omitempty"`
	ApprovalOptions []string `json:"approval_options,omitempty"`
	Reason          string   `json:"reason,omitempty"`
	Error           string   `json:"error,omitempty"`
}

func runSetupCheck(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("setup-check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoFn := parseRepoFlag(fs)
	socketPathFlag := fs.String("socket", "", "Unix socket path (default: LOOPGATE_SOCKET or <repo>/runtime/state/loopgate.sock)")
	claudeDirFlag := fs.String("claude-dir", "", "Claude config directory (default: ~/.claude)")
	jsonFlag := fs.Bool("json", false, "print machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	repoRoot, err := repoFn()
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}
	report := collectSetupCheckReport(repoRoot, resolveSocketPath(repoRoot, *socketPathFlag), resolveClaudeDir(*claudeDirFlag))
	if *jsonFlag {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintln(stderr, "ERROR: encode setup check:", err)
			return 1
		}
		return 0
	}
	writeSetupCheckReport(stdout, report)
	return 0
}

func collectSetupCheckReport(repoRoot string, socketPath string, claudeDir string) setupCheckReport {
	report := setupCheckReport{
		RepoRoot:   filepath.Clean(repoRoot),
		SocketPath: filepath.Clean(socketPath),
		Policy: setupCheckPolicyStatus{
			Profile: "unknown",
		},
		ClaudeHooks: inspectSetupClaudeHooks(repoRoot, claudeDir),
	}

	if policyLoadResult, err := config.LoadPolicyWithHash(repoRoot); err != nil {
		report.Policy.Error = err.Error()
	} else {
		report.Policy.Loaded = true
		report.Policy.ContentSHA256 = policyLoadResult.ContentSHA256
		report.Policy.Profile = config.DetectSetupPolicyTemplatePresetName(policyLoadResult.Policy)
	}
	if signatureFile, err := config.LoadPolicySignatureFile(repoRoot); err != nil {
		if report.Policy.Error == "" {
			report.Policy.Error = err.Error()
		}
	} else {
		report.Policy.SignatureKeyID = signatureFile.KeyID
	}

	if overrideLoadResult, err := config.LoadOperatorOverrideDocumentWithHash(repoRoot); err != nil {
		report.OperatorOverrides.Error = err.Error()
	} else {
		report.OperatorOverrides.Present = overrideLoadResult.Present
		report.OperatorOverrides.ContentSHA256 = overrideLoadResult.ContentSHA256
		report.OperatorOverrides.SignatureKeyID = overrideLoadResult.SignatureKeyID
		report.OperatorOverrides.ActiveGrantCount = countActiveOperatorOverrideGrants(overrideLoadResult.Document.Grants)
	}

	if healthResponse, err := checkLoopgateHealth(report.SocketPath); err != nil {
		report.Daemon.Error = err.Error()
	} else {
		report.Daemon.Healthy = healthResponse.OK
		report.Daemon.Version = healthResponse.Version
	}

	report.SampleDecisions = collectSetupSampleDecisions(repoRoot)
	report.NextSteps = setupCheckNextSteps(report)
	return report
}

func countActiveOperatorOverrideGrants(grants []config.OperatorOverrideGrant) int {
	count := 0
	for _, grant := range grants {
		if strings.TrimSpace(grant.State) == "active" {
			count++
		}
	}
	return count
}

func collectSetupSampleDecisions(repoRoot string) []setupCheckSampleDecision {
	samples := []struct {
		label string
		req   controlapipkg.HookPreValidateRequest
	}{
		{
			label: "repo search",
			req: controlapipkg.HookPreValidateRequest{
				HookEventName: "PreToolUse",
				ToolName:      "Grep",
				ToolInput: map[string]interface{}{
					"pattern": "Loopgate",
					"path":    repoRoot,
				},
				CWD: repoRoot,
			},
		},
		{
			label: "repo write",
			req: controlapipkg.HookPreValidateRequest{
				HookEventName: "PreToolUse",
				ToolName:      "Write",
				ToolInput: map[string]interface{}{
					"file_path": filepath.Join(repoRoot, "README.md"),
					"content":   "diagnostic probe",
				},
				CWD: repoRoot,
			},
		},
		{
			label: "shell command",
			req: controlapipkg.HookPreValidateRequest{
				HookEventName: "PreToolUse",
				ToolName:      "Bash",
				ToolInput: map[string]interface{}{
					"command": "git status --short",
				},
				CWD: repoRoot,
			},
		},
	}

	decisions := make([]setupCheckSampleDecision, 0, len(samples))
	for _, sample := range samples {
		response, err := loopgate.ExplainClaudeCodeHookDecision(repoRoot, sample.req)
		decision := setupCheckSampleDecision{Label: sample.label}
		if err != nil {
			decision.Error = err.Error()
		} else {
			decision.Decision = response.Decision
			decision.ReasonCode = response.ReasonCode
			decision.DenialCode = response.DenialCode
			decision.ApprovalOwner = response.ApprovalOwner
			decision.ApprovalOptions = append([]string(nil), response.ApprovalOptions...)
			decision.Reason = response.Reason
		}
		decisions = append(decisions, decision)
	}
	return decisions
}

func writeSetupCheckReport(output io.Writer, report setupCheckReport) {
	fmt.Fprintln(output, "Loopgate setup check")
	fmt.Fprintf(output, "repo: %s\n", report.RepoRoot)
	fmt.Fprintf(output, "socket: %s\n", report.SocketPath)
	fmt.Fprintf(output, "policy: %s\n", formatSetupPolicyStatus(report.Policy))
	fmt.Fprintf(output, "operator_overrides: %s\n", formatSetupOperatorOverrideStatus(report.OperatorOverrides))
	fmt.Fprintf(output, "daemon: %s\n", formatSetupDaemonStatus(report.Daemon))
	fmt.Fprintf(output, "claude_hooks: %s\n", formatSetupClaudeHooksStatus(report.ClaudeHooks))
	if len(report.ClaudeHooks.SettingsPaths) > 0 {
		fmt.Fprintf(output, "claude_settings: %s\n", strings.Join(report.ClaudeHooks.SettingsPaths, ", "))
	}
	fmt.Fprintln(output, "sample_decisions:")
	for _, decision := range report.SampleDecisions {
		fmt.Fprintf(output, "  - %s: %s\n", decision.Label, formatSetupSampleDecision(decision))
	}
	if len(report.NextSteps) > 0 {
		fmt.Fprintln(output, "next_steps:")
		for _, nextStep := range report.NextSteps {
			fmt.Fprintf(output, "  - %s\n", nextStep)
		}
	}
}

func formatSetupPolicyStatus(status setupCheckPolicyStatus) string {
	if status.Error != "" {
		return "error: " + status.Error
	}
	if !status.Loaded {
		return "missing"
	}
	parts := []string{"ok", "profile=" + status.Profile}
	if status.SignatureKeyID != "" {
		parts = append(parts, "key_id="+status.SignatureKeyID)
	}
	if status.ContentSHA256 != "" {
		parts = append(parts, "sha256="+shortSHA256(status.ContentSHA256))
	}
	return strings.Join(parts, " ")
}

func formatSetupOperatorOverrideStatus(status setupCheckOperatorOverrideStatus) string {
	if status.Error != "" {
		return "error: " + status.Error
	}
	if !status.Present {
		return "not present"
	}
	parts := []string{"ok", fmt.Sprintf("active_grants=%d", status.ActiveGrantCount)}
	if status.SignatureKeyID != "" {
		parts = append(parts, "key_id="+status.SignatureKeyID)
	}
	if status.ContentSHA256 != "" {
		parts = append(parts, "sha256="+shortSHA256(status.ContentSHA256))
	}
	return strings.Join(parts, " ")
}

func formatSetupDaemonStatus(status setupCheckDaemonStatus) string {
	if status.Error != "" {
		return "offline: " + status.Error
	}
	if !status.Healthy {
		return "unhealthy"
	}
	if status.Version != "" {
		return "healthy version=" + status.Version
	}
	return "healthy"
}

func formatSetupClaudeHooksStatus(status setupCheckClaudeHooksStatus) string {
	if status.Error != "" {
		return "error: " + status.Error
	}
	return fmt.Sprintf("%s installed=%t configured_entries=%d copied_scripts=%d claude_dir=%s", status.State, status.Installed, status.ConfiguredEntries, status.CopiedScriptCount, status.ClaudeDir)
}

func formatSetupSampleDecision(decision setupCheckSampleDecision) string {
	if decision.Error != "" {
		return "error: " + decision.Error
	}
	parts := []string{decision.Decision}
	if decision.ReasonCode != "" {
		parts = append(parts, "reason_code="+decision.ReasonCode)
	}
	if decision.DenialCode != "" {
		parts = append(parts, "denial_code="+decision.DenialCode)
	}
	if decision.ApprovalOwner != "" {
		parts = append(parts, "approval_owner="+decision.ApprovalOwner)
	}
	if len(decision.ApprovalOptions) > 0 {
		parts = append(parts, "approval_options="+strings.Join(decision.ApprovalOptions, ","))
	}
	if decision.Reason != "" {
		parts = append(parts, "reason="+strconv.Quote(decision.Reason))
	}
	return strings.Join(parts, " ")
}

func setupCheckNextSteps(report setupCheckReport) []string {
	nextSteps := []string{}
	if report.Policy.Error != "" || !report.Policy.Loaded {
		nextSteps = append(nextSteps, "create and sign a root policy with loopgate setup or loopgate-policy-admin")
	}
	if report.ClaudeHooks.State == "partial" {
		nextSteps = append(nextSteps, "repair Claude Code hooks with loopgate install-hooks or remove stale entries with loopgate remove-hooks")
	} else if !report.ClaudeHooks.Installed {
		nextSteps = append(nextSteps, "install Claude Code hooks with loopgate install-hooks")
	}
	if !report.Daemon.Healthy {
		nextSteps = append(nextSteps, "start Loopgate before relying on live hook enforcement")
	}
	if len(report.SampleDecisions) == 0 {
		return nextSteps
	}
	for _, decision := range report.SampleDecisions {
		if decision.Error != "" {
			nextSteps = append(nextSteps, "rerun after policy errors are fixed so sample decisions can be evaluated")
			break
		}
	}
	return nextSteps
}

func resolveClaudeDir(claudeDirFlag string) string {
	if trimmedClaudeDir := strings.TrimSpace(claudeDirFlag); trimmedClaudeDir != "" {
		return filepath.Clean(trimmedClaudeDir)
	}
	homeDir, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(homeDir) == "" {
		return filepath.Clean(".claude")
	}
	return filepath.Join(homeDir, ".claude")
}

var setupCheckLoopgateHookScripts = []string{
	"loopgate_hook_common.py",
	"loopgate_pretool.py",
	"loopgate_posttool.py",
	"loopgate_posttoolfailure.py",
	"loopgate_sessionstart.py",
	"loopgate_sessionend.py",
	"loopgate_userpromptsubmit.py",
	"loopgate_permissionrequest.py",
}

func inspectSetupClaudeHooks(repoRoot string, claudeDir string) setupCheckClaudeHooksStatus {
	status := setupCheckClaudeHooksStatus{
		ClaudeDir: filepath.Clean(claudeDir),
		State:     "missing",
	}
	for _, settingsPath := range []string{
		filepath.Join(repoRoot, ".claude", "settings.json"),
		filepath.Join(status.ClaudeDir, "settings.json"),
	} {
		rawSettings, err := os.ReadFile(settingsPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			status.Error = fmt.Sprintf("read %s: %v", settingsPath, err)
			return status
		}
		configuredEntries := countConfiguredSetupHookScripts(string(rawSettings))
		if configuredEntries > 0 {
			status.SettingsPaths = append(status.SettingsPaths, settingsPath)
			status.ConfiguredEntries += configuredEntries
		}
	}

	hooksDir := filepath.Join(status.ClaudeDir, "hooks")
	for _, scriptName := range setupCheckLoopgateHookScripts {
		if _, err := os.Stat(filepath.Join(hooksDir, scriptName)); err == nil {
			status.CopiedScriptCount++
		} else if err != nil && !os.IsNotExist(err) {
			status.Error = fmt.Sprintf("stat hook script %s: %v", scriptName, err)
			return status
		}
	}

	switch {
	case status.ConfiguredEntries >= len(setupCheckRequiredHookScripts()) && status.CopiedScriptCount == len(setupCheckLoopgateHookScripts):
		status.State = "installed"
	case status.ConfiguredEntries > 0 || status.CopiedScriptCount > 0:
		status.State = "partial"
	default:
		status.State = "missing"
	}
	status.Installed = status.State == "installed"
	return status
}

func setupCheckRequiredHookScripts() []string {
	return setupCheckLoopgateHookScripts[1:]
}

func countConfiguredSetupHookScripts(rawSettings string) int {
	count := 0
	for _, scriptName := range setupCheckRequiredHookScripts() {
		if strings.Contains(rawSettings, scriptName) {
			count++
		}
	}
	return count
}

func shortSHA256(value string) string {
	trimmedValue := strings.TrimSpace(value)
	if len(trimmedValue) <= 12 {
		return trimmedValue
	}
	return trimmedValue[:12]
}

func parseRepoFlag(fs *flag.FlagSet) func() (string, error) {
	repo := fs.String("repo", "", "repository root (default: cwd)")
	return func() (string, error) {
		if s := strings.TrimSpace(*repo); s != "" {
			return filepath.Clean(s), nil
		}
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return cwd, nil
	}
}

func runReport(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoFn := parseRepoFlag(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	repoRoot, err := repoFn()
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}
	runtimeConfig, err := troubleshoot.LoadEffectiveRuntimeConfig(repoRoot)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: load runtime config:", err)
		return 1
	}
	report, err := troubleshoot.BuildReport(repoRoot, runtimeConfig)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: build report:", err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintln(stderr, "ERROR: encode:", err)
		return 1
	}
	return 0
}

func runBundle(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("bundle", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoFn := parseRepoFlag(fs)
	outDirFlag := fs.String("out", "", "output directory (required)")
	logLinesFlag := fs.Int("log-lines", 200, "max lines to copy per diagnostic log file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*outDirFlag) == "" {
		fmt.Fprintln(stderr, "ERROR: -out is required")
		return 2
	}
	repoRoot, err := repoFn()
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}
	runtimeConfig, err := troubleshoot.LoadEffectiveRuntimeConfig(repoRoot)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: load runtime config:", err)
		return 1
	}
	outDir := filepath.Clean(*outDirFlag)
	if err := troubleshoot.WriteOperatorBundle(repoRoot, runtimeConfig, outDir, *logLinesFlag); err != nil {
		fmt.Fprintln(stderr, "ERROR: write bundle:", err)
		return 1
	}
	fmt.Fprintln(stdout, "wrote bundle to", outDir)
	return 0
}

func runExplainDenial(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("explain-denial", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoFn := parseRepoFlag(fs)
	approvalIDFlag := fs.String("approval-id", "", "approval request id to explain")
	requestIDFlag := fs.String("request-id", "", "capability request id to explain")
	hookSessionIDFlag := fs.String("hook-session-id", "", "Claude hook session id to explain")
	toolUseIDFlag := fs.String("tool-use-id", "", "Claude tool use id filter for hook explanation")
	hookEventNameFlag := fs.String("hook-event-name", "", "Claude hook event name filter for hook explanation")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	trimmedApprovalID := strings.TrimSpace(*approvalIDFlag)
	trimmedRequestID := strings.TrimSpace(*requestIDFlag)
	trimmedHookSessionID := strings.TrimSpace(*hookSessionIDFlag)
	trimmedToolUseID := strings.TrimSpace(*toolUseIDFlag)
	trimmedHookEventName := strings.TrimSpace(*hookEventNameFlag)
	if trimmedHookSessionID == "" && (trimmedToolUseID != "" || trimmedHookEventName != "") {
		fmt.Fprintln(stderr, "ERROR: -tool-use-id and -hook-event-name require -hook-session-id")
		return 2
	}
	selectedPrimaryFlags := 0
	if trimmedApprovalID != "" {
		selectedPrimaryFlags++
	}
	if trimmedRequestID != "" {
		selectedPrimaryFlags++
	}
	if trimmedHookSessionID != "" {
		selectedPrimaryFlags++
	}
	if selectedPrimaryFlags != 1 {
		fmt.Fprintln(stderr, "ERROR: exactly one of -approval-id, -request-id, or -hook-session-id is required")
		return 2
	}
	repoRoot, err := repoFn()
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}
	runtimeConfig, err := troubleshoot.LoadEffectiveRuntimeConfig(repoRoot)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: load runtime config:", err)
		return 1
	}

	if trimmedApprovalID != "" {
		explanation, err := troubleshoot.ExplainApprovalRequest(repoRoot, runtimeConfig, trimmedApprovalID)
		if err != nil {
			if errors.Is(err, troubleshoot.ErrApprovalRequestNotFound) {
				fmt.Fprintln(stderr, "ERROR:", err)
				return 1
			}
			fmt.Fprintln(stderr, "ERROR: explain approval denial:", err)
			return 1
		}
		if err := troubleshoot.WriteApprovalExplanation(stdout, explanation); err != nil {
			fmt.Fprintln(stderr, "ERROR: write explanation:", err)
			return 1
		}
		return 0
	}
	if trimmedHookSessionID != "" {
		explanation, err := troubleshoot.ExplainHookBlock(repoRoot, runtimeConfig, troubleshoot.HookBlockQuery{
			SessionID:     trimmedHookSessionID,
			ToolUseID:     trimmedToolUseID,
			HookEventName: trimmedHookEventName,
		})
		if err != nil {
			if errors.Is(err, troubleshoot.ErrHookBlockNotFound) {
				fmt.Fprintln(stderr, "ERROR:", err)
				return 1
			}
			fmt.Fprintln(stderr, "ERROR: explain hook block:", err)
			return 1
		}
		if err := troubleshoot.WriteHookBlockExplanation(stdout, explanation); err != nil {
			fmt.Fprintln(stderr, "ERROR: write explanation:", err)
			return 1
		}
		return 0
	}

	explanation, err := troubleshoot.ExplainCapabilityRequest(repoRoot, runtimeConfig, trimmedRequestID)
	if err != nil {
		if errors.Is(err, troubleshoot.ErrCapabilityRequestNotFound) {
			fmt.Fprintln(stderr, "ERROR:", err)
			return 1
		}
		fmt.Fprintln(stderr, "ERROR: explain request denial:", err)
		return 1
	}
	if err := troubleshoot.WriteCapabilityRequestExplanation(stdout, explanation); err != nil {
		fmt.Fprintln(stderr, "ERROR: write explanation:", err)
		return 1
	}
	return 0
}

func runTrustCheck(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("trust-check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoFn := parseRepoFlag(fs)
	socketPathFlag := fs.String("socket", "", "Unix socket path (default: LOOPGATE_SOCKET or <repo>/runtime/state/loopgate.sock)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	repoRoot, err := repoFn()
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}
	socketPath := resolveSocketPath(repoRoot, *socketPathFlag)
	trustCheckResponse, err := checkAuditExportTrust(socketPath)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: audit export trust check:", err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(trustCheckResponse); err != nil {
		fmt.Fprintln(stderr, "ERROR: encode:", err)
		return 1
	}
	return 0
}

func resolveSocketPath(repoRoot string, socketPathFlag string) string {
	if trimmedSocketPath := strings.TrimSpace(socketPathFlag); trimmedSocketPath != "" {
		return filepath.Clean(trimmedSocketPath)
	}
	if socketPathFromEnv := strings.TrimSpace(os.Getenv("LOOPGATE_SOCKET")); socketPathFromEnv != "" {
		return filepath.Clean(socketPathFromEnv)
	}
	return filepath.Join(repoRoot, "runtime", "state", "loopgate.sock")
}

func defaultDoctorSessionID(subcommandName string) string {
	trimmedSubcommandName := strings.TrimSpace(subcommandName)
	if trimmedSubcommandName == "" {
		trimmedSubcommandName = "doctor"
	}
	return "loopgate-doctor-" + trimmedSubcommandName + "-" + strconv.Itoa(os.Getpid())
}
