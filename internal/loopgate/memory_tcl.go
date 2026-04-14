package loopgate

import (
	"fmt"
	"strings"

	tclpkg "loopgate/internal/tcl"
)

const (
	memoryCandidateSourceExplicitFact = "explicit_fact"
	memorySourceChannelUnknown        = "unknown"
	memorySourceChannelUserInput      = "user_input"
	memorySourceChannelCapability     = "capability_request"
	memorySourceChannelShellCommand   = "shell_command"
)

type memoryValidatedCandidate struct {
	ValidatedCandidate tclpkg.ValidatedMemoryCandidate
	AuditSummary       map[string]interface{}
}

// This adapter is the last place legacy remember requests are interpreted.
// Persistence should consume the validated contract it returns, not re-read semantic meaning from request fields.
func buildValidatedMemoryRememberCandidate(validatedRequest MemoryRememberRequest) (memoryValidatedCandidate, error) {
	candidateInput, err := memoryCandidateInputFromRememberRequest(validatedRequest)
	if err != nil {
		return memoryValidatedCandidate{}, err
	}
	validatedCandidate, err := tclpkg.BuildValidatedMemoryCandidate(candidateInput)
	if err != nil {
		return memoryValidatedCandidate{}, err
	}
	return memoryValidatedCandidate{
		ValidatedCandidate: validatedCandidate,
		AuditSummary:       memoryValidatedCandidateAuditSummary(validatedCandidate),
	}, nil
}

func deriveMemoryCandidateSemanticProjection(memoryCandidate tclpkg.MemoryCandidate) *tclpkg.SemanticProjection {
	analysisResult, err := tclpkg.AnalyzeMemoryCandidate(memoryCandidate)
	if err != nil {
		return nil
	}
	return cloneSemanticProjection(&analysisResult.Projection)
}

// The request-to-input adapter stays separate from request preflight so key canonicalization and anchor semantics
// live in the TCL validated-candidate boundary, not in scattered request-normalization helpers.
func memoryCandidateInputFromRememberRequest(validatedRequest MemoryRememberRequest) (tclpkg.MemoryCandidateInput, error) {
	candidateSource := strings.TrimSpace(validatedRequest.CandidateSource)
	if candidateSource == "" {
		candidateSource = memoryCandidateSourceExplicitFact
	}
	sourceChannel := strings.TrimSpace(validatedRequest.SourceChannel)
	if sourceChannel == "" {
		sourceChannel = memorySourceChannelUnknown
	}

	tclCandidateSource, err := tclCandidateSourceFromString(candidateSource)
	if err != nil {
		return tclpkg.MemoryCandidateInput{}, err
	}
	return tclpkg.MemoryCandidateInput{
		Source:              tclCandidateSource,
		SourceChannel:       sourceChannel,
		RawSourceText:       strings.TrimSpace(validatedRequest.SourceText),
		NormalizedFactKey:   strings.TrimSpace(validatedRequest.FactKey),
		NormalizedFactValue: strings.TrimSpace(validatedRequest.FactValue),
		Trust:               trustForMemorySourceChannel(sourceChannel),
		Actor:               actorForMemorySourceChannel(sourceChannel),
	}, nil
}

func tclCandidateSourceFromString(rawCandidateSource string) (tclpkg.CandidateSource, error) {
	switch rawCandidateSource {
	case string(tclpkg.CandidateSourceExplicitFact):
		return tclpkg.CandidateSourceExplicitFact, nil
	case string(tclpkg.CandidateSourceContinuity):
		return tclpkg.CandidateSourceContinuity, nil
	case string(tclpkg.CandidateSourceTaskMetadata):
		return tclpkg.CandidateSourceTaskMetadata, nil
	case string(tclpkg.CandidateSourceWorkflowStep):
		return tclpkg.CandidateSourceWorkflowStep, nil
	case string(tclpkg.CandidateSourceToolOutput):
		return tclpkg.CandidateSourceToolOutput, nil
	case string(tclpkg.CandidateSourceUnknown), "":
		return tclpkg.CandidateSourceUnknown, nil
	default:
		return "", fmt.Errorf("unsupported candidate_source %q", rawCandidateSource)
	}
}

func trustForMemorySourceChannel(sourceChannel string) tclpkg.Trust {
	switch sourceChannel {
	case memorySourceChannelUserInput:
		return tclpkg.TrustUserOriginated
	case memorySourceChannelCapability, memorySourceChannelShellCommand:
		return tclpkg.TrustInferred
	default:
		return tclpkg.TrustInferred
	}
}

func actorForMemorySourceChannel(sourceChannel string) tclpkg.Object {
	switch sourceChannel {
	case memorySourceChannelCapability:
		return tclpkg.ObjectSystem
	default:
		return tclpkg.ObjectUser
	}
}

func memoryValidatedCandidateAuditSummary(validatedCandidate tclpkg.ValidatedMemoryCandidate) map[string]interface{} {
	return map[string]interface{}{
		"tcl_disposition":      string(validatedCandidate.Decision.Disposition),
		"tcl_reason_code":      validatedCandidate.Decision.ReasonCode,
		"tcl_signature_family": validatedCandidate.Signatures.Family,
		"tcl_risk_tier":        memoryTCLRiskTier(validatedCandidate.Decision),
		"tcl_risk_motifs":      riskMotifStrings(validatedCandidate.Signatures.RiskMotifs),
		"tcl_source_channel":   validatedCandidate.SourceChannel,
		"tcl_candidate_source": string(validatedCandidate.Source),
	}
}

func memoryTCLRiskTier(validatedDecision tclpkg.ValidatedMemoryDecision) string {
	switch {
	case validatedDecision.HardDeny:
		return "high"
	case validatedDecision.Risky:
		return "medium"
	default:
		return "low"
	}
}

func riskMotifStrings(riskMotifs []tclpkg.RiskMotif) []string {
	result := make([]string, 0, len(riskMotifs))
	for _, riskMotif := range riskMotifs {
		result = append(result, string(riskMotif))
	}
	return result
}

func mergeMemoryTCLAuditSummary(auditData map[string]interface{}, summary map[string]interface{}) map[string]interface{} {
	if auditData == nil {
		auditData = map[string]interface{}{}
	}
	for key, value := range summary {
		auditData[key] = value
	}
	return auditData
}

func (server *Server) logDeniedMemoryRememberCandidate(controlSessionID string, validatedRequest MemoryRememberRequest, denialCode string, safeReason string, auditSummary map[string]interface{}) error {
	auditData := mergeMemoryTCLAuditSummary(map[string]interface{}{
		"fact_key":    validatedRequest.FactKey,
		"scope":       validatedRequest.Scope,
		"denial_code": denialCode,
		"reason_code": safeReason,
	}, auditSummary)
	return server.logEvent("memory.fact.remember_denied", controlSessionID, auditData)
}

func memoryRememberGovernanceDecision(validatedDecision tclpkg.ValidatedMemoryDecision) (denialCode string, safeReason string, shouldPersist bool) {
	switch {
	case validatedDecision.HardDeny:
		return DenialCodeMemoryCandidateDangerous, "explicit memory write was denied as unsafe and was not stored", false
	case validatedDecision.Disposition == tclpkg.DispositionKeep:
		return "", "", true
	case validatedDecision.Disposition == tclpkg.DispositionQuarantine:
		return DenialCodeMemoryCandidateQuarantineRequired, "explicit memory write requires quarantine review and was not stored", false
	case validatedDecision.Disposition == tclpkg.DispositionReview || validatedDecision.Disposition == tclpkg.DispositionFlag:
		return DenialCodeMemoryCandidateReviewRequired, "explicit memory write requires review and was not stored", false
	default:
		return DenialCodeMemoryCandidateDropped, "explicit memory write was not retained and was not stored", false
	}
}
