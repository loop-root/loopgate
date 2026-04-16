// Command loopgate-ledger inspects the Loopgate hash-chained audit JSONL (active file + rotation manifest).
// It does not replace server-side integrity checks; use verify after incidents or before trusting history.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
	"loopgate/internal/loopgate"
	"loopgate/internal/troubleshoot"
)

var ensureLoopgateStoppedForDemoReset = ensureLoopgateStopped

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}
	sub := os.Args[1]
	switch sub {
	case "verify":
		runVerify(os.Args[2:])
	case "summary":
		runSummary(os.Args[2:])
	case "tail":
		runTail(os.Args[2:])
	case "demo-reset":
		runDemoReset(os.Args[2:])
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", sub)
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage:
  loopgate-ledger verify  [-repo DIR]   Verify hash chain and, when configured, HMAC checkpoints
  loopgate-ledger summary [-repo DIR]   Count events by type on the active JSONL only (no chain verification)
  loopgate-ledger tail    [-repo DIR] [-n N] [-verbose]  Print last N events from active JSONL (no verification)
  loopgate-ledger demo-reset [-repo DIR] [-socket PATH] [-yes]  Remove local demo ledger/log state after confirming Loopgate is not running

-repo defaults to the current working directory.
`)
}

func resolveRepoRoot(flagValue string) string {
	if strings.TrimSpace(flagValue) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, "ERROR: repo root:", err)
			os.Exit(1)
		}
		return cwd
	}
	return filepath.Clean(flagValue)
}

func loadRotationSettings(repoRoot string) ledger.RotationSettings {
	runtimeConfig, err := config.LoadRuntimeConfig(repoRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: load runtime config:", err)
		os.Exit(1)
	}
	verifyClosed := true
	if runtimeConfig.Logging.AuditLedger.VerifyClosedSegmentsOnStartup != nil {
		verifyClosed = *runtimeConfig.Logging.AuditLedger.VerifyClosedSegmentsOnStartup
	}
	return ledger.RotationSettings{
		MaxEventBytes:                 runtimeConfig.Logging.AuditLedger.MaxEventBytes,
		RotateAtBytes:                 runtimeConfig.Logging.AuditLedger.RotateAtBytes,
		SegmentDir:                    filepath.Join(repoRoot, runtimeConfig.Logging.AuditLedger.SegmentDir),
		ManifestPath:                  filepath.Join(repoRoot, runtimeConfig.Logging.AuditLedger.ManifestPath),
		VerifyClosedSegmentsOnStartup: verifyClosed,
	}
}

func activeAuditPath(repoRoot string) string {
	return filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl")
}

func hashPrefix(full string) string {
	if len(full) <= 16 {
		return full
	}
	return full[:16]
}

func runVerify(args []string) {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	repoFlag := fs.String("repo", "", "repository root (default: current directory)")
	_ = fs.Parse(args)
	repoRoot := resolveRepoRoot(*repoFlag)
	rotation := loadRotationSettings(repoRoot)
	path := activeAuditPath(repoRoot)
	runtimeConfig, err := config.LoadRuntimeConfig(repoRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: load runtime config:", err)
		os.Exit(1)
	}
	lastSeq, lastHash, err := ledger.ReadSegmentedChainState(path, "audit_sequence", rotation)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: verify chain:", err)
		os.Exit(1)
	}
	checkpointReport, err := troubleshoot.VerifyAuditLedgerCheckpoints(repoRoot, runtimeConfig)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: verify checkpoints:", err)
		os.Exit(1)
	}
	fmt.Println(formatVerifySummary(lastSeq, lastHash, checkpointReport.Status, checkpointReport.CheckpointCount))
}

func formatVerifySummary(lastAuditSequence int64, lastEventHash string, checkpointStatus string, checkpointCount int) string {
	return fmt.Sprintf(
		"verify ok  last_audit_sequence=%d  last_event_hash_prefix=%s  hmac_checkpoints=%s  checkpoint_count=%d",
		lastAuditSequence,
		hashPrefix(lastEventHash),
		checkpointStatus,
		checkpointCount,
	)
}

func runSummary(args []string) {
	fs := flag.NewFlagSet("summary", flag.ExitOnError)
	repoFlag := fs.String("repo", "", "repository root (default: current directory)")
	_ = fs.Parse(args)
	repoRoot := resolveRepoRoot(*repoFlag)
	path := activeAuditPath(repoRoot)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("active ledger file missing; event count 0")
			return
		}
		fmt.Fprintln(os.Stderr, "ERROR: open ledger:", err)
		os.Exit(1)
	}
	defer f.Close()

	counts := make(map[string]int)
	var lines int
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		lines++
		ev, ok := ledger.ParseEvent(scanner.Bytes())
		if !ok {
			fmt.Fprintf(os.Stderr, "ERROR: malformed JSONL at line %d\n", lines)
			os.Exit(1)
		}
		counts[ev.Type]++
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: read ledger:", err)
		os.Exit(1)
	}
	fmt.Printf("active_file=%s  lines=%d\n", path, lines)
	type pair struct {
		t string
		n int
	}
	var list []pair
	for t, n := range counts {
		list = append(list, pair{t: t, n: n})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].n != list[j].n {
			return list[i].n > list[j].n
		}
		return list[i].t < list[j].t
	})
	for _, p := range list {
		fmt.Printf("  %8d  %s\n", p.n, p.t)
	}
}

func runTail(args []string) {
	fs := flag.NewFlagSet("tail", flag.ExitOnError)
	repoFlag := fs.String("repo", "", "repository root (default: current directory)")
	nFlag := fs.Int("n", 20, "number of trailing events to print")
	verboseFlag := fs.Bool("verbose", false, "render recent events in a human-readable demo-friendly format")
	_ = fs.Parse(args)
	repoRoot := resolveRepoRoot(*repoFlag)
	n := *nFlag
	if n < 1 {
		n = 1
	}
	verbose := *verboseFlag
	if exitCode := runTailWithIO(repoRoot, n, verbose, os.Stdout, os.Stderr); exitCode != 0 {
		os.Exit(exitCode)
	}
}

func runDemoReset(args []string) {
	fs := flag.NewFlagSet("demo-reset", flag.ExitOnError)
	repoFlag := fs.String("repo", "", "repository root (default: current directory)")
	socketFlag := fs.String("socket", "", "Unix socket path (default: LOOPGATE_SOCKET or <repo>/runtime/state/loopgate.sock)")
	yesFlag := fs.Bool("yes", false, "confirm destructive local demo reset")
	_ = fs.Parse(args)
	repoRoot := resolveRepoRoot(*repoFlag)
	socketPath := resolveSocketPath(repoRoot, *socketFlag)
	if exitCode := runDemoResetWithIO(repoRoot, socketPath, *yesFlag, os.Stdout, os.Stderr); exitCode != 0 {
		os.Exit(exitCode)
	}
}

func runTailWithIO(repoRoot string, n int, verbose bool, stdout io.Writer, stderr io.Writer) int {
	path := activeAuditPath(repoRoot)
	ring := make([]string, 0, n)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(stdout, "(no active ledger file)")
			return 0
		}
		fmt.Fprintln(stderr, "ERROR: open ledger:", err)
		return 1
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		raw := scanner.Text()
		if len(ring) < cap(ring) {
			ring = append(ring, raw)
		} else {
			copy(ring, ring[1:])
			ring[len(ring)-1] = raw
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(stderr, "ERROR: read ledger:", err)
		return 1
	}
	for _, raw := range ring {
		ev, ok := ledger.ParseEvent([]byte(raw))
		if !ok {
			fmt.Fprintln(stdout, "malformed:", raw)
			continue
		}
		if verbose {
			fmt.Fprintln(stdout, formatVerboseTailEvent(ev))
			continue
		}
		seq := ""
		hashP := ""
		if ev.Data != nil {
			if v, ok := ev.Data["audit_sequence"]; ok {
				seq = fmt.Sprintf("%v", v)
			}
			if h, ok := ev.Data["event_hash"].(string); ok {
				hashP = hashPrefix(h)
			}
		}
		fmt.Fprintf(stdout, "ts=%s type=%s session=%s audit_sequence=%s event_hash_prefix=%s\n", ev.TS, ev.Type, ev.Session, seq, hashP)
	}
	return 0
}

func runDemoResetWithIO(repoRoot string, socketPath string, confirmed bool, stdout io.Writer, stderr io.Writer) int {
	if !confirmed {
		fmt.Fprintln(stderr, "ERROR: demo-reset is destructive; rerun with -yes")
		return 2
	}
	if err := ensureLoopgateStoppedForDemoReset(socketPath); err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}
	runtimeConfig, err := config.LoadRuntimeConfig(repoRoot)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: load runtime config:", err)
		return 1
	}
	resetReport, err := troubleshoot.ResetDemoState(repoRoot, runtimeConfig, socketPath)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: demo reset:", err)
		return 1
	}
	fmt.Fprintln(stdout, "demo reset complete")
	for _, removedPath := range resetReport.Removed {
		fmt.Fprintf(stdout, "removed %s\n", removedPath)
	}
	if len(resetReport.Removed) == 0 {
		fmt.Fprintln(stdout, "removed nothing; demo state was already clean")
	}
	return 0
}

func formatVerboseTailEvent(ev ledger.Event) string {
	status := strings.ToUpper(strings.TrimSpace(tailEventDataString(ev.Data, "decision")))
	if status == "" {
		status = verboseTailStatusFallback(ev)
	}
	summary := verboseTailSummary(ev)
	return fmt.Sprintf("ts=%s  %-7s  %s", strings.TrimSpace(ev.TS), status, summary)
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

func ensureLoopgateStopped(socketPath string) error {
	if strings.TrimSpace(socketPath) == "" {
		return nil
	}
	socketPath = filepath.Clean(socketPath)
	if _, err := os.Stat(socketPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat loopgate socket: %w", err)
	}
	healthClient := loopgate.NewClient(socketPath)
	healthContext, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if _, err := healthClient.Health(healthContext); err == nil {
		return fmt.Errorf("refusing demo reset while Loopgate is running at %s", socketPath)
	}
	return nil
}

func verboseTailStatusFallback(ev ledger.Event) string {
	if status := strings.ToUpper(strings.TrimSpace(tailEventDataString(ev.Data, "status"))); status != "" {
		return status
	}
	switch ev.Type {
	case "capability.denied", "approval.denied":
		return "DENIED"
	case "capability.executed":
		return "SUCCESS"
	case "capability.error":
		return "ERROR"
	case "approval.created":
		return "PENDING"
	case "approval.granted":
		return "GRANTED"
	default:
		return "INFO"
	}
}

func verboseTailSummary(ev ledger.Event) string {
	toolName := strings.TrimSpace(tailEventDataString(ev.Data, "tool_name"))
	commandPreview := strings.TrimSpace(tailEventDataString(ev.Data, "command_redacted_preview"))
	reason := strings.TrimSpace(tailEventDataString(ev.Data, "reason"))
	if toolName != "" && commandPreview != "" {
		if reason != "" {
			return fmt.Sprintf("%s: %s — %s", toolName, commandPreview, reason)
		}
		return fmt.Sprintf("%s: %s", toolName, commandPreview)
	}
	if toolName != "" {
		if reason != "" {
			return fmt.Sprintf("%s — %s", toolName, reason)
		}
		return toolName
	}
	if capability := strings.TrimSpace(tailEventDataString(ev.Data, "capability")); capability != "" {
		if reason != "" {
			return fmt.Sprintf("%s — %s", capability, reason)
		}
		return capability
	}
	if reason != "" {
		return fmt.Sprintf("%s — %s", ev.Type, reason)
	}
	return ev.Type
}

func tailEventDataString(data map[string]interface{}, key string) string {
	if data == nil {
		return ""
	}
	rawValue, ok := data[key]
	if !ok || rawValue == nil {
		return ""
	}
	switch typedValue := rawValue.(type) {
	case string:
		return typedValue
	case bool:
		return strconv.FormatBool(typedValue)
	case float64:
		return strconv.FormatInt(int64(typedValue), 10)
	case int:
		return strconv.Itoa(typedValue)
	case int64:
		return strconv.FormatInt(typedValue, 10)
	default:
		return ""
	}
}
