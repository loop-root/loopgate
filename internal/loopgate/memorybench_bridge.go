package loopgate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"morph/internal/config"
	tclpkg "morph/internal/tcl"
)

type ProjectedNodeDiscoverBackend interface {
	DiscoverProjectedNodes(ctx context.Context, rawRequest ProjectedNodeDiscoverRequest) ([]ProjectedNodeDiscoverItem, error)
}

type MemoryCandidateGovernanceBackend interface {
	EvaluateMemoryCandidate(ctx context.Context, rawRequest BenchmarkMemoryCandidateRequest) (BenchmarkMemoryCandidateDecision, error)
}

type BenchmarkMemoryCandidateRequest struct {
	FactKey         string
	FactValue       string
	SourceText      string
	CandidateSource string
	SourceChannel   string
}

type BenchmarkMemoryCandidateDecision struct {
	PersistenceDisposition string
	ShouldPersist          bool
	HardDeny               bool
	ReasonCode             string
	RiskMotifs             []string
}

func OpenContinuityTCLProjectedNodeDiscoverBackend(repoRoot string) (ProjectedNodeDiscoverBackend, error) {
	benchmarkServer, err := NewServer(repoRoot, filepath.Join(repoRoot, "runtime", "memorybench-loopgate.sock"))
	if err != nil {
		return nil, fmt.Errorf("open loopgate server for benchmark discovery: %w", err)
	}
	benchmarkServer.memoryMu.Lock()
	defaultPartition, partitionErr := benchmarkServer.ensureMemoryPartitionLocked("")
	benchmarkServer.memoryMu.Unlock()
	if partitionErr != nil {
		return nil, fmt.Errorf("benchmark default memory partition: %w", partitionErr)
	}
	if defaultPartition == nil || defaultPartition.backend == nil {
		return nil, fmt.Errorf("benchmark memory backend is not configured")
	}
	continuityBackend, ok := defaultPartition.backend.(*continuityTCLMemoryBackend)
	if !ok {
		return nil, fmt.Errorf("benchmark discovery backend %T does not support projected-node discovery", defaultPartition.backend)
	}
	return continuityBackend, nil
}

func OpenContinuityTCLFixtureProjectedNodeDiscoverBackend(repoRoot string, seedNodes []BenchmarkProjectedNodeSeed) (ProjectedNodeDiscoverBackend, error) {
	if len(seedNodes) == 0 {
		return nil, fmt.Errorf("at least one benchmark projected node seed is required")
	}
	benchmarkStateDir := filepath.Join(repoRoot, "runtime", "memorybench")
	if err := os.MkdirAll(benchmarkStateDir, 0o700); err != nil {
		return nil, fmt.Errorf("ensure benchmark continuity state dir: %w", err)
	}
	temporaryStoreFile, err := os.CreateTemp(benchmarkStateDir, "continuity-fixture-*.sqlite")
	if err != nil {
		return nil, fmt.Errorf("create benchmark continuity sqlite path: %w", err)
	}
	temporaryStorePath := temporaryStoreFile.Name()
	if err := temporaryStoreFile.Close(); err != nil {
		return nil, fmt.Errorf("close benchmark continuity sqlite placeholder: %w", err)
	}
	sqliteStore, err := openContinuitySQLiteStore(temporaryStorePath)
	if err != nil {
		return nil, fmt.Errorf("open benchmark continuity sqlite store: %w", err)
	}
	if err := sqliteStore.replaceBenchmarkProjectedNodes(seedNodes); err != nil {
		return nil, fmt.Errorf("seed benchmark continuity sqlite store: %w", err)
	}
	return &continuityTCLMemoryBackend{
		server:    nil,
		partition: nil,
		store:     sqliteStore,
	}, nil
}

func OpenContinuityTCLMemoryCandidateGovernanceBackend() (MemoryCandidateGovernanceBackend, error) {
	return continuityTCLMemoryCandidateGovernanceBackend{
		runtimeConfig: config.DefaultRuntimeConfig(),
	}, nil
}

type continuityTCLMemoryCandidateGovernanceBackend struct {
	runtimeConfig config.RuntimeConfig
}

func (backend continuityTCLMemoryCandidateGovernanceBackend) EvaluateMemoryCandidate(ctx context.Context, rawRequest BenchmarkMemoryCandidateRequest) (BenchmarkMemoryCandidateDecision, error) {
	_ = ctx
	validatedRequest, err := backend.normalizeBenchmarkMemoryCandidateRequest(rawRequest)
	if err != nil {
		return BenchmarkMemoryCandidateDecision{
			PersistenceDisposition: "invalid",
			ShouldPersist:          false,
			HardDeny:               true,
			ReasonCode:             "candidate_validation_failed",
		}, nil
	}

	tclAnalysis, err := analyzeExplicitMemoryCandidate(validatedRequest)
	if err != nil {
		return BenchmarkMemoryCandidateDecision{
			PersistenceDisposition: "invalid",
			ShouldPersist:          false,
			HardDeny:               true,
			ReasonCode:             DenialCodeMemoryCandidateInvalid,
		}, nil
	}
	denialCode, _, shouldPersist := memoryRememberGovernanceDecision(tclAnalysis.PolicyDecision)
	return BenchmarkMemoryCandidateDecision{
		PersistenceDisposition: benchmarkPersistenceDisposition(tclAnalysis.PolicyDecision, shouldPersist),
		ShouldPersist:          shouldPersist,
		HardDeny:               tclAnalysis.PolicyDecision.HardDeny,
		ReasonCode:             strings.TrimSpace(denialCode),
		RiskMotifs:             riskMotifStrings(tclAnalysis.Signatures.RiskMotifs),
	}, nil
}

func (backend continuityTCLMemoryCandidateGovernanceBackend) normalizeBenchmarkMemoryCandidateRequest(rawRequest BenchmarkMemoryCandidateRequest) (MemoryRememberRequest, error) {
	validatedRequest := MemoryRememberRequest{
		Scope:           memoryScopeGlobal,
		FactKey:         strings.TrimSpace(rawRequest.FactKey),
		FactValue:       strings.TrimSpace(rawRequest.FactValue),
		SourceText:      strings.TrimSpace(rawRequest.SourceText),
		CandidateSource: strings.TrimSpace(rawRequest.CandidateSource),
		SourceChannel:   strings.TrimSpace(rawRequest.SourceChannel),
	}
	if validatedRequest.CandidateSource == "" {
		validatedRequest.CandidateSource = memoryCandidateSourceExplicitFact
	}
	if validatedRequest.SourceChannel == "" {
		validatedRequest.SourceChannel = memorySourceChannelUnknown
	}
	if validatedRequest.FactKey == "" {
		return MemoryRememberRequest{}, fmt.Errorf("fact_key is required")
	}
	if validatedRequest.FactValue == "" {
		return MemoryRememberRequest{}, fmt.Errorf("fact_value is required")
	}
	if len([]byte(validatedRequest.FactValue)) > backend.runtimeConfig.Memory.ExplicitFactWrites.MaxValueBytes {
		return MemoryRememberRequest{}, fmt.Errorf("fact_value exceeds max_value_bytes")
	}
	if len([]byte(validatedRequest.SourceText)) > 512 {
		return MemoryRememberRequest{}, fmt.Errorf("source_text exceeds maximum length")
	}

	candidateSource, err := tclCandidateSourceFromString(validatedRequest.CandidateSource)
	if err != nil {
		return MemoryRememberRequest{}, err
	}
	if candidateSource == tclpkg.CandidateSourceExplicitFact {
		validatedRequest.FactKey = tclpkg.CanonicalizeExplicitMemoryFactKey(validatedRequest.FactKey)
		if validatedRequest.FactKey == "" {
			return MemoryRememberRequest{}, fmt.Errorf("fact_key is unsupported")
		}
	}
	return validatedRequest, nil
}

func benchmarkPersistenceDisposition(policyDecision tclpkg.PolicyDecision, shouldPersist bool) string {
	if shouldPersist {
		return "persist"
	}
	switch policyDecision.DISP {
	case tclpkg.DispositionQuarantine:
		return "quarantine"
	case tclpkg.DispositionReview, tclpkg.DispositionFlag:
		return "review"
	case tclpkg.DispositionDrop:
		return "deny"
	default:
		return "deny"
	}
}
