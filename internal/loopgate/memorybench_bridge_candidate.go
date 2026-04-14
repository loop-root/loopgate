package loopgate

import (
	"context"
	"fmt"
	"strings"

	"loopgate/internal/config"
	tclpkg "loopgate/internal/tcl"
)

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

// benchmarkNormalizedMemoryCandidateRequest is benchmark-only input shaping for
// candidate-governance evaluation. It intentionally stays separate from the
// live memory.remember request contract because the benchmark needs to score
// non-explicit sources without pretending the product API already accepts them.
type benchmarkNormalizedMemoryCandidateRequest struct {
	FactKey         string
	FactValue       string
	SourceText      string
	CandidateSource string
	SourceChannel   string
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
			ReasonCode:             DenialCodeMemoryCandidateInvalid,
		}, nil
	}

	if validatedRequest.CandidateSource != memoryCandidateSourceExplicitFact {
		return backend.evaluateNonExplicitBenchmarkMemoryCandidate(validatedRequest)
	}

	return backend.evaluateExplicitBenchmarkMemoryCandidate(validatedRequest)
}

func (backend continuityTCLMemoryCandidateGovernanceBackend) evaluateExplicitBenchmarkMemoryCandidate(validatedRequest benchmarkNormalizedMemoryCandidateRequest) (BenchmarkMemoryCandidateDecision, error) {
	validatedCandidateResult, err := buildValidatedMemoryRememberCandidate(MemoryRememberRequest{
		Scope:           memoryScopeGlobal,
		FactKey:         validatedRequest.FactKey,
		FactValue:       validatedRequest.FactValue,
		SourceText:      validatedRequest.SourceText,
		CandidateSource: validatedRequest.CandidateSource,
		SourceChannel:   validatedRequest.SourceChannel,
	})
	if err != nil {
		return BenchmarkMemoryCandidateDecision{
			PersistenceDisposition: "invalid",
			ShouldPersist:          false,
			HardDeny:               true,
			ReasonCode:             DenialCodeMemoryCandidateInvalid,
		}, nil
	}
	denialCode, _, shouldPersist := memoryRememberGovernanceDecision(validatedCandidateResult.ValidatedCandidate.Decision)
	return BenchmarkMemoryCandidateDecision{
		PersistenceDisposition: benchmarkPersistenceDisposition(validatedCandidateResult.ValidatedCandidate.Decision.Disposition, shouldPersist),
		ShouldPersist:          shouldPersist,
		HardDeny:               validatedCandidateResult.ValidatedCandidate.Decision.HardDeny,
		ReasonCode:             strings.TrimSpace(denialCode),
		RiskMotifs:             riskMotifStrings(validatedCandidateResult.ValidatedCandidate.Signatures.RiskMotifs),
	}, nil
}

func (backend continuityTCLMemoryCandidateGovernanceBackend) evaluateNonExplicitBenchmarkMemoryCandidate(validatedRequest benchmarkNormalizedMemoryCandidateRequest) (BenchmarkMemoryCandidateDecision, error) {
	candidateInput, err := benchmarkMemoryCandidateInput(validatedRequest)
	if err != nil {
		return BenchmarkMemoryCandidateDecision{
			PersistenceDisposition: "invalid",
			ShouldPersist:          false,
			HardDeny:               true,
			ReasonCode:             DenialCodeMemoryCandidateInvalid,
		}, nil
	}

	analysisResult, err := tclpkg.AnalyzeMemoryCandidate(candidateInput)
	if err != nil {
		return BenchmarkMemoryCandidateDecision{
			PersistenceDisposition: "invalid",
			ShouldPersist:          false,
			HardDeny:               true,
			ReasonCode:             DenialCodeMemoryCandidateInvalid,
		}, nil
	}

	policyDecision := tclpkg.ValidatedMemoryDecision{
		Disposition:     analysisResult.PolicyDecision.DISP,
		HardDeny:        analysisResult.PolicyDecision.HardDeny,
		ReviewRequired:  analysisResult.PolicyDecision.REVIEW_REQUIRED,
		Risky:           analysisResult.PolicyDecision.RISKY,
		PoisonCandidate: analysisResult.PolicyDecision.POISON_CANDIDATE,
		ReasonCode:      strings.TrimSpace(analysisResult.PolicyDecision.REASON),
	}
	denialCode, _, shouldPersist := memoryRememberGovernanceDecision(policyDecision)
	return BenchmarkMemoryCandidateDecision{
		PersistenceDisposition: benchmarkPersistenceDisposition(policyDecision.Disposition, shouldPersist),
		ShouldPersist:          shouldPersist,
		HardDeny:               policyDecision.HardDeny,
		ReasonCode:             strings.TrimSpace(denialCode),
		RiskMotifs:             riskMotifStrings(analysisResult.Signatures.RiskMotifs),
	}, nil
}

func (backend continuityTCLMemoryCandidateGovernanceBackend) normalizeBenchmarkMemoryCandidateRequest(rawRequest BenchmarkMemoryCandidateRequest) (benchmarkNormalizedMemoryCandidateRequest, error) {
	validatedRequest := benchmarkNormalizedMemoryCandidateRequest{
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
		return benchmarkNormalizedMemoryCandidateRequest{}, fmt.Errorf("fact_key is required")
	}
	if validatedRequest.FactValue == "" {
		return benchmarkNormalizedMemoryCandidateRequest{}, fmt.Errorf("fact_value is required")
	}
	if len([]byte(validatedRequest.FactValue)) > backend.runtimeConfig.Memory.ExplicitFactWrites.MaxValueBytes {
		return benchmarkNormalizedMemoryCandidateRequest{}, fmt.Errorf("fact_value exceeds max_value_bytes")
	}
	if strings.ContainsAny(validatedRequest.FactValue, "\r\n") {
		return benchmarkNormalizedMemoryCandidateRequest{}, fmt.Errorf("fact_value must be a single line")
	}
	if len(strings.TrimSpace(validatedRequest.SourceText)) > 512 {
		return benchmarkNormalizedMemoryCandidateRequest{}, fmt.Errorf("source_text exceeds maximum length")
	}
	if len(validatedRequest.CandidateSource) > 64 {
		return benchmarkNormalizedMemoryCandidateRequest{}, fmt.Errorf("candidate_source exceeds maximum length")
	}
	if len(validatedRequest.SourceChannel) > 64 {
		return benchmarkNormalizedMemoryCandidateRequest{}, fmt.Errorf("source_channel exceeds maximum length")
	}
	if _, err := tclCandidateSourceFromString(validatedRequest.CandidateSource); err != nil {
		return benchmarkNormalizedMemoryCandidateRequest{}, err
	}
	return validatedRequest, nil
}

func benchmarkMemoryCandidateInput(validatedRequest benchmarkNormalizedMemoryCandidateRequest) (tclpkg.MemoryCandidateInput, error) {
	tclCandidateSource, err := tclCandidateSourceFromString(validatedRequest.CandidateSource)
	if err != nil {
		return tclpkg.MemoryCandidateInput{}, err
	}
	return tclpkg.MemoryCandidateInput{
		Source:              tclCandidateSource,
		SourceChannel:       validatedRequest.SourceChannel,
		RawSourceText:       validatedRequest.SourceText,
		NormalizedFactKey:   validatedRequest.FactKey,
		NormalizedFactValue: validatedRequest.FactValue,
		Trust:               trustForMemorySourceChannel(validatedRequest.SourceChannel),
		Actor:               actorForMemorySourceChannel(validatedRequest.SourceChannel),
	}, nil
}

func benchmarkPersistenceDisposition(validatedDisposition tclpkg.Disposition, shouldPersist bool) string {
	if shouldPersist {
		return "persist"
	}
	switch validatedDisposition {
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
