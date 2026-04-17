package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

const (
	defaultShellTimeout = 30 * time.Second
	maxOutputBytes      = 256 * 1024 // 256 KB
)

var hermeticShellPATHEntries = []string{
	// Bare allowlisted command names resolve only through this fixed PATH so an
	// attacker-controlled ambient PATH cannot shadow a permitted executable.
	"/opt/homebrew/bin",
	"/usr/local/bin",
	"/usr/local/go/bin",
	"/usr/bin",
	"/bin",
	"/usr/sbin",
	"/sbin",
}

// ShellExec runs shell commands in the sandbox workspace.
type ShellExec struct {
	WorkDir         string   // working directory for commands (sandbox root)
	AllowedCommands []string // explicit command-name or exact-path allowlist
}

func (t *ShellExec) Name() string      { return "shell_exec" }
func (t *ShellExec) Category() string  { return "shell" }
func (t *ShellExec) Operation() string { return OpExecute }

func (t *ShellExec) Schema() Schema {
	return Schema{
		Description: "Execute a single direct command and return its output. Shell control operators, pipelines, and implicit shell expansion are not available.",
		Args: []ArgDef{
			{
				Name:        "command",
				Description: "A single direct command line, parsed into argv without invoking /bin/sh",
				Required:    true,
				Type:        "string",
				MaxLen:      4096,
			},
		},
	}
}

func (t *ShellExec) ValidatePolicyArguments(args map[string]string) error {
	_, _, _, err := t.prepareCommand(args)
	return err
}

func (t *ShellExec) Execute(ctx context.Context, args map[string]string) (string, error) {
	commandLine, executablePath, argv, err := t.prepareCommand(args)
	if err != nil {
		return "", err
	}

	execCtx, cancel := context.WithTimeout(ctx, defaultShellTimeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, executablePath, argv[1:]...)
	cmd.Args = append([]string(nil), argv...)
	if t.WorkDir != "" {
		cmd.Dir = t.WorkDir
	}
	cmd.Env = minimalShellEnvironment(t.WorkDir, hermeticShellPATH())

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	// Build output combining stdout and stderr.
	var result strings.Builder
	if stdout.Len() > 0 {
		out := stdout.Bytes()
		if len(out) > maxOutputBytes {
			out = out[:maxOutputBytes]
			result.Write(out)
			result.WriteString("\n... (output truncated)")
		} else {
			result.Write(out)
		}
	}
	if stderr.Len() > 0 {
		if result.Len() > 0 {
			result.WriteString("\n")
		}
		errOut := stderr.Bytes()
		if len(errOut) > maxOutputBytes/2 {
			errOut = errOut[:maxOutputBytes/2]
			result.WriteString("STDERR: ")
			result.Write(errOut)
			result.WriteString("\n... (stderr truncated)")
		} else {
			result.WriteString("STDERR: ")
			result.Write(errOut)
		}
	}

	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			return result.String(), fmt.Errorf("command timed out after %s", defaultShellTimeout)
		}
		// Include exit code in output but don't fail — the model needs to see the error
		if result.Len() > 0 {
			return fmt.Sprintf("%s\nExit status: %v\nCommand: %s", result.String(), err, commandLine), nil
		}
		return fmt.Sprintf("Command failed: %v\nCommand: %s", err, commandLine), nil
	}

	if result.Len() == 0 {
		return "(no output)", nil
	}
	return result.String(), nil
}

func (t *ShellExec) prepareCommand(args map[string]string) (string, string, []string, error) {
	commandLine := strings.TrimSpace(args["command"])
	if commandLine == "" {
		return "", "", nil, fmt.Errorf("empty command")
	}

	argv, err := splitDirectCommandLine(commandLine)
	if err != nil {
		return commandLine, "", nil, err
	}
	executablePath, err := t.resolveAllowedCommandPath(argv[0])
	if err != nil {
		return commandLine, "", nil, err
	}
	return commandLine, executablePath, argv, nil
}

func (t *ShellExec) resolveAllowedCommandPath(commandName string) (string, error) {
	if len(t.AllowedCommands) == 0 {
		return "", fmt.Errorf("shell allowed_commands is empty; configure a direct-command allowlist before enabling shell")
	}

	trimmedCommandName := strings.TrimSpace(commandName)
	if trimmedCommandName == "" {
		return "", fmt.Errorf("shell command name is required")
	}
	if strings.HasPrefix(trimmedCommandName, "-") {
		return "", fmt.Errorf("shell command name %q is invalid", trimmedCommandName)
	}

	hasPathSeparator := strings.ContainsAny(trimmedCommandName, `/\`)
	normalizedCommandName := trimmedCommandName
	if hasPathSeparator {
		normalizedCommandName = filepath.Clean(trimmedCommandName)
	}

	for _, allowedCommand := range t.AllowedCommands {
		trimmedAllowedCommand := strings.TrimSpace(allowedCommand)
		if trimmedAllowedCommand == "" {
			continue
		}
		if strings.ContainsAny(trimmedAllowedCommand, `/\`) {
			if hasPathSeparator && filepath.Clean(trimmedAllowedCommand) == normalizedCommandName {
				return normalizedCommandName, nil
			}
			continue
		}
		if !hasPathSeparator && trimmedAllowedCommand == trimmedCommandName {
			resolvedCommandPath, err := resolveHermeticAllowedCommandPath(trimmedCommandName)
			if err != nil {
				return "", err
			}
			return resolvedCommandPath, nil
		}
	}

	return "", fmt.Errorf("shell command %q is not allowed by policy", trimmedCommandName)
}

func resolveHermeticAllowedCommandPath(commandName string) (string, error) {
	for _, searchDir := range hermeticShellPATHEntries {
		candidatePath := filepath.Join(searchDir, commandName)
		candidateInfo, err := os.Stat(candidatePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", fmt.Errorf("resolve shell command %q in %q: %w", commandName, searchDir, err)
		}
		if candidateInfo.IsDir() {
			continue
		}
		if candidateInfo.Mode()&0o111 == 0 {
			continue
		}
		return candidatePath, nil
	}
	return "", fmt.Errorf("shell command %q is allowed by policy but not found in Loopgate's fixed PATH", commandName)
}

func hermeticShellPATH() string {
	return strings.Join(hermeticShellPATHEntries, string(os.PathListSeparator))
}

func splitDirectCommandLine(rawCommand string) ([]string, error) {
	trimmedCommand := strings.TrimSpace(rawCommand)
	if trimmedCommand == "" {
		return nil, fmt.Errorf("empty command")
	}

	arguments := make([]string, 0, 8)
	var currentArgument strings.Builder
	inSingleQuotes := false
	inDoubleQuotes := false
	escapeNextRune := false

	flushCurrentArgument := func() {
		if currentArgument.Len() == 0 {
			return
		}
		arguments = append(arguments, currentArgument.String())
		currentArgument.Reset()
	}

	for _, currentRune := range rawCommand {
		if currentRune == '\n' || currentRune == '\r' {
			return nil, fmt.Errorf("shell control operators are not supported; execute one direct command at a time")
		}
		if currentRune != '\t' && unicode.IsControl(currentRune) {
			return nil, fmt.Errorf("control characters are not allowed in shell commands")
		}

		switch {
		case escapeNextRune:
			currentArgument.WriteRune(currentRune)
			escapeNextRune = false
		case inSingleQuotes:
			if currentRune == '\'' {
				inSingleQuotes = false
				continue
			}
			currentArgument.WriteRune(currentRune)
		case inDoubleQuotes:
			switch currentRune {
			case '"':
				inDoubleQuotes = false
			case '\\':
				escapeNextRune = true
			default:
				currentArgument.WriteRune(currentRune)
			}
		default:
			switch {
			case unicode.IsSpace(currentRune):
				flushCurrentArgument()
			case currentRune == '\'':
				inSingleQuotes = true
			case currentRune == '"':
				inDoubleQuotes = true
			case currentRune == '\\':
				escapeNextRune = true
			case strings.ContainsRune("|&;<>", currentRune):
				return nil, fmt.Errorf("shell control operators are not supported; execute one direct command at a time")
			default:
				currentArgument.WriteRune(currentRune)
			}
		}
	}

	if escapeNextRune {
		return nil, fmt.Errorf("shell command ends with an unfinished escape sequence")
	}
	if inSingleQuotes || inDoubleQuotes {
		return nil, fmt.Errorf("shell command contains an unterminated quote")
	}
	flushCurrentArgument()
	if len(arguments) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	return arguments, nil
}

func minimalShellEnvironment(workDir string, searchPath string) []string {
	envVars := make([]string, 0, 6)
	appendIfPresent := func(key string, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		envVars = append(envVars, key+"="+value)
	}

	appendIfPresent("PATH", searchPath)
	appendIfPresent("TMPDIR", os.Getenv("TMPDIR"))
	appendIfPresent("LANG", os.Getenv("LANG"))
	appendIfPresent("LC_ALL", os.Getenv("LC_ALL"))
	appendIfPresent("TERM", os.Getenv("TERM"))
	if strings.TrimSpace(workDir) != "" {
		appendIfPresent("HOME", workDir)
	} else {
		appendIfPresent("HOME", os.Getenv("HOME"))
	}
	appendIfPresent("SHELL", "/bin/sh")
	return envVars
}
