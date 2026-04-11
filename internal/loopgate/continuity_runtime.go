package loopgate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"morph/internal/config"
	"morph/internal/secrets"
)

func normalizeGoalSummarySlug(rawGoalSummary string) string {
	lowerGoalSummary := strings.ToLower(strings.TrimSpace(rawGoalSummary))
	if lowerGoalSummary == "" {
		return "empty_goal"
	}
	var slugBuilder strings.Builder
	lastWasUnderscore := false
	for _, goalRune := range lowerGoalSummary {
		switch {
		case goalRune >= 'a' && goalRune <= 'z':
			slugBuilder.WriteRune(goalRune)
			lastWasUnderscore = false
		case goalRune >= '0' && goalRune <= '9':
			slugBuilder.WriteRune(goalRune)
			lastWasUnderscore = false
		default:
			if !lastWasUnderscore {
				slugBuilder.WriteRune('_')
				lastWasUnderscore = true
			}
		}
	}
	slugValue := strings.Trim(slugBuilder.String(), "_")
	if slugValue == "" {
		return "empty_goal"
	}
	return slugValue
}

func normalizeGoalFamily(goalSummary string, goalAliases config.GoalAliases) continuityGoalNormalization {
	normalizedSummarySlug := normalizeGoalSummarySlug(goalSummary)
	lowerGoalSummary := strings.ToLower(strings.TrimSpace(goalSummary))

	for _, goalType := range config.GoalTypeKeys(goalAliases) {
		for _, rawAlias := range goalAliases.Aliases[goalType] {
			normalizedAlias := config.NormalizeGoalAliasPublic(rawAlias)
			aliasNeedle := strings.ReplaceAll(normalizedAlias, "_", " ")
			if strings.Contains(lowerGoalSummary, aliasNeedle) || normalizedSummarySlug == normalizedAlias {
				return continuityGoalNormalization{
					GoalType:             goalType,
					GoalFamilyID:         goalType + ":" + normalizedAlias,
					NormalizationVersion: continuityNormalizationVersion,
					AliasMatched:         true,
					AliasKey:             normalizedAlias,
				}
			}
		}
	}

	switch {
	case strings.Contains(lowerGoalSummary, "security") || strings.Contains(lowerGoalSummary, "hardening") || strings.Contains(lowerGoalSummary, "threat"):
		return fallbackGoalNormalization(goalTypeSecurityHardening, normalizedSummarySlug)
	case strings.Contains(lowerGoalSummary, "architecture") || strings.Contains(lowerGoalSummary, "system design") || strings.Contains(lowerGoalSummary, "design"):
		return fallbackGoalNormalization(goalTypeArchitecturePlanning, normalizedSummarySlug)
	case strings.Contains(lowerGoalSummary, "readme") || strings.Contains(lowerGoalSummary, "docs") || strings.Contains(lowerGoalSummary, "documentation") || strings.Contains(lowerGoalSummary, "rfc edit"):
		return fallbackGoalNormalization(goalTypeDocumentationUpdate, normalizedSummarySlug)
	case strings.Contains(lowerGoalSummary, "bug") || strings.Contains(lowerGoalSummary, "incident") || strings.Contains(lowerGoalSummary, "debug") || strings.Contains(lowerGoalSummary, "failure"):
		return fallbackGoalNormalization(goalTypeDebuggingInvestigation, normalizedSummarySlug)
	case strings.Contains(lowerGoalSummary, "deadline") || strings.Contains(lowerGoalSummary, "calendar") || strings.Contains(lowerGoalSummary, "reminder"):
		return fallbackGoalNormalization(goalTypeSchedulingCommitment, normalizedSummarySlug)
	case strings.Contains(lowerGoalSummary, "review") || strings.Contains(lowerGoalSummary, "design doc") || strings.Contains(lowerGoalSummary, "rfc"):
		return fallbackGoalNormalization(goalTypeTechnicalReview, normalizedSummarySlug)
	case strings.Contains(lowerGoalSummary, "research") || strings.Contains(lowerGoalSummary, "option") || strings.Contains(lowerGoalSummary, "recommendation") || strings.Contains(lowerGoalSummary, "summary"):
		return fallbackGoalNormalization(goalTypeResearchSynthesis, normalizedSummarySlug)
	case strings.Contains(lowerGoalSummary, "config") || strings.Contains(lowerGoalSummary, "preference") || strings.Contains(lowerGoalSummary, "setup"):
		return fallbackGoalNormalization(goalTypePreferenceOrConfigUpdate, normalizedSummarySlug)
	case strings.Contains(lowerGoalSummary, "implement") || strings.Contains(lowerGoalSummary, "change"):
		return fallbackGoalNormalization(goalTypeImplementationChange, normalizedSummarySlug)
	default:
		return fallbackGoalNormalization(goalTypeWorkflowFollowup, normalizedSummarySlug)
	}
}

func fallbackGoalNormalization(goalType string, summarySlug string) continuityGoalNormalization {
	return continuityGoalNormalization{
		GoalType:             goalType,
		GoalFamilyID:         goalType + ":fallback_" + summarySlug,
		NormalizationVersion: continuityNormalizationVersion,
		NeedsAliasCuration:   true,
	}
}

func redactedWakeSummary(rawValue string) string {
	trimmedValue := secrets.RedactText(strings.TrimSpace(rawValue))
	if trimmedValue == "" {
		return ""
	}
	if len(trimmedValue) > 96 {
		trimmedValue = trimmedValue[:96]
	}
	trimmedValue = strings.ReplaceAll(trimmedValue, "\n", " ")
	return trimmedValue
}

func itemKindID(itemKind string, itemID string) string {
	return itemKind + ":" + itemID
}

func stableWakeEntryLess(leftEntry continuityDiagnosticWakeEntry, rightEntry continuityDiagnosticWakeEntry) bool {
	if leftEntry.ItemKind != rightEntry.ItemKind {
		return leftEntry.ItemKind < rightEntry.ItemKind
	}
	return leftEntry.ItemID < rightEntry.ItemID
}

func hashStableJSONIdentifier(payload interface{}) string {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	payloadHash := sha256.Sum256(payloadBytes)
	return hex.EncodeToString(payloadHash[:8])
}

func runtimeConfigToResolvedProfile(runtimeConfig config.RuntimeConfig) continuityResolvedProfileConfig {
	return continuityResolvedProfileConfig{
		CandidatePanelSize:       runtimeConfig.Memory.CandidatePanelSize,
		DecompositionPreference:  runtimeConfig.Memory.DecompositionPreference,
		ReviewPreference:         runtimeConfig.Memory.ReviewPreference,
		SoftMorphlingConcurrency: runtimeConfig.Memory.SoftMorphlingConcurrency,
		BatchingPreference:       runtimeConfig.Memory.BatchingPreference,
	}
}

func runtimeConfigCorrections(runtimeConfig config.RuntimeConfig, nowUTC time.Time) []continuityCorrectionRecord {
	correctionRecords := make([]continuityCorrectionRecord, 0, len(runtimeConfig.Memory.Corrections))
	for _, rawCorrection := range runtimeConfig.Memory.Corrections {
		createdAtUTC := strings.TrimSpace(rawCorrection.CreatedAtUTC)
		correctionID := strings.TrimSpace(rawCorrection.ID)
		if correctionID == "" {
			correctionID = makeEventPayloadID("corr", rawCorrection)
		}
		activeState := correctionStateActive
		if strings.TrimSpace(rawCorrection.StrengthClass) == correctionStrengthPreference {
			referenceTime := parseTimeOrZero(createdAtUTC)
			if reviewTime := parseTimeOrZero(rawCorrection.ReviewAtUTC); !reviewTime.IsZero() {
				referenceTime = reviewTime
			}
			if !referenceTime.IsZero() && nowUTC.Sub(referenceTime) > 90*24*time.Hour {
				activeState = correctionStateInactive
			}
		}
		correctionRecords = append(correctionRecords, continuityCorrectionRecord{
			SchemaVersion:           continuityMemorySchemaVersion,
			CorrectionID:            correctionID,
			Type:                    strings.TrimSpace(rawCorrection.Type),
			Scope:                   strings.TrimSpace(rawCorrection.Scope),
			GoalType:                strings.TrimSpace(rawCorrection.GoalType),
			GoalFamilyID:            strings.TrimSpace(rawCorrection.GoalFamilyID),
			TargetRuleClass:         strings.TrimSpace(rawCorrection.TargetRuleClass),
			TargetDerivationStage:   strings.TrimSpace(rawCorrection.TargetDerivationStage),
			TargetOutputKind:        strings.TrimSpace(rawCorrection.TargetOutputKind),
			CorrectionStrengthClass: strings.TrimSpace(rawCorrection.StrengthClass),
			Precedence:              100,
			ActiveState:             activeState,
			CreatedAtUTC:            createdAtUTC,
			ReasonSummary:           strings.TrimSpace(rawCorrection.Reason),
			InterceptedProposal:     strings.TrimSpace(rawCorrection.InterceptedProposal),
			InterceptedStage:        strings.TrimSpace(rawCorrection.InterceptedStage),
			ReviewAtUTC:             strings.TrimSpace(rawCorrection.ReviewAtUTC),
		})
	}
	sort.Slice(correctionRecords, func(leftIndex int, rightIndex int) bool {
		return correctionRecords[leftIndex].CorrectionID < correctionRecords[rightIndex].CorrectionID
	})
	return correctionRecords
}

func buildResolvedProfileSnapshot(runtimeConfig config.RuntimeConfig, nowUTC time.Time) continuityResolvedProfileSnapshot {
	activeCorrections := runtimeConfigCorrections(runtimeConfig, nowUTC)
	filteredCorrections := make([]continuityCorrectionRecord, 0, len(activeCorrections))
	for _, correctionRecord := range activeCorrections {
		if correctionRecord.ActiveState == correctionStateActive {
			filteredCorrections = append(filteredCorrections, correctionRecord)
		}
	}
	return continuityResolvedProfileSnapshot{
		SchemaVersion:     continuityMemorySchemaVersion,
		ResolutionVersion: continuityResolutionVersion,
		CreatedAtUTC:      nowUTC.Format(time.RFC3339Nano),
		ExplicitConfig:    runtimeConfigToResolvedProfile(runtimeConfig),
		ActiveCorrections: filteredCorrections,
		ActiveScopedRules: []continuityLearnedRuleRecord{},
		ActiveGlobalRules: []continuityLearnedRuleRecord{},
	}
}

func buildRankingCache(currentState continuityMemoryState, nowUTC time.Time) continuityRankingCache {
	rankingEntries := make([]continuityRankingCacheEntry, 0, len(currentState.Distillates)+len(currentState.ResonateKeys))
	for _, distillateRecord := range currentState.Distillates {
		rankingEntries = append(rankingEntries, continuityRankingCacheEntry{
			ItemKind:         wakeEntryKindDistillate,
			ItemID:           distillateRecord.DistillateID,
			GoalFamilyID:     distillateRecord.GoalFamilyID,
			Scope:            distillateRecord.Scope,
			RetentionScore:   distillateRecord.RetentionScore,
			EffectiveHotness: distillateRecord.EffectiveHotness,
		})
	}
	for _, resonateKeyRecord := range currentState.ResonateKeys {
		rankingEntries = append(rankingEntries, continuityRankingCacheEntry{
			ItemKind:         wakeEntryKindResonateKey,
			ItemID:           resonateKeyRecord.KeyID,
			GoalFamilyID:     resonateKeyRecord.GoalFamilyID,
			Scope:            resonateKeyRecord.Scope,
			RetentionScore:   resonateKeyRecord.RetentionScore,
			EffectiveHotness: resonateKeyRecord.EffectiveHotness,
		})
	}
	sort.Slice(rankingEntries, func(leftIndex int, rightIndex int) bool {
		if rankingEntries[leftIndex].EffectiveHotness != rankingEntries[rightIndex].EffectiveHotness {
			return rankingEntries[leftIndex].EffectiveHotness > rankingEntries[rightIndex].EffectiveHotness
		}
		if rankingEntries[leftIndex].RetentionScore != rankingEntries[rightIndex].RetentionScore {
			return rankingEntries[leftIndex].RetentionScore > rankingEntries[rightIndex].RetentionScore
		}
		return itemKindID(rankingEntries[leftIndex].ItemKind, rankingEntries[leftIndex].ItemID) < itemKindID(rankingEntries[rightIndex].ItemKind, rankingEntries[rightIndex].ItemID)
	})
	return continuityRankingCache{
		SchemaVersion:     continuityMemorySchemaVersion,
		ResolutionVersion: continuityResolutionVersion,
		CreatedAtUTC:      nowUTC.Format(time.RFC3339Nano),
		Entries:           rankingEntries,
	}
}

func buildRevalidationTickets(runtimeConfig config.RuntimeConfig, nowUTC time.Time) []continuityRevalidationTicket {
	correctionRecords := runtimeConfigCorrections(runtimeConfig, nowUTC)
	revalidationTickets := make([]continuityRevalidationTicket, 0, len(correctionRecords))
	for _, correctionRecord := range correctionRecords {
		if correctionRecord.CorrectionStrengthClass != correctionStrengthPreference {
			continue
		}
		referenceTime := parseTimeOrZero(correctionRecord.CreatedAtUTC)
		if reviewTime := parseTimeOrZero(correctionRecord.ReviewAtUTC); !reviewTime.IsZero() {
			referenceTime = reviewTime
		}
		if referenceTime.IsZero() {
			continue
		}
		inactiveAge := nowUTC.Sub(referenceTime)
		if inactiveAge < 60*24*time.Hour {
			continue
		}
		status := revalidationStatusQueued
		if inactiveAge >= 90*24*time.Hour {
			status = revalidationStatusExpired
		}
		revalidationTickets = append(revalidationTickets, continuityRevalidationTicket{
			SchemaVersion: continuityMemorySchemaVersion,
			RevalidationID: makeEventPayloadID("reval", struct {
				CorrectionID string `json:"correction_id"`
				Status       string `json:"status"`
			}{
				CorrectionID: correctionRecord.CorrectionID,
				Status:       status,
			}),
			CorrectionID: correctionRecord.CorrectionID,
			Scope:        correctionRecord.Scope,
			GoalType:     correctionRecord.GoalType,
			GoalFamilyID: correctionRecord.GoalFamilyID,
			CreatedAtUTC: nowUTC.Format(time.RFC3339Nano),
			DueAtUTC:     referenceTime.Add(60 * 24 * time.Hour).Format(time.RFC3339Nano),
			Status:       status,
			Reason:       "preference_correction_inactive_without_reaffirmation",
		})
	}
	sort.Slice(revalidationTickets, func(leftIndex int, rightIndex int) bool {
		return revalidationTickets[leftIndex].CorrectionID < revalidationTickets[rightIndex].CorrectionID
	})
	return revalidationTickets
}

func newDiagnosticWakeReportID(nowUTC time.Time) string {
	return "wake_diag_" + nowUTC.Format("20060102T150405Z")
}

func timeBandKeyFor(rawTimestamp string) string {
	parsedTime := parseTimeOrZero(rawTimestamp)
	if parsedTime.IsZero() {
		return "unknown"
	}
	yearValue, weekValue := parsedTime.ISOWeek()
	return fmt.Sprintf("%04d-%02d", yearValue, weekValue)
}

func buildGoalsCurrentSnapshot(currentState continuityMemoryState) map[string]interface{} {
	goalsByID := map[string]string{}
	for _, distillateRecord := range activeLoopgateDistillates(currentState) {
		for _, goalOp := range distillateRecord.GoalOps {
			switch goalOp.Action {
			case "opened":
				goalsByID[goalOp.GoalID] = goalOp.Text
			case "closed":
				delete(goalsByID, goalOp.GoalID)
			}
		}
	}
	goalIDs := make([]string, 0, len(goalsByID))
	for goalID := range goalsByID {
		goalIDs = append(goalIDs, goalID)
	}
	sort.Strings(goalIDs)
	goals := make([]map[string]string, 0, len(goalIDs))
	for _, goalID := range goalIDs {
		goals = append(goals, map[string]string{
			"goal_id": goalID,
			"text":    goalsByID[goalID],
		})
	}
	return map[string]interface{}{
		"schema_version": continuityMemorySchemaVersion,
		"goals":          goals,
	}
}

func buildTasksCurrentSnapshot(currentState continuityMemoryState) map[string]interface{} {
	itemsByID := map[string]MemoryWakeStateOpenItem{}
	for _, distillateRecord := range activeLoopgateDistillates(currentState) {
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
				itemsByID[itemOp.ItemID] = taskMetadata
			case "closed":
				delete(itemsByID, itemOp.ItemID)
			case todoItemOpStatusSet:
				if existingItem, ok := itemsByID[itemOp.ItemID]; ok {
					if normalized := normalizeExplicitTodoWorkflowStatus(itemOp.Status); normalized != "" {
						existingItem.Status = normalized
						itemsByID[itemOp.ItemID] = existingItem
					}
				}
			}
		}
	}
	itemIDs := make([]string, 0, len(itemsByID))
	for itemID := range itemsByID {
		itemIDs = append(itemIDs, itemID)
	}
	sort.Strings(itemIDs)
	items := make([]map[string]string, 0, len(itemIDs))
	for _, itemID := range itemIDs {
		itemRecord := itemsByID[itemID]
		status := itemRecord.Status
		if status == "" {
			status = explicitTodoWorkflowStatusTodo
		}
		items = append(items, map[string]string{
			"item_id":           itemID,
			"text":              itemRecord.Text,
			"task_kind":         itemRecord.TaskKind,
			"source_kind":       itemRecord.SourceKind,
			"next_step":         itemRecord.NextStep,
			"scheduled_for_utc": itemRecord.ScheduledForUTC,
			"execution_class":   itemRecord.ExecutionClass,
			"created_at_utc":    itemRecord.CreatedAtUTC,
			"status":            status,
		})
	}
	return map[string]interface{}{
		"schema_version": continuityMemorySchemaVersion,
		"tasks":          items,
	}
}

func buildReviewsCurrentSnapshot(currentState continuityMemoryState) map[string]interface{} {
	reviews := make([]map[string]string, 0, len(currentState.Inspections))
	for _, inspectionRecord := range currentState.Inspections {
		if inspectionRecord.Review.Status == continuityReviewStatusPendingReview {
			reviews = append(reviews, map[string]string{
				"inspection_id": inspectionRecord.InspectionID,
				"thread_id":     inspectionRecord.ThreadID,
				"review_status": inspectionRecord.Review.Status,
			})
		}
	}
	sort.Slice(reviews, func(leftIndex int, rightIndex int) bool {
		return reviews[leftIndex]["inspection_id"] < reviews[rightIndex]["inspection_id"]
	})
	return map[string]interface{}{
		"schema_version": continuityMemorySchemaVersion,
		"reviews":        reviews,
	}
}

func buildDerivationSignature(observedPacket continuityObservedPacket) string {
	return hashStableJSONIdentifier(observedPacket)
}

func importanceBase(runtimeConfig config.RuntimeConfig, userImportance string) int {
	switch userImportance {
	case "critical":
		return runtimeConfig.Memory.Scoring.ImportanceBase.Critical
	case "not_important":
		return runtimeConfig.Memory.Scoring.ImportanceBase.NotImportant
	default:
		return runtimeConfig.Memory.Scoring.ImportanceBase.SomewhatImportant
	}
}

func hotnessBase(runtimeConfig config.RuntimeConfig, userImportance string) int {
	switch userImportance {
	case "critical":
		return runtimeConfig.Memory.Scoring.HotnessBase.Critical
	case "not_important":
		return runtimeConfig.Memory.Scoring.HotnessBase.NotImportant
	default:
		return runtimeConfig.Memory.Scoring.HotnessBase.SomewhatImportant
	}
}

func parseTimeOrZero(rawTimestamp string) time.Time {
	if strings.TrimSpace(rawTimestamp) == "" {
		return time.Time{}
	}
	parsedTime, err := time.Parse(time.RFC3339Nano, rawTimestamp)
	if err != nil {
		return time.Time{}
	}
	return parsedTime
}

func deriveMemoryState(effectiveHotness int, lineageStatus string) string {
	switch lineageStatus {
	case continuityLineageStatusPurged:
		return memoryStatePurged
	case continuityLineageStatusTombstoned:
		return memoryStateTombstoned
	}
	switch {
	case effectiveHotness >= 60:
		return memoryStateHot
	case effectiveHotness >= 30:
		return memoryStateWarm
	default:
		return memoryStateCold
	}
}

func makeEventPayloadID(prefix string, payload interface{}) string {
	return prefix + "_" + hashStableJSONIdentifier(payload)
}
