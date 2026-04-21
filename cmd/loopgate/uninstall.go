package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"loopgate/internal/config"
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
	purgeFlag := fs.Bool("purge", false, "also remove repo runtime state, local signer material, and default installed binaries")
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

	purgeResult := uninstallPurgeResult{}
	if *purgeFlag {
		purgeResult, err = purgeLoopgateLocalState(context.Background(), repoRoot)
		if err != nil {
			return err
		}
	}

	fmt.Fprintln(stdout, "uninstall OK")
	fmt.Fprintf(stdout, "purge: %t\n", *purgeFlag)
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
	if *purgeFlag {
		fmt.Fprintf(stdout, "removed_runtime_dir: %t\n", purgeResult.RuntimeDirRemoved)
		fmt.Fprintf(stdout, "removed_signer_private_key: %t\n", purgeResult.SignerPrivateKeyRemoved)
		fmt.Fprintf(stdout, "removed_signer_public_key: %t\n", purgeResult.SignerPublicKeyRemoved)
		fmt.Fprintf(stdout, "removed_installed_binaries: %d\n", purgeResult.RemovedInstalledBinaries)
		fmt.Fprintln(stdout, "left_in_place: tracked repo policy files such as core/policy/policy.yaml and core/policy/policy.yaml.sig")
	} else {
		fmt.Fprintln(stdout, "left_in_place: local binaries, signed policy files, and runtime/audit state")
		fmt.Fprintln(stdout, "next_step: rerun with --purge to also remove repo runtime state, local signer material, and default installed binaries.")
	}
	return nil
}

type uninstallPurgeResult struct {
	RuntimeDirRemoved        bool
	SignerPrivateKeyRemoved  bool
	SignerPublicKeyRemoved   bool
	RemovedInstalledBinaries int
}

func purgeLoopgateLocalState(ctx context.Context, repoRoot string) (uninstallPurgeResult, error) {
	result := uninstallPurgeResult{}

	runtimeDir := filepath.Join(repoRoot, "runtime")
	if err := os.RemoveAll(runtimeDir); err != nil {
		return result, fmt.Errorf("remove runtime directory %s: %w", runtimeDir, err)
	}
	result.RuntimeDirRemoved = true

	signatureFile, err := config.LoadPolicySignatureFile(repoRoot)
	if err == nil {
		privateKeyPath, privateKeyErr := defaultOperatorPolicySigningPrivateKeyPath(signatureFile.KeyID)
		if privateKeyErr != nil {
			return result, privateKeyErr
		}
		publicKeyPath, publicKeyErr := defaultOperatorPolicySigningPublicKeyPath(signatureFile.KeyID)
		if publicKeyErr != nil {
			return result, publicKeyErr
		}

		if removed, removeErr := removePathIfPresent(privateKeyPath); removeErr != nil {
			return result, removeErr
		} else {
			result.SignerPrivateKeyRemoved = removed
		}
		if removed, removeErr := removePathIfPresent(publicKeyPath); removeErr != nil {
			return result, removeErr
		} else {
			result.SignerPublicKeyRemoved = removed
		}
	}

	installedBinaryPaths, err := defaultInstalledBinaryPaths()
	if err != nil {
		return result, err
	}
	for _, installedBinaryPath := range installedBinaryPaths {
		removed, removeErr := removePathIfPresent(installedBinaryPath)
		if removeErr != nil {
			return result, removeErr
		}
		if removed {
			result.RemovedInstalledBinaries++
		}
	}

	_ = ctx
	return result, nil
}

func removePathIfPresent(path string) (bool, error) {
	if err := os.Remove(path); err == nil {
		return true, nil
	} else if os.IsNotExist(err) {
		return false, nil
	} else {
		return false, fmt.Errorf("remove %s: %w", path, err)
	}
}

func defaultInstalledBinaryPaths() ([]string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("determine home directory for uninstall purge: %w", err)
	}
	installDir := filepath.Join(homeDir, ".local", "bin")
	return []string{
		filepath.Join(installDir, "loopgate"),
		filepath.Join(installDir, "loopgate-doctor"),
		filepath.Join(installDir, "loopgate-ledger"),
		filepath.Join(installDir, "loopgate-policy-admin"),
		filepath.Join(installDir, "loopgate-policy-sign"),
	}, nil
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
