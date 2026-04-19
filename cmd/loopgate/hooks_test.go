package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInstallHooks_CopiesScriptsAndWritesSettings(t *testing.T) {
	repoRoot := makeTestHookRepo(t)
	claudeDir := t.TempDir()

	if err := runInstallHooks([]string{"-repo", repoRoot, "-claude-dir", claudeDir}); err != nil {
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
		if err := runInstallHooks([]string{"-repo", repoRoot, "-claude-dir", claudeDir}); err != nil {
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

	if err := runInstallHooks([]string{"-repo", repoRoot, "-claude-dir", claudeDir}); err != nil {
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

	if err := runInstallHooks([]string{"-repo", repoRoot, "-claude-dir", claudeDir}); err != nil {
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

	err := runInstallHooks([]string{"-repo", repoRoot, "-claude-dir", claudeDir})
	if err == nil {
		t.Fatal("expected install-hooks to fail when the tracked hook bundle is missing")
	}
	if !strings.Contains(err.Error(), loopgateHookBundleDir) {
		t.Fatalf("expected missing-bundle error to mention %q, got %v", loopgateHookBundleDir, err)
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

	if err := runInstallHooks([]string{"-repo", repoRoot, "-claude-dir", claudeDir}); err != nil {
		t.Fatalf("runInstallHooks returned error: %v", err)
	}
	if err := runRemoveHooks([]string{"-claude-dir", claudeDir}); err != nil {
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

	if err := runRemoveHooks([]string{"-repo", repoRoot, "-claude-dir", claudeDir}); err != nil {
		t.Fatalf("runRemoveHooks returned error: %v", err)
	}

	updatedConfig := loadTestClaudeSettings(t, repoSettingsPath)
	if len(updatedConfig.Hooks) != 0 {
		t.Fatalf("expected repo-local loopgate hooks removed, got %#v", updatedConfig.Hooks)
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
