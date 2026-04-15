package ledger

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
)

// AuditHMACCheckpointInspection summarizes verified checkpoint state across a
// segmented audit ledger.
type AuditHMACCheckpointInspection struct {
	CheckpointCount                    int
	LastCheckpointThroughAuditSequence int64
	LastCheckpointTimestampUTC         string
	OrdinaryEventsSinceLastCheckpoint  int
}

// OrderedSegmentedPaths returns sealed segment paths followed by the active
// ledger path when present.
func OrderedSegmentedPaths(activePath string, rotationSettings RotationSettings) ([]string, error) {
	if err := validateRotationSettings(rotationSettings); err != nil {
		return nil, err
	}

	orderedPaths := make([]string, 0, 8)
	if rotationSettings.RotateAtBytes > 0 {
		verifiedManifestState, err := readVerifiedManifestState(
			rotationSettings.ManifestPath,
			rotationSettings.SegmentDir,
			rotationSettings.VerifyClosedSegmentsOnStartup,
		)
		if err != nil {
			return nil, err
		}
		for _, manifestEntry := range verifiedManifestState.entries {
			orderedPaths = append(orderedPaths, filepath.Join(rotationSettings.SegmentDir, manifestEntry.SegmentFilename))
		}
	}
	if _, err := os.Stat(activePath); err == nil {
		orderedPaths = append(orderedPaths, activePath)
	} else if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat active ledger: %w", err)
	}
	return orderedPaths, nil
}

// InspectAuditHMACCheckpoints verifies all configured checkpoint events against
// the provided key and confirms each checkpoint attests the most recent
// non-checkpoint audit event seen so far.
func InspectAuditHMACCheckpoints(paths []string, key []byte) (AuditHMACCheckpointInspection, error) {
	inspection := AuditHMACCheckpointInspection{}
	var lastNonCheckpointAuditSequence int64
	lastNonCheckpointEventHash := ""

	for _, path := range paths {
		fileHandle, err := os.Open(path)
		if err != nil {
			return inspection, fmt.Errorf("open audit file %q: %w", path, err)
		}

		scanner := bufio.NewScanner(fileHandle)
		scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
		for scanner.Scan() {
			parsedEvent, ok := ParseEvent(scanner.Bytes())
			if !ok {
				_ = fileHandle.Close()
				return inspection, fmt.Errorf("%w: malformed audit line in %s", ErrLedgerIntegrity, path)
			}

			if parsedEvent.Type == AuditLedgerHMACCheckpointEventType {
				if err := VerifyAuditLedgerHMACCheckpointEvent(parsedEvent, key); err != nil {
					_ = fileHandle.Close()
					return inspection, fmt.Errorf("verify audit checkpoint in %s: %w", path, err)
				}
				throughAuditSequence, err := int64FromEventData(parsedEvent.Data, "through_audit_sequence")
				if err != nil {
					_ = fileHandle.Close()
					return inspection, fmt.Errorf("decode checkpoint through_audit_sequence in %s: %w", path, err)
				}
				throughEventHash, ok := parsedEvent.Data["through_event_hash"].(string)
				if !ok {
					_ = fileHandle.Close()
					return inspection, fmt.Errorf("%w: checkpoint through_event_hash missing in %s", ErrLedgerIntegrity, path)
				}
				if throughAuditSequence != lastNonCheckpointAuditSequence {
					_ = fileHandle.Close()
					return inspection, fmt.Errorf(
						"%w: checkpoint through_audit_sequence %d does not match last non-checkpoint audit_sequence %d",
						ErrLedgerIntegrity,
						throughAuditSequence,
						lastNonCheckpointAuditSequence,
					)
				}
				if throughEventHash != lastNonCheckpointEventHash {
					_ = fileHandle.Close()
					return inspection, fmt.Errorf("%w: checkpoint through_event_hash does not match last non-checkpoint event hash", ErrLedgerIntegrity)
				}
				inspection.CheckpointCount++
				inspection.LastCheckpointThroughAuditSequence = throughAuditSequence
				inspection.LastCheckpointTimestampUTC = parsedEvent.TS
				inspection.OrdinaryEventsSinceLastCheckpoint = 0
				continue
			}

			auditSequence, err := int64FromEventData(parsedEvent.Data, "audit_sequence")
			if err != nil {
				_ = fileHandle.Close()
				return inspection, fmt.Errorf("decode audit_sequence in %s: %w", path, err)
			}
			eventHash, ok := parsedEvent.Data["event_hash"].(string)
			if !ok || eventHash == "" {
				_ = fileHandle.Close()
				return inspection, fmt.Errorf("%w: missing event_hash in %s", ErrLedgerIntegrity, path)
			}

			lastNonCheckpointAuditSequence = auditSequence
			lastNonCheckpointEventHash = eventHash
			inspection.OrdinaryEventsSinceLastCheckpoint++
		}
		if err := scanner.Err(); err != nil {
			_ = fileHandle.Close()
			return inspection, fmt.Errorf("scan audit file %q: %w", path, err)
		}
		if err := fileHandle.Close(); err != nil {
			return inspection, fmt.Errorf("close audit file %q: %w", path, err)
		}
	}

	return inspection, nil
}
