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
	"morph/internal/loopgate/mcpserve"
	"morph/internal/memory"
)

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "mcp-serve" {
		mcpServeFlags := flag.NewFlagSet("mcp-serve", flag.ContinueOnError)
		mcpServeFlags.SetOutput(os.Stderr)
		localOpenSession := mcpServeFlags.Bool("local-open-session", false, "Local/dev IDE integration only. Opens a normal local Loopgate session over the Unix socket; not a new general auth model or remote bootstrap path.")
		localActor := mcpServeFlags.String("actor", mcpserve.DefaultActor, "Actor label for local-open-session mode.")
		localClientSession := mcpServeFlags.String("client-session", mcpserve.DefaultClientSession, "Client session label for local-open-session mode.")
		localRequestedCapabilities := mcpServeFlags.String("requested-capabilities", "", "Comma-separated requested capabilities for local-open-session mode. Required when -local-open-session is set.")
		localWorkspaceID := mcpServeFlags.String("workspace-id", "", "Optional workspace id bound into local-open-session requests.")
		if err := mcpServeFlags.Parse(os.Args[2:]); err != nil {
			os.Exit(2)
		}

		runOptions := mcpserve.RunOptions{}
		if *localOpenSession {
			requestedCapabilities := splitAndTrimCommaList(*localRequestedCapabilities)
			runOptions.LocalOpenSession = &mcpserve.LocalOpenSessionConfig{
				Actor:                 *localActor,
				ClientSession:         *localClientSession,
				RequestedCapabilities: requestedCapabilities,
				WorkspaceID:           strings.TrimSpace(*localWorkspaceID),
			}
		}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		if err := mcpserve.RunWithOptions(ctx, runOptions); err != nil {
			fmt.Fprintln(os.Stderr, "ERROR: mcp-serve:", err)
			os.Exit(1)
		}
		return
	}

	acceptPolicy := flag.Bool("accept-policy", false, "accept a changed policy file hash on startup")
	enableAdmin := flag.Bool("admin", false, "start loopback admin console TCP server when admin_console.enabled is true in config/runtime.yaml")
	flag.Parse()

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
		socketPath = filepath.Clean(envSocket)
	}
	server, err := loopgate.NewServerWithOptions(repoRoot, socketPath, *acceptPolicy, *enableAdmin)
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
	if adminAddr := server.ConfiguredAdminListenAddr(); adminAddr != "" {
		fmt.Fprintf(os.Stderr, "Admin console (configured bind): tcp://%s — open http://%s/admin/ after startup\n", adminAddr, adminAddr)
	}
	if err := server.Serve(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: serve loopgate:", err)
		os.Exit(1)
	}
}

func splitAndTrimCommaList(raw string) []string {
	rawParts := strings.Split(raw, ",")
	trimmedParts := make([]string, 0, len(rawParts))
	for _, rawPart := range rawParts {
		trimmedPart := strings.TrimSpace(rawPart)
		if trimmedPart == "" {
			continue
		}
		trimmedParts = append(trimmedParts, trimmedPart)
	}
	return trimmedParts
}
