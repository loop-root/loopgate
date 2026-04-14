package loopgate

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"morph/internal/ledger"

	"golang.org/x/sys/unix"
)

type auditExportBatch struct {
	DestinationKind      string
	DestinationLabel     string
	FromAuditSequence    uint64
	ThroughAuditSequence uint64
	ThroughEventHash     string
	EventCount           int
	ApproxBytes          int
	Events               []ledger.Event
}

func (server *Server) prepareNextAuditExportBatch() (auditExportBatch, error) {
	if !server.runtimeConfig.Logging.AuditExport.Enabled {
		return auditExportBatch{}, nil
	}

	server.auditExportMu.Lock()
	defer server.auditExportMu.Unlock()

	exportState, err := server.loadAuditExportStateLocked()
	if err != nil {
		return auditExportBatch{}, fmt.Errorf("load audit export state: %w", err)
	}

	lockHandle, err := openAuditLedgerReadLock(server.auditPath)
	if err != nil {
		return auditExportBatch{}, err
	}
	defer closeAuditLedgerReadLock(lockHandle)

	rotationSettings := server.auditLedgerRotationSettings()
	if _, _, err := ledger.ReadSegmentedChainState(server.auditPath, "audit_sequence", rotationSettings); err != nil {
		return auditExportBatch{}, fmt.Errorf("verify audit ledger before export: %w", err)
	}

	manifestEntries, err := readAuditExportManifestEntries(rotationSettings.ManifestPath)
	if err != nil {
		return auditExportBatch{}, err
	}

	exportBatch := auditExportBatch{
		DestinationKind:   strings.TrimSpace(exportState.DestinationKind),
		DestinationLabel:  strings.TrimSpace(exportState.DestinationLabel),
		FromAuditSequence: exportState.LastExportedAuditSequence + 1,
		Events:            make([]ledger.Event, 0),
	}

	appendEventToBatch := func(auditEvent ledger.Event, rawLineBytes int) bool {
		auditSequence, eventHash, ok := auditExportCursorForEvent(auditEvent)
		if !ok || auditSequence <= exportState.LastExportedAuditSequence {
			return true
		}
		if exportBatch.EventCount > 0 {
			if exportBatch.EventCount >= server.runtimeConfig.Logging.AuditExport.MaxBatchEvents {
				return false
			}
			if exportBatch.ApproxBytes+rawLineBytes > server.runtimeConfig.Logging.AuditExport.MaxBatchBytes {
				return false
			}
		}
		exportBatch.Events = append(exportBatch.Events, auditEvent)
		exportBatch.EventCount++
		exportBatch.ApproxBytes += rawLineBytes
		exportBatch.ThroughAuditSequence = auditSequence
		exportBatch.ThroughEventHash = eventHash
		return true
	}

	for _, manifestEntry := range manifestEntries {
		segmentPath := filepath.Join(rotationSettings.SegmentDir, manifestEntry.SegmentFilename)
		stopped, err := scanAuditExportFile(segmentPath, appendEventToBatch)
		if err != nil {
			return auditExportBatch{}, err
		}
		if stopped {
			return exportBatch, nil
		}
	}

	stopped, err := scanAuditExportFile(server.auditPath, appendEventToBatch)
	if err != nil {
		return auditExportBatch{}, err
	}
	if stopped {
		return exportBatch, nil
	}

	return exportBatch, nil
}

func openAuditLedgerReadLock(ledgerPath string) (*os.File, error) {
	lockHandle, err := os.OpenFile(ledgerPath+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open audit ledger lock: %w", err)
	}
	if err := unix.Flock(int(lockHandle.Fd()), unix.LOCK_SH); err != nil {
		_ = lockHandle.Close()
		return nil, fmt.Errorf("lock audit ledger for export read: %w", err)
	}
	return lockHandle, nil
}

func closeAuditLedgerReadLock(lockHandle *os.File) {
	if lockHandle == nil {
		return
	}
	_ = unix.Flock(int(lockHandle.Fd()), unix.LOCK_UN)
	_ = lockHandle.Close()
}

func readAuditExportManifestEntries(manifestPath string) ([]ledger.SegmentManifestEntry, error) {
	manifestHandle, err := os.Open(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open audit export manifest: %w", err)
	}
	defer manifestHandle.Close()

	manifestScanner := bufio.NewScanner(manifestHandle)
	manifestScanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	manifestEntries := make([]ledger.SegmentManifestEntry, 0)
	for manifestScanner.Scan() {
		var manifestEntry ledger.SegmentManifestEntry
		if err := json.Unmarshal(manifestScanner.Bytes(), &manifestEntry); err != nil {
			return nil, fmt.Errorf("decode audit export manifest entry: %w", err)
		}
		manifestEntries = append(manifestEntries, manifestEntry)
	}
	if err := manifestScanner.Err(); err != nil {
		return nil, fmt.Errorf("scan audit export manifest: %w", err)
	}
	return manifestEntries, nil
}

func scanAuditExportFile(path string, appendEvent func(ledger.Event, int) bool) (bool, error) {
	fileHandle, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("open audit export source %q: %w", path, err)
	}
	defer fileHandle.Close()

	scanner := bufio.NewScanner(fileHandle)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		lineBytes := append([]byte(nil), scanner.Bytes()...)
		auditEvent, ok := ledger.ParseEvent(lineBytes)
		if !ok {
			return false, fmt.Errorf("decode audit export event from %q", path)
		}
		if !appendEvent(auditEvent, len(lineBytes)+1) {
			return true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("scan audit export source %q: %w", path, err)
	}
	return false, nil
}

func auditExportCursorForEvent(auditEvent ledger.Event) (uint64, string, bool) {
	if auditEvent.Data == nil {
		return 0, "", false
	}
	eventHash, _ := auditEvent.Data["event_hash"].(string)
	if strings.TrimSpace(eventHash) == "" {
		return 0, "", false
	}
	if rawAuditSequence, found := auditEvent.Data["audit_sequence"]; found {
		if auditSequence, ok := auditExportSequenceValue(rawAuditSequence); ok {
			return auditSequence, strings.TrimSpace(eventHash), true
		}
	}
	if rawLedgerSequence, found := auditEvent.Data["ledger_sequence"]; found {
		if ledgerSequence, ok := auditExportSequenceValue(rawLedgerSequence); ok {
			return ledgerSequence, strings.TrimSpace(eventHash), true
		}
	}
	return 0, "", false
}

func auditExportSequenceValue(rawSequence any) (uint64, bool) {
	switch typedSequence := rawSequence.(type) {
	case float64:
		if typedSequence < 0 {
			return 0, false
		}
		return uint64(typedSequence), true
	case int:
		if typedSequence < 0 {
			return 0, false
		}
		return uint64(typedSequence), true
	case int64:
		if typedSequence < 0 {
			return 0, false
		}
		return uint64(typedSequence), true
	case uint64:
		return typedSequence, true
	default:
		return 0, false
	}
}
