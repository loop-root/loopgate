package troubleshoot

import (
	"fmt"
	"os"
	"path/filepath"

	"loopgate/internal/config"
)

// DemoResetReport describes which local demo/runtime artifacts were removed.
type DemoResetReport struct {
	Removed []string
	Missing []string
}

// ResetDemoState removes local runtime artifacts that create noisy demo state.
// It is intentionally destructive and should only be used for local demo resets.
func ResetDemoState(repoRoot string, runtimeConfig config.RuntimeConfig, socketPath string) (DemoResetReport, error) {
	resetTargets := []string{
		filepath.Join(repoRoot, runtimeConfig.Logging.Diagnostic.ResolvedDirectory()),
		filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"),
		filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl.lock"),
		filepath.Join(repoRoot, runtimeConfig.Logging.AuditLedger.SegmentDir),
		filepath.Join(repoRoot, runtimeConfig.Logging.AuditExport.StatePath),
		filepath.Join(repoRoot, "runtime", "state", "claude_hook_sessions.json"),
		filepath.Join(repoRoot, "runtime", "state", "nonce_replay.json"),
		filepath.Join(repoRoot, "runtime", "state", "quarantine"),
		filepath.Join(repoRoot, "runtime", "state", "working_state.json"),
		filepath.Join(repoRoot, "runtime", "state", "model_runtime.json"),
		filepath.Join(repoRoot, "runtime", "state", "loopgate_model_connections.json"),
		filepath.Join(repoRoot, "runtime", "state", "config"),
		filepath.Join(repoRoot, "runtime", "state", "policy_hash.sha256"),
		filepath.Join(repoRoot, "runtime", "state", ".history"),
		filepath.Join(repoRoot, "runtime", "state", "haven_desk_notes.json"),
		filepath.Join(repoRoot, "runtime", "state", "haven_journal_resident.json"),
		filepath.Join(repoRoot, "runtime", "state", "haven_preferences.json"),
		filepath.Join(repoRoot, "runtime", "state", "haven_presence.json"),
	}
	if socketPath = filepath.Clean(socketPath); socketPath != "" {
		resetTargets = append(resetTargets, socketPath)
	}

	var resetReport DemoResetReport
	for _, resetTarget := range resetTargets {
		fileInfo, err := os.Stat(resetTarget)
		if err != nil {
			if os.IsNotExist(err) {
				resetReport.Missing = append(resetReport.Missing, resetTarget)
				continue
			}
			return DemoResetReport{}, fmt.Errorf("stat reset target %q: %w", resetTarget, err)
		}
		if fileInfo.IsDir() {
			if err := os.RemoveAll(resetTarget); err != nil {
				return DemoResetReport{}, fmt.Errorf("remove directory %q: %w", resetTarget, err)
			}
		} else {
			if err := os.Remove(resetTarget); err != nil {
				return DemoResetReport{}, fmt.Errorf("remove file %q: %w", resetTarget, err)
			}
		}
		resetReport.Removed = append(resetReport.Removed, resetTarget)
	}
	return resetReport, nil
}
