package main

import (
	"io"
)

// quickstart is a thin wrapper over setup that accepts the recommended defaults
// without interactive prompts. It keeps onboarding on the same audited,
// signed-policy path as setup rather than introducing a second installer flow.
func runQuickstart(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	quickstartArgs := make([]string, 0, len(args)+1)
	quickstartArgs = append(quickstartArgs, "-yes")
	quickstartArgs = append(quickstartArgs, args...)
	return runSetup(quickstartArgs, stdin, stdout, stderr)
}
