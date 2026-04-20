package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func runUninstall(args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(stderr)

	defaultRepoRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("determine default repo root: %w", err)
	}
	defaultClaudeConfigDir, err := defaultClaudeDir()
	if err != nil {
		return err
	}

	repoRootFlag := fs.String("repo-root", defaultRepoRoot, "repository root that Loopgate serves from")
	claudeDirFlag := fs.String("claude-dir", defaultClaudeConfigDir, "Claude config directory")
	launchAgentsDirFlag := fs.String("launch-agents-dir", "", "LaunchAgents directory used for macOS LaunchAgent removal")
	labelFlag := fs.String("label", "", "launch agent label override")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}

	repoRoot, err := resolveLoopgateRepoRoot(strings.TrimSpace(*repoRootFlag))
	if err != nil {
		return err
	}
	claudeConfigDir := filepath.Clean(strings.TrimSpace(*claudeDirFlag))
	if claudeConfigDir == "" {
		return fmt.Errorf("claude-dir path must not be empty")
	}

	if err := runRemoveHooks([]string{"-repo", repoRoot, "-claude-dir", claudeConfigDir}, stdout); err != nil {
		return err
	}
	removedScripts, err := removeLoopgateHookScripts(claudeConfigDir)
	if err != nil {
		return err
	}

	var launchAgentResult launchAgentRemoveResult
	launchAgentRemoved := false
	if runtime.GOOS == "darwin" {
		launchAgentResult, err = removeLaunchAgent(launchAgentRemoveOptions{
			RepoRoot:        repoRoot,
			LaunchAgentsDir: strings.TrimSpace(*launchAgentsDirFlag),
			Label:           strings.TrimSpace(*labelFlag),
		}, defaultLaunchAgentDependencies())
		if err != nil {
			return err
		}
		launchAgentRemoved = true
	}

	fmt.Fprintln(stdout, "uninstall OK")
	fmt.Fprintf(stdout, "claude_dir: %s\n", claudeConfigDir)
	fmt.Fprintf(stdout, "removed_hook_scripts: %d\n", removedScripts)
	if launchAgentRemoved {
		fmt.Fprintf(stdout, "launch_agent_label: %s\n", launchAgentResult.Label)
		fmt.Fprintf(stdout, "launch_agent_plist: %s\n", launchAgentResult.PlistPath)
		fmt.Fprintf(stdout, "launch_agent_removed: %t\n", launchAgentResult.Removed)
		fmt.Fprintf(stdout, "launch_agent_unloaded: %t\n", launchAgentResult.Unloaded)
	} else {
		fmt.Fprintln(stdout, "launch_agent_removed: skipped (not macOS)")
	}
	fmt.Fprintln(stdout, "left_in_place: local binaries, signed policy files, and runtime/audit state")
	fmt.Fprintln(stdout, "next_step: If you installed binaries into ~/.local/bin, run make uninstall-local or remove them manually.")
	return nil
}

func removeLoopgateHookScripts(claudeDir string) (int, error) {
	claudeHooksDir := filepath.Join(filepath.Clean(claudeDir), claudeHooksDirname)
	removedScripts := 0
	for _, scriptName := range loopgateHookBundleFiles {
		scriptPath := filepath.Join(claudeHooksDir, scriptName)
		if err := os.Remove(scriptPath); err == nil {
			removedScripts++
			continue
		} else if !os.IsNotExist(err) {
			return removedScripts, fmt.Errorf("remove hook script %s: %w", scriptPath, err)
		}
	}
	return removedScripts, nil
}
