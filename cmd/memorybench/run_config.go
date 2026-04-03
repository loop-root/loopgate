package main

import (
	"fmt"
	"strings"

	"morph/internal/memorybench"
)

type repeatedStringFlag struct {
	values []string
}

func (flagValue *repeatedStringFlag) String() string {
	return strings.Join(flagValue.values, ",")
}

func (flagValue *repeatedStringFlag) Set(rawValue string) error {
	trimmedValue := strings.TrimSpace(rawValue)
	if trimmedValue == "" {
		return fmt.Errorf("flag value cannot be empty")
	}
	flagValue.values = append(flagValue.values, trimmedValue)
	return nil
}

func (flagValue *repeatedStringFlag) Values() []string {
	return append([]string(nil), flagValue.values...)
}

func normalizeContinuitySeedingMode(rawContinuitySeedingMode string, continuitySeedFixturesAlias bool) (string, error) {
	if continuitySeedFixturesAlias {
		if strings.TrimSpace(rawContinuitySeedingMode) != "" {
			return "", fmt.Errorf("-continuity-seed-fixtures cannot be combined with -continuity-seeding-mode")
		}
		return memorybench.ContinuitySeedingModeSyntheticProjectedNodes, nil
	}
	normalizedContinuitySeedingMode := strings.ToLower(strings.TrimSpace(rawContinuitySeedingMode))
	if normalizedContinuitySeedingMode == "" {
		return "", nil
	}
	switch normalizedContinuitySeedingMode {
	case memorybench.ContinuitySeedingModeSyntheticProjectedNodes,
		memorybench.ContinuitySeedingModeProductionWriteParity,
		memorybench.ContinuitySeedingModeDebugAmbientRepo:
		return normalizedContinuitySeedingMode, nil
	default:
		return "", fmt.Errorf("unknown continuity seeding mode %q", rawContinuitySeedingMode)
	}
}

func benchmarkComparisonClass(benchmarkProfile string, normalizedBackendName string, validatedContinuitySeedingMode string, scenarioFilter memorybench.ScenarioFilter) string {
	if strings.TrimSpace(benchmarkProfile) != "fixtures" {
		return memorybench.ComparisonClassTargetedDebugRun
	}
	if normalizedBackendName == memorybench.BackendContinuityTCL && validatedContinuitySeedingMode == memorybench.ContinuitySeedingModeDebugAmbientRepo {
		return memorybench.ComparisonClassUnscoredDebugRun
	}
	if !scenarioFilter.IsZero() {
		return memorybench.ComparisonClassTargetedDebugRun
	}
	return memorybench.ComparisonClassScoredFixtureRun
}

func benchmarkFixturesForProfile(benchmarkProfile string, scenarioFilter memorybench.ScenarioFilter) ([]memorybench.ScenarioFixture, error) {
	switch strings.TrimSpace(strings.ToLower(benchmarkProfile)) {
	case "smoke":
		if !scenarioFilter.IsZero() {
			return nil, fmt.Errorf("scenario filters are only supported with -profile fixtures")
		}
		return []memorybench.ScenarioFixture{}, nil
	case "fixtures":
		defaultScenarioFixtures := memorybench.DefaultScenarioFixtures()
		return memorybench.SelectScenarioFixtures(defaultScenarioFixtures, scenarioFilter)
	default:
		return nil, fmt.Errorf("unsupported benchmark profile %q", benchmarkProfile)
	}
}
