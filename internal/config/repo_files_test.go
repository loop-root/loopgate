package config

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"loopgate/internal/safety"
)

func TestRepositoryPolicyFile_LoadsWithStrictSchema(t *testing.T) {
	repoRoot := repositoryRootFromTestFile(t)

	policy, err := LoadPolicy(repoRoot)
	if err != nil {
		t.Fatalf("repository policy must decode strictly: %v", err)
	}

	if policy.Version == "" {
		t.Fatal("expected policy version to be set")
	}
	if len(policy.Tools.Filesystem.AllowedRoots) == 0 {
		t.Fatal("expected at least one allowed root")
	}
	for _, root := range policy.Tools.Filesystem.AllowedRoots {
		if strings.HasPrefix(root, "~/") || root == "~" {
			t.Fatalf("allowed root was not normalized: %q", root)
		}
	}
}

func TestRepositoryPersonaFile_LoadsWithStrictSchema(t *testing.T) {
	repoRoot := repositoryRootFromTestFile(t)

	persona, err := LoadPersona(repoRoot)
	if err != nil {
		t.Fatalf("repository persona must decode strictly: %v", err)
	}

	if strings.TrimSpace(persona.Name) == "" {
		t.Fatal("expected persona name")
	}
	if strings.TrimSpace(persona.Version) == "" {
		t.Fatal("expected persona version")
	}
	if !persona.Trust.TreatModelOutputAsUntrusted {
		t.Fatal("expected persona to treat model output as untrusted")
	}
	if !persona.Trust.TreatToolOutputAsUntrusted {
		t.Fatal("expected persona to treat tool output as untrusted")
	}
	if !persona.HallucinationControls.AdmitUnknowns {
		t.Fatal("expected persona to admit unknowns explicitly")
	}
	if len(persona.RiskControls.RiskyBehaviorDefinition) == 0 {
		t.Fatal("expected risky behavior definition to be populated")
	}
	if strings.TrimSpace(persona.Personality.Honesty) == "" {
		t.Fatal("expected persona honesty trait")
	}
	if strings.TrimSpace(persona.Communication.Tone) == "" {
		t.Fatal("expected persona communication tone")
	}
}

func TestRepositoryRuntimeConfigFile_LoadsWithStrictSchema(t *testing.T) {
	repoRoot := repositoryRootFromTestFile(t)

	runtimeConfig, err := LoadRuntimeConfig(repoRoot)
	if err != nil {
		t.Fatalf("runtime config must decode strictly: %v", err)
	}
	if runtimeConfig.Memory.CandidatePanelSize <= 0 {
		t.Fatal("expected positive candidate panel size")
	}
	if runtimeConfig.Memory.SoftWorkerConcurrency <= 0 {
		t.Fatal("expected positive soft morphling concurrency")
	}
	if runtimeConfig.Logging.AuditLedger.MaxEventBytes <= 0 {
		t.Fatal("expected positive audit max_event_bytes")
	}
	if runtimeConfig.Logging.AuditLedger.RotateAtBytes <= 0 {
		t.Fatal("expected positive audit rotate_at_bytes")
	}
	if runtimeConfig.Logging.AuditLedger.VerifyClosedSegmentsOnStartup == nil {
		t.Fatal("expected audit verify_closed_segments_on_startup to be populated")
	}
}

func TestRepositoryGoalAliasesFile_LoadsWithStrictSchema(t *testing.T) {
	repoRoot := repositoryRootFromTestFile(t)

	goalAliases, err := LoadGoalAliases(repoRoot)
	if err != nil {
		t.Fatalf("goal aliases must decode strictly: %v", err)
	}
	if len(goalAliases.Aliases) == 0 {
		t.Fatal("expected goal aliases to be populated")
	}
	if _, found := goalAliases.Aliases["technical_review"]; !found {
		t.Fatal("expected technical_review aliases")
	}
}

func TestRepositoryPolicyFile_AllowsRepoDocsButDeniesSensitiveRoots(t *testing.T) {
	repoRoot := repositoryRootFromTestFile(t)

	policy, err := LoadPolicy(repoRoot)
	if err != nil {
		t.Fatalf("load repository policy: %v", err)
	}

	allowedPath, err := safety.SafePath(
		repoRoot,
		policy.Tools.Filesystem.AllowedRoots,
		policy.Tools.Filesystem.DeniedPaths,
		"docs/setup/SETUP.md",
	)
	if err != nil {
		t.Fatalf("expected docs path to be allowed under repository policy: %v", err)
	}
	if !strings.Contains(allowedPath, filepath.Join("docs", "setup", "SETUP.md")) {
		t.Fatalf("unexpected allowed path: %q", allowedPath)
	}

	_, err = safety.SafePath(
		repoRoot,
		policy.Tools.Filesystem.AllowedRoots,
		policy.Tools.Filesystem.DeniedPaths,
		"core/memory/ledger/ledger.jsonl",
	)
	if err == nil {
		t.Fatal("expected repository policy to deny ledger path")
	}

	_, err = safety.SafePath(
		repoRoot,
		policy.Tools.Filesystem.AllowedRoots,
		policy.Tools.Filesystem.DeniedPaths,
		"core/policy/policy.yaml",
	)
	if err == nil {
		t.Fatal("expected repository policy to deny policy path")
	}
}

func repositoryRootFromTestFile(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}
