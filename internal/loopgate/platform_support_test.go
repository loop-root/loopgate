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

func TestVerifySupportedExecutionPlatform_NonDarwinFailsClosed(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("non-Darwin only")
	}
	if err := verifySupportedExecutionPlatform(); err == nil {
		t.Fatalf("expected non-Darwin platform rejection on %s", runtime.GOOS)
	}
}
