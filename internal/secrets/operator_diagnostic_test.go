package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLoopgateOperatorErrorClass_NoEchoOfMessage(t *testing.T) {
	secretLike := "Bearer sk-live-abc123deadbeef and api_key=supersecret"
	wrapped := errors.New("model provider said: " + secretLike)
	class := LoopgateOperatorErrorClass(wrapped)
	if strings.Contains(class, "sk-live") || strings.Contains(class, "supersecret") || strings.Contains(class, "Bearer") {
		t.Fatalf("class must not echo error text, got %q", class)
	}
	if class != "unspecified" {
		t.Fatalf("expected unspecified for opaque error, got %q", class)
	}
}

func TestLoopgateOperatorErrorClass_Deadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer cancel()
	<-ctx.Done()
	if got := LoopgateOperatorErrorClass(ctx.Err()); got != "context_deadline" {
		t.Fatalf("got %q", got)
	}
}

func TestLoopgateOperatorErrorClass_JSON(t *testing.T) {
	_, err := json.Marshal(make(chan int))
	if err == nil {
		t.Fatal("expected marshal error")
	}
	if got := LoopgateOperatorErrorClass(err); got != "json_unsupported_type" && got != "json_marshal" {
		t.Fatalf("got %q for %v", got, err)
	}
}
