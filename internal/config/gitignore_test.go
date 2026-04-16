package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitignore_ContainsRuntimeAndBuildArtifactRules(t *testing.T) {
	repoRoot := repositoryRootFromTestFile(t)
	gitignorePath := filepath.Join(repoRoot, ".gitignore")

	rawBytes, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	gitignoreText := string(rawBytes)

	requiredPatterns := []string{
		"runtime/",
		".cache/",
		"cmd/memorybench/",
		"core/memory/ledger/*.jsonl",
		"core/memory/distillates/*.jsonl",
		"core/memory/keys/",
		"core/memory/keys_old/",
		"output/",
		"tmp/",
		".claude/",
		"morph",
		"*.tar.gz",
		"*.zip",
	}
	for _, requiredPattern := range requiredPatterns {
		if !strings.Contains(gitignoreText, requiredPattern) {
			t.Fatalf(".gitignore missing required pattern %q", requiredPattern)
		}
	}
}
