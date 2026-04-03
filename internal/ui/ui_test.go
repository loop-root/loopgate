package ui

import (
	"strings"
	"testing"
)

// ── visibleLen ────────────────────────────────────────────────────────────────

func TestVisibleLen_PlainString(t *testing.T) {
	if got := visibleLen("hello"); got != 5 {
		t.Fatalf("want 5, got %d", got)
	}
}

func TestVisibleLen_ANSIStripped(t *testing.T) {
	colored := "\x1b[38;5;199mhello\x1b[0m"
	if got := visibleLen(colored); got != 5 {
		t.Fatalf("want 5, got %d (ANSI not stripped)", got)
	}
}

func TestVisibleLen_MultipleEscapes(t *testing.T) {
	s := "\x1b[1m\x1b[38;5;51mfoo\x1b[0m \x1b[38;5;220mbar\x1b[0m"
	if got := visibleLen(s); got != 7 { // "foo bar"
		t.Fatalf("want 7, got %d", got)
	}
}

func TestVisibleLen_Empty(t *testing.T) {
	if got := visibleLen(""); got != 0 {
		t.Fatalf("want 0, got %d", got)
	}
}

func TestVisibleLen_OSCSequenceStripped(t *testing.T) {
	// Window-title OSC: \x1b]0;title\x07
	s := "\x1b]0;my title\x07hello"
	if got := visibleLen(s); got != 5 { // only "hello"
		t.Fatalf("want 5, got %d", got)
	}
}

func TestVisibleLen_OSCWithSTTerminator(t *testing.T) {
	// OSC terminated by ST (\x1b\\)
	s := "\x1b]0;title\x1b\\hello"
	if got := visibleLen(s); got != 5 {
		t.Fatalf("want 5, got %d", got)
	}
}

func TestVisibleLen_FeSequenceStripped(t *testing.T) {
	// Two-byte Fe sequence: \x1b7 (save cursor position)
	s := "\x1b7hello\x1b8" // save, "hello", restore
	if got := visibleLen(s); got != 5 {
		t.Fatalf("want 5, got %d", got)
	}
}

func TestVisibleLen_CSITerminatesOnAnyFinalByte(t *testing.T) {
	// Cursor movement \x1b[2A (two bytes: '2', 'A') — 'A' is final, not a letter in
	// the narrow sense but is in range 0x40-0x7E.
	s := "\x1b[2Ahello"
	if got := visibleLen(s); got != 5 {
		t.Fatalf("want 5, got %d", got)
	}
}

// ── RenderBox ────────────────────────────────────────────────────────────────

func TestRenderBox_NoTitle(t *testing.T) {
	lines := []string{"hello", "world"}
	out := RenderBox("", lines, SingleBorder(), 20, nil)

	rows := strings.Split(out, "\n")
	// top border, 2 content lines, bottom border = 4 rows
	if len(rows) != 4 {
		t.Fatalf("want 4 rows, got %d:\n%s", len(rows), out)
	}
	if !strings.HasPrefix(rows[0], "┌") || !strings.HasSuffix(rows[0], "┐") {
		t.Errorf("top border malformed: %q", rows[0])
	}
	if !strings.HasPrefix(rows[3], "└") || !strings.HasSuffix(rows[3], "┘") {
		t.Errorf("bottom border malformed: %q", rows[3])
	}
}

func TestRenderBox_WithTitle(t *testing.T) {
	out := RenderBox("POLICY", []string{"line"}, SingleBorder(), 40, nil)
	top := strings.Split(out, "\n")[0]
	if !strings.Contains(top, "POLICY") {
		t.Errorf("title not present in top border: %q", top)
	}
	if !strings.HasPrefix(top, "┌") || !strings.HasSuffix(top, "┐") {
		t.Errorf("top border chars malformed: %q", top)
	}
}

func TestRenderBox_ContentWidthCorrect(t *testing.T) {
	// Each content row should be exactly `width` chars wide
	// (border-left + innerWidth + border-right).
	width := 30
	lines := []string{"short", strings.Repeat("x", 24)} // fits exactly
	out := RenderBox("", lines, SingleBorder(), width, nil)
	for i, row := range strings.Split(out, "\n") {
		if len(row) == 0 {
			continue
		}
		if len([]rune(row)) != width {
			t.Errorf("row %d width: want %d, got %d: %q", i, width, len([]rune(row)), row)
		}
	}
}

func TestRenderBox_OverflowContentTruncated(t *testing.T) {
	// A content line longer than inner width must not push the right border out.
	width := 20
	long := strings.Repeat("x", 40) // far exceeds inner width of 18
	out := RenderBox("", []string{long}, SingleBorder(), width, nil)
	for i, row := range strings.Split(out, "\n") {
		if len(row) == 0 {
			continue
		}
		if len([]rune(row)) != width {
			t.Errorf("row %d width: want %d, got %d: %q", i, width, len([]rune(row)), row)
		}
	}
}

func TestTruncateLine_PlainFits(t *testing.T) {
	if got := truncateLine("hello", 10); got != "hello" {
		t.Errorf("truncateLine mutated fitting string: %q", got)
	}
}

func TestTruncateLine_PlainOverflows(t *testing.T) {
	out := truncateLine("hello world", 7)
	if visibleLen(out) != 7 {
		t.Errorf("truncateLine visible length: want 7, got %d: %q", visibleLen(out), out)
	}
	if !strings.HasSuffix(out, "…") {
		t.Errorf("truncateLine missing ellipsis: %q", out)
	}
}

func TestTruncateLine_ANSIOverflows(t *testing.T) {
	colored := "\x1b[38;5;51m" + strings.Repeat("x", 30) + "\x1b[0m"
	out := truncateLine(colored, 10)
	if visibleLen(out) != 10 {
		t.Errorf("truncateLine ANSI visible length: want 10, got %d", visibleLen(out))
	}
}

func TestRenderBox_AnsiContentHandled(t *testing.T) {
	// A colored line should still produce a correctly-padded row.
	colored := Teal("hello") // 5 visible chars, extra ANSI bytes
	out := RenderBox("", []string{colored}, SingleBorder(), 20, nil)
	rows := strings.Split(out, "\n")
	// Content row should start with │ and end with │
	contentRow := rows[1]
	if !strings.HasPrefix(contentRow, "│") {
		t.Errorf("content row missing left border: %q", contentRow)
	}
	if !strings.HasSuffix(contentRow, "│") {
		t.Errorf("content row missing right border: %q", contentRow)
	}
}

// ── Logo lines ────────────────────────────────────────────────────────────────

func TestLogoLines_AllVariantsNonEmpty(t *testing.T) {
	for _, style := range []LogoStyle{LogoBig, LogoPixel, LogoSlim, MorphChameleon} {
		lines := logoLines(style)
		if len(lines) == 0 {
			t.Errorf("logo style %d returned no lines", style)
		}
		for i, l := range lines {
			if l == "" {
				t.Errorf("logo style %d line %d is empty", style, i)
			}
		}
	}
}

func TestLogoLines_BigWidthFitsIn80Cols(t *testing.T) {
	for _, line := range logoLines(LogoBig) {
		if visibleLen(line) > 78 {
			t.Errorf("LogoBig line too wide (%d): %q", visibleLen(line), line)
		}
	}
}

func TestLogoLines_PixelWidthFitsIn80Cols(t *testing.T) {
	for _, line := range logoLines(LogoPixel) {
		if visibleLen(line) > 78 {
			t.Errorf("LogoPixel line too wide (%d): %q", visibleLen(line), line)
		}
	}
}

// ── Denial / Allow / Warn ─────────────────────────────────────────────────────

func TestDenial_ContainsReason(t *testing.T) {
	reason := "filesystem writes are disabled by policy"
	out := Denial(reason)
	// Strip ANSI for content check
	if !strings.Contains(out, reason) {
		t.Errorf("denial output does not contain reason: %q", out)
	}
}

func TestAllow_ContainsMessage(t *testing.T) {
	msg := "write approved by operator"
	out := Allow(msg)
	if !strings.Contains(out, msg) {
		t.Errorf("allow output does not contain message: %q", out)
	}
}

// ── Approval — secret safety ──────────────────────────────────────────────────

func TestApproval_HiddenPreviewNotLeaked(t *testing.T) {
	rawSecret := "sk-supersecret-token-abc123"
	req := ApprovalRequest{
		Tool:    "write",
		Path:    "runtime/keys/session.json",
		Bytes:   len(rawSecret),
		Preview: "", // caller suppressed it
		Hidden:  true,
	}
	out := Approval(req)
	if strings.Contains(out, rawSecret) {
		t.Errorf("raw secret appeared in approval output: %q", out)
	}
	if !strings.Contains(out, "redacted") {
		t.Errorf("expected redaction notice in approval output: %q", out)
	}
}

func TestApproval_PathRendered(t *testing.T) {
	req := ApprovalRequest{
		Tool:    "write",
		Path:    "runtime/state/notes.txt",
		Bytes:   42,
		Preview: "hello world",
	}
	out := Approval(req)
	if !strings.Contains(out, "runtime/state/notes.txt") {
		t.Errorf("path not rendered in approval output: %q", out)
	}
}

func TestApproval_ClassRendered(t *testing.T) {
	req := ApprovalRequest{
		Tool:  "sandbox export",
		Class: "export sandbox artifact",
		Path:  "/morph/home/outputs/staged.txt",
	}
	out := Approval(req)
	if !strings.Contains(out, "export sandbox artifact") {
		t.Errorf("class not rendered in approval output: %q", out)
	}
}

// ── Prompt ────────────────────────────────────────────────────────────────────

func TestPrompt_ContainsTurnCount(t *testing.T) {
	out := Prompt(7)
	if !strings.Contains(out, "7") {
		t.Errorf("prompt does not contain turn count: %q", out)
	}
}

func TestPrompt_ContainsMorph(t *testing.T) {
	out := Prompt(0)
	if !strings.Contains(out, "morph") {
		t.Errorf("prompt does not contain 'morph': %q", out)
	}
}

// ── Safe ──────────────────────────────────────────────────────────────────────

func TestSafe_BearerTokenRedacted(t *testing.T) {
	raw := "Authorization: Bearer sk-supersecret-1234"
	out := Safe(raw)
	if strings.Contains(out, "sk-supersecret-1234") {
		t.Errorf("Safe() did not redact bearer token: %q", out)
	}
}

func TestSafe_ApiKeyRedacted(t *testing.T) {
	// The redaction regex matches key=value / key: value patterns (no outer quotes on key).
	raw := "api_key=my-very-secret-key"
	out := Safe(raw)
	if strings.Contains(out, "my-very-secret-key") {
		t.Errorf("Safe() did not redact api_key value: %q", out)
	}
}

func TestSafe_PlainTextPassthrough(t *testing.T) {
	raw := "hello world"
	if got := Safe(raw); got != raw {
		t.Errorf("Safe() mutated plain text: got %q, want %q", got, raw)
	}
}

// ── SessionEnd ────────────────────────────────────────────────────────────────

func TestSessionEnd_ContainsFields(t *testing.T) {
	out := SessionEnd(SessionEndConfig{
		SessionID:  "abc1d3e7",
		Turns:      12,
		ExitReason: "command (/exit)",
		CapsuleOK:  true,
	})
	for _, want := range []string{"SESSION END", "abc1d3e7", "12", "/exit", "written", "✓"} {
		if !strings.Contains(out, want) {
			t.Errorf("SessionEnd missing %q in output:\n%s", want, out)
		}
	}
}

func TestSessionEnd_AllRowsSameWidth(t *testing.T) {
	out := SessionEnd(SessionEndConfig{SessionID: "x", Turns: 1, ExitReason: "test", CapsuleOK: false})
	rows := strings.Split(out, "\n")
	var width int
	for i, row := range rows {
		if row == "" {
			continue
		}
		w := len([]rune(row))
		if i == 0 {
			width = w
		} else if w != width {
			t.Errorf("row %d width %d != first row %d: %q", i, w, width, row)
		}
	}
}

// ── Panel primitives ──────────────────────────────────────────────────────────

func TestKV_ContainsKeyAndValue(t *testing.T) {
	out := KV("reads", StatusOK("enabled"))
	if !strings.Contains(out, "reads") {
		t.Errorf("KV missing key: %q", out)
	}
	if !strings.Contains(out, "enabled") {
		t.Errorf("KV missing value: %q", out)
	}
}

func TestKV_KeyColumnAligned(t *testing.T) {
	short := KV("r", "x")
	long := KV("resolved", "x")
	// Both should have the same prefix width up to the separator.
	shortSep := strings.Index(short, "▸")
	longSep := strings.Index(long, "▸")
	if shortSep < 0 || longSep < 0 {
		t.Fatalf("KV missing separator: short=%q long=%q", short, long)
	}
	// visible length up to separator must match (ANSI stripped)
	shortPre := visibleLen(short[:shortSep])
	longPre := visibleLen(long[:longSep])
	if shortPre != longPre {
		t.Errorf("KV columns not aligned: short prefix %d, long prefix %d", shortPre, longPre)
	}
}

func TestBadge_WrapsInBrackets(t *testing.T) {
	out := Badge("approval", nil)
	if out != "[approval]" {
		t.Errorf("Badge want %q, got %q", "[approval]", out)
	}
}

func TestBadge_WithColor(t *testing.T) {
	out := Badge("ok", Green)
	if !strings.Contains(out, "[ok]") {
		t.Errorf("Badge with color missing text: %q", out)
	}
}

func TestStatusOK_ContainsText(t *testing.T) {
	out := StatusOK("enabled")
	if !strings.Contains(out, "enabled") || !strings.Contains(out, "✓") {
		t.Errorf("StatusOK malformed: %q", out)
	}
}

func TestStatusWarn_ContainsText(t *testing.T) {
	out := StatusWarn("approval required")
	if !strings.Contains(out, "approval required") || !strings.Contains(out, "⚠") {
		t.Errorf("StatusWarn malformed: %q", out)
	}
}

func TestStatusDeny_ContainsText(t *testing.T) {
	out := StatusDeny("disabled")
	if !strings.Contains(out, "disabled") || !strings.Contains(out, "✗") {
		t.Errorf("StatusDeny malformed: %q", out)
	}
}

func TestDivider_IsEmpty(t *testing.T) {
	if got := Divider(); got != "" {
		t.Errorf("Divider want empty string, got %q", got)
	}
}

func TestRow_JoinsWithSeparator(t *testing.T) {
	out := Row("a", "b", "c")
	if !strings.Contains(out, "a") || !strings.Contains(out, "b") || !strings.Contains(out, "c") {
		t.Errorf("Row missing columns: %q", out)
	}
	if !strings.Contains(out, "·") {
		t.Errorf("Row missing separator: %q", out)
	}
}

func TestPanel_TitleAndContentPresent(t *testing.T) {
	out := Panel("POLICY",
		KV("reads", StatusOK("enabled")),
		KV("writes", StatusWarn("approval required")),
	)
	if !strings.Contains(out, "POLICY") {
		t.Errorf("Panel missing title: %q", out)
	}
	if !strings.Contains(out, "reads") || !strings.Contains(out, "writes") {
		t.Errorf("Panel missing content: %q", out)
	}
}

func TestPanel_AllRowsSameWidth(t *testing.T) {
	out := Panel("TITLE",
		KV("reads", StatusOK("enabled")),
		Divider(),
		KV("writes", StatusWarn("approval required")),
	)
	var width int
	for i, row := range strings.Split(out, "\n") {
		if row == "" {
			continue
		}
		w := len([]rune(row))
		if i == 0 {
			width = w
		} else if w != width {
			t.Errorf("row %d width %d != first row width %d: %q", i, w, width, row)
		}
	}
}

func TestParagraph_WrapsAtWidth(t *testing.T) {
	words := strings.Repeat("word ", 20)
	lines := Paragraph(strings.TrimSpace(words), 20)
	if len(lines) == 0 {
		t.Fatal("Paragraph returned no lines")
	}
	for i, line := range lines {
		if visibleLen(line) > 20 {
			t.Errorf("Paragraph line %d exceeds width: %q", i, line)
		}
	}
}

func TestPadRight_PadsToWidth(t *testing.T) {
	out := padRight("hi", 8)
	if visibleLen(out) != 8 {
		t.Errorf("padRight want visible 8, got %d: %q", visibleLen(out), out)
	}
}

func TestPadRight_NoopWhenFits(t *testing.T) {
	out := padRight("hello world", 4)
	if out != "hello world" {
		t.Errorf("padRight mutated wider string: %q", out)
	}
}
