package loopgate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"morph/internal/config"
)

type continuityMemoryState struct {
	SchemaVersion  string
	Inspections    map[string]continuityInspectionRecord
	Distillates    map[string]continuityDistillateRecord
	ResonateKeys   map[string]continuityResonateKeyRecord
	WakeState      MemoryWakeStateResponse
	DiagnosticWake continuityDiagnosticWakeReport
}

type continuityMemoryStateFile struct {
	SchemaVersion  string                         `json:"schema_version"`
	Inspections    []continuityInspectionRecord   `json:"inspections,omitempty"`
	Distillates    []continuityDistillateRecord   `json:"distillates,omitempty"`
	ResonateKeys   []continuityResonateKeyRecord  `json:"resonate_keys,omitempty"`
	WakeState      MemoryWakeStateResponse        `json:"wake_state"`
	DiagnosticWake continuityDiagnosticWakeReport `json:"diagnostic_wake"`
}

func loadContinuityMemoryState(rootPath string, legacyStatePath string) (continuityMemoryState, error) {
	memoryPaths := newContinuityMemoryPaths(rootPath, legacyStatePath)
	_, continuityEventsErr := os.Stat(memoryPaths.ContinuityEventsPath)
	if continuityEventsErr == nil {
		replayedState, replayErr := replayContinuityMemoryStateFromEvents(memoryPaths)
		if replayErr != nil {
			return continuityMemoryState{}, fmt.Errorf("replay continuity event log: %w", replayErr)
		}
		return replayedState, nil
	}
	if !os.IsNotExist(continuityEventsErr) {
		return continuityMemoryState{}, continuityEventsErr
	}

	rawStateBytes, err := os.ReadFile(memoryPaths.CurrentStatePath)
	if err != nil {
		if os.IsNotExist(err) {
			if strings.TrimSpace(memoryPaths.LegacyStatePath) != "" {
				legacyState, legacyErr := loadLegacyContinuityMemoryState(memoryPaths.LegacyStatePath)
				if legacyErr == nil {
					return legacyState, nil
				}
				if !os.IsNotExist(legacyErr) {
					return continuityMemoryState{}, legacyErr
				}
			}
			return newEmptyContinuityMemoryState(), nil
		}
		return continuityMemoryState{}, err
	}

	var parsedStateFile continuityMemoryStateFile
	decoder := json.NewDecoder(bytes.NewReader(rawStateBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parsedStateFile); err != nil {
		return continuityMemoryState{}, err
	}

	loadedState := continuityMemoryState{
		SchemaVersion:  strings.TrimSpace(parsedStateFile.SchemaVersion),
		Inspections:    make(map[string]continuityInspectionRecord, len(parsedStateFile.Inspections)),
		Distillates:    make(map[string]continuityDistillateRecord, len(parsedStateFile.Distillates)),
		ResonateKeys:   make(map[string]continuityResonateKeyRecord, len(parsedStateFile.ResonateKeys)),
		WakeState:      parsedStateFile.WakeState,
		DiagnosticWake: parsedStateFile.DiagnosticWake,
	}
	if loadedState.SchemaVersion == "" {
		loadedState.SchemaVersion = continuityMemorySchemaVersion
	}
	for _, inspectionRecord := range parsedStateFile.Inspections {
		normalizedInspectionRecord, err := normalizeContinuityInspectionRecord(inspectionRecord)
		if err != nil {
			return continuityMemoryState{}, fmt.Errorf("normalize inspection %q: %w", inspectionRecord.InspectionID, err)
		}
		loadedState.Inspections[normalizedInspectionRecord.InspectionID] = normalizedInspectionRecord
	}
	for _, distillateRecord := range parsedStateFile.Distillates {
		loadedState.Distillates[distillateRecord.DistillateID] = cloneContinuityDistillateRecord(distillateRecord)
	}
	for _, resonateKeyRecord := range parsedStateFile.ResonateKeys {
		loadedState.ResonateKeys[resonateKeyRecord.KeyID] = cloneContinuityResonateKeyRecord(resonateKeyRecord)
	}
	if err := loadedState.Validate(); err != nil {
		return continuityMemoryState{}, err
	}
	return canonicalizeContinuityMemoryState(loadedState), nil
}

func loadLegacyContinuityMemoryState(path string) (continuityMemoryState, error) {
	rawStateBytes, err := os.ReadFile(path)
	if err != nil {
		return continuityMemoryState{}, err
	}

	var parsedStateFile continuityMemoryStateFile
	decoder := json.NewDecoder(bytes.NewReader(rawStateBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parsedStateFile); err != nil {
		return continuityMemoryState{}, err
	}

	loadedState := continuityMemoryState{
		SchemaVersion:  strings.TrimSpace(parsedStateFile.SchemaVersion),
		Inspections:    make(map[string]continuityInspectionRecord, len(parsedStateFile.Inspections)),
		Distillates:    make(map[string]continuityDistillateRecord, len(parsedStateFile.Distillates)),
		ResonateKeys:   make(map[string]continuityResonateKeyRecord, len(parsedStateFile.ResonateKeys)),
		WakeState:      parsedStateFile.WakeState,
		DiagnosticWake: parsedStateFile.DiagnosticWake,
	}
	if loadedState.SchemaVersion == "" {
		loadedState.SchemaVersion = continuityMemorySchemaVersion
	}
	for _, inspectionRecord := range parsedStateFile.Inspections {
		normalizedInspectionRecord, normalizeErr := normalizeContinuityInspectionRecord(inspectionRecord)
		if normalizeErr != nil {
			return continuityMemoryState{}, fmt.Errorf("normalize inspection %q: %w", inspectionRecord.InspectionID, normalizeErr)
		}
		loadedState.Inspections[normalizedInspectionRecord.InspectionID] = normalizedInspectionRecord
	}
	for _, distillateRecord := range parsedStateFile.Distillates {
		loadedState.Distillates[distillateRecord.DistillateID] = cloneContinuityDistillateRecord(distillateRecord)
	}
	for _, resonateKeyRecord := range parsedStateFile.ResonateKeys {
		loadedState.ResonateKeys[resonateKeyRecord.KeyID] = cloneContinuityResonateKeyRecord(resonateKeyRecord)
	}
	if err := loadedState.Validate(); err != nil {
		return continuityMemoryState{}, err
	}
	return canonicalizeContinuityMemoryState(loadedState), nil
}

func newEmptyContinuityMemoryState() continuityMemoryState {
	return continuityMemoryState{
		SchemaVersion: continuityMemorySchemaVersion,
		Inspections:   map[string]continuityInspectionRecord{},
		Distillates:   map[string]continuityDistillateRecord{},
		ResonateKeys:  map[string]continuityResonateKeyRecord{},
	}
}

func saveContinuityMemoryState(rootPath string, currentState continuityMemoryState, runtimeConfig config.RuntimeConfig, nowUTC time.Time) error {
	if err := currentState.Validate(); err != nil {
		return err
	}
	canonicalizedState := canonicalizeContinuityMemoryState(currentState)
	memoryPaths := newContinuityMemoryPaths(rootPath, "")
	stateFile := continuityMemoryStateFile{
		SchemaVersion:  canonicalizedState.SchemaVersion,
		WakeState:      canonicalizedState.WakeState,
		DiagnosticWake: canonicalizedState.DiagnosticWake,
	}

	inspectionIDs := make([]string, 0, len(canonicalizedState.Inspections))
	for inspectionID := range canonicalizedState.Inspections {
		inspectionIDs = append(inspectionIDs, inspectionID)
	}
	sort.Strings(inspectionIDs)
	for _, inspectionID := range inspectionIDs {
		stateFile.Inspections = append(stateFile.Inspections, cloneContinuityInspectionRecord(canonicalizedState.Inspections[inspectionID]))
	}

	distillateIDs := make([]string, 0, len(canonicalizedState.Distillates))
	for distillateID := range canonicalizedState.Distillates {
		distillateIDs = append(distillateIDs, distillateID)
	}
	sort.Strings(distillateIDs)
	for _, distillateID := range distillateIDs {
		stateFile.Distillates = append(stateFile.Distillates, cloneContinuityDistillateRecord(canonicalizedState.Distillates[distillateID]))
	}

	keyIDs := make([]string, 0, len(canonicalizedState.ResonateKeys))
	for keyID := range canonicalizedState.ResonateKeys {
		keyIDs = append(keyIDs, keyID)
	}
	sort.Strings(keyIDs)
	for _, keyID := range keyIDs {
		stateFile.ResonateKeys = append(stateFile.ResonateKeys, cloneContinuityResonateKeyRecord(canonicalizedState.ResonateKeys[keyID]))
	}

	stateBytes, err := json.MarshalIndent(stateFile, "", "  ")
	if err != nil {
		return err
	}
	if err := memoryPaths.ensure(); err != nil {
		return err
	}
	if err := memoryWritePrivateJSONAtomically(memoryPaths.CurrentStatePath, stateBytes); err != nil {
		return err
	}
	return writeContinuityArtifacts(memoryPaths, canonicalizedState, runtimeConfig, nowUTC)
}

func (currentState continuityMemoryState) Validate() error {
	if strings.TrimSpace(currentState.SchemaVersion) == "" {
		return fmt.Errorf("schema_version is required")
	}
	for inspectionID, inspectionRecord := range currentState.Inspections {
		if inspectionID != inspectionRecord.InspectionID {
			return fmt.Errorf("inspection key mismatch for %q", inspectionID)
		}
		if err := validateContinuityInspectionRecord(inspectionRecord); err != nil {
			return fmt.Errorf("inspection %q invalid: %w", inspectionID, err)
		}
	}
	for distillateID, distillateRecord := range currentState.Distillates {
		if distillateID != distillateRecord.DistillateID {
			return fmt.Errorf("distillate key mismatch for %q", distillateID)
		}
		if strings.TrimSpace(distillateRecord.InspectionID) == "" {
			return fmt.Errorf("distillate %q missing inspection_id", distillateID)
		}
		if _, found := currentState.Inspections[distillateRecord.InspectionID]; !found {
			return fmt.Errorf("distillate %q references unknown inspection %q", distillateID, distillateRecord.InspectionID)
		}
		for factIndex, factRecord := range distillateRecord.Facts {
			if err := validateContinuityDistillateFact(factRecord); err != nil {
				return fmt.Errorf("distillate %q fact %d invalid: %w", distillateID, factIndex, err)
			}
		}
		for goalOpIndex, goalOp := range distillateRecord.GoalOps {
			if err := validateContinuityGoalOp(goalOp); err != nil {
				return fmt.Errorf("distillate %q goal_op %d invalid: %w", distillateID, goalOpIndex, err)
			}
		}
		for itemOpIndex, itemOp := range distillateRecord.UnresolvedItemOps {
			if err := validateContinuityUnresolvedItemOp(itemOp); err != nil {
				return fmt.Errorf("distillate %q unresolved_item_op %d invalid: %w", distillateID, itemOpIndex, err)
			}
		}
	}
	for keyID, resonateKeyRecord := range currentState.ResonateKeys {
		if keyID != resonateKeyRecord.KeyID {
			return fmt.Errorf("resonate key mismatch for %q", keyID)
		}
		if strings.TrimSpace(resonateKeyRecord.DistillateID) == "" {
			return fmt.Errorf("resonate key %q missing distillate_id", keyID)
		}
		if _, found := currentState.Distillates[resonateKeyRecord.DistillateID]; !found {
			return fmt.Errorf("resonate key %q references unknown distillate %q", keyID, resonateKeyRecord.DistillateID)
		}
	}
	for inspectionID, inspectionRecord := range currentState.Inspections {
		for _, derivedDistillateID := range inspectionRecord.DerivedDistillateIDs {
			if _, found := currentState.Distillates[derivedDistillateID]; !found {
				return fmt.Errorf("inspection %q references unknown distillate %q", inspectionID, derivedDistillateID)
			}
		}
		for _, derivedKeyID := range inspectionRecord.DerivedResonateKeyIDs {
			if _, found := currentState.ResonateKeys[derivedKeyID]; !found {
				return fmt.Errorf("inspection %q references unknown resonate key %q", inspectionID, derivedKeyID)
			}
		}
	}
	return nil
}

func cloneContinuityMemoryState(currentState continuityMemoryState) continuityMemoryState {
	clonedState := continuityMemoryState{
		SchemaVersion:  currentState.SchemaVersion,
		Inspections:    make(map[string]continuityInspectionRecord, len(currentState.Inspections)),
		Distillates:    make(map[string]continuityDistillateRecord, len(currentState.Distillates)),
		ResonateKeys:   make(map[string]continuityResonateKeyRecord, len(currentState.ResonateKeys)),
		WakeState:      cloneMemoryWakeStateResponse(currentState.WakeState),
		DiagnosticWake: currentState.DiagnosticWake,
	}
	for inspectionID, inspectionRecord := range currentState.Inspections {
		clonedState.Inspections[inspectionID] = cloneContinuityInspectionRecord(inspectionRecord)
	}
	for distillateID, distillateRecord := range currentState.Distillates {
		clonedState.Distillates[distillateID] = cloneContinuityDistillateRecord(distillateRecord)
	}
	for keyID, resonateKeyRecord := range currentState.ResonateKeys {
		clonedState.ResonateKeys[keyID] = cloneContinuityResonateKeyRecord(resonateKeyRecord)
	}
	return clonedState
}

func cloneContinuityDistillateRecord(distillateRecord continuityDistillateRecord) continuityDistillateRecord {
	distillateRecord.SourceRefs = append([]continuityArtifactSourceRef(nil), distillateRecord.SourceRefs...)
	distillateRecord.Tags = append([]string(nil), distillateRecord.Tags...)
	distillateRecord.Facts = append([]continuityDistillateFact(nil), distillateRecord.Facts...)
	for factIndex := range distillateRecord.Facts {
		distillateRecord.Facts[factIndex] = normalizeContinuityDistillateFactForValidation(distillateRecord.Facts[factIndex])
	}
	distillateRecord.GoalOps = append([]continuityGoalOp(nil), distillateRecord.GoalOps...)
	for goalOpIndex := range distillateRecord.GoalOps {
		distillateRecord.GoalOps[goalOpIndex] = normalizeContinuityGoalOpForValidation(distillateRecord.GoalOps[goalOpIndex])
	}
	distillateRecord.UnresolvedItemOps = append([]continuityUnresolvedItemOp(nil), distillateRecord.UnresolvedItemOps...)
	for itemOpIndex := range distillateRecord.UnresolvedItemOps {
		distillateRecord.UnresolvedItemOps[itemOpIndex] = normalizeContinuityUnresolvedItemOpForValidation(distillateRecord.UnresolvedItemOps[itemOpIndex])
	}
	return distillateRecord
}

func cloneContinuityResonateKeyRecord(resonateKeyRecord continuityResonateKeyRecord) continuityResonateKeyRecord {
	resonateKeyRecord.Tags = append([]string(nil), resonateKeyRecord.Tags...)
	return resonateKeyRecord
}

func cloneMemoryWakeStateResponse(wakeStateResponse MemoryWakeStateResponse) MemoryWakeStateResponse {
	wakeStateResponse.SourceRefs = append([]MemoryWakeStateSourceRef(nil), wakeStateResponse.SourceRefs...)
	wakeStateResponse.ActiveGoals = append([]string(nil), wakeStateResponse.ActiveGoals...)
	wakeStateResponse.UnresolvedItems = append([]MemoryWakeStateOpenItem(nil), wakeStateResponse.UnresolvedItems...)
	wakeStateResponse.RecentFacts = append([]MemoryWakeStateRecentFact(nil), wakeStateResponse.RecentFacts...)
	wakeStateResponse.ResonateKeys = append([]string(nil), wakeStateResponse.ResonateKeys...)
	return wakeStateResponse
}

func canonicalizeContinuityMemoryState(currentState continuityMemoryState) continuityMemoryState {
	canonicalizedState := cloneContinuityMemoryState(currentState)
	for distillateID, distillateRecord := range canonicalizedState.Distillates {
		canonicalizedState.Distillates[distillateID] = canonicalizeContinuityDistillateRecord(distillateRecord)
	}
	return canonicalizedState
}
