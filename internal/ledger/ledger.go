package ledger

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"
)

// SchemaVersion is the current ledger event schema version.
// Increment this when making breaking changes to the Event structure.
const SchemaVersion = 1

var ErrLedgerIntegrity = errors.New("ledger integrity anomaly")

type cachedChainState struct {
	lastSequence int64
	lastHash     string
	fileState    ledgerFileState
}

type cachedChainStateKey struct {
	normalizedPath string
	sequenceField  string
}

var appendChainStateCache = struct {
	mu     sync.Mutex
	states map[cachedChainStateKey]cachedChainState
}{
	states: make(map[cachedChainStateKey]cachedChainState),
}

// Keep ledger fsync as an injectable seam so durability failures can be tested
// deterministically without depending on flaky OS behavior.
var syncLedgerFileHandle = func(fileHandle *os.File) error {
	return fileHandle.Sync()
}

// Event represents a ledger entry.
// The ledger is append-only and serves as the canonical source of truth.
type Event struct {
	// V is the schema version. Required for forward compatibility.
	V int `json:"v"`

	// TS is the timestamp in RFC3339Nano format.
	TS string `json:"ts"`

	// Type identifies the event category (e.g., "tool.success", "session.started").
	Type string `json:"type"`

	// Session is the session ID that generated this event.
	Session string `json:"session"`

	// Data contains event-specific payload. May be nil for simple events.
	Data map[string]interface{} `json:"data,omitempty"`
}

// NewEvent creates a new event with the current schema version.
func NewEvent(ts, eventType, session string, data map[string]interface{}) Event {
	return Event{
		V:       SchemaVersion,
		TS:      ts,
		Type:    eventType,
		Session: session,
		Data:    data,
	}
}

// Append writes a single JSONL event to the ledger file.
// Uses O_APPEND for atomic writes on POSIX systems.
func Append(path string, e Event) error {
	// Ensure schema version is set
	if e.V == 0 {
		e.V = SchemaVersion
	}
	if e.Data == nil {
		e.Data = map[string]interface{}{}
	}

	normalizedPath := normalizeLedgerPath(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		return fmt.Errorf("lock ledger: %w", err)
	}
	defer func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
	}()

	currentFileInfo, err := f.Stat()
	if err != nil {
		clearCachedChainState(normalizedPath, "ledger_sequence")
		return fmt.Errorf("stat ledger file: %w", err)
	}
	currentFileState, err := ledgerFileStateFromFileInfo(currentFileInfo)
	if err != nil {
		clearCachedChainState(normalizedPath, "ledger_sequence")
		return fmt.Errorf("load ledger file state: %w", err)
	}

	chainState, found := loadCachedChainState(normalizedPath, "ledger_sequence", currentFileState)
	if !found {
		lastSequence, lastHash, err := ReadVerifiedChainState(f, "ledger_sequence")
		if err != nil {
			clearCachedChainState(normalizedPath, "ledger_sequence")
			return err
		}
		chainState = cachedChainState{
			lastSequence: lastSequence,
			lastHash:     lastHash,
			fileState:    currentFileState,
		}
	}

	e.Data["ledger_sequence"] = chainState.lastSequence + 1
	e.Data["previous_event_hash"] = chainState.lastHash
	eventHash, err := hashEvent(e)
	if err != nil {
		clearCachedChainState(normalizedPath, "ledger_sequence")
		return fmt.Errorf("hash ledger event: %w", err)
	}
	e.Data["event_hash"] = eventHash

	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		clearCachedChainState(normalizedPath, "ledger_sequence")
		return fmt.Errorf("seek ledger end: %w", err)
	}
	enc := json.NewEncoder(f)
	if err := enc.Encode(e); err != nil {
		clearCachedChainState(normalizedPath, "ledger_sequence")
		return err
	}
	if err := syncLedgerFileHandle(f); err != nil {
		clearCachedChainState(normalizedPath, "ledger_sequence")
		return fmt.Errorf("sync ledger: %w", err)
	}

	updatedFileInfo, err := f.Stat()
	if err != nil {
		clearCachedChainState(normalizedPath, "ledger_sequence")
		return nil
	}
	updatedFileState, err := ledgerFileStateFromFileInfo(updatedFileInfo)
	if err != nil {
		clearCachedChainState(normalizedPath, "ledger_sequence")
		return nil
	}
	storeCachedChainState(normalizedPath, "ledger_sequence", cachedChainState{
		lastSequence: chainState.lastSequence + 1,
		lastHash:     eventHash,
		fileState:    updatedFileState,
	})
	return nil
}

// ParseEvent attempts to parse a JSON line as an Event.
// Returns the event and a boolean indicating if parsing succeeded.
// Tolerates missing version field for backwards compatibility.
func ParseEvent(line []byte) (Event, bool) {
	var evt Event
	if err := json.Unmarshal(line, &evt); err != nil {
		return Event{}, false
	}

	// Backfill version for old events
	if evt.V == 0 {
		evt.V = 1
	}

	return evt, true
}

func ReadVerifiedChainState(fileHandle *os.File, sequenceField string) (int64, string, error) {
	return ReadVerifiedChainStateFromBase(fileHandle, sequenceField, 0, "")
}

func chainSequenceValue(event Event, sequenceField string) (int64, error) {
	rawSequence, found := event.Data[sequenceField]
	if !found {
		// Legacy audit events written before hook pre-validation used the shared
		// ledger append path directly, which populated ledger_sequence but omitted
		// audit_sequence. Accept only that exact compatibility case so startup can
		// resume from the append-only audit log without weakening other chain checks.
		if sequenceField == "audit_sequence" {
			rawLedgerSequence, hasLedgerSequence := event.Data["ledger_sequence"]
			if hasLedgerSequence {
				return sequenceValueFromRaw(rawLedgerSequence, sequenceField)
			}
		}
		return 0, fmt.Errorf("%w: missing %s", ErrLedgerIntegrity, sequenceField)
	}
	return sequenceValueFromRaw(rawSequence, sequenceField)
}

func sequenceValueFromRaw(rawSequence interface{}, sequenceField string) (int64, error) {
	switch typedSequence := rawSequence.(type) {
	case float64:
		return int64(typedSequence), nil
	case int64:
		return typedSequence, nil
	case int:
		return int64(typedSequence), nil
	default:
		return 0, fmt.Errorf("%w: invalid %s type", ErrLedgerIntegrity, sequenceField)
	}
}

func hashStoredChainEvent(event Event) (string, error) {
	canonicalEvent := event
	if event.Data != nil {
		canonicalData := make(map[string]interface{}, len(event.Data))
		for key, value := range event.Data {
			if key == "event_hash" {
				continue
			}
			canonicalData[key] = value
		}
		canonicalEvent.Data = canonicalData
	}
	return hashEvent(canonicalEvent)
}

// hashEvent returns hex(SHA-256(canonical JSON of event)) with event_hash stripped from Data.
// The chain detects reordering/corruption and links each line to the previous hash; it is not
// a secret-keyed MAC, so a filesystem writer can replace the entire file with a new consistent
// chain. See docs/setup/LEDGER_AND_AUDIT_INTEGRITY.md for operator-facing semantics.
func hashEvent(event Event) (string, error) {
	canonicalEvent := event
	if event.Data != nil {
		canonicalData := make(map[string]interface{}, len(event.Data))
		for key, value := range event.Data {
			if key == "event_hash" {
				continue
			}
			canonicalData[key] = value
		}
		canonicalEvent.Data = canonicalData
	}
	payloadBytes, err := json.Marshal(canonicalEvent)
	if err != nil {
		return "", err
	}
	payloadHash := sha256.Sum256(payloadBytes)
	return fmt.Sprintf("%x", payloadHash[:]), nil
}

func cacheVerifiedChainStateFromFileHandle(fileHandle *os.File, sequenceField string, lastSequence int64, lastHash string) {
	if fileHandle == nil {
		return
	}
	fileInfo, err := fileHandle.Stat()
	if err != nil {
		return
	}
	fileState, err := ledgerFileStateFromFileInfo(fileInfo)
	if err != nil {
		return
	}
	storeCachedChainState(normalizeLedgerPath(fileHandle.Name()), sequenceField, cachedChainState{
		lastSequence: lastSequence,
		lastHash:     lastHash,
		fileState:    fileState,
	})
}

func normalizeLedgerPath(path string) string {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(absolutePath)
}

func loadCachedChainState(normalizedPath string, sequenceField string, fileState ledgerFileState) (cachedChainState, bool) {
	appendChainStateCache.mu.Lock()
	defer appendChainStateCache.mu.Unlock()

	cacheKey := cachedChainStateKey{
		normalizedPath: normalizedPath,
		sequenceField:  sequenceField,
	}
	cachedState, found := appendChainStateCache.states[cacheKey]
	if !found {
		return cachedChainState{}, false
	}
	if cachedState.fileState != fileState {
		delete(appendChainStateCache.states, cacheKey)
		return cachedChainState{}, false
	}
	return cachedState, true
}

func storeCachedChainState(normalizedPath string, sequenceField string, chainState cachedChainState) {
	appendChainStateCache.mu.Lock()
	defer appendChainStateCache.mu.Unlock()

	cacheKey := cachedChainStateKey{
		normalizedPath: normalizedPath,
		sequenceField:  sequenceField,
	}
	appendChainStateCache.states[cacheKey] = chainState
}

func clearCachedChainState(normalizedPath string, sequenceField string) {
	appendChainStateCache.mu.Lock()
	defer appendChainStateCache.mu.Unlock()

	cacheKey := cachedChainStateKey{
		normalizedPath: normalizedPath,
		sequenceField:  sequenceField,
	}
	delete(appendChainStateCache.states, cacheKey)
}

func PrimeAppendChainState(path string, fileHandle *os.File, lastSequence int64, lastHash string) error {
	if fileHandle == nil {
		return fmt.Errorf("file handle is required")
	}
	fileInfo, err := fileHandle.Stat()
	if err != nil {
		return fmt.Errorf("stat ledger file: %w", err)
	}
	fileState, err := ledgerFileStateFromFileInfo(fileInfo)
	if err != nil {
		return fmt.Errorf("load ledger file state: %w", err)
	}
	storeCachedChainState(normalizeLedgerPath(path), "ledger_sequence", cachedChainState{
		lastSequence: lastSequence,
		lastHash:     lastHash,
		fileState:    fileState,
	})
	return nil
}

func useLedgerFileSyncForTest(syncOverride func(fileHandle *os.File) error) func() {
	previousSync := syncLedgerFileHandle
	syncLedgerFileHandle = syncOverride
	return func() {
		syncLedgerFileHandle = previousSync
	}
}
