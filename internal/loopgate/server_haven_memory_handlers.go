package loopgate

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"morph/internal/secrets"
)

const (
	havenMemoryObjectKindExplicitFact = "explicit_fact"
	havenMemoryObjectKindTask         = "task"
	havenMemoryObjectKindGoal         = "goal"
	havenMemoryObjectKindInspection   = "inspection"
	havenMemoryObjectKindMixed        = "mixed"
)

func (server *Server) handleHavenMemoryInventory(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityMemoryRead) {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	server.memoryMu.Lock()
	partition, partitionErr := server.ensureMemoryPartitionLocked(tokenClaims.TenantID)
	var response HavenMemoryInventoryResponse
	if partitionErr == nil {
		response = buildHavenMemoryInventoryResponse(cloneContinuityMemoryState(partition.state), server.now().UTC())
	}
	server.memoryMu.Unlock()
	if partitionErr != nil {
		server.writeMemoryOperationError(writer, partitionErr)
		return
	}

	server.writeJSON(writer, http.StatusOK, response)
}

func (server *Server) handleHavenMemoryReset(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityMemoryReset) {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var resetRequest HavenMemoryResetRequest
	if err := decodeJSONBytes(requestBodyBytes, &resetRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	resetResponse, err := server.resetHavenMemory(tokenClaims, resetRequest)
	if err != nil {
		server.writeMemoryOperationError(writer, err)
		return
	}
	server.writeJSON(writer, http.StatusOK, resetResponse)
}

func buildHavenMemoryInventoryResponse(currentState continuityMemoryState, nowUTC time.Time) HavenMemoryInventoryResponse {
	response := HavenMemoryInventoryResponse{
		WakeStateID:             currentState.WakeState.ID,
		WakeCreatedAtUTC:        currentState.WakeState.CreatedAtUTC,
		RecentFactCount:         len(currentState.WakeState.RecentFacts),
		ActiveGoalCount:         len(currentState.WakeState.ActiveGoals),
		UnresolvedItemCount:     len(currentState.WakeState.UnresolvedItems),
		ResonateKeyCount:        len(currentState.WakeState.ResonateKeys),
		IncludedDiagnosticCount: currentState.DiagnosticWake.EntryCount(),
		ExcludedDiagnosticCount: len(currentState.DiagnosticWake.ExcludedEntries),
		Objects:                 make([]HavenMemoryObjectEntry, 0, len(currentState.Inspections)),
	}

	inspectionRecords := make([]continuityInspectionRecord, 0, len(currentState.Inspections))
	for _, inspectionRecord := range currentState.Inspections {
		inspectionRecords = append(inspectionRecords, normalizeContinuityInspectionRecordMust(inspectionRecord))
	}
	sort.Slice(inspectionRecords, func(leftIndex int, rightIndex int) bool {
		leftRecord := inspectionRecords[leftIndex]
		rightRecord := inspectionRecords[rightIndex]
		leftTimestamp := firstNonEmptyMemoryTimestamp(leftRecord.CompletedAtUTC, leftRecord.SubmittedAtUTC)
		rightTimestamp := firstNonEmptyMemoryTimestamp(rightRecord.CompletedAtUTC, rightRecord.SubmittedAtUTC)
		switch {
		case leftTimestamp != rightTimestamp:
			return leftTimestamp > rightTimestamp
		default:
			return leftRecord.InspectionID > rightRecord.InspectionID
		}
	})

	for _, inspectionRecord := range inspectionRecords {
		switch inspectionRecord.Review.Status {
		case continuityReviewStatusPendingReview:
			response.PendingReviewCount++
		}
		switch inspectionRecord.Lineage.Status {
		case continuityLineageStatusEligible:
			response.EligibleCount++
		case continuityLineageStatusTombstoned:
			response.TombstonedCount++
		case continuityLineageStatusPurged:
			response.PurgedCount++
		}
		response.Objects = append(response.Objects, buildHavenMemoryObjectEntry(currentState, inspectionRecord, nowUTC))
	}

	return response
}

func buildHavenMemoryObjectEntry(currentState continuityMemoryState, inspectionRecord continuityInspectionRecord, nowUTC time.Time) HavenMemoryObjectEntry {
	objectKind, summaryText := summarizeContinuityInspectionForHaven(currentState, inspectionRecord)
	retentionWindowActive := inspectionRecord.Lineage.Status == continuityLineageStatusTombstoned && continuitySupersessionRetentionActive(inspectionRecord.Lineage, nowUTC)

	return HavenMemoryObjectEntry{
		InspectionID:             inspectionRecord.InspectionID,
		ThreadID:                 inspectionRecord.ThreadID,
		Scope:                    inspectionRecord.Scope,
		ObjectKind:               objectKind,
		Summary:                  summaryText,
		SubmittedAtUTC:           inspectionRecord.SubmittedAtUTC,
		CompletedAtUTC:           inspectionRecord.CompletedAtUTC,
		DerivationOutcome:        inspectionRecord.DerivationOutcome,
		ReviewStatus:             inspectionRecord.Review.Status,
		LineageStatus:            inspectionRecord.Lineage.Status,
		GoalType:                 firstDerivedGoalType(currentState, inspectionRecord),
		GoalFamilyID:             firstDerivedGoalFamilyID(currentState, inspectionRecord),
		DerivedDistillateCount:   len(inspectionRecord.DerivedDistillateIDs),
		DerivedResonateKeyCount:  len(inspectionRecord.DerivedResonateKeyIDs),
		SupersedesInspectionID:   inspectionRecord.Lineage.SupersedesInspectionID,
		SupersededByInspectionID: inspectionRecord.Lineage.SupersededByInspectionID,
		RetentionWindowActive:    retentionWindowActive,
		CanReview:                inspectionRecord.Review.Status == continuityReviewStatusPendingReview,
		CanTombstone:             inspectionRecord.Lineage.Status == continuityLineageStatusEligible,
		CanPurge:                 inspectionRecord.Lineage.Status != continuityLineageStatusPurged && !retentionWindowActive,
	}
}

func summarizeContinuityInspectionForHaven(currentState continuityMemoryState, inspectionRecord continuityInspectionRecord) (string, string) {
	if inspectionRecord.Lineage.Status == continuityLineageStatusPurged {
		return havenMemoryObjectKindInspection, "purged continuity lineage"
	}

	derivedDistillates := derivedDistillatesForInspection(currentState, inspectionRecord)
	summaryCandidates := make([]havenMemorySummaryCandidate, 0, len(derivedDistillates))
	seenKinds := map[string]struct{}{}

	for _, distillateRecord := range derivedDistillates {
		if isExplicitProfileFactDistillate(distillateRecord) {
			for _, factRecord := range distillateRecord.Facts {
				summaryCandidates = append(summaryCandidates, havenMemorySummaryCandidate{
					ObjectKind: havenMemoryObjectKindExplicitFact,
					Summary:    redactedWakeSummary(fmt.Sprintf("%s=%v", factRecord.Name, factRecord.Value)),
				})
				seenKinds[havenMemoryObjectKindExplicitFact] = struct{}{}
				break
			}
		}
		for _, itemOp := range distillateRecord.UnresolvedItemOps {
			if strings.TrimSpace(itemOp.Text) == "" {
				continue
			}
			summaryCandidates = append(summaryCandidates, havenMemorySummaryCandidate{
				ObjectKind: havenMemoryObjectKindTask,
				Summary:    redactedWakeSummary(itemOp.Text),
			})
			seenKinds[havenMemoryObjectKindTask] = struct{}{}
			break
		}
		for _, goalOp := range distillateRecord.GoalOps {
			if strings.TrimSpace(goalOp.Text) == "" {
				continue
			}
			summaryCandidates = append(summaryCandidates, havenMemorySummaryCandidate{
				ObjectKind: havenMemoryObjectKindGoal,
				Summary:    redactedWakeSummary(goalOp.Text),
			})
			seenKinds[havenMemoryObjectKindGoal] = struct{}{}
			break
		}
	}

	if len(summaryCandidates) > 0 {
		selectedCandidate := summaryCandidates[0]
		if len(seenKinds) == 1 {
			return selectedCandidate.ObjectKind, selectedCandidate.Summary
		}
		return havenMemoryObjectKindMixed, selectedCandidate.Summary
	}

	switch inspectionRecord.DerivationOutcome {
	case continuityInspectionOutcomeSkippedThreshold:
		return havenMemoryObjectKindInspection, "skipped continuity distillation under threshold"
	case continuityInspectionOutcomeNoArtifacts:
		return havenMemoryObjectKindInspection, "no retained continuity artifacts"
	}

	goalFamilyID := firstDerivedGoalFamilyID(currentState, inspectionRecord)
	if strings.TrimSpace(goalFamilyID) != "" {
		return havenMemoryObjectKindInspection, redactedWakeSummary(goalFamilyID)
	}
	if strings.TrimSpace(inspectionRecord.ThreadID) != "" {
		return havenMemoryObjectKindInspection, redactedWakeSummary("thread " + inspectionRecord.ThreadID)
	}
	return havenMemoryObjectKindInspection, ""
}

type havenMemorySummaryCandidate struct {
	ObjectKind string
	Summary    string
}

func derivedDistillatesForInspection(currentState continuityMemoryState, inspectionRecord continuityInspectionRecord) []continuityDistillateRecord {
	derivedDistillates := make([]continuityDistillateRecord, 0, len(inspectionRecord.DerivedDistillateIDs))
	for _, distillateID := range inspectionRecord.DerivedDistillateIDs {
		distillateRecord, found := currentState.Distillates[distillateID]
		if !found {
			continue
		}
		derivedDistillates = append(derivedDistillates, cloneContinuityDistillateRecord(distillateRecord))
	}
	sort.Slice(derivedDistillates, func(leftIndex int, rightIndex int) bool {
		leftRecord := derivedDistillates[leftIndex]
		rightRecord := derivedDistillates[rightIndex]
		switch {
		case leftRecord.CreatedAtUTC != rightRecord.CreatedAtUTC:
			return leftRecord.CreatedAtUTC < rightRecord.CreatedAtUTC
		default:
			return leftRecord.DistillateID < rightRecord.DistillateID
		}
	})
	return derivedDistillates
}

func firstNonEmptyMemoryTimestamp(values ...string) string {
	for _, candidateValue := range values {
		if strings.TrimSpace(candidateValue) != "" {
			return candidateValue
		}
	}
	return ""
}

func (diagnosticReport continuityDiagnosticWakeReport) EntryCount() int {
	return len(diagnosticReport.Entries)
}

func (server *Server) resetHavenMemory(tokenClaims capabilityToken, rawRequest HavenMemoryResetRequest) (HavenMemoryResetResponse, error) {
	validatedRequest := rawRequest
	if err := validatedRequest.Validate(); err != nil {
		return HavenMemoryResetResponse{}, err
	}

	server.memoryMu.Lock()
	defer server.memoryMu.Unlock()

	partition, err := server.ensureMemoryPartitionLocked(tokenClaims.TenantID)
	if err != nil {
		return HavenMemoryResetResponse{}, err
	}

	previousState := cloneContinuityMemoryState(partition.state)
	nowUTC := server.now().UTC()
	preparedState := newEmptyContinuityMemoryState()
	preparedState.WakeState, preparedState.DiagnosticWake = buildLoopgateWakeProducts(preparedState, nowUTC, server.runtimeConfig)

	archiveID := buildHavenMemoryArchiveID(validatedRequest.OperationID, nowUTC)
	archivePath := filepath.Join(server.havenMemoryArchiveRoot(), archiveID)
	hadArchivedRoot := false

	if err := closeMemoryBackend(partition.backend); err != nil {
		return HavenMemoryResetResponse{}, fmt.Errorf("close memory backend before reset: %w", err)
	}
	partition.backend = nil

	partitionRoot := partition.rootPath
	if _, statErr := os.Stat(partitionRoot); statErr == nil {
		if err := os.MkdirAll(server.havenMemoryArchiveRoot(), 0o700); err != nil {
			if rollbackErr := server.restorePartitionResetMemoryStateLocked(partition, previousState, false, ""); rollbackErr != nil {
				return HavenMemoryResetResponse{}, fmt.Errorf("prepare memory archive root: %w (rollback failed: %v)", err, rollbackErr)
			}
			return HavenMemoryResetResponse{}, fmt.Errorf("prepare memory archive root: %w", err)
		}
		if err := os.Rename(partitionRoot, archivePath); err != nil {
			if rollbackErr := server.restorePartitionResetMemoryStateLocked(partition, previousState, false, ""); rollbackErr != nil {
				return HavenMemoryResetResponse{}, fmt.Errorf("archive current memory partition root: %w (rollback failed: %v)", err, rollbackErr)
			}
			return HavenMemoryResetResponse{}, fmt.Errorf("archive current memory partition root: %w", err)
		}
		hadArchivedRoot = true
	} else if !os.IsNotExist(statErr) {
		if rollbackErr := server.restorePartitionResetMemoryStateLocked(partition, previousState, false, ""); rollbackErr != nil {
			return HavenMemoryResetResponse{}, fmt.Errorf("stat memory partition root before reset: %w (rollback failed: %v)", statErr, rollbackErr)
		}
		return HavenMemoryResetResponse{}, fmt.Errorf("stat memory partition root before reset: %w", statErr)
	}

	if err := os.MkdirAll(partitionRoot, 0o700); err != nil {
		if rollbackErr := server.restorePartitionResetMemoryStateLocked(partition, previousState, hadArchivedRoot, archivePath); rollbackErr != nil {
			return HavenMemoryResetResponse{}, fmt.Errorf("recreate memory partition root: %w (rollback failed: %v)", err, rollbackErr)
		}
		return HavenMemoryResetResponse{}, fmt.Errorf("recreate memory partition root: %w", err)
	}

	if err := server.saveMemoryState(partitionRoot, preparedState, server.runtimeConfig); err != nil {
		if rollbackErr := server.restorePartitionResetMemoryStateLocked(partition, previousState, hadArchivedRoot, archivePath); rollbackErr != nil {
			return HavenMemoryResetResponse{}, fmt.Errorf("write fresh memory state: %w (rollback failed: %v)", err, rollbackErr)
		}
		return HavenMemoryResetResponse{}, fmt.Errorf("write fresh memory state: %w", err)
	}

	preparedBackend, err := newMemoryBackendForPartition(server, partition)
	if err != nil {
		if rollbackErr := server.restorePartitionResetMemoryStateLocked(partition, previousState, hadArchivedRoot, archivePath); rollbackErr != nil {
			return HavenMemoryResetResponse{}, fmt.Errorf("reopen memory backend after reset: %w (rollback failed: %v)", err, rollbackErr)
		}
		return HavenMemoryResetResponse{}, fmt.Errorf("reopen memory backend after reset: %w", err)
	}
	if err := preparedBackend.SyncAuthoritativeState(context.Background(), preparedState); err != nil {
		_ = closeMemoryBackend(preparedBackend)
		if rollbackErr := server.restorePartitionResetMemoryStateLocked(partition, previousState, hadArchivedRoot, archivePath); rollbackErr != nil {
			return HavenMemoryResetResponse{}, fmt.Errorf("sync memory backend after reset: %w (rollback failed: %v)", err, rollbackErr)
		}
		return HavenMemoryResetResponse{}, fmt.Errorf("sync memory backend after reset: %w", err)
	}

	auditData := map[string]interface{}{
		"operation_id":                validatedRequest.OperationID,
		"reason":                      secrets.RedactText(strings.TrimSpace(validatedRequest.Reason)),
		"archive_id":                  archiveID,
		"memory_partition_key":        partition.partitionKey,
		"previous_inspection_count":   len(previousState.Inspections),
		"previous_distillate_count":   len(previousState.Distillates),
		"previous_resonate_key_count": len(previousState.ResonateKeys),
	}
	if err := server.logEvent("memory.reset", tokenClaims.ControlSessionID, auditData); err != nil {
		_ = closeMemoryBackend(preparedBackend)
		if rollbackErr := server.restorePartitionResetMemoryStateLocked(partition, previousState, hadArchivedRoot, archivePath); rollbackErr != nil {
			return HavenMemoryResetResponse{}, fmt.Errorf("audit memory reset: %w (rollback failed: %v)", err, rollbackErr)
		}
		return HavenMemoryResetResponse{}, continuityGovernanceError{
			httpStatus:     http.StatusServiceUnavailable,
			responseStatus: ResponseStatusError,
			denialCode:     DenialCodeAuditUnavailable,
			reason:         "control-plane audit is unavailable",
		}
	}

	partition.backend = preparedBackend
	partition.state = canonicalizeContinuityMemoryState(preparedState)
	delete(server.memoryFactWritesBySession, tokenClaims.ControlSessionID)
	delete(server.memoryFactWritesByUID, tokenClaims.PeerIdentity.UID)

	return HavenMemoryResetResponse{
		ResetAtUTC:               nowUTC.Format(time.RFC3339Nano),
		ArchiveID:                archiveID,
		PreviousInspectionCount:  len(previousState.Inspections),
		PreviousDistillateCount:  len(previousState.Distillates),
		PreviousResonateKeyCount: len(previousState.ResonateKeys),
		WakeStateID:              preparedState.WakeState.ID,
	}, nil
}

func (server *Server) restorePartitionResetMemoryStateLocked(partition *memoryPartition, previousState continuityMemoryState, hadArchivedRoot bool, archivePath string) error {
	if removeErr := os.RemoveAll(partition.rootPath); removeErr != nil && !os.IsNotExist(removeErr) {
		return fmt.Errorf("remove fresh memory partition root during rollback: %w", removeErr)
	}
	if hadArchivedRoot {
		if err := os.Rename(archivePath, partition.rootPath); err != nil {
			return fmt.Errorf("restore archived memory partition root: %w", err)
		}
	} else if _, statErr := os.Stat(partition.rootPath); os.IsNotExist(statErr) {
		if mkErr := os.MkdirAll(partition.rootPath, 0o700); mkErr != nil {
			return fmt.Errorf("recreate memory partition root during rollback: %w", mkErr)
		}
	}
	_ = closeMemoryBackend(partition.backend)
	partition.backend = nil
	restoredBackend, err := newMemoryBackendForPartition(server, partition)
	if err != nil {
		return fmt.Errorf("reopen memory backend during rollback: %w", err)
	}
	if err := restoredBackend.SyncAuthoritativeState(context.Background(), previousState); err != nil {
		_ = closeMemoryBackend(restoredBackend)
		return fmt.Errorf("sync memory backend during rollback: %w", err)
	}
	partition.backend = restoredBackend
	partition.state = canonicalizeContinuityMemoryState(previousState)
	return nil
}

func (server *Server) havenMemoryArchiveRoot() string {
	return filepath.Join(server.repoRoot, "runtime", "state", "memory_archives")
}

func buildHavenMemoryArchiveID(operationID string, nowUTC time.Time) string {
	return "memory_reset_" + nowUTC.Format("20060102T150405.000000000Z0700") + "_" + operationID
}

func closeMemoryBackend(memoryBackend MemoryBackend) error {
	switch typedBackend := memoryBackend.(type) {
	case nil:
		return nil
	case *continuityTCLMemoryBackend:
		if typedBackend.store == nil || typedBackend.store.database == nil {
			return nil
		}
		return typedBackend.store.database.Close()
	default:
		return fmt.Errorf("memory backend close is not implemented for %T", memoryBackend)
	}
}
