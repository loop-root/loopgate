package loopgate

import (
	"strings"
	"time"

	"loopgate/internal/secrets"
)

func (backend *continuityTCLMemoryBackend) reviewContinuityInspectionAuthoritatively(authenticatedSession capabilityToken, inspectionID string, rawRequest MemoryInspectionReviewRequest) (MemoryInspectionGovernanceResponse, error) {
	validatedRequest := rawRequest
	if err := validatedRequest.Validate(); err != nil {
		return MemoryInspectionGovernanceResponse{}, err
	}

	var governanceResponse MemoryInspectionGovernanceResponse
	// Use the backend's bound tenant after the partition check at the backend seam
	// so governance mutations cannot drift back toward token-trust shortcuts.
	err := backend.server.mutateContinuityMemory(backend.partition.tenantID, authenticatedSession.ControlSessionID, "memory.continuity.reviewed", func(workingState continuityMemoryState, nowUTC time.Time) (continuityMemoryState, map[string]interface{}, continuityMutationEvents, error) {
		inspectionRecord, found := workingState.Inspections[inspectionID]
		if !found {
			return workingState, nil, continuityMutationEvents{}, continuityGovernanceError{
				httpStatus:     404,
				responseStatus: ResponseStatusDenied,
				denialCode:     DenialCodeContinuityInspectionNotFound,
				reason:         "continuity inspection not found",
			}
		}
		inspectionRecord = normalizeContinuityInspectionRecordMust(inspectionRecord)
		targetStatus := strings.TrimSpace(validatedRequest.Decision)
		if inspectionRecord.Review.Status == targetStatus {
			governanceResponse = buildMemoryInspectionGovernanceResponse(inspectionRecord)
			return workingState, nil, continuityMutationEvents{}, nil
		}
		if inspectionRecord.Review.Status != continuityReviewStatusPendingReview {
			return workingState, nil, continuityMutationEvents{}, continuityGovernanceError{
				httpStatus:     409,
				responseStatus: ResponseStatusDenied,
				denialCode:     DenialCodeContinuityGovernanceStateConflict,
				reason:         "continuity review is already terminal",
			}
		}
		inspectionRecord.Review = continuityInspectionReview{
			Status:         targetStatus,
			DecisionSource: continuityReviewDecisionSourceOperator,
			ReviewedAtUTC:  nowUTC.Format(time.RFC3339Nano),
			Reason:         secrets.RedactText(strings.TrimSpace(validatedRequest.Reason)),
			OperationID:    validatedRequest.OperationID,
		}
		inspectionRecord.Outcome = inspectionRecord.DerivationOutcome
		workingState.Inspections[inspectionID] = inspectionRecord
		governanceResponse = buildMemoryInspectionGovernanceResponse(inspectionRecord)
		mutationEvents := continuityMutationEvents{
			Continuity: []continuityAuthoritativeEvent{{
				SchemaVersion: continuityMemorySchemaVersion,
				EventID:       "continuity_review_" + validatedRequest.OperationID,
				EventType:     "continuity_inspection_reviewed",
				CreatedAtUTC:  nowUTC.Format(time.RFC3339Nano),
				Actor:         authenticatedSession.ControlSessionID,
				Scope:         inspectionRecord.Scope,
				InspectionID:  inspectionRecord.InspectionID,
				ThreadID:      inspectionRecord.ThreadID,
				Review:        ptrContinuityInspectionReview(inspectionRecord.Review),
			}},
			Profile: []continuityProfileEvent{{
				SchemaVersion: continuityMemorySchemaVersion,
				EventID:       "profile_review_" + validatedRequest.OperationID,
				EventType:     "continuity_review_recorded",
				CreatedAtUTC:  nowUTC.Format(time.RFC3339Nano),
				Actor:         authenticatedSession.ControlSessionID,
				InspectionID:  inspectionRecord.InspectionID,
				ThreadID:      inspectionRecord.ThreadID,
				GoalType:      firstDerivedGoalType(workingState, inspectionRecord),
				GoalFamilyID:  firstDerivedGoalFamilyID(workingState, inspectionRecord),
				Review:        ptrContinuityInspectionReview(inspectionRecord.Review),
			}},
		}
		return workingState, map[string]interface{}{
			"inspection_id":      inspectionRecord.InspectionID,
			"thread_id":          inspectionRecord.ThreadID,
			"review_status":      inspectionRecord.Review.Status,
			"lineage_status":     inspectionRecord.Lineage.Status,
			"derivation_outcome": inspectionRecord.DerivationOutcome,
			"operation_id":       validatedRequest.OperationID,
			"reason":             secrets.RedactText(strings.TrimSpace(validatedRequest.Reason)),
		}, mutationEvents, nil
	})
	if err != nil {
		return MemoryInspectionGovernanceResponse{}, err
	}
	return governanceResponse, nil
}

func (backend *continuityTCLMemoryBackend) updateContinuityLineageStatusAuthoritatively(authenticatedSession capabilityToken, inspectionID string, targetStatus string, rawRequest MemoryInspectionLineageRequest, auditEventType string) (MemoryInspectionGovernanceResponse, error) {
	validatedRequest := rawRequest
	if err := validatedRequest.Validate(); err != nil {
		return MemoryInspectionGovernanceResponse{}, err
	}

	var governanceResponse MemoryInspectionGovernanceResponse
	// Keep lineage mutations on the same backend-owned tenant binding as remember
	// and inspect so review and purge cannot drift into a parallel authority path.
	err := backend.server.mutateContinuityMemory(backend.partition.tenantID, authenticatedSession.ControlSessionID, auditEventType, func(workingState continuityMemoryState, nowUTC time.Time) (continuityMemoryState, map[string]interface{}, continuityMutationEvents, error) {
		inspectionRecord, found := workingState.Inspections[inspectionID]
		if !found {
			return workingState, nil, continuityMutationEvents{}, continuityGovernanceError{
				httpStatus:     404,
				responseStatus: ResponseStatusDenied,
				denialCode:     DenialCodeContinuityInspectionNotFound,
				reason:         "continuity inspection not found",
			}
		}
		inspectionRecord = normalizeContinuityInspectionRecordMust(inspectionRecord)
		if inspectionRecord.Lineage.Status == targetStatus {
			governanceResponse = buildMemoryInspectionGovernanceResponse(inspectionRecord)
			return workingState, nil, continuityMutationEvents{}, nil
		}
		if inspectionRecord.Lineage.Status == continuityLineageStatusPurged {
			return workingState, nil, continuityMutationEvents{}, continuityGovernanceError{
				httpStatus:     409,
				responseStatus: ResponseStatusDenied,
				denialCode:     DenialCodeContinuityGovernanceStateConflict,
				reason:         "purged continuity lineage is terminal",
			}
		}
		if inspectionRecord.Lineage.Status == continuityLineageStatusTombstoned && targetStatus == continuityLineageStatusEligible {
			return workingState, nil, continuityMutationEvents{}, continuityGovernanceError{
				httpStatus:     409,
				responseStatus: ResponseStatusDenied,
				denialCode:     DenialCodeContinuityGovernanceStateConflict,
				reason:         "tombstoned continuity lineage cannot become eligible",
			}
		}
		if targetStatus == continuityLineageStatusPurged && inspectionRecord.Lineage.Status == continuityLineageStatusTombstoned && continuitySupersessionRetentionActive(inspectionRecord.Lineage, nowUTC) {
			return workingState, nil, continuityMutationEvents{}, continuityGovernanceError{
				httpStatus:     409,
				responseStatus: ResponseStatusDenied,
				denialCode:     DenialCodeContinuityRetentionWindowActive,
				reason:         "superseded continuity lineage remains under retention and cannot be purged yet",
			}
		}
		updatedLineage := inspectionRecord.Lineage
		updatedLineage.Status = targetStatus
		updatedLineage.ChangedAtUTC = nowUTC.Format(time.RFC3339Nano)
		updatedLineage.Reason = secrets.RedactText(strings.TrimSpace(validatedRequest.Reason))
		updatedLineage.OperationID = validatedRequest.OperationID
		inspectionRecord.Lineage = updatedLineage
		inspectionRecord.Outcome = inspectionRecord.DerivationOutcome
		stampContinuityDerivedArtifactsExcluded(&workingState, inspectionRecord, nowUTC)
		workingState.Inspections[inspectionID] = inspectionRecord
		governanceResponse = buildMemoryInspectionGovernanceResponse(inspectionRecord)
		mutationEvents := continuityMutationEvents{
			Continuity: []continuityAuthoritativeEvent{{
				SchemaVersion: continuityMemorySchemaVersion,
				EventID:       "continuity_lineage_" + validatedRequest.OperationID,
				EventType:     "continuity_inspection_lineage_updated",
				CreatedAtUTC:  nowUTC.Format(time.RFC3339Nano),
				Actor:         authenticatedSession.ControlSessionID,
				Scope:         inspectionRecord.Scope,
				InspectionID:  inspectionRecord.InspectionID,
				ThreadID:      inspectionRecord.ThreadID,
				Lineage:       ptrContinuityInspectionLineage(inspectionRecord.Lineage),
			}},
			Profile: []continuityProfileEvent{{
				SchemaVersion: continuityMemorySchemaVersion,
				EventID:       "profile_lineage_" + validatedRequest.OperationID,
				EventType:     "continuity_lineage_recorded",
				CreatedAtUTC:  nowUTC.Format(time.RFC3339Nano),
				Actor:         authenticatedSession.ControlSessionID,
				InspectionID:  inspectionRecord.InspectionID,
				ThreadID:      inspectionRecord.ThreadID,
				GoalType:      firstDerivedGoalType(workingState, inspectionRecord),
				GoalFamilyID:  firstDerivedGoalFamilyID(workingState, inspectionRecord),
				Lineage:       ptrContinuityInspectionLineage(inspectionRecord.Lineage),
			}},
		}
		return workingState, map[string]interface{}{
			"inspection_id":      inspectionRecord.InspectionID,
			"thread_id":          inspectionRecord.ThreadID,
			"review_status":      inspectionRecord.Review.Status,
			"lineage_status":     inspectionRecord.Lineage.Status,
			"derivation_outcome": inspectionRecord.DerivationOutcome,
			"operation_id":       validatedRequest.OperationID,
			"reason":             secrets.RedactText(strings.TrimSpace(validatedRequest.Reason)),
		}, mutationEvents, nil
	})
	if err != nil {
		return MemoryInspectionGovernanceResponse{}, err
	}
	return governanceResponse, nil
}
