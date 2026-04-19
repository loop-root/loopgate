package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	claudeSettingsFilename = "settings.json"
	claudeHooksDirname     = "hooks"
	loopgateHookBundleDir  = "claude/hooks/scripts"
)

var (
	requiredLoopgateHookScripts = []string{
		"loopgate_pretool.py",
		"loopgate_posttool.py",
		"loopgate_posttoolfailure.py",
		"loopgate_sessionstart.py",
		"loopgate_sessionend.py",
		"loopgate_userpromptsubmit.py",
		"loopgate_permissionrequest.py",
	}
	loopgateHookBundleFiles = append(
		[]string{"loopgate_hook_common.py"},
		requiredLoopgateHookScripts...,
	)
	loopgateHookEvents = []loopgateClaudeHookSpec{
		{
			EventName:  "PreToolUse",
			Matcher:    "*",
			ScriptName: "loopgate_pretool.py",
		},
		{
			EventName:  "PostToolUse",
			Matcher:    "*",
			ScriptName: "loopgate_posttool.py",
		},
		{
			EventName:  "PostToolUseFailure",
			Matcher:    "*",
			ScriptName: "loopgate_posttoolfailure.py",
		},
		{
			EventName:  "SessionStart",
			ScriptName: "loopgate_sessionstart.py",
		},
		{
			EventName:  "SessionEnd",
			ScriptName: "loopgate_sessionend.py",
		},
		{
			EventName:  "UserPromptSubmit",
			ScriptName: "loopgate_userpromptsubmit.py",
		},
		{
			EventName:  "PermissionRequest",
			Matcher:    "*",
			ScriptName: "loopgate_permissionrequest.py",
		},
	}
)

type loopgateClaudeHookSpec struct {
	EventName  string
	Matcher    string
	ScriptName string
}

type claudeSettings struct {
	Hooks       map[string][]claudeHookMatcherGroup
	otherFields map[string]json.RawMessage
}

type claudeHookMatcherGroup struct {
	Matcher string             `json:"matcher,omitempty"`
	Hooks   []claudeHookAction `json:"hooks"`
}

type claudeHookAction struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

func handleLoopgateSubcommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "version", "--version", "-version":
		printVersion(os.Stdout)
		exitProcess(0)
		return true
	case "init":
		if err := runInit(args[1:], os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "ERROR: init:", err)
			exitProcess(1)
		}
		exitProcess(0)
		return true
	case "install-hooks":
		if err := runInstallHooks(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "ERROR: install hooks:", err)
			exitProcess(1)
		}
		exitProcess(0)
		return true
	case "remove-hooks":
		if err := runRemoveHooks(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "ERROR: remove hooks:", err)
			exitProcess(1)
		}
		exitProcess(0)
		return true
	default:
		return false
	}
}

func runInstallHooks(args []string) error {
	repoRoot, claudeDir, err := parseHookCommandArgs("install-hooks", args)
	if err != nil {
		return err
	}
	repoHooksDir := filepath.Join(repoRoot, filepath.FromSlash(loopgateHookBundleDir))
	claudeHooksDir := filepath.Join(claudeDir, claudeHooksDirname)
	if err := os.MkdirAll(claudeHooksDir, 0o755); err != nil {
		return fmt.Errorf("create claude hooks directory: %w", err)
	}
	copiedScripts, err := installLoopgateHookScripts(repoHooksDir, claudeHooksDir)
	if err != nil {
		return err
	}
	settingsPath := filepath.Join(claudeDir, claudeSettingsFilename)
	settingsConfig, err := loadClaudeSettings(settingsPath)
	if err != nil {
		return err
	}
	installedHooks := applyLoopgateHookSettings(&settingsConfig, claudeHooksDir)
	if err := writeClaudeSettings(settingsPath, settingsConfig); err != nil {
		return err
	}
	fmt.Printf("Installed Loopgate Claude hooks into %s\n", claudeDir)
	fmt.Printf("Copied %d hook files into %s\n", len(copiedScripts), claudeHooksDir)
	fmt.Printf("Configured %d hook events in %s\n", installedHooks, settingsPath)
	return nil
}

func runRemoveHooks(args []string) error {
	repoRoot, claudeDir, err := parseHookCommandArgs("remove-hooks", args)
	if err != nil {
		return err
	}
	settingsPaths := collectClaudeSettingsPaths(repoRoot, claudeDir)
	totalRemovedHooks := 0
	for _, settingsPath := range settingsPaths {
		settingsConfig, err := loadClaudeSettings(settingsPath)
		if err != nil {
			return err
		}
		removedHooks := removeLoopgateHookSettings(&settingsConfig)
		if err := writeClaudeSettings(settingsPath, settingsConfig); err != nil {
			return err
		}
		if removedHooks > 0 {
			fmt.Printf("Removed %d Loopgate Claude hook entries from %s\n", removedHooks, settingsPath)
		}
		totalRemovedHooks += removedHooks
	}
	if totalRemovedHooks == 0 {
		fmt.Printf("Removed 0 Loopgate Claude hook entries from %s\n", filepath.Join(claudeDir, claudeSettingsFilename))
	}
	fmt.Printf("Hook scripts under %s were left in place\n", filepath.Join(claudeDir, claudeHooksDirname))
	return nil
}

func parseHookCommandArgs(commandName string, args []string) (string, string, error) {
	flagSet := flag.NewFlagSet(commandName, flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)
	defaultRepoRoot, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("determine default repo root: %w", err)
	}
	defaultClaudeDir, err := defaultClaudeDir()
	if err != nil {
		return "", "", err
	}
	repoFlag := flagSet.String("repo", defaultRepoRoot, "Loopgate repo root")
	claudeDirFlag := flagSet.String("claude-dir", defaultClaudeDir, "Claude config directory")
	if err := flagSet.Parse(args); err != nil {
		return "", "", err
	}
	if flagSet.NArg() != 0 {
		return "", "", fmt.Errorf("unexpected positional arguments: %s", strings.Join(flagSet.Args(), " "))
	}
	repoRoot := filepath.Clean(strings.TrimSpace(*repoFlag))
	claudeDir := filepath.Clean(strings.TrimSpace(*claudeDirFlag))
	if repoRoot == "" {
		return "", "", errors.New("repo path must not be empty")
	}
	if claudeDir == "" {
		return "", "", errors.New("claude-dir path must not be empty")
	}
	return repoRoot, claudeDir, nil
}

func defaultClaudeDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home directory: %w", err)
	}
	return filepath.Join(homeDir, ".claude"), nil
}

func collectClaudeSettingsPaths(repoRoot string, claudeDir string) []string {
	candidateSettingsPaths := []string{
		filepath.Join(claudeDir, claudeSettingsFilename),
		filepath.Join(repoRoot, ".claude", claudeSettingsFilename),
		filepath.Join(repoRoot, ".claude", "settings.local.json"),
	}
	seenPaths := make(map[string]struct{}, len(candidateSettingsPaths))
	settingsPaths := make([]string, 0, len(candidateSettingsPaths))
	for _, candidateSettingsPath := range candidateSettingsPaths {
		cleanSettingsPath := filepath.Clean(strings.TrimSpace(candidateSettingsPath))
		if cleanSettingsPath == "" {
			continue
		}
		if _, alreadySeen := seenPaths[cleanSettingsPath]; alreadySeen {
			continue
		}
		seenPaths[cleanSettingsPath] = struct{}{}
		settingsPaths = append(settingsPaths, cleanSettingsPath)
	}
	return settingsPaths
}

func installLoopgateHookScripts(repoHooksDir string, claudeHooksDir string) ([]string, error) {
	if err := validateLoopgateHookBundle(repoHooksDir); err != nil {
		return nil, err
	}
	copiedScripts := make([]string, 0, len(loopgateHookBundleFiles))
	for _, scriptName := range loopgateHookBundleFiles {
		sourcePath := filepath.Join(repoHooksDir, scriptName)
		destinationPath := filepath.Join(claudeHooksDir, scriptName)
		if err := copyFile(sourcePath, destinationPath); err != nil {
			return nil, fmt.Errorf("copy %s: %w", scriptName, err)
		}
		copiedScripts = append(copiedScripts, scriptName)
	}
	return copiedScripts, nil
}

func validateLoopgateHookBundle(repoHooksDir string) error {
	repoHooksInfo, err := os.Stat(repoHooksDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("repo hook bundle missing at %s (expected tracked hook sources under %s)", repoHooksDir, loopgateHookBundleDir)
		}
		return fmt.Errorf("stat repo hook bundle: %w", err)
	}
	if !repoHooksInfo.IsDir() {
		return fmt.Errorf("repo hook bundle path %s is not a directory", repoHooksDir)
	}
	return nil
}

func copyFile(sourcePath string, destinationPath string) error {
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	sourceInfo, err := sourceFile.Stat()
	if err != nil {
		return err
	}
	if !sourceInfo.Mode().IsRegular() {
		return fmt.Errorf("source is not a regular file")
	}

	temporaryDestination, err := os.CreateTemp(filepath.Dir(destinationPath), filepath.Base(destinationPath)+".tmp-*")
	if err != nil {
		return err
	}
	temporaryDestinationPath := temporaryDestination.Name()
	cleanupTemporary := true
	defer func() {
		if cleanupTemporary {
			_ = os.Remove(temporaryDestinationPath)
		}
	}()
	defer temporaryDestination.Close()

	if err := temporaryDestination.Chmod(sourceInfo.Mode().Perm()); err != nil {
		return err
	}

	if _, err := io.Copy(temporaryDestination, sourceFile); err != nil {
		return err
	}
	if err := temporaryDestination.Sync(); err != nil {
		return err
	}
	if err := temporaryDestination.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryDestinationPath, destinationPath); err != nil {
		return err
	}
	cleanupTemporary = false
	return nil
}

func loadClaudeSettings(settingsPath string) (claudeSettings, error) {
	settingsConfig := claudeSettings{}
	settingsBytes, err := os.ReadFile(settingsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return settingsConfig, nil
		}
		return claudeSettings{}, fmt.Errorf("read %s: %w", settingsPath, err)
	}
	if strings.TrimSpace(string(settingsBytes)) == "" {
		return settingsConfig, nil
	}
	if err := json.Unmarshal(settingsBytes, &settingsConfig); err != nil {
		return claudeSettings{}, fmt.Errorf("decode %s: %w", settingsPath, err)
	}
	return settingsConfig, nil
}

func writeClaudeSettings(settingsPath string, settingsConfig claudeSettings) error {
	if settingsConfig.Hooks == nil {
		settingsConfig.Hooks = map[string][]claudeHookMatcherGroup{}
	}
	parentDir := filepath.Dir(settingsPath)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}
	settingsBytes, err := json.MarshalIndent(settingsConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", settingsPath, err)
	}
	settingsBytes = append(settingsBytes, '\n')
	if err := os.WriteFile(settingsPath, settingsBytes, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", settingsPath, err)
	}
	return nil
}

func applyLoopgateHookSettings(settingsConfig *claudeSettings, claudeHooksDir string) int {
	if settingsConfig.Hooks == nil {
		settingsConfig.Hooks = map[string][]claudeHookMatcherGroup{}
	}
	removeLoopgateHookSettings(settingsConfig)
	installedHooks := 0
	for _, hookSpec := range loopgateHookEvents {
		commandPath := filepath.Join(claudeHooksDir, hookSpec.ScriptName)
		hookAction := claudeHookAction{
			Type:    "command",
			Command: "python3 " + shellQuoteHookCommandPath(commandPath),
		}
		matcherGroup := claudeHookMatcherGroup{
			Hooks: []claudeHookAction{hookAction},
		}
		if strings.TrimSpace(hookSpec.Matcher) != "" {
			matcherGroup.Matcher = hookSpec.Matcher
		}
		settingsConfig.Hooks[hookSpec.EventName] = append(settingsConfig.Hooks[hookSpec.EventName], matcherGroup)
		installedHooks++
	}
	normalizeClaudeHooks(settingsConfig)
	return installedHooks
}

func removeLoopgateHookSettings(settingsConfig *claudeSettings) int {
	if settingsConfig.Hooks == nil {
		return 0
	}
	removedHooks := 0
	for eventName, matcherGroups := range settingsConfig.Hooks {
		keptGroups := make([]claudeHookMatcherGroup, 0, len(matcherGroups))
		for _, matcherGroup := range matcherGroups {
			keptActions := make([]claudeHookAction, 0, len(matcherGroup.Hooks))
			for _, hookAction := range matcherGroup.Hooks {
				if isLoopgateHookCommand(hookAction.Command) {
					removedHooks++
					continue
				}
				keptActions = append(keptActions, hookAction)
			}
			if len(keptActions) == 0 {
				continue
			}
			matcherGroup.Hooks = keptActions
			keptGroups = append(keptGroups, matcherGroup)
		}
		if len(keptGroups) == 0 {
			delete(settingsConfig.Hooks, eventName)
			continue
		}
		settingsConfig.Hooks[eventName] = keptGroups
	}
	if len(settingsConfig.Hooks) == 0 {
		settingsConfig.Hooks = map[string][]claudeHookMatcherGroup{}
	}
	normalizeClaudeHooks(settingsConfig)
	return removedHooks
}

func isLoopgateHookCommand(command string) bool {
	trimmedCommand := strings.TrimSpace(command)
	if !strings.HasPrefix(trimmedCommand, "python3 ") {
		return false
	}
	scriptPath := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmedCommand, "python3 ")), `"'`)
	scriptBase := filepath.Base(scriptPath)
	for _, scriptName := range requiredLoopgateHookScripts {
		if scriptBase == scriptName {
			return true
		}
	}
	return false
}

func shellQuoteHookCommandPath(commandPath string) string {
	var quotedPath strings.Builder
	quotedPath.Grow(len(commandPath) + 2)
	quotedPath.WriteByte('"')
	for _, pathRune := range commandPath {
		switch pathRune {
		case '\\', '"', '$', '`':
			quotedPath.WriteByte('\\')
		}
		quotedPath.WriteRune(pathRune)
	}
	quotedPath.WriteByte('"')
	return quotedPath.String()
}

func normalizeClaudeHooks(settingsConfig *claudeSettings) {
	if settingsConfig.Hooks == nil {
		return
	}
	eventNames := make([]string, 0, len(settingsConfig.Hooks))
	for eventName := range settingsConfig.Hooks {
		eventNames = append(eventNames, eventName)
	}
	sort.Strings(eventNames)
	normalizedHooks := make(map[string][]claudeHookMatcherGroup, len(settingsConfig.Hooks))
	for _, eventName := range eventNames {
		normalizedHooks[eventName] = settingsConfig.Hooks[eventName]
	}
	settingsConfig.Hooks = normalizedHooks
}

func (settingsConfig *claudeSettings) UnmarshalJSON(rawBytes []byte) error {
	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal(rawBytes, &rawFields); err != nil {
		return err
	}
	settingsConfig.otherFields = make(map[string]json.RawMessage, len(rawFields))
	settingsConfig.Hooks = nil
	for fieldName, fieldValue := range rawFields {
		if fieldName == "hooks" {
			if len(fieldValue) == 0 || string(fieldValue) == "null" {
				continue
			}
			if err := decodeStrictJSON(fieldValue, &settingsConfig.Hooks); err != nil {
				return err
			}
			continue
		}
		settingsConfig.otherFields[fieldName] = fieldValue
	}
	return nil
}

func decodeStrictJSON(rawBytes []byte, target interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(rawBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.More() {
		return fmt.Errorf("unexpected trailing JSON value")
	}
	return nil
}

func (settingsConfig claudeSettings) MarshalJSON() ([]byte, error) {
	rawFields := make(map[string]json.RawMessage, len(settingsConfig.otherFields)+1)
	for fieldName, fieldValue := range settingsConfig.otherFields {
		rawFields[fieldName] = fieldValue
	}
	hooksValue := settingsConfig.Hooks
	if hooksValue == nil {
		hooksValue = map[string][]claudeHookMatcherGroup{}
	}
	hooksBytes, err := json.Marshal(hooksValue)
	if err != nil {
		return nil, err
	}
	rawFields["hooks"] = hooksBytes
	return json.Marshal(rawFields)
}
