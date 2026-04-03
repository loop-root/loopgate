package ui

import (
	"os"
	"strconv"
)

// isTTY reports whether f is connected to a terminal.
// Uses os.ModeCharDevice — no platform-specific syscalls required,
// no external dependencies.
func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// TermWidth returns the usable terminal column count.
// Checks COLUMNS env var (set by most shells on resize), then defaults to 80.
// Enforces a minimum of 40 to prevent layout breakage on very narrow outputs.
func TermWidth() int {
	if v := os.Getenv("COLUMNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 40 {
			return n
		}
	}
	return 80
}

// visibleLen returns the printable display width of s,
// skipping ANSI/VT escape sequences.
//
// Handled sequence types (covering what we emit + common terminal output):
//
//	CSI sequences  \x1b[ ... <letter>   e.g. \x1b[38;5;199m  (colors, cursor)
//	OSC sequences  \x1b] ... \x07       e.g. \x1b]0;title\x07 (window title)
//	               \x1b] ... \x1b\\     e.g. OSC with ST terminator
//	Fe sequences   \x1b<single-byte>    e.g. \x1b7 (save cursor)
//
// This is intentionally conservative: unknown sequence types are skipped
// byte-by-byte, which may over-count on exotic sequences but never under-counts
// for the sequences morph itself emits.
func visibleLen(s string) int {
	n := 0
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		if runes[i] != '\x1b' {
			n++
			i++
			continue
		}

		// ESC seen — determine sequence type from the next byte.
		if i+1 >= len(runes) {
			// Bare ESC at end of string; count it as visible.
			n++
			i++
			continue
		}

		switch runes[i+1] {
		case '[': // CSI: \x1b[ ... <terminating-letter>
			i += 2
			for i < len(runes) {
				ch := runes[i]
				i++
				if ch >= 0x40 && ch <= 0x7E { // @A-Z[\]^_`a-z{|}~
					break
				}
			}

		case ']': // OSC: \x1b] ... \x07  or  \x1b] ... \x1b\\
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

		default: // Fe/Fs/Fp: two-byte sequences like \x1b7, \x1bM, \x1b=
			i += 2
		}
	}
	return n
}
