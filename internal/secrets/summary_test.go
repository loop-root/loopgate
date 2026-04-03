package secrets

import (
	"strings"
	"testing"
)

func TestSummarizeForPersistence_PlainText(t *testing.T) {
	summary := SummarizeForPersistence("hello world", 200)
	if summary.Preview != "hello world" {
		t.Errorf("expected preview 'hello world', got %q", summary.Preview)
	}
	if summary.Bytes != 11 {
		t.Errorf("expected 11 bytes, got %d", summary.Bytes)
	}
	if summary.Truncated {
		t.Error("expected not truncated")
	}
	if summary.Redacted {
		t.Error("expected not redacted")
	}
	if summary.SHA256 == "" {
		t.Error("expected non-empty SHA256")
	}
}

func TestSummarizeForPersistence_RedactsSensitiveContent(t *testing.T) {
	summary := SummarizeForPersistence("private_key=super-secret-value", 200)
	if strings.Contains(summary.Preview, "super-secret-value") {
		t.Errorf("preview should not contain raw secret: %q", summary.Preview)
	}
	if !summary.Redacted {
		t.Error("expected redacted=true for sensitive content")
	}
}

func TestSummarizeForPersistence_TruncatesLongInput(t *testing.T) {
	longInput := strings.Repeat("x", 500)
	summary := SummarizeForPersistence(longInput, 40)
	if !summary.Truncated {
		t.Error("expected truncated=true")
	}
	if !strings.HasSuffix(summary.Preview, "... (truncated)") {
		t.Errorf("expected truncated suffix, got %q", summary.Preview)
	}
	// Preview prefix should be the first 40 bytes of input.
	if !strings.HasPrefix(summary.Preview, longInput[:40]) {
		t.Errorf("expected preview to start with first 40 bytes")
	}
	// Bytes should reflect original length, not truncated.
	if summary.Bytes != 500 {
		t.Errorf("expected 500 bytes, got %d", summary.Bytes)
	}
}

func TestSummarizeForPersistence_SHA256IsFromOriginalInput(t *testing.T) {
	input := "private_key=something-secret"
	summary := SummarizeForPersistence(input, 200)
	// The SHA256 should be of the raw input, not the redacted preview.
	// Verify by comparing two summaries of the same input with different maxPreview.
	summary2 := SummarizeForPersistence(input, 10)
	if summary.SHA256 != summary2.SHA256 {
		t.Errorf("SHA256 should be identical regardless of maxPreview")
	}
}

func TestSummarizeForPersistence_TrimsWhitespace(t *testing.T) {
	summary := SummarizeForPersistence("  hello  ", 200)
	if summary.Preview != "hello" {
		t.Errorf("expected trimmed preview 'hello', got %q", summary.Preview)
	}
}

func TestSummarizeForPersistence_EmptyInput(t *testing.T) {
	summary := SummarizeForPersistence("", 200)
	if summary.Preview != "" {
		t.Errorf("expected empty preview, got %q", summary.Preview)
	}
	if summary.Bytes != 0 {
		t.Errorf("expected 0 bytes, got %d", summary.Bytes)
	}
}

func TestSummarizeForPersistence_ZeroMaxPreviewNoTruncation(t *testing.T) {
	summary := SummarizeForPersistence("hello", 0)
	if summary.Preview != "hello" {
		t.Errorf("expected full preview with maxPreview=0, got %q", summary.Preview)
	}
	if summary.Truncated {
		t.Error("expected not truncated with maxPreview=0")
	}
}
