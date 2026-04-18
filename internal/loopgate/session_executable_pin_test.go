package loopgate

import (
	"context"
	"errors"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"path/filepath"
	"testing"

	"loopgate/internal/config"
)

func TestOpenSessionRejectedWhenExecutablePinMismatch(t *testing.T) {
	repoRoot := t.TempDir()
	cfg := config.DefaultRuntimeConfig()
	cfg.ControlPlane.ExpectedSessionClientExecutable = "/nonexistent/loopgate/wrong_client_executable"
	client, _, _ := startLoopgateServerWithRuntime(t, repoRoot, loopgatePolicyYAML(false), &cfg, false)
	client.ConfigureSession("test-actor", "test-session", []string{"fs_list"})
	_, err := client.ensureCapabilityToken(context.Background())
	var denied RequestDeniedError
	if !errors.As(err, &denied) || denied.DenialCode != controlapipkg.DenialCodeProcessBindingRejected {
		t.Fatalf("expected process binding denial, got %v", err)
	}
}

func TestOpenSessionSucceedsWhenExecutablePinMatches(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("executable path: %v", err)
	}
	pin := filepath.Clean(exe)
	repoRoot := t.TempDir()
	cfg := config.DefaultRuntimeConfig()
	cfg.ControlPlane.ExpectedSessionClientExecutable = pin
	client, _, _ := startLoopgateServerWithRuntime(t, repoRoot, loopgatePolicyYAML(false), &cfg, true)
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("expected session open with matching executable pin: %v", err)
	}
}
