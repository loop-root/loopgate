package main

import (
	"context"
	"testing"
	"time"
)

func TestWithFolderAccessTimeoutAddsDeadlineWhenMissing(t *testing.T) {
	ctx, cancel := withFolderAccessTimeout(context.Background())
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected folder access timeout context to include a deadline")
	}

	remaining := time.Until(deadline)
	if remaining < 110*time.Second || remaining > 121*time.Second {
		t.Fatalf("expected deadline close to %s, got remaining=%s", folderAccessRequestTimeout, remaining)
	}
}

func TestWithFolderAccessTimeoutPreservesExistingDeadline(t *testing.T) {
	parentContext, cancelParent := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelParent()

	ctx, cancel := withFolderAccessTimeout(parentContext)
	defer cancel()

	parentDeadline, _ := parentContext.Deadline()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected preserved deadline")
	}
	if !deadline.Equal(parentDeadline) {
		t.Fatalf("expected existing deadline %s to be preserved, got %s", parentDeadline, deadline)
	}
}
