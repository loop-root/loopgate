package loopgate

import (
	"fmt"
	"strings"

	tclpkg "morph/internal/tcl"
)

const (
	memoryCandidateSourceExplicitFact = "explicit_fact"
	memorySourceChannelUnknown        = "unknown"
	memorySourceChannelUserInput      = "user_input"
	memorySourceChannelCapability     = "capability_request"
	memorySourceChannelShellCommand   = "shell_command"
)

type memoryTCLAnalysis struct {
	Node           tclpkg.TCLNode
	Signatures     tclpkg.SignatureSet
	PolicyDecision tclpkg.PolicyDecision
	Projection     tclpkg.SemanticProjection
	AuditSummary   map[string]interface{}
}

func memoryAnchorTupleForNode(node tclpkg.TCLNode) (string, string) {
	return tclpkg.AnchorTupleForNode(node)
}

func analyzeExplicitMemoryCandidate(validatedRequest MemoryRememberRequest) (memoryTCLAnalysis, error) {
	memoryCandidate, err := memoryCandidateFromRememberRequest(validatedRequest)
	if err != nil {
		return memoryTCLAnalysis{}, err
	}
	analysisResult, err := tclpkg.AnalyzeMemoryCandidate(memoryCandidate)
	if err != nil {
		return memoryTCLAnalysis{}, err
	}
	return memoryTCLAnalysis{
		Node:           analysisResult.Node,
		Signatures:     analysisResult.Signatures,
		PolicyDecision: analysisResult.PolicyDecision,
		Projection:     analysisResult.Projection,
		AuditSummary:   memoryTCLAuditSummary(validatedRequest, analysisResult.Signatures, analysisResult.PolicyDecision),
	}, nil
}

func deriveMemoryCandidateSemanticProjection(memoryCandidate tclpkg.MemoryCandidate) *tclpkg.SemanticProjection {
	analysisResult, err := tclpkg.AnalyzeMemoryCandidate(memoryCandidate)
	if err != nil {
		return nil
	}
	return cloneSemanticProjection(&analysisResult.Projection)
}

func memoryCandidateFromRememberRequest(validatedRequest MemoryRememberRequest) (tclpkg.MemoryCandidate, error) {
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
		return tclpkg.MemoryCandidate{}, err
	}
	return tclpkg.MemoryCandidate{
		Source:              tclCandidateSource,
		SourceChannel:       sourceChannel,
		RawSourceText:       strings.TrimSpace(validatedRequest.SourceText),
		NormalizedFactKey:   validatedRequest.FactKey,
		NormalizedFactValue: validatedRequest.FactValue,
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

func memoryTCLAuditSummary(validatedRequest MemoryRememberRequest, signatureSet tclpkg.SignatureSet, policyDecision tclpkg.PolicyDecision) map[string]interface{} {
	candidateSource := strings.TrimSpace(validatedRequest.CandidateSource)
	if candidateSource == "" {
		candidateSource = memoryCandidateSourceExplicitFact
	}
	sourceChannel := strings.TrimSpace(validatedRequest.SourceChannel)
	if sourceChannel == "" {
		sourceChannel = memorySourceChannelUnknown
	}
	return map[string]interface{}{
		"tcl_disposition":      string(policyDecision.DISP),
		"tcl_reason_code":      policyDecision.REASON,
		"tcl_signature_family": signatureSet.Family,
		"tcl_risk_tier":        memoryTCLRiskTier(policyDecision),
		"tcl_risk_motifs":      riskMotifStrings(signatureSet.RiskMotifs),
		"tcl_source_channel":   sourceChannel,
		"tcl_candidate_source": candidateSource,
	}
}

func memoryTCLRiskTier(policyDecision tclpkg.PolicyDecision) string {
	switch {
	case policyDecision.HardDeny:
		return "high"
	case policyDecision.RISKY:
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

func memoryRememberGovernanceDecision(policyDecision tclpkg.PolicyDecision) (denialCode string, safeReason string, shouldPersist bool) {
	switch {
	case policyDecision.HardDeny:
		return DenialCodeMemoryCandidateDangerous, "explicit memory write was denied as unsafe and was not stored", false
	case policyDecision.DISP == tclpkg.DispositionKeep:
		return "", "", true
	case policyDecision.DISP == tclpkg.DispositionQuarantine:
		return DenialCodeMemoryCandidateQuarantineRequired, "explicit memory write requires quarantine review and was not stored", false
	case policyDecision.DISP == tclpkg.DispositionReview || policyDecision.DISP == tclpkg.DispositionFlag:
		return DenialCodeMemoryCandidateReviewRequired, "explicit memory write requires review and was not stored", false
	default:
		return DenialCodeMemoryCandidateDropped, "explicit memory write was not retained and was not stored", false
	}
}
