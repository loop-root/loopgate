// Command loopgate-doctor produces operator diagnostic bundles and JSON reports for troubleshooting.
// Prefer this or GET /v1/diagnostic/report (authenticated) over reading raw audit JSONL by hand.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"morph/internal/troubleshoot"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "report":
		runReport(os.Args[2:])
	case "bundle":
		runBundle(os.Args[2:])
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", os.Args[1])
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage:
  loopgate-doctor report  [-repo DIR]   Print JSON diagnostic report to stdout
  loopgate-doctor bundle  [-repo DIR] -out DIR [-log-lines N]   Write report.json + diagnostic log tails

-repo defaults to the current working directory.
Effective runtime config matches Loopgate: config/runtime.yaml (plus optional diagnostic override JSON).
`)
}

func parseRepoFlag(fs *flag.FlagSet) func() string {
	repo := fs.String("repo", "", "repository root (default: cwd)")
	return func() string {
		if s := strings.TrimSpace(*repo); s != "" {
			return filepath.Clean(s)
		}
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, "ERROR:", err)
			os.Exit(1)
		}
		return cwd
	}
}

func runReport(args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	repoFn := parseRepoFlag(fs)
	_ = fs.Parse(args)
	repoRoot := repoFn()
	rc, err := troubleshoot.LoadEffectiveRuntimeConfig(repoRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: load runtime config:", err)
		os.Exit(1)
	}
	rep, err := troubleshoot.BuildReport(repoRoot, rc)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: build report:", err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rep); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: encode:", err)
		os.Exit(1)
	}
}

func runBundle(args []string) {
	fs := flag.NewFlagSet("bundle", flag.ExitOnError)
	repoFn := parseRepoFlag(fs)
	out := fs.String("out", "", "output directory (required)")
	logLines := fs.Int("log-lines", 200, "max lines to copy per diagnostic log file")
	_ = fs.Parse(args)
	if strings.TrimSpace(*out) == "" {
		fmt.Fprintln(os.Stderr, "ERROR: -out is required")
		os.Exit(2)
	}
	repoRoot := repoFn()
	rc, err := troubleshoot.LoadEffectiveRuntimeConfig(repoRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: load runtime config:", err)
		os.Exit(1)
	}
	outDir := filepath.Clean(*out)
	if err := troubleshoot.WriteOperatorBundle(repoRoot, rc, outDir, *logLines); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: write bundle:", err)
		os.Exit(1)
	}
	fmt.Println("wrote bundle to", outDir)
}
