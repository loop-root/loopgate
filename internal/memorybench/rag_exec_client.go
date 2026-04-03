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

type PythonRAGRetrieverClientConfig struct {
	RepoRoot          string
	PythonExecutable  string
	HelperScriptPath  string
	RAGBaselineConfig RAGBaselineConfig
}

type pythonRAGRetrieverClient struct {
	pythonExecutable  string
	helperScriptPath  string
	qdrantURL         string
	collectionName    string
	embeddingModel    string
	rerankerName      string
	fastembedCacheDir string
	homeDirectory     string
}

func NewPythonRAGRetrieverClient(rawConfig PythonRAGRetrieverClientConfig) (RAGRetrieverClient, error) {
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

	return pythonRAGRetrieverClient{
		pythonExecutable:  pythonExecutable,
		helperScriptPath:  helperScriptPath,
		qdrantURL:         rawConfig.RAGBaselineConfig.QdrantURL,
		collectionName:    rawConfig.RAGBaselineConfig.CollectionName,
		embeddingModel:    rawConfig.RAGBaselineConfig.EmbeddingModel,
		rerankerName:      rawConfig.RAGBaselineConfig.RerankerName,
		fastembedCacheDir: fastembedCacheDir,
		homeDirectory:     homeDirectory,
	}, nil
}

func (client pythonRAGRetrieverClient) Search(ctx context.Context, scope string, query string, maxItems int) ([]RAGSearchResult, error) {
	trimmedQuery := strings.TrimSpace(query)
	if trimmedQuery == "" {
		return []RAGSearchResult{}, nil
	}
	if maxItems <= 0 {
		maxItems = 5
	}

	commandArgs := []string{
		client.helperScriptPath,
		"search",
		"--qdrant-url", client.qdrantURL,
		"--collection", client.collectionName,
		"--query", trimmedQuery,
		"--top-k", fmt.Sprintf("%d", maxItems),
	}
	if strings.TrimSpace(scope) != "" {
		commandArgs = append(commandArgs, "--scope", strings.TrimSpace(scope))
	}
	if strings.TrimSpace(client.embeddingModel) != "" {
		commandArgs = append(commandArgs, "--embedding-model", client.embeddingModel)
	}
	if strings.TrimSpace(client.rerankerName) != "" {
		commandArgs = append(commandArgs, "--reranker-model", client.rerankerName)
		commandArgs = append(commandArgs, "--candidate-pool", fmt.Sprintf("%d", maxItems*3))
	}

	command := exec.CommandContext(ctx, client.pythonExecutable, commandArgs...)
	command.Env = append(os.Environ(),
		"HAYSTACK_TELEMETRY_ENABLED=false",
		fmt.Sprintf("FASTEMBED_CACHE_PATH=%s", client.fastembedCacheDir),
		fmt.Sprintf("HOME=%s", client.homeDirectory),
		fmt.Sprintf("XDG_CACHE_HOME=%s", client.homeDirectory),
		"PYTHONUNBUFFERED=1",
	)

	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	command.Stdout = &stdoutBuffer
	command.Stderr = &stderrBuffer
	if err := command.Run(); err != nil {
		stderrText := strings.TrimSpace(stderrBuffer.String())
		if stderrText == "" {
			return nil, fmt.Errorf("rag baseline helper failed: %w", err)
		}
		return nil, fmt.Errorf("rag baseline helper failed: %s: %w", stderrText, err)
	}

	var parsedResponse struct {
		Results []RAGSearchResult `json:"results"`
	}
	if err := json.Unmarshal(stdoutBuffer.Bytes(), &parsedResponse); err != nil {
		return nil, fmt.Errorf("parse rag baseline helper output: %w", err)
	}
	return parsedResponse.Results, nil
}
