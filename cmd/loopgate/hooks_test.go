package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInstallHooks_CopiesScriptsAndWritesSettings(t *testing.T) {
	repoRoot := makeTestHookRepo(t)
	claudeDir := t.TempDir()

	if err := runInstallHooks([]string{"-repo", repoRoot, "-claude-dir", claudeDir}, io.Discard); err != nil {
		t.Fatalf("runInstallHooks returned error: %v", err)
	}

	for _, scriptName := range loopgateHookBundleFiles {
		scriptPath := filepath.Join(claudeDir, claudeHooksDirname, scriptName)
		scriptBytes, err := os.ReadFile(scriptPath)
		if err != nil {
			t.Fatalf("read copied script %s: %v", scriptName, err)
		}
		if string(scriptBytes) != testHookBundleFileContents(scriptName) {
			t.Fatalf("unexpected script contents for %s: %q", scriptName, string(scriptBytes))
		}
	}

	settingsConfig := loadTestClaudeSettings(t, filepath.Join(claudeDir, claudeSettingsFilename))
	if len(settingsConfig.Hooks) != len(loopgateHookEvents) {
		t.Fatalf("expected %d hook events, got %d", len(loopgateHookEvents), len(settingsConfig.Hooks))
	}
	for _, hookSpec := range loopgateHookEvents {
		groups := settingsConfig.Hooks[hookSpec.EventName]
		if len(groups) != 1 {
			t.Fatalf("expected one matcher group for %s, got %d", hookSpec.EventName, len(groups))
		}
		if groups[0].Matcher != hookSpec.Matcher {
			t.Fatalf("expected matcher %q for %s, got %q", hookSpec.Matcher, hookSpec.EventName, groups[0].Matcher)
		}
		if len(groups[0].Hooks) != 1 {
			t.Fatalf("expected one hook action for %s, got %d", hookSpec.EventName, len(groups[0].Hooks))
		}
		expectedCommand := "python3 " + shellQuoteHookCommandPath(filepath.Join(claudeDir, claudeHooksDirname, hookSpec.ScriptName))
		if groups[0].Hooks[0].Type != "command" || groups[0].Hooks[0].Command != expectedCommand {
			t.Fatalf("unexpected hook action for %s: %#v", hookSpec.EventName, groups[0].Hooks[0])
		}
	}
}

func TestLoopgateHookBundleCopiesSharedHelperFirst(t *testing.T) {
	if len(loopgateHookBundleFiles) == 0 {
		t.Fatal("expected tracked hook bundle files")
	}
	if loopgateHookBundleFiles[0] != "loopgate_hook_common.py" {
		t.Fatalf("expected shared helper to copy first, got %q", loopgateHookBundleFiles[0])
	}
}

func TestRunInstallHooks_IsIdempotent(t *testing.T) {
	repoRoot := makeTestHookRepo(t)
	claudeDir := t.TempDir()

	for runIndex := 0; runIndex < 2; runIndex++ {
		if err := runInstallHooks([]string{"-repo", repoRoot, "-claude-dir", claudeDir}, io.Discard); err != nil {
			t.Fatalf("runInstallHooks returned error on pass %d: %v", runIndex+1, err)
		}
	}

	settingsConfig := loadTestClaudeSettings(t, filepath.Join(claudeDir, claudeSettingsFilename))
	for _, hookSpec := range loopgateHookEvents {
		groups := settingsConfig.Hooks[hookSpec.EventName]
		if len(groups) != 1 {
			t.Fatalf("expected one matcher group for %s after rerun, got %d", hookSpec.EventName, len(groups))
		}
		if len(groups[0].Hooks) != 1 {
			t.Fatalf("expected one hook action for %s after rerun, got %d", hookSpec.EventName, len(groups[0].Hooks))
		}
	}
}

func TestRunInstallHooks_PreservesOtherSettings(t *testing.T) {
	repoRoot := makeTestHookRepo(t)
	claudeDir := t.TempDir()
	settingsPath := filepath.Join(claudeDir, claudeSettingsFilename)
	rawSettings := []byte("{\n  \"permissions\": {\"allow\": [\"Bash(git status)\"]},\n  \"hooks\": {}\n}\n")
	if err := os.WriteFile(settingsPath, rawSettings, 0o644); err != nil {
		t.Fatalf("write raw settings: %v", err)
	}

	if err := runInstallHooks([]string{"-repo", repoRoot, "-claude-dir", claudeDir}, io.Discard); err != nil {
		t.Fatalf("runInstallHooks returned error: %v", err)
	}

	settingsBytes, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings file: %v", err)
	}
	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal(settingsBytes, &rawFields); err != nil {
		t.Fatalf("unmarshal raw settings: %v", err)
	}
	if _, ok := rawFields["permissions"]; !ok {
		t.Fatalf("expected permissions field to be preserved in settings.json")
	}
}

func TestRunInstallHooks_QuotesCommandPathsWithSpaces(t *testing.T) {
	repoRoot := makeTestHookRepo(t)
	parentDir := t.TempDir()
	claudeDir := filepath.Join(parentDir, "Claude Dir With Space")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir claude dir: %v", err)
	}

	if err := runInstallHooks([]string{"-repo", repoRoot, "-claude-dir", claudeDir}, io.Discard); err != nil {
		t.Fatalf("runInstallHooks returned error: %v", err)
	}

	settingsConfig := loadTestClaudeSettings(t, filepath.Join(claudeDir, claudeSettingsFilename))
	for _, hookSpec := range loopgateHookEvents {
		groups := settingsConfig.Hooks[hookSpec.EventName]
		if len(groups) != 1 || len(groups[0].Hooks) != 1 {
			t.Fatalf("unexpected hook groups for %s: %#v", hookSpec.EventName, groups)
		}
		expectedCommand := "python3 " + shellQuoteHookCommandPath(filepath.Join(claudeDir, claudeHooksDirname, hookSpec.ScriptName))
		if groups[0].Hooks[0].Command != expectedCommand {
			t.Fatalf("expected quoted hook command %q for %s, got %#v", expectedCommand, hookSpec.EventName, groups[0].Hooks[0])
		}
	}
}

func TestRunInstallHooks_MissingHookBundleExplainsExpectedSourceDir(t *testing.T) {
	repoRoot := t.TempDir()
	claudeDir := t.TempDir()

	err := runInstallHooks([]string{"-repo", repoRoot, "-claude-dir", claudeDir}, io.Discard)
	if err == nil {
		t.Fatal("expected install-hooks to fail when the tracked hook bundle is missing")
	}
	if !strings.Contains(err.Error(), loopgateHookBundleDir) {
		t.Fatalf("expected missing-bundle error to mention %q, got %v", loopgateHookBundleDir, err)
	}
}

func TestParseHookCommandArgs_PrefersLoopgateRepoRootEnv(t *testing.T) {
	repoRoot := makeTestHookRepo(t)
	claudeDir := t.TempDir()
	otherDir := t.TempDir()

	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(otherDir); err != nil {
		t.Fatalf("os.Chdir(%s): %v", otherDir, err)
	}
	defer func() {
		if err := os.Chdir(workingDir); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	}()

	t.Setenv(loopgateRepoRootEnv, repoRoot)

	resolvedRepoRoot, resolvedClaudeDir, err := parseHookCommandArgs("install-hooks", []string{
		"-claude-dir", claudeDir,
	})
	if err != nil {
		t.Fatalf("parseHookCommandArgs: %v", err)
	}
	if resolvedRepoRoot != repoRoot {
		t.Fatalf("expected repo root %q from %s, got %q", repoRoot, loopgateRepoRootEnv, resolvedRepoRoot)
	}
	if resolvedClaudeDir != claudeDir {
		t.Fatalf("expected claude dir %q, got %q", claudeDir, resolvedClaudeDir)
	}
}

func TestGovernedHookScriptsFailClosedWhenLoopgateUnreachable(t *testing.T) {
	python3Path, err := exec.LookPath("python3")
	if err != nil {
		t.Skipf("python3 not available: %v", err)
	}
	currentWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("determine working directory: %v", err)
	}
	repoRoot := filepath.Clean(filepath.Join(currentWorkingDirectory, "..", ".."))
	unreachableSocketPath := fmt.Sprintf("/tmp/loopgate-hook-unreachable-%d.sock", os.Getpid())
	_ = os.Remove(unreachableSocketPath)

	testCases := []struct {
		name       string
		scriptName string
		inputJSON  string
	}{
		{
			name:       "pretool",
			scriptName: "loopgate_pretool.py",
			inputJSON:  `{"tool_name":"Bash","tool_use_id":"toolu_test","tool_input":{"command":"pwd"}}`,
		},
		{
			name:       "permissionrequest",
			scriptName: "loopgate_permissionrequest.py",
			inputJSON:  `{"tool_name":"Bash","tool_use_id":"toolu_test","reason":"need shell access"}`,
		},
		{
			name:       "userpromptsubmit",
			scriptName: "loopgate_userpromptsubmit.py",
			inputJSON:  `{"prompt":"review the repo"}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			scriptPath := filepath.Join(repoRoot, filepath.FromSlash(loopgateHookBundleDir), testCase.scriptName)
			command := exec.Command(python3Path, scriptPath)
			command.Env = append(os.Environ(), "LOOPGATE_SOCKET="+unreachableSocketPath)
			command.Stdin = strings.NewReader(testCase.inputJSON)

			var stdoutBuffer bytes.Buffer
			var stderrBuffer bytes.Buffer
			command.Stdout = &stdoutBuffer
			command.Stderr = &stderrBuffer

			runErr := command.Run()
			if runErr == nil {
				t.Fatalf("expected %s to fail closed when Loopgate is unreachable", testCase.scriptName)
			}
			exitErr, ok := runErr.(*exec.ExitError)
			if !ok {
				t.Fatalf("expected exit error for %s, got %T: %v", testCase.scriptName, runErr, runErr)
			}
			if exitErr.ExitCode() != 2 {
				t.Fatalf("expected blocking exit code 2 for %s, got %d with stderr %q", testCase.scriptName, exitErr.ExitCode(), stderrBuffer.String())
			}
			if stdoutBuffer.Len() != 0 {
				t.Fatalf("expected no stdout for %s when Loopgate is unreachable, got %q", testCase.scriptName, stdoutBuffer.String())
			}
			expectedErrorPrefix := "Loopgate hook error: failed to contact Loopgate over " + unreachableSocketPath
			if !strings.Contains(stderrBuffer.String(), expectedErrorPrefix) {
				t.Fatalf("expected stderr for %s to contain %q, got %q", testCase.scriptName, expectedErrorPrefix, stderrBuffer.String())
			}
		})
	}
}

func TestLoadClaudeSettings_RejectsUnknownHookActionField(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), claudeSettingsFilename)
	rawSettings := []byte("{\"hooks\":{\"PreToolUse\":[{\"hooks\":[{\"type\":\"command\",\"command\":\"python3 /tmp/x.py\",\"surprise\":true}]}]}}\n")
	if err := os.WriteFile(settingsPath, rawSettings, 0o644); err != nil {
		t.Fatalf("write raw settings: %v", err)
	}

	_, err := loadClaudeSettings(settingsPath)
	if err == nil {
		t.Fatal("expected unknown hook field to be rejected")
	}
	if !strings.Contains(err.Error(), "surprise") {
		t.Fatalf("expected unknown-field error, got %v", err)
	}
}

func TestRunRemoveHooks_RemovesOnlyLoopgateEntries(t *testing.T) {
	repoRoot := makeTestHookRepo(t)
	claudeDir := t.TempDir()

	settingsConfig := claudeSettings{
		Hooks: map[string][]claudeHookMatcherGroup{
			"PreToolUse": {
				{
					Matcher: "Bash",
					Hooks: []claudeHookAction{
						{
							Type:    "command",
							Command: "python3 /some/other/script.py",
						},
					},
				},
			},
		},
	}
	writeTestClaudeSettings(t, filepath.Join(claudeDir, claudeSettingsFilename), settingsConfig)

	if err := runInstallHooks([]string{"-repo", repoRoot, "-claude-dir", claudeDir}, io.Discard); err != nil {
		t.Fatalf("runInstallHooks returned error: %v", err)
	}
	if err := runRemoveHooks([]string{"-claude-dir", claudeDir}, io.Discard); err != nil {
		t.Fatalf("runRemoveHooks returned error: %v", err)
	}

	updatedConfig := loadTestClaudeSettings(t, filepath.Join(claudeDir, claudeSettingsFilename))
	preToolGroups := updatedConfig.Hooks["PreToolUse"]
	if len(preToolGroups) != 1 {
		t.Fatalf("expected preserved third-party PreToolUse group, got %d groups", len(preToolGroups))
	}
	if len(preToolGroups[0].Hooks) != 1 || preToolGroups[0].Hooks[0].Command != "python3 /some/other/script.py" {
		t.Fatalf("unexpected preserved PreToolUse hooks: %#v", preToolGroups[0].Hooks)
	}
	for _, hookSpec := range loopgateHookEvents {
		if hookSpec.EventName == "PreToolUse" {
			continue
		}
		if _, ok := updatedConfig.Hooks[hookSpec.EventName]; ok {
			t.Fatalf("expected Loopgate hook event %s to be removed", hookSpec.EventName)
		}
	}
}

func TestRunRemoveHooks_RemovesRepoLocalLoopgateEntries(t *testing.T) {
	repoRoot := makeTestHookRepo(t)
	claudeDir := t.TempDir()

	repoSettingsPath := filepath.Join(repoRoot, ".claude", claudeSettingsFilename)
	writeTestClaudeSettings(t, repoSettingsPath, claudeSettings{
		Hooks: map[string][]claudeHookMatcherGroup{
			"PreToolUse": {
				{
					Hooks: []claudeHookAction{
						{
							Type:    "command",
							Command: "python3 " + filepath.Join(repoRoot, ".claude", claudeHooksDirname, "loopgate_pretool.py"),
						},
					},
				},
			},
		},
	})

	if err := runRemoveHooks([]string{"-repo", repoRoot, "-claude-dir", claudeDir}, io.Discard); err != nil {
		t.Fatalf("runRemoveHooks returned error: %v", err)
	}

	updatedConfig := loadTestClaudeSettings(t, repoSettingsPath)
	if len(updatedConfig.Hooks) != 0 {
		t.Fatalf("expected repo-local loopgate hooks removed, got %#v", updatedConfig.Hooks)
	}
}

func TestRunRemoveHooks_DoesNotCreateMissingSettingsFiles(t *testing.T) {
	repoRoot := makeTestHookRepo(t)
	claudeDir := t.TempDir()

	if err := runRemoveHooks([]string{"-repo", repoRoot, "-claude-dir", claudeDir}, io.Discard); err != nil {
		t.Fatalf("runRemoveHooks returned error: %v", err)
	}

	for _, settingsPath := range collectClaudeSettingsPaths(repoRoot, claudeDir) {
		if _, err := os.Stat(settingsPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected remove-hooks not to create %s, stat err=%v", settingsPath, err)
		}
	}
}

func TestRemoveLoopgateHookScripts_RemovesCopiedBundleOnly(t *testing.T) {
	claudeDir := t.TempDir()
	hooksDir := filepath.Join(claudeDir, claudeHooksDirname)
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("mkdir hooks dir: %v", err)
	}
	for _, scriptName := range loopgateHookBundleFiles {
		if err := os.WriteFile(filepath.Join(hooksDir, scriptName), []byte(scriptName), 0o644); err != nil {
			t.Fatalf("write loopgate hook script %s: %v", scriptName, err)
		}
	}
	extraHookPath := filepath.Join(hooksDir, "keep_me.py")
	if err := os.WriteFile(extraHookPath, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write extra hook script: %v", err)
	}

	removedScripts, err := removeLoopgateHookScripts(claudeDir)
	if err != nil {
		t.Fatalf("removeLoopgateHookScripts: %v", err)
	}
	if removedScripts != len(loopgateHookBundleFiles) {
		t.Fatalf("expected %d removed scripts, got %d", len(loopgateHookBundleFiles), removedScripts)
	}
	for _, scriptName := range loopgateHookBundleFiles {
		if _, err := os.Stat(filepath.Join(hooksDir, scriptName)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected %s to be removed, stat err=%v", scriptName, err)
		}
	}
	if _, err := os.Stat(extraHookPath); err != nil {
		t.Fatalf("expected non-loopgate hook script to remain, stat err=%v", err)
	}
}

func makeTestHookRepo(t *testing.T) string {
	t.Helper()
	repoRoot := t.TempDir()
	hooksDir := filepath.Join(repoRoot, filepath.FromSlash(loopgateHookBundleDir))
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("mkdir hooks dir: %v", err)
	}
	for _, scriptName := range loopgateHookBundleFiles {
		scriptPath := filepath.Join(hooksDir, scriptName)
		if err := os.WriteFile(scriptPath, []byte(testHookBundleFileContents(scriptName)), 0o644); err != nil {
			t.Fatalf("write script %s: %v", scriptName, err)
		}
	}
	return repoRoot
}

func testHookBundleFileContents(scriptName string) string {
	return "#!/usr/bin/env python3\n# " + scriptName + "\n"
}

func loadTestClaudeSettings(t *testing.T, settingsPath string) claudeSettings {
	t.Helper()
	settingsBytes, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings file: %v", err)
	}
	var settingsConfig claudeSettings
	if err := json.Unmarshal(settingsBytes, &settingsConfig); err != nil {
		t.Fatalf("unmarshal settings file: %v", err)
	}
	return settingsConfig
}

func writeTestClaudeSettings(t *testing.T, settingsPath string, settingsConfig claudeSettings) {
	t.Helper()
	settingsBytes, err := json.MarshalIndent(settingsConfig, "", "  ")
	if err != nil {
		t.Fatalf("marshal settings file: %v", err)
	}
	settingsBytes = append(settingsBytes, '\n')
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(settingsPath, settingsBytes, 0o644); err != nil {
		t.Fatalf("write settings file: %v", err)
	}
}
