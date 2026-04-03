package anthropic

import (
	"net/http"
	"testing"
	"time"
)

func TestAnthropicRateLimitWait_RetryAfterSeconds(t *testing.T) {
	header := http.Header{}
	header.Set("Retry-After", "5")
	if got := anthropicRateLimitWait(header, 99); got != 5*time.Second {
		t.Fatalf("got %v want 5s", got)
	}
}

func TestAnthropicRateLimitWait_ExponentialWithoutHeader(t *testing.T) {
	if got := anthropicRateLimitWait(nil, 0); got != time.Second {
		t.Fatalf("attempt 0: got %v want 1s", got)
	}
	if got := anthropicRateLimitWait(nil, 2); got != 4*time.Second {
		t.Fatalf("attempt 2: got %v want 4s", got)
	}
}

func TestAnthropicRateLimitWait_CapsLargeRetryAfter(t *testing.T) {
	header := http.Header{}
	header.Set("Retry-After", "9999")
	if got := anthropicRateLimitWait(header, 0); got >= 200*time.Second {
		t.Fatalf("expected fallback for huge Retry-After, got %v", got)
	}
}
