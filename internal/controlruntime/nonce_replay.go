package controlruntime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type SeenRequest struct {
	ControlSessionID string
	SeenAt           time.Time
}

type AuthNonceReplayStore interface {
	Load(nowUTC time.Time) (map[string]SeenRequest, error)
	Record(nonceKey string, seenNonce SeenRequest) error
	Compact(seenAuthNonces map[string]SeenRequest) error
}

func NewSnapshotNonceReplayStore(path string, replayWindow time.Duration) AuthNonceReplayStore {
	return snapshotNonceReplayStore{
		path:         path,
		replayWindow: replayWindow,
	}
}

func NewAppendOnlyNonceReplayStore(path string, legacySnapshotPath string, replayWindow time.Duration) AuthNonceReplayStore {
	return appendOnlyNonceReplayStore{
		path:               path,
		legacySnapshotPath: legacySnapshotPath,
		replayWindow:       replayWindow,
	}
}

func CopySeenRequests(source map[string]SeenRequest) map[string]SeenRequest {
	copied := make(map[string]SeenRequest, len(source))
	for key, seen := range source {
		copied[key] = seen
	}
	return copied
}

type persistedNonce struct {
	ControlSessionID string `json:"control_session_id"`
	SeenAt           string `json:"seen_at"`
}

type nonceReplayFile struct {
	Nonces map[string]persistedNonce `json:"nonces"`
}

type snapshotNonceReplayStore struct {
	path         string
	replayWindow time.Duration
}

func (store snapshotNonceReplayStore) Load(nowUTC time.Time) (map[string]SeenRequest, error) {
	rawBytes, err := os.ReadFile(store.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]SeenRequest{}, nil
		}
		return nil, fmt.Errorf("read nonce replay state: %w", err)
	}
	var stateFile nonceReplayFile
	if err := json.Unmarshal(rawBytes, &stateFile); err != nil {
		return nil, fmt.Errorf("decode nonce replay state: %w", err)
	}
	loadedNonces := make(map[string]SeenRequest, len(stateFile.Nonces))
	for nonceKey, entry := range stateFile.Nonces {
		seenAt, parseErr := time.Parse(time.RFC3339Nano, entry.SeenAt)
		if parseErr != nil {
			continue
		}
		if store.entryExpired(nowUTC, seenAt) {
			continue
		}
		loadedNonces[nonceKey] = SeenRequest{
			ControlSessionID: entry.ControlSessionID,
			SeenAt:           seenAt,
		}
	}
	return loadedNonces, nil
}

func (store snapshotNonceReplayStore) Record(nonceKey string, seenNonce SeenRequest) error {
	seenAuthNonces, err := store.Load(seenNonce.SeenAt.UTC())
	if err != nil {
		return err
	}
	seenAuthNonces[nonceKey] = seenNonce
	return store.Compact(seenAuthNonces)
}

func (store snapshotNonceReplayStore) Compact(seenAuthNonces map[string]SeenRequest) error {
	stateFile := nonceReplaySnapshot(seenAuthNonces)
	jsonBytes, err := json.Marshal(stateFile)
	if err != nil {
		return fmt.Errorf("marshal nonce replay state: %w", err)
	}
	if err := atomicWritePrivateJSON(store.path, jsonBytes); err != nil {
		return fmt.Errorf("persist nonce replay state: %w", err)
	}
	return nil
}

func (store snapshotNonceReplayStore) entryExpired(nowUTC time.Time, seenAt time.Time) bool {
	return store.replayWindow > 0 && nowUTC.Sub(seenAt) > store.replayWindow
}

type nonceReplayLogRecord struct {
	NonceKey         string `json:"nonce_key"`
	ControlSessionID string `json:"control_session_id"`
	SeenAt           string `json:"seen_at"`
}

type appendOnlyNonceReplayStore struct {
	path               string
	legacySnapshotPath string
	replayWindow       time.Duration
}

func (store appendOnlyNonceReplayStore) Load(nowUTC time.Time) (map[string]SeenRequest, error) {
	if _, err := os.Stat(store.path); err == nil {
		return loadAppendOnlyNonceReplayLog(store.path, nowUTC, store.replayWindow)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat nonce replay log: %w", err)
	}
	if strings.TrimSpace(store.legacySnapshotPath) != "" {
		return snapshotNonceReplayStore{path: store.legacySnapshotPath, replayWindow: store.replayWindow}.Load(nowUTC)
	}
	return map[string]SeenRequest{}, nil
}

func (store appendOnlyNonceReplayStore) Record(nonceKey string, seenNonce SeenRequest) error {
	record := nonceReplayLogRecord{
		NonceKey:         nonceKey,
		ControlSessionID: seenNonce.ControlSessionID,
		SeenAt:           seenNonce.SeenAt.UTC().Format(time.RFC3339Nano),
	}
	recordBytes, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal nonce replay log record: %w", err)
	}
	recordBytes = append(recordBytes, '\n')
	return appendPrivateStateLine(store.path, recordBytes)
}

func (store appendOnlyNonceReplayStore) Compact(seenAuthNonces map[string]SeenRequest) error {
	nonceKeys := make([]string, 0, len(seenAuthNonces))
	for nonceKey := range seenAuthNonces {
		nonceKeys = append(nonceKeys, nonceKey)
	}
	slices.Sort(nonceKeys)

	logBytes := make([]byte, 0, len(nonceKeys)*96)
	for _, nonceKey := range nonceKeys {
		seenNonce := seenAuthNonces[nonceKey]
		recordBytes, err := json.Marshal(nonceReplayLogRecord{
			NonceKey:         nonceKey,
			ControlSessionID: seenNonce.ControlSessionID,
			SeenAt:           seenNonce.SeenAt.UTC().Format(time.RFC3339Nano),
		})
		if err != nil {
			return fmt.Errorf("marshal compacted nonce replay log record: %w", err)
		}
		logBytes = append(logBytes, recordBytes...)
		logBytes = append(logBytes, '\n')
	}
	if err := atomicWritePrivateStateFile(store.path, logBytes); err != nil {
		return fmt.Errorf("persist compacted nonce replay log: %w", err)
	}
	return nil
}

func nonceReplaySnapshot(seenAuthNonces map[string]SeenRequest) nonceReplayFile {
	entries := make(map[string]persistedNonce, len(seenAuthNonces))
	for nonceKey, seen := range seenAuthNonces {
		entries[nonceKey] = persistedNonce{
			ControlSessionID: seen.ControlSessionID,
			SeenAt:           seen.SeenAt.UTC().Format(time.RFC3339Nano),
		}
	}
	return nonceReplayFile{Nonces: entries}
}

func loadAppendOnlyNonceReplayLog(path string, nowUTC time.Time, replayWindow time.Duration) (map[string]SeenRequest, error) {
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]SeenRequest{}, nil
		}
		return nil, fmt.Errorf("read nonce replay log: %w", err)
	}
	if len(rawBytes) == 0 {
		return map[string]SeenRequest{}, nil
	}

	lines := bytes.Split(rawBytes, []byte{'\n'})
	hasTrailingNewline := len(rawBytes) > 0 && rawBytes[len(rawBytes)-1] == '\n'
	loadedNonces := make(map[string]SeenRequest)
	for lineIndex, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		isLastLine := lineIndex == len(lines)-1
		var record nonceReplayLogRecord
		if err := json.Unmarshal(line, &record); err != nil {
			if isLastLine && !hasTrailingNewline {
				continue
			}
			return nil, fmt.Errorf("decode nonce replay log line %d: %w", lineIndex+1, err)
		}
		seenAt, err := time.Parse(time.RFC3339Nano, record.SeenAt)
		if err != nil {
			if isLastLine && !hasTrailingNewline {
				continue
			}
			return nil, fmt.Errorf("parse nonce replay log timestamp on line %d: %w", lineIndex+1, err)
		}
		if replayWindow > 0 && nowUTC.Sub(seenAt) > replayWindow {
			continue
		}
		loadedNonces[record.NonceKey] = SeenRequest{
			ControlSessionID: record.ControlSessionID,
			SeenAt:           seenAt.UTC(),
		}
	}
	return loadedNonces, nil
}

func AtomicWritePrivateStateFile(path string, fileBytes []byte) error {
	return atomicWritePrivateStateFile(path, fileBytes)
}

func AtomicWritePrivateJSON(path string, jsonBytes []byte) error {
	return atomicWritePrivateStateFile(path, jsonBytes)
}

func atomicWritePrivateJSON(path string, jsonBytes []byte) error {
	return atomicWritePrivateStateFile(path, jsonBytes)
}

func atomicWritePrivateStateFile(path string, fileBytes []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	tempFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create state temp file: %w", err)
	}
	tempPath := tempFile.Name()
	cleanupTemp := func() {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
	}
	if err := tempFile.Chmod(0o600); err != nil {
		cleanupTemp()
		return fmt.Errorf("chmod state temp file: %w", err)
	}
	if _, err := tempFile.Write(fileBytes); err != nil {
		cleanupTemp()
		return fmt.Errorf("write state temp file: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		cleanupTemp()
		return fmt.Errorf("sync state temp file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("close state temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("commit state file: %w", err)
	}
	if stateDir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = stateDir.Sync()
		_ = stateDir.Close()
	}
	return nil
}

func appendPrivateStateLine(path string, line []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	createdFile := false
	if _, err := os.Stat(path); os.IsNotExist(err) {
		createdFile = true
	} else if err != nil {
		return fmt.Errorf("stat state file: %w", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open state file for append: %w", err)
	}
	if _, err := file.Write(line); err != nil {
		_ = file.Close()
		return fmt.Errorf("append state file: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync state file append: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close state file append: %w", err)
	}
	if createdFile {
		if stateDir, err := os.Open(filepath.Dir(path)); err == nil {
			_ = stateDir.Sync()
			_ = stateDir.Close()
		}
	}
	return nil
}
