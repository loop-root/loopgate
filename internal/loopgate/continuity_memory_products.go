package loopgate

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

func (server *Server) loadMemoryWakeState(tenantID string) (MemoryWakeStateResponse, error) {
	backend, err := server.memoryBackendForTenant(tenantID)
	if err != nil {
		return MemoryWakeStateResponse{}, err
	}
	return backend.BuildWakeState(context.Background(), MemoryWakeStateRequest{Scope: memoryScopeGlobal})
}

func (server *Server) loadMemoryDiagnosticWake(tenantID string) MemoryDiagnosticWakeResponse {
	server.memoryMu.Lock()
	defer server.memoryMu.Unlock()
	partition, err := server.ensureMemoryPartitionLocked(tenantID)
	if err != nil {
		return MemoryDiagnosticWakeResponse{}
	}
	return cloneMemoryDiagnosticWakeResponse(memoryDiagnosticWakeResponseFromReport(partition.state.DiagnosticWake))
}

func (server *Server) discoverMemoryFromPartitionState(partition *memoryPartition, validatedRequest MemoryDiscoverRequest) (MemoryDiscoverResponse, error) {
	continuityBackend, ok := partition.backend.(*continuityTCLMemoryBackend)
	if !ok {
		if partition.backend == nil {
			return MemoryDiscoverResponse{}, fmt.Errorf("memory backend is not configured")
		}
		return MemoryDiscoverResponse{}, fmt.Errorf("memory backend %q does not support continuity_tcl discover helpers", partition.backend.Name())
	}
	// Retain the partition-state helper for focused tests while the live discover path
	// routes through the backend interface. This keeps test setup small without letting
	// production traffic bypass the backend seam.
	return continuityBackend.discoverFromBoundPartitionState(validatedRequest)
}

func (server *Server) recallMemoryFromPartitionState(partition *memoryPartition, validatedRequest MemoryRecallRequest) (MemoryRecallResponse, error) {
	continuityBackend, ok := partition.backend.(*continuityTCLMemoryBackend)
	if !ok {
		if partition.backend == nil {
			return MemoryRecallResponse{}, fmt.Errorf("memory backend is not configured")
		}
		return MemoryRecallResponse{}, fmt.Errorf("memory backend %q does not support continuity_tcl recall helpers", partition.backend.Name())
	}
	// Retain the partition-state helper for focused tests while the live recall path
	// routes through the backend interface. This keeps test setup small without letting
	// production traffic bypass the backend seam.
	return continuityBackend.recallFromBoundPartitionState(validatedRequest)
}

type explicitProfileFactRecord struct {
	InspectionID   string
	DistillateID   string
	ResonateKeyID  string
	FactKey        string
	FactValue      string
	AnchorTupleKey string
	CreatedAtUTC   string
}

func activeExplicitProfileFactByAnchorTuple(currentState continuityMemoryState, anchorVersion string, anchorKey string) (explicitProfileFactRecord, bool) {
	wantedAnchorTupleKey := anchorTupleKey(anchorVersion, anchorKey)
	if wantedAnchorTupleKey == "" {
		return explicitProfileFactRecord{}, false
	}
	return activeExplicitProfileFactByAnchorTupleKey(currentState, wantedAnchorTupleKey)
}

func activeExplicitProfileFactByAnchorTupleKey(currentState continuityMemoryState, wantedAnchorTupleKey string) (explicitProfileFactRecord, bool) {
	if strings.TrimSpace(wantedAnchorTupleKey) == "" {
		return explicitProfileFactRecord{}, false
	}
	for _, distillateRecord := range activeLoopgateDistillates(currentState) {
		explicitProfileFact, found := explicitProfileFactFromDistillate(currentState, distillateRecord)
		if found && explicitProfileFact.AnchorTupleKey == wantedAnchorTupleKey {
			return explicitProfileFact, true
		}
	}
	return explicitProfileFactRecord{}, false
}

func explicitProfileFactFromDistillate(currentState continuityMemoryState, distillateRecord continuityDistillateRecord) (explicitProfileFactRecord, bool) {
	if !isExplicitProfileFactDistillate(distillateRecord) || len(distillateRecord.Facts) != 1 {
		return explicitProfileFactRecord{}, false
	}
	var resonateKeyID string
	for _, resonateKeyRecord := range currentState.ResonateKeys {
		if resonateKeyRecord.DistillateID == distillateRecord.DistillateID {
			resonateKeyID = resonateKeyRecord.KeyID
			break
		}
	}
	factRecord := distillateRecord.Facts[0]
	factValue, isString := factRecord.Value.(string)
	if !isString {
		return explicitProfileFactRecord{}, false
	}
	anchorVersion, anchorKey := continuityFactAnchorTuple(factRecord)
	return explicitProfileFactRecord{
		InspectionID:   distillateRecord.InspectionID,
		DistillateID:   distillateRecord.DistillateID,
		ResonateKeyID:  resonateKeyID,
		FactKey:        factRecord.Name,
		FactValue:      factValue,
		AnchorTupleKey: anchorTupleKey(anchorVersion, anchorKey),
		CreatedAtUTC:   distillateRecord.CreatedAtUTC,
	}, true
}

func isExplicitProfileFactDistillate(distillateRecord continuityDistillateRecord) bool {
	for _, sourceRef := range distillateRecord.SourceRefs {
		if sourceRef.Kind == explicitProfileFactSourceKind {
			return true
		}
	}
	return false
}

func loopgateRecallOpenItems(distillateRecord continuityDistillateRecord) ([]string, []MemoryWakeStateOpenItem) {
	activeGoalsByID := map[string]string{}
	activeGoalOrder := make([]string, 0, len(distillateRecord.GoalOps))
	for _, goalOp := range distillateRecord.GoalOps {
		switch goalOp.Action {
		case "opened":
			activeGoalsByID[goalOp.GoalID] = goalOp.Text
			activeGoalOrder = appendWithoutDuplicate(activeGoalOrder, goalOp.GoalID)
		case "closed":
			delete(activeGoalsByID, goalOp.GoalID)
			activeGoalOrder = removeStringValue(activeGoalOrder, goalOp.GoalID)
		}
	}
	activeGoals := make([]string, 0, len(activeGoalOrder))
	for _, goalID := range activeGoalOrder {
		if goalText, found := activeGoalsByID[goalID]; found {
			activeGoals = append(activeGoals, goalText)
		}
	}

	unresolvedItemsByID := map[string]MemoryWakeStateOpenItem{}
	unresolvedItemOrder := make([]string, 0, len(distillateRecord.UnresolvedItemOps))
	for _, itemOp := range distillateRecord.UnresolvedItemOps {
		switch itemOp.Action {
		case "opened":
			taskMetadata := explicitTodoTaskMetadataFromDistillate(distillateRecord)
			taskMetadata.ID = itemOp.ItemID
			taskMetadata.Text = itemOp.Text
			taskMetadata.Status = explicitTodoWorkflowStatusTodo
			if taskMetadata.CreatedAtUTC == "" {
				taskMetadata.CreatedAtUTC = distillateRecord.CreatedAtUTC
			}
			unresolvedItemsByID[itemOp.ItemID] = taskMetadata
			unresolvedItemOrder = appendWithoutDuplicate(unresolvedItemOrder, itemOp.ItemID)
		case "closed":
			delete(unresolvedItemsByID, itemOp.ItemID)
			unresolvedItemOrder = removeStringValue(unresolvedItemOrder, itemOp.ItemID)
		case todoItemOpStatusSet:
			if existingItem, ok := unresolvedItemsByID[itemOp.ItemID]; ok {
				if normalized := normalizeExplicitTodoWorkflowStatus(itemOp.Status); normalized != "" {
					existingItem.Status = normalized
					unresolvedItemsByID[itemOp.ItemID] = existingItem
				}
			}
		}
	}
	unresolvedItems := make([]MemoryWakeStateOpenItem, 0, len(unresolvedItemOrder))
	for _, itemID := range unresolvedItemOrder {
		if unresolvedItem, found := unresolvedItemsByID[itemID]; found {
			unresolvedItems = append(unresolvedItems, unresolvedItem)
		}
	}
	return activeGoals, unresolvedItems
}

func activeLoopgateDistillates(currentState continuityMemoryState) []continuityDistillateRecord {
	distillates := make([]continuityDistillateRecord, 0, len(currentState.Distillates))
	for _, distillateRecord := range currentState.Distillates {
		decision := inspectionLineageSelectionDecision(currentState, distillateRecord.InspectionID)
		if !decision.Allowed {
			continue
		}
		distillates = append(distillates, cloneContinuityDistillateRecord(distillateRecord))
	}
	sort.Slice(distillates, func(leftIndex int, rightIndex int) bool {
		if distillates[leftIndex].CreatedAtUTC != distillates[rightIndex].CreatedAtUTC {
			return distillates[leftIndex].CreatedAtUTC < distillates[rightIndex].CreatedAtUTC
		}
		return distillates[leftIndex].DistillateID < distillates[rightIndex].DistillateID
	})
	return distillates
}

func activeLoopgateResonateKeys(currentState continuityMemoryState) []continuityResonateKeyRecord {
	resonateKeys := make([]continuityResonateKeyRecord, 0, len(currentState.ResonateKeys))
	for _, resonateKeyRecord := range currentState.ResonateKeys {
		_, _, decision, err := resolveRecallMaterial(currentState, resonateKeyRecord.KeyID)
		if err != nil || !decision.Allowed {
			continue
		}
		resonateKeys = append(resonateKeys, cloneContinuityResonateKeyRecord(resonateKeyRecord))
	}
	sort.Slice(resonateKeys, func(leftIndex int, rightIndex int) bool {
		if resonateKeys[leftIndex].CreatedAtUTC != resonateKeys[rightIndex].CreatedAtUTC {
			return resonateKeys[leftIndex].CreatedAtUTC < resonateKeys[rightIndex].CreatedAtUTC
		}
		return resonateKeys[leftIndex].KeyID < resonateKeys[rightIndex].KeyID
	})
	return resonateKeys
}

func resolveRecallMaterial(currentState continuityMemoryState, requestedKeyID string) (continuityResonateKeyRecord, continuityDistillateRecord, continuityEligibilityDecision, error) {
	resonateKeyRecord, found := currentState.ResonateKeys[requestedKeyID]
	if !found {
		return continuityResonateKeyRecord{}, continuityDistillateRecord{}, continuityEligibilityDecision{}, continuityGovernanceError{
			httpStatus:     404,
			responseStatus: ResponseStatusDenied,
			denialCode:     DenialCodeContinuityInspectionNotFound,
			reason:         fmt.Sprintf("resonate key %q not found", requestedKeyID),
		}
	}
	distillateRecord, found := currentState.Distillates[resonateKeyRecord.DistillateID]
	if !found {
		return continuityResonateKeyRecord{}, continuityDistillateRecord{}, continuityEligibilityDecision{}, continuityGovernanceError{
			httpStatus:     404,
			responseStatus: ResponseStatusDenied,
			denialCode:     DenialCodeContinuityInspectionNotFound,
			reason:         fmt.Sprintf("distillate for key %q not found", requestedKeyID),
		}
	}
	decision := inspectionLineageSelectionDecision(currentState, distillateRecord.InspectionID)
	if !decision.Allowed {
		return continuityResonateKeyRecord{}, continuityDistillateRecord{}, decision, continuityGovernanceError{
			httpStatus:     403,
			responseStatus: ResponseStatusDenied,
			denialCode:     decision.DenialCode,
			reason:         fmt.Sprintf("resonate key %q is not eligible", requestedKeyID),
		}
	}
	return cloneContinuityResonateKeyRecord(resonateKeyRecord), cloneContinuityDistillateRecord(distillateRecord), decision, nil
}

func memoryRecallFactFromDistillateFact(factRecord continuityDistillateFact, stateClass string) MemoryRecallFact {
	conflictAnchorVersion, conflictAnchorKey := continuityFactAnchorTuple(factRecord)
	return MemoryRecallFact{
		Name:               factRecord.Name,
		Value:              factRecord.Value,
		SourceRef:          factRecord.SourceRef,
		EpistemicFlavor:    factRecord.EpistemicFlavor,
		StateClass:         stateClass,
		ConflictKeyVersion: conflictAnchorVersion,
		ConflictKey:        conflictAnchorKey,
		CertaintyScore:     factRecord.CertaintyScore,
	}
}
