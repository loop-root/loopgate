package ui

// LogoStyle selects which ASCII art variant to render.
type LogoStyle int

const (
	// LogoBig — figlet "big" font. Tall block letters using ██ and ╗╚╝.
	// ~47 chars wide, 6 lines tall. Most legible at any terminal width.
	LogoBig LogoStyle = iota

	// LogoPixel — doubled-pixel retro art. Each glyph is a 5×5 pixel grid
	// scaled to 2-char-wide blocks (██). Evokes Apple II CRT phosphor displays.
	// ~58 chars wide, 5 lines tall.
	LogoPixel

	// LogoSlim — minimal 3-line box-drawing font. Clean and compact.
	// ~18 chars wide, 3 lines tall. Good for narrow terminals or inline use.
	LogoSlim

	// MorphChameleon — a chameleon on a branch. The literal shapeshifter.
	// Colored half-block pixel art when colors are available, ASCII fallback otherwise.
	// ~24 chars wide, 7 lines tall (pixel art) or 5 lines tall (ASCII fallback).
	MorphChameleon
)

// logoPreColored returns true if the given style provides its own ANSI colors
// (e.g. pixel art) and should not be wrapped in a uniform color.
func logoPreColored(style LogoStyle) bool {
	return style == MorphChameleon && colorable
}

// logoLines returns the raw (uncolored) lines for the given logo style.
// Lines are left-aligned; callers apply padding and color.
func logoLines(style LogoStyle) []string {
	switch style {
	case LogoPixel:
		// Each letter occupies a 5×5 pixel grid, each pixel rendered as "██" or "  ".
		// Letters: M O R P H — separated by a single empty column ("  ").
		//
		// M [1,0,0,0,1]   O [0,1,1,1,0]   R [1,1,1,1,0]   P [1,1,1,1,0]   H [1,0,0,0,1]
		//   [1,1,0,1,1]     [1,0,0,0,1]     [1,0,0,0,1]     [1,0,0,0,1]     [1,0,0,0,1]
		//   [1,0,1,0,1]     [1,0,0,0,1]     [1,1,1,1,0]     [1,1,1,1,0]     [1,1,1,1,1]
		//   [1,0,0,0,1]     [1,0,0,0,1]     [1,0,1,0,0]     [1,0,0,0,0]     [1,0,0,0,1]
		//   [1,0,0,0,1]     [0,1,1,1,0]     [1,0,0,1,0]     [1,0,0,0,0]     [1,0,0,0,1]
		return []string{
			"██      ██    ██████    ████████    ████████    ██      ██",
			"████  ████  ██      ██  ██      ██  ██      ██  ██      ██",
			"██  ██  ██  ██      ██  ████████    ████████    ██████████",
			"██      ██  ██      ██  ██  ██      ██          ██      ██",
			"██      ██    ██████    ██    ██    ██          ██      ██",
		}

	case LogoSlim:
		// Box-drawing "slim" font — 3 lines, ~18 chars wide.
		// Clean enough for narrow terminals or secondary display contexts.
		//
		// M: ┌┬┐ │││ ┴ ┴     O: ┌─┐ │ │ └─┘
		// R: ┬─┐ ├┬┘ ┴└─     P: ┌─┐ ├─┘ ┴
		// H: ┬ ┬ ├─┤ ┴ ┴
		return []string{
			"┌┬┐┌─┐┬─┐┌─┐┬ ┬",
			"│││ │ ├┬┘├─┘├─┤",
			"┴ ┴└─┘┴└─┴  ┴ ┴",
		}

	case MorphChameleon:
		// Colored half-block pixel art when the terminal supports it.
		if colorable {
			return ChameleonArt()
		}
		// Plain-text fallback.
		return []string{
			`    .~~~.`,
			`   (◉    \`,
			`  /  ~~~  )--`,
			` /       /`,
			`~~~~@~~~'`,
		}

	default: // LogoBig
		// figlet "big" font — 6 lines, ~47 chars wide.
		// Rendered with ██ fill blocks and ╗╚╝ box corners.
		return []string{
			`███╗   ███╗ ██████╗ ██████╗ ██████╗ ██╗  ██╗`,
			`████╗ ████║██╔═══██╗██╔══██╗██╔══██╗██║  ██║`,
			`██╔████╔██║██║   ██║██████╔╝██████╔╝███████║`,
			`██║╚██╔╝██║██║   ██║██╔══██╗██╔═══╝ ██╔══██║`,
			`██║ ╚═╝ ██║╚██████╔╝██║  ██║██║     ██║  ██║`,
			`╚═╝     ╚═╝ ╚═════╝ ╚═╝  ╚═╝╚═╝     ╚═╝  ╚═╝`,
		}
	}
}
