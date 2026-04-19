// Command loopgate-doctor produces operator diagnostic bundles, offline JSON reports,
// and live trust preflight checks for local troubleshooting.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"loopgate/internal/loopgate"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"loopgate/internal/troubleshoot"
)

var checkAuditExportTrust = func(socketPath string) (controlapipkg.AuditExportTrustCheckResponse, error) {
	client := loopgate.NewClient(socketPath)
	client.ConfigureSession("loopgate-doctor", defaultDoctorSessionID("trust-check"), []string{"diagnostic.read"})
	return client.CheckAuditExportTrust(context.Background())
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) < 1 {
		printUsage(stderr)
		return 2
	}
	switch args[0] {
	case "report":
		return runReport(args[1:], stdout, stderr)
	case "bundle":
		return runBundle(args[1:], stdout, stderr)
	case "explain-denial":
		return runExplainDenial(args[1:], stdout, stderr)
	case "trust-check":
		return runTrustCheck(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stderr)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown subcommand %q\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintf(w, `Usage:
  loopgate-doctor report      [-repo DIR]                        Print offline JSON diagnostic report to stdout
  loopgate-doctor bundle      [-repo DIR] -out DIR [-log-lines N]   Write report.json + diagnostic log tails
  loopgate-doctor explain-denial [-repo DIR] -approval-id ID    Explain one approval request from the verified audit ledger
  loopgate-doctor trust-check [-repo DIR] [-socket PATH]            Query the running local Loopgate audit-export trust preflight

-repo defaults to the current working directory.
trust-check defaults to LOOPGATE_SOCKET or <repo>/runtime/state/loopgate.sock.
Effective runtime config for offline report/bundle matches Loopgate: config/runtime.yaml (plus optional diagnostic override JSON).
`)
}

func parseRepoFlag(fs *flag.FlagSet) func() (string, error) {
	repo := fs.String("repo", "", "repository root (default: cwd)")
	return func() (string, error) {
		if s := strings.TrimSpace(*repo); s != "" {
			return filepath.Clean(s), nil
		}
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return cwd, nil
	}
}

func runReport(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoFn := parseRepoFlag(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	repoRoot, err := repoFn()
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}
	runtimeConfig, err := troubleshoot.LoadEffectiveRuntimeConfig(repoRoot)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: load runtime config:", err)
		return 1
	}
	report, err := troubleshoot.BuildReport(repoRoot, runtimeConfig)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: build report:", err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintln(stderr, "ERROR: encode:", err)
		return 1
	}
	return 0
}

func runBundle(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("bundle", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoFn := parseRepoFlag(fs)
	outDirFlag := fs.String("out", "", "output directory (required)")
	logLinesFlag := fs.Int("log-lines", 200, "max lines to copy per diagnostic log file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*outDirFlag) == "" {
		fmt.Fprintln(stderr, "ERROR: -out is required")
		return 2
	}
	repoRoot, err := repoFn()
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}
	runtimeConfig, err := troubleshoot.LoadEffectiveRuntimeConfig(repoRoot)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: load runtime config:", err)
		return 1
	}
	outDir := filepath.Clean(*outDirFlag)
	if err := troubleshoot.WriteOperatorBundle(repoRoot, runtimeConfig, outDir, *logLinesFlag); err != nil {
		fmt.Fprintln(stderr, "ERROR: write bundle:", err)
		return 1
	}
	fmt.Fprintln(stdout, "wrote bundle to", outDir)
	return 0
}

func runExplainDenial(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("explain-denial", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoFn := parseRepoFlag(fs)
	approvalIDFlag := fs.String("approval-id", "", "approval request id to explain")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*approvalIDFlag) == "" {
		fmt.Fprintln(stderr, "ERROR: -approval-id is required")
		return 2
	}
	repoRoot, err := repoFn()
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}
	runtimeConfig, err := troubleshoot.LoadEffectiveRuntimeConfig(repoRoot)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: load runtime config:", err)
		return 1
	}
	explanation, err := troubleshoot.ExplainApprovalRequest(repoRoot, runtimeConfig, *approvalIDFlag)
	if err != nil {
		if errors.Is(err, troubleshoot.ErrApprovalRequestNotFound) {
			fmt.Fprintln(stderr, "ERROR:", err)
			return 1
		}
		fmt.Fprintln(stderr, "ERROR: explain approval denial:", err)
		return 1
	}
	if err := troubleshoot.WriteApprovalExplanation(stdout, explanation); err != nil {
		fmt.Fprintln(stderr, "ERROR: write explanation:", err)
		return 1
	}
	return 0
}

func runTrustCheck(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("trust-check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoFn := parseRepoFlag(fs)
	socketPathFlag := fs.String("socket", "", "Unix socket path (default: LOOPGATE_SOCKET or <repo>/runtime/state/loopgate.sock)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	repoRoot, err := repoFn()
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}
	socketPath := resolveSocketPath(repoRoot, *socketPathFlag)
	trustCheckResponse, err := checkAuditExportTrust(socketPath)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: audit export trust check:", err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(trustCheckResponse); err != nil {
		fmt.Fprintln(stderr, "ERROR: encode:", err)
		return 1
	}
	return 0
}

func resolveSocketPath(repoRoot string, socketPathFlag string) string {
	if trimmedSocketPath := strings.TrimSpace(socketPathFlag); trimmedSocketPath != "" {
		return filepath.Clean(trimmedSocketPath)
	}
	if socketPathFromEnv := strings.TrimSpace(os.Getenv("LOOPGATE_SOCKET")); socketPathFromEnv != "" {
		return filepath.Clean(socketPathFromEnv)
	}
	return filepath.Join(repoRoot, "runtime", "state", "loopgate.sock")
}

func defaultDoctorSessionID(subcommandName string) string {
	trimmedSubcommandName := strings.TrimSpace(subcommandName)
	if trimmedSubcommandName == "" {
		trimmedSubcommandName = "doctor"
	}
	return "loopgate-doctor-" + trimmedSubcommandName + "-" + strconv.Itoa(os.Getpid())
}
