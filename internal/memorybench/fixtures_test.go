package memorybench

import (
	"strings"
	"testing"
)

func TestDefaultScenarioFixtures_IncludePoisoningFixture(t *testing.T) {
	defaultFixtures := DefaultScenarioFixtures()
	if len(defaultFixtures) < 70 {
		t.Fatalf("expected at least seventy default scenario fixtures, got %d", len(defaultFixtures))
	}

	foundScenarioFixtures := map[string]bool{
		"poisoning.remember_ignore_safety.v1":                                            false,
		"poisoning.continuity_replay_ignore_safety.v1":                                   false,
		"poisoning.hint_leak_secret_instructions.v1":                                     false,
		"poisoning.authority_spoof_override_token.v1":                                    false,
		"poisoning.stable_slot_name_override.v1":                                         false,
		"poisoning.paraphrased_authority_bypass_family.v1":                               false,
		"poisoning.paraphrased_secret_exfil_family.v1":                                   false,
		"poisoning.long_history_benign_malicious_interleaving.v1":                        false,
		"poisoning.delayed_trigger_review_waiver.v1":                                     false,
		"poisoning.markdown_checklist_secret_exfil.v1":                                   false,
		"poisoning.yaml_frontmatter_authority_spoof.v1":                                  false,
		"poisoning.session_cookie_exfil_family.v1":                                       false,
		"poisoning.deferred_activation_signing_key_handoff.v1":                           false,
		"poisoning.approval_waiver_client_secret_note.v1":                                false,
		"contradiction.preference_latest_theme_wins.v1":                                  false,
		"contradiction.identity_old_name_suppressed.v1":                                  false,
		"contradiction.preference_multiple_theme_supersessions.v1":                       false,
		"contradiction.preference_indentation_update.v1":                                 false,
		"contradiction.identity_entity_disambiguation.v1":                                false,
		"contradiction.identity_alias_supersession_paraphrase.v1":                        false,
		"contradiction.identity_alias_entity_guard.v1":                                   false,
		"contradiction.identity_profile_name_slot_probe.v1":                              false,
		"contradiction.identity_profile_name_different_entity_slot_probe.v1":             false,
		"contradiction.identity_profile_name_same_entity_wrong_current_probe.v1":         false,
		"contradiction.identity_profile_name_different_entity_wrong_current_probe.v1":    false,
		"contradiction.identity_interleaved_alias_chain_slot_probe.v1":                   false,
		"contradiction.profile_timezone_slot_probe.v1":                                   false,
		"contradiction.profile_timezone_same_entity_wrong_current_probe.v1":              false,
		"contradiction.profile_timezone_different_entity_wrong_current_probe.v1":         false,
		"contradiction.profile_locale_same_entity_wrong_current_probe.v1":                false,
		"contradiction.profile_locale_different_entity_wrong_current_probe.v1":           false,
		"contradiction.profile_timezone_interleaved_preview_chain_slot_probe.v1":         false,
		"contradiction.profile_locale_interleaved_preview_chain_slot_probe.v1":           false,
		"contradiction.profile_timezone_close_preview_label_slot_probe.v1":               false,
		"contradiction.profile_timezone_preview_bias_far_match_slot_probe.v1":            false,
		"contradiction.profile_timezone_preview_bias_margin_two_slot_probe.v1":           false,
		"contradiction.profile_timezone_preview_bias_distractor_chain_slot_probe.v1":     false,
		"contradiction.profile_timezone_multiple_preview_labels_slot_probe.v1":           false,
		"contradiction.profile_timezone_conflicting_recent_preview_labels_slot_probe.v1": false,
		"contradiction.profile_timezone_preview_only_control.v1":                         false,
		"contradiction.profile_locale_preview_bias_far_match_slot_probe.v1":              false,
		"contradiction.profile_locale_preview_bias_distractor_chain_slot_probe.v1":       false,
		"contradiction.profile_locale_preview_only_control.v1":                           false,
		"contradiction.profile_pronouns_same_entity_wrong_current_probe.v1":              false,
		"contradiction.profile_pronouns_preview_bias_far_match_slot_probe.v1":            false,
		"contradiction.profile_pronouns_preview_only_control.v1":                         false,
		"contradiction.identity_profile_name_preview_bias_far_match_slot_probe.v1":       false,
		"contradiction.identity_profile_name_preview_only_control.v1":                    false,
		"task_resumption.benchmark_seeding_after_pause.v1":                               false,
		"task_resumption.hardening_follow_up_after_restart.v1":                           false,
		"task_resumption.blocker_changes_over_time.v1":                                   false,
		"task_resumption.multi_hop_dependency_context.v1":                                false,
		"task_resumption.long_history_cost_pressure.v1":                                  false,
		"task_resumption.long_supersession_chain_multi_blocker_updates.v1":               false,
		"task_resumption.blocker_drift_distractor_guard.v1":                              false,
		"task_resumption.multi_hop_distractor_guard.v1":                                  false,
		"task_resumption.blocker_paraphrase_guard.v1":                                    false,
		"task_resumption.multi_hop_alias_guard.v1":                                       false,
		"task_resumption.blocker_drift_multi_update_guard.v1":                            false,
		"task_resumption.alias_preview_distractor_guard.v1":                              false,
		"task_resumption.interleaved_malicious_history_guard.v1":                         false,
		"safety_precision.secret_santa_note.v1":                                          false,
		"safety_precision.follow_safety_preference.v1":                                   false,
		"safety_precision.approval_checklist_note.v1":                                    false,
		"safety_precision.override_review_note_guard.v1":                                 false,
		"safety_precision.secret_rotation_guard.v1":                                      false,
		"safety_precision.interleaved_release_history_note.v1":                           false,
		"safety_precision.markdown_release_checklist.v1":                                 false,
		"safety_precision.yaml_rotation_runbook.v1":                                      false,
		"safety_precision.review_waiver_denied_postmortem.v1":                            false,
	}
	for _, fixture := range defaultFixtures {
		if _, found := foundScenarioFixtures[fixture.Metadata.ScenarioID]; !found {
			continue
		}
		foundScenarioFixtures[fixture.Metadata.ScenarioID] = true
		switch fixture.Metadata.Category {
		case CategoryMemoryPoisoning:
			if fixture.PoisoningExpectation == nil {
				t.Fatalf("expected poisoning expectation on fixture %#v", fixture)
			}
			if fixture.GovernedCandidate == nil {
				t.Fatalf("expected governed candidate on poisoning fixture %#v", fixture)
			}
			if !fixture.PoisoningExpectation.MustBlockRecall || !fixture.PoisoningExpectation.MustAvoidPromptInjection {
				t.Fatalf("unexpected poisoning expectations: %#v", fixture.PoisoningExpectation)
			}
		case CategoryMemoryContradiction:
			if fixture.ContradictionExpectation == nil {
				t.Fatalf("expected contradiction expectation on fixture %#v", fixture)
			}
			if fixture.Metadata.SubfamilyID == "" {
				t.Fatalf("expected contradiction subfamily on fixture %#v", fixture)
			}
			if fixture.ContradictionExpectation.ExpectedPrimaryHint == "" {
				t.Fatalf("expected primary hint on contradiction fixture %#v", fixture)
			}
			if fixture.ContradictionExpectation.ProbeMode == "slot_only" && strings.Contains(strings.ToLower(fixture.Steps[len(fixture.Steps)-1].Content), strings.ToLower(fixture.ContradictionExpectation.ExpectedPrimaryHint)) {
				t.Fatalf("expected slot-only probe not to leak answer text on fixture %#v", fixture)
			}
		case CategoryTaskResumption:
			if fixture.TaskResumptionExpectation == nil {
				t.Fatalf("expected task resumption expectation on fixture %#v", fixture)
			}
			if len(fixture.TaskResumptionExpectation.RequiredHints) == 0 {
				t.Fatalf("expected required hints on task resumption fixture %#v", fixture)
			}
			if fixture.TaskResumptionExpectation.MaxItemsReturned <= 0 {
				t.Fatalf("expected bounded item budget on task resumption fixture %#v", fixture)
			}
			if fixture.TaskResumptionExpectation.MaxHintBytesRetrieved <= 0 {
				t.Fatalf("expected bounded hint-byte budget on task resumption fixture %#v", fixture)
			}
		case CategoryMemorySafetyPrecision:
			if fixture.GovernedCandidate == nil {
				t.Fatalf("expected governed candidate on safety precision fixture %#v", fixture)
			}
			if fixture.SafetyPrecisionExpectation == nil || !fixture.SafetyPrecisionExpectation.MustPersist {
				t.Fatalf("expected must-persist safety precision expectation on fixture %#v", fixture)
			}
		default:
			t.Fatalf("unexpected fixture category: %#v", fixture.Metadata)
		}
		if fixture.Metadata.Category != CategoryMemorySafetyPrecision && len(fixture.Steps) < 2 {
			t.Fatalf("expected multi-step fixture outside safety precision, got %#v", fixture.Steps)
		}
		if fixture.Metadata.ArchitecturalMechanism == "" {
			t.Fatalf("expected architectural mechanism on fixture %q", fixture.Metadata.ScenarioID)
		}
		if fixture.Metadata.TargetFailureMode == "" {
			t.Fatalf("expected target failure mode on fixture %q", fixture.Metadata.ScenarioID)
		}
		if fixture.Metadata.BenignControlOrDistractor == "" {
			t.Fatalf("expected benign control or distractor on fixture %q", fixture.Metadata.ScenarioID)
		}
	}
	for scenarioID, found := range foundScenarioFixtures {
		if !found {
			t.Fatalf("expected scenario fixture %q in default fixtures", scenarioID)
		}
	}
}
