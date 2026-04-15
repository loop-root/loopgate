package state

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// RuntimeState holds Loopgate's persistent runtime state.
// This should stay small and stable; detailed history belongs in the ledger.
type RuntimeState struct {
	SessionID         string `json:"session_id"`
	StartedAtUTC      string `json:"started_at_utc"`
	TurnCount         int    `json:"turn_count"`
	DistillCursorLine int    `json:"distill_cursor_line"`
	LastActivityUTC   string `json:"last_activity_utc"`
}

// LoadOrInit loads state from path. If missing or corrupt, it initializes a new state.
// Corrupt state files are preserved by renaming them with a `.corrupt.<timestamp>` suffix.
func LoadOrInit(path string) (RuntimeState, error) {
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			state := New()
			return state, Save(path, state)
		}
		return RuntimeState{}, err
	}

	var state RuntimeState
	if err := json.Unmarshal(rawBytes, &state); err != nil {
		// If state is corrupt, preserve it for forensics, then reinitialize.
		// We do NOT silently discard; we rename with a timestamp suffix.
		corruptPath := path + ".corrupt." + time.Now().UTC().Format("20060102-150405")
		_ = os.Rename(path, corruptPath)

		fresh := New()
		// Best effort: persist a usable state so Loopgate can start.
		if saveErr := Save(path, fresh); saveErr != nil {
			return RuntimeState{}, saveErr
		}
		return fresh, nil
	}

	// Backfill defaults for older state files.
	if state.SessionID == "" {
		state.SessionID = MakeSessionID()
	}
	if state.StartedAtUTC == "" {
		state.StartedAtUTC = nowUTC()
	}
	if state.LastActivityUTC == "" {
		state.LastActivityUTC = state.StartedAtUTC
	}

	return state, nil
}

// Save writes state to path as pretty JSON.
// It is crash-safe: write to a temp file in the same directory, fsync, then rename.
func Save(path string, state RuntimeState) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	jsonBytes, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	tmp := path + ".tmp"

	// Create/overwrite temp file with restrictive permissions, then write.
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}

	// Ensure we close on all paths.
	defer func() { _ = f.Close() }()

	if _, err := f.Write(jsonBytes); err != nil {
		return err
	}
	// Ensure the file ends with a newline for readability/debugging (optional).
	if len(jsonBytes) == 0 || jsonBytes[len(jsonBytes)-1] != '\n' {
		if _, err := io.WriteString(f, "\n"); err != nil {
			return err
		}
	}

	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	// Atomic on POSIX when source+dest are on same filesystem.
	if err := os.Rename(tmp, path); err != nil {
		return err
	}

	// Best-effort directory fsync for durability of the rename.
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}

	// Ensure final permissions are sane (tmp is 0600; keep state file private).
	_ = os.Chmod(path, 0600)
	return nil
}

// New creates a fresh runtime state for a new session.
func New() RuntimeState {
	now := nowUTC()
	return RuntimeState{
		SessionID:       MakeSessionID(),
		StartedAtUTC:    now,
		TurnCount:       0,
		LastActivityUTC: now,
		// DistillCursorLine intentionally starts at 0.
	}
}

// TouchActivity updates the activity timestamp.
func TouchActivity(state *RuntimeState) {
	state.LastActivityUTC = nowUTC()
}

// IncrementTurn increments the turn counter.
func IncrementTurn(state *RuntimeState) {
	state.TurnCount++
	TouchActivity(state)
}

// MakeSessionID returns a reasonably unique session id.
func MakeSessionID() string {
	randBytes := make([]byte, 8)
	_, _ = rand.Read(randBytes)
	suffix := hex.EncodeToString(randBytes)
	return fmt.Sprintf("s-%s-%s", time.Now().UTC().Format("20060102-150405"), suffix)
}

func nowUTC() string { return time.Now().UTC().Format(time.RFC3339) }
