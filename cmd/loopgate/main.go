package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"loopgate/internal/loopgate"
)

var exitProcess = os.Exit

func main() {
	if handleLoopgateSubcommand(os.Args[1:]) {
		return
	}
	if len(os.Args) > 1 {
		fmt.Fprintln(os.Stderr, "ERROR: startup flags are no longer supported; policy changes require a valid detached signature, not --accept-policy")
		exitProcess(2)
	}

	repoRoot := os.Getenv("MORPH_REPO_ROOT")
	if strings.TrimSpace(repoRoot) == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, "ERROR: determine repo root:", err)
			exitProcess(1)
		}
	} else {
		repoRoot = filepath.Clean(repoRoot)
	}
	socketPath := filepath.Join(repoRoot, "runtime", "state", "loopgate.sock")
	if envSocket := strings.TrimSpace(os.Getenv("LOOPGATE_SOCKET")); envSocket != "" {
		socketPath = envSocket
	}
	server, err := loopgate.NewServerWithOptions(repoRoot, socketPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: initialize loopgate server:", err)
		exitProcess(1)
	}
	defer server.CloseDiagnosticLogs()

	if hint := server.DiagnosticLogDirectoryMessage(); hint != "" {
		fmt.Fprintln(os.Stderr, hint)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	fmt.Printf("Loopgate listening on %s\n", socketPath)
	fmt.Println(server.AuditIntegrityModeMessage())
	if err := server.Serve(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: serve loopgate:", err)
		exitProcess(1)
	}
}
