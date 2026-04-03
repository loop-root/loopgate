// Package ui provides terminal rendering primitives for the morph CLI.
// It is intentionally dependency-free and security-aware:
//   - Colors are evaluated once at startup (NO_COLOR / TTY check).
//   - apply() is the single point of ANSI emission; nothing else writes escape codes.
//   - All render functions accept plain string inputs; callers are responsible
//     for ensuring no raw secret material is passed to render functions.
package ui

import "os"

// ANSI 256-color palette used by morph.
// 256-color mode is preferred over truecolor for broader terminal support.
const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiReverse = "\x1b[7m"

	// Foreground colors: \x1b[38;5;<n>m
	ansiHotPink = "\x1b[38;5;169m" // #D75FAF — muted rose pink (brand primary)
	ansiTeal    = "\x1b[38;5;51m"  // #00FFFF — accent / values
	ansiPurple  = "\x1b[38;5;141m" // #AF87FF — secondary labels
	ansiAmber   = "\x1b[38;5;220m" // #FFD700 — approval / warning
	ansiGreen   = "\x1b[38;5;84m"  // #5FFF87 — allow / ok
	ansiRed     = "\x1b[38;5;196m" // #FF0000 — error / deny
	ansiSlate   = "\x1b[38;5;240m" // #585858 — dim / secondary
	ansiMagenta = "\x1b[38;5;90m"  // #870087 — borders
	ansiWhite   = "\x1b[38;5;255m" // #EEEEEE — bright content
)

// colorable is computed once at process start and never mutated.
// Checking NO_COLOR and TTY status once prevents per-call overhead
// and avoids state drift if the environment changes mid-session.
var colorable = computeColorable()

func computeColorable() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("MORPH_NO_COLOR") != "" {
		return false
	}
	return isTTY(os.Stdout)
}

// apply wraps s with an ANSI escape code if colors are enabled.
// This is the only function in this package that emits escape sequences.
// Callers must never pass raw secret material as s.
func apply(code, s string) string {
	if !colorable || s == "" {
		return s
	}
	return code + s + ansiReset
}

// Semantic color wrappers — named for their role, not their shade.
func Pink(s string) string    { return apply(ansiHotPink, s) }
func Teal(s string) string    { return apply(ansiTeal, s) }
func Purple(s string) string  { return apply(ansiPurple, s) }
func Amber(s string) string   { return apply(ansiAmber, s) }
func Green(s string) string   { return apply(ansiGreen, s) }
func Red(s string) string     { return apply(ansiRed, s) }
func Dim(s string) string     { return apply(ansiSlate, s) }
func Bold(s string) string    { return apply(ansiBold, s) }
func Magenta(s string) string { return apply(ansiMagenta, s) }
func White(s string) string   { return apply(ansiWhite, s) }
