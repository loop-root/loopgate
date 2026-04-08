// morphling-runner is a minimal separate-process morphling executor.
//
// It reads a TaskPlanRunnerConfig as JSON from stdin, executes the lease-bound
// capability through Loopgate mediation, finalizes the lease, and writes a
// TaskPlanRunnerResult as JSON to stdout before exiting.
//
// This binary proves that morphling execution can occur in a separate process
// communicating with Loopgate via the Unix domain socket. It does NOT implement
// process isolation, sandboxing, or IDE integration.
//
// Usage:
//
//	echo '{"socket_path":"/path/to/loopgate.sock",...}' | morphling-runner
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"morph/internal/loopgate"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	configBytes, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read config from stdin: %v\n", err)
		os.Exit(1)
	}

	resultBytes, err := loopgate.RunMorphlingRunnerProcess(ctx, configBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "runner process failed: %v\n", err)
		os.Exit(1)
	}

	if _, err := os.Stdout.Write(resultBytes); err != nil {
		fmt.Fprintf(os.Stderr, "write result to stdout: %v\n", err)
		os.Exit(1)
	}
}
