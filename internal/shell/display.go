package shell

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type helpCommandEntry struct {
	Name string
	Args string
	Desc string
}

type approvalDisplayRequest struct {
	Tool    string
	Class   string
	Path    string
	Bytes   int
	Preview string
	Hidden  bool
	Reason  string
}

type policyDisplayConfig struct {
	Version               string
	ReadEnabled           bool
	WriteEnabled          bool
	WriteRequiresApproval bool
	AllowedRoots          []string
	DeniedPaths           []string
	LogCommands           bool
	LogToolCalls          bool
}

type selectOption struct {
	Value string
	Label string
	Desc  string
}

type panelOption struct{}

func withSingleBorder() panelOption  { return panelOption{} }
func withMinWidth(_ int) panelOption { return panelOption{} }

func renderHelpPanel(entries []helpCommandEntry) string {
	lines := make([]string, 0, len(entries)+2)
	lines = append(lines, "Commands:")
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Name)
		args := strings.TrimSpace(entry.Args)
		desc := strings.TrimSpace(entry.Desc)
		if args != "" {
			name += " " + args
		}
		if desc != "" {
			lines = append(lines, fmt.Sprintf("  %-28s %s", name, desc))
		} else {
			lines = append(lines, "  "+name)
		}
	}
	return renderPanel("HELP", nil, lines...)
}

func renderApproval(req approvalDisplayRequest) string {
	lines := []string{
		fmt.Sprintf("tool: %s", defaultDisplayValue(req.Tool, "unknown")),
		fmt.Sprintf("class: %s", defaultDisplayValue(req.Class, "unknown")),
	}
	if strings.TrimSpace(req.Path) != "" {
		lines = append(lines, fmt.Sprintf("path: %s", req.Path))
	}
	if req.Bytes > 0 {
		lines = append(lines, fmt.Sprintf("bytes: %d", req.Bytes))
	}
	if strings.TrimSpace(req.Reason) != "" {
		lines = append(lines, fmt.Sprintf("reason: %s", req.Reason))
	}
	switch {
	case req.Hidden:
		lines = append(lines, "preview: hidden")
	case strings.TrimSpace(req.Preview) != "":
		lines = append(lines, fmt.Sprintf("preview: %s", req.Preview))
	}
	lines = append(lines, "approve? [y/N]")
	return renderPanel("APPROVAL REQUIRED", nil, lines...)
}

func renderPolicySummary(cfg policyDisplayConfig) string {
	lines := []string{
		fmt.Sprintf("version: %s", defaultDisplayValue(cfg.Version, "unset")),
		fmt.Sprintf("filesystem_read_enabled: %t", cfg.ReadEnabled),
		fmt.Sprintf("filesystem_write_enabled: %t", cfg.WriteEnabled),
		fmt.Sprintf("filesystem_write_requires_approval: %t", cfg.WriteRequiresApproval),
		fmt.Sprintf("allowed_roots: %s", formatList(cfg.AllowedRoots, "none")),
		fmt.Sprintf("denied_paths: %s", formatList(cfg.DeniedPaths, "none")),
		fmt.Sprintf("log_commands: %t", cfg.LogCommands),
		fmt.Sprintf("log_tool_calls: %t", cfg.LogToolCalls),
	}
	return renderPanel("POLICY", nil, lines...)
}

func wizardHeader() string {
	return renderPanel("MODEL SETUP", nil,
		"Configure the model runtime through Loopgate validation.",
		"Press Enter to accept the default value shown for each prompt.",
	)
}

func wizardSummary(lines []string) string {
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		filtered = append(filtered, line)
	}
	return renderPanel("SETUP COMPLETE", nil, filtered...)
}

func commandPrompt(_ int) string {
	return "> "
}

func approvalPrompt(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		label = "request"
	}
	return fmt.Sprintf("approve %s? [y/N]: ", label)
}

func renderManKV(key, value string) string {
	return fmt.Sprintf("%s: %s", key, value)
}

func renderPanel(title string, _ []panelOption, lines ...string) string {
	width := len(title) + 4
	for _, line := range lines {
		if l := len(line) + 2; l > width {
			width = l
		}
	}
	if width < 28 {
		width = 28
	}

	top := "┌" + strings.Repeat("─", width) + "┐"
	bottom := "└" + strings.Repeat("─", width) + "┘"
	body := make([]string, 0, len(lines)+1)
	body = append(body, fmt.Sprintf("│ %-*s │", width-1, title))
	for _, line := range lines {
		body = append(body, fmt.Sprintf("│ %-*s │", width-1, line))
	}
	return strings.Join(append(append([]string{top}, body...), bottom), "\n")
}

func divider() string {
	return strings.Repeat("─", 40)
}

func dim(text string) string    { return text }
func teal(text string) string   { return text }
func purple(text string) string { return text }
func white(text string) string  { return text }
func amber(text string) string  { return text }
func red(text string) string    { return text }

func selectMenu(title string, options []selectOption, defaultIdx int) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("no options available")
	}
	if defaultIdx < 0 || defaultIdx >= len(options) {
		defaultIdx = 0
	}
	fmt.Println(title)
	for index, option := range options {
		marker := " "
		if index == defaultIdx {
			marker = "*"
		}
		line := fmt.Sprintf("  %s %d. %s", marker, index+1, defaultDisplayValue(option.Label, option.Value))
		if strings.TrimSpace(option.Desc) != "" {
			line += " - " + option.Desc
		}
		fmt.Println(line)
	}
	fmt.Printf("Select [default %d]: ", defaultIdx+1)
	reader := bufio.NewReader(os.Stdin)
	rawChoice, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	trimmedChoice := strings.TrimSpace(rawChoice)
	if trimmedChoice == "" {
		return options[defaultIdx].Value, nil
	}
	selectedIndex, err := strconv.Atoi(trimmedChoice)
	if err != nil || selectedIndex < 1 || selectedIndex > len(options) {
		return "", fmt.Errorf("invalid selection %q", trimmedChoice)
	}
	return options[selectedIndex-1].Value, nil
}

func defaultDisplayValue(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
