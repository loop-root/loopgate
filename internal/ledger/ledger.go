package ledger

import (
	"bufio"
	"bytes"
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

type appendChainStateCache struct {
	mu     sync.Mutex
	states map[cachedChainStateKey]cachedChainState
}

// AppendRuntime owns append-chain cache state for a family of ledger reads and
// appends. It is safe for concurrent use.
type AppendRuntime struct {
	chainStateCache    appendChainStateCache
	syncLedgerFileFunc func(fileHandle *os.File) error
}

func NewAppendRuntime() *AppendRuntime {
	return &AppendRuntime{
		syncLedgerFileFunc: func(fileHandle *os.File) error {
			return fileHandle.Sync()
		},
	}
}

var defaultAppendRuntime = NewAppendRuntime()

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
// It serializes concurrent appenders with LOCK_EX, verifies the chain state,
// seeks to the current end, and then writes the next canonical JSONL record.
func Append(path string, e Event) error {
	return defaultAppendRuntime.Append(path, e)
}

// Append writes a single JSONL event to the ledger file using this runtime's
// append-chain cache ownership. It serializes concurrent appenders with LOCK_EX,
// verifies the chain state, seeks to the current end, and then writes the next
// canonical JSONL record.
func (runtime *AppendRuntime) Append(path string, e Event) error {
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
		runtime.clearCachedChainState(normalizedPath, "ledger_sequence")
		return fmt.Errorf("stat ledger file: %w", err)
	}
	currentFileState, err := ledgerFileStateFromFileInfo(currentFileInfo)
	if err != nil {
		runtime.clearCachedChainState(normalizedPath, "ledger_sequence")
		return fmt.Errorf("load ledger file state: %w", err)
	}

	chainState, found := runtime.loadCachedChainState(normalizedPath, "ledger_sequence", currentFileState)
	if !found {
		lastSequence, lastHash, err := runtime.ReadVerifiedChainState(f, "ledger_sequence")
		if err != nil {
			runtime.clearCachedChainState(normalizedPath, "ledger_sequence")
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
		runtime.clearCachedChainState(normalizedPath, "ledger_sequence")
		return fmt.Errorf("hash ledger event: %w", err)
	}
	e.Data["event_hash"] = eventHash
	e, err = canonicalizeEvent(e, false)
	if err != nil {
		runtime.clearCachedChainState(normalizedPath, "ledger_sequence")
		return fmt.Errorf("canonicalize ledger event: %w", err)
	}

	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		runtime.clearCachedChainState(normalizedPath, "ledger_sequence")
		return fmt.Errorf("seek ledger end: %w", err)
	}
	enc := json.NewEncoder(f)
	if err := enc.Encode(e); err != nil {
		runtime.clearCachedChainState(normalizedPath, "ledger_sequence")
		return err
	}
	if err := runtime.syncLedgerFileHandle(f); err != nil {
		runtime.clearCachedChainState(normalizedPath, "ledger_sequence")
		return fmt.Errorf("sync ledger: %w", err)
	}

	updatedFileInfo, err := f.Stat()
	if err != nil {
		runtime.clearCachedChainState(normalizedPath, "ledger_sequence")
		return nil
	}
	updatedFileState, err := ledgerFileStateFromFileInfo(updatedFileInfo)
	if err != nil {
		runtime.clearCachedChainState(normalizedPath, "ledger_sequence")
		return nil
	}
	runtime.storeCachedChainState(normalizedPath, "ledger_sequence", cachedChainState{
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
	return defaultAppendRuntime.ReadVerifiedChainState(fileHandle, sequenceField)
}

func (runtime *AppendRuntime) ReadVerifiedChainState(fileHandle *os.File, sequenceField string) (int64, string, error) {
	return runtime.readVerifiedChainStateFromBase(fileHandle, sequenceField, 0, "")
}

func ReadVerifiedChainStateFromBase(fileHandle *os.File, sequenceField string, baseSequence int64, baseEventHash string) (int64, string, error) {
	return defaultAppendRuntime.readVerifiedChainStateFromBase(fileHandle, sequenceField, baseSequence, baseEventHash)
}

func (runtime *AppendRuntime) readVerifiedChainStateFromBase(fileHandle *os.File, sequenceField string, baseSequence int64, baseEventHash string) (int64, string, error) {
	if _, err := fileHandle.Seek(0, io.SeekStart); err != nil {
		return 0, "", fmt.Errorf("seek ledger start: %w", err)
	}

	scanner := bufio.NewScanner(fileHandle)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	foundEvent := false
	expectedSequence := baseSequence + 1
	previousEventHash := baseEventHash
	for scanner.Scan() {
		parsedEvent, ok := ParseEvent(scanner.Bytes())
		if !ok {
			return 0, "", fmt.Errorf("%w: malformed ledger line", ErrLedgerIntegrity)
		}

		sequenceValue, err := chainSequenceValue(parsedEvent, sequenceField)
		if err != nil {
			return 0, "", err
		}
		if sequenceValue != expectedSequence {
			return 0, "", fmt.Errorf("%w: unexpected %s %d (expected %d)", ErrLedgerIntegrity, sequenceField, sequenceValue, expectedSequence)
		}

		storedPreviousHash, ok := parsedEvent.Data["previous_event_hash"].(string)
		if !ok {
			return 0, "", fmt.Errorf("%w: missing previous_event_hash", ErrLedgerIntegrity)
		}
		if storedPreviousHash != previousEventHash {
			return 0, "", fmt.Errorf("%w: previous_event_hash mismatch", ErrLedgerIntegrity)
		}

		storedEventHash, ok := parsedEvent.Data["event_hash"].(string)
		if !ok || storedEventHash == "" {
			return 0, "", fmt.Errorf("%w: missing event_hash", ErrLedgerIntegrity)
		}
		expectedEventHash, err := hashStoredChainEvent(parsedEvent)
		if err != nil {
			return 0, "", fmt.Errorf("hash ledger event: %w", err)
		}
		if storedEventHash != expectedEventHash {
			return 0, "", fmt.Errorf("%w: event_hash mismatch", ErrLedgerIntegrity)
		}

		previousEventHash = storedEventHash
		expectedSequence++
		foundEvent = true
	}
	if err := scanner.Err(); err != nil {
		return 0, "", fmt.Errorf("scan ledger: %w", err)
	}
	if !foundEvent {
		runtime.cacheVerifiedChainStateFromFileHandle(fileHandle, sequenceField, baseSequence, baseEventHash)
		return baseSequence, baseEventHash, nil
	}

	lastSequence := expectedSequence - 1
	runtime.cacheVerifiedChainStateFromFileHandle(fileHandle, sequenceField, lastSequence, previousEventHash)
	return lastSequence, previousEventHash, nil
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
	return hashEvent(event)
}

// hashEvent returns hex(SHA-256(canonical JSON of event)) with event_hash stripped from Data.
// The chain detects reordering/corruption and links each line to the previous hash; it is not
// a secret-keyed MAC, so a filesystem writer can replace the entire file with a new consistent
// chain. See docs/setup/LEDGER_AND_AUDIT_INTEGRITY.md for operator-facing semantics.
func hashEvent(event Event) (string, error) {
	payloadBytes, err := marshalCanonicalEventJSON(event, true)
	if err != nil {
		return "", err
	}
	payloadHash := sha256.Sum256(payloadBytes)
	return fmt.Sprintf("%x", payloadHash[:]), nil
}

// hashCanonicalEvent hashes an event whose Data values have already been
// normalized by canonicalizeEvent. It strips event_hash without re-walking the
// full payload, which keeps append preparation from canonicalizing the same
// audit data twice.
func hashCanonicalEvent(event Event) (string, error) {
	canonicalEvent := event
	if canonicalEvent.Data != nil {
		if _, hasEventHash := canonicalEvent.Data["event_hash"]; hasEventHash {
			canonicalData := make(map[string]interface{}, len(canonicalEvent.Data)-1)
			for key, value := range canonicalEvent.Data {
				if key == "event_hash" {
					continue
				}
				canonicalData[key] = value
			}
			canonicalEvent.Data = canonicalData
		}
	}
	payloadBytes, err := json.Marshal(canonicalEvent)
	if err != nil {
		return "", err
	}
	payloadHash := sha256.Sum256(payloadBytes)
	return fmt.Sprintf("%x", payloadHash[:]), nil
}

// ComputeEventHash returns the append-chain hash for event after normalizing it
// into a JSON-compatible representation. The canonical bytes rely on
// encoding/json's deterministic ordering of string-keyed map objects.
func ComputeEventHash(event Event) (string, error) {
	return hashEvent(event)
}

func marshalCanonicalEventJSON(event Event, stripEventHash bool) ([]byte, error) {
	canonicalEvent, err := canonicalizeEvent(event, stripEventHash)
	if err != nil {
		return nil, err
	}
	return json.Marshal(canonicalEvent)
}

func canonicalizeEvent(event Event, stripEventHash bool) (Event, error) {
	canonicalEvent := Event{
		V:       event.V,
		TS:      event.TS,
		Type:    event.Type,
		Session: event.Session,
	}
	if canonicalEvent.V == 0 {
		canonicalEvent.V = SchemaVersion
	}
	if event.Data == nil {
		return canonicalEvent, nil
	}
	canonicalData, err := canonicalizeDataMap(event.Data, stripEventHash)
	if err != nil {
		return Event{}, err
	}
	canonicalEvent.Data = canonicalData
	return canonicalEvent, nil
}

func canonicalizeDataMap(rawData map[string]interface{}, stripEventHash bool) (map[string]interface{}, error) {
	if rawData == nil {
		return nil, nil
	}
	canonicalData := make(map[string]interface{}, len(rawData))
	for key, value := range rawData {
		if stripEventHash && key == "event_hash" {
			continue
		}
		canonicalValue, err := canonicalizeValue(value)
		if err != nil {
			return nil, fmt.Errorf("canonicalize data field %q: %w", key, err)
		}
		canonicalData[key] = canonicalValue
	}
	return canonicalData, nil
}

func canonicalizeValue(rawValue interface{}) (interface{}, error) {
	switch typedValue := rawValue.(type) {
	case nil,
		string,
		bool,
		json.Number,
		float64,
		float32,
		int,
		int8,
		int16,
		int32,
		int64,
		uint,
		uint8,
		uint16,
		uint32,
		uint64:
		return typedValue, nil
	case json.RawMessage:
		return append(json.RawMessage(nil), typedValue...), nil
	case []byte:
		return append([]byte(nil), typedValue...), nil
	case map[string]interface{}:
		return canonicalizeDataMap(typedValue, false)
	case map[string]string:
		canonicalMap := make(map[string]interface{}, len(typedValue))
		for key, value := range typedValue {
			canonicalMap[key] = value
		}
		return canonicalMap, nil
	case []interface{}:
		canonicalSlice := make([]interface{}, 0, len(typedValue))
		for index, nestedValue := range typedValue {
			canonicalValue, err := canonicalizeValue(nestedValue)
			if err != nil {
				return nil, fmt.Errorf("canonicalize slice index %d: %w", index, err)
			}
			canonicalSlice = append(canonicalSlice, canonicalValue)
		}
		return canonicalSlice, nil
	case []string:
		return append([]string(nil), typedValue...), nil
	default:
		payloadBytes, err := json.Marshal(typedValue)
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(bytes.NewReader(payloadBytes))
		decoder.UseNumber()
		var canonicalValue interface{}
		if err := decoder.Decode(&canonicalValue); err != nil {
			return nil, err
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			if err == nil {
				return nil, fmt.Errorf("unexpected trailing JSON value")
			}
			return nil, err
		}
		return canonicalValue, nil
	}
}

func (runtime *AppendRuntime) cacheVerifiedChainStateFromFileHandle(fileHandle *os.File, sequenceField string, lastSequence int64, lastHash string) {
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
	runtime.storeCachedChainState(normalizeLedgerPath(fileHandle.Name()), sequenceField, cachedChainState{
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
	return defaultAppendRuntime.loadCachedChainState(normalizedPath, sequenceField, fileState)
}

func (runtime *AppendRuntime) loadCachedChainState(normalizedPath string, sequenceField string, fileState ledgerFileState) (cachedChainState, bool) {
	runtime.chainStateCache.mu.Lock()
	defer runtime.chainStateCache.mu.Unlock()

	if runtime.chainStateCache.states == nil {
		return cachedChainState{}, false
	}
	cacheKey := cachedChainStateKey{
		normalizedPath: normalizedPath,
		sequenceField:  sequenceField,
	}
	cachedState, found := runtime.chainStateCache.states[cacheKey]
	if !found {
		return cachedChainState{}, false
	}
	if cachedState.fileState != fileState {
		delete(runtime.chainStateCache.states, cacheKey)
		return cachedChainState{}, false
	}
	return cachedState, true
}

func (runtime *AppendRuntime) storeCachedChainState(normalizedPath string, sequenceField string, chainState cachedChainState) {
	runtime.chainStateCache.mu.Lock()
	defer runtime.chainStateCache.mu.Unlock()

	if runtime.chainStateCache.states == nil {
		runtime.chainStateCache.states = make(map[cachedChainStateKey]cachedChainState)
	}
	cacheKey := cachedChainStateKey{
		normalizedPath: normalizedPath,
		sequenceField:  sequenceField,
	}
	runtime.chainStateCache.states[cacheKey] = chainState
}

func clearCachedChainState(normalizedPath string, sequenceField string) {
	defaultAppendRuntime.clearCachedChainState(normalizedPath, sequenceField)
}

func (runtime *AppendRuntime) clearCachedChainState(normalizedPath string, sequenceField string) {
	runtime.chainStateCache.mu.Lock()
	defer runtime.chainStateCache.mu.Unlock()

	if runtime.chainStateCache.states == nil {
		return
	}
	cacheKey := cachedChainStateKey{
		normalizedPath: normalizedPath,
		sequenceField:  sequenceField,
	}
	delete(runtime.chainStateCache.states, cacheKey)
}

func PrimeAppendChainState(path string, fileHandle *os.File, lastSequence int64, lastHash string) error {
	return defaultAppendRuntime.PrimeAppendChainState(path, fileHandle, lastSequence, lastHash)
}

func (runtime *AppendRuntime) PrimeAppendChainState(path string, fileHandle *os.File, lastSequence int64, lastHash string) error {
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
	runtime.storeCachedChainState(normalizeLedgerPath(path), "ledger_sequence", cachedChainState{
		lastSequence: lastSequence,
		lastHash:     lastHash,
		fileState:    fileState,
	})
	return nil
}

func (runtime *AppendRuntime) syncLedgerFileHandle(fileHandle *os.File) error {
	if runtime == nil || runtime.syncLedgerFileFunc == nil {
		return fileHandle.Sync()
	}
	return runtime.syncLedgerFileFunc(fileHandle)
}

func useLedgerFileSyncForTest(syncOverride func(fileHandle *os.File) error) func() {
	previousSync := defaultAppendRuntime.syncLedgerFileFunc
	defaultAppendRuntime.syncLedgerFileFunc = syncOverride
	return func() {
		defaultAppendRuntime.syncLedgerFileFunc = previousSync
	}
}
