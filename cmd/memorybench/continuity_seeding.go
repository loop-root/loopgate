package main

import (
	"fmt"
	"strings"
	"time"

	"morph/internal/loopgate"
	"morph/internal/memorybench"
	tclpkg "morph/internal/tcl"
)

const benchmarkFixtureSourceChannel = "benchmark_fixture"

func buildSyntheticSeedManifestRecords(seedNodes []loopgate.BenchmarkProjectedNodeSeed) []memorybench.SeedManifestRecord {
	seedManifestRecords := make([]memorybench.SeedManifestRecord, 0, len(seedNodes))
	for _, seedNode := range seedNodes {
		seedManifestRecords = append(seedManifestRecords, memorybench.SeedManifestRecord{
			ScenarioID:              scenarioIDFromBenchmarkSeed(seedNode.Scope, seedNode.NodeID),
			SeedGroup:               seedGroupFromBenchmarkNodeID(seedNode.NodeID),
			SeedPath:                memorybench.ContinuitySeedPathSyntheticProjected,
			AuthorityClass:          memorybench.ContinuityAuthoritySyntheticProjected,
			ValidatedWriteSupported: false,
			FactValue:               strings.TrimSpace(seedNode.HintText),
			NodeID:                  strings.TrimSpace(seedNode.NodeID),
			SourceKind:              "benchmark_fixture",
			SourceRef:               strings.TrimSpace(seedNode.ProvenanceEvent),
			LineageStatus:           lineageStatusForSeedState(seedNode.State),
		})
	}
	return seedManifestRecords
}

func buildContinuityProductionParitySeeds(selectedScenarioFixtures []memorybench.ScenarioFixture) ([]loopgate.BenchmarkRememberedFactSeed, []loopgate.BenchmarkObservedThreadSeed, []loopgate.BenchmarkTodoSeed, []loopgate.BenchmarkProjectedNodeSeed, []memorybench.SeedManifestRecord, error) {
	rememberedFactSeeds := make([]loopgate.BenchmarkRememberedFactSeed, 0, len(selectedScenarioFixtures))
	observedThreadSeeds := make([]loopgate.BenchmarkObservedThreadSeed, 0, len(selectedScenarioFixtures))
	todoSeeds := make([]loopgate.BenchmarkTodoSeed, 0, len(selectedScenarioFixtures))
	fixtureSeedNodes := make([]loopgate.BenchmarkProjectedNodeSeed, 0, len(selectedScenarioFixtures)*2)
	seedManifestRecords := make([]memorybench.SeedManifestRecord, 0, len(selectedScenarioFixtures)*2)
	baseTimestampUTC := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	seedOffset := 0

	for _, scenarioFixture := range selectedScenarioFixtures {
		switch scenarioFixture.Metadata.Category {
		case memorybench.CategoryMemoryPoisoning, memorybench.CategoryMemorySafetyPrecision:
			continue
		case memorybench.CategoryTaskResumption:
			taskResumptionTodoSeeds, taskResumptionManifestRecords, err := buildContinuityTaskResumptionTodoSeeds(scenarioFixture)
			if err != nil {
				return nil, nil, nil, nil, nil, err
			}
			todoSeeds = append(todoSeeds, taskResumptionTodoSeeds...)
			seedManifestRecords = append(seedManifestRecords, taskResumptionManifestRecords...)
		case memorybench.CategoryMemoryContradiction:
			if scenarioFixture.ContinuityParitySeedSpec == nil {
				return nil, nil, nil, nil, nil, fmt.Errorf("contradiction fixture %q is missing continuity parity seed spec", scenarioFixture.Metadata.ScenarioID)
			}
			contradictionRememberedSeeds, contradictionObservedThreadSeeds, contradictionFixtureSeedNodes, contradictionManifestRecords, err := buildParityContradictionFixtureSeeds(scenarioFixture, baseTimestampUTC, &seedOffset)
			if err != nil {
				return nil, nil, nil, nil, nil, err
			}
			rememberedFactSeeds = append(rememberedFactSeeds, contradictionRememberedSeeds...)
			observedThreadSeeds = append(observedThreadSeeds, contradictionObservedThreadSeeds...)
			fixtureSeedNodes = append(fixtureSeedNodes, contradictionFixtureSeedNodes...)
			seedManifestRecords = append(seedManifestRecords, contradictionManifestRecords...)
		}
	}
	if len(rememberedFactSeeds) == 0 && len(observedThreadSeeds) == 0 && len(todoSeeds) == 0 && len(fixtureSeedNodes) == 0 {
		return nil, nil, nil, nil, nil, fmt.Errorf("production-parity continuity seeding produced no seeds")
	}
	return rememberedFactSeeds, observedThreadSeeds, todoSeeds, fixtureSeedNodes, seedManifestRecords, nil
}

func buildParityContradictionFixtureSeeds(
	scenarioFixture memorybench.ScenarioFixture,
	baseTimestampUTC time.Time,
	seedOffset *int,
) ([]loopgate.BenchmarkRememberedFactSeed, []loopgate.BenchmarkObservedThreadSeed, []loopgate.BenchmarkProjectedNodeSeed, []memorybench.SeedManifestRecord, error) {
	if scenarioFixture.ContradictionExpectation == nil {
		return nil, nil, nil, nil, fmt.Errorf("contradiction fixture %q is missing contradiction expectation", scenarioFixture.Metadata.ScenarioID)
	}
	if scenarioFixture.ContinuityParitySeedSpec == nil {
		return nil, nil, nil, nil, fmt.Errorf("contradiction fixture %q is missing continuity parity seed spec", scenarioFixture.Metadata.ScenarioID)
	}
	if err := memorybench.ValidateContinuityParitySeedSpec(scenarioFixture); err != nil {
		return nil, nil, nil, nil, err
	}
	contradictionExpectation := scenarioFixture.ContradictionExpectation
	paritySeedSpec := scenarioFixture.ContinuityParitySeedSpec
	scenarioID := strings.TrimSpace(scenarioFixture.Metadata.ScenarioID)
	canonicalFactKey := strings.TrimSpace(paritySeedSpec.CanonicalFactKey)
	continuityFactKey := contradictionContinuityFactKey(scenarioFixture)

	rememberedFactSeeds := make([]loopgate.BenchmarkRememberedFactSeed, 0, len(contradictionExpectation.SuppressedHints)+1)
	observedThreadSeeds := make([]loopgate.BenchmarkObservedThreadSeed, 0, len(contradictionExpectation.SuppressedHints)+len(contradictionExpectation.DistractorHints)+1)
	fixtureSeedNodes := make([]loopgate.BenchmarkProjectedNodeSeed, 0, len(contradictionExpectation.DistractorHints))
	seedManifestRecords := make([]memorybench.SeedManifestRecord, 0, len(contradictionExpectation.SuppressedHints)+len(contradictionExpectation.DistractorHints)+1)

	currentExactSignature := continuityFixtureContradictionSignature(scenarioID, contradictionExpectation.CurrentSignatureHint, "")
	currentFamilySignature := continuityFixtureContradictionFamilySignature(scenarioID, contradictionExpectation.CurrentSignatureHint, "")

	for suppressedIndex, suppressedHint := range contradictionExpectation.SuppressedHints {
		trimmedSuppressedHint := strings.TrimSpace(suppressedHint)
		if trimmedSuppressedHint == "" {
			continue
		}
		switch strings.TrimSpace(paritySeedSpec.SuppressedPath) {
		case memorybench.ContinuitySeedPathRememberMemoryFact:
			rememberedFactSeed := loopgate.BenchmarkRememberedFactSeed{
				FactKey:       canonicalFactKey,
				FactValue:     trimmedSuppressedHint,
				SourceText:    sourceTextForFixtureHint(scenarioFixture, trimmedSuppressedHint),
				SourceChannel: benchmarkFixtureSourceChannel,
				Scope:         memorybench.BenchmarkScenarioScope(scenarioID),
			}
			rememberedFactSeeds = append(rememberedFactSeeds, rememberedFactSeed)
			suppressedManifestRecord, err := rememberedFactSeedManifestRecord(scenarioID, "suppressed", rememberedFactSeed, "tombstoned")
			if err != nil {
				return nil, nil, nil, nil, err
			}
			seedManifestRecords = append(seedManifestRecords, suppressedManifestRecord)
		case memorybench.ContinuitySeedPathFixtureIngest:
			if continuityFactKey != "" {
				observedThreadSeed := benchmarkObservedFactThreadSeed(scenarioID, "suppressed", suppressedIndex, continuityFactKey, trimmedSuppressedHint)
				observedThreadSeeds = append(observedThreadSeeds, observedThreadSeed)
				seedManifestRecords = append(seedManifestRecords, observedThreadSeedManifestRecord(scenarioID, "suppressed", continuityFactKey, trimmedSuppressedHint))
			} else {
				suppressedSeedNode := loopgate.BenchmarkProjectedNodeSeed{
					NodeID:          fmt.Sprintf("%s::suppressed::%02d", scenarioID, suppressedIndex),
					CreatedAtUTC:    continuityFixtureSeedTimestamp(baseTimestampUTC, seedOffset),
					Scope:           memorybench.BenchmarkScenarioScope(scenarioID),
					NodeKind:        memorybench.BenchmarkNodeKindStep,
					State:           "tombstoned",
					HintText:        trimmedSuppressedHint,
					ExactSignature:  currentExactSignature,
					FamilySignature: currentFamilySignature,
					ProvenanceEvent: fmt.Sprintf("fixture:%s::suppressed::%02d", scenarioID, suppressedIndex),
				}
				fixtureSeedNodes = append(fixtureSeedNodes, suppressedSeedNode)
				seedManifestRecords = append(seedManifestRecords, fixtureIngestManifestRecords(scenarioID, []loopgate.BenchmarkProjectedNodeSeed{suppressedSeedNode})...)
			}
		default:
			return nil, nil, nil, nil, fmt.Errorf("fixture %q uses unsupported suppressed seed path %q", scenarioID, paritySeedSpec.SuppressedPath)
		}
	}

	currentFactValue := strings.TrimSpace(contradictionExpectation.ExpectedPrimaryHint)
	if currentFactValue != "" {
		switch strings.TrimSpace(paritySeedSpec.CurrentPath) {
		case memorybench.ContinuitySeedPathRememberMemoryFact:
			rememberedFactSeed := loopgate.BenchmarkRememberedFactSeed{
				FactKey:       canonicalFactKey,
				FactValue:     currentFactValue,
				SourceText:    sourceTextForFixtureHint(scenarioFixture, currentFactValue),
				SourceChannel: benchmarkFixtureSourceChannel,
				Scope:         memorybench.BenchmarkScenarioScope(scenarioID),
			}
			rememberedFactSeeds = append(rememberedFactSeeds, rememberedFactSeed)
			currentManifestRecord, err := rememberedFactSeedManifestRecord(scenarioID, "current", rememberedFactSeed, "eligible")
			if err != nil {
				return nil, nil, nil, nil, err
			}
			seedManifestRecords = append(seedManifestRecords, currentManifestRecord)
		case memorybench.ContinuitySeedPathFixtureIngest:
			if continuityFactKey != "" {
				observedThreadSeed := benchmarkObservedFactThreadSeed(scenarioID, "current", 0, continuityFactKey, currentFactValue)
				observedThreadSeeds = append(observedThreadSeeds, observedThreadSeed)
				seedManifestRecords = append(seedManifestRecords, observedThreadSeedManifestRecord(scenarioID, "current", continuityFactKey, currentFactValue))
			} else {
				currentSeedNode := loopgate.BenchmarkProjectedNodeSeed{
					NodeID:          scenarioID + "::current",
					CreatedAtUTC:    continuityFixtureSeedTimestamp(baseTimestampUTC, seedOffset),
					Scope:           memorybench.BenchmarkScenarioScope(scenarioID),
					NodeKind:        memorybench.BenchmarkNodeKindStep,
					State:           "active",
					HintText:        currentFactValue,
					ExactSignature:  currentExactSignature,
					FamilySignature: currentFamilySignature,
					ProvenanceEvent: "fixture:" + scenarioID + "::current",
				}
				fixtureSeedNodes = append(fixtureSeedNodes, currentSeedNode)
				seedManifestRecords = append(seedManifestRecords, fixtureIngestManifestRecords(scenarioID, []loopgate.BenchmarkProjectedNodeSeed{currentSeedNode})...)
			}
		default:
			return nil, nil, nil, nil, fmt.Errorf("fixture %q uses unsupported current seed path %q", scenarioID, paritySeedSpec.CurrentPath)
		}
	}

	for distractorIndex, distractorHint := range contradictionExpectation.DistractorHints {
		trimmedDistractorHint := strings.TrimSpace(distractorHint)
		if trimmedDistractorHint == "" {
			continue
		}
		if strings.TrimSpace(paritySeedSpec.DistractorPath) != memorybench.ContinuitySeedPathFixtureIngest {
			return nil, nil, nil, nil, fmt.Errorf("fixture %q uses unsupported distractor seed path %q", scenarioID, paritySeedSpec.DistractorPath)
		}
		distractorSignatureHint := ""
		if distractorIndex < len(contradictionExpectation.DistractorSignatureHints) {
			distractorSignatureHint = contradictionExpectation.DistractorSignatureHints[distractorIndex]
		}
		if continuityFactKey != "" {
			observedThreadSeed := benchmarkObservedFactThreadSeed(scenarioID, "distractor", distractorIndex, continuityFactKey, trimmedDistractorHint)
			observedThreadSeeds = append(observedThreadSeeds, observedThreadSeed)
			seedManifestRecords = append(seedManifestRecords, observedThreadSeedManifestRecord(scenarioID, "distractor", continuityFactKey, trimmedDistractorHint))
			continue
		}
		distractorSeedNode := loopgate.BenchmarkProjectedNodeSeed{
			NodeID:       fmt.Sprintf("%s::distractor::%02d", scenarioID, distractorIndex),
			CreatedAtUTC: continuityFixtureSeedTimestamp(baseTimestampUTC, seedOffset),
			Scope:        memorybench.BenchmarkScenarioScope(scenarioID),
			NodeKind:     memorybench.BenchmarkNodeKindStep,
			State:        "active",
			HintText:     trimmedDistractorHint,
			ExactSignature: continuityFixtureContradictionSignature(
				scenarioID,
				distractorSignatureHint,
				"distractor",
			),
			FamilySignature: continuityFixtureContradictionFamilySignature(
				scenarioID,
				distractorSignatureHint,
				"distractor",
			),
			ProvenanceEvent: fmt.Sprintf("fixture:%s::distractor::%02d", scenarioID, distractorIndex),
		}
		fixtureSeedNodes = append(fixtureSeedNodes, distractorSeedNode)
		seedManifestRecords = append(seedManifestRecords, fixtureIngestManifestRecords(scenarioID, []loopgate.BenchmarkProjectedNodeSeed{distractorSeedNode})...)
	}

	return rememberedFactSeeds, observedThreadSeeds, fixtureSeedNodes, seedManifestRecords, nil
}

func rememberedFactSeedManifestRecord(scenarioID string, seedGroup string, rememberedFactSeed loopgate.BenchmarkRememberedFactSeed, lineageStatus string) (memorybench.SeedManifestRecord, error) {
	validatedCandidate, err := tclpkg.BuildValidatedMemoryCandidate(tclpkg.MemoryCandidateInput{
		Source:              tclpkg.CandidateSourceExplicitFact,
		SourceChannel:       benchmarkFixtureSourceChannel,
		RawSourceText:       strings.TrimSpace(rememberedFactSeed.SourceText),
		NormalizedFactKey:   strings.TrimSpace(rememberedFactSeed.FactKey),
		NormalizedFactValue: strings.TrimSpace(rememberedFactSeed.FactValue),
		Trust:               tclpkg.TrustInferred,
		Actor:               tclpkg.ObjectUser,
	})
	if err != nil {
		return memorybench.SeedManifestRecord{}, fmt.Errorf("build validated memory candidate for benchmark seed %q %q: %w", scenarioID, seedGroup, err)
	}
	return memorybench.SeedManifestRecord{
		ScenarioID:              scenarioID,
		SeedGroup:               seedGroup,
		SeedPath:                memorybench.ContinuitySeedPathRememberMemoryFact,
		AuthorityClass:          memorybench.ContinuityAuthorityValidatedWrite,
		ValidatedWriteSupported: true,
		CanonicalFactKey:        validatedCandidate.CanonicalKey,
		FactValue:               validatedCandidate.FactValue,
		AnchorTupleKey:          validatedCandidate.AnchorTupleKey(),
		SourceKind:              "explicit_profile_fact",
		SourceRef:               "fixture:" + scenarioID + "::" + seedGroup,
		LineageStatus:           lineageStatus,
	}, nil
}

func fixtureIngestManifestRecords(scenarioID string, seedNodes []loopgate.BenchmarkProjectedNodeSeed) []memorybench.SeedManifestRecord {
	seedManifestRecords := make([]memorybench.SeedManifestRecord, 0, len(seedNodes))
	for _, seedNode := range seedNodes {
		seedManifestRecords = append(seedManifestRecords, memorybench.SeedManifestRecord{
			ScenarioID:              scenarioID,
			SeedGroup:               seedGroupFromBenchmarkNodeID(seedNode.NodeID),
			SeedPath:                memorybench.ContinuitySeedPathFixtureIngest,
			AuthorityClass:          memorybench.ContinuityAuthorityFixtureIngest,
			ValidatedWriteSupported: false,
			FactValue:               strings.TrimSpace(seedNode.HintText),
			NodeID:                  strings.TrimSpace(seedNode.NodeID),
			SourceKind:              "benchmark_fixture",
			SourceRef:               strings.TrimSpace(seedNode.ProvenanceEvent),
			LineageStatus:           lineageStatusForSeedState(seedNode.State),
		})
	}
	return seedManifestRecords
}

func benchmarkObservedFactThreadSeed(scenarioID string, seedGroup string, seedIndex int, factKey string, factValue string) loopgate.BenchmarkObservedThreadSeed {
	return loopgate.BenchmarkObservedThreadSeed{
		Scope: memorybench.BenchmarkScenarioScope(scenarioID),
		Events: []loopgate.BenchmarkObservedThreadEventSeed{{
			EventType:  "orchestration.tool_result",
			Capability: "benchmark.fixture.observe",
			Status:     "ok",
			Facts: map[string]string{
				strings.TrimSpace(factKey): strings.TrimSpace(factValue),
			},
			CallID: fmt.Sprintf("fixture-observe-%s-%s-%02d", safeBenchmarkSeedToken(scenarioID), seedGroup, seedIndex),
		}},
	}
}

func observedThreadSeedManifestRecord(scenarioID string, seedGroup string, factKey string, factValue string) memorybench.SeedManifestRecord {
	return memorybench.SeedManifestRecord{
		ScenarioID:              scenarioID,
		SeedGroup:               seedGroup,
		SeedPath:                memorybench.ContinuitySeedPathObservedThread,
		AuthorityClass:          memorybench.ContinuityAuthorityObservedThread,
		ValidatedWriteSupported: false,
		CanonicalFactKey:        strings.TrimSpace(factKey),
		FactValue:               strings.TrimSpace(factValue),
		SourceKind:              "observed_thread_seed",
		SourceRef:               "fixture:" + scenarioID + "::" + seedGroup,
		LineageStatus:           "eligible",
	}
}

func buildContinuityTaskResumptionTodoSeeds(scenarioFixture memorybench.ScenarioFixture) ([]loopgate.BenchmarkTodoSeed, []memorybench.SeedManifestRecord, error) {
	if scenarioFixture.TaskResumptionExpectation == nil {
		return nil, nil, fmt.Errorf("task resumption fixture %q is missing task resumption expectation", scenarioFixture.Metadata.ScenarioID)
	}
	scenarioID := strings.TrimSpace(scenarioFixture.Metadata.ScenarioID)
	if scenarioID == "" {
		return nil, nil, fmt.Errorf("task resumption fixture is missing scenario id")
	}

	taskResumptionExpectation := scenarioFixture.TaskResumptionExpectation
	todoSeeds := make([]loopgate.BenchmarkTodoSeed, 0, len(taskResumptionExpectation.RequiredHints)+len(taskResumptionExpectation.ForbiddenHints))
	seedManifestRecords := make([]memorybench.SeedManifestRecord, 0, len(taskResumptionExpectation.RequiredHints)+len(taskResumptionExpectation.ForbiddenHints))

	for _, forbiddenHint := range taskResumptionExpectation.ForbiddenHints {
		trimmedForbiddenHint := strings.TrimSpace(forbiddenHint)
		if trimmedForbiddenHint == "" {
			continue
		}
		todoSeed := loopgate.BenchmarkTodoSeed{
			Scope:       memorybench.BenchmarkScenarioScope(scenarioID),
			SeedGroup:   "forbidden_hint",
			Text:        trimmedForbiddenHint,
			TaskKind:    "carry_over",
			SourceKind:  "benchmark_task_resumption",
			FinalStatus: "done",
		}
		todoSeeds = append(todoSeeds, todoSeed)
		seedManifestRecords = append(seedManifestRecords, todoSeedManifestRecord(scenarioID, len(todoSeeds)-1, todoSeed))
	}

	// Pair required hints into real todo items so the control-plane benchmark uses
	// the same compact open-item shape Haven relies on during resume: current text
	// plus optional next-step metadata, not one synthetic node per string.
	for requiredIndex := 0; requiredIndex < len(taskResumptionExpectation.RequiredHints); requiredIndex += 2 {
		trimmedPrimaryHint := strings.TrimSpace(taskResumptionExpectation.RequiredHints[requiredIndex])
		if trimmedPrimaryHint == "" {
			continue
		}
		nextStepHint := ""
		if requiredIndex+1 < len(taskResumptionExpectation.RequiredHints) {
			nextStepHint = strings.TrimSpace(taskResumptionExpectation.RequiredHints[requiredIndex+1])
		}
		todoSeed := loopgate.BenchmarkTodoSeed{
			Scope:       memorybench.BenchmarkScenarioScope(scenarioID),
			SeedGroup:   "required_hint",
			Text:        trimmedPrimaryHint,
			NextStep:    nextStepHint,
			TaskKind:    "carry_over",
			SourceKind:  "benchmark_task_resumption",
			FinalStatus: "todo",
		}
		todoSeeds = append(todoSeeds, todoSeed)
		seedManifestRecords = append(seedManifestRecords, todoSeedManifestRecord(scenarioID, len(todoSeeds)-1, todoSeed))
	}

	if len(todoSeeds) == 0 {
		return nil, nil, fmt.Errorf("task resumption fixture %q produced no todo workflow seeds", scenarioID)
	}
	return todoSeeds, seedManifestRecords, nil
}

func todoSeedManifestRecord(scenarioID string, seedIndex int, todoSeed loopgate.BenchmarkTodoSeed) memorybench.SeedManifestRecord {
	return memorybench.SeedManifestRecord{
		ScenarioID:              scenarioID,
		SeedGroup:               strings.TrimSpace(todoSeed.SeedGroup),
		SeedPath:                memorybench.ContinuitySeedPathTodoWorkflow,
		AuthorityClass:          memorybench.ContinuityAuthorityTodoWorkflow,
		ValidatedWriteSupported: false,
		FactValue:               benchmarkTodoSeedHintText(todoSeed),
		SourceKind:              "todo_workflow_seed",
		SourceRef:               fmt.Sprintf("fixture:%s::%s::%02d", scenarioID, strings.TrimSpace(todoSeed.SeedGroup), seedIndex),
		LineageStatus:           todoSeedLineageStatus(todoSeed),
	}
}

func benchmarkTodoSeedHintText(todoSeed loopgate.BenchmarkTodoSeed) string {
	trimmedText := strings.TrimSpace(todoSeed.Text)
	trimmedNextStep := strings.TrimSpace(todoSeed.NextStep)
	switch {
	case trimmedText == "":
		return trimmedNextStep
	case trimmedNextStep == "":
		return trimmedText
	default:
		return trimmedText + "\n" + trimmedNextStep
	}
}

func todoSeedLineageStatus(todoSeed loopgate.BenchmarkTodoSeed) string {
	switch strings.TrimSpace(todoSeed.FinalStatus) {
	case "done":
		return "tombstoned"
	default:
		return "eligible"
	}
}

func contradictionContinuityFactKey(scenarioFixture memorybench.ScenarioFixture) string {
	if scenarioFixture.ContradictionExpectation == nil {
		return ""
	}
	if scenarioFixture.ContinuityParitySeedSpec != nil {
		if trimmedCanonicalFactKey := strings.TrimSpace(scenarioFixture.ContinuityParitySeedSpec.CanonicalFactKey); trimmedCanonicalFactKey != "" {
			return trimmedCanonicalFactKey
		}
	}
	normalizedScenarioID := strings.ToLower(strings.TrimSpace(scenarioFixture.Metadata.ScenarioID))
	normalizedSignatureHint := strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(scenarioFixture.ContradictionExpectation.CurrentSignatureHint))), " ")
	switch {
	case strings.Contains(normalizedSignatureHint, "identity timezone slot timezone"):
		return "profile.timezone"
	case strings.Contains(normalizedSignatureHint, "identity locale slot locale"):
		return "profile.locale"
	case strings.Contains(normalizedSignatureHint, "identity name slot preferred_name"):
		return "preferred_name"
	case strings.Contains(normalizedSignatureHint, "identity name slot name"):
		return "name"
	case strings.HasPrefix(normalizedScenarioID, "contradiction.preference_"):
		return "preference.stated_preference"
	default:
		return ""
	}
}

func safeBenchmarkSeedToken(rawValue string) string {
	normalizedValue := strings.ToLower(strings.TrimSpace(rawValue))
	normalizedValue = strings.ReplaceAll(normalizedValue, ".", "_")
	normalizedValue = strings.ReplaceAll(normalizedValue, ":", "_")
	return strings.ReplaceAll(normalizedValue, "-", "_")
}

func scenarioIDFromBenchmarkSeed(scope string, nodeID string) string {
	trimmedScope := strings.TrimSpace(scope)
	if strings.HasPrefix(trimmedScope, "scenario:") {
		return strings.TrimPrefix(trimmedScope, "scenario:")
	}
	trimmedNodeID := strings.TrimSpace(nodeID)
	if delimiterIndex := strings.Index(trimmedNodeID, "::"); delimiterIndex > 0 {
		return trimmedNodeID[:delimiterIndex]
	}
	return trimmedNodeID
}

func seedGroupFromBenchmarkNodeID(nodeID string) string {
	trimmedNodeID := strings.TrimSpace(nodeID)
	switch {
	case strings.Contains(trimmedNodeID, "::current"):
		return "current"
	case strings.Contains(trimmedNodeID, "::suppressed::"):
		return "suppressed"
	case strings.Contains(trimmedNodeID, "::distractor::"):
		return "distractor"
	case strings.Contains(trimmedNodeID, "::resume::"):
		return "required_hint"
	case strings.Contains(trimmedNodeID, "::stale::"):
		return "forbidden_hint"
	default:
		return "fixture_node"
	}
}

func lineageStatusForSeedState(seedState string) string {
	switch strings.TrimSpace(seedState) {
	case "tombstoned":
		return "tombstoned"
	default:
		return "eligible"
	}
}

func sourceTextForFixtureHint(scenarioFixture memorybench.ScenarioFixture, expectedHint string) string {
	trimmedExpectedHint := strings.TrimSpace(expectedHint)
	for _, scenarioStep := range scenarioFixture.Steps {
		if strings.Contains(strings.ToLower(scenarioStep.Content), strings.ToLower(trimmedExpectedHint)) {
			return scenarioStep.Content
		}
	}
	return trimmedExpectedHint
}
