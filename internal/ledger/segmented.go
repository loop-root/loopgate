package ledger

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	segmentManifestVersion = 1
	segmentFilePrefix      = "segment-"
	segmentFileSuffix      = ".jsonl"
)

type RotationSettings struct {
	MaxEventBytes                 int
	RotateAtBytes                 int64
	SegmentDir                    string
	ManifestPath                  string
	VerifyClosedSegmentsOnStartup bool
}

type SegmentManifestEntry struct {
	V                    int    `json:"v"`
	ClosedAt             string `json:"closed_at"`
	SegmentFilename      string `json:"segment_filename"`
	FirstSequence        int64  `json:"first_sequence"`
	LastSequence         int64  `json:"last_sequence"`
	EventCount           int64  `json:"event_count"`
	FileSizeBytes        int64  `json:"file_size_bytes"`
	FileSHA256           string `json:"file_sha256"`
	LastEventHash        string `json:"last_event_hash"`
	ManifestSequence     int64  `json:"manifest_sequence"`
	PreviousManifestHash string `json:"previous_manifest_hash"`
	ManifestHash         string `json:"manifest_hash"`
}

type manifestState struct {
	lastManifestSequence int64
	lastManifestHash     string
	lastSequence         int64
	lastEventHash        string
	entries              []SegmentManifestEntry
}

func AppendWithRotation(path string, ledgerEvent Event, rotationSettings RotationSettings) error {
	return defaultAppendRuntime.AppendWithRotation(path, ledgerEvent, rotationSettings)
}

func (runtime *AppendRuntime) AppendWithRotation(path string, ledgerEvent Event, rotationSettings RotationSettings) error {
	if err := validateRotationSettings(rotationSettings); err != nil {
		return err
	}

	if rotationSettings.RotateAtBytes <= 0 {
		return appendWithMaxEventBytes(runtime, path, ledgerEvent, rotationSettings.MaxEventBytes)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create active ledger dir: %w", err)
	}
	if err := os.MkdirAll(rotationSettings.SegmentDir, 0o700); err != nil {
		return fmt.Errorf("create segment dir: %w", err)
	}

	lockHandle, err := openLedgerLock(path)
	if err != nil {
		return err
	}
	defer lockHandle.Close()

	verifiedManifestState, err := readVerifiedManifestState(rotationSettings.ManifestPath, rotationSettings.SegmentDir, rotationSettings.VerifyClosedSegmentsOnStartup)
	if err != nil {
		return err
	}

	activeHandle, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open active ledger: %w", err)
	}
	defer activeHandle.Close()

	activeChainState, activeLedgerSize, err := loadVerifiedActiveChainState(
		runtime,
		activeHandle,
		path,
		verifiedManifestState.lastSequence,
		verifiedManifestState.lastEventHash,
	)
	if err != nil {
		return err
	}

	_, eventLineBytes, _, err := prepareLedgerEventLine(activeChainState.lastSequence, activeChainState.lastHash, ledgerEvent)
	if err != nil {
		runtime.clearCachedChainState(normalizeLedgerPath(path), "ledger_sequence")
		return fmt.Errorf("prepare rotated ledger event: %w", err)
	}
	if err := validateEventLineSize(eventLineBytes, rotationSettings.MaxEventBytes); err != nil {
		runtime.clearCachedChainState(normalizeLedgerPath(path), "ledger_sequence")
		return err
	}

	if activeLedgerSize > 0 && activeLedgerSize+int64(len(eventLineBytes)) > rotationSettings.RotateAtBytes {
		if err := activeHandle.Close(); err != nil {
			return fmt.Errorf("close active ledger for rollover: %w", err)
		}
		if err := rolloverActiveSegment(path, rotationSettings, verifiedManifestState, activeChainState); err != nil {
			return err
		}

		verifiedManifestState, err = readVerifiedManifestState(rotationSettings.ManifestPath, rotationSettings.SegmentDir, rotationSettings.VerifyClosedSegmentsOnStartup)
		if err != nil {
			return err
		}
		activeHandle, err = os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return fmt.Errorf("open new active ledger after rollover: %w", err)
		}
		defer activeHandle.Close()
	}

	return appendPreparedEventToFile(
		runtime,
		activeHandle,
		path,
		verifiedManifestState.lastSequence,
		verifiedManifestState.lastEventHash,
		ledgerEvent,
		rotationSettings.MaxEventBytes,
	)
}

func ReadSegmentedChainState(path string, sequenceField string, rotationSettings RotationSettings) (int64, string, error) {
	return defaultAppendRuntime.ReadSegmentedChainState(path, sequenceField, rotationSettings)
}

func (runtime *AppendRuntime) ReadSegmentedChainState(path string, sequenceField string, rotationSettings RotationSettings) (int64, string, error) {
	if err := validateRotationSettings(rotationSettings); err != nil {
		return 0, "", err
	}

	baseSequence := int64(0)
	baseEventHash := ""
	if rotationSettings.RotateAtBytes > 0 {
		verifiedManifestState, err := readVerifiedManifestState(rotationSettings.ManifestPath, rotationSettings.SegmentDir, rotationSettings.VerifyClosedSegmentsOnStartup)
		if err != nil {
			return 0, "", err
		}
		baseSequence = verifiedManifestState.lastSequence
		baseEventHash = verifiedManifestState.lastEventHash
	}

	activeHandle, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			runtime.clearCachedChainState(normalizeLedgerPath(path), "ledger_sequence")
			return baseSequence, baseEventHash, nil
		}
		return 0, "", fmt.Errorf("open active ledger: %w", err)
	}
	defer activeHandle.Close()

	lastSequence, lastEventHash, err := runtime.readVerifiedChainStateFromBase(activeHandle, sequenceField, baseSequence, baseEventHash)
	if err != nil {
		return 0, "", err
	}
	if err := runtime.PrimeAppendChainState(path, activeHandle, lastSequence, lastEventHash); err != nil {
		return 0, "", err
	}
	return lastSequence, lastEventHash, nil
}

func rolloverActiveSegment(path string, rotationSettings RotationSettings, verifiedManifestState manifestState, activeChainState cachedChainState) error {
	if activeChainState.lastSequence <= verifiedManifestState.lastSequence {
		return fmt.Errorf("%w: active ledger rollover requires non-empty active segment", ErrLedgerIntegrity)
	}

	firstSequence := verifiedManifestState.lastSequence + 1
	sealedSegmentFilename := segmentFilename(firstSequence, activeChainState.lastSequence)
	sealedSegmentPath := filepath.Join(rotationSettings.SegmentDir, sealedSegmentFilename)
	if _, err := os.Stat(sealedSegmentPath); err == nil {
		return fmt.Errorf("%w: sealed segment already exists: %s", ErrLedgerIntegrity, sealedSegmentFilename)
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat sealed segment path: %w", err)
	}

	if err := os.Rename(path, sealedSegmentPath); err != nil {
		clearCachedChainState(normalizeLedgerPath(path), "ledger_sequence")
		return fmt.Errorf("rename active ledger to sealed segment: %w", err)
	}
	clearCachedChainState(normalizeLedgerPath(path), "ledger_sequence")
	if err := syncParentDirectory(path); err != nil {
		return fmt.Errorf("sync active ledger dir after rollover rename: %w", err)
	}
	if err := syncParentDirectory(sealedSegmentPath); err != nil {
		return fmt.Errorf("sync segment dir after rollover rename: %w", err)
	}

	sealedSegmentHash, sealedSegmentSize, err := hashFileSHA256(sealedSegmentPath)
	if err != nil {
		return fmt.Errorf("hash sealed segment: %w", err)
	}

	manifestEntry := SegmentManifestEntry{
		V:                    segmentManifestVersion,
		ClosedAt:             time.Now().UTC().Format(time.RFC3339Nano),
		SegmentFilename:      sealedSegmentFilename,
		FirstSequence:        firstSequence,
		LastSequence:         activeChainState.lastSequence,
		EventCount:           activeChainState.lastSequence - firstSequence + 1,
		FileSizeBytes:        sealedSegmentSize,
		FileSHA256:           sealedSegmentHash,
		LastEventHash:        activeChainState.lastHash,
		ManifestSequence:     verifiedManifestState.lastManifestSequence + 1,
		PreviousManifestHash: verifiedManifestState.lastManifestHash,
	}
	manifestHash, err := hashManifestEntry(manifestEntry)
	if err != nil {
		return fmt.Errorf("hash segment manifest entry: %w", err)
	}
	manifestEntry.ManifestHash = manifestHash

	if err := appendManifestEntry(rotationSettings.ManifestPath, manifestEntry); err != nil {
		return err
	}
	if err := syncParentDirectory(rotationSettings.ManifestPath); err != nil {
		return fmt.Errorf("sync manifest dir after rollover: %w", err)
	}
	return nil
}

func appendPreparedEventToFile(runtime *AppendRuntime, fileHandle *os.File, path string, baseSequence int64, baseEventHash string, ledgerEvent Event, maxEventBytes int) error {
	normalizedPath := normalizeLedgerPath(path)
	currentFileInfo, err := fileHandle.Stat()
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
		lastSequence, lastEventHash, err := runtime.readVerifiedChainStateFromBase(fileHandle, "ledger_sequence", baseSequence, baseEventHash)
		if err != nil {
			runtime.clearCachedChainState(normalizedPath, "ledger_sequence")
			return err
		}
		chainState = cachedChainState{
			lastSequence: lastSequence,
			lastHash:     lastEventHash,
			fileState:    currentFileState,
		}
	}

	_, eventLineBytes, eventHash, err := prepareLedgerEventLine(chainState.lastSequence, chainState.lastHash, ledgerEvent)
	if err != nil {
		runtime.clearCachedChainState(normalizedPath, "ledger_sequence")
		return fmt.Errorf("prepare ledger event: %w", err)
	}
	if err := validateEventLineSize(eventLineBytes, maxEventBytes); err != nil {
		runtime.clearCachedChainState(normalizedPath, "ledger_sequence")
		return err
	}

	if _, err := fileHandle.Seek(0, io.SeekEnd); err != nil {
		runtime.clearCachedChainState(normalizedPath, "ledger_sequence")
		return fmt.Errorf("seek ledger end: %w", err)
	}
	if _, err := fileHandle.Write(eventLineBytes); err != nil {
		runtime.clearCachedChainState(normalizedPath, "ledger_sequence")
		return fmt.Errorf("write ledger event: %w", err)
	}
	if err := runtime.syncLedgerFileHandle(fileHandle); err != nil {
		runtime.clearCachedChainState(normalizedPath, "ledger_sequence")
		return fmt.Errorf("sync ledger: %w", err)
	}

	updatedFileInfo, err := fileHandle.Stat()
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

func loadVerifiedActiveChainState(runtime *AppendRuntime, fileHandle *os.File, path string, baseSequence int64, baseEventHash string) (cachedChainState, int64, error) {
	currentFileInfo, err := fileHandle.Stat()
	if err != nil {
		return cachedChainState{}, 0, fmt.Errorf("stat active ledger: %w", err)
	}
	currentFileState, err := ledgerFileStateFromFileInfo(currentFileInfo)
	if err != nil {
		return cachedChainState{}, 0, fmt.Errorf("load active ledger file state: %w", err)
	}

	normalizedPath := normalizeLedgerPath(path)
	cachedState, found := runtime.loadCachedChainState(normalizedPath, "ledger_sequence", currentFileState)
	if found {
		return cachedState, currentFileInfo.Size(), nil
	}

	lastSequence, lastEventHash, err := runtime.readVerifiedChainStateFromBase(fileHandle, "ledger_sequence", baseSequence, baseEventHash)
	if err != nil {
		return cachedChainState{}, 0, err
	}
	return cachedChainState{
		lastSequence: lastSequence,
		lastHash:     lastEventHash,
		fileState:    currentFileState,
	}, currentFileInfo.Size(), nil
}

func readVerifiedManifestState(manifestPath string, segmentDir string, verifyClosedSegments bool) (manifestState, error) {
	fileHandle, err := os.Open(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			if err := verifyManifestReferences(segmentDir, nil, verifyClosedSegments); err != nil {
				return manifestState{}, err
			}
			return manifestState{}, nil
		}
		return manifestState{}, fmt.Errorf("open ledger segment manifest: %w", err)
	}
	defer fileHandle.Close()

	scanner := bufio.NewScanner(fileHandle)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	verifiedManifestState := manifestState{}
	expectedManifestSequence := int64(1)
	expectedFirstSequence := int64(1)
	expectedPreviousManifestHash := ""
	referencedSegments := make(map[string]SegmentManifestEntry)
	for scanner.Scan() {
		var entry SegmentManifestEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return manifestState{}, fmt.Errorf("%w: malformed ledger segment manifest line: %v", ErrLedgerIntegrity, err)
		}
		if err := validateManifestEntry(entry, expectedManifestSequence, expectedFirstSequence, expectedPreviousManifestHash); err != nil {
			return manifestState{}, err
		}
		if _, exists := referencedSegments[entry.SegmentFilename]; exists {
			return manifestState{}, fmt.Errorf("%w: duplicate segment manifest filename %q", ErrLedgerIntegrity, entry.SegmentFilename)
		}
		referencedSegments[entry.SegmentFilename] = entry
		verifiedManifestState.entries = append(verifiedManifestState.entries, entry)
		verifiedManifestState.lastManifestSequence = entry.ManifestSequence
		verifiedManifestState.lastManifestHash = entry.ManifestHash
		verifiedManifestState.lastSequence = entry.LastSequence
		verifiedManifestState.lastEventHash = entry.LastEventHash
		expectedManifestSequence++
		expectedFirstSequence = entry.LastSequence + 1
		expectedPreviousManifestHash = entry.ManifestHash
	}
	if err := scanner.Err(); err != nil {
		return manifestState{}, fmt.Errorf("scan ledger segment manifest: %w", err)
	}
	if err := verifyManifestReferences(segmentDir, referencedSegments, verifyClosedSegments); err != nil {
		return manifestState{}, err
	}
	return verifiedManifestState, nil
}

func validateManifestEntry(entry SegmentManifestEntry, expectedManifestSequence int64, expectedFirstSequence int64, expectedPreviousManifestHash string) error {
	if entry.V == 0 {
		entry.V = segmentManifestVersion
	}
	if entry.V != segmentManifestVersion {
		return fmt.Errorf("%w: unsupported segment manifest version %d", ErrLedgerIntegrity, entry.V)
	}
	if entry.ManifestSequence != expectedManifestSequence {
		return fmt.Errorf("%w: unexpected manifest_sequence %d (expected %d)", ErrLedgerIntegrity, entry.ManifestSequence, expectedManifestSequence)
	}
	if entry.PreviousManifestHash != expectedPreviousManifestHash {
		return fmt.Errorf("%w: previous_manifest_hash mismatch", ErrLedgerIntegrity)
	}
	if strings.TrimSpace(entry.ClosedAt) == "" {
		return fmt.Errorf("%w: missing closed_at", ErrLedgerIntegrity)
	}
	if filepath.Base(entry.SegmentFilename) != entry.SegmentFilename {
		return fmt.Errorf("%w: invalid segment filename %q", ErrLedgerIntegrity, entry.SegmentFilename)
	}
	if !strings.HasPrefix(entry.SegmentFilename, segmentFilePrefix) || !strings.HasSuffix(entry.SegmentFilename, segmentFileSuffix) {
		return fmt.Errorf("%w: invalid segment filename %q", ErrLedgerIntegrity, entry.SegmentFilename)
	}
	if entry.FirstSequence != expectedFirstSequence {
		return fmt.Errorf("%w: unexpected first_sequence %d (expected %d)", ErrLedgerIntegrity, entry.FirstSequence, expectedFirstSequence)
	}
	if entry.LastSequence < entry.FirstSequence {
		return fmt.Errorf("%w: invalid segment sequence range", ErrLedgerIntegrity)
	}
	expectedEventCount := entry.LastSequence - entry.FirstSequence + 1
	if entry.EventCount != expectedEventCount {
		return fmt.Errorf("%w: unexpected event_count %d (expected %d)", ErrLedgerIntegrity, entry.EventCount, expectedEventCount)
	}
	if entry.FileSizeBytes <= 0 {
		return fmt.Errorf("%w: invalid file_size_bytes", ErrLedgerIntegrity)
	}
	if strings.TrimSpace(entry.FileSHA256) == "" {
		return fmt.Errorf("%w: missing file_sha256", ErrLedgerIntegrity)
	}
	if strings.TrimSpace(entry.LastEventHash) == "" {
		return fmt.Errorf("%w: missing last_event_hash", ErrLedgerIntegrity)
	}
	expectedManifestHash, err := hashManifestEntry(entry)
	if err != nil {
		return fmt.Errorf("hash segment manifest entry: %w", err)
	}
	if entry.ManifestHash != expectedManifestHash {
		return fmt.Errorf("%w: manifest_hash mismatch", ErrLedgerIntegrity)
	}
	return nil
}

func verifyManifestReferences(segmentDir string, referencedSegments map[string]SegmentManifestEntry, verifyClosedSegments bool) error {
	segmentDirEntries, err := os.ReadDir(segmentDir)
	if err != nil {
		if os.IsNotExist(err) {
			if len(referencedSegments) == 0 {
				return nil
			}
			return fmt.Errorf("%w: segment dir is missing for sealed ledger segments", ErrLedgerIntegrity)
		}
		return fmt.Errorf("read segment dir: %w", err)
	}

	if referencedSegments == nil {
		referencedSegments = map[string]SegmentManifestEntry{}
	}
	for _, segmentDirEntry := range segmentDirEntries {
		if segmentDirEntry.IsDir() {
			continue
		}
		segmentFilename := segmentDirEntry.Name()
		if !strings.HasPrefix(segmentFilename, segmentFilePrefix) || !strings.HasSuffix(segmentFilename, segmentFileSuffix) {
			continue
		}
		if _, found := referencedSegments[segmentFilename]; !found {
			return fmt.Errorf("%w: unreferenced sealed ledger segment %q", ErrLedgerIntegrity, segmentFilename)
		}
	}
	for segmentFilename, manifestEntry := range referencedSegments {
		segmentPath := filepath.Join(segmentDir, segmentFilename)
		fileInfo, err := os.Stat(segmentPath)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("%w: referenced sealed ledger segment %q is missing", ErrLedgerIntegrity, segmentFilename)
			}
			return fmt.Errorf("stat sealed ledger segment: %w", err)
		}
		if !fileInfo.Mode().IsRegular() {
			return fmt.Errorf("%w: sealed ledger segment %q is not a regular file", ErrLedgerIntegrity, segmentFilename)
		}
		if fileInfo.Size() != manifestEntry.FileSizeBytes {
			return fmt.Errorf("%w: sealed ledger segment %q size mismatch", ErrLedgerIntegrity, segmentFilename)
		}
		if !verifyClosedSegments {
			continue
		}
		fileHash, _, err := hashFileSHA256(segmentPath)
		if err != nil {
			return fmt.Errorf("hash sealed ledger segment: %w", err)
		}
		if fileHash != manifestEntry.FileSHA256 {
			return fmt.Errorf("%w: sealed ledger segment %q hash mismatch", ErrLedgerIntegrity, segmentFilename)
		}
	}
	return nil
}

func appendManifestEntry(manifestPath string, manifestEntry SegmentManifestEntry) error {
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o700); err != nil {
		return fmt.Errorf("create manifest dir: %w", err)
	}

	manifestHandle, err := os.OpenFile(manifestPath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open segment manifest: %w", err)
	}
	defer manifestHandle.Close()

	if err := unix.Flock(int(manifestHandle.Fd()), unix.LOCK_EX); err != nil {
		return fmt.Errorf("lock segment manifest: %w", err)
	}
	defer func() {
		_ = unix.Flock(int(manifestHandle.Fd()), unix.LOCK_UN)
	}()

	manifestBytes, err := json.Marshal(manifestEntry)
	if err != nil {
		return fmt.Errorf("marshal segment manifest entry: %w", err)
	}
	manifestBytes = append(manifestBytes, '\n')
	if _, err := manifestHandle.Write(manifestBytes); err != nil {
		return fmt.Errorf("write segment manifest entry: %w", err)
	}
	if err := manifestHandle.Sync(); err != nil {
		return fmt.Errorf("sync segment manifest entry: %w", err)
	}
	return nil
}

func prepareLedgerEventLine(lastSequence int64, lastEventHash string, ledgerEvent Event) (Event, []byte, string, error) {
	preparedEvent := Event{
		V:       ledgerEvent.V,
		TS:      ledgerEvent.TS,
		Type:    ledgerEvent.Type,
		Session: ledgerEvent.Session,
	}
	if preparedEvent.V == 0 {
		preparedEvent.V = SchemaVersion
	}
	preparedEvent.Data = cloneDataMap(ledgerEvent.Data)
	if preparedEvent.Data == nil {
		preparedEvent.Data = map[string]interface{}{}
	}
	preparedEvent.Data["ledger_sequence"] = lastSequence + 1
	preparedEvent.Data["previous_event_hash"] = lastEventHash
	preparedEvent, err := canonicalizeEvent(preparedEvent, false)
	if err != nil {
		return Event{}, nil, "", err
	}
	eventHash, err := hashCanonicalEvent(preparedEvent)
	if err != nil {
		return Event{}, nil, "", err
	}
	preparedEvent.Data["event_hash"] = eventHash
	eventLineBytes, err := json.Marshal(preparedEvent)
	if err != nil {
		return Event{}, nil, "", err
	}
	eventLineBytes = append(eventLineBytes, '\n')
	return preparedEvent, eventLineBytes, eventHash, nil
}

func appendWithMaxEventBytes(runtime *AppendRuntime, path string, ledgerEvent Event, maxEventBytes int) error {
	if maxEventBytes <= 0 {
		return runtime.Append(path, ledgerEvent)
	}

	lockHandle, err := openLedgerLock(path)
	if err != nil {
		return err
	}
	defer lockHandle.Close()

	activeHandle, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open ledger: %w", err)
	}
	defer activeHandle.Close()

	return appendPreparedEventToFile(runtime, activeHandle, path, 0, "", ledgerEvent, maxEventBytes)
}

func openLedgerLock(path string) (*os.File, error) {
	lockPath := path + ".lock"
	lockHandle, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open ledger lock: %w", err)
	}
	if err := unix.Flock(int(lockHandle.Fd()), unix.LOCK_EX); err != nil {
		_ = lockHandle.Close()
		return nil, fmt.Errorf("lock ledger state: %w", err)
	}
	return lockHandle, nil
}

func validateEventLineSize(eventLineBytes []byte, maxEventBytes int) error {
	if maxEventBytes <= 0 {
		return nil
	}
	if len(eventLineBytes) > maxEventBytes {
		return fmt.Errorf("ledger event exceeds max_event_bytes (%d > %d)", len(eventLineBytes), maxEventBytes)
	}
	return nil
}

func validateRotationSettings(rotationSettings RotationSettings) error {
	if rotationSettings.MaxEventBytes < 0 {
		return fmt.Errorf("max_event_bytes must not be negative")
	}
	if rotationSettings.RotateAtBytes < 0 {
		return fmt.Errorf("rotate_at_bytes must not be negative")
	}
	if rotationSettings.RotateAtBytes == 0 {
		return nil
	}
	if strings.TrimSpace(rotationSettings.SegmentDir) == "" {
		return fmt.Errorf("segment_dir is required when rotate_at_bytes is enabled")
	}
	if strings.TrimSpace(rotationSettings.ManifestPath) == "" {
		return fmt.Errorf("manifest_path is required when rotate_at_bytes is enabled")
	}
	if normalizeLedgerPath(rotationSettings.ManifestPath) == normalizeLedgerPath(rotationSettings.SegmentDir) {
		return fmt.Errorf("manifest_path must not point to the segment directory")
	}
	return nil
}

func cloneDataMap(rawData map[string]interface{}) map[string]interface{} {
	if rawData == nil {
		return nil
	}
	clonedData := make(map[string]interface{}, len(rawData))
	for key, value := range rawData {
		clonedData[key] = value
	}
	return clonedData
}

func segmentFilename(firstSequence int64, lastSequence int64) string {
	return fmt.Sprintf("%s%020d-%020d%s", segmentFilePrefix, firstSequence, lastSequence, segmentFileSuffix)
}

func hashManifestEntry(manifestEntry SegmentManifestEntry) (string, error) {
	canonicalEntry := manifestEntry
	canonicalEntry.ManifestHash = ""
	payloadBytes, err := json.Marshal(canonicalEntry)
	if err != nil {
		return "", err
	}
	payloadHash := sha256.Sum256(payloadBytes)
	return fmt.Sprintf("%x", payloadHash[:]), nil
}

func hashFileSHA256(path string) (string, int64, error) {
	fileHandle, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("open file for hash: %w", err)
	}
	defer fileHandle.Close()

	hasher := sha256.New()
	writtenBytes, err := io.Copy(hasher, fileHandle)
	if err != nil {
		return "", 0, fmt.Errorf("hash file: %w", err)
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), writtenBytes, nil
}

func syncParentDirectory(path string) error {
	parentDir := filepath.Dir(path)
	parentHandle, err := os.Open(parentDir)
	if err != nil {
		return err
	}
	defer parentHandle.Close()
	return parentHandle.Sync()
}
