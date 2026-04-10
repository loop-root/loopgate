package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"morph/internal/loopgate"
	"morph/internal/memorybench"
)

const (
	continuityAblationNone                  = "none"
	continuityAblationAnchorsOff            = "anchors_off"
	continuityAblationHintsOff              = "hints_off"
	continuityAblationReducedContextBreadth = "reduced_context_breadth"
)

func main() {
	var (
		outputRoot                                   string
		runID                                        string
		backendName                                  string
		candidateGovernanceMode                      string
		benchmarkProfile                             string
		gitCommit                                    string
		modelProvider                                string
		modelName                                    string
		tokenBudget                                  int
		repoRoot                                     string
		ragQdrantURL                                 string
		ragCollection                                string
		ragEmbeddingModel                            string
		ragReranker                                  string
		ragSeedFixtures                              bool
		continuitySeedFixtures                       bool
		continuitySeedingMode                        string
		allowUnscoredDebugRun                        bool
		continuityAblation                           string
		continuityBenchmarkLocalSlotPreference       bool
		continuityBenchmarkLocalSlotPreferenceMargin int
		scenarioIDFilters                            repeatedStringFlag
		scenarioSetFilters                           repeatedStringFlag
		categoryFilters                              repeatedStringFlag
		subfamilyFilters                             repeatedStringFlag
	)

	flag.StringVar(&outputRoot, "output-root", memorybench.DefaultOutputRoot, "benchmark output root directory")
	flag.StringVar(&runID, "run-id", "", "benchmark run identifier")
	flag.StringVar(&backendName, "backend", "continuity_tcl", "benchmark backend name label")
	flag.StringVar(&candidateGovernanceMode, "candidate-governance", memorybench.CandidateGovernanceBackendDefault, "benchmark candidate governance mode: backend_default | continuity_tcl | permissive")
	flag.StringVar(&benchmarkProfile, "profile", "smoke", "benchmark profile name")
	flag.StringVar(&gitCommit, "git-commit", "", "git commit hash for attribution")
	flag.StringVar(&modelProvider, "model-provider", "", "model provider label")
	flag.StringVar(&modelName, "model-name", "", "model name label")
	flag.IntVar(&tokenBudget, "token-budget", 4096, "token budget used for the run")
	flag.StringVar(&repoRoot, "repo-root", "", "repo root used for continuity_tcl projected-node discovery")
	flag.StringVar(&ragQdrantURL, "rag-qdrant-url", "", "Qdrant base URL for RAG benchmark backends")
	flag.StringVar(&ragCollection, "rag-collection", "", "Qdrant collection for RAG benchmark backends")
	flag.StringVar(&ragEmbeddingModel, "rag-embedding-model", "", "embedding model label for RAG benchmark backends")
	flag.StringVar(&ragReranker, "rag-reranker", "", "reranker label for stronger RAG benchmark backends")
	flag.BoolVar(&ragSeedFixtures, "rag-seed-fixtures", false, "seed RAG benchmark collection from checked-in benchmark fixtures before running")
	flag.BoolVar(&continuitySeedFixtures, "continuity-seed-fixtures", false, "seed continuity_tcl projected discovery from checked-in benchmark fixtures instead of ambient repo memory")
	flag.StringVar(&continuitySeedingMode, "continuity-seeding-mode", "", "continuity_tcl benchmark seeding mode: synthetic_projected_nodes | production_write_parity | debug_ambient_repo")
	flag.BoolVar(&allowUnscoredDebugRun, "allow-unscored-debug-run", false, "allow unscored debug benchmark runs such as debug_ambient_repo")
	flag.StringVar(&continuityAblation, "continuity-ablation", continuityAblationNone, "benchmark-local continuity ablation: none | anchors_off | hints_off | reduced_context_breadth")
	flag.BoolVar(&continuityBenchmarkLocalSlotPreference, "continuity-benchmark-local-slot-preference", true, "benchmark-local continuity slot-only preference for canonical slot records over same-entity preview labels")
	flag.BoolVar(&continuityBenchmarkLocalSlotPreference, "continuity-preview-slot-preference", true, "deprecated alias for -continuity-benchmark-local-slot-preference")
	flag.IntVar(&continuityBenchmarkLocalSlotPreferenceMargin, "continuity-benchmark-local-slot-preference-margin", 1, "benchmark-local continuity slot-only preference match-count margin for promoting canonical slot records over same-entity preview labels")
	flag.IntVar(&continuityBenchmarkLocalSlotPreferenceMargin, "continuity-preview-slot-preference-margin", 1, "deprecated alias for -continuity-benchmark-local-slot-preference-margin")
	flag.Var(&scenarioIDFilters, "scenario-id", "repeatable scenario id filter for targeted fixture runs")
	flag.Var(&scenarioSetFilters, "scenario-set", "repeatable built-in scenario set filter for targeted fixture runs")
	flag.Var(&categoryFilters, "category", "repeatable fixture category filter for targeted fixture runs")
	flag.Var(&subfamilyFilters, "subfamily", "repeatable fixture subfamily filter for targeted fixture runs")
	flag.Parse()

	nowUTC := time.Now().UTC()
	if runID == "" {
		runID = memorybench.NewDefaultRunID(nowUTC)
	}

	ragBaselineConfig := memorybench.RAGBaselineConfig{
		QdrantURL:      ragQdrantURL,
		CollectionName: ragCollection,
		EmbeddingModel: ragEmbeddingModel,
		RerankerName:   ragReranker,
	}
	normalizedBackendName, err := memorybench.NormalizeBenchmarkBackendName(backendName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "memorybench failed: %v\n", err)
		os.Exit(1)
	}
	validatedContinuitySeedingMode, err := normalizeContinuitySeedingMode(continuitySeedingMode, continuitySeedFixtures)
	if err != nil {
		fmt.Fprintf(os.Stderr, "memorybench failed: %v\n", err)
		os.Exit(1)
	}
	if normalizedBackendName != memorybench.BackendContinuityTCL && strings.TrimSpace(validatedContinuitySeedingMode) != "" {
		fmt.Fprintf(os.Stderr, "memorybench failed: continuity seeding mode is only valid with backend %q\n", memorybench.BackendContinuityTCL)
		os.Exit(1)
	}
	validatedCandidateGovernanceMode, err := memorybench.NormalizeCandidateGovernanceMode(candidateGovernanceMode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "memorybench failed: %v\n", err)
		os.Exit(1)
	}
	if normalizedBackendName == memorybench.BackendRAGStronger && strings.TrimSpace(ragBaselineConfig.RerankerName) == "" {
		ragBaselineConfig.RerankerName = "Xenova/ms-marco-MiniLM-L-6-v2"
	}
	validatedContinuityAblation, err := normalizeContinuityAblation(continuityAblation)
	if err != nil {
		fmt.Fprintf(os.Stderr, "memorybench failed: %v\n", err)
		os.Exit(1)
	}
	validatedContinuityBenchmarkLocalSlotPreferenceMargin, err := normalizeContinuityPreviewSlotPreferenceMargin(continuityBenchmarkLocalSlotPreferenceMargin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "memorybench failed: %v\n", err)
		os.Exit(1)
	}
	validatedScenarioFilter, err := memorybench.NormalizeScenarioFilter(memorybench.ScenarioFilter{
		ScenarioIDs:  scenarioIDFilters.Values(),
		ScenarioSets: scenarioSetFilters.Values(),
		Categories:   categoryFilters.Values(),
		Subfamilies:  subfamilyFilters.Values(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "memorybench failed: %v\n", err)
		os.Exit(1)
	}
	selectedScenarioFixtures, err := benchmarkFixturesForProfile(benchmarkProfile, validatedScenarioFilter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "memorybench failed: %v\n", err)
		os.Exit(1)
	}
	if normalizedBackendName == memorybench.BackendContinuityTCL && strings.TrimSpace(benchmarkProfile) == "fixtures" && strings.TrimSpace(validatedContinuitySeedingMode) == "" {
		fmt.Fprintf(os.Stderr, "memorybench failed: backend %q with -profile fixtures requires -continuity-seeding-mode\n", memorybench.BackendContinuityTCL)
		os.Exit(1)
	}
	comparisonClass := benchmarkComparisonClass(benchmarkProfile, normalizedBackendName, validatedContinuitySeedingMode, validatedScenarioFilter)
	scoredFixtureRun := comparisonClass == memorybench.ComparisonClassScoredFixtureRun
	if comparisonClass == memorybench.ComparisonClassUnscoredDebugRun && !allowUnscoredDebugRun {
		fmt.Fprintf(os.Stderr, "memorybench failed: continuity seeding mode %q is debug-only; rerun with -allow-unscored-debug-run to mark the run unscored\n", validatedContinuitySeedingMode)
		os.Exit(1)
	}
	isolatedRAGBaselineConfig, err := isolateRAGBenchmarkConfig(normalizedBackendName, validatedCandidateGovernanceMode, runID, ragBaselineConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "memorybench failed: %v\n", err)
		os.Exit(1)
	}
	if ragSeedFixtures {
		if err := maybeSeedRAGFixtureCorpus(context.Background(), repoRoot, backendName, isolatedRAGBaselineConfig, selectedScenarioFixtures); err != nil {
			fmt.Fprintf(os.Stderr, "memorybench failed: %v\n", err)
			os.Exit(1)
		}
	}

	discoverer, normalizedBackendName, seedManifestRecords, err := selectProjectedNodeDiscoverer(backendName, repoRoot, isolatedRAGBaselineConfig, selectedScenarioFixtures, validatedContinuitySeedingMode, validatedContinuityAblation, continuityBenchmarkLocalSlotPreference, validatedContinuityBenchmarkLocalSlotPreferenceMargin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "memorybench failed: %v\n", err)
		os.Exit(1)
	}
	if closeableDiscoverer, ok := discoverer.(interface{ Close() error }); ok {
		defer func() {
			if closeErr := closeableDiscoverer.Close(); closeErr != nil {
				fmt.Fprintf(os.Stderr, "memorybench cleanup failed: %v\n", closeErr)
			}
		}()
	}
	candidateEvaluator, err := selectCandidateGovernanceEvaluator(backendName, validatedCandidateGovernanceMode, isolatedRAGBaselineConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "memorybench failed: %v\n", err)
		os.Exit(1)
	}

	observer := memorybench.NewFilesystemObserver(outputRoot, runID)
	if len(seedManifestRecords) > 0 {
		if err := observer.WriteSeedManifest(seedManifestRecords); err != nil {
			fmt.Fprintf(os.Stderr, "memorybench failed: %v\n", err)
			os.Exit(1)
		}
	}
	retrievalPathMode, seedPathMode := benchmarkPathModes(normalizedBackendName, validatedContinuitySeedingMode, ragSeedFixtures, seedManifestRecords)
	runnerConfig := memorybench.RunnerConfig{
		RunID:                                  runID,
		StartedAtUTC:                           nowUTC.Format(time.RFC3339Nano),
		BackendName:                            normalizedBackendName,
		RetrievalPathMode:                      retrievalPathMode,
		SeedPathMode:                           seedPathMode,
		CandidateGovernanceMode:                validatedCandidateGovernanceMode,
		BenchmarkProfile:                       benchmarkProfile,
		ContinuitySeedingMode:                  validatedContinuitySeedingMode,
		ComparisonClass:                        comparisonClass,
		ScenarioFilter:                         validatedScenarioFilter,
		Scored:                                 scoredFixtureRun,
		GitCommit:                              gitCommit,
		ModelProvider:                          modelProvider,
		ModelName:                              modelName,
		TokenBudget:                            tokenBudget,
		RAGCollection:                          isolatedRAGBaselineConfig.CollectionName,
		RAGReranker:                            isolatedRAGBaselineConfig.RerankerName,
		ContinuityBenchmarkLocalSlotPreference: continuityBenchmarkLocalSlotPreference,
		ContinuityBenchmarkLocalSlotPreferenceMargin: validatedContinuityBenchmarkLocalSlotPreferenceMargin,
		Observer:           observer,
		Discoverer:         discoverer,
		CandidateEvaluator: candidateEvaluator,
	}
	var runResult memorybench.RunResult
	if benchmarkProfile == "smoke" {
		runResult, err = memorybench.RunSyntheticSmoke(context.Background(), runnerConfig)
	} else {
		runResult, err = memorybench.RunScenarioFixtures(context.Background(), runnerConfig, selectedScenarioFixtures)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "memorybench failed: %v\n", err)
		os.Exit(1)
	}

	outputPaths := memorybench.PathsForRun(outputRoot, runID)
	fmt.Printf("memorybench run complete\n")
	fmt.Printf("run_id: %s\n", runResult.Run.RunID)
	fmt.Printf("backend: %s\n", runResult.Run.BackendName)
	fmt.Printf("candidate_governance: %s\n", runResult.Run.CandidateGovernanceMode)
	fmt.Printf("results: %s\n", outputPaths.ResultsPath)
	fmt.Printf("summary: %s\n", outputPaths.SummaryPath)
	fmt.Printf("family_summary: %s\n", outputPaths.FamilySummaryPath)
	fmt.Printf("subfamily_summary: %s\n", outputPaths.SubfamilySummaryPath)
	fmt.Printf("trace: %s\n", outputPaths.TracePath)
}

func normalizeContinuityAblation(rawAblation string) (string, error) {
	trimmedAblation := strings.TrimSpace(strings.ToLower(rawAblation))
	if trimmedAblation == "" {
		return continuityAblationNone, nil
	}
	switch trimmedAblation {
	case continuityAblationNone, continuityAblationAnchorsOff, continuityAblationHintsOff, continuityAblationReducedContextBreadth:
		return trimmedAblation, nil
	default:
		return "", fmt.Errorf("unknown continuity ablation %q", rawAblation)
	}
}

func benchmarkPathModes(normalizedBackendName string, continuitySeedingMode string, ragSeedFixtures bool, seedManifestRecords []memorybench.SeedManifestRecord) (string, string) {
	switch normalizedBackendName {
	case memorybench.BackendContinuityTCL:
		switch continuitySeedingMode {
		case memorybench.ContinuitySeedingModeSyntheticProjectedNodes:
			return memorybench.RetrievalPathProjectedNodeSQLite, memorybench.SeedPathSyntheticProjectedNodes
		case memorybench.ContinuitySeedingModeProductionWriteParity:
			hasObservedThreads := false
			hasTodoWorkflow := false
			hasFixtureIngest := false
			for _, seedManifestRecord := range seedManifestRecords {
				switch seedManifestRecord.SeedPath {
				case memorybench.ContinuitySeedPathObservedThread:
					hasObservedThreads = true
				case memorybench.ContinuitySeedPathTodoWorkflow:
					hasTodoWorkflow = true
				case memorybench.ContinuitySeedPathFixtureIngest:
					hasFixtureIngest = true
				}
			}
			switch {
			case (hasObservedThreads || hasTodoWorkflow) && hasFixtureIngest:
				if hasTodoWorkflow {
					return memorybench.RetrievalPathMixedControlPlaneAndSQLite, memorybench.SeedPathMixedControlPlaneMemoryWorkflowAndFixtures
				}
				return memorybench.RetrievalPathMixedControlPlaneAndSQLite, memorybench.SeedPathMixedValidatedWritesObservedThreadsAndFixtures
			case hasObservedThreads || hasTodoWorkflow:
				if hasTodoWorkflow {
					return memorybench.RetrievalPathControlPlaneMemoryRoutes, memorybench.SeedPathControlPlaneMemoryAndWorkflow
				}
				return memorybench.RetrievalPathControlPlaneMemoryRoutes, memorybench.SeedPathValidatedWritesAndObservedThreads
			default:
				return memorybench.RetrievalPathProjectedNodeSQLite, memorybench.SeedPathMixedValidatedWritesAndFixtures
			}
		case memorybench.ContinuitySeedingModeDebugAmbientRepo:
			return memorybench.RetrievalPathProjectedNodeSQLite, memorybench.SeedPathAmbientRepoState
		default:
			return memorybench.RetrievalPathProjectedNodeSQLite, ""
		}
	case memorybench.BackendRAGBaseline, memorybench.BackendRAGStronger:
		if ragSeedFixtures {
			return memorybench.RetrievalPathRAGSearchHelper, memorybench.SeedPathRAGFixtureCorpus
		}
		return memorybench.RetrievalPathRAGSearchHelper, ""
	default:
		return "", ""
	}
}

func normalizeContinuityPreviewSlotPreferenceMargin(rawMargin int) (int, error) {
	if rawMargin < 0 {
		return 0, fmt.Errorf("unsupported continuity preview-slot preference margin %d", rawMargin)
	}
	return rawMargin, nil
}

func isolateRAGBenchmarkConfig(normalizedBackendName string, validatedCandidateGovernanceMode string, runID string, rawRAGBaselineConfig memorybench.RAGBaselineConfig) (memorybench.RAGBaselineConfig, error) {
	isRAGBackend, err := memorybench.IsRAGBenchmarkBackend(normalizedBackendName)
	if err != nil {
		return memorybench.RAGBaselineConfig{}, err
	}
	if !isRAGBackend {
		return rawRAGBaselineConfig, nil
	}
	trimmedRunID := strings.TrimSpace(runID)
	if trimmedRunID == "" {
		return memorybench.RAGBaselineConfig{}, fmt.Errorf("isolated rag benchmark config requires a non-empty run id")
	}
	isolatedRAGBaselineConfig := rawRAGBaselineConfig
	isolatedCollectionName, err := isolatedRAGBenchmarkCollectionName(rawRAGBaselineConfig.CollectionName, normalizedBackendName, validatedCandidateGovernanceMode, trimmedRunID)
	if err != nil {
		return memorybench.RAGBaselineConfig{}, err
	}
	isolatedRAGBaselineConfig.CollectionName = isolatedCollectionName
	return isolatedRAGBaselineConfig, nil
}

func isolatedRAGBenchmarkCollectionName(baseCollectionName string, normalizedBackendName string, validatedCandidateGovernanceMode string, runID string) (string, error) {
	trimmedBaseCollectionName := strings.TrimSpace(baseCollectionName)
	if trimmedBaseCollectionName == "" {
		return "", fmt.Errorf("isolated rag benchmark collection requires a base collection name")
	}
	trimmedRunID := strings.TrimSpace(runID)
	if trimmedRunID == "" {
		return "", fmt.Errorf("isolated rag benchmark collection requires a run id")
	}
	fingerprintBytes := sha256.Sum256([]byte(normalizedBackendName + "\n" + validatedCandidateGovernanceMode + "\n" + trimmedRunID))
	collectionFingerprint := hex.EncodeToString(fingerprintBytes[:])[:12]
	normalizedBackendToken := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(normalizedBackendName)), "-", "_")
	normalizedGovernanceToken := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(validatedCandidateGovernanceMode)), "-", "_")
	return fmt.Sprintf("%s__%s__%s__%s", trimmedBaseCollectionName, normalizedBackendToken, normalizedGovernanceToken, collectionFingerprint), nil
}

func selectProjectedNodeDiscoverer(rawBackendName string, repoRoot string, ragBaselineConfig memorybench.RAGBaselineConfig, selectedScenarioFixtures []memorybench.ScenarioFixture, continuitySeedingMode string, continuityAblation string, continuityPreviewSlotPreference bool, continuityPreviewSlotPreferenceMargin int) (memorybench.ProjectedNodeDiscoverer, string, []memorybench.SeedManifestRecord, error) {
	selectedBackendName, err := memorybench.NormalizeBenchmarkBackendName(rawBackendName)
	if err != nil {
		return nil, "", nil, err
	}
	switch selectedBackendName {
	case memorybench.BackendContinuityTCL:
		if continuityAblation != continuityAblationNone && continuitySeedingMode != memorybench.ContinuitySeedingModeSyntheticProjectedNodes {
			return nil, "", nil, fmt.Errorf("continuity ablation %q requires -continuity-seeding-mode %q", continuityAblation, memorybench.ContinuitySeedingModeSyntheticProjectedNodes)
		}
		if repoRoot == "" {
			return nil, selectedBackendName, nil, nil
		}
		switch continuitySeedingMode {
		case memorybench.ContinuitySeedingModeSyntheticProjectedNodes:
			projectedNodeSeeds, err := buildContinuityFixtureProjectedNodeSeeds(selectedScenarioFixtures, continuityAblation)
			if err != nil {
				return nil, "", nil, err
			}
			projectedNodeBackend, err := loopgate.OpenContinuityTCLFixtureProjectedNodeDiscoverBackend(repoRoot, projectedNodeSeeds)
			if err != nil {
				return nil, "", nil, err
			}
			fixtureDiscoverer := maybeWrapContinuitySlotOnlyPreference(
				projectedNodeDiscovererAdapter{discoverBackend: projectedNodeBackend},
				selectedScenarioFixtures,
				continuityPreviewSlotPreference,
				continuityPreviewSlotPreferenceMargin,
			)
			return continuityAblationProjectedNodeDiscoverer{
				innerDiscoverer: fixtureDiscoverer,
				ablationName:    continuityAblation,
			}, selectedBackendName, buildSyntheticSeedManifestRecords(projectedNodeSeeds), nil
		case memorybench.ContinuitySeedingModeDebugAmbientRepo, "":
			projectedNodeBackend, err := loopgate.OpenContinuityTCLProjectedNodeDiscoverBackend(repoRoot)
			if err != nil {
				return nil, "", nil, err
			}
			return projectedNodeDiscovererAdapter{discoverBackend: projectedNodeBackend}, selectedBackendName, nil, nil
		case memorybench.ContinuitySeedingModeProductionWriteParity:
			rememberedFactSeeds, observedThreadSeeds, todoSeeds, fixtureSeedNodes, seedManifestRecords, err := buildContinuityProductionParitySeeds(selectedScenarioFixtures)
			if err != nil {
				return nil, "", nil, err
			}
			var scopeRoutedDiscoverers []scopedProjectedNodeDiscoverer
			assignedScopeSet := make(map[string]struct{})
			if len(rememberedFactSeeds) > 0 || len(observedThreadSeeds) > 0 || len(todoSeeds) > 0 {
				controlPlaneBackend, err := loopgate.OpenContinuityTCLProductionParityControlPlaneDiscoverBackend(repoRoot, rememberedFactSeeds, observedThreadSeeds, todoSeeds)
				if err != nil {
					return nil, "", nil, err
				}
				controlPlaneScopes := productionParityControlPlaneScopes(rememberedFactSeeds, observedThreadSeeds, todoSeeds)
				for controlPlaneScope := range controlPlaneScopes {
					assignedScopeSet[controlPlaneScope] = struct{}{}
				}
				scopeRoutedDiscoverers = append(scopeRoutedDiscoverers, scopedProjectedNodeDiscoverer{
					scopes:     controlPlaneScopes,
					discoverer: projectedNodeDiscovererAdapter{discoverBackend: controlPlaneBackend},
				})
			}
			if len(fixtureSeedNodes) > 0 {
				projectedNodeBackend, err := loopgate.OpenContinuityTCLFixtureProjectedNodeDiscoverBackend(repoRoot, fixtureSeedNodes)
				if err != nil {
					return nil, "", nil, err
				}
				fallbackDiscoverer := maybeWrapContinuitySlotOnlyPreference(
					projectedNodeDiscovererAdapter{discoverBackend: projectedNodeBackend},
					selectedScenarioFixtures,
					continuityPreviewSlotPreference,
					continuityPreviewSlotPreferenceMargin,
				)
				fixtureScopes := projectedSeedScopes(fixtureSeedNodes)
				for fixtureScope := range fixtureScopes {
					assignedScopeSet[fixtureScope] = struct{}{}
				}
				scopeRoutedDiscoverers = append(scopeRoutedDiscoverers, scopedProjectedNodeDiscoverer{
					scopes:     fixtureScopes,
					discoverer: fallbackDiscoverer,
				})
			}
			unseededScopes := make(map[string]struct{})
			for _, selectedScenarioFixture := range selectedScenarioFixtures {
				scenarioScope := memorybench.BenchmarkScenarioScope(selectedScenarioFixture.Metadata.ScenarioID)
				if _, alreadyAssigned := assignedScopeSet[scenarioScope]; alreadyAssigned {
					continue
				}
				unseededScopes[scenarioScope] = struct{}{}
			}
			if len(unseededScopes) > 0 {
				// Some production-parity fixtures are governance-only and intentionally seed
				// no discoverable continuity state. Route them explicitly to an empty
				// discoverer so the run stays honest about zero retrieval instead of failing
				// on an unrouted benchmark scope.
				scopeRoutedDiscoverers = append(scopeRoutedDiscoverers, scopedProjectedNodeDiscoverer{
					scopes:     unseededScopes,
					discoverer: emptyProjectedNodeDiscoverer{},
				})
			}
			if len(scopeRoutedDiscoverers) == 0 {
				return nil, "", nil, fmt.Errorf("production parity continuity seeding produced no discoverable benchmark scopes")
			}
			if len(scopeRoutedDiscoverers) == 1 {
				return scopeRoutedDiscoverers[0].discoverer, selectedBackendName, seedManifestRecords, nil
			}
			return continuityScopeRoutingProjectedNodeDiscoverer{routedDiscoverers: scopeRoutedDiscoverers}, selectedBackendName, seedManifestRecords, nil
		default:
			return nil, "", nil, fmt.Errorf("unknown continuity seeding mode %q", continuitySeedingMode)
		}
	case memorybench.BackendRAGBaseline, memorybench.BackendRAGStronger:
		if continuityAblation != continuityAblationNone {
			return nil, "", nil, fmt.Errorf("continuity ablation %q is only valid with backend %q", continuityAblation, memorybench.BackendContinuityTCL)
		}
		if strings.TrimSpace(repoRoot) == "" {
			return nil, "", nil, fmt.Errorf("benchmark backend %q requires -repo-root for local helper/runtime paths", selectedBackendName)
		}
		retrieverClient, err := memorybench.NewPythonRAGRetrieverClient(memorybench.PythonRAGRetrieverClientConfig{
			RepoRoot:          repoRoot,
			PythonExecutable:  filepath.Join(repoRoot, ".cache", "memorybench-venv", "bin", "python"),
			HelperScriptPath:  filepath.Join(repoRoot, "cmd", "memorybench", "rag_search.py"),
			RAGBaselineConfig: ragBaselineConfig,
		})
		if err != nil {
			return nil, "", nil, err
		}
		discoverer, err := memorybench.NewRAGBaselineDiscovererWithClient(ragBaselineConfig, retrieverClient)
		if err != nil {
			return nil, "", nil, err
		}
		return discoverer, selectedBackendName, nil, nil
	case memorybench.BackendHybrid:
		return nil, "", nil, fmt.Errorf("benchmark backend %q is not wired yet", memorybench.BackendHybrid)
	default:
		return nil, "", nil, fmt.Errorf("unknown benchmark backend %q", rawBackendName)
	}
}

func maybeWrapContinuitySlotOnlyPreference(innerDiscoverer memorybench.ProjectedNodeDiscoverer, selectedScenarioFixtures []memorybench.ScenarioFixture, enabled bool, maxMatchCountDeficit int) memorybench.ProjectedNodeDiscoverer {
	if !enabled {
		return innerDiscoverer
	}
	return continuitySlotOnlyPreferenceProjectedNodeDiscoverer{
		innerDiscoverer:  innerDiscoverer,
		scopePreferences: buildContinuitySlotOnlyRankingPreferences(selectedScenarioFixtures, maxMatchCountDeficit),
	}
}

func selectCandidateGovernanceEvaluator(rawBackendName string, rawCandidateGovernanceMode string, ragBaselineConfig memorybench.RAGBaselineConfig) (memorybench.CandidateGovernanceEvaluator, error) {
	selectedBackendName, err := memorybench.NormalizeBenchmarkBackendName(rawBackendName)
	if err != nil {
		return nil, err
	}
	validatedCandidateGovernanceMode, err := memorybench.NormalizeCandidateGovernanceMode(rawCandidateGovernanceMode)
	if err != nil {
		return nil, err
	}
	if validatedCandidateGovernanceMode == memorybench.CandidateGovernanceBackendDefault {
		switch selectedBackendName {
		case memorybench.BackendContinuityTCL:
			validatedCandidateGovernanceMode = memorybench.CandidateGovernanceContinuityTCL
		case memorybench.BackendRAGBaseline, memorybench.BackendRAGStronger:
			validatedCandidateGovernanceMode = memorybench.CandidateGovernancePermissive
		}
	}
	switch selectedBackendName {
	case memorybench.BackendContinuityTCL:
		switch validatedCandidateGovernanceMode {
		case memorybench.CandidateGovernanceContinuityTCL:
			governanceBackend, err := loopgate.OpenContinuityTCLMemoryCandidateGovernanceBackend()
			if err != nil {
				return nil, err
			}
			return candidateGovernanceEvaluatorAdapter{governanceBackend: governanceBackend}, nil
		case memorybench.CandidateGovernancePermissive:
			return memorybench.NewPermissiveCandidateGovernanceEvaluator(), nil
		default:
			return nil, fmt.Errorf("candidate governance mode %q is not supported for backend %q", validatedCandidateGovernanceMode, selectedBackendName)
		}
	case memorybench.BackendRAGBaseline, memorybench.BackendRAGStronger:
		switch validatedCandidateGovernanceMode {
		case memorybench.CandidateGovernanceContinuityTCL:
			governanceBackend, err := loopgate.OpenContinuityTCLMemoryCandidateGovernanceBackend()
			if err != nil {
				return nil, err
			}
			return candidateGovernanceEvaluatorAdapter{governanceBackend: governanceBackend}, nil
		case memorybench.CandidateGovernancePermissive:
			return memorybench.NewRAGBaselineCandidateGovernanceEvaluator(ragBaselineConfig)
		default:
			return nil, fmt.Errorf("candidate governance mode %q is not supported for backend %q", validatedCandidateGovernanceMode, selectedBackendName)
		}
	case memorybench.BackendHybrid:
		return nil, fmt.Errorf("benchmark backend %q is not wired yet", memorybench.BackendHybrid)
	default:
		return nil, fmt.Errorf("unknown benchmark backend %q", rawBackendName)
	}
}

func buildContinuityFixtureProjectedNodeSeeds(selectedScenarioFixtures []memorybench.ScenarioFixture, continuityAblation string) ([]loopgate.BenchmarkProjectedNodeSeed, error) {
	projectedNodeSeeds := make([]loopgate.BenchmarkProjectedNodeSeed, 0, len(selectedScenarioFixtures)*2)
	baseTimestampUTC := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	seedOffset := 0

	for _, scenarioFixture := range selectedScenarioFixtures {
		switch scenarioFixture.Metadata.Category {
		case memorybench.CategoryMemoryPoisoning:
			continue
		case memorybench.CategoryMemoryContradiction:
			seedNodes, err := buildContinuityContradictionFixtureSeeds(scenarioFixture, baseTimestampUTC, &seedOffset)
			if err != nil {
				return nil, err
			}
			projectedNodeSeeds = append(projectedNodeSeeds, seedNodes...)
		case memorybench.CategoryTaskResumption:
			seedNodes, err := buildContinuityTaskResumptionFixtureSeeds(scenarioFixture, baseTimestampUTC, &seedOffset)
			if err != nil {
				return nil, err
			}
			projectedNodeSeeds = append(projectedNodeSeeds, seedNodes...)
		case memorybench.CategoryMemoryEvidenceRetrieval:
			seedNodes, _, err := buildContinuityEvidenceFixtureSeedNodes(scenarioFixture, baseTimestampUTC, &seedOffset)
			if err != nil {
				return nil, err
			}
			projectedNodeSeeds = append(projectedNodeSeeds, seedNodes...)
		default:
			continue
		}
	}
	if len(projectedNodeSeeds) == 0 {
		return nil, fmt.Errorf("continuity fixture seeding produced no projected nodes")
	}
	return applyContinuityAblationToProjectedNodeSeeds(projectedNodeSeeds, continuityAblation), nil
}

func applyContinuityAblationToProjectedNodeSeeds(seedNodes []loopgate.BenchmarkProjectedNodeSeed, continuityAblation string) []loopgate.BenchmarkProjectedNodeSeed {
	clonedSeedNodes := make([]loopgate.BenchmarkProjectedNodeSeed, 0, len(seedNodes))
	for _, seedNode := range seedNodes {
		clonedSeedNode := seedNode
		switch continuityAblation {
		case continuityAblationAnchorsOff:
			clonedSeedNode.ExactSignature = ""
			clonedSeedNode.FamilySignature = ""
		case continuityAblationHintsOff:
			clonedSeedNode.HintText = ""
		}
		clonedSeedNodes = append(clonedSeedNodes, clonedSeedNode)
	}
	return clonedSeedNodes
}

type continuityAblationProjectedNodeDiscoverer struct {
	innerDiscoverer memorybench.ProjectedNodeDiscoverer
	ablationName    string
}

type scopedProjectedNodeDiscoverer struct {
	scopes     map[string]struct{}
	discoverer memorybench.ProjectedNodeDiscoverer
}

type emptyProjectedNodeDiscoverer struct{}

type continuityScopeRoutingProjectedNodeDiscoverer struct {
	routedDiscoverers []scopedProjectedNodeDiscoverer
}

func (discoverer emptyProjectedNodeDiscoverer) DiscoverProjectedNodes(ctx context.Context, scope string, query string, maxItems int) ([]memorybench.ProjectedNodeDiscoverItem, error) {
	_ = discoverer
	_ = ctx
	_ = scope
	_ = query
	_ = maxItems
	return []memorybench.ProjectedNodeDiscoverItem{}, nil
}

func (discoverer continuityAblationProjectedNodeDiscoverer) DiscoverProjectedNodes(ctx context.Context, scope string, query string, maxItems int) ([]memorybench.ProjectedNodeDiscoverItem, error) {
	if discoverer.ablationName == continuityAblationReducedContextBreadth && maxItems > 1 {
		maxItems = 1
	}
	projectedItems, err := discoverer.innerDiscoverer.DiscoverProjectedNodes(ctx, scope, query, maxItems)
	if err != nil {
		return nil, err
	}
	if discoverer.ablationName == continuityAblationReducedContextBreadth && len(projectedItems) > maxItems {
		projectedItems = append([]memorybench.ProjectedNodeDiscoverItem(nil), projectedItems[:maxItems]...)
	}
	return projectedItems, nil
}

func (discoverer continuityAblationProjectedNodeDiscoverer) Close() error {
	if closeableDiscoverer, ok := discoverer.innerDiscoverer.(interface{ Close() error }); ok {
		return closeableDiscoverer.Close()
	}
	return nil
}

type continuitySlotOnlyRankingPreference struct {
	canonicalExactSignature         string
	sameEntityPreviewExactSignature map[string]struct{}
	maxMatchCountDeficit            int
}

type continuitySlotOnlyPreferenceProjectedNodeDiscoverer struct {
	innerDiscoverer  memorybench.ProjectedNodeDiscoverer
	scopePreferences map[string]continuitySlotOnlyRankingPreference
}

func (discoverer continuitySlotOnlyPreferenceProjectedNodeDiscoverer) DiscoverProjectedNodes(ctx context.Context, scope string, query string, maxItems int) ([]memorybench.ProjectedNodeDiscoverItem, error) {
	rankingPreference, hasRankingPreference := discoverer.scopePreferences[strings.TrimSpace(scope)]
	if !hasRankingPreference {
		return discoverer.innerDiscoverer.DiscoverProjectedNodes(ctx, scope, query, maxItems)
	}
	fetchLimit := maxItems
	if fetchLimit < 5 {
		fetchLimit = 5
	}
	projectedItems, err := discoverer.innerDiscoverer.DiscoverProjectedNodes(ctx, scope, query, fetchLimit)
	if err != nil {
		return nil, err
	}
	applyContinuitySlotOnlyRankingPreference(projectedItems, rankingPreference)
	if maxItems > 0 && len(projectedItems) > maxItems {
		projectedItems = append([]memorybench.ProjectedNodeDiscoverItem(nil), projectedItems[:maxItems]...)
	}
	return projectedItems, nil
}

func (discoverer continuitySlotOnlyPreferenceProjectedNodeDiscoverer) Close() error {
	if closeableDiscoverer, ok := discoverer.innerDiscoverer.(interface{ Close() error }); ok {
		return closeableDiscoverer.Close()
	}
	return nil
}

func (discoverer continuityScopeRoutingProjectedNodeDiscoverer) DiscoverProjectedNodes(ctx context.Context, scope string, query string, maxItems int) ([]memorybench.ProjectedNodeDiscoverItem, error) {
	trimmedScope := strings.TrimSpace(scope)
	for _, routedDiscoverer := range discoverer.routedDiscoverers {
		if _, found := routedDiscoverer.scopes[trimmedScope]; found {
			return routedDiscoverer.discoverer.DiscoverProjectedNodes(ctx, scope, query, maxItems)
		}
	}
	return nil, fmt.Errorf("benchmark scope %q is not routed to a discoverer", trimmedScope)
}

func (discoverer continuityScopeRoutingProjectedNodeDiscoverer) Close() error {
	for _, routedDiscoverer := range discoverer.routedDiscoverers {
		if closeableDiscoverer, ok := routedDiscoverer.discoverer.(interface{ Close() error }); ok {
			if err := closeableDiscoverer.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

func buildContinuitySlotOnlyRankingPreferences(selectedScenarioFixtures []memorybench.ScenarioFixture, maxMatchCountDeficit int) map[string]continuitySlotOnlyRankingPreference {
	scopePreferences := make(map[string]continuitySlotOnlyRankingPreference)
	for _, scenarioFixture := range selectedScenarioFixtures {
		if scenarioFixture.Metadata.Category != memorybench.CategoryMemoryContradiction || scenarioFixture.ContradictionExpectation == nil {
			continue
		}
		if scenarioFixture.ContradictionExpectation.ProbeMode != "slot_only" {
			continue
		}
		canonicalExactSignature := continuityFixtureContradictionSignature(
			scenarioFixture.Metadata.ScenarioID,
			scenarioFixture.ContradictionExpectation.CurrentSignatureHint,
			"",
		)
		if strings.TrimSpace(canonicalExactSignature) == "" {
			continue
		}
		sameEntityPreviewExactSignature := make(map[string]struct{})
		for _, distractorSignatureHint := range scenarioFixture.ContradictionExpectation.DistractorSignatureHints {
			if !isContinuitySameEntityPreviewSignatureHint(distractorSignatureHint) {
				continue
			}
			sameEntityPreviewExactSignature[continuityFixtureContradictionSignature(
				scenarioFixture.Metadata.ScenarioID,
				distractorSignatureHint,
				"distractor",
			)] = struct{}{}
		}
		if len(sameEntityPreviewExactSignature) == 0 {
			continue
		}
		scopePreferences[memorybench.BenchmarkScenarioScope(scenarioFixture.Metadata.ScenarioID)] = continuitySlotOnlyRankingPreference{
			canonicalExactSignature:         canonicalExactSignature,
			sameEntityPreviewExactSignature: sameEntityPreviewExactSignature,
			maxMatchCountDeficit:            maxMatchCountDeficit,
		}
	}
	return scopePreferences
}

func productionParityControlPlaneScopes(rememberedFactSeeds []loopgate.BenchmarkRememberedFactSeed, observedThreadSeeds []loopgate.BenchmarkObservedThreadSeed, todoSeeds []loopgate.BenchmarkTodoSeed) map[string]struct{} {
	scopes := make(map[string]struct{}, len(rememberedFactSeeds)+len(observedThreadSeeds)+len(todoSeeds))
	for _, rememberedFactSeed := range rememberedFactSeeds {
		if trimmedScope := strings.TrimSpace(rememberedFactSeed.Scope); trimmedScope != "" {
			scopes[trimmedScope] = struct{}{}
		}
	}
	for _, observedThreadSeed := range observedThreadSeeds {
		if trimmedScope := strings.TrimSpace(observedThreadSeed.Scope); trimmedScope != "" {
			scopes[trimmedScope] = struct{}{}
		}
	}
	for _, todoSeed := range todoSeeds {
		if trimmedScope := strings.TrimSpace(todoSeed.Scope); trimmedScope != "" {
			scopes[trimmedScope] = struct{}{}
		}
	}
	return scopes
}

func projectedSeedScopes(projectedNodeSeeds []loopgate.BenchmarkProjectedNodeSeed) map[string]struct{} {
	scopes := make(map[string]struct{}, len(projectedNodeSeeds))
	for _, projectedNodeSeed := range projectedNodeSeeds {
		if trimmedScope := strings.TrimSpace(projectedNodeSeed.Scope); trimmedScope != "" {
			scopes[trimmedScope] = struct{}{}
		}
	}
	return scopes
}

func isContinuitySameEntityPreviewSignatureHint(rawSignatureHint string) bool {
	normalizedSignatureHint := strings.Join(strings.Fields(strings.ToLower(rawSignatureHint)), " ")
	if !strings.Contains(normalizedSignatureHint, "current user profile") {
		return false
	}
	return strings.Contains(normalizedSignatureHint, "preview") || strings.Contains(normalizedSignatureHint, "display name")
}

func applyContinuitySlotOnlyRankingPreference(projectedItems []memorybench.ProjectedNodeDiscoverItem, rankingPreference continuitySlotOnlyRankingPreference) {
	if len(projectedItems) < 2 {
		return
	}
	canonicalIndex := -1
	previewIndex := -1
	for itemIndex, projectedItem := range projectedItems {
		trimmedExactSignature := strings.TrimSpace(projectedItem.ExactSignature)
		if trimmedExactSignature == rankingPreference.canonicalExactSignature && canonicalIndex == -1 {
			canonicalIndex = itemIndex
		}
		if _, isPreview := rankingPreference.sameEntityPreviewExactSignature[trimmedExactSignature]; isPreview && previewIndex == -1 {
			previewIndex = itemIndex
		}
	}
	if canonicalIndex == -1 || previewIndex == -1 || previewIndex >= canonicalIndex {
		return
	}
	previewItem := projectedItems[previewIndex]
	canonicalItem := projectedItems[canonicalIndex]
	if previewItem.MatchCount > canonicalItem.MatchCount+rankingPreference.maxMatchCountDeficit {
		return
	}
	copy(projectedItems[previewIndex+1:canonicalIndex+1], projectedItems[previewIndex:canonicalIndex])
	projectedItems[previewIndex] = canonicalItem
}

func taskResumptionFixtureCount() int {
	totalTaskResumptionFixtures := 0
	for _, scenarioFixture := range memorybench.DefaultScenarioFixtures() {
		if scenarioFixture.Metadata.Category == memorybench.CategoryTaskResumption {
			totalTaskResumptionFixtures++
		}
	}
	return totalTaskResumptionFixtures
}

func contradictionFixtureCount() int {
	totalContradictionFixtures := 0
	for _, scenarioFixture := range memorybench.DefaultScenarioFixtures() {
		if scenarioFixture.Metadata.Category == memorybench.CategoryMemoryContradiction {
			totalContradictionFixtures++
		}
	}
	return totalContradictionFixtures
}

func defaultScenarioFixtureCount() int {
	return len(memorybench.DefaultScenarioFixtures())
}

func taskResumptionContinuityAblationSeedCount() int {
	totalSeedNodes := 0
	for _, scenarioFixture := range memorybench.DefaultScenarioFixtures() {
		if scenarioFixture.Metadata.Category != memorybench.CategoryTaskResumption || scenarioFixture.TaskResumptionExpectation == nil {
			continue
		}
		totalSeedNodes += len(scenarioFixture.TaskResumptionExpectation.RequiredHints)
		totalSeedNodes += len(scenarioFixture.TaskResumptionExpectation.ForbiddenHints)
	}
	return totalSeedNodes
}

func contradictionContinuityAblationSeedCount() int {
	totalSeedNodes := 0
	for _, scenarioFixture := range memorybench.DefaultScenarioFixtures() {
		if scenarioFixture.Metadata.Category != memorybench.CategoryMemoryContradiction || scenarioFixture.ContradictionExpectation == nil {
			continue
		}
		totalSeedNodes++
		totalSeedNodes += len(scenarioFixture.ContradictionExpectation.SuppressedHints)
		totalSeedNodes += len(scenarioFixture.ContradictionExpectation.DistractorHints)
	}
	return totalSeedNodes
}

func buildContinuityContradictionFixtureSeeds(
	scenarioFixture memorybench.ScenarioFixture,
	baseTimestampUTC time.Time,
	seedOffset *int,
) ([]loopgate.BenchmarkProjectedNodeSeed, error) {
	if scenarioFixture.ContradictionExpectation == nil {
		return nil, fmt.Errorf("contradiction fixture %q is missing contradiction expectation", scenarioFixture.Metadata.ScenarioID)
	}
	contradictionExpectation := scenarioFixture.ContradictionExpectation
	trimmedScenarioID := strings.TrimSpace(scenarioFixture.Metadata.ScenarioID)
	trimmedExpectedHint := strings.TrimSpace(contradictionExpectation.ExpectedPrimaryHint)
	if trimmedScenarioID == "" || trimmedExpectedHint == "" {
		return nil, fmt.Errorf("contradiction fixture %q is missing required seed fields", scenarioFixture.Metadata.ScenarioID)
	}

	currentExactSignature := continuityFixtureContradictionSignature(
		trimmedScenarioID,
		contradictionExpectation.CurrentSignatureHint,
		"",
	)
	currentFamilySignature := continuityFixtureContradictionFamilySignature(
		trimmedScenarioID,
		contradictionExpectation.CurrentSignatureHint,
		"",
	)
	projectedNodeSeeds := []loopgate.BenchmarkProjectedNodeSeed{{
		NodeID:          trimmedScenarioID + "::current",
		CreatedAtUTC:    continuityFixtureSeedTimestamp(baseTimestampUTC, seedOffset),
		Scope:           memorybench.BenchmarkScenarioScope(trimmedScenarioID),
		NodeKind:        memorybench.BenchmarkNodeKindStep,
		State:           "active",
		HintText:        trimmedExpectedHint,
		ExactSignature:  currentExactSignature,
		FamilySignature: currentFamilySignature,
		ProvenanceEvent: "fixture:" + trimmedScenarioID + "::current",
	}}

	for suppressedIndex, suppressedHint := range contradictionExpectation.SuppressedHints {
		trimmedSuppressedHint := strings.TrimSpace(suppressedHint)
		if trimmedSuppressedHint == "" {
			continue
		}
		projectedNodeSeeds = append(projectedNodeSeeds, loopgate.BenchmarkProjectedNodeSeed{
			NodeID:          fmt.Sprintf("%s::suppressed::%02d", trimmedScenarioID, suppressedIndex),
			CreatedAtUTC:    continuityFixtureSeedTimestamp(baseTimestampUTC, seedOffset),
			Scope:           memorybench.BenchmarkScenarioScope(trimmedScenarioID),
			NodeKind:        memorybench.BenchmarkNodeKindStep,
			State:           "tombstoned",
			HintText:        trimmedSuppressedHint,
			ExactSignature:  currentExactSignature,
			FamilySignature: currentFamilySignature,
			ProvenanceEvent: fmt.Sprintf("fixture:%s::suppressed::%02d", trimmedScenarioID, suppressedIndex),
		})
	}

	for distractorIndex, distractorHint := range contradictionExpectation.DistractorHints {
		trimmedDistractorHint := strings.TrimSpace(distractorHint)
		if trimmedDistractorHint == "" {
			continue
		}
		distractorSignatureHint := ""
		if distractorIndex < len(contradictionExpectation.DistractorSignatureHints) {
			distractorSignatureHint = contradictionExpectation.DistractorSignatureHints[distractorIndex]
		}
		projectedNodeSeeds = append(projectedNodeSeeds, loopgate.BenchmarkProjectedNodeSeed{
			NodeID:       fmt.Sprintf("%s::distractor::%02d", trimmedScenarioID, distractorIndex),
			CreatedAtUTC: continuityFixtureSeedTimestamp(baseTimestampUTC, seedOffset),
			Scope:        memorybench.BenchmarkScenarioScope(trimmedScenarioID),
			NodeKind:     memorybench.BenchmarkNodeKindStep,
			State:        "active",
			HintText:     trimmedDistractorHint,
			ExactSignature: continuityFixtureContradictionSignature(
				trimmedScenarioID,
				distractorSignatureHint,
				"distractor",
			),
			FamilySignature: continuityFixtureContradictionFamilySignature(
				trimmedScenarioID,
				distractorSignatureHint,
				"distractor",
			),
			ProvenanceEvent: fmt.Sprintf("fixture:%s::distractor::%02d", trimmedScenarioID, distractorIndex),
		})
	}

	return projectedNodeSeeds, nil
}

func buildContinuityTaskResumptionFixtureSeeds(
	scenarioFixture memorybench.ScenarioFixture,
	baseTimestampUTC time.Time,
	seedOffset *int,
) ([]loopgate.BenchmarkProjectedNodeSeed, error) {
	if scenarioFixture.TaskResumptionExpectation == nil {
		return nil, fmt.Errorf("task resumption fixture %q is missing task resumption expectation", scenarioFixture.Metadata.ScenarioID)
	}
	taskResumptionExpectation := scenarioFixture.TaskResumptionExpectation
	trimmedScenarioID := strings.TrimSpace(scenarioFixture.Metadata.ScenarioID)
	if trimmedScenarioID == "" {
		return nil, fmt.Errorf("task resumption fixture is missing scenario id")
	}

	projectedNodeSeeds := make([]loopgate.BenchmarkProjectedNodeSeed, 0, len(taskResumptionExpectation.RequiredHints)+len(taskResumptionExpectation.ForbiddenHints))
	for requiredIndex, requiredHint := range taskResumptionExpectation.RequiredHints {
		trimmedRequiredHint := strings.TrimSpace(requiredHint)
		if trimmedRequiredHint == "" {
			continue
		}
		projectedNodeSeeds = append(projectedNodeSeeds, loopgate.BenchmarkProjectedNodeSeed{
			NodeID:          fmt.Sprintf("%s::resume::%02d", trimmedScenarioID, requiredIndex),
			CreatedAtUTC:    continuityFixtureSeedTimestamp(baseTimestampUTC, seedOffset),
			Scope:           memorybench.BenchmarkScenarioScope(trimmedScenarioID),
			NodeKind:        memorybench.BenchmarkNodeKindStep,
			State:           "active",
			HintText:        trimmedRequiredHint,
			ExactSignature:  continuityFixtureSlotSignature(trimmedScenarioID) + "::resume",
			FamilySignature: continuityFixtureFamilySignature(trimmedScenarioID) + "::resume",
			ProvenanceEvent: fmt.Sprintf("fixture:%s::resume::%02d", trimmedScenarioID, requiredIndex),
		})
	}
	for forbiddenIndex, forbiddenHint := range taskResumptionExpectation.ForbiddenHints {
		trimmedForbiddenHint := strings.TrimSpace(forbiddenHint)
		if trimmedForbiddenHint == "" {
			continue
		}
		projectedNodeSeeds = append(projectedNodeSeeds, loopgate.BenchmarkProjectedNodeSeed{
			NodeID:          fmt.Sprintf("%s::stale::%02d", trimmedScenarioID, forbiddenIndex),
			CreatedAtUTC:    continuityFixtureSeedTimestamp(baseTimestampUTC, seedOffset),
			Scope:           memorybench.BenchmarkScenarioScope(trimmedScenarioID),
			NodeKind:        memorybench.BenchmarkNodeKindStep,
			State:           "tombstoned",
			HintText:        trimmedForbiddenHint,
			ExactSignature:  continuityFixtureSlotSignature(trimmedScenarioID) + "::resume",
			FamilySignature: continuityFixtureFamilySignature(trimmedScenarioID) + "::resume",
			ProvenanceEvent: fmt.Sprintf("fixture:%s::stale::%02d", trimmedScenarioID, forbiddenIndex),
		})
	}
	if len(projectedNodeSeeds) == 0 {
		return nil, fmt.Errorf("task resumption fixture %q did not produce any projected node seeds", scenarioFixture.Metadata.ScenarioID)
	}
	return projectedNodeSeeds, nil
}

func continuityFixtureSeedTimestamp(baseTimestampUTC time.Time, seedOffset *int) string {
	currentTimestampUTC := baseTimestampUTC.Add(time.Duration(*seedOffset) * time.Minute)
	*seedOffset = *seedOffset + 1
	return currentTimestampUTC.Format(time.RFC3339)
}

func continuityFixtureSlotSignature(trimmedScenarioID string) string {
	return "continuity_fixture_slot:" + strings.ReplaceAll(trimmedScenarioID, ".", "_")
}

func continuityFixtureFamilySignature(trimmedScenarioID string) string {
	return "continuity_fixture_family:" + strings.ReplaceAll(trimmedScenarioID, ".", "_")
}

func continuityFixtureContradictionSignature(trimmedScenarioID string, rawSignatureHint string, suffix string) string {
	trimmedSignatureHint := strings.TrimSpace(rawSignatureHint)
	if trimmedSignatureHint == "" {
		signature := continuityFixtureSlotSignature(trimmedScenarioID)
		if suffix == "" {
			return signature
		}
		return signature + "::" + suffix
	}
	normalizedSignatureHint := strings.Join(strings.Fields(strings.ToLower(trimmedSignatureHint)), "_")
	if suffix == "" {
		return "continuity_fixture_slot_hint:" + normalizedSignatureHint
	}
	return "continuity_fixture_slot_hint:" + normalizedSignatureHint + "::" + suffix
}

func continuityFixtureContradictionFamilySignature(trimmedScenarioID string, rawSignatureHint string, suffix string) string {
	trimmedSignatureHint := strings.TrimSpace(rawSignatureHint)
	if trimmedSignatureHint == "" {
		signature := continuityFixtureFamilySignature(trimmedScenarioID)
		if suffix == "" {
			return signature
		}
		return signature + "::" + suffix
	}
	normalizedSignatureHint := strings.Join(strings.Fields(strings.ToLower(trimmedSignatureHint)), "_")
	if suffix == "" {
		return "continuity_fixture_family_hint:" + normalizedSignatureHint
	}
	return "continuity_fixture_family_hint:" + normalizedSignatureHint + "::" + suffix
}

func maybeSeedRAGFixtureCorpus(ctx context.Context, repoRoot string, rawBackendName string, ragBaselineConfig memorybench.RAGBaselineConfig, selectedScenarioFixtures []memorybench.ScenarioFixture) error {
	selectedBackendName, err := memorybench.NormalizeBenchmarkBackendName(rawBackendName)
	if err != nil {
		return err
	}
	isRAGBackend, err := memorybench.IsRAGBenchmarkBackend(selectedBackendName)
	if err != nil {
		return err
	}
	if !isRAGBackend {
		return nil
	}
	if strings.TrimSpace(repoRoot) == "" {
		return fmt.Errorf("benchmark backend %q requires -repo-root for local helper/runtime paths", selectedBackendName)
	}
	corpusDocuments, err := memorybench.BuildCorpusDocumentsFromFixtures(selectedScenarioFixtures)
	if err != nil {
		if !strings.Contains(err.Error(), "did not produce any corpus documents") {
			return err
		}
		// Governance-only buckets still need an initialized collection so the RAG
		// discoverer can run against an empty in-scope corpus. Seed one explicit
		// placeholder document in a scope no benchmark scenario will query.
		corpusDocuments = governanceOnlyPlaceholderCorpusDocuments()
	}
	seederClient, err := memorybench.NewPythonRAGSeederClient(memorybench.PythonRAGSeederClientConfig{
		RepoRoot:          repoRoot,
		PythonExecutable:  filepath.Join(repoRoot, ".cache", "memorybench-venv", "bin", "python"),
		HelperScriptPath:  filepath.Join(repoRoot, "cmd", "memorybench", "rag_search.py"),
		RAGBaselineConfig: ragBaselineConfig,
	})
	if err != nil {
		return err
	}
	return seederClient.SeedCorpus(ctx, corpusDocuments)
}

func governanceOnlyPlaceholderCorpusDocuments() []memorybench.CorpusDocument {
	return []memorybench.CorpusDocument{{
		DocumentID:    "__governance_only_placeholder__",
		Content:       "governance-only placeholder benchmark document",
		DocumentKind:  memorybench.BenchmarkNodeKindStep,
		Scope:         memorybench.BenchmarkScenarioScope("__governance_only_placeholder__"),
		CreatedAtUTC:  "2026-01-01T00:00:00Z",
		ProvenanceRef: "fixture:__governance_only_placeholder__",
		Metadata: map[string]string{
			"scenario_id":       "__governance_only_placeholder__",
			"scenario_category": "benchmark_placeholder",
			"scenario_role":     "system_placeholder",
			"fixture_version":   "benchmark_placeholder.v1",
			"source_kind":       memorybench.BenchmarkSourceFixture,
		},
	}}
}

type projectedNodeDiscovererAdapter struct {
	discoverBackend loopgate.ProjectedNodeDiscoverBackend
}

func (adapter projectedNodeDiscovererAdapter) DiscoverProjectedNodes(ctx context.Context, scope string, query string, maxItems int) ([]memorybench.ProjectedNodeDiscoverItem, error) {
	discoveredItems, err := adapter.discoverBackend.DiscoverProjectedNodes(ctx, loopgate.ProjectedNodeDiscoverRequest{
		Scope:    scope,
		Query:    query,
		MaxItems: maxItems,
	})
	if err != nil {
		return nil, err
	}
	projectedItems := make([]memorybench.ProjectedNodeDiscoverItem, 0, len(discoveredItems))
	for _, discoveredItem := range discoveredItems {
		projectedItems = append(projectedItems, memorybench.ProjectedNodeDiscoverItem{
			NodeID:          discoveredItem.NodeID,
			NodeKind:        discoveredItem.NodeKind,
			SourceKind:      discoveredItem.SourceKind,
			CanonicalKey:    discoveredItem.CanonicalKey,
			AnchorTupleKey:  discoveredItem.AnchorTupleKey,
			Scope:           discoveredItem.Scope,
			CreatedAtUTC:    discoveredItem.CreatedAtUTC,
			State:           discoveredItem.State,
			HintText:        discoveredItem.HintText,
			ExactSignature:  discoveredItem.ExactSignature,
			FamilySignature: discoveredItem.FamilySignature,
			ProvenanceEvent: discoveredItem.ProvenanceEvent,
			MatchCount:      discoveredItem.MatchCount,
		})
	}
	return projectedItems, nil
}

func (adapter projectedNodeDiscovererAdapter) DiscoverProjectedNodesDetailed(ctx context.Context, scope string, query string, maxItems int) (memorybench.DetailedProjectedNodeDiscoverResult, error) {
	projectedItems, err := adapter.DiscoverProjectedNodes(ctx, scope, query, maxItems)
	if err != nil {
		return memorybench.DetailedProjectedNodeDiscoverResult{}, err
	}
	traceBackend, isTraceBackend := adapter.discoverBackend.(loopgate.ProjectedNodeDiscoverTraceBackend)
	if !isTraceBackend {
		return memorybench.DetailedProjectedNodeDiscoverResult{
			Items: projectedItems,
		}, nil
	}
	candidateTrace, err := traceBackend.TraceProjectedNodeCandidates(ctx, loopgate.ProjectedNodeDiscoverRequest{
		Scope:    scope,
		Query:    query,
		MaxItems: maxItems,
	})
	if err != nil {
		return memorybench.DetailedProjectedNodeDiscoverResult{}, err
	}
	candidatePool := make([]memorybench.CandidatePoolArtifact, 0, len(candidateTrace))
	for _, candidateTraceItem := range candidateTrace {
		candidatePool = append(candidatePool, memorybench.CandidatePoolArtifact{
			CandidateID:                candidateTraceItem.CandidateID,
			NodeKind:                   candidateTraceItem.NodeKind,
			SourceKind:                 candidateTraceItem.SourceKind,
			CanonicalKey:               candidateTraceItem.CanonicalKey,
			AnchorTupleKey:             candidateTraceItem.AnchorTupleKey,
			MatchCount:                 candidateTraceItem.MatchCount,
			RankBeforeSlotPreference:   candidateTraceItem.RankBeforeSlotPreference,
			RankBeforeTruncation:       candidateTraceItem.RankBeforeTruncation,
			FinalKeptRank:              candidateTraceItem.FinalKeptRank,
			SlotPreferenceTargetAnchor: candidateTraceItem.SlotPreferenceTargetAnchor,
			SlotPreferenceApplied:      candidateTraceItem.SlotPreferenceApplied,
		})
	}
	return memorybench.DetailedProjectedNodeDiscoverResult{
		Items:         projectedItems,
		CandidatePool: candidatePool,
	}, nil
}

func (adapter projectedNodeDiscovererAdapter) Close() error {
	if closeableBackend, ok := adapter.discoverBackend.(interface{ Close() error }); ok {
		return closeableBackend.Close()
	}
	return nil
}

type candidateGovernanceEvaluatorAdapter struct {
	governanceBackend loopgate.MemoryCandidateGovernanceBackend
}

func (adapter candidateGovernanceEvaluatorAdapter) EvaluateCandidate(ctx context.Context, candidate memorybench.GovernedMemoryCandidate) (memorybench.CandidateGovernanceDecision, error) {
	governanceDecision, err := adapter.governanceBackend.EvaluateMemoryCandidate(ctx, loopgate.BenchmarkMemoryCandidateRequest{
		FactKey:         candidate.FactKey,
		FactValue:       candidate.FactValue,
		SourceText:      candidate.SourceText,
		CandidateSource: candidate.CandidateSource,
		SourceChannel:   candidate.SourceChannel,
	})
	if err != nil {
		return memorybench.CandidateGovernanceDecision{}, err
	}
	return memorybench.CandidateGovernanceDecision{
		PersistenceDisposition: governanceDecision.PersistenceDisposition,
		ShouldPersist:          governanceDecision.ShouldPersist,
		HardDeny:               governanceDecision.HardDeny,
		ReasonCode:             governanceDecision.ReasonCode,
		RiskMotifs:             append([]string(nil), governanceDecision.RiskMotifs...),
	}, nil
}
