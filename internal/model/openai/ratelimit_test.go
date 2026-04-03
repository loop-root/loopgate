package openai

import (
	"net/http"
	"testing"
	"time"
)

func TestOpenAIRateLimitWait_RetryAfterSeconds(t *testing.T) {
	header := http.Header{}
	header.Set("Retry-After", "5")
	if got := openaiRateLimitWait(header, 99); got != 5*time.Second {
		t.Fatalf("got %v want 5s", got)
	}
}

func TestOpenAIRateLimitWait_ExponentialWithoutHeader(t *testing.T) {
	if got := openaiRateLimitWait(nil, 0); got != time.Second {
		t.Fatalf("attempt 0: got %v want 1s", got)
	}
	if got := openaiRateLimitWait(nil, 2); got != 4*time.Second {
		t.Fatalf("attempt 2: got %v want 4s", got)
	}
}
