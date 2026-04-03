package ui

import (
	"testing"
)

// ── fgbg tests (no-color / fallback mode) ────────────────────────────────────

func TestFgbg_BothTransparent(t *testing.T) {
	got := fgbg(px_, px_)
	if got != " " {
		t.Errorf("fgbg(0, 0) = %q; want %q", got, " ")
	}
}

func TestFgbg_TopOnly(t *testing.T) {
	got := fgbg(pxGreen, px_)
	if got != "▀" {
		t.Errorf("fgbg(green, 0) = %q; want %q", got, "▀")
	}
}

func TestFgbg_BottomOnly(t *testing.T) {
	got := fgbg(px_, pxPink)
	if got != "▄" {
		t.Errorf("fgbg(0, pink) = %q; want %q", got, "▄")
	}
}

func TestFgbg_BothSameColor(t *testing.T) {
	got := fgbg(pxTeal, pxTeal)
	if got != "█" {
		t.Errorf("fgbg(teal, teal) = %q; want %q", got, "█")
	}
}

func TestFgbg_BothDifferentColors(t *testing.T) {
	// In no-color mode, both non-transparent yields "█" regardless of whether
	// the colors differ.
	got := fgbg(pxGreen, pxPink)
	if got != "█" {
		t.Errorf("fgbg(green, pink) = %q; want %q", got, "█")
	}
}

// ── renderPixelGrid tests ────────────────────────────────────────────────────

func TestRenderPixelGrid_Empty(t *testing.T) {
	got := renderPixelGrid(nil)
	if got != nil {
		t.Errorf("renderPixelGrid(nil) = %v; want nil", got)
	}

	got = renderPixelGrid([][]int{})
	if got != nil {
		t.Errorf("renderPixelGrid([]) = %v; want nil", got)
	}
}

func TestRenderPixelGrid_SingleRow(t *testing.T) {
	// Odd height (1 row) should be padded to 2 rows, producing 1 output line.
	grid := [][]int{
		{pxGreen, px_, pxPink},
	}
	lines := renderPixelGrid(grid)
	if len(lines) != 1 {
		t.Fatalf("renderPixelGrid(1 row) produced %d lines; want 1", len(lines))
	}
	// With padding, top=green/bot=0 → "▀", top=0/bot=0 → " ", top=pink/bot=0 → "▀"
	want := "▀ ▀"
	if lines[0] != want {
		t.Errorf("got %q; want %q", lines[0], want)
	}
}

func TestRenderPixelGrid_TwoRows(t *testing.T) {
	grid := [][]int{
		{pxGreen, px_},
		{px_, pxPink},
	}
	lines := renderPixelGrid(grid)
	if len(lines) != 1 {
		t.Fatalf("renderPixelGrid(2 rows) produced %d lines; want 1", len(lines))
	}
	// col 0: top=green, bot=0 → "▀"; col 1: top=0, bot=pink → "▄"
	want := "▀▄"
	if lines[0] != want {
		t.Errorf("got %q; want %q", lines[0], want)
	}
}

func TestRenderPixelGrid_PreservesWidth(t *testing.T) {
	width := 6
	grid := [][]int{
		make([]int, width),
		make([]int, width),
	}
	lines := renderPixelGrid(grid)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line; got %d", len(lines))
	}
	// All transparent → each cell is " ", so total rune count == width.
	runes := []rune(lines[0])
	if len(runes) != width {
		t.Errorf("line rune length = %d; want %d", len(runes), width)
	}
}

// ── ChameleonArt tests ───────────────────────────────────────────────────────

func TestChameleonArt_NotEmpty(t *testing.T) {
	art := ChameleonArt()
	if len(art) == 0 {
		t.Fatal("ChameleonArt() returned empty slice")
	}
}

func TestChameleonArt_ConsistentWidth(t *testing.T) {
	art := ChameleonArt()
	for i, line := range art {
		runes := []rune(line)
		if len(runes) == 0 {
			t.Errorf("line %d has 0 rune length", i)
		}
	}
}
