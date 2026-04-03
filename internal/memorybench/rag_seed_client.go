package memorybench

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type PythonRAGSeederClientConfig struct {
	RepoRoot          string
	PythonExecutable  string
	HelperScriptPath  string
	RAGBaselineConfig RAGBaselineConfig
}

type pythonRAGSeederClient struct {
	pythonExecutable  string
	helperScriptPath  string
	qdrantURL         string
	collectionName    string
	fastembedCacheDir string
	homeDirectory     string
}

func NewPythonRAGSeederClient(rawConfig PythonRAGSeederClientConfig) (*pythonRAGSeederClient, error) {
	if err := ValidateRAGBaselineConfig(rawConfig.RAGBaselineConfig); err != nil {
		return nil, err
	}
	repoRoot := strings.TrimSpace(rawConfig.RepoRoot)
	if repoRoot == "" {
		return nil, fmt.Errorf("rag baseline repo root is required")
	}
	pythonExecutable := strings.TrimSpace(rawConfig.PythonExecutable)
	if pythonExecutable == "" {
		return nil, fmt.Errorf("rag baseline python executable path is required")
	}
	if _, err := os.Stat(pythonExecutable); err != nil {
		return nil, fmt.Errorf("rag baseline python executable unavailable: %w", err)
	}
	helperScriptPath := strings.TrimSpace(rawConfig.HelperScriptPath)
	if helperScriptPath == "" {
		return nil, fmt.Errorf("rag baseline helper script path is required")
	}
	if _, err := os.Stat(helperScriptPath); err != nil {
		return nil, fmt.Errorf("rag baseline helper script unavailable: %w", err)
	}

	cacheRoot := filepath.Join(repoRoot, ".cache")
	fastembedCacheDir := filepath.Join(cacheRoot, "fastembed")
	homeDirectory := filepath.Join(cacheRoot, "haystack-home")
	if err := os.MkdirAll(fastembedCacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("create rag baseline fastembed cache dir: %w", err)
	}
	if err := os.MkdirAll(homeDirectory, 0o755); err != nil {
		return nil, fmt.Errorf("create rag baseline haystack home dir: %w", err)
	}

	return &pythonRAGSeederClient{
		pythonExecutable:  pythonExecutable,
		helperScriptPath:  helperScriptPath,
		qdrantURL:         rawConfig.RAGBaselineConfig.QdrantURL,
		collectionName:    rawConfig.RAGBaselineConfig.CollectionName,
		fastembedCacheDir: fastembedCacheDir,
		homeDirectory:     homeDirectory,
	}, nil
}

func (client *pythonRAGSeederClient) SeedCorpus(ctx context.Context, corpusDocuments []CorpusDocument) error {
	if len(corpusDocuments) == 0 {
		return fmt.Errorf("at least one corpus document is required")
	}
	command := exec.CommandContext(
		ctx,
		client.pythonExecutable,
		client.helperScriptPath,
		"seed",
		"--qdrant-url", client.qdrantURL,
		"--collection", client.collectionName,
	)
	command.Env = append(os.Environ(),
		"HAYSTACK_TELEMETRY_ENABLED=false",
		fmt.Sprintf("FASTEMBED_CACHE_PATH=%s", client.fastembedCacheDir),
		fmt.Sprintf("HOME=%s", client.homeDirectory),
		fmt.Sprintf("XDG_CACHE_HOME=%s", client.homeDirectory),
		"PYTHONUNBUFFERED=1",
	)
	payloadBytes, err := json.Marshal(map[string]any{
		"documents": corpusDocuments,
	})
	if err != nil {
		return fmt.Errorf("marshal rag seed corpus: %w", err)
	}
	command.Stdin = bytes.NewReader(payloadBytes)
	var stderrBuffer bytes.Buffer
	command.Stderr = &stderrBuffer
	if err := command.Run(); err != nil {
		stderrText := strings.TrimSpace(stderrBuffer.String())
		if stderrText == "" {
			return fmt.Errorf("rag baseline seed helper failed: %w", err)
		}
		return fmt.Errorf("rag baseline seed helper failed: %s: %w", stderrText, err)
	}
	return nil
}
