package memorybench

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewPythonRAGRetrieverClient_RejectsMissingPaths(t *testing.T) {
	repoRoot := t.TempDir()
	_, err := NewPythonRAGRetrieverClient(PythonRAGRetrieverClientConfig{
		RepoRoot:         repoRoot,
		PythonExecutable: filepath.Join(repoRoot, "missing-python"),
		HelperScriptPath: filepath.Join(repoRoot, "missing-helper.py"),
		RAGBaselineConfig: RAGBaselineConfig{
			QdrantURL:      "http://127.0.0.1:6333",
			CollectionName: "memorybench_default",
		},
	})
	if err == nil {
		t.Fatal("expected missing python executable to fail")
	}
	if !strings.Contains(err.Error(), "python executable unavailable") {
		t.Fatalf("expected python executable error, got %v", err)
	}
}

func TestNewPythonRAGRetrieverClient_AllowsConfiguredReranker(t *testing.T) {
	repoRoot := t.TempDir()
	pythonExecutablePath, helperScriptPath := writeFakeRAGRuntimeFiles(t, repoRoot, "#!/bin/sh\nexit 0\n")
	_, err := NewPythonRAGRetrieverClient(PythonRAGRetrieverClientConfig{
		RepoRoot:         repoRoot,
		PythonExecutable: pythonExecutablePath,
		HelperScriptPath: helperScriptPath,
		RAGBaselineConfig: RAGBaselineConfig{
			QdrantURL:      "http://127.0.0.1:6333",
			CollectionName: "memorybench_default",
			RerankerName:   "bge-reranker-v2-m3",
		},
	})
	if err != nil {
		t.Fatalf("expected configured reranker to be accepted, got %v", err)
	}
}

func TestPythonRAGRetrieverClient_SearchParsesResults(t *testing.T) {
	repoRoot := t.TempDir()
	pythonExecutablePath, helperScriptPath := writeFakeRAGRuntimeFiles(t, repoRoot, "#!/bin/sh\nprintf '%s' '{\"results\":[{\"document_id\":\"doc-1\",\"document_kind\":\"rag_chunk\",\"scope\":\"global\",\"created_at_utc\":\"2026-03-27T00:00:00Z\",\"snippet\":\"Grace\",\"exact_signature\":\"sig-exact\",\"family_signature\":\"sig-family\",\"provenance_ref\":\"rag:doc-1\",\"score\":0.91}]}'\n")
	retrieverClient, err := NewPythonRAGRetrieverClient(PythonRAGRetrieverClientConfig{
		RepoRoot:         repoRoot,
		PythonExecutable: pythonExecutablePath,
		HelperScriptPath: helperScriptPath,
		RAGBaselineConfig: RAGBaselineConfig{
			QdrantURL:      "http://127.0.0.1:6333",
			CollectionName: "memorybench_default",
		},
	})
	if err != nil {
		t.Fatalf("NewPythonRAGRetrieverClient: %v", err)
	}

	searchResults, err := retrieverClient.Search(context.Background(), "global", "Grace", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(searchResults) != 1 {
		t.Fatalf("expected one search result, got %#v", searchResults)
	}
	if searchResults[0].DocumentID != "doc-1" || searchResults[0].Snippet != "Grace" {
		t.Fatalf("unexpected search result: %#v", searchResults[0])
	}
}

func TestPythonRAGRetrieverClient_SearchPropagatesHelperFailure(t *testing.T) {
	repoRoot := t.TempDir()
	pythonExecutablePath, helperScriptPath := writeFakeRAGRuntimeFiles(t, repoRoot, "#!/bin/sh\necho 'qdrant unavailable' >&2\nexit 7\n")
	retrieverClient, err := NewPythonRAGRetrieverClient(PythonRAGRetrieverClientConfig{
		RepoRoot:         repoRoot,
		PythonExecutable: pythonExecutablePath,
		HelperScriptPath: helperScriptPath,
		RAGBaselineConfig: RAGBaselineConfig{
			QdrantURL:      "http://127.0.0.1:6333",
			CollectionName: "memorybench_default",
		},
	})
	if err != nil {
		t.Fatalf("NewPythonRAGRetrieverClient: %v", err)
	}

	_, err = retrieverClient.Search(context.Background(), "global", "Grace", 5)
	if err == nil {
		t.Fatal("expected helper failure to propagate")
	}
	if !strings.Contains(err.Error(), "qdrant unavailable") {
		t.Fatalf("expected helper stderr in error, got %v", err)
	}
}

func TestPythonRAGRetrieverClient_SearchPassesRerankerArguments(t *testing.T) {
	repoRoot := t.TempDir()
	pythonExecutablePath, helperScriptPath := writeFakeRAGRuntimeFiles(
		t,
		repoRoot,
		"#!/bin/sh\nprintf '%s' \"$@\" > \"$TEST_ARGS_OUT\"\nprintf '%s' '{\"results\":[]}'\n",
	)
	argsOutputPath := filepath.Join(repoRoot, "args.txt")
	retrieverClient, err := NewPythonRAGRetrieverClient(PythonRAGRetrieverClientConfig{
		RepoRoot:         repoRoot,
		PythonExecutable: pythonExecutablePath,
		HelperScriptPath: helperScriptPath,
		RAGBaselineConfig: RAGBaselineConfig{
			QdrantURL:      "http://127.0.0.1:6333",
			CollectionName: "memorybench_default",
			RerankerName:   "Xenova/ms-marco-MiniLM-L-6-v2",
		},
	})
	if err != nil {
		t.Fatalf("NewPythonRAGRetrieverClient: %v", err)
	}

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

	if _, err := retrieverClient.Search(context.Background(), "scenario:test", "Grace", 5); err != nil {
		t.Fatalf("Search: %v", err)
	}
	argsBytes, err := os.ReadFile(argsOutputPath)
	if err != nil {
		t.Fatalf("read args output: %v", err)
	}
	argsText := string(argsBytes)
	if !strings.Contains(argsText, "--reranker-model") || !strings.Contains(argsText, "Xenova/ms-marco-MiniLM-L-6-v2") {
		t.Fatalf("expected reranker args, got %q", argsText)
	}
	if !strings.Contains(argsText, "--candidate-pool") || !strings.Contains(argsText, "15") {
		t.Fatalf("expected candidate-pool args, got %q", argsText)
	}
	if !strings.Contains(argsText, "--scope") || !strings.Contains(argsText, "scenario:test") {
		t.Fatalf("expected scope args, got %q", argsText)
	}
}

func writeFakeRAGRuntimeFiles(t *testing.T, repoRoot string, fakePythonScript string) (string, string) {
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
