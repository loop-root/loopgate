package loopgate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"loopgate/internal/config"
	"loopgate/internal/memorybench"
)

// memoryEvidenceRetriever is the bounded runtime seam for non-authoritative
// evidence search. Hybrid memory uses it only on read paths; continuity remains
// the write-side authority.
type memoryEvidenceRetriever interface {
	Search(ctx context.Context, scope string, query string, maxItems int) ([]memoryEvidenceSearchResult, error)
}

type memoryEvidenceSearchResult struct {
	EvidenceID    string
	SourceKind    string
	Scope         string
	CreatedAtUTC  string
	Snippet       string
	ProvenanceRef string
	MatchCount    int
}

type memorybenchRAGEvidenceRetriever struct {
	inner memorybench.RAGRetrieverClient
}

func (retriever memorybenchRAGEvidenceRetriever) Search(ctx context.Context, scope string, query string, maxItems int) ([]memoryEvidenceSearchResult, error) {
	searchResults, err := retriever.inner.Search(ctx, scope, query, maxItems)
	if err != nil {
		return nil, err
	}
	evidenceResults := make([]memoryEvidenceSearchResult, 0, len(searchResults))
	for _, searchResult := range searchResults {
		evidenceResults = append(evidenceResults, memoryEvidenceSearchResult{
			EvidenceID:    strings.TrimSpace(searchResult.DocumentID),
			SourceKind:    strings.TrimSpace(searchResult.DocumentKind),
			Scope:         strings.TrimSpace(searchResult.Scope),
			CreatedAtUTC:  strings.TrimSpace(searchResult.CreatedAtUTC),
			Snippet:       strings.TrimSpace(searchResult.Snippet),
			ProvenanceRef: strings.TrimSpace(searchResult.ProvenanceRef),
			MatchCount:    ragEvidenceScoreToMatchCount(searchResult.Score),
		})
	}
	return evidenceResults, nil
}

// The runtime currently reuses the memorybench Python/Qdrant helper so product
// hybrid retrieval can follow the same measured evidence path while we keep the
// authoritative continuity path unchanged. This is intentionally explicit so a
// future extraction into a shared runtime package does not hide the dependency.
func newRuntimeMemoryEvidenceRetriever(repoRoot string, runtimeConfig config.RuntimeConfig) (memoryEvidenceRetriever, error) {
	hybridEvidenceConfig := runtimeConfig.Memory.HybridEvidence
	resolvedPythonExecutable, err := resolveHybridPythonExecutable(hybridEvidenceConfig.PythonExecutable)
	if err != nil {
		return nil, fmt.Errorf("resolve hybrid python executable: %w", err)
	}
	resolvedHelperScriptPath, err := resolveHybridHelperScriptPath(repoRoot, hybridEvidenceConfig.HelperScriptPath)
	if err != nil {
		return nil, fmt.Errorf("resolve hybrid helper script: %w", err)
	}
	retrieverClient, err := memorybench.NewPythonRAGRetrieverClient(memorybench.PythonRAGRetrieverClientConfig{
		RepoRoot:         repoRoot,
		PythonExecutable: resolvedPythonExecutable,
		HelperScriptPath: resolvedHelperScriptPath,
		RAGBaselineConfig: memorybench.RAGBaselineConfig{
			QdrantURL:      hybridEvidenceConfig.QdrantURL,
			CollectionName: hybridEvidenceConfig.CollectionName,
			EmbeddingModel: hybridEvidenceConfig.EmbeddingModel,
			RerankerName:   hybridEvidenceConfig.RerankerModel,
		},
	})
	if err != nil {
		return nil, err
	}
	return memorybenchRAGEvidenceRetriever{inner: retrieverClient}, nil
}

func resolveHybridPythonExecutable(rawExecutable string) (string, error) {
	trimmedExecutable := strings.TrimSpace(rawExecutable)
	if trimmedExecutable == "" {
		return "", fmt.Errorf("python executable is required")
	}
	if filepath.IsAbs(trimmedExecutable) {
		if _, err := os.Stat(trimmedExecutable); err != nil {
			return "", fmt.Errorf("python executable unavailable: %w", err)
		}
		return trimmedExecutable, nil
	}
	resolvedExecutablePath, err := exec.LookPath(trimmedExecutable)
	if err != nil {
		return "", fmt.Errorf("python executable unavailable: %w", err)
	}
	return resolvedExecutablePath, nil
}

func resolveHybridHelperScriptPath(repoRoot string, rawHelperScriptPath string) (string, error) {
	trimmedHelperScriptPath := strings.TrimSpace(rawHelperScriptPath)
	if trimmedHelperScriptPath == "" {
		return "", fmt.Errorf("helper script path is required")
	}
	if filepath.IsAbs(trimmedHelperScriptPath) {
		if _, err := os.Stat(trimmedHelperScriptPath); err != nil {
			return "", fmt.Errorf("helper script unavailable: %w", err)
		}
		return filepath.Clean(trimmedHelperScriptPath), nil
	}
	cleanedHelperScriptPath := filepath.Clean(trimmedHelperScriptPath)
	if cleanedHelperScriptPath == "." || cleanedHelperScriptPath == ".." || strings.HasPrefix(cleanedHelperScriptPath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("helper script path must stay within the repository root")
	}
	resolvedHelperScriptPath := filepath.Join(repoRoot, cleanedHelperScriptPath)
	if _, err := os.Stat(resolvedHelperScriptPath); err != nil {
		return "", fmt.Errorf("helper script unavailable: %w", err)
	}
	return resolvedHelperScriptPath, nil
}

func ragEvidenceScoreToMatchCount(score float64) int {
	switch {
	case score >= 0.95:
		return 3
	case score >= 0.5:
		return 2
	case score > 0:
		return 1
	default:
		return 0
	}
}
