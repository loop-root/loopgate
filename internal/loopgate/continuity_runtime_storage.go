package loopgate

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"loopgate/internal/config"
)

func writeJSONArtifact(path string, payload interface{}) error {
	payloadBytes, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return memoryWritePrivateJSONAtomically(path, payloadBytes)
}

func memoryWritePrivateJSONAtomically(targetPath string, fileContents []byte) error {
	parentDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(parentDir, 0o700); err != nil {
		return err
	}
	tempFileHandle, err := os.CreateTemp(parentDir, ".tmp-*")
	if err != nil {
		return err
	}
	tempPath := tempFileHandle.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()
	if _, err := tempFileHandle.Write(fileContents); err != nil {
		_ = tempFileHandle.Close()
		return err
	}
	if len(fileContents) == 0 || fileContents[len(fileContents)-1] != '\n' {
		if _, err := tempFileHandle.Write([]byte("\n")); err != nil {
			_ = tempFileHandle.Close()
			return err
		}
	}
	if err := tempFileHandle.Sync(); err != nil {
		_ = tempFileHandle.Close()
		return err
	}
	if err := tempFileHandle.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, targetPath); err != nil {
		return err
	}
	parentHandle, err := os.Open(parentDir)
	if err == nil {
		_ = parentHandle.Sync()
		_ = parentHandle.Close()
	}
	return nil
}

func writeContinuityArtifacts(memoryPaths continuityMemoryPaths, currentState continuityMemoryState, runtimeConfig config.RuntimeConfig, nowUTC time.Time) error {
	if err := memoryPaths.ensure(); err != nil {
		return err
	}
	nowUTC = nowUTC.UTC()

	for _, derivedDir := range []string{
		memoryPaths.DistillatesDir,
		memoryPaths.WakeRuntimeDir,
		memoryPaths.WakeDiagnosticDir,
		memoryPaths.ProfilesCorrectionsDir,
		memoryPaths.ProfilesRevalidationDir,
	} {
		if err := removeDerivedJSONArtifacts(derivedDir); err != nil {
			return err
		}
	}

	goalSnapshot := buildGoalsCurrentSnapshot(currentState)
	if err := writeJSONArtifact(memoryPaths.GoalsCurrentPath, goalSnapshot); err != nil {
		return err
	}
	taskSnapshot := buildTasksCurrentSnapshot(currentState)
	if err := writeJSONArtifact(memoryPaths.TasksCurrentPath, taskSnapshot); err != nil {
		return err
	}
	reviewSnapshot := buildReviewsCurrentSnapshot(currentState)
	if err := writeJSONArtifact(memoryPaths.ReviewsCurrentPath, reviewSnapshot); err != nil {
		return err
	}
	resolvedProfileSnapshot := buildResolvedProfileSnapshot(runtimeConfig, nowUTC)
	if err := writeJSONArtifact(memoryPaths.ProfileResolvedPath, resolvedProfileSnapshot); err != nil {
		return err
	}
	rankingCache := buildRankingCache(currentState, nowUTC)
	if err := writeJSONArtifact(memoryPaths.RankingCachePath, rankingCache); err != nil {
		return err
	}
	for _, correctionRecord := range resolvedProfileSnapshot.ActiveCorrections {
		if err := writeJSONArtifact(filepath.Join(memoryPaths.ProfilesCorrectionsDir, correctionRecord.CorrectionID+".json"), correctionRecord); err != nil {
			return err
		}
	}
	for _, revalidationTicket := range buildRevalidationTickets(runtimeConfig, nowUTC) {
		if err := writeJSONArtifact(filepath.Join(memoryPaths.ProfilesRevalidationDir, revalidationTicket.RevalidationID+".json"), revalidationTicket); err != nil {
			return err
		}
	}
	for _, distillateRecord := range currentState.Distillates {
		if err := writeJSONArtifact(filepath.Join(memoryPaths.DistillatesDir, distillateRecord.DistillateID+".json"), distillateRecord); err != nil {
			return err
		}
	}
	if strings.TrimSpace(currentState.WakeState.ID) != "" {
		if err := writeJSONArtifact(filepath.Join(memoryPaths.WakeRuntimeDir, currentState.WakeState.ID+".json"), currentState.WakeState); err != nil {
			return err
		}
	}
	if strings.TrimSpace(currentState.DiagnosticWake.ReportID) != "" {
		if err := writeJSONArtifact(filepath.Join(memoryPaths.WakeDiagnosticDir, currentState.DiagnosticWake.ReportID+".json"), currentState.DiagnosticWake); err != nil {
			return err
		}
	}
	return nil
}

func removeDerivedJSONArtifacts(dir string) error {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, dirEntry := range dirEntries {
		if dirEntry.IsDir() {
			continue
		}
		if filepath.Ext(dirEntry.Name()) != ".json" {
			continue
		}
		if err := os.Remove(filepath.Join(dir, dirEntry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func appendPrivateJSONL(path string, payload interface{}) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	payloadBytes = append(payloadBytes, '\n')
	fileHandle, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := fileHandle.Write(payloadBytes); err != nil {
		_ = fileHandle.Close()
		return err
	}
	if err := fileHandle.Sync(); err != nil {
		_ = fileHandle.Close()
		return err
	}
	return fileHandle.Close()
}

func appendContinuityMutationEvents(memoryPaths continuityMemoryPaths, mutationEvents continuityMutationEvents) error {
	if err := memoryPaths.ensure(); err != nil {
		return err
	}
	for _, continuityEvent := range mutationEvents.Continuity {
		if err := appendPrivateJSONL(memoryPaths.ContinuityEventsPath, continuityEvent); err != nil {
			return err
		}
	}
	for _, goalEvent := range mutationEvents.Goal {
		if err := appendPrivateJSONL(memoryPaths.GoalEventsPath, goalEvent); err != nil {
			return err
		}
	}
	for _, profileEvent := range mutationEvents.Profile {
		if err := appendPrivateJSONL(memoryPaths.ProfileEventsPath, profileEvent); err != nil {
			return err
		}
	}
	return nil
}

func replayContinuityMemoryStateFromEvents(memoryPaths continuityMemoryPaths) (continuityMemoryState, error) {
	replayedState := newEmptyContinuityMemoryState()
	if err := replayJSONL(memoryPaths.ContinuityEventsPath, func(rawLine []byte) error {
		var continuityEvent continuityAuthoritativeEvent
		decoder := json.NewDecoder(strings.NewReader(string(rawLine)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&continuityEvent); err != nil {
			return err
		}
		switch continuityEvent.EventType {
		case "continuity_inspection_recorded":
			if continuityEvent.Inspection == nil {
				return fmt.Errorf("continuity inspection event %q missing inspection", continuityEvent.EventID)
			}
			if _, found := replayedState.Inspections[continuityEvent.Inspection.InspectionID]; found {
				return fmt.Errorf("duplicate continuity inspection %q", continuityEvent.Inspection.InspectionID)
			}
			replayedState.Inspections[continuityEvent.Inspection.InspectionID] = cloneContinuityInspectionRecord(*continuityEvent.Inspection)
			if continuityEvent.Distillate != nil {
				replayedState.Distillates[continuityEvent.Distillate.DistillateID] = cloneContinuityDistillateRecord(*continuityEvent.Distillate)
			}
			if continuityEvent.ResonateKey != nil {
				replayedState.ResonateKeys[continuityEvent.ResonateKey.KeyID] = cloneContinuityResonateKeyRecord(*continuityEvent.ResonateKey)
			}
		case "memory_fact_remembered":
			if continuityEvent.Inspection == nil {
				return fmt.Errorf("memory fact event %q missing inspection", continuityEvent.EventID)
			}
			if continuityEvent.Distillate == nil {
				return fmt.Errorf("memory fact event %q missing distillate", continuityEvent.EventID)
			}
			if continuityEvent.ResonateKey == nil {
				return fmt.Errorf("memory fact event %q missing resonate_key", continuityEvent.EventID)
			}
			replayedState.Inspections[continuityEvent.Inspection.InspectionID] = cloneContinuityInspectionRecord(*continuityEvent.Inspection)
			replayedState.Distillates[continuityEvent.Distillate.DistillateID] = cloneContinuityDistillateRecord(*continuityEvent.Distillate)
			replayedState.ResonateKeys[continuityEvent.ResonateKey.KeyID] = cloneContinuityResonateKeyRecord(*continuityEvent.ResonateKey)
		case "todo_item_added":
			if continuityEvent.Inspection == nil {
				return fmt.Errorf("todo add event %q missing inspection", continuityEvent.EventID)
			}
			if continuityEvent.Distillate == nil {
				return fmt.Errorf("todo add event %q missing distillate", continuityEvent.EventID)
			}
			if continuityEvent.ResonateKey == nil {
				return fmt.Errorf("todo add event %q missing resonate_key", continuityEvent.EventID)
			}
			replayedState.Inspections[continuityEvent.Inspection.InspectionID] = cloneContinuityInspectionRecord(*continuityEvent.Inspection)
			replayedState.Distillates[continuityEvent.Distillate.DistillateID] = cloneContinuityDistillateRecord(*continuityEvent.Distillate)
			replayedState.ResonateKeys[continuityEvent.ResonateKey.KeyID] = cloneContinuityResonateKeyRecord(*continuityEvent.ResonateKey)
		case "todo_item_completed":
			if continuityEvent.Inspection == nil {
				return fmt.Errorf("todo complete event %q missing inspection", continuityEvent.EventID)
			}
			if continuityEvent.Distillate == nil {
				return fmt.Errorf("todo complete event %q missing distillate", continuityEvent.EventID)
			}
			replayedState.Inspections[continuityEvent.Inspection.InspectionID] = cloneContinuityInspectionRecord(*continuityEvent.Inspection)
			replayedState.Distillates[continuityEvent.Distillate.DistillateID] = cloneContinuityDistillateRecord(*continuityEvent.Distillate)
		case "todo_item_status_changed":
			if continuityEvent.Inspection == nil {
				return fmt.Errorf("todo status event %q missing inspection", continuityEvent.EventID)
			}
			if continuityEvent.Distillate == nil {
				return fmt.Errorf("todo status event %q missing distillate", continuityEvent.EventID)
			}
			replayedState.Inspections[continuityEvent.Inspection.InspectionID] = cloneContinuityInspectionRecord(*continuityEvent.Inspection)
			replayedState.Distillates[continuityEvent.Distillate.DistillateID] = cloneContinuityDistillateRecord(*continuityEvent.Distillate)
		case "continuity_inspection_reviewed":
			inspectionRecord, found := replayedState.Inspections[continuityEvent.InspectionID]
			if !found {
				return fmt.Errorf("review event references unknown inspection %q", continuityEvent.InspectionID)
			}
			if continuityEvent.Review == nil {
				return fmt.Errorf("review event %q missing review", continuityEvent.EventID)
			}
			inspectionRecord.Review = *continuityEvent.Review
			replayedState.Inspections[continuityEvent.InspectionID] = inspectionRecord
		case "continuity_inspection_lineage_updated":
			inspectionRecord, found := replayedState.Inspections[continuityEvent.InspectionID]
			if !found {
				return fmt.Errorf("lineage event references unknown inspection %q", continuityEvent.InspectionID)
			}
			if continuityEvent.Lineage == nil {
				return fmt.Errorf("lineage event %q missing lineage", continuityEvent.EventID)
			}
			inspectionRecord.Lineage = *continuityEvent.Lineage
			stampContinuityDerivedArtifactsExcluded(&replayedState, inspectionRecord, parseTimeOrZero(inspectionRecord.Lineage.ChangedAtUTC))
			replayedState.Inspections[continuityEvent.InspectionID] = inspectionRecord
		default:
			return fmt.Errorf("unknown continuity event type %q", continuityEvent.EventType)
		}
		return nil
	}); err != nil {
		return continuityMemoryState{}, err
	}
	replayedState.SchemaVersion = continuityMemorySchemaVersion
	if err := replayedState.Validate(); err != nil {
		return continuityMemoryState{}, err
	}
	return canonicalizeContinuityMemoryState(replayedState), nil
}

func replayJSONL(path string, applyLine func([]byte) error) error {
	fileHandle, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fileHandle.Close()

	fileScanner := bufio.NewScanner(fileHandle)
	fileScanner.Buffer(make([]byte, 0, 1024*1024), 4*1024*1024)
	lineNumber := 0
	for fileScanner.Scan() {
		lineNumber++
		rawLine := bytes.TrimSpace(fileScanner.Bytes())
		if len(rawLine) == 0 {
			continue
		}
		if err := applyLine(rawLine); err != nil {
			return fmt.Errorf("%s line %d: %w", path, lineNumber, err)
		}
	}
	if err := fileScanner.Err(); err != nil {
		return err
	}
	return nil
}
