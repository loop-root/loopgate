package loopgate

import (
	"fmt"
	"time"
)

func (server *Server) transitionMorphlingLocked(morphlingID string, lifecycleEvent morphlingLifecycleEvent, eventTime time.Time, mutateRecord func(*morphlingRecord) error) (morphlingRecord, error) {
	currentRecord, found := server.morphlings[morphlingID]
	if !found {
		return morphlingRecord{}, errMorphlingNotFound
	}
	nextState, err := morphlingNextState(currentRecord.State, lifecycleEvent)
	if err != nil {
		return morphlingRecord{}, err
	}
	updatedRecord := cloneMorphlingRecord(currentRecord)
	updatedRecord.State = nextState
	updatedRecord.LastEventAtUTC = eventTime.UTC().Format(time.RFC3339Nano)
	if mutateRecord != nil {
		if err := mutateRecord(&updatedRecord); err != nil {
			return morphlingRecord{}, err
		}
	}
	updatedRecord.StatusText = morphlingStatusText(updatedRecord)
	if err := updatedRecord.Validate(); err != nil {
		return morphlingRecord{}, err
	}

	workingRecords := cloneMorphlingRecords(server.morphlings)
	workingRecords[morphlingID] = updatedRecord
	if err := saveMorphlingRecords(server.morphlingPath, workingRecords, server.morphlingStateKey); err != nil {
		return morphlingRecord{}, err
	}
	server.morphlings = workingRecords
	return updatedRecord, nil
}

func (server *Server) updateMorphlingRecordLocked(morphlingID string, eventTime time.Time, mutateRecord func(*morphlingRecord) error) (morphlingRecord, error) {
	currentRecord, found := server.morphlings[morphlingID]
	if !found {
		return morphlingRecord{}, errMorphlingNotFound
	}
	updatedRecord := cloneMorphlingRecord(currentRecord)
	updatedRecord.LastEventAtUTC = eventTime.UTC().Format(time.RFC3339Nano)
	if mutateRecord != nil {
		if err := mutateRecord(&updatedRecord); err != nil {
			return morphlingRecord{}, err
		}
	}
	updatedRecord.StatusText = morphlingStatusText(updatedRecord)
	if err := updatedRecord.Validate(); err != nil {
		return morphlingRecord{}, err
	}

	workingRecords := cloneMorphlingRecords(server.morphlings)
	workingRecords[morphlingID] = updatedRecord
	if err := saveMorphlingRecords(server.morphlingPath, workingRecords, server.morphlingStateKey); err != nil {
		return morphlingRecord{}, err
	}
	server.morphlings = workingRecords
	return updatedRecord, nil
}

func (server *Server) rollbackMorphlingRecordAfterAuditFailure(morphlingID string, expectedCurrentState string, previousRecord morphlingRecord) error {
	server.morphlingsMu.Lock()
	defer server.morphlingsMu.Unlock()

	currentRecord, found := server.morphlings[morphlingID]
	if !found {
		return errMorphlingNotFound
	}
	if currentRecord.State != expectedCurrentState {
		return fmt.Errorf("%w: morphling %s changed state from %s before audit rollback", errMorphlingStateInvalid, morphlingID, expectedCurrentState)
	}

	workingRecords := cloneMorphlingRecords(server.morphlings)
	workingRecords[morphlingID] = cloneMorphlingRecord(previousRecord)
	if err := saveMorphlingRecords(server.morphlingPath, workingRecords, server.morphlingStateKey); err != nil {
		return err
	}
	server.morphlings = workingRecords
	return nil
}

func (server *Server) restoreMorphlingRecords(previousRecords map[string]morphlingRecord) {
	server.morphlingsMu.Lock()
	defer server.morphlingsMu.Unlock()

	if err := saveMorphlingRecords(server.morphlingPath, previousRecords, server.morphlingStateKey); err != nil {
		return
	}
	server.morphlings = cloneMorphlingRecords(previousRecords)
}
