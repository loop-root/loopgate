package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"morph/internal/memorybench"
)

type fakeMainProjectedNodeDiscoverer struct {
	items []memorybench.ProjectedNodeDiscoverItem
}

func (discoverer fakeMainProjectedNodeDiscoverer) DiscoverProjectedNodes(tctx context.Context, scope string, query string, maxItems int) ([]memorybench.ProjectedNodeDiscoverItem, error) {
	_ = tctx
	_ = scope
	_ = query
	clonedItems := append([]memorybench.ProjectedNodeDiscoverItem(nil), discoverer.items...)
	if maxItems > 0 && len(clonedItems) > maxItems {
		clonedItems = append([]memorybench.ProjectedNodeDiscoverItem(nil), clonedItems[:maxItems]...)
	}
	return clonedItems, nil
}

func TestSelectProjectedNodeDiscoverer_DefaultContinuityWithoutRepoRoot(t *testing.T) {
	discoverer, backendName, err := selectProjectedNodeDiscoverer("continuity_tcl", "", memorybench.RAGBaselineConfig{}, false, continuityAblationNone, true, 1)
	if err != nil {
		t.Fatalf("selectProjectedNodeDiscoverer: %v", err)
	}
	if discoverer != nil {
		t.Fatalf("expected nil discoverer without repo root, got %#v", discoverer)
	}
	if backendName != memorybench.BackendContinuityTCL {
		t.Fatalf("expected normalized continuity backend, got %q", backendName)
	}
}

func TestSelectProjectedNodeDiscoverer_RejectsUnwiredRAGBaseline(t *testing.T) {
	_, _, err := selectProjectedNodeDiscoverer("rag_baseline", "", memorybench.RAGBaselineConfig{}, false, continuityAblationNone, true, 1)
	if err == nil {
		t.Fatal("expected rag_baseline selection without repo root to fail")
	}
	if !strings.Contains(err.Error(), "requires -repo-root") {
		t.Fatalf("expected repo-root requirement error, got %v", err)
	}

	_, _, err = selectProjectedNodeDiscoverer("rag_baseline", "", memorybench.RAGBaselineConfig{
		QdrantURL:      "http://127.0.0.1:6333",
		CollectionName: "memorybench_default",
	}, false, continuityAblationNone, true, 1)
	if err == nil {
		t.Fatal("expected rag_baseline selection without repo root to fail closed")
	}
	if !strings.Contains(err.Error(), "requires -repo-root") {
		t.Fatalf("expected repo-root requirement error, got %v", err)
	}
}

func TestSelectProjectedNodeDiscoverer_RejectsStrongerRAGWithoutRepoRoot(t *testing.T) {
	_, _, err := selectProjectedNodeDiscoverer("rag_stronger", "", memorybench.RAGBaselineConfig{}, false, continuityAblationNone, true, 1)
	if err == nil {
		t.Fatal("expected rag_stronger selection without repo root to fail")
	}
	if !strings.Contains(err.Error(), "requires -repo-root") {
		t.Fatalf("expected repo-root requirement error, got %v", err)
	}
}

func TestSelectProjectedNodeDiscoverer_RejectsMissingRAGRuntimePaths(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, err := selectProjectedNodeDiscoverer("rag_baseline", repoRoot, memorybench.RAGBaselineConfig{
		QdrantURL:      "http://127.0.0.1:6333",
		CollectionName: "memorybench_default",
	}, false, continuityAblationNone, true, 1)
	if err == nil {
		t.Fatal("expected missing rag runtime paths to fail")
	}
	if !strings.Contains(err.Error(), "python executable unavailable") {
		t.Fatalf("expected missing python executable error, got %v", err)
	}
}

func TestSelectProjectedNodeDiscoverer_WiresRAGBaselineWithLocalHelperPaths(t *testing.T) {
	repoRoot := t.TempDir()
	pythonExecutablePath := filepath.Join(repoRoot, ".cache", "memorybench-venv", "bin", "python")
	if err := os.MkdirAll(filepath.Dir(pythonExecutablePath), 0o755); err != nil {
		t.Fatalf("mkdir python executable parent: %v", err)
	}
	if err := os.WriteFile(pythonExecutablePath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake python executable: %v", err)
	}
	helperScriptPath := filepath.Join(repoRoot, "cmd", "memorybench", "rag_search.py")
	if err := os.MkdirAll(filepath.Dir(helperScriptPath), 0o755); err != nil {
		t.Fatalf("mkdir helper script parent: %v", err)
	}
	if err := os.WriteFile(helperScriptPath, []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatalf("write fake helper script: %v", err)
	}

	discoverer, backendName, err := selectProjectedNodeDiscoverer("rag_baseline", repoRoot, memorybench.RAGBaselineConfig{
		QdrantURL:      "http://127.0.0.1:6333",
		CollectionName: "memorybench_default",
	}, false, continuityAblationNone, true, 1)
	if err != nil {
		t.Fatalf("selectProjectedNodeDiscoverer: %v", err)
	}
	if discoverer == nil {
		t.Fatal("expected rag baseline discoverer to be constructed")
	}
	if backendName != memorybench.BackendRAGBaseline {
		t.Fatalf("expected rag baseline backend, got %q", backendName)
	}
}

func TestSelectProjectedNodeDiscoverer_WiresStrongerRAGWithLocalHelperPaths(t *testing.T) {
	repoRoot := t.TempDir()
	pythonExecutablePath := filepath.Join(repoRoot, ".cache", "memorybench-venv", "bin", "python")
	if err := os.MkdirAll(filepath.Dir(pythonExecutablePath), 0o755); err != nil {
		t.Fatalf("mkdir python executable parent: %v", err)
	}
	if err := os.WriteFile(pythonExecutablePath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake python executable: %v", err)
	}
	helperScriptPath := filepath.Join(repoRoot, "cmd", "memorybench", "rag_search.py")
	if err := os.MkdirAll(filepath.Dir(helperScriptPath), 0o755); err != nil {
		t.Fatalf("mkdir helper script parent: %v", err)
	}
	if err := os.WriteFile(helperScriptPath, []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatalf("write fake helper script: %v", err)
	}

	discoverer, backendName, err := selectProjectedNodeDiscoverer("rag_stronger", repoRoot, memorybench.RAGBaselineConfig{
		QdrantURL:      "http://127.0.0.1:6333",
		CollectionName: "memorybench_default",
		RerankerName:   "Xenova/ms-marco-MiniLM-L-6-v2",
	}, false, continuityAblationNone, true, 1)
	if err != nil {
		t.Fatalf("selectProjectedNodeDiscoverer: %v", err)
	}
	if discoverer == nil {
		t.Fatal("expected stronger rag discoverer to be constructed")
	}
	if backendName != memorybench.BackendRAGStronger {
		t.Fatalf("expected stronger rag backend, got %q", backendName)
	}
}

func TestSelectProjectedNodeDiscoverer_RejectsUnknownBackend(t *testing.T) {
	_, _, err := selectProjectedNodeDiscoverer("mystery_backend", "", memorybench.RAGBaselineConfig{}, false, continuityAblationNone, true, 1)
	if err == nil {
		t.Fatal("expected unknown backend selection to fail")
	}
	if !strings.Contains(err.Error(), "mystery_backend") {
		t.Fatalf("expected unknown backend in error, got %v", err)
	}
}

func TestIsolateRAGBenchmarkConfig_IsolatesPerRunAndMode(t *testing.T) {
	baseConfig := memorybench.RAGBaselineConfig{
		QdrantURL:      "http://127.0.0.1:6333",
		CollectionName: "memorybench_rerank",
		RerankerName:   "Xenova/ms-marco-MiniLM-L-6-v2",
	}
	defaultModeConfig, err := isolateRAGBenchmarkConfig(memorybench.BackendRAGStronger, memorybench.CandidateGovernanceBackendDefault, "run_a", baseConfig)
	if err != nil {
		t.Fatalf("isolateRAGBenchmarkConfig default: %v", err)
	}
	governedModeConfig, err := isolateRAGBenchmarkConfig(memorybench.BackendRAGStronger, memorybench.CandidateGovernanceContinuityTCL, "run_a", baseConfig)
	if err != nil {
		t.Fatalf("isolateRAGBenchmarkConfig governed: %v", err)
	}
	secondRunConfig, err := isolateRAGBenchmarkConfig(memorybench.BackendRAGStronger, memorybench.CandidateGovernanceBackendDefault, "run_b", baseConfig)
	if err != nil {
		t.Fatalf("isolateRAGBenchmarkConfig second run: %v", err)
	}

	if defaultModeConfig.CollectionName == baseConfig.CollectionName {
		t.Fatalf("expected isolated collection name to differ from base collection, got %q", defaultModeConfig.CollectionName)
	}
	if defaultModeConfig.CollectionName == governedModeConfig.CollectionName {
		t.Fatalf("expected different governance modes to isolate collections, got %q", defaultModeConfig.CollectionName)
	}
	if defaultModeConfig.CollectionName == secondRunConfig.CollectionName {
		t.Fatalf("expected different run ids to isolate collections, got %q", defaultModeConfig.CollectionName)
	}
	if !strings.Contains(defaultModeConfig.CollectionName, "memorybench_rerank__rag_stronger__backend_default__") {
		t.Fatalf("expected collection name to include backend and mode, got %q", defaultModeConfig.CollectionName)
	}
}

func TestIsolateRAGBenchmarkConfig_LeavesContinuityUntouched(t *testing.T) {
	baseConfig := memorybench.RAGBaselineConfig{
		QdrantURL:      "http://127.0.0.1:6333",
		CollectionName: "memorybench_default",
	}
	isolatedConfig, err := isolateRAGBenchmarkConfig(memorybench.BackendContinuityTCL, memorybench.CandidateGovernanceBackendDefault, "run_a", baseConfig)
	if err != nil {
		t.Fatalf("isolateRAGBenchmarkConfig continuity: %v", err)
	}
	if isolatedConfig != baseConfig {
		t.Fatalf("expected non-rag backend config to stay unchanged, got %#v", isolatedConfig)
	}
}

func TestIsolatedRAGBenchmarkCollectionName_RejectsEmptyInputs(t *testing.T) {
	_, err := isolatedRAGBenchmarkCollectionName("", memorybench.BackendRAGBaseline, memorybench.CandidateGovernanceBackendDefault, "run_a")
	if err == nil {
		t.Fatal("expected empty collection name to fail")
	}
	_, err = isolatedRAGBenchmarkCollectionName("memorybench_default", memorybench.BackendRAGBaseline, memorybench.CandidateGovernanceBackendDefault, "")
	if err == nil {
		t.Fatal("expected empty run id to fail")
	}
}

func TestSelectCandidateGovernanceEvaluator_WiresContinuityAndRAGBackends(t *testing.T) {
	continuityEvaluator, err := selectCandidateGovernanceEvaluator("continuity_tcl", memorybench.CandidateGovernanceBackendDefault, memorybench.RAGBaselineConfig{})
	if err != nil {
		t.Fatalf("select continuity candidate evaluator: %v", err)
	}
	if continuityEvaluator == nil {
		t.Fatal("expected continuity candidate evaluator")
	}

	ragEvaluator, err := selectCandidateGovernanceEvaluator("rag_baseline", memorybench.CandidateGovernanceBackendDefault, memorybench.RAGBaselineConfig{
		QdrantURL:      "http://127.0.0.1:6333",
		CollectionName: "memorybench_default",
	})
	if err != nil {
		t.Fatalf("select rag candidate evaluator: %v", err)
	}
	if ragEvaluator == nil {
		t.Fatal("expected rag candidate evaluator")
	}

	strongerRAGEvaluator, err := selectCandidateGovernanceEvaluator("rag_stronger", memorybench.CandidateGovernanceBackendDefault, memorybench.RAGBaselineConfig{
		QdrantURL:      "http://127.0.0.1:6333",
		CollectionName: "memorybench_default",
		RerankerName:   "Xenova/ms-marco-MiniLM-L-6-v2",
	})
	if err != nil {
		t.Fatalf("select stronger rag candidate evaluator: %v", err)
	}
	if strongerRAGEvaluator == nil {
		t.Fatal("expected stronger rag candidate evaluator")
	}
}

func TestSelectCandidateGovernanceEvaluator_AllowsFairnessOverrides(t *testing.T) {
	permissiveContinuityEvaluator, err := selectCandidateGovernanceEvaluator("continuity_tcl", memorybench.CandidateGovernancePermissive, memorybench.RAGBaselineConfig{})
	if err != nil {
		t.Fatalf("select permissive continuity evaluator: %v", err)
	}
	if permissiveContinuityEvaluator == nil {
		t.Fatal("expected permissive continuity candidate evaluator")
	}

	governedRAGEvaluator, err := selectCandidateGovernanceEvaluator("rag_baseline", memorybench.CandidateGovernanceContinuityTCL, memorybench.RAGBaselineConfig{
		QdrantURL:      "http://127.0.0.1:6333",
		CollectionName: "memorybench_default",
	})
	if err != nil {
		t.Fatalf("select governed rag evaluator: %v", err)
	}
	if governedRAGEvaluator == nil {
		t.Fatal("expected governed rag candidate evaluator")
	}
}

func TestMaybeSeedRAGFixtureCorpus_NoOpForContinuityBackend(t *testing.T) {
	if err := maybeSeedRAGFixtureCorpus(t.Context(), "", "continuity_tcl", memorybench.RAGBaselineConfig{}); err != nil {
		t.Fatalf("maybeSeedRAGFixtureCorpus: %v", err)
	}
}

func TestBuildContinuityFixtureProjectedNodeSeeds_ReturnsFixtureCorpusSeeds(t *testing.T) {
	projectedNodeSeeds, err := buildContinuityFixtureProjectedNodeSeeds(continuityAblationNone)
	if err != nil {
		t.Fatalf("buildContinuityFixtureProjectedNodeSeeds: %v", err)
	}
	if len(projectedNodeSeeds) == 0 {
		t.Fatal("expected fixture seeds")
	}
	if projectedNodeSeeds[0].NodeKind == "" || projectedNodeSeeds[0].HintText == "" {
		t.Fatalf("unexpected projected node seed: %#v", projectedNodeSeeds[0])
	}
	foundActiveCurrent := false
	foundTombstonedSuppressed := false
	foundActiveDistractor := false
	foundTaskResumptionActive := false
	foundTaskResumptionStale := false
	foundSlotProbeCurrentSignature := false
	foundSlotProbeDistractorSignature := false
	foundTimezoneSlotProbeCurrentSignature := false
	foundLocaleSlotProbeCurrentSignature := false
	foundTimezoneInterleavedCurrentSignature := false
	foundLocaleInterleavedCurrentSignature := false
	for _, projectedNodeSeed := range projectedNodeSeeds {
		switch {
		case strings.Contains(projectedNodeSeed.NodeID, "::current") && projectedNodeSeed.State == "active":
			foundActiveCurrent = true
		case strings.Contains(projectedNodeSeed.NodeID, "::suppressed::") && projectedNodeSeed.State == "tombstoned":
			foundTombstonedSuppressed = true
		case strings.Contains(projectedNodeSeed.NodeID, "::distractor::") && projectedNodeSeed.State == "active":
			foundActiveDistractor = true
		case strings.Contains(projectedNodeSeed.NodeID, "task_resumption.") && strings.Contains(projectedNodeSeed.NodeID, "::resume::") && projectedNodeSeed.State == "active":
			foundTaskResumptionActive = true
		case strings.Contains(projectedNodeSeed.NodeID, "task_resumption.") && strings.Contains(projectedNodeSeed.NodeID, "::stale::") && projectedNodeSeed.State == "tombstoned":
			foundTaskResumptionStale = true
		}
		if projectedNodeSeed.NodeID == "contradiction.identity_profile_name_slot_probe.v1::current" &&
			projectedNodeSeed.ExactSignature == continuityFixtureContradictionSignature(
				"contradiction.identity_profile_name_slot_probe.v1",
				"current user profile identity name slot preferred_name",
				"",
			) {
			foundSlotProbeCurrentSignature = true
		}
		if projectedNodeSeed.NodeID == "contradiction.identity_profile_name_different_entity_slot_probe.v1::distractor::00" &&
			projectedNodeSeed.ExactSignature == continuityFixtureContradictionSignature(
				"contradiction.identity_profile_name_different_entity_slot_probe.v1",
				"teammate profile identity name slot preferred_name",
				"distractor",
			) {
			foundSlotProbeDistractorSignature = true
		}
		if projectedNodeSeed.NodeID == "contradiction.profile_timezone_slot_probe.v1::current" &&
			projectedNodeSeed.ExactSignature == continuityFixtureContradictionSignature(
				"contradiction.profile_timezone_slot_probe.v1",
				"current user profile identity timezone slot timezone",
				"",
			) {
			foundTimezoneSlotProbeCurrentSignature = true
		}
		if projectedNodeSeed.NodeID == "contradiction.profile_locale_same_entity_wrong_current_probe.v1::current" &&
			projectedNodeSeed.ExactSignature == continuityFixtureContradictionSignature(
				"contradiction.profile_locale_same_entity_wrong_current_probe.v1",
				"current user profile identity locale slot locale",
				"",
			) {
			foundLocaleSlotProbeCurrentSignature = true
		}
		if projectedNodeSeed.NodeID == "contradiction.profile_timezone_interleaved_preview_chain_slot_probe.v1::current" &&
			projectedNodeSeed.ExactSignature == continuityFixtureContradictionSignature(
				"contradiction.profile_timezone_interleaved_preview_chain_slot_probe.v1",
				"current user profile identity timezone slot timezone",
				"",
			) {
			foundTimezoneInterleavedCurrentSignature = true
		}
		if projectedNodeSeed.NodeID == "contradiction.profile_locale_interleaved_preview_chain_slot_probe.v1::current" &&
			projectedNodeSeed.ExactSignature == continuityFixtureContradictionSignature(
				"contradiction.profile_locale_interleaved_preview_chain_slot_probe.v1",
				"current user profile identity locale slot locale",
				"",
			) {
			foundLocaleInterleavedCurrentSignature = true
		}
		if strings.Contains(projectedNodeSeed.NodeID, "poisoning.") {
			t.Fatalf("expected poisoning fixtures to stay out of continuity governed seeds, got %#v", projectedNodeSeed)
		}
	}
	if !foundActiveCurrent {
		t.Fatal("expected active current continuity seed")
	}
	if !foundTombstonedSuppressed {
		t.Fatal("expected tombstoned suppressed continuity seed")
	}
	if !foundActiveDistractor {
		t.Fatal("expected active distractor continuity seed")
	}
	if !foundTaskResumptionActive {
		t.Fatal("expected active task resumption continuity seed")
	}
	if !foundTaskResumptionStale {
		t.Fatal("expected tombstoned task resumption continuity seed")
	}
	if !foundSlotProbeCurrentSignature {
		t.Fatal("expected slot-probe current seed to use explicit signature hint")
	}
	if !foundSlotProbeDistractorSignature {
		t.Fatal("expected slot-probe distractor seed to use explicit distractor signature hint")
	}
	if !foundTimezoneSlotProbeCurrentSignature {
		t.Fatal("expected timezone slot-probe current seed to use explicit signature hint")
	}
	if !foundLocaleSlotProbeCurrentSignature {
		t.Fatal("expected locale slot-probe current seed to use explicit signature hint")
	}
	if !foundTimezoneInterleavedCurrentSignature {
		t.Fatal("expected timezone interleaved slot-probe current seed to use explicit signature hint")
	}
	if !foundLocaleInterleavedCurrentSignature {
		t.Fatal("expected locale interleaved slot-probe current seed to use explicit signature hint")
	}
}

func TestNormalizeContinuityAblation_DefaultsAndRejectsUnknownModes(t *testing.T) {
	validatedAblation, err := normalizeContinuityAblation("")
	if err != nil {
		t.Fatalf("normalizeContinuityAblation empty: %v", err)
	}
	if validatedAblation != continuityAblationNone {
		t.Fatalf("expected default continuity ablation, got %q", validatedAblation)
	}

	validatedAblation, err = normalizeContinuityAblation("anchors_off")
	if err != nil {
		t.Fatalf("normalizeContinuityAblation anchors_off: %v", err)
	}
	if validatedAblation != continuityAblationAnchorsOff {
		t.Fatalf("expected anchors_off ablation, got %q", validatedAblation)
	}

	_, err = normalizeContinuityAblation("mystery_mode")
	if err == nil {
		t.Fatal("expected unknown continuity ablation to fail")
	}
}

func TestSelectProjectedNodeDiscoverer_RejectsContinuityAblationWithoutFixtureSeeds(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, err := selectProjectedNodeDiscoverer("continuity_tcl", repoRoot, memorybench.RAGBaselineConfig{}, false, continuityAblationAnchorsOff, true, 1)
	if err == nil {
		t.Fatal("expected continuity ablation without fixture seeds to fail")
	}
	if !strings.Contains(err.Error(), "requires -continuity-seed-fixtures") {
		t.Fatalf("expected fixture seed requirement error, got %v", err)
	}
}

func TestBuildContinuityFixtureProjectedNodeSeeds_AppliesAnchorAndHintAblations(t *testing.T) {
	anchorlessSeeds, err := buildContinuityFixtureProjectedNodeSeeds(continuityAblationAnchorsOff)
	if err != nil {
		t.Fatalf("buildContinuityFixtureProjectedNodeSeeds anchors_off: %v", err)
	}
	if len(anchorlessSeeds) != contradictionContinuityAblationSeedCount()+taskResumptionContinuityAblationSeedCount() {
		t.Fatalf("unexpected anchorless seed count: %d", len(anchorlessSeeds))
	}
	for _, seedNode := range anchorlessSeeds {
		if seedNode.ExactSignature != "" || seedNode.FamilySignature != "" {
			t.Fatalf("expected anchor ablation to clear signatures, got %#v", seedNode)
		}
	}

	hintlessSeeds, err := buildContinuityFixtureProjectedNodeSeeds(continuityAblationHintsOff)
	if err != nil {
		t.Fatalf("buildContinuityFixtureProjectedNodeSeeds hints_off: %v", err)
	}
	if len(hintlessSeeds) != contradictionContinuityAblationSeedCount()+taskResumptionContinuityAblationSeedCount() {
		t.Fatalf("unexpected hintless seed count: %d", len(hintlessSeeds))
	}
	for _, seedNode := range hintlessSeeds {
		if seedNode.HintText != "" {
			t.Fatalf("expected hint ablation to clear hint text, got %#v", seedNode)
		}
	}
}

func TestContinuityAblationProjectedNodeDiscoverer_ReducesContextBreadth(t *testing.T) {
	wrappedDiscoverer := continuityAblationProjectedNodeDiscoverer{
		innerDiscoverer: fakeMainProjectedNodeDiscoverer{items: []memorybench.ProjectedNodeDiscoverItem{
			{NodeID: "one"},
			{NodeID: "two"},
		}},
		ablationName: continuityAblationReducedContextBreadth,
	}
	projectedItems, err := wrappedDiscoverer.DiscoverProjectedNodes(t.Context(), "scope", "query", 5)
	if err != nil {
		t.Fatalf("DiscoverProjectedNodes: %v", err)
	}
	if len(projectedItems) != 1 {
		t.Fatalf("expected reduced context breadth to limit items, got %#v", projectedItems)
	}
}

func TestBuildContinuitySlotOnlyRankingPreferences_TracksSameEntityPreviewScopes(t *testing.T) {
	scopePreferences := buildContinuitySlotOnlyRankingPreferences(1)
	timezoneScope := memorybench.BenchmarkScenarioScope("contradiction.profile_timezone_same_entity_wrong_current_probe.v1")
	timezonePreference, foundTimezonePreference := scopePreferences[timezoneScope]
	if !foundTimezonePreference {
		t.Fatalf("expected timezone same-entity scope preference, got %#v", scopePreferences)
	}
	expectedCurrentSignature := continuityFixtureContradictionSignature(
		"contradiction.profile_timezone_same_entity_wrong_current_probe.v1",
		"current user profile identity timezone slot timezone",
		"",
	)
	if timezonePreference.canonicalExactSignature != expectedCurrentSignature {
		t.Fatalf("unexpected timezone canonical signature: %#v", timezonePreference)
	}
	expectedPreviewSignature := continuityFixtureContradictionSignature(
		"contradiction.profile_timezone_same_entity_wrong_current_probe.v1",
		"current user profile locale preview timezone label",
		"distractor",
	)
	if _, foundPreviewSignature := timezonePreference.sameEntityPreviewExactSignature[expectedPreviewSignature]; !foundPreviewSignature {
		t.Fatalf("expected timezone preview signature in preference set, got %#v", timezonePreference)
	}

	differentEntityScope := memorybench.BenchmarkScenarioScope("contradiction.profile_timezone_different_entity_wrong_current_probe.v1")
	if _, foundDifferentEntityPreference := scopePreferences[differentEntityScope]; foundDifferentEntityPreference {
		t.Fatalf("did not expect different-entity scope preference, got %#v", scopePreferences[differentEntityScope])
	}

	previewOnlyControlScope := memorybench.BenchmarkScenarioScope("contradiction.profile_timezone_preview_only_control.v1")
	if _, foundPreviewOnlyControlPreference := scopePreferences[previewOnlyControlScope]; foundPreviewOnlyControlPreference {
		t.Fatalf("did not expect preview-only control scope preference, got %#v", scopePreferences[previewOnlyControlScope])
	}
}

func TestContinuitySlotOnlyPreferenceProjectedNodeDiscoverer_PromotesCanonicalCurrentOverPreviewWhenClose(t *testing.T) {
	scope := memorybench.BenchmarkScenarioScope("contradiction.profile_timezone_same_entity_wrong_current_probe.v1")
	currentSignature := continuityFixtureContradictionSignature(
		"contradiction.profile_timezone_same_entity_wrong_current_probe.v1",
		"current user profile identity timezone slot timezone",
		"",
	)
	previewSignature := continuityFixtureContradictionSignature(
		"contradiction.profile_timezone_same_entity_wrong_current_probe.v1",
		"current user profile locale preview timezone label",
		"distractor",
	)
	wrappedDiscoverer := continuitySlotOnlyPreferenceProjectedNodeDiscoverer{
		innerDiscoverer: fakeMainProjectedNodeDiscoverer{items: []memorybench.ProjectedNodeDiscoverItem{
			{NodeID: "preview", ExactSignature: previewSignature, MatchCount: 5},
			{NodeID: "current", ExactSignature: currentSignature, MatchCount: 4},
		}},
		scopePreferences: map[string]continuitySlotOnlyRankingPreference{
			scope: {
				canonicalExactSignature: currentSignature,
				sameEntityPreviewExactSignature: map[string]struct{}{
					previewSignature: {},
				},
				maxMatchCountDeficit: 1,
			},
		},
	}

	projectedItems, err := wrappedDiscoverer.DiscoverProjectedNodes(t.Context(), scope, "Retrieve the current user profile timezone from the profile slot.", 1)
	if err != nil {
		t.Fatalf("DiscoverProjectedNodes: %v", err)
	}
	if len(projectedItems) != 1 || projectedItems[0].NodeID != "current" {
		t.Fatalf("expected canonical current item to be promoted, got %#v", projectedItems)
	}
}

func TestContinuitySlotOnlyPreferenceProjectedNodeDiscoverer_DoesNotPromoteWhenCanonicalIsNotClose(t *testing.T) {
	scope := memorybench.BenchmarkScenarioScope("contradiction.profile_timezone_same_entity_wrong_current_probe.v1")
	currentSignature := continuityFixtureContradictionSignature(
		"contradiction.profile_timezone_same_entity_wrong_current_probe.v1",
		"current user profile identity timezone slot timezone",
		"",
	)
	previewSignature := continuityFixtureContradictionSignature(
		"contradiction.profile_timezone_same_entity_wrong_current_probe.v1",
		"current user profile locale preview timezone label",
		"distractor",
	)
	wrappedDiscoverer := continuitySlotOnlyPreferenceProjectedNodeDiscoverer{
		innerDiscoverer: fakeMainProjectedNodeDiscoverer{items: []memorybench.ProjectedNodeDiscoverItem{
			{NodeID: "preview", ExactSignature: previewSignature, MatchCount: 5},
			{NodeID: "current", ExactSignature: currentSignature, MatchCount: 3},
		}},
		scopePreferences: map[string]continuitySlotOnlyRankingPreference{
			scope: {
				canonicalExactSignature: currentSignature,
				sameEntityPreviewExactSignature: map[string]struct{}{
					previewSignature: {},
				},
				maxMatchCountDeficit: 1,
			},
		},
	}

	projectedItems, err := wrappedDiscoverer.DiscoverProjectedNodes(t.Context(), scope, "Retrieve the current user profile timezone from the profile slot.", 1)
	if err != nil {
		t.Fatalf("DiscoverProjectedNodes: %v", err)
	}
	if len(projectedItems) != 1 || projectedItems[0].NodeID != "preview" {
		t.Fatalf("expected preview item to remain primary when canonical is not close, got %#v", projectedItems)
	}
}

func TestContinuitySlotOnlyPreferenceProjectedNodeDiscoverer_PromotesWhenConfiguredMarginAllowsFurtherGap(t *testing.T) {
	scope := memorybench.BenchmarkScenarioScope("contradiction.profile_timezone_preview_bias_far_match_slot_probe.v1")
	currentSignature := continuityFixtureContradictionSignature(
		"contradiction.profile_timezone_preview_bias_far_match_slot_probe.v1",
		"current user profile identity timezone slot timezone",
		"",
	)
	previewSignature := continuityFixtureContradictionSignature(
		"contradiction.profile_timezone_preview_bias_far_match_slot_probe.v1",
		"current user profile preview card timezone slot label",
		"distractor",
	)
	wrappedDiscoverer := continuitySlotOnlyPreferenceProjectedNodeDiscoverer{
		innerDiscoverer: fakeMainProjectedNodeDiscoverer{items: []memorybench.ProjectedNodeDiscoverItem{
			{NodeID: "preview", ExactSignature: previewSignature, MatchCount: 8},
			{NodeID: "current", ExactSignature: currentSignature, MatchCount: 5},
		}},
		scopePreferences: map[string]continuitySlotOnlyRankingPreference{
			scope: {
				canonicalExactSignature: currentSignature,
				sameEntityPreviewExactSignature: map[string]struct{}{
					previewSignature: {},
				},
				maxMatchCountDeficit: 3,
			},
		},
	}

	projectedItems, err := wrappedDiscoverer.DiscoverProjectedNodes(t.Context(), scope, "Retrieve the current user profile timezone from the profile slot, not the preview card label.", 1)
	if err != nil {
		t.Fatalf("DiscoverProjectedNodes: %v", err)
	}
	if len(projectedItems) != 1 || projectedItems[0].NodeID != "current" {
		t.Fatalf("expected canonical current item to be promoted under a larger configured margin, got %#v", projectedItems)
	}
}

func TestMaybeWrapContinuitySlotOnlyPreference_DisabledReturnsInnerDiscoverer(t *testing.T) {
	innerDiscoverer := fakeMainProjectedNodeDiscoverer{items: []memorybench.ProjectedNodeDiscoverItem{{NodeID: "current"}}}
	wrappedDiscoverer := maybeWrapContinuitySlotOnlyPreference(innerDiscoverer, false, 1)
	if _, isWrappedDiscoverer := wrappedDiscoverer.(continuitySlotOnlyPreferenceProjectedNodeDiscoverer); isWrappedDiscoverer {
		t.Fatalf("expected disabled preview preference to return inner discoverer, got %#v", wrappedDiscoverer)
	}
}

func TestNormalizeContinuityPreviewSlotPreferenceMargin_RejectsNegative(t *testing.T) {
	if _, err := normalizeContinuityPreviewSlotPreferenceMargin(-1); err == nil {
		t.Fatal("expected negative preview-slot preference margin to fail")
	}
}

func TestMaybeSeedRAGFixtureCorpus_RejectsMissingRepoRoot(t *testing.T) {
	err := maybeSeedRAGFixtureCorpus(t.Context(), "", "rag_baseline", memorybench.RAGBaselineConfig{
		QdrantURL:      "http://127.0.0.1:6333",
		CollectionName: "memorybench_default",
	})
	if err == nil {
		t.Fatal("expected missing repo root to fail")
	}
	if !strings.Contains(err.Error(), "requires -repo-root") {
		t.Fatalf("expected repo-root requirement error, got %v", err)
	}
}

func TestMaybeSeedRAGFixtureCorpus_UsesLocalRuntimePaths(t *testing.T) {
	repoRoot := t.TempDir()
	pythonExecutablePath := filepath.Join(repoRoot, ".cache", "memorybench-venv", "bin", "python")
	if err := os.MkdirAll(filepath.Dir(pythonExecutablePath), 0o755); err != nil {
		t.Fatalf("mkdir python executable parent: %v", err)
	}
	if err := os.WriteFile(pythonExecutablePath, []byte("#!/bin/sh\ncat >/dev/null\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake python executable: %v", err)
	}
	helperScriptPath := filepath.Join(repoRoot, "cmd", "memorybench", "rag_search.py")
	if err := os.MkdirAll(filepath.Dir(helperScriptPath), 0o755); err != nil {
		t.Fatalf("mkdir helper script parent: %v", err)
	}
	if err := os.WriteFile(helperScriptPath, []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatalf("write fake helper script: %v", err)
	}

	if err := maybeSeedRAGFixtureCorpus(t.Context(), repoRoot, "rag_baseline", memorybench.RAGBaselineConfig{
		QdrantURL:      "http://127.0.0.1:6333",
		CollectionName: "memorybench_default",
	}); err != nil {
		t.Fatalf("maybeSeedRAGFixtureCorpus: %v", err)
	}
}

func TestMaybeSeedRAGFixtureCorpus_UsesIsolatedCollectionName(t *testing.T) {
	repoRoot := t.TempDir()
	pythonExecutablePath := filepath.Join(repoRoot, ".cache", "memorybench-venv", "bin", "python")
	if err := os.MkdirAll(filepath.Dir(pythonExecutablePath), 0o755); err != nil {
		t.Fatalf("mkdir python executable parent: %v", err)
	}
	if err := os.WriteFile(pythonExecutablePath, []byte("#!/bin/sh\nprintf '%s' \"$@\" > \"$TEST_ARGS_OUT\"\ncat >/dev/null\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake python executable: %v", err)
	}
	helperScriptPath := filepath.Join(repoRoot, "cmd", "memorybench", "rag_search.py")
	if err := os.MkdirAll(filepath.Dir(helperScriptPath), 0o755); err != nil {
		t.Fatalf("mkdir helper script parent: %v", err)
	}
	if err := os.WriteFile(helperScriptPath, []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatalf("write fake helper script: %v", err)
	}
	argsOutputPath := filepath.Join(repoRoot, "seed-args.txt")
	originalEnvironment := os.Getenv("TEST_ARGS_OUT")
	t.Cleanup(func() {
		if originalEnvironment == "" {
			_ = os.Unsetenv("TEST_ARGS_OUT")
			return
		}
		_ = os.Setenv("TEST_ARGS_OUT", originalEnvironment)
	})
	if err := os.Setenv("TEST_ARGS_OUT", argsOutputPath); err != nil {
		t.Fatalf("set TEST_ARGS_OUT: %v", err)
	}

	isolatedConfig, err := isolateRAGBenchmarkConfig(memorybench.BackendRAGStronger, memorybench.CandidateGovernanceContinuityTCL, "run_integrity", memorybench.RAGBaselineConfig{
		QdrantURL:      "http://127.0.0.1:6333",
		CollectionName: "memorybench_rerank",
		RerankerName:   "Xenova/ms-marco-MiniLM-L-6-v2",
	})
	if err != nil {
		t.Fatalf("isolateRAGBenchmarkConfig: %v", err)
	}
	if err := maybeSeedRAGFixtureCorpus(t.Context(), repoRoot, "rag_stronger", isolatedConfig); err != nil {
		t.Fatalf("maybeSeedRAGFixtureCorpus: %v", err)
	}
	argsBytes, err := os.ReadFile(argsOutputPath)
	if err != nil {
		t.Fatalf("read args output: %v", err)
	}
	argsText := string(argsBytes)
	if !strings.Contains(argsText, "--collection") || !strings.Contains(argsText, isolatedConfig.CollectionName) {
		t.Fatalf("expected isolated collection in seed args, got %q", argsText)
	}
}
