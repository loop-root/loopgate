package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"loopgate/internal/config"
)

const defaultSetupPolicyProfile = "balanced"

func runSetup(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoRootFlag := fs.String("repo-root", "", "repository root containing Loopgate config and signed policy files")
	keyIDFlag := fs.String("key-id", "", "policy signing key identifier (default: local-operator-<short-hostname>)")
	profileFlag := fs.String("profile", "", "starter policy profile: strict, balanced, or developer")
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
		return err
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

	interactive := readerIsInteractive(stdin)
	promptReader := bufio.NewReader(stdin)

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
	fmt.Fprintf(stdout, "profile: %s\n", selectedPreset.Name)
	if initResult.AlreadyInitialized {
		fmt.Fprintf(stdout, "policy_signing: reused local signer (%s)\n", initResult.KeyID)
	} else {
		fmt.Fprintf(stdout, "policy_signing: initialized local signer (%s)\n", initResult.KeyID)
	}
	fmt.Fprintf(stdout, "policy_path: %s\n", filepath.Join(repoRoot, "core", "policy", "policy.yaml"))
	fmt.Fprintf(stdout, "signature_path: %s\n", config.PolicySignaturePath(repoRoot))
	fmt.Fprintf(stdout, "socket_path: %s\n", initResult.SocketPath)
	fmt.Fprintf(stdout, "claude_hooks_installed: %t\n", installHooks)
	fmt.Fprintf(stdout, "launch_agent_installed: %t\n", installLaunchAgentChoice)
	if installLaunchAgentChoice {
		fmt.Fprintf(stdout, "launch_agent_label: %s\n", launchAgentResult.Label)
		fmt.Fprintf(stdout, "launch_agent_plist: %s\n", launchAgentResult.PlistPath)
		fmt.Fprintf(stdout, "launch_agent_loaded: %t\n", launchAgentResult.Loaded)
	}
	switch {
	case installLaunchAgentChoice && launchAgentResult.Loaded:
		fmt.Fprintln(stdout, "next_step: Loopgate is managed by launchd and should stay running in the background.")
	case installLaunchAgentChoice:
		fmt.Fprintln(stdout, "next_step: Load the LaunchAgent when you are ready, or start Loopgate once in the foreground to verify startup.")
	default:
		fmt.Fprintln(stdout, "next_step: Start Loopgate with ./bin/loopgate, or install a LaunchAgent later with ./bin/loopgate install-launch-agent -load.")
	}
	return nil
}

func resolveSetupPolicyPreset(profileFlag string, assumeDefaults bool, interactive bool, promptReader *bufio.Reader, stdout io.Writer) (config.PolicyTemplatePreset, error) {
	if trimmedProfileFlag := strings.TrimSpace(profileFlag); trimmedProfileFlag != "" {
		return config.ResolvePolicyTemplatePreset(trimmedProfileFlag)
	}
	if assumeDefaults {
		return config.ResolvePolicyTemplatePreset(defaultSetupPolicyProfile)
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
	for index, preset := range config.PolicyTemplatePresets() {
		fmt.Fprintf(stdout, "  %d. %s - %s\n", index+1, preset.Name, preset.Summary)
	}
	for {
		fmt.Fprintf(stdout, "Policy profile [%s]: ", defaultSetupPolicyProfile)
		responseLine, err := promptReader.ReadString('\n')
		if err != nil && err != io.EOF {
			return config.PolicyTemplatePreset{}, fmt.Errorf("read policy profile choice: %w", err)
		}
		trimmedResponse := strings.TrimSpace(responseLine)
		if trimmedResponse == "" {
			return config.ResolvePolicyTemplatePreset(defaultSetupPolicyProfile)
		}
		if numericChoice, parseErr := strconv.Atoi(trimmedResponse); parseErr == nil {
			presets := config.PolicyTemplatePresets()
			if numericChoice >= 1 && numericChoice <= len(presets) {
				return presets[numericChoice-1], nil
			}
		}
		preset, resolveErr := config.ResolvePolicyTemplatePreset(trimmedResponse)
		if resolveErr == nil {
			return preset, nil
		}
		fmt.Fprintf(stdout, "Enter one of: %s\n", strings.Join(config.PolicyTemplatePresetNames(), ", "))
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
