package ui

import (
	"fmt"
	"strings"
)

// ── Startup Banner ────────────────────────────────────────────────────────────

// BannerConfig holds the data displayed in the startup banner.
// All fields are pre-validated strings — no raw secrets, no model output.
type BannerConfig struct {
	Logo        LogoStyle
	PersonaName string
	Version     string
	SessionID   string
	WriteMode   string // e.g. "approval" | "enabled" | "disabled"
	ReadMode    string // e.g. "enabled"  | "disabled"
}

// Banner renders the morph startup panel.
// Color scheme: logo in pink, border in magenta, metadata in teal/dim.
func Banner(cfg BannerConfig) string {
	logoRows := logoLines(cfg.Logo)
	preColored := logoPreColored(cfg.Logo)

	// Indent the logo by 2 spaces for breathing room inside the border.
	indent := "  "
	var contentLines []string
	contentLines = append(contentLines, blankLine())
	for _, row := range logoRows {
		if preColored {
			contentLines = append(contentLines, indent+row)
		} else {
			contentLines = append(contentLines, indent+Pink(row))
		}
	}
	contentLines = append(contentLines, blankLine())
	contentLines = append(contentLines, indent+Dim("capability-governed AI orchestrator  ·  v"+cfg.Version))
	contentLines = append(contentLines, blankLine())

	contentLines = append(contentLines, indent+
		Dim("persona  ")+"▸  "+Teal(cfg.PersonaName))
	contentLines = append(contentLines, indent+
		Dim("session  ")+"▸  "+Teal(cfg.SessionID))

	writeBadge := Amber("writes:" + cfg.WriteMode)
	readBadge := Green("reads:" + cfg.ReadMode)
	contentLines = append(contentLines, indent+
		Dim("policy   ")+"▸  "+writeBadge+"  ·  "+readBadge)
	contentLines = append(contentLines, blankLine())

	// Choose box width: wide enough for logo + padding.
	width := boxWidth(logoRows, 68)

	return RenderBox("", contentLines, DoubleBorder(), width, Magenta)
}

// ── Prompt ────────────────────────────────────────────────────────────────────

// Prompt returns the readline prompt string.
// Format: "◈ morph [t:N] › "
// The turn count gives operators a sense of session progression.
func Prompt(turn int) string {
	diamond := Pink("◈")
	name := Pink("morph")
	counter := Dim(fmt.Sprintf("[t:%d]", turn))
	arrow := Teal("›")
	return fmt.Sprintf("%s %s %s %s ", diamond, name, counter, arrow)
}

// ApprovalPrompt returns the prompt string for a write/tool approval question.
func ApprovalPrompt(action string) string {
	label := Amber("approve " + action + "?")
	choices := Dim("[y/N]")
	arrow := Teal("▸")
	return fmt.Sprintf("%s %s %s ", label, choices, arrow)
}

// ── Help Panel ────────────────────────────────────────────────────────────────

// HelpCommandEntry describes a single command row for the help panel.
type HelpCommandEntry struct {
	Name string
	Args string
	Desc string
}

// HelpPanel renders the full /help command panel from a list of command entries.
// Commands are grouped by visual sections with blank-line separators between groups.
func HelpPanel(commands []HelpCommandEntry) string {
	cmd := func(name, args, desc string) string {
		namePart := Teal(padRight(name, 14))
		argPart := ""
		if args != "" {
			argPart = Purple(padRight(args, 18)) + " "
		} else {
			argPart = padRight("", 19)
		}
		return "  " + namePart + " " + argPart + Dim(desc)
	}

	sessionGroup := []string{"/help", "/man", "/exit", "/quit", "/reset"}
	fileGroup := []string{"/pwd", "/ls", "/cat", "/write"}
	modelGroup := []string{"/setup", "/model", "/policy", "/agent", "/persona", "/settings"}
	toolGroup := []string{"/tools", "/config", "/network", "/connections"}
	workflowGroup := []string{"/goal", "/todo", "/memory"}
	sandboxGroup := []string{"/sandbox", "/quarantine", "/site"}
	debugGroup := []string{"/debug"}

	groups := [][]string{sessionGroup, fileGroup, modelGroup, toolGroup, workflowGroup, sandboxGroup, debugGroup}

	// Build lookup from command entries
	lookup := make(map[string]HelpCommandEntry, len(commands))
	for _, entry := range commands {
		lookup[entry.Name] = entry
	}

	lines := []string{blankLine()}
	for gi, group := range groups {
		emitted := false
		for _, name := range group {
			entry, found := lookup[name]
			if !found {
				continue
			}
			lines = append(lines, cmd(entry.Name, entry.Args, entry.Desc))
			emitted = true
		}
		if emitted && gi < len(groups)-1 {
			lines = append(lines, blankLine())
		}
	}

	// Emit any remaining commands not in a group
	grouped := make(map[string]bool)
	for _, group := range groups {
		for _, name := range group {
			grouped[name] = true
		}
	}
	hasExtra := false
	for _, entry := range commands {
		if !grouped[entry.Name] {
			if !hasExtra {
				lines = append(lines, blankLine())
				hasExtra = true
			}
			lines = append(lines, cmd(entry.Name, entry.Args, entry.Desc))
		}
	}

	lines = append(lines, blankLine())
	lines = append(lines, "  "+Dim("Tip: /man <command> or /<command> --help for detailed usage"))
	lines = append(lines, blankLine())

	return RenderBox("COMMANDS", lines, SingleBorder(), 76, Magenta)
}

// ── Policy Panel ─────────────────────────────────────────────────────────────

// PolicyConfig carries the display-safe fields extracted from config.Policy.
// Callers must not pass raw secret values here.
type PolicyConfig struct {
	Version               string
	ReadEnabled           bool
	WriteEnabled          bool
	WriteRequiresApproval bool
	AllowedRoots          []string
	DeniedPaths           []string
	LogCommands           bool
	LogToolCalls          bool
	LogMemoryPromotions   bool
	AutoDistillate        bool
}

// Policy renders the /policy command panel.
func Policy(cfg PolicyConfig) string {
	boolVal := func(b bool) string {
		if b {
			return Green("✓ enabled")
		}
		return Dim("✗ disabled")
	}

	row := func(label, value string) string {
		return "  " + Dim(fmt.Sprintf("%-28s", label)) + value
	}

	lines := []string{
		blankLine(),
		row("version", Teal(cfg.Version)),
		blankLine(),
		"  " + Dim("filesystem"),
		row("  reads", boolVal(cfg.ReadEnabled)),
	}

	writeVal := boolVal(cfg.WriteEnabled)
	if cfg.WriteEnabled && cfg.WriteRequiresApproval {
		writeVal += Amber("  (approval required)")
	}
	lines = append(lines, row("  writes", writeVal))

	if len(cfg.AllowedRoots) > 0 {
		lines = append(lines, row("  allowed roots",
			Teal(strings.Join(cfg.AllowedRoots, "  "))))
	}
	if len(cfg.DeniedPaths) > 0 {
		lines = append(lines, row("  denied paths",
			Dim(strings.Join(cfg.DeniedPaths, "  "))))
	}

	lines = append(lines, blankLine())
	lines = append(lines, "  "+Dim("logging"))
	lines = append(lines, row("  commands", boolVal(cfg.LogCommands)))
	lines = append(lines, row("  tool calls", boolVal(cfg.LogToolCalls)))
	lines = append(lines, row("  memory promotions", boolVal(cfg.LogMemoryPromotions)))

	lines = append(lines, blankLine())
	lines = append(lines, "  "+Dim("memory"))
	lines = append(lines, row("  auto-distillate", boolVal(cfg.AutoDistillate)))
	lines = append(lines, blankLine())

	return RenderBox("POLICY", lines, SingleBorder(), 66, Magenta)
}

// ── Approval Card ─────────────────────────────────────────────────────────────

// ApprovalRequest carries display-safe fields for the approval UI.
// Content and preview must be pre-redacted by the caller.
type ApprovalRequest struct {
	Tool        string // e.g. "write", "shell", "http"
	Class       string // product-scoped approval class label
	Path        string // resolved path (filesystem ops)
	ResolvedAbs string // absolute resolved path
	Bytes       int    // content size in bytes
	Preview     string // redacted content preview (never raw secret content)
	Hidden      bool   // true if preview was suppressed due to sensitive content
	Reason      string // optional model-provided reason (pre-redacted)
}

// Approval renders the approval request card for a policy-gated tool call.
func Approval(req ApprovalRequest) string {
	row := func(label, value string) string {
		return "  " + Amber(fmt.Sprintf("%-12s", label)) + "▸  " + value
	}

	lines := []string{blankLine()}
	if req.Class != "" {
		lines = append(lines, row("class", Teal(req.Class)))
	}
	if req.Path != "" {
		lines = append(lines, row("path", White(req.Path)))
	}
	if req.ResolvedAbs != "" {
		lines = append(lines, row("resolved", Dim(req.ResolvedAbs)))
	}
	if req.Bytes > 0 {
		lines = append(lines, row("bytes", Dim(fmt.Sprintf("%d", req.Bytes))))
	}
	if req.Hidden {
		lines = append(lines, row("preview", Dim("[redacted — possible secret content]")))
	} else if req.Preview != "" {
		lines = append(lines, row("preview", White(fmt.Sprintf("%q", req.Preview))))
	}
	if req.Reason != "" {
		lines = append(lines, row("reason", Dim(req.Reason)))
	}
	lines = append(lines, blankLine())

	title := Amber("⚠  " + strings.ToUpper(req.Tool) + " APPROVAL")
	return RenderBox(title, lines, DoubleBorder(), 66, Amber)
}

// ── Outcome Lines ─────────────────────────────────────────────────────────────

// Denial renders a single-line policy denial message.
// reason must be pre-redacted — it must not contain secret material.
func Denial(reason string) string {
	return Red("✗") + " " + Amber("denied") + "  ·  " + reason
}

// Allow renders a single-line approval/allow confirmation.
func Allow(msg string) string {
	return Green("✓") + " " + Green("allowed") + "  ·  " + msg
}

// Warn renders a single-line warning.
func Warn(msg string) string {
	return Amber("⚠") + "  " + msg
}

// ── Session End Panel ─────────────────────────────────────────────────────────

// SessionEndConfig carries session summary data.
type SessionEndConfig struct {
	SessionID  string
	Turns      int
	ExitReason string
	CapsuleOK  bool
}

// SessionEnd renders the shutdown summary panel.
func SessionEnd(cfg SessionEndConfig) string {
	capsuleStatus := StatusOK("written")
	if !cfg.CapsuleOK {
		capsuleStatus = StatusWarn("skipped")
	}

	return PanelWithOptions("SESSION END",
		[]PanelOption{WithSingleBorder(), WithMinWidth(40)},
		Divider(),
		KV("session", Teal(cfg.SessionID)),
		KV("turns", Teal(fmt.Sprintf("%d", cfg.Turns))),
		KV("reason", Dim(cfg.ExitReason)),
		KV("capsule", capsuleStatus),
		Divider(),
	)
}

// ── Intent Indicator ──────────────────────────────────────────────────────────

// IntentIndicator renders a visual indicator for auto-detected natural language
// commands. Format: "  → /command args" in dimmed arrow + teal command.
func IntentIndicator(command string) string {
	return Dim("  → ") + Teal(command)
}

// ── Welcome Message ──────────────────────────────────────────────────────────

// WelcomeMessage renders the first-run welcome panel.
func WelcomeMessage() string {
	lines := []string{
		blankLine(),
		"  Welcome to Morph! Let's connect you to an AI model.",
		"  This only takes a minute. You can re-run /setup any time.",
		blankLine(),
	}
	return RenderBox("", lines, SingleBorder(), 60, Magenta)
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// boxWidth returns the max visible line length across logoRows, plus padding,
// clamped to at least minWidth and at most TermWidth().
func boxWidth(logoRows []string, minWidth int) int {
	maxLogo := 0
	for _, row := range logoRows {
		if vl := visibleLen(row); vl > maxLogo {
			maxLogo = vl
		}
	}
	// Add 2 for indent + 2 for border chars + a little breathing room.
	w := maxLogo + 6
	if w < minWidth {
		w = minWidth
	}
	tw := TermWidth()
	if w > tw {
		w = tw
	}
	return w
}
