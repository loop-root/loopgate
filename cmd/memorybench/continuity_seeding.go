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

func buildContinuityProductionParitySeeds(selectedScenarioFixtures []memorybench.ScenarioFixture) ([]loopgate.BenchmarkRememberedFactSeed, []loopgate.BenchmarkProjectedNodeSeed, []memorybench.SeedManifestRecord, error) {
	rememberedFactSeeds := make([]loopgate.BenchmarkRememberedFactSeed, 0, len(selectedScenarioFixtures))
	fixtureSeedNodes := make([]loopgate.BenchmarkProjectedNodeSeed, 0, len(selectedScenarioFixtures)*2)
	seedManifestRecords := make([]memorybench.SeedManifestRecord, 0, len(selectedScenarioFixtures)*2)
	baseTimestampUTC := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	seedOffset := 0

	for _, scenarioFixture := range selectedScenarioFixtures {
		switch scenarioFixture.Metadata.Category {
		case memorybench.CategoryMemoryPoisoning, memorybench.CategoryMemorySafetyPrecision:
			continue
		case memorybench.CategoryTaskResumption:
			taskResumptionSeedNodes, err := buildContinuityTaskResumptionFixtureSeeds(scenarioFixture, baseTimestampUTC, &seedOffset)
			if err != nil {
				return nil, nil, nil, err
			}
			fixtureSeedNodes = append(fixtureSeedNodes, taskResumptionSeedNodes...)
			seedManifestRecords = append(seedManifestRecords, fixtureIngestManifestRecords(scenarioFixture.Metadata.ScenarioID, taskResumptionSeedNodes)...)
		case memorybench.CategoryMemoryContradiction:
			if scenarioFixture.ContinuityParitySeedSpec == nil {
				return nil, nil, nil, fmt.Errorf("contradiction fixture %q is missing continuity parity seed spec", scenarioFixture.Metadata.ScenarioID)
			}
			if strings.TrimSpace(scenarioFixture.ContinuityParitySeedSpec.CurrentPath) == memorybench.ContinuitySeedPathRememberMemoryFact ||
				strings.TrimSpace(scenarioFixture.ContinuityParitySeedSpec.SuppressedPath) == memorybench.ContinuitySeedPathRememberMemoryFact {
				contradictionRememberedSeeds, contradictionFixtureSeedNodes, contradictionManifestRecords, err := buildParityContradictionFixtureSeeds(scenarioFixture, baseTimestampUTC, &seedOffset)
				if err != nil {
					return nil, nil, nil, err
				}
				rememberedFactSeeds = append(rememberedFactSeeds, contradictionRememberedSeeds...)
				fixtureSeedNodes = append(fixtureSeedNodes, contradictionFixtureSeedNodes...)
				seedManifestRecords = append(seedManifestRecords, contradictionManifestRecords...)
				continue
			}
			contradictionSeedNodes, err := buildContinuityContradictionFixtureSeeds(scenarioFixture, baseTimestampUTC, &seedOffset)
			if err != nil {
				return nil, nil, nil, err
			}
			fixtureSeedNodes = append(fixtureSeedNodes, contradictionSeedNodes...)
			seedManifestRecords = append(seedManifestRecords, fixtureIngestManifestRecords(scenarioFixture.Metadata.ScenarioID, contradictionSeedNodes)...)
		}
	}
	if len(rememberedFactSeeds) == 0 && len(fixtureSeedNodes) == 0 {
		return nil, nil, nil, fmt.Errorf("production-parity continuity seeding produced no seeds")
	}
	return rememberedFactSeeds, fixtureSeedNodes, seedManifestRecords, nil
}

func buildParityContradictionFixtureSeeds(
	scenarioFixture memorybench.ScenarioFixture,
	baseTimestampUTC time.Time,
	seedOffset *int,
) ([]loopgate.BenchmarkRememberedFactSeed, []loopgate.BenchmarkProjectedNodeSeed, []memorybench.SeedManifestRecord, error) {
	if scenarioFixture.ContradictionExpectation == nil {
		return nil, nil, nil, fmt.Errorf("contradiction fixture %q is missing contradiction expectation", scenarioFixture.Metadata.ScenarioID)
	}
	if scenarioFixture.ContinuityParitySeedSpec == nil {
		return nil, nil, nil, fmt.Errorf("contradiction fixture %q is missing continuity parity seed spec", scenarioFixture.Metadata.ScenarioID)
	}
	if err := memorybench.ValidateContinuityParitySeedSpec(scenarioFixture); err != nil {
		return nil, nil, nil, err
	}
	contradictionExpectation := scenarioFixture.ContradictionExpectation
	paritySeedSpec := scenarioFixture.ContinuityParitySeedSpec
	scenarioID := strings.TrimSpace(scenarioFixture.Metadata.ScenarioID)
	canonicalFactKey := strings.TrimSpace(paritySeedSpec.CanonicalFactKey)

	rememberedFactSeeds := make([]loopgate.BenchmarkRememberedFactSeed, 0, len(contradictionExpectation.SuppressedHints)+1)
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
				return nil, nil, nil, err
			}
			seedManifestRecords = append(seedManifestRecords, suppressedManifestRecord)
		case memorybench.ContinuitySeedPathFixtureIngest:
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
		default:
			return nil, nil, nil, fmt.Errorf("fixture %q uses unsupported suppressed seed path %q", scenarioID, paritySeedSpec.SuppressedPath)
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
				return nil, nil, nil, err
			}
			seedManifestRecords = append(seedManifestRecords, currentManifestRecord)
		case memorybench.ContinuitySeedPathFixtureIngest:
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
		default:
			return nil, nil, nil, fmt.Errorf("fixture %q uses unsupported current seed path %q", scenarioID, paritySeedSpec.CurrentPath)
		}
	}

	for distractorIndex, distractorHint := range contradictionExpectation.DistractorHints {
		trimmedDistractorHint := strings.TrimSpace(distractorHint)
		if trimmedDistractorHint == "" {
			continue
		}
		if strings.TrimSpace(paritySeedSpec.DistractorPath) != memorybench.ContinuitySeedPathFixtureIngest {
			return nil, nil, nil, fmt.Errorf("fixture %q uses unsupported distractor seed path %q", scenarioID, paritySeedSpec.DistractorPath)
		}
		distractorSignatureHint := ""
		if distractorIndex < len(contradictionExpectation.DistractorSignatureHints) {
			distractorSignatureHint = contradictionExpectation.DistractorSignatureHints[distractorIndex]
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

	return rememberedFactSeeds, fixtureSeedNodes, seedManifestRecords, nil
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
