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
	// Some filtered fixture sets are governance-only and intentionally produce no
	// discoverable continuity seeds. Let the caller route those scenario scopes
	// to an explicit empty discoverer instead of failing the benchmark setup.
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
	scenarioObservedFactKey := contradictionObservedFactKeyForSignatureHint(scenarioFixture, contradictionExpectation.CurrentSignatureHint)

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
			// Suppressed hints currently do not carry their own signature annotation in the
			// fixture schema. Fall back to the scenario's primary observed fact key so the
			// parity harness can stay on the real observed-thread continuity path instead of
			// fabricating benchmark-only projected nodes.
			suppressedSourceText := sourceTextForFixtureHint(scenarioFixture, trimmedSuppressedHint)
			suppressedObservedFactKey := benchmarkObservedFactKeyWithEntityScope(scenarioObservedFactKey, trimmedSuppressedHint, suppressedSourceText)
			if suppressedObservedFactKey != "" {
				observedThreadSeed := benchmarkObservedFactThreadSeed(
					scenarioID,
					"suppressed",
					suppressedIndex,
					suppressedObservedFactKey,
					trimmedSuppressedHint,
					suppressedSourceText,
				)
				observedThreadSeeds = append(observedThreadSeeds, observedThreadSeed)
				seedManifestRecords = append(seedManifestRecords, observedThreadSeedManifestRecord(scenarioID, "suppressed", suppressedObservedFactKey, trimmedSuppressedHint))
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
			currentSourceText := sourceTextForFixtureHint(scenarioFixture, currentFactValue)
			currentObservedFactKey := benchmarkObservedFactKeyWithEntityScope(scenarioObservedFactKey, currentFactValue, currentSourceText)
			if currentObservedFactKey != "" {
				observedThreadSeed := benchmarkObservedFactThreadSeed(
					scenarioID,
					"current",
					0,
					currentObservedFactKey,
					currentFactValue,
					currentSourceText,
				)
				observedThreadSeeds = append(observedThreadSeeds, observedThreadSeed)
				seedManifestRecords = append(seedManifestRecords, observedThreadSeedManifestRecord(scenarioID, "current", currentObservedFactKey, currentFactValue))
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
		distractorSourceText := sourceTextForFixtureHint(scenarioFixture, trimmedDistractorHint)
		distractorObservedFactKey := contradictionObservedFactKeyForSignatureHint(scenarioFixture, distractorSignatureHint)
		if distractorObservedFactKey == "" {
			distractorObservedFactKey = scenarioObservedFactKey
		}
		distractorObservedFactKey = benchmarkObservedFactKeyWithEntityScope(
			distractorObservedFactKey,
			distractorSignatureHint,
			trimmedDistractorHint,
			distractorSourceText,
		)
		if distractorObservedFactKey != "" {
			observedThreadSeed := benchmarkObservedFactThreadSeed(
				scenarioID,
				"distractor",
				distractorIndex,
				distractorObservedFactKey,
				trimmedDistractorHint,
				distractorSourceText,
			)
			observedThreadSeeds = append(observedThreadSeeds, observedThreadSeed)
			seedManifestRecords = append(seedManifestRecords, observedThreadSeedManifestRecord(scenarioID, "distractor", distractorObservedFactKey, trimmedDistractorHint))
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
		FactKey:                 validatedCandidate.CanonicalKey,
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

func benchmarkObservedFactThreadSeed(scenarioID string, seedGroup string, seedIndex int, factKey string, factValue string, sourceText string) loopgate.BenchmarkObservedThreadSeed {
	return loopgate.BenchmarkObservedThreadSeed{
		Scope: memorybench.BenchmarkScenarioScope(scenarioID),
		Events: []loopgate.BenchmarkObservedThreadEventSeed{{
			EventType:  "orchestration.tool_result",
			Text:       strings.TrimSpace(sourceText),
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
		FactKey:                 strings.TrimSpace(factKey),
		CanonicalFactKey:        manifestCanonicalFactKey(factKey),
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

func contradictionObservedFactKeyForSignatureHint(scenarioFixture memorybench.ScenarioFixture, rawSignatureHint string) string {
	normalizedScenarioID := strings.ToLower(strings.TrimSpace(scenarioFixture.Metadata.ScenarioID))
	normalizedSignatureHint := strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(rawSignatureHint))), " ")
	switch {
	case strings.Contains(normalizedSignatureHint, "identity timezone slot timezone"):
		return "profile.timezone"
	case strings.Contains(normalizedSignatureHint, "preview timezone label"),
		strings.Contains(normalizedSignatureHint, "preview card timezone slot label"),
		strings.Contains(normalizedSignatureHint, "preview timezone slot chip"):
		return "profile.preview_timezone_label"
	case strings.Contains(normalizedSignatureHint, "identity locale slot locale"):
		return "profile.locale"
	case strings.Contains(normalizedSignatureHint, "preview locale label"),
		strings.Contains(normalizedSignatureHint, "preview card locale label"):
		return "profile.preview_locale_label"
	case strings.Contains(normalizedSignatureHint, "identity name slot preferred_name"):
		return "preferred_name"
	case strings.Contains(normalizedSignatureHint, "identity name slot name"):
		return "name"
	case strings.Contains(normalizedSignatureHint, "display name preview label"),
		strings.Contains(normalizedSignatureHint, "display name preview alias"):
		return "profile.display_name_preview_label"
	case strings.Contains(normalizedSignatureHint, "identity pronouns slot pronouns"):
		return "profile.pronouns"
	case strings.Contains(normalizedSignatureHint, "preview pronouns badge"):
		return "profile.preview_pronouns_badge"
	case strings.HasPrefix(normalizedScenarioID, "contradiction.identity_old_name_"),
		strings.HasPrefix(normalizedScenarioID, "contradiction.identity_entity_"):
		return "name"
	case strings.HasPrefix(normalizedScenarioID, "contradiction.identity_alias_"):
		return "preferred_name"
	case strings.HasPrefix(normalizedScenarioID, "contradiction.preference_"):
		return "preference.stated_preference"
	default:
		return ""
	}
}

func manifestCanonicalFactKey(rawFactKey string) string {
	canonicalFactKey := tclpkg.CanonicalizeExplicitMemoryFactKey(strings.TrimSpace(rawFactKey))
	if canonicalFactKey == "" {
		return ""
	}
	return canonicalFactKey
}

func benchmarkObservedFactKeyWithEntityScope(baseFactKey string, rawContexts ...string) string {
	normalizedBaseFactKey := strings.TrimSpace(baseFactKey)
	if normalizedBaseFactKey == "" {
		return ""
	}
	normalizedContext := strings.ToLower(strings.Join(rawContexts, "\n"))
	switch {
	case strings.Contains(normalizedContext, "shadow operator"),
		strings.Contains(normalizedContext, "teammate"),
		strings.Contains(normalizedContext, "on-call handoff"):
		return "teammate." + normalizedBaseFactKey
	case strings.Contains(normalizedContext, "my cat"):
		return "pet." + normalizedBaseFactKey
	default:
		return normalizedBaseFactKey
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
