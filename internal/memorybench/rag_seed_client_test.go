package memorybench

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewPythonRAGSeederClient_RejectsMissingPaths(t *testing.T) {
	repoRoot := t.TempDir()
	_, err := NewPythonRAGSeederClient(PythonRAGSeederClientConfig{
		RepoRoot:         repoRoot,
		PythonExecutable: filepath.Join(repoRoot, "missing-python"),
		HelperScriptPath: filepath.Join(repoRoot, "missing-helper.py"),
		RAGBaselineConfig: RAGBaselineConfig{
			QdrantURL:      "http://127.0.0.1:6333",
			CollectionName: "memorybench_default",
		},
	})
	if err == nil {
		t.Fatal("expected missing seeder runtime to fail")
	}
}

func TestPythonRAGSeederClient_SeedCorpusPropagatesHelperFailure(t *testing.T) {
	repoRoot := t.TempDir()
	pythonExecutablePath, helperScriptPath := writeFakeRAGSeederRuntimeFiles(t, repoRoot, "#!/bin/sh\ncat >/dev/null\necho 'seed unavailable' >&2\nexit 9\n")
	seederClient, err := NewPythonRAGSeederClient(PythonRAGSeederClientConfig{
		RepoRoot:         repoRoot,
		PythonExecutable: pythonExecutablePath,
		HelperScriptPath: helperScriptPath,
		RAGBaselineConfig: RAGBaselineConfig{
			QdrantURL:      "http://127.0.0.1:6333",
			CollectionName: "memorybench_default",
		},
	})
	if err != nil {
		t.Fatalf("NewPythonRAGSeederClient: %v", err)
	}
	err = seederClient.SeedCorpus(context.Background(), []CorpusDocument{{
		DocumentID:   "fixture:1",
		Content:      "Grace is the current name",
		DocumentKind: BenchmarkNodeKindStep,
		Scope:        BenchmarkScopeGlobal,
	}})
	if err == nil {
		t.Fatal("expected helper failure to propagate")
	}
	if !strings.Contains(err.Error(), "seed unavailable") {
		t.Fatalf("expected helper stderr in error, got %v", err)
	}
}

func writeFakeRAGSeederRuntimeFiles(t *testing.T, repoRoot string, fakePythonScript string) (string, string) {
	t.Helper()
	pythonExecutablePath := filepath.Join(repoRoot, ".cache", "memorybench-venv", "bin", "python")
	if err := os.MkdirAll(filepath.Dir(pythonExecutablePath), 0o755); err != nil {
		t.Fatalf("mkdir python executable parent: %v", err)
	}
	if err := os.WriteFile(pythonExecutablePath, []byte(fakePythonScript), 0o755); err != nil {
		t.Fatalf("write fake python executable: %v", err)
	}
	helperScriptPath := filepath.Join(repoRoot, "cmd", "memorybench", "rag_search.py")
	if err := os.MkdirAll(filepath.Dir(helperScriptPath), 0o755); err != nil {
		t.Fatalf("mkdir helper script parent: %v", err)
	}
	if err := os.WriteFile(helperScriptPath, []byte("print('helper placeholder')\n"), 0o644); err != nil {
		t.Fatalf("write helper script: %v", err)
	}
	return pythonExecutablePath, helperScriptPath
}
