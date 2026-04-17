//go:build unix

package loopgate

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestServe_CreatesSocketWithPrivatePermissionsUnderPermissiveUmask(t *testing.T) {
	repoRoot := newShortLoopgateTestRepoRoot(t)
	socketPath := newShortLoopgateSocketPath(t)
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))

	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	previousUmask := syscall.Umask(0)
	defer func() { _ = syscall.Umask(previousUmask) }()

	serverContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(serverContext)
	}()
	defer func() {
		cancel()
		select {
		case serveErr := <-serveDone:
			if serveErr != nil {
				t.Fatalf("serve exit: %v", serveErr)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for serve shutdown")
		}
	}()

	client := NewClient(socketPath)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := client.Health(context.Background()); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("wait for loopgate health at %s", socketPath)
		}
		time.Sleep(25 * time.Millisecond)
	}

	socketInfo, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if socketInfo.Mode().Perm() != 0o600 {
		t.Fatalf("expected socket mode 0600, got %#o", socketInfo.Mode().Perm())
	}
}
