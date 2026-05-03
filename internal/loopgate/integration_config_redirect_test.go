package loopgate

import (
	"strings"
	"testing"
)

func TestParseAndValidateRedirectURL_AllowsHTTPSAndLoopbackHTTPOnly(t *testing.T) {
	validRedirects := []string{
		"https://example.test/callback",
		"http://127.0.0.1/callback",
		"http://localhost:8080/callback",
	}
	for _, rawRedirectURL := range validRedirects {
		if _, err := parseAndValidateRedirectURL(rawRedirectURL); err != nil {
			t.Fatalf("expected redirect_url %q to be allowed, got %v", rawRedirectURL, err)
		}
	}
}

func TestParseAndValidateRedirectURL_DeniesCustomAndUnsafeSchemes(t *testing.T) {
	invalidRedirects := []string{
		"myapp:/callback",
		"myapp://callback",
		"file:///tmp/callback",
		"ssh://example.test/callback",
		"http://example.test/callback",
	}
	for _, rawRedirectURL := range invalidRedirects {
		_, err := parseAndValidateRedirectURL(rawRedirectURL)
		if err == nil {
			t.Fatalf("expected redirect_url %q to be denied", rawRedirectURL)
		}
		if !strings.Contains(err.Error(), "https or localhost http") {
			t.Fatalf("unexpected error for %q: %v", rawRedirectURL, err)
		}
	}
}
