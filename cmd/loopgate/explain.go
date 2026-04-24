package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"loopgate/internal/loopgate"
	controlapipkg "loopgate/internal/loopgate/controlapi"
)

func runExplain(args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("explain", flag.ContinueOnError)
	fs.SetOutput(stderr)

	defaultRepoRoot, err := resolveLoopgateRepoRoot("")
	if err != nil {
		return err
	}

	repoRootFlag := fs.String("repo-root", defaultRepoRoot, "repository root containing Loopgate config and signed policy files")
	toolFlag := fs.String("tool", "", "Claude Code tool name to explain (for example: Grep, Write, Bash)")
	pathFlag := fs.String("path", "", "target path for filesystem/search tools")
	commandFlag := fs.String("command", "", "shell command for Bash")
	urlFlag := fs.String("url", "", "target URL for WebFetch")
	cwdFlag := fs.String("cwd", "", "working directory for path resolution (default: repo root)")
	jsonFlag := fs.Bool("json", false, "print machine-readable JSON")
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
	toolName := strings.TrimSpace(*toolFlag)
	if toolName == "" {
		return fmt.Errorf("-tool is required")
	}
	cwd := strings.TrimSpace(*cwdFlag)
	if cwd == "" {
		cwd = repoRoot
	}

	toolInput, err := explainToolInput(toolName, strings.TrimSpace(*pathFlag), strings.TrimSpace(*commandFlag), strings.TrimSpace(*urlFlag))
	if err != nil {
		return err
	}
	request := controlapipkg.HookPreValidateRequest{
		HookEventName: "PreToolUse",
		ToolName:      toolName,
		ToolInput:     toolInput,
		CWD:           cwd,
	}
	response, err := loopgate.ExplainClaudeCodeHookDecision(repoRoot, request)
	if err != nil {
		return err
	}

	if *jsonFlag {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(response)
	}
	printExplainResponse(stdout, repoRoot, cwd, toolName, toolInput, response)
	return nil
}

func explainToolInput(toolName string, targetPath string, command string, targetURL string) (map[string]interface{}, error) {
	switch toolName {
	case "Read", "Write", "Edit", "MultiEdit":
		if targetPath == "" {
			return nil, fmt.Errorf("-path is required for %s", toolName)
		}
		return map[string]interface{}{"file_path": targetPath}, nil
	case "Grep", "Glob":
		if targetPath == "" {
			return nil, fmt.Errorf("-path is required for %s", toolName)
		}
		return map[string]interface{}{"path": targetPath}, nil
	case "Bash":
		if command == "" {
			return nil, fmt.Errorf("-command is required for Bash")
		}
		return map[string]interface{}{"command": command}, nil
	case "WebFetch":
		if targetURL == "" {
			return nil, fmt.Errorf("-url is required for WebFetch")
		}
		return map[string]interface{}{"url": targetURL}, nil
	default:
		return map[string]interface{}{}, nil
	}
}

func printExplainResponse(stdout io.Writer, repoRoot string, cwd string, toolName string, toolInput map[string]interface{}, response controlapipkg.HookPreValidateResponse) {
	fmt.Fprintln(stdout, "explain OK")
	fmt.Fprintf(stdout, "repo_root: %s\n", repoRoot)
	fmt.Fprintf(stdout, "cwd: %s\n", filepath.Clean(cwd))
	fmt.Fprintf(stdout, "tool: %s\n", toolName)
	if targetPath, _ := toolInput["file_path"].(string); targetPath != "" {
		fmt.Fprintf(stdout, "target.path: %s\n", targetPath)
	}
	if targetPath, _ := toolInput["path"].(string); targetPath != "" {
		fmt.Fprintf(stdout, "target.path: %s\n", targetPath)
	}
	if command, _ := toolInput["command"].(string); command != "" {
		fmt.Fprintf(stdout, "target.command: %s\n", command)
	}
	if targetURL, _ := toolInput["url"].(string); targetURL != "" {
		fmt.Fprintf(stdout, "target.url: %s\n", targetURL)
	}
	fmt.Fprintf(stdout, "decision: %s\n", response.Decision)
	if response.ReasonCode != "" {
		fmt.Fprintf(stdout, "reason_code: %s\n", response.ReasonCode)
	}
	if response.DenialCode != "" {
		fmt.Fprintf(stdout, "denial_code: %s\n", response.DenialCode)
	}
	if response.Reason != "" {
		fmt.Fprintf(stdout, "reason: %s\n", response.Reason)
	}
	if response.ApprovalOwner != "" {
		fmt.Fprintf(stdout, "approval_owner: %s\n", response.ApprovalOwner)
	}
	if len(response.ApprovalOptions) > 0 {
		fmt.Fprintf(stdout, "approval_options: %s\n", strings.Join(response.ApprovalOptions, ","))
	}
	if response.OperatorOverrideClass != "" {
		fmt.Fprintf(stdout, "operator_override.class: %s\n", response.OperatorOverrideClass)
	}
	if response.OperatorOverrideMaxDelegation != "" {
		fmt.Fprintf(stdout, "operator_override.max_delegation: %s\n", response.OperatorOverrideMaxDelegation)
	}
}
