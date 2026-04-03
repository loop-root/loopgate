package ui

import "strings"

// Border defines the character set for a box-drawing frame.
// All fields are single-character strings to allow Unicode glyphs.
// Inspired by lipgloss's Border struct, but simplified and self-contained.
type Border struct {
	TopLeft     string
	Top         string
	TopRight    string
	Right       string
	BottomRight string
	Bottom      string
	BottomLeft  string
	Left        string
}

// Pre-defined border styles.

// DoubleBorder returns a double-line box (╔═╗║╚╝).
// Used for high-importance panels: banner, approval.
func DoubleBorder() Border {
	return Border{
		TopLeft: "╔", Top: "═", TopRight: "╗",
		Right: "║", BottomRight: "╝",
		Bottom: "═", BottomLeft: "╚",
		Left: "║",
	}
}

// SingleBorder returns a single-line box (┌─┐│└┘).
// Used for informational panels: help, policy, session end.
func SingleBorder() Border {
	return Border{
		TopLeft: "┌", Top: "─", TopRight: "┐",
		Right: "│", BottomRight: "┘",
		Bottom: "─", BottomLeft: "└",
		Left: "│",
	}
}

// HeavyBorder returns a heavy-line box (┏━┓┃┗┛).
// Used for denial and error panels.
func HeavyBorder() Border {
	return Border{
		TopLeft: "┏", Top: "━", TopRight: "┓",
		Right: "┃", BottomRight: "┛",
		Bottom: "━", BottomLeft: "┗",
		Left: "┃",
	}
}

// RenderBox renders content lines inside a bordered frame.
//
//	title       — text embedded in the top edge; "" for no title
//	contentLines— content rows; each may contain ANSI codes (visibleLen handles them)
//	b           — border character set
//	width       — total outer width including the two border columns
//	borderColor — applied to each border character at render time; pass nil for no color.
//	              Applying color here (not post-hoc) ensures border chars inside logo/content
//	              rows are not accidentally re-colored.
func RenderBox(title string, contentLines []string, b Border, width int, borderColor func(string) string) string {
	if borderColor == nil {
		borderColor = func(s string) string { return s }
	}

	bc := borderColor // shorthand

	// innerWidth is the content area width (total minus the two border chars).
	innerWidth := width - 2
	if innerWidth < 4 {
		innerWidth = 4
	}

	var sb strings.Builder

	// ── Top edge ──────────────────────────────────────────────────────────
	// Pattern: TopLeft + "══" + " TITLE " + "══...══" + TopRight
	//       or TopLeft + "══...══" + TopRight (no title)
	sb.WriteString(bc(b.TopLeft))
	if title != "" {
		// visibleLen handles ANSI in the title (e.g. Amber("⚠  WRITE APPROVAL")).
		titleVisible := visibleLen(title)
		// Fixed prefix of two fill chars before the title, one space on each side.
		prefix := bc(b.Top) + bc(b.Top) + " " + title + " "
		prefixVisible := 2 + 1 + titleVisible + 1
		remaining := innerWidth - prefixVisible
		if remaining < 0 {
			remaining = 0
		}
		sb.WriteString(prefix)
		sb.WriteString(strings.Repeat(bc(b.Top), remaining))
	} else {
		sb.WriteString(strings.Repeat(bc(b.Top), innerWidth))
	}
	sb.WriteString(bc(b.TopRight))
	sb.WriteString("\n")

	// ── Content lines ──────────────────────────────────────────────────────
	for _, line := range contentLines {
		if visibleLen(line) > innerWidth {
			line = truncateLine(line, innerWidth)
		}
		pad := innerWidth - visibleLen(line)
		if pad < 0 {
			pad = 0
		}
		sb.WriteString(bc(b.Left))
		sb.WriteString(line)
		sb.WriteString(strings.Repeat(" ", pad))
		sb.WriteString(bc(b.Right))
		sb.WriteString("\n")
	}

	// ── Bottom edge ────────────────────────────────────────────────────────
	sb.WriteString(bc(b.BottomLeft))
	sb.WriteString(strings.Repeat(bc(b.Bottom), innerWidth))
	sb.WriteString(bc(b.BottomRight))

	return sb.String()
}

// blankLine returns an empty padded line for use as vertical padding inside a box.
func blankLine() string { return "" }

// truncateLine shortens line so its visible length is at most maxVisible,
// replacing the overflow with "…". ANSI sequences are not counted.
// If no truncation is needed the original string is returned unchanged.
func truncateLine(line string, maxVisible int) string {
	if visibleLen(line) <= maxVisible {
		return line
	}
	if maxVisible <= 0 {
		return "…"
	}
	runes := []rune(line)
	n := 0
	i := 0
	sawANSI := false
	for i < len(runes) {
		if runes[i] == '\x1b' {
			sawANSI = true
			if i+1 >= len(runes) {
				i++
				continue
			}
			switch runes[i+1] {
			case '[':
				i += 2
				for i < len(runes) {
					ch := runes[i]
					i++
					if ch >= 0x40 && ch <= 0x7E {
						break
					}
				}
			case ']':
				i += 2
				for i < len(runes) {
					if runes[i] == '\x07' {
						i++
						break
					}
					if runes[i] == '\x1b' && i+1 < len(runes) && runes[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
			default:
				i += 2
			}
			continue
		}
		n++
		if n == maxVisible {
			// runes[:i] contains (maxVisible-1) visible chars.
			// Append reset (only if ANSI was used) + "…" for exactly maxVisible visible chars.
			reset := ""
			if sawANSI {
				reset = ansiReset
			}
			return string(runes[:i]) + reset + "…"
		}
		i++
	}
	return line
}
