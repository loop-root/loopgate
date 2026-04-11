package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"morph/internal/loopgate"
	"morph/internal/memory"
)

func main() {
	rootFlags, acceptPolicy := newRootFlagSet()
	if err := rootFlags.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	repoRoot := os.Getenv("MORPH_REPO_ROOT")
	if strings.TrimSpace(repoRoot) == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, "ERROR: determine repo root:", err)
			os.Exit(1)
		}
	} else {
		repoRoot = filepath.Clean(repoRoot)
	}
	unsupportedRawMemoryArtifacts, rawMemoryInspectErr := memory.InspectUnsupportedRawMemoryArtifacts(repoRoot)
	if rawMemoryInspectErr != nil {
		fmt.Fprintln(os.Stderr, "WARN: inspect unsupported raw memory artifacts:", rawMemoryInspectErr)
	}
	for _, rawMemoryArtifact := range unsupportedRawMemoryArtifacts {
		fmt.Fprintln(os.Stderr, "WARN:", memory.FormatUnsupportedRawMemoryArtifactWarning(rawMemoryArtifact))
	}

	socketPath := filepath.Join(repoRoot, "runtime", "state", "loopgate.sock")
	if envSocket := strings.TrimSpace(os.Getenv("LOOPGATE_SOCKET")); envSocket != "" {
		socketPath = envSocket
	}
	server, err := loopgate.NewServerWithOptions(repoRoot, socketPath, *acceptPolicy)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: initialize loopgate server:", err)
		os.Exit(1)
	}
	defer server.CloseDiagnosticLogs()

	if hint := server.DiagnosticLogDirectoryMessage(); hint != "" {
		fmt.Fprintln(os.Stderr, hint)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	fmt.Printf("Loopgate listening on %s\n", socketPath)
	if err := server.Serve(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: serve loopgate:", err)
		os.Exit(1)
	}
}

func newRootFlagSet() (*flag.FlagSet, *bool) {
	rootFlags := flag.NewFlagSet("loopgate", flag.ContinueOnError)
	rootFlags.SetOutput(os.Stderr)
	acceptPolicy := rootFlags.Bool("accept-policy", false, "accept a changed policy file hash on startup")
	return rootFlags, acceptPolicy
}
