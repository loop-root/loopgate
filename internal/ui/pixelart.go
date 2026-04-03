package ui

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// PixelArt renders a grid of color indices as half-block (▀▄) terminal art.
// Each cell in the grid is a 256-color palette index; 0 means transparent (no color).
// Two vertical rows are packed into one terminal row using ▀ with fg=top, bg=bottom.
// Falls back to a plain-text version when colors are disabled.

// pixel color constants — 256-color palette indices used in character art.
const (
	px_     = 0   // transparent
	pxPink  = 169 // #D75FAF — morph brand pink
	pxTeal  = 51  // #00FFFF — morph accent
	pxGreen = 35  // #00AF5F — chameleon body green
	pxLime  = 76  // #5FD700 — chameleon highlight
	pxDark  = 22  // #005F00 — chameleon shadow/detail
	pxWhite = 255 // #EEEEEE — eye highlight
	pxBlack = 16  // #000000 — eye pupil
	pxAmber = 178 // #D7AF00 — eye ring / warm accent
	pxBrown = 94  // #875F00 — branch
	pxSlate = 240 // #585858 — branch shadow
)

// chameleonPalette defines a color-shift variant for the chameleon.
// Each palette remaps the body, highlight, shadow, tail accent, and branch accent.
type chameleonPalette struct {
	Body      int // main body color
	Highlight int // scale highlights
	Shadow    int // dark details
	Tail      int // tail tip accent
	Branch    int // branch accent endpoints
}

// Color-shift palettes — the chameleon cycles through these during the
// startup animation, settling on the final (green) palette.
var chameleonPalettes = []chameleonPalette{
	{Body: 169, Highlight: 212, Shadow: 125, Tail: 51, Branch: 76},   // pink body
	{Body: 51, Highlight: 87, Shadow: 24, Tail: 169, Branch: 169},    // teal body
	{Body: 178, Highlight: 220, Shadow: 136, Tail: 51, Branch: 169},  // amber body
	{Body: 35, Highlight: 76, Shadow: 22, Tail: 51, Branch: 169},     // green (final)
}

// fgbg emits a single half-block character with foreground (top pixel) and
// background (bottom pixel) colors. Uses ▀ (upper half block).
func fgbg(top, bottom int) string {
	if !colorable {
		if top != px_ && bottom != px_ {
			return "█"
		}
		if top != px_ {
			return "▀"
		}
		if bottom != px_ {
			return "▄"
		}
		return " "
	}

	switch {
	case top == px_ && bottom == px_:
		return " "
	case top == px_:
		// Only bottom pixel — use ▄ with fg=bottom color
		return fmt.Sprintf("\x1b[38;5;%dm▄\x1b[0m", bottom)
	case bottom == px_:
		// Only top pixel — use ▀ with fg=top color
		return fmt.Sprintf("\x1b[38;5;%dm▀\x1b[0m", top)
	case top == bottom:
		// Both same color — full block
		return fmt.Sprintf("\x1b[38;5;%dm█\x1b[0m", top)
	default:
		// Both different — ▀ with fg=top, bg=bottom
		return fmt.Sprintf("\x1b[38;5;%d;48;5;%dm▀\x1b[0m", top, bottom)
	}
}

// renderPixelGrid takes a 2D grid of color indices (row-major, top to bottom)
// and renders it as half-block art. Grid height should be even; if odd, a
// transparent row is appended.
func renderPixelGrid(grid [][]int) []string {
	if len(grid) == 0 {
		return nil
	}

	// Ensure even height
	rows := grid
	if len(rows)%2 != 0 {
		emptyRow := make([]int, len(rows[0]))
		rows = append(rows, emptyRow)
	}

	width := len(rows[0])
	lines := make([]string, 0, len(rows)/2)

	for y := 0; y < len(rows); y += 2 {
		var sb strings.Builder
		topRow := rows[y]
		botRow := rows[y+1]
		for x := 0; x < width; x++ {
			top := px_
			bot := px_
			if x < len(topRow) {
				top = topRow[x]
			}
			if x < len(botRow) {
				bot = botRow[x]
			}
			sb.WriteString(fgbg(top, bot))
		}
		lines = append(lines, sb.String())
	}

	return lines
}

// ── Chameleon character ─────────────────────────────────────────────────────

// chameleonGrid builds the 24×14 pixel grid using the given palette.
// The shape is constant; only the colors change per palette.
func chameleonGrid(p chameleonPalette) [][]int {
	G := p.Body
	L := p.Highlight
	D := p.Shadow
	W := pxWhite
	B := pxBlack
	A := pxAmber
	R := pxBrown
	T := p.Tail
	P := p.Branch

	return [][]int{
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, T, T, 0, 0}, // 0  tail tip
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, T, G, 0, 0, 0}, // 1  tail curl
		{0, 0, 0, 0, 0, 0, L, L, L, 0, 0, 0, 0, 0, 0, 0, 0, 0, G, G, 0, 0, 0, 0}, // 2  crest + tail
		{0, 0, 0, 0, 0, L, G, G, G, L, 0, 0, 0, 0, 0, 0, 0, G, D, 0, 0, 0, 0, 0}, // 3  head dome
		{0, 0, 0, 0, G, G, G, G, G, G, G, 0, 0, 0, 0, 0, G, G, 0, 0, 0, 0, 0, 0}, // 4  head
		{0, 0, 0, G, G, A, W, W, A, G, G, G, 0, 0, 0, G, G, 0, 0, 0, 0, 0, 0, 0}, // 5  eye row top
		{0, 0, 0, G, G, A, B, B, A, G, G, G, G, G, G, G, 0, 0, 0, 0, 0, 0, 0, 0}, // 6  eye row bottom
		{0, 0, 0, 0, G, G, G, G, G, L, G, G, G, G, G, 0, 0, 0, 0, 0, 0, 0, 0, 0}, // 7  cheek
		{0, 0, 0, 0, 0, G, G, G, L, G, L, G, G, D, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, // 8  body
		{0, 0, 0, 0, 0, 0, G, G, G, G, G, G, D, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, // 9  belly
		{0, 0, 0, 0, 0, G, D, 0, 0, 0, 0, D, G, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, // 10 feet
		{0, P, P, R, R, R, R, R, R, R, R, R, R, R, R, R, R, P, P, 0, 0, 0, 0, 0}, // 11 branch
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, // 12
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, // 13
	}
}

// ChameleonArt returns the chameleon in its default green palette.
func ChameleonArt() []string {
	return renderPixelGrid(chameleonGrid(chameleonPalettes[len(chameleonPalettes)-1]))
}

// ChameleonArtPalette returns the chameleon in the given palette index.
func ChameleonArtPalette(idx int) []string {
	p := chameleonPalettes[idx%len(chameleonPalettes)]
	return renderPixelGrid(chameleonGrid(p))
}

// ChameleonPaletteCount returns the number of available color-shift palettes.
func ChameleonPaletteCount() int {
	return len(chameleonPalettes)
}

// ── Animated banner ─────────────────────────────────────────────────────────

// AnimateChameleonBanner writes the startup banner to stdout with a brief
// color-shift animation. The chameleon cycles through palettes before
// settling on the final (green) form. Falls back to a static banner if
// colors are disabled or stdout is not a TTY.
func AnimateChameleonBanner(cfg BannerConfig) {
	if !colorable {
		fmt.Print(Banner(cfg))
		return
	}

	frameDelay := 200 * time.Millisecond
	nPalettes := len(chameleonPalettes)

	for i := 0; i < nPalettes; i++ {
		frame := buildChameleonBanner(cfg, i)
		lines := strings.Split(frame, "\n")
		lineCount := len(lines)

		fmt.Print(frame)

		if i < nPalettes-1 {
			time.Sleep(frameDelay)
			// Move cursor up to overwrite — lineCount lines (minus 1 because
			// the last line doesn't end with \n in RenderBox output).
			fmt.Fprintf(os.Stdout, "\x1b[%dA\r", lineCount-1)
		}
	}
}

// buildChameleonBanner renders a side-by-side layout:
// chameleon pixel art on the left, MORPH wordmark + metadata on the right.
func buildChameleonBanner(cfg BannerConfig, paletteIdx int) string {
	chameleonLines := ChameleonArtPalette(paletteIdx)
	wordmark := logoLines(LogoBig)

	// Measure the chameleon's visible width for alignment.
	artWidth := 0
	for _, line := range chameleonLines {
		if vl := visibleLen(line); vl > artWidth {
			artWidth = vl
		}
	}

	gap := "   " // space between chameleon and wordmark
	indent := "  "

	// Build right-side content: wordmark + metadata, vertically centered.
	var rightLines []string
	for _, row := range wordmark {
		rightLines = append(rightLines, Pink(row))
	}
	rightLines = append(rightLines, "")
	rightLines = append(rightLines, Dim("v"+cfg.Version+"  ·  capability-governed AI orchestrator"))
	rightLines = append(rightLines, "")
	rightLines = append(rightLines, Dim("persona  ")+"▸  "+Teal(cfg.PersonaName))
	rightLines = append(rightLines, Dim("session  ")+"▸  "+Teal(cfg.SessionID))
	writeBadge := Amber("writes:" + cfg.WriteMode)
	readBadge := Green("reads:" + cfg.ReadMode)
	rightLines = append(rightLines, Dim("policy   ")+"▸  "+writeBadge+"  ·  "+readBadge)

	// Merge left (chameleon) and right (wordmark+meta) with vertical centering.
	totalLines := len(chameleonLines)
	if len(rightLines) > totalLines {
		totalLines = len(rightLines)
	}
	leftOffset := 0
	rightOffset := 0
	if len(chameleonLines) < totalLines {
		leftOffset = (totalLines - len(chameleonLines)) / 2
	}
	if len(rightLines) < totalLines {
		rightOffset = (totalLines - len(rightLines)) / 2
	}

	artPad := strings.Repeat(" ", artWidth)
	var contentLines []string
	contentLines = append(contentLines, blankLine())
	for i := 0; i < totalLines; i++ {
		left := artPad
		if li := i - leftOffset; li >= 0 && li < len(chameleonLines) {
			line := chameleonLines[li]
			pad := artWidth - visibleLen(line)
			if pad > 0 {
				left = line + strings.Repeat(" ", pad)
			} else {
				left = line
			}
		}
		right := ""
		if ri := i - rightOffset; ri >= 0 && ri < len(rightLines) {
			right = rightLines[ri]
		}
		contentLines = append(contentLines, indent+left+gap+right)
	}
	contentLines = append(contentLines, blankLine())

	// Compute width from content.
	maxVisible := 0
	for _, line := range contentLines {
		if vl := visibleLen(line); vl > maxVisible {
			maxVisible = vl
		}
	}
	width := maxVisible + 4
	tw := TermWidth()
	if width > tw {
		width = tw
	}
	if width < 68 {
		width = 68
	}

	return RenderBox("", contentLines, DoubleBorder(), width, Magenta)
}
