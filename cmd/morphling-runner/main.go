// morphling-runner is a minimal task-plan runner interface binary.
//
// It reads a TaskPlanRunnerConfig as JSON from stdin, executes the lease-bound
// capability through Loopgate mediation, finalizes the lease, and writes a
// TaskPlanRunnerResult as JSON to stdout before exiting.
//
// Important current constraint:
// this binary does not bypass Loopgate peer binding. When invoked as a distinct
// OS process with a parent process's delegated session credentials, execution is
// expected to fail with peer-binding denial. The real cross-process path is the
// morphling worker launch/open flow, not generic delegated-session reuse.
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
