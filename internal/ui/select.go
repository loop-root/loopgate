package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// SelectOption represents a single choice in an interactive select menu.
type SelectOption struct {
	Value string // internal value returned on selection
	Label string // display name shown to the user
	Desc  string // short description shown beside the label
}

// SelectMenu renders an interactive arrow-key selection menu on the terminal.
// Returns the Value of the selected option, or an error on cancellation/IO failure.
//
// Uses the charmbracelet/huh library for correct terminal input handling.
// Falls back to a simple numbered prompt if the terminal is not interactive.
func SelectMenu(title string, options []SelectOption, defaultIdx int) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("no options provided")
	}
	if defaultIdx < 0 || defaultIdx >= len(options) {
		defaultIdx = 0
	}

	if !colorable {
		return selectFallback(title, options, defaultIdx)
	}

	huhOptions := make([]huh.Option[string], len(options))
	for i, opt := range options {
		label := opt.Label
		if opt.Desc != "" {
			label = opt.Label + "  " + opt.Desc
		}
		huhOptions[i] = huh.NewOption(label, opt.Value)
	}

	var selectedValue string
	selectField := huh.NewSelect[string]().
		Title(title).
		Options(huhOptions...).
		Value(&selectedValue)

	// Set the default cursor position.
	if defaultIdx >= 0 && defaultIdx < len(options) {
		selectField = selectField.Value(&selectedValue)
		selectedValue = options[defaultIdx].Value
	}

	form := huh.NewForm(huh.NewGroup(selectField)).
		WithTheme(morphHuhTheme())

	if err := form.Run(); err != nil {
		return "", fmt.Errorf("selection cancelled")
	}

	return selectedValue, nil
}

// morphHuhTheme returns a huh theme matching morph's color palette.
func morphHuhTheme() *huh.Theme {
	theme := huh.ThemeBase()

	// morph brand colors as lipgloss styles.
	pink := lipgloss.NewStyle().Foreground(lipgloss.Color("169"))
	teal := lipgloss.NewStyle().Foreground(lipgloss.Color("51"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("84"))
	white := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))

	// Title: dimmed like the rest of the wizard.
	theme.Focused.Title = dim
	theme.Blurred.Title = dim

	// Selected option indicator.
	theme.Focused.SelectSelector = pink.SetString("▸ ")
	theme.Focused.SelectedOption = white
	theme.Focused.UnselectedOption = dim

	// After submission.
	theme.Focused.SelectedPrefix = green.SetString("✓ ")
	theme.Focused.UnselectedPrefix = lipgloss.NewStyle().SetString("  ")

	// Option styling.
	theme.Focused.Option = dim

	// Base styling — no border or extra chrome.
	theme.Focused.Base = lipgloss.NewStyle().PaddingLeft(2)
	theme.Blurred.Base = lipgloss.NewStyle().PaddingLeft(2)

	// Next/submit button — hide it.
	theme.Focused.Next = lipgloss.NewStyle()
	theme.Blurred.Next = lipgloss.NewStyle()

	// Description styling.
	theme.Focused.Description = teal

	return theme
}

func selectFallback(title string, options []SelectOption, defaultIdx int) (string, error) {
	fmt.Println()
	fmt.Println("  " + title)
	for i, opt := range options {
		marker := "  "
		if i == defaultIdx {
			marker = "▸ "
		}
		fmt.Printf("  %s%d. %s — %s\n", marker, i+1, opt.Label, opt.Desc)
	}
	fmt.Println()
	return options[defaultIdx].Value, nil
}

// WizardStep renders a styled wizard step prompt label.
func WizardStep(stepNum int, total int, label string) string {
	counter := Dim(fmt.Sprintf("[%d/%d]", stepNum, total))
	return fmt.Sprintf("  %s %s %s", Pink("◈"), counter, Teal(label))
}

// WizardHeader renders the setup wizard header.
func WizardHeader() string {
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(RenderBox("MODEL SETUP", []string{
		blankLine(),
		"  " + Dim("Configure your AI model connection."),
		"  " + Dim("Only non-secret settings are saved locally."),
		"  " + Dim("Loopgate validates secrets before activation."),
		blankLine(),
	}, SingleBorder(), 52, Magenta))
	return sb.String()
}

// WizardSummary renders the post-setup summary.
func WizardSummary(lines []string) string {
	var content []string
	content = append(content, blankLine())
	for _, line := range lines {
		content = append(content, "  "+line)
	}
	content = append(content, blankLine())
	return RenderBox(Green("✓")+" SETUP COMPLETE", content, SingleBorder(), 60, Green)
}
