// Command loopgate-ledger inspects the Loopgate hash-chained audit JSONL (active file + rotation manifest).
// It does not replace server-side integrity checks; use verify after incidents or before trusting history.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"morph/internal/config"
	"morph/internal/ledger"
)

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
  loopgate-ledger verify  [-repo DIR]   Verify hash chain (active JSONL + manifest / sealed segments per config)
  loopgate-ledger summary [-repo DIR]   Count events by type on the active JSONL only (no chain verification)
  loopgate-ledger tail    [-repo DIR] [-n N]  Print last N events from active JSONL as key=value lines (no verification)

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
	lastSeq, lastHash, err := ledger.ReadSegmentedChainState(path, "audit_sequence", rotation)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: verify chain:", err)
		os.Exit(1)
	}
	fmt.Printf("verify ok  last_audit_sequence=%d  last_event_hash_prefix=%s\n", lastSeq, hashPrefix(lastHash))
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
	_ = fs.Parse(args)
	repoRoot := resolveRepoRoot(*repoFlag)
	n := *nFlag
	if n < 1 {
		n = 1
	}
	path := activeAuditPath(repoRoot)
	ring := make([]string, 0, n)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("(no active ledger file)")
			return
		}
		fmt.Fprintln(os.Stderr, "ERROR: open ledger:", err)
		os.Exit(1)
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
		fmt.Fprintln(os.Stderr, "ERROR: read ledger:", err)
		os.Exit(1)
	}
	for _, raw := range ring {
		ev, ok := ledger.ParseEvent([]byte(raw))
		if !ok {
			fmt.Println("malformed:", raw)
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
		fmt.Printf("ts=%s type=%s session=%s audit_sequence=%s event_hash_prefix=%s\n", ev.TS, ev.Type, ev.Session, seq, hashP)
	}
}
