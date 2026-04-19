package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const loopgateLaunchAgentLabelPrefix = "com.loopgate"

type launchAgentInstallOptions struct {
	RepoRoot        string
	BinaryPath      string
	LaunchAgentsDir string
	Label           string
	LoadImmediately bool
}

type launchAgentInstallResult struct {
	Label             string
	PlistPath         string
	BinaryPath        string
	SocketPath        string
	StandardOutPath   string
	StandardErrorPath string
	Loaded            bool
}

type launchAgentDependencies struct {
	Platform       string
	UserUID        int
	ExecutablePath func() (string, error)
	UserHomeDir    func() (string, error)
	RunLaunchctl   func(args ...string) error
}

func runInstallLaunchAgent(args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("install-launch-agent", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoRootFlag := fs.String("repo-root", "", "repository root that Loopgate should serve from")
	binaryPathFlag := fs.String("binary-path", "", "Loopgate server binary path (default: current executable)")
	launchAgentsDirFlag := fs.String("launch-agents-dir", "", "LaunchAgents directory (default: ~/Library/LaunchAgents)")
	labelFlag := fs.String("label", "", "launch agent label override")
	loadFlag := fs.Bool("load", false, "load and start the launch agent immediately with launchctl")
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
	result, err := installLaunchAgent(launchAgentInstallOptions{
		RepoRoot:        repoRoot,
		BinaryPath:      strings.TrimSpace(*binaryPathFlag),
		LaunchAgentsDir: strings.TrimSpace(*launchAgentsDirFlag),
		Label:           strings.TrimSpace(*labelFlag),
		LoadImmediately: *loadFlag,
	}, defaultLaunchAgentDependencies())
	if err != nil {
		return err
	}

	fmt.Fprintln(stdout, "launch agent install OK")
	fmt.Fprintf(stdout, "label: %s\n", result.Label)
	fmt.Fprintf(stdout, "plist_path: %s\n", result.PlistPath)
	fmt.Fprintf(stdout, "binary_path: %s\n", result.BinaryPath)
	fmt.Fprintf(stdout, "socket_path: %s\n", result.SocketPath)
	fmt.Fprintf(stdout, "stdout_path: %s\n", result.StandardOutPath)
	fmt.Fprintf(stdout, "stderr_path: %s\n", result.StandardErrorPath)
	fmt.Fprintf(stdout, "loaded: %t\n", result.Loaded)
	return nil
}

func defaultLaunchAgentDependencies() launchAgentDependencies {
	return launchAgentDependencies{
		Platform:       runtime.GOOS,
		UserUID:        os.Getuid(),
		ExecutablePath: os.Executable,
		UserHomeDir:    os.UserHomeDir,
		RunLaunchctl:   runLaunchctlCommand,
	}
}

func installLaunchAgent(options launchAgentInstallOptions, deps launchAgentDependencies) (launchAgentInstallResult, error) {
	if deps.Platform != "darwin" {
		return launchAgentInstallResult{}, fmt.Errorf("install-launch-agent is only supported on macOS")
	}

	repoRoot := filepath.Clean(strings.TrimSpace(options.RepoRoot))
	if repoRoot == "" {
		return launchAgentInstallResult{}, fmt.Errorf("repo root must not be empty")
	}
	binaryPath, err := resolveLoopgateExecutablePath(options.BinaryPath, deps)
	if err != nil {
		return launchAgentInstallResult{}, err
	}
	launchAgentsDir, err := resolveLaunchAgentsDir(options.LaunchAgentsDir, deps)
	if err != nil {
		return launchAgentInstallResult{}, err
	}
	label := strings.TrimSpace(options.Label)
	if label == "" {
		label = defaultLoopgateLaunchAgentLabel(repoRoot)
	}
	socketPath := filepath.Join(repoRoot, "runtime", "state", "loopgate.sock")
	stdoutPath := filepath.Join(repoRoot, "runtime", "logs", "loopgate.stdout.log")
	stderrPath := filepath.Join(repoRoot, "runtime", "logs", "loopgate.stderr.log")
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return launchAgentInstallResult{}, fmt.Errorf("create runtime state directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(stdoutPath), 0o700); err != nil {
		return launchAgentInstallResult{}, fmt.Errorf("create runtime logs directory: %w", err)
	}
	if err := os.MkdirAll(launchAgentsDir, 0o755); err != nil {
		return launchAgentInstallResult{}, fmt.Errorf("create LaunchAgents directory: %w", err)
	}

	plistPath := filepath.Join(launchAgentsDir, label+".plist")
	plistBytes, err := renderLaunchAgentPlist(launchAgentPlistSpec{
		Label:             label,
		BinaryPath:        binaryPath,
		WorkingDirectory:  repoRoot,
		SocketPath:        socketPath,
		StandardOutPath:   stdoutPath,
		StandardErrorPath: stderrPath,
	})
	if err != nil {
		return launchAgentInstallResult{}, err
	}
	if err := writeLoopgateFileAtomically(plistPath, plistBytes, 0o644); err != nil {
		return launchAgentInstallResult{}, fmt.Errorf("write launch agent plist: %w", err)
	}

	if options.LoadImmediately {
		launchctlTarget := fmt.Sprintf("gui/%d", deps.UserUID)
		serviceTarget := fmt.Sprintf("%s/%s", launchctlTarget, label)
		_ = deps.RunLaunchctl("bootout", serviceTarget)
		if err := deps.RunLaunchctl("bootstrap", launchctlTarget, plistPath); err != nil {
			return launchAgentInstallResult{}, err
		}
		if err := deps.RunLaunchctl("kickstart", "-k", serviceTarget); err != nil {
			return launchAgentInstallResult{}, err
		}
	}

	return launchAgentInstallResult{
		Label:             label,
		PlistPath:         plistPath,
		BinaryPath:        binaryPath,
		SocketPath:        socketPath,
		StandardOutPath:   stdoutPath,
		StandardErrorPath: stderrPath,
		Loaded:            options.LoadImmediately,
	}, nil
}

func resolveLoopgateExecutablePath(flagValue string, deps launchAgentDependencies) (string, error) {
	binaryPath := strings.TrimSpace(flagValue)
	if binaryPath == "" {
		var err error
		binaryPath, err = deps.ExecutablePath()
		if err != nil {
			return "", fmt.Errorf("determine current executable path: %w", err)
		}
	}
	if !filepath.IsAbs(binaryPath) {
		absoluteBinaryPath, err := filepath.Abs(binaryPath)
		if err != nil {
			return "", fmt.Errorf("resolve absolute binary path for %s: %w", binaryPath, err)
		}
		binaryPath = absoluteBinaryPath
	}
	binaryPath = filepath.Clean(binaryPath)
	if looksLikeGoRunExecutable(binaryPath) {
		return "", fmt.Errorf("binary path %s looks like a transient go run executable; run make build and use ./bin/loopgate install-launch-agent instead", binaryPath)
	}
	binaryInfo, err := os.Stat(binaryPath)
	if err != nil {
		return "", fmt.Errorf("stat loopgate binary %s: %w", binaryPath, err)
	}
	if binaryInfo.IsDir() {
		return "", fmt.Errorf("loopgate binary path %s is a directory", binaryPath)
	}
	return binaryPath, nil
}

func resolveLaunchAgentsDir(flagValue string, deps launchAgentDependencies) (string, error) {
	if trimmedFlagValue := strings.TrimSpace(flagValue); trimmedFlagValue != "" {
		return filepath.Clean(trimmedFlagValue), nil
	}
	homeDir, err := deps.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home directory for LaunchAgents: %w", err)
	}
	return filepath.Join(filepath.Clean(homeDir), "Library", "LaunchAgents"), nil
}

func looksLikeGoRunExecutable(binaryPath string) bool {
	cleanPath := filepath.Clean(binaryPath)
	return strings.Contains(cleanPath, string(filepath.Separator)+"go-build") &&
		strings.Contains(cleanPath, string(filepath.Separator)+"exe"+string(filepath.Separator))
}

func defaultLoopgateLaunchAgentLabel(repoRoot string) string {
	repoBaseName := sanitizeLaunchAgentLabelComponent(filepath.Base(filepath.Clean(repoRoot)))
	if repoBaseName == "" {
		repoBaseName = "repo"
	}
	repoRootHash := sha256.Sum256([]byte(filepath.Clean(repoRoot)))
	return fmt.Sprintf("%s.%s.%s", loopgateLaunchAgentLabelPrefix, repoBaseName, hex.EncodeToString(repoRootHash[:4]))
}

func sanitizeLaunchAgentLabelComponent(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	var builder strings.Builder
	for _, runeValue := range value {
		switch {
		case runeValue >= 'a' && runeValue <= 'z':
			builder.WriteRune(runeValue)
		case runeValue >= '0' && runeValue <= '9':
			builder.WriteRune(runeValue)
		case runeValue == '-' || runeValue == '_':
			builder.WriteRune(runeValue)
		default:
			builder.WriteRune('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}

type launchAgentPlistSpec struct {
	Label             string
	BinaryPath        string
	WorkingDirectory  string
	SocketPath        string
	StandardOutPath   string
	StandardErrorPath string
}

func renderLaunchAgentPlist(spec launchAgentPlistSpec) ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	buffer.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	buffer.WriteString(`<plist version="1.0">` + "\n")
	buffer.WriteString(`  <dict>` + "\n")
	writePlistKeyString(&buffer, "Label", spec.Label)
	buffer.WriteString("    <key>ProgramArguments</key>\n")
	buffer.WriteString("    <array>\n")
	writePlistString(&buffer, 6, spec.BinaryPath)
	buffer.WriteString("    </array>\n")
	writePlistKeyString(&buffer, "WorkingDirectory", spec.WorkingDirectory)
	buffer.WriteString("    <key>EnvironmentVariables</key>\n")
	buffer.WriteString("    <dict>\n")
	environmentVariables := map[string]string{
		loopgateRepoRootEnv: spec.WorkingDirectory,
		"LOOPGATE_SOCKET":   spec.SocketPath,
	}
	environmentKeys := make([]string, 0, len(environmentVariables))
	for environmentKey := range environmentVariables {
		environmentKeys = append(environmentKeys, environmentKey)
	}
	sort.Strings(environmentKeys)
	for _, environmentKey := range environmentKeys {
		writePlistKeyString(&buffer, environmentKey, environmentVariables[environmentKey])
	}
	buffer.WriteString("    </dict>\n")
	buffer.WriteString("    <key>RunAtLoad</key>\n")
	buffer.WriteString("    <true/>\n")
	buffer.WriteString("    <key>KeepAlive</key>\n")
	buffer.WriteString("    <true/>\n")
	writePlistKeyString(&buffer, "StandardOutPath", spec.StandardOutPath)
	writePlistKeyString(&buffer, "StandardErrorPath", spec.StandardErrorPath)
	buffer.WriteString("  </dict>\n")
	buffer.WriteString("</plist>\n")
	return buffer.Bytes(), nil
}

func writePlistKeyString(buffer *bytes.Buffer, key string, value string) {
	buffer.WriteString("    <key>")
	writeEscapedXML(buffer, key)
	buffer.WriteString("</key>\n")
	writePlistString(buffer, 4, value)
}

func writePlistString(buffer *bytes.Buffer, indentSpaces int, value string) {
	buffer.WriteString(strings.Repeat(" ", indentSpaces))
	buffer.WriteString("<string>")
	writeEscapedXML(buffer, value)
	buffer.WriteString("</string>\n")
}

func writeEscapedXML(buffer *bytes.Buffer, value string) {
	_ = xml.EscapeText(buffer, []byte(value))
}

func writeLoopgateFileAtomically(path string, data []byte, mode os.FileMode) error {
	parentDir := filepath.Dir(path)
	temporaryFile, err := os.CreateTemp(parentDir, ".loopgate-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	temporaryPath := temporaryFile.Name()
	if _, err := temporaryFile.Write(data); err != nil {
		_ = temporaryFile.Close()
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := temporaryFile.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(temporaryPath, mode); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

func runLaunchctlCommand(args ...string) error {
	command := exec.Command("launchctl", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		trimmedOutput := strings.TrimSpace(string(output))
		if trimmedOutput == "" {
			return fmt.Errorf("launchctl %s: %w", strings.Join(args, " "), err)
		}
		return fmt.Errorf("launchctl %s: %w: %s", strings.Join(args, " "), err, trimmedOutput)
	}
	return nil
}
