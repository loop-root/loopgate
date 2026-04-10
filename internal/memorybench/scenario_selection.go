package memorybench

import (
	"fmt"
	"slices"
	"strings"
)

var builtInScenarioSets = map[string][]string{
	"profile_slot_same_entity_preview": {
		"contradiction.profile_timezone_same_entity_wrong_current_probe.v1",
		"contradiction.profile_locale_same_entity_wrong_current_probe.v1",
		"contradiction.profile_timezone_interleaved_preview_chain_slot_probe.v1",
		"contradiction.profile_locale_interleaved_preview_chain_slot_probe.v1",
	},
	"profile_slot_preview_bias": {
		"contradiction.profile_timezone_preview_bias_far_match_slot_probe.v1",
		"contradiction.profile_timezone_preview_bias_margin_two_slot_probe.v1",
		"contradiction.profile_timezone_preview_bias_distractor_chain_slot_probe.v1",
		"contradiction.profile_locale_preview_bias_far_match_slot_probe.v1",
		"contradiction.profile_locale_preview_bias_distractor_chain_slot_probe.v1",
	},
	"profile_slot_preview_controls": {
		"contradiction.profile_timezone_close_preview_label_slot_probe.v1",
		"contradiction.profile_timezone_preview_only_control.v1",
		"contradiction.profile_locale_preview_only_control.v1",
	},
	"demo_task_resumption": {
		"task_resumption.blocker_changes_over_time.v1",
		"task_resumption.long_history_cost_pressure.v1",
	},
	"demo_slot_truth": {
		"contradiction.profile_timezone_same_entity_wrong_current_probe.v1",
		"contradiction.profile_locale_preview_bias_far_match_slot_probe.v1",
	},
	"poisoning_policy_matrix": {
		"poisoning.remember_ignore_safety.v1",
		"poisoning.continuity_replay_ignore_safety.v1",
		"poisoning.hint_leak_secret_instructions.v1",
		"poisoning.authority_spoof_override_token.v1",
		"poisoning.stable_slot_name_override.v1",
		"poisoning.paraphrased_authority_bypass_family.v1",
		"poisoning.paraphrased_secret_exfil_family.v1",
		"poisoning.long_history_benign_malicious_interleaving.v1",
		"poisoning.delayed_trigger_review_waiver.v1",
		"poisoning.markdown_checklist_secret_exfil.v1",
		"poisoning.yaml_frontmatter_authority_spoof.v1",
		"poisoning.session_cookie_exfil_family.v1",
		"poisoning.deferred_activation_signing_key_handoff.v1",
		"poisoning.approval_waiver_client_secret_note.v1",
		"safety_precision.secret_santa_note.v1",
		"safety_precision.follow_safety_preference.v1",
		"safety_precision.approval_checklist_note.v1",
		"safety_precision.override_review_note_guard.v1",
		"safety_precision.secret_rotation_guard.v1",
		"safety_precision.interleaved_release_history_note.v1",
		"safety_precision.markdown_release_checklist.v1",
		"safety_precision.yaml_rotation_runbook.v1",
		"safety_precision.review_waiver_denied_postmortem.v1",
	},
	"poisoning_format_laundering": {
		"poisoning.markdown_checklist_secret_exfil.v1",
		"poisoning.yaml_frontmatter_authority_spoof.v1",
		"safety_precision.markdown_release_checklist.v1",
		"safety_precision.yaml_rotation_runbook.v1",
	},
	"poisoning_delayed_trigger": {
		"poisoning.delayed_trigger_review_waiver.v1",
		"poisoning.deferred_activation_signing_key_handoff.v1",
		"safety_precision.review_waiver_denied_postmortem.v1",
	},
	"rag_evidence_matrix": {
		"evidence.semantic_paraphrase_replay_batch_root_cause.v1",
		"evidence.multi_document_mount_grant_design_thread.v1",
		"evidence.incident_qdrant_backfill_socket_stall.v1",
		"evidence.preview_card_authority_boundary_thread.v1",
	},
}

func NormalizeScenarioFilter(rawScenarioFilter ScenarioFilter) (ScenarioFilter, error) {
	normalizedScenarioFilter := ScenarioFilter{
		ScenarioIDs:  normalizeScenarioFilterValues(rawScenarioFilter.ScenarioIDs),
		ScenarioSets: normalizeScenarioFilterValues(rawScenarioFilter.ScenarioSets),
		Categories:   normalizeScenarioFilterValues(rawScenarioFilter.Categories),
		Subfamilies:  normalizeScenarioFilterValues(rawScenarioFilter.Subfamilies),
	}
	for _, scenarioSetID := range normalizedScenarioFilter.ScenarioSets {
		if _, foundScenarioSet := builtInScenarioSets[scenarioSetID]; !foundScenarioSet {
			return ScenarioFilter{}, fmt.Errorf("unknown scenario set %q", scenarioSetID)
		}
	}
	return normalizedScenarioFilter, nil
}

func normalizeScenarioFilterValues(rawValues []string) []string {
	normalizedValues := make([]string, 0, len(rawValues))
	seenValues := make(map[string]struct{}, len(rawValues))
	for _, rawValue := range rawValues {
		normalizedValue := strings.TrimSpace(strings.ToLower(rawValue))
		if normalizedValue == "" {
			continue
		}
		if _, seenValue := seenValues[normalizedValue]; seenValue {
			continue
		}
		seenValues[normalizedValue] = struct{}{}
		normalizedValues = append(normalizedValues, normalizedValue)
	}
	slices.Sort(normalizedValues)
	return normalizedValues
}

func SelectScenarioFixtures(defaultFixtures []ScenarioFixture, scenarioFilter ScenarioFilter) ([]ScenarioFixture, error) {
	if scenarioFilter.IsZero() {
		return append([]ScenarioFixture(nil), defaultFixtures...), nil
	}
	selectedScenarioIDs := make(map[string]struct{}, len(scenarioFilter.ScenarioIDs))
	for _, scenarioID := range scenarioFilter.ScenarioIDs {
		selectedScenarioIDs[scenarioID] = struct{}{}
	}
	for _, scenarioSetID := range scenarioFilter.ScenarioSets {
		for _, scenarioID := range builtInScenarioSets[scenarioSetID] {
			selectedScenarioIDs[strings.ToLower(strings.TrimSpace(scenarioID))] = struct{}{}
		}
	}

	selectedFixtures := make([]ScenarioFixture, 0, len(defaultFixtures))
	for _, fixture := range defaultFixtures {
		if !scenarioFixtureMatchesFilter(fixture, selectedScenarioIDs, scenarioFilter) {
			continue
		}
		selectedFixtures = append(selectedFixtures, fixture)
	}
	if len(selectedFixtures) == 0 {
		return nil, fmt.Errorf("scenario filter matched zero fixtures")
	}
	return selectedFixtures, nil
}

func scenarioFixtureMatchesFilter(fixture ScenarioFixture, selectedScenarioIDs map[string]struct{}, scenarioFilter ScenarioFilter) bool {
	normalizedScenarioID := strings.ToLower(strings.TrimSpace(fixture.Metadata.ScenarioID))
	if len(selectedScenarioIDs) > 0 {
		if _, selectedScenarioID := selectedScenarioIDs[normalizedScenarioID]; !selectedScenarioID {
			return false
		}
	}
	if len(scenarioFilter.Categories) > 0 {
		normalizedCategory := strings.ToLower(strings.TrimSpace(fixture.Metadata.Category))
		if !slices.Contains(scenarioFilter.Categories, normalizedCategory) {
			return false
		}
	}
	if len(scenarioFilter.Subfamilies) > 0 {
		normalizedSubfamilyID := strings.ToLower(strings.TrimSpace(fixture.Metadata.SubfamilyID))
		if !slices.Contains(scenarioFilter.Subfamilies, normalizedSubfamilyID) {
			return false
		}
	}
	return true
}
