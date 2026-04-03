package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	defaultShellTimeout = 30 * time.Second
	maxOutputBytes      = 256 * 1024 // 256 KB
)

// ShellExec runs shell commands in the sandbox workspace.
type ShellExec struct {
	WorkDir string // working directory for commands (sandbox root)
}

func (t *ShellExec) Name() string      { return "shell_exec" }
func (t *ShellExec) Category() string  { return "shell" }
func (t *ShellExec) Operation() string { return OpExecute }

func (t *ShellExec) Schema() Schema {
	return Schema{
		Description: "Execute a shell command and return its output. Use this for running scripts, installing packages, compiling code, running tests, or any task that requires a command line.",
		Args: []ArgDef{
			{
				Name:        "command",
				Description: "The shell command to execute (passed to /bin/sh -c)",
				Required:    true,
				Type:        "string",
				MaxLen:      4096,
			},
		},
	}
}

func (t *ShellExec) Execute(ctx context.Context, args map[string]string) (string, error) {
	command := strings.TrimSpace(args["command"])
	if command == "" {
		return "", fmt.Errorf("empty command")
	}

	execCtx, cancel := context.WithTimeout(ctx, defaultShellTimeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "/bin/sh", "-c", command)
	if t.WorkDir != "" {
		cmd.Dir = t.WorkDir
	}
	cmd.Env = minimalShellEnvironment(t.WorkDir)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

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
			return fmt.Sprintf("%s\nExit status: %v", result.String(), err), nil
		}
		return fmt.Sprintf("Command failed: %v", err), nil
	}

	if result.Len() == 0 {
		return "(no output)", nil
	}
	return result.String(), nil
}

func minimalShellEnvironment(workDir string) []string {
	envVars := make([]string, 0, 6)
	appendIfPresent := func(key string, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		envVars = append(envVars, key+"="+value)
	}

	appendIfPresent("PATH", os.Getenv("PATH"))
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
