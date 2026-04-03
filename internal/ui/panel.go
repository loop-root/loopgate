package ui

import "strings"

// Color is a terminal color-application function.
// The named color helpers (Pink, Teal, Amber, Green, Red, Dim, …) all satisfy
// this type, so callers can pass them directly:
//
//	ui.Badge("approval", ui.Amber)
//	ui.PanelWithOptions("TITLE", []ui.PanelOption{ui.WithBorderColor(ui.Teal)}, …)
type Color = func(string) string

// ── Panel ─────────────────────────────────────────────────────────────────────

// PanelOption configures Panel rendering.
type PanelOption func(*panelConfig)

type panelConfig struct {
	border      Border
	titleColor  Color
	borderColor Color
	paddingX    int // horizontal padding (spaces) inside the left/right border
	minWidth    int // minimum inner width (border chars excluded)
}

func defaultPanelConfig() panelConfig {
	return panelConfig{
		border:      DoubleBorder(),
		titleColor:  Pink,
		borderColor: Magenta,
		paddingX:    1,
		minWidth:    32,
	}
}

// WithBorder sets a custom border style.
func WithBorder(b Border) PanelOption { return func(c *panelConfig) { c.border = b } }

// WithBorderColor sets the border color function.
func WithBorderColor(color Color) PanelOption { return func(c *panelConfig) { c.borderColor = color } }

// WithTitleColor sets the title color function.
func WithTitleColor(color Color) PanelOption { return func(c *panelConfig) { c.titleColor = color } }

// WithMinWidth sets the minimum inner panel width (border chars excluded).
func WithMinWidth(n int) PanelOption { return func(c *panelConfig) { c.minWidth = n } }

// WithSingleBorder switches to single-line border style.
func WithSingleBorder() PanelOption { return WithBorder(SingleBorder()) }

// Panel renders a bordered panel with auto-computed width.
// Lines should be built with KV, Badge, Status*, Divider, Row, or plain strings.
//
//	ui.Panel("POLICY",
//	    ui.KV("reads",  ui.StatusOK("enabled")),
//	    ui.KV("writes", ui.StatusWarn("approval required")),
//	    ui.Divider(),
//	    ui.KV("allowed", ". body persona core runtime"),
//	)
func Panel(title string, lines ...string) string {
	return PanelWithOptions(title, nil, lines...)
}

// PanelWithOptions renders a panel with custom PanelOptions.
func PanelWithOptions(title string, opts []PanelOption, lines ...string) string {
	cfg := defaultPanelConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return renderPanel(cfg, title, lines...)
}

func renderPanel(cfg panelConfig, title string, lines ...string) string {
	margin := strings.Repeat(" ", cfg.paddingX)

	// Apply horizontal margin to non-empty lines.
	padded := make([]string, len(lines))
	for i, line := range lines {
		if line == "" {
			padded[i] = "" // Divider — kept as blank line
		} else {
			padded[i] = margin + line + margin
		}
	}

	// Inner width must fit the title bar (2 fill + space + title + space = +4)
	// and every padded content line.
	innerWidth := visibleLen(title) + 4
	for _, line := range padded {
		if w := visibleLen(line); w > innerWidth {
			innerWidth = w
		}
	}
	if innerWidth < cfg.minWidth {
		innerWidth = cfg.minWidth
	}
	if tw := TermWidth() - 2; tw > 0 && innerWidth > tw {
		innerWidth = tw
	}

	return RenderBox(cfg.titleColor(title), padded, cfg.border, innerWidth+2, cfg.borderColor)
}

// ── Row primitives ────────────────────────────────────────────────────────────

// kvKeyWidth is the fixed visible width of the key column in KV rows.
// Long enough to align common keys (path, bytes, reads, writes, persona…).
const kvKeyWidth = 8

// KV renders an ANSI-aware aligned key-value row for use inside a Panel.
// The key is padded to kvKeyWidth visible chars before coloring so values align
// regardless of key length.
//
//	ui.KV("reads",  ui.StatusOK("enabled"))   →  "reads    ▸ ✓ enabled"
//	ui.KV("writes", ui.StatusWarn("approval")) →  "writes   ▸ ⚠ approval"
func KV(key, value string) string {
	return Dim(padRight(key, kvKeyWidth)) + " " + Teal("▸") + " " + value
}

// Badge renders a compact inline [text] chip in the given color.
// Pass nil for no color.
//
//	ui.Badge("approval", ui.Amber)  →  "[approval]" in amber
//	ui.Badge("enabled",  ui.Green)  →  "[enabled]"  in green
func Badge(text string, colorFn Color) string {
	s := "[" + text + "]"
	if colorFn == nil {
		return s
	}
	return colorFn(s)
}

// StatusOK renders a green "✓ text" for positive / allowed states.
func StatusOK(text string) string { return Green("✓ " + text) }

// StatusWarn renders an amber "⚠ text" for cautionary / approval-gated states.
func StatusWarn(text string) string { return Amber("⚠ " + text) }

// StatusDeny renders a red "✗ text" for denied / disabled states.
func StatusDeny(text string) string { return Red("✗ " + text) }

// Divider returns a blank semantic separator for use between sections in a Panel.
func Divider() string { return "" }

// Row joins columns with "  ·  " separators for compact inline summaries.
//
//	ui.Row(ui.Green("ok"), ui.Dim("32 bytes"))  →  "ok  ·  32 bytes"
func Row(cols ...string) string {
	return strings.Join(cols, "  ·  ")
}

// Paragraph word-wraps text into lines of at most width visible chars.
// Returns lines ready for use inside a Panel.
func Paragraph(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 || width <= 0 {
		return nil
	}
	var lines []string
	current := ""
	for _, word := range words {
		switch {
		case current == "":
			current = word
		case visibleLen(current)+1+visibleLen(word) <= width:
			current += " " + word
		default:
			lines = append(lines, current)
			current = word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// padRight pads a plain (uncolored) string with trailing spaces to the given
// visible width. Apply color AFTER padding to avoid ANSI byte inflation.
func padRight(s string, width int) string {
	if n := visibleLen(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}
