package loopgate

import (
	"runtime"
	"testing"
)

func TestVerifySupportedExecutionPlatform_DarwinSucceeds(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("GOOS-specific check")
	}
	if err := verifySupportedExecutionPlatform(); err != nil {
		t.Fatalf("verifySupportedExecutionPlatform: %v", err)
	}
}

func TestVerifySupportedExecutionPlatform_NonDarwinRequiresOptIn(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("non-Darwin only")
	}
	t.Setenv("LOOPGATE_ALLOW_NON_DARWIN", "")
	if err := verifySupportedExecutionPlatform(); err == nil {
		t.Fatalf("expected error without LOOPGATE_ALLOW_NON_DARWIN on %s", runtime.GOOS)
	}
	t.Setenv("LOOPGATE_ALLOW_NON_DARWIN", "1")
	if err := verifySupportedExecutionPlatform(); err != nil {
		t.Fatalf("expected opt-in to succeed: %v", err)
	}
}
