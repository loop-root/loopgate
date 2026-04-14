package loopgate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/identifiers"
)

func cloneContinuityInspectionRecord(inspectionRecord continuityInspectionRecord) continuityInspectionRecord {
	inspectionRecord.Outcome = inspectionRecord.DerivationOutcome
	inspectionRecord.DerivedDistillateIDs = append([]string(nil), inspectionRecord.DerivedDistillateIDs...)
	inspectionRecord.DerivedResonateKeyIDs = append([]string(nil), inspectionRecord.DerivedResonateKeyIDs...)
	return inspectionRecord
}

func inspectionLineageSelectionDecision(currentState continuityMemoryState, inspectionID string) continuityEligibilityDecision {
	inspectionRecord, found := currentState.Inspections[inspectionID]
	if !found {
		return continuityEligibilityDecision{
			Allowed:    false,
			DenialCode: DenialCodeContinuityInspectionNotFound,
		}
	}
	inspectionRecord = normalizeContinuityInspectionRecordMust(inspectionRecord)
	if inspectionRecord.DerivationOutcome != continuityInspectionOutcomeDerived {
		return continuityEligibilityDecision{
			Allowed:           false,
			DenialCode:        DenialCodeContinuityLineageIneligible,
			ReviewStatus:      inspectionRecord.Review.Status,
			LineageStatus:     inspectionRecord.Lineage.Status,
			DerivationOutcome: inspectionRecord.DerivationOutcome,
		}
	}
	if inspectionRecord.Review.Status != continuityReviewStatusAccepted {
		return continuityEligibilityDecision{
			Allowed:           false,
			DenialCode:        DenialCodeContinuityLineageIneligible,
			ReviewStatus:      inspectionRecord.Review.Status,
			LineageStatus:     inspectionRecord.Lineage.Status,
			DerivationOutcome: inspectionRecord.DerivationOutcome,
		}
	}
	if inspectionRecord.Lineage.Status != continuityLineageStatusEligible {
		return continuityEligibilityDecision{
			Allowed:           false,
			DenialCode:        DenialCodeContinuityLineageIneligible,
			ReviewStatus:      inspectionRecord.Review.Status,
			LineageStatus:     inspectionRecord.Lineage.Status,
			DerivationOutcome: inspectionRecord.DerivationOutcome,
		}
	}
	return continuityEligibilityDecision{
		Allowed:           true,
		ReviewStatus:      inspectionRecord.Review.Status,
		LineageStatus:     inspectionRecord.Lineage.Status,
		DerivationOutcome: inspectionRecord.DerivationOutcome,
	}
}

func (server *Server) inspectionLineageSelectionDecisionLocked(currentState continuityMemoryState, inspectionID string) continuityEligibilityDecision {
	return inspectionLineageSelectionDecision(currentState, inspectionID)
}

func buildMemoryInspectionGovernanceResponse(inspectionRecord continuityInspectionRecord) MemoryInspectionGovernanceResponse {
	return MemoryInspectionGovernanceResponse{
		InspectionID:          inspectionRecord.InspectionID,
		ThreadID:              inspectionRecord.ThreadID,
		DerivationOutcome:     inspectionRecord.DerivationOutcome,
		ReviewStatus:          inspectionRecord.Review.Status,
		LineageStatus:         inspectionRecord.Lineage.Status,
		DerivedDistillateIDs:  append([]string(nil), inspectionRecord.DerivedDistillateIDs...),
		DerivedResonateKeyIDs: append([]string(nil), inspectionRecord.DerivedResonateKeyIDs...),
	}
}

func (server *Server) reviewContinuityInspection(tokenClaims capabilityToken, inspectionID string, rawRequest MemoryInspectionReviewRequest) (MemoryInspectionGovernanceResponse, error) {
	backend, err := server.memoryBackendForTenant(tokenClaims.TenantID)
	if err != nil {
		return MemoryInspectionGovernanceResponse{}, err
	}
	return backend.ReviewContinuityInspection(context.Background(), tokenClaims, inspectionID, rawRequest)
}

func (server *Server) tombstoneContinuityInspection(tokenClaims capabilityToken, inspectionID string, rawRequest MemoryInspectionLineageRequest) (MemoryInspectionGovernanceResponse, error) {
	backend, err := server.memoryBackendForTenant(tokenClaims.TenantID)
	if err != nil {
		return MemoryInspectionGovernanceResponse{}, err
	}
	return backend.TombstoneContinuityInspection(context.Background(), tokenClaims, inspectionID, rawRequest)
}

func (server *Server) purgeContinuityInspection(tokenClaims capabilityToken, inspectionID string, rawRequest MemoryInspectionLineageRequest) (MemoryInspectionGovernanceResponse, error) {
	backend, err := server.memoryBackendForTenant(tokenClaims.TenantID)
	if err != nil {
		return MemoryInspectionGovernanceResponse{}, err
	}
	return backend.PurgeContinuityInspection(context.Background(), tokenClaims, inspectionID, rawRequest)
}

func continuitySupersessionRetentionActive(currentLineage continuityInspectionLineage, nowUTC time.Time) bool {
	if strings.TrimSpace(currentLineage.SupersededByInspectionID) == "" {
		return false
	}
	changedAtUTC := strings.TrimSpace(currentLineage.ChangedAtUTC)
	if changedAtUTC == "" {
		return true
	}
	supersededAtUTC, err := time.Parse(time.RFC3339Nano, changedAtUTC)
	if err != nil {
		return true
	}
	return nowUTC.Before(supersededAtUTC.Add(config.DefaultSupersededLineageRetentionWindow))
}

func stampContinuityDerivedArtifactsExcluded(workingState *continuityMemoryState, inspectionRecord continuityInspectionRecord, changedAt time.Time) {
	stampedAtUTC := changedAt.UTC().Format(time.RFC3339Nano)
	for _, distillateID := range inspectionRecord.DerivedDistillateIDs {
		distillateRecord, found := workingState.Distillates[distillateID]
		if !found || strings.TrimSpace(distillateRecord.TombstonedAtUTC) != "" {
			continue
		}
		distillateRecord.TombstonedAtUTC = stampedAtUTC
		workingState.Distillates[distillateID] = distillateRecord
	}
	for _, keyID := range inspectionRecord.DerivedResonateKeyIDs {
		resonateKeyRecord, found := workingState.ResonateKeys[keyID]
		if !found || strings.TrimSpace(resonateKeyRecord.TombstonedAtUTC) != "" {
			continue
		}
		resonateKeyRecord.TombstonedAtUTC = stampedAtUTC
		workingState.ResonateKeys[keyID] = resonateKeyRecord
	}
}

func continuityMemoryStatesEqual(leftState continuityMemoryState, rightState continuityMemoryState) bool {
	leftBytes, leftErr := json.Marshal(leftState)
	rightBytes, rightErr := json.Marshal(rightState)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return bytes.Equal(leftBytes, rightBytes)
}

func normalizeContinuityInspectionRecord(inspectionRecord continuityInspectionRecord) (continuityInspectionRecord, error) {
	normalizedRecord := cloneContinuityInspectionRecord(inspectionRecord)
	if strings.TrimSpace(normalizedRecord.DerivationOutcome) == "" {
		normalizedRecord.DerivationOutcome = strings.TrimSpace(normalizedRecord.Outcome)
	}
	if strings.TrimSpace(normalizedRecord.DerivationOutcome) == "" {
		normalizedRecord.DerivationOutcome = continuityInspectionOutcomeNoArtifacts
	}
	if strings.TrimSpace(normalizedRecord.Review.Status) == "" {
		normalizedRecord.Review.Status = continuityReviewStatusAccepted
	}
	if strings.TrimSpace(normalizedRecord.Lineage.Status) == "" {
		normalizedRecord.Lineage.Status = continuityLineageStatusEligible
	}
	normalizedRecord.Outcome = normalizedRecord.DerivationOutcome
	if err := validateContinuityInspectionRecord(normalizedRecord); err != nil {
		return continuityInspectionRecord{}, err
	}
	return normalizedRecord, nil
}

func normalizeContinuityInspectionRecordMust(inspectionRecord continuityInspectionRecord) continuityInspectionRecord {
	normalizedRecord, err := normalizeContinuityInspectionRecord(inspectionRecord)
	if err != nil {
		return inspectionRecord
	}
	return normalizedRecord
}

func validateContinuityInspectionRecord(inspectionRecord continuityInspectionRecord) error {
	switch inspectionRecord.DerivationOutcome {
	case continuityInspectionOutcomeSkippedThreshold, continuityInspectionOutcomeNoArtifacts, continuityInspectionOutcomeDerived:
	default:
		return fmt.Errorf("invalid derivation_outcome %q", inspectionRecord.DerivationOutcome)
	}
	switch inspectionRecord.Review.Status {
	case continuityReviewStatusPendingReview, continuityReviewStatusAccepted, continuityReviewStatusRejected:
	default:
		return fmt.Errorf("invalid review status %q", inspectionRecord.Review.Status)
	}
	switch inspectionRecord.Lineage.Status {
	case continuityLineageStatusEligible, continuityLineageStatusTombstoned, continuityLineageStatusPurged:
	default:
		return fmt.Errorf("invalid lineage status %q", inspectionRecord.Lineage.Status)
	}
	if strings.TrimSpace(inspectionRecord.Review.ReviewedAtUTC) != "" {
		if _, err := time.Parse(time.RFC3339Nano, inspectionRecord.Review.ReviewedAtUTC); err != nil {
			return fmt.Errorf("reviewed_at_utc is invalid: %w", err)
		}
	}
	if strings.TrimSpace(inspectionRecord.Lineage.ChangedAtUTC) != "" {
		if _, err := time.Parse(time.RFC3339Nano, inspectionRecord.Lineage.ChangedAtUTC); err != nil {
			return fmt.Errorf("lineage changed_at_utc is invalid: %w", err)
		}
	}
	if strings.TrimSpace(inspectionRecord.Review.OperationID) != "" {
		if err := identifiers.ValidateSafeIdentifier("review operation_id", inspectionRecord.Review.OperationID); err != nil {
			return err
		}
	}
	if strings.TrimSpace(inspectionRecord.Lineage.OperationID) != "" {
		if err := identifiers.ValidateSafeIdentifier("lineage operation_id", inspectionRecord.Lineage.OperationID); err != nil {
			return err
		}
	}
	if inspectionRecord.Review.Status == continuityReviewStatusPendingReview && strings.TrimSpace(inspectionRecord.Review.ReviewedAtUTC) != "" {
		return fmt.Errorf("pending_review must not set reviewed_at_utc")
	}
	return nil
}
