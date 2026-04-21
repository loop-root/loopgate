package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"loopgate/internal/config"
)

const defaultSetupPolicyProfile = "balanced"

type loopgateSetupPlan struct {
	RepoRoot           string
	KeyID              string
	ClaudeDir          string
	SelectedPreset     config.PolicyTemplatePreset
	InstallHooks       bool
	Python3Path        string
	InstallLaunchAgent bool
	LoadLaunchAgent    bool
}

func runSetup(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoRootFlag := fs.String("repo-root", "", "repository root containing Loopgate config and signed policy files")
	keyIDFlag := fs.String("key-id", "", "policy signing key identifier (default: local-operator-<short-hostname>)")
	profileFlag := fs.String("profile", "", "starter policy profile: balanced, strict, or read-only")
	installHooksFlag := fs.Bool("install-hooks", false, "install Claude Code hooks")
	skipHooksFlag := fs.Bool("skip-hooks", false, "skip Claude Code hook installation")
	claudeDirFlag := fs.String("claude-dir", "", "Claude config directory used with -install-hooks")
	installLaunchAgentFlag := fs.Bool("install-launch-agent", false, "install a macOS LaunchAgent so Loopgate can run in the background")
	skipLaunchAgentFlag := fs.Bool("skip-launch-agent", false, "skip macOS LaunchAgent installation")
	loadLaunchAgentFlag := fs.Bool("load-launch-agent", false, "load and start the macOS LaunchAgent after installing it")
	binaryPathFlag := fs.String("binary-path", "", "Loopgate server binary path used with -install-launch-agent")
	launchAgentsDirFlag := fs.String("launch-agents-dir", "", "LaunchAgents directory used with -install-launch-agent")
	yesFlag := fs.Bool("yes", false, "accept the recommended setup choices without prompting")
	if err := fs.Parse(args); err != nil {
		return normalizeFlagParseError(err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	if *installHooksFlag && *skipHooksFlag {
		return fmt.Errorf("-install-hooks and -skip-hooks cannot be used together")
	}
	if *installLaunchAgentFlag && *skipLaunchAgentFlag {
		return fmt.Errorf("-install-launch-agent and -skip-launch-agent cannot be used together")
	}
	if *skipLaunchAgentFlag && *loadLaunchAgentFlag {
		return fmt.Errorf("-skip-launch-agent and -load-launch-agent cannot be used together")
	}

	repoRoot, err := resolveLoopgateRepoRoot(strings.TrimSpace(*repoRootFlag))
	if err != nil {
		return err
	}
	keyID, err := resolveLoopgateInitKeyID(strings.TrimSpace(*keyIDFlag))
	if err != nil {
		return err
	}
	claudeDir := resolveSetupClaudeDir(strings.TrimSpace(*claudeDirFlag))

	interactive := readerIsInteractive(stdin)
	promptReader := bufio.NewReader(stdin)
	if interactive && !*yesFlag {
		printSetupIntro(stdout, repoRoot, keyID, claudeDir)
	}

	selectedPreset, err := resolveSetupPolicyPreset(strings.TrimSpace(*profileFlag), *yesFlag, interactive, promptReader, stdout)
	if err != nil {
		return err
	}
	installHooks, err := resolveSetupBooleanChoice("Claude Code hooks", *installHooksFlag, *skipHooksFlag, true, *yesFlag, interactive, promptReader, stdout, buildHookPromptText(*claudeDirFlag))
	if err != nil {
		return err
	}
	installLaunchAgentChoice, err := resolveLaunchAgentInstallChoice(*installLaunchAgentFlag || *loadLaunchAgentFlag, *skipLaunchAgentFlag, *yesFlag, interactive, promptReader, stdout)
	if err != nil {
		return err
	}
	loadLaunchAgent := *loadLaunchAgentFlag
	if installLaunchAgentChoice && !loadLaunchAgent {
		if *yesFlag {
			loadLaunchAgent = true
		} else if interactive {
			loadLaunchAgent, err = promptYesNo(promptReader, stdout, "Load and start the LaunchAgent now", true)
			if err != nil {
				return err
			}
		}
	}

	setupPlan := loopgateSetupPlan{
		RepoRoot:           repoRoot,
		KeyID:              keyID,
		ClaudeDir:          claudeDir,
		SelectedPreset:     selectedPreset,
		InstallHooks:       installHooks,
		InstallLaunchAgent: installLaunchAgentChoice,
		LoadLaunchAgent:    loadLaunchAgent,
	}
	python3Path, err := validateSetupPrerequisites(setupPlan)
	if err != nil {
		return err
	}
	setupPlan.Python3Path = python3Path
	if interactive && !*yesFlag {
		printSetupPlanSummary(stdout, setupPlan)
		proceed, err := promptYesNo(promptReader, stdout, "Proceed with setup", true)
		if err != nil {
			return err
		}
		if !proceed {
			return fmt.Errorf("setup canceled by operator")
		}
	}

	initResult, err := initializeLoopgatePolicySigning(repoRoot, keyID, false)
	if err != nil {
		return err
	}
	if err := writeAndSignPolicyPreset(repoRoot, selectedPreset, initResult.KeyID); err != nil {
		return err
	}

	if installHooks {
		hookArgs := []string{"-repo", repoRoot}
		if trimmedClaudeDir := strings.TrimSpace(*claudeDirFlag); trimmedClaudeDir != "" {
			hookArgs = append(hookArgs, "-claude-dir", trimmedClaudeDir)
		}
		if err := runInstallHooks(hookArgs, stdout); err != nil {
			return err
		}
	}

	var launchAgentResult launchAgentInstallResult
	if installLaunchAgentChoice {
		launchAgentResult, err = installLaunchAgent(launchAgentInstallOptions{
			RepoRoot:        repoRoot,
			BinaryPath:      strings.TrimSpace(*binaryPathFlag),
			LaunchAgentsDir: strings.TrimSpace(*launchAgentsDirFlag),
			LoadImmediately: loadLaunchAgent,
		}, defaultLaunchAgentDependencies())
		if err != nil {
			return err
		}
	}

	fmt.Fprintln(stdout, "setup OK")
	fmt.Fprintf(stdout, "operator_mode: %s\n", operatorMode(repoRoot))
	fmt.Fprintf(stdout, "profile: %s\n", selectedPreset.Name)
	if initResult.AlreadyInitialized {
		fmt.Fprintf(stdout, "policy_signing: reused local signer (%s)\n", initResult.KeyID)
	} else {
		fmt.Fprintf(stdout, "policy_signing: initialized local signer (%s)\n", initResult.KeyID)
	}
	fmt.Fprintf(stdout, "policy_path: %s\n", filepath.Join(repoRoot, "core", "policy", "policy.yaml"))
	fmt.Fprintf(stdout, "signature_path: %s\n", config.PolicySignaturePath(repoRoot))
	fmt.Fprintf(stdout, "socket_path: %s\n", initResult.SocketPath)
	fmt.Fprintf(stdout, "audit_ledger_path: %s\n", filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	fmt.Fprintf(stdout, "claude_hooks_installed: %t\n", installHooks)
	if installHooks {
		fmt.Fprintf(stdout, "claude_dir: %s\n", claudeDir)
		fmt.Fprintf(stdout, "python3_path: %s\n", setupPlan.Python3Path)
	} else {
		fmt.Fprintf(stdout, "claude_dir: %s\n", claudeDir)
	}
	fmt.Fprintf(stdout, "launch_agent_installed: %t\n", installLaunchAgentChoice)
	if installLaunchAgentChoice {
		fmt.Fprintf(stdout, "launch_agent_label: %s\n", launchAgentResult.Label)
		fmt.Fprintf(stdout, "launch_agent_plist: %s\n", launchAgentResult.PlistPath)
		fmt.Fprintf(stdout, "launch_agent_loaded: %t\n", launchAgentResult.Loaded)
	} else {
		fmt.Fprintln(stdout, "launch_agent_loaded: false")
	}
	fmt.Fprintf(stdout, "readiness_state: %s\n", deriveSetupReadinessState(installHooks, installLaunchAgentChoice, launchAgentResult.Loaded))
	nextSteps := setupNextSteps(repoRoot, installHooks, installLaunchAgentChoice, launchAgentResult.Loaded)
	if len(nextSteps) > 0 {
		fmt.Fprintln(stdout, "next_steps:")
		for _, nextStep := range nextSteps {
			fmt.Fprintf(stdout, "  - %s\n", nextStep)
		}
	}
	fmt.Fprintln(stdout, "next_commands:")
	fmt.Fprintf(stdout, "  - %s status\n", operatorCommandPath(repoRoot, "loopgate"))
	fmt.Fprintf(stdout, "  - %s test\n", operatorCommandPath(repoRoot, "loopgate"))
	fmt.Fprintf(stdout, "  - %s uninstall\n", operatorCommandPath(repoRoot, "loopgate"))
	printSetupVerificationHints(stdout, repoRoot)
	return nil
}

func resolveSetupPolicyPreset(profileFlag string, assumeDefaults bool, interactive bool, promptReader *bufio.Reader, stdout io.Writer) (config.PolicyTemplatePreset, error) {
	if trimmedProfileFlag := strings.TrimSpace(profileFlag); trimmedProfileFlag != "" {
		return config.ResolveSetupPolicyTemplatePreset(trimmedProfileFlag)
	}
	if assumeDefaults {
		return config.ResolveSetupPolicyTemplatePreset(defaultSetupPolicyProfile)
	}
	if !interactive {
		return config.PolicyTemplatePreset{}, fmt.Errorf("setup needs -profile or -yes when stdin is not interactive")
	}
	return promptForPolicyTemplatePreset(promptReader, stdout)
}

func resolveSetupBooleanChoice(choiceName string, positiveFlag bool, negativeFlag bool, defaultValue bool, assumeDefaults bool, interactive bool, promptReader *bufio.Reader, stdout io.Writer, promptText string) (bool, error) {
	if positiveFlag {
		return true, nil
	}
	if negativeFlag {
		return false, nil
	}
	if assumeDefaults {
		return defaultValue, nil
	}
	if !interactive {
		return false, fmt.Errorf("setup needs an explicit choice for %s when stdin is not interactive", choiceName)
	}
	return promptYesNo(promptReader, stdout, promptText, defaultValue)
}

func resolveLaunchAgentInstallChoice(positiveFlag bool, negativeFlag bool, assumeDefaults bool, interactive bool, promptReader *bufio.Reader, stdout io.Writer) (bool, error) {
	if runtime.GOOS != "darwin" {
		if positiveFlag {
			return false, fmt.Errorf("install-launch-agent is only supported on macOS")
		}
		return false, nil
	}
	return resolveSetupBooleanChoice("LaunchAgent installation", positiveFlag, negativeFlag, true, assumeDefaults, interactive, promptReader, stdout, "Install a LaunchAgent so Loopgate stays running in the background")
}

func buildHookPromptText(claudeDirFlag string) string {
	if trimmedClaudeDirFlag := strings.TrimSpace(claudeDirFlag); trimmedClaudeDirFlag != "" {
		return fmt.Sprintf("Install Claude Code hooks into %s", filepath.Clean(trimmedClaudeDirFlag))
	}
	claudeDir, err := defaultClaudeDir()
	if err != nil {
		return "Install Claude Code hooks"
	}
	return fmt.Sprintf("Install Claude Code hooks into %s", claudeDir)
}

func promptForPolicyTemplatePreset(promptReader *bufio.Reader, stdout io.Writer) (config.PolicyTemplatePreset, error) {
	fmt.Fprintln(stdout, "Choose a starter policy profile:")
	for index, preset := range config.SetupPolicyTemplatePresets() {
		printPolicyTemplatePresetChoice(stdout, index+1, preset)
	}
	fmt.Fprintln(stdout, "For the current v1 setup flow, the supported guided profiles are balanced, strict, and read-only.")
	fmt.Fprintln(stdout, "If you need the broader developer template, render and review it manually with loopgate-policy-admin.")
	for {
		fmt.Fprintf(stdout, "Policy profile [%s]: ", defaultSetupPolicyProfile)
		responseLine, err := promptReader.ReadString('\n')
		if err != nil && err != io.EOF {
			return config.PolicyTemplatePreset{}, fmt.Errorf("read policy profile choice: %w", err)
		}
		trimmedResponse := strings.TrimSpace(responseLine)
		if trimmedResponse == "" {
			return config.ResolveSetupPolicyTemplatePreset(defaultSetupPolicyProfile)
		}
		if numericChoice, parseErr := strconv.Atoi(trimmedResponse); parseErr == nil {
			presets := config.SetupPolicyTemplatePresets()
			if numericChoice >= 1 && numericChoice <= len(presets) {
				return presets[numericChoice-1], nil
			}
		}
		preset, resolveErr := config.ResolveSetupPolicyTemplatePreset(trimmedResponse)
		if resolveErr == nil {
			return preset, nil
		}
		fmt.Fprintf(stdout, "Enter one of: %s\n", strings.Join(config.SetupPolicyTemplatePresetNames(), ", "))
	}
}

func printPolicyTemplatePresetChoice(output io.Writer, numericChoice int, preset config.PolicyTemplatePreset) {
	fmt.Fprintf(output, "\n  %d. %s", numericChoice, preset.Name)
	if preset.RecommendedInSetup {
		fmt.Fprint(output, " (recommended)")
	}
	fmt.Fprintln(output)
	fmt.Fprintf(output, "     %s\n", preset.Summary)
	if strings.TrimSpace(preset.UseCase) != "" {
		fmt.Fprintf(output, "     Use when: %s\n", preset.UseCase)
	}
	printIndentedList(output, "     Always allowed:", preset.AlwaysAllowed)
	printIndentedList(output, "     Approval required:", preset.ApprovalRequired)
	printIndentedList(output, "     Hard blocks:", preset.HardBlocks)
}

func printIndentedList(output io.Writer, heading string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintln(output, heading)
	for _, value := range values {
		fmt.Fprintf(output, "       - %s\n", value)
	}
}

func promptYesNo(promptReader *bufio.Reader, stdout io.Writer, promptText string, defaultValue bool) (bool, error) {
	defaultPrompt := "[y/N]"
	if defaultValue {
		defaultPrompt = "[Y/n]"
	}
	for {
		fmt.Fprintf(stdout, "%s %s: ", promptText, defaultPrompt)
		responseLine, err := promptReader.ReadString('\n')
		if err != nil && err != io.EOF {
			return false, fmt.Errorf("read prompt response: %w", err)
		}
		trimmedResponse := strings.TrimSpace(strings.ToLower(responseLine))
		if trimmedResponse == "" {
			return defaultValue, nil
		}
		switch trimmedResponse {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		fmt.Fprintln(stdout, "Please answer yes or no.")
	}
}

func readerIsInteractive(reader io.Reader) bool {
	fileReader, ok := reader.(*os.File)
	if !ok {
		return false
	}
	fileInfo, err := fileReader.Stat()
	if err != nil {
		return false
	}
	return fileInfo.Mode()&os.ModeCharDevice != 0
}

func writeAndSignPolicyPreset(repoRoot string, preset config.PolicyTemplatePreset, keyID string) error {
	policy, err := config.ParsePolicyDocument([]byte(preset.TemplateYAML))
	if err != nil {
		return fmt.Errorf("parse %s starter policy profile: %w", preset.Name, err)
	}
	if err := config.WritePolicyYAML(repoRoot, policy); err != nil {
		return fmt.Errorf("write %s starter policy profile: %w", preset.Name, err)
	}
	if err := signRepoPolicyWithLocalOperatorKey(repoRoot, keyID); err != nil {
		return err
	}
	if _, err := config.LoadPolicy(repoRoot); err != nil {
		return fmt.Errorf("verify signed policy after applying %s starter profile: %w", preset.Name, err)
	}
	return nil
}

func resolveSetupClaudeDir(claudeDirFlag string) string {
	if trimmedClaudeDirFlag := strings.TrimSpace(claudeDirFlag); trimmedClaudeDirFlag != "" {
		return filepath.Clean(trimmedClaudeDirFlag)
	}
	claudeDir, err := defaultClaudeDir()
	if err != nil {
		return "(unavailable: could not determine default Claude config directory)"
	}
	return claudeDir
}

func printSetupIntro(output io.Writer, repoRoot string, keyID string, claudeDir string) {
	fmt.Fprintln(output, "Loopgate setup")
	fmt.Fprintln(output, "This wizard runs the repo-local operator checklist once and prints every change it is about to make.")
	fmt.Fprintf(output, "Repository: %s\n", repoRoot)
	fmt.Fprintf(output, "Policy signer: %s\n", keyID)
	fmt.Fprintf(output, "Claude config: %s\n", claudeDir)
	fmt.Fprintf(output, "Recommended starter profile: %s\n", defaultSetupPolicyProfile)
	fmt.Fprintln(output, "Checklist:")
	fmt.Fprintln(output, "  1. Initialize or reuse the local policy signer")
	fmt.Fprintln(output, "  2. Apply and sign a starter policy")
	fmt.Fprintln(output, "  3. Install Claude Code governance hooks")
	if runtime.GOOS == "darwin" {
		fmt.Fprintln(output, "  4. Optionally install and load the macOS LaunchAgent")
	}
	fmt.Fprintln(output)
}

func validateSetupPrerequisites(plan loopgateSetupPlan) (string, error) {
	if !plan.InstallHooks {
		return "", nil
	}
	python3Path, err := exec.LookPath("python3")
	if err != nil {
		return "", fmt.Errorf("claude hooks require python3 on PATH; install Python 3 or rerun setup with -skip-hooks")
	}
	return filepath.Clean(python3Path), nil
}

func printSetupPlanSummary(output io.Writer, plan loopgateSetupPlan) {
	fmt.Fprintln(output)
	fmt.Fprintln(output, "Operator checklist:")
	fmt.Fprintf(output, "  1. Signer: initialize or reuse %s\n", plan.KeyID)
	fmt.Fprintf(output, "  Policy profile: %s\n", plan.SelectedPreset.Name)
	fmt.Fprintf(output, "    %s\n", plan.SelectedPreset.Summary)
	fmt.Fprintf(output, "  2. Signed policy path: %s\n", filepath.Join(plan.RepoRoot, "core", "policy", "policy.yaml"))
	if plan.InstallHooks {
		fmt.Fprintf(output, "  3. Claude hooks: install into %s\n", plan.ClaudeDir)
		fmt.Fprintf(output, "     Python runtime: %s\n", plan.Python3Path)
	} else {
		fmt.Fprintln(output, "  3. Claude hooks: skip")
	}
	switch {
	case plan.InstallLaunchAgent && plan.LoadLaunchAgent:
		fmt.Fprintln(output, "  4. Background startup: install and load the macOS LaunchAgent")
	case plan.InstallLaunchAgent:
		fmt.Fprintln(output, "  4. Background startup: install the macOS LaunchAgent without loading it yet")
	default:
		fmt.Fprintln(output, "  4. Background startup: skip LaunchAgent installation")
	}
	fmt.Fprintln(output)
}

func printSetupVerificationHints(output io.Writer, repoRoot string) {
	fmt.Fprintln(output, "verify:")
	fmt.Fprintf(output, "  1. Run %s status for the quick operator summary.\n", operatorCommandPath(repoRoot, "loopgate"))
	fmt.Fprintf(output, "  2. Run %s test for a governed local smoke test.\n", operatorCommandPath(repoRoot, "loopgate"))
	fmt.Fprintf(output, "  3. Run %s tail -verbose to inspect the resulting local audit trail.\n", operatorCommandPath(repoRoot, "loopgate-ledger"))
}

func deriveSetupReadinessState(hooksInstalled bool, launchAgentInstalled bool, launchAgentLoaded bool) string {
	switch {
	case hooksInstalled && launchAgentLoaded:
		return "ready-for-claude"
	case hooksInstalled && launchAgentInstalled:
		return "launch-agent-load-required"
	case hooksInstalled:
		return "daemon-start-required"
	case launchAgentLoaded:
		return "hooks-required"
	case launchAgentInstalled:
		return "hooks-and-launch-agent-load-required"
	default:
		return "hooks-and-daemon-required"
	}
}

func setupNextSteps(repoRoot string, hooksInstalled bool, launchAgentInstalled bool, launchAgentLoaded bool) []string {
	loopgateCmd := operatorCommandPath(repoRoot, "loopgate")
	nextSteps := make([]string, 0, 3)
	if !hooksInstalled {
		nextSteps = append(nextSteps, fmt.Sprintf("Install Claude Code hooks with %s install-hooks before relying on Claude Code.", loopgateCmd))
	}
	switch {
	case launchAgentLoaded:
		nextSteps = append(nextSteps, "Loopgate is managed by launchd and should stay running in the background.")
	case launchAgentInstalled:
		nextSteps = append(nextSteps, fmt.Sprintf("Load the LaunchAgent with %s install-launch-agent -load, or start Loopgate once in the foreground to verify startup.", loopgateCmd))
	default:
		if runtime.GOOS == "darwin" {
			nextSteps = append(nextSteps, fmt.Sprintf("Start Loopgate with %s, or install a LaunchAgent later with %s install-launch-agent -load.", loopgateCmd, loopgateCmd))
		} else {
			nextSteps = append(nextSteps, fmt.Sprintf("Start Loopgate with %s before relying on the governed path.", loopgateCmd))
		}
	}
	return nextSteps
}
