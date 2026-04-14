package troubleshoot

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"loopgate/internal/config"
)

func testRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func TestBuildReport_MorphWorkspace(t *testing.T) {
	root := testRepoRoot(t)
	rc, err := LoadEffectiveRuntimeConfig(root)
	if err != nil {
		t.Fatalf("load effective runtime: %v", err)
	}
	rep, err := BuildReport(root, rc)
	if err != nil {
		t.Fatalf("build report: %v", err)
	}
	if got := filepath.Base(rep.LedgerActive.ActiveFile); got != "loopgate_events.jsonl" {
		t.Fatalf("active file basename: %q", got)
	}
	if rep.LedgerActive.LineCount < 0 {
		t.Fatal("line count")
	}
	if rep.Diagnostics.Enabled != rc.Logging.Diagnostic.Enabled {
		t.Fatalf("diagnostic enabled mismatch")
	}
}

func TestBuildReport_ActiveSummaryErrorOnMalformedLine(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "runtime", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(stateDir, "loopgate_events.jsonl")
	if err := os.WriteFile(active, []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rc := config.DefaultRuntimeConfig()
	rep, err := BuildReport(dir, rc)
	if err != nil {
		t.Fatalf("unexpected build error: %v", err)
	}
	if rep.LedgerActive.SummaryError == "" {
		t.Fatal("expected summary_error for malformed ledger line")
	}
}
