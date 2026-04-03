package tcl

import "strings"

type AnalysisResult struct {
	Node           TCLNode
	Signatures     SignatureSet
	PolicyDecision PolicyDecision
	AnchorVersion  string
	AnchorKey      string
	Projection     SemanticProjection
}

func AnalyzeMemoryCandidate(candidate MemoryCandidate) (AnalysisResult, error) {
	normalizedNode, err := NormalizeMemoryCandidate(candidate)
	if err != nil {
		return AnalysisResult{}, err
	}

	signatureSet, err := DeriveSignatureSet(normalizedNode)
	if err != nil {
		return AnalysisResult{}, err
	}

	policyDecision := EvaluatePolicy(normalizedNode, signatureSet)
	anchorVersion, anchorKey := AnchorTupleForNode(normalizedNode)
	semanticProjection := BuildSemanticProjection(anchorVersion, anchorKey, signatureSet)
	return AnalysisResult{
		Node:           normalizedNode,
		Signatures:     signatureSet,
		PolicyDecision: policyDecision,
		AnchorVersion:  anchorVersion,
		AnchorKey:      anchorKey,
		Projection:     semanticProjection,
	}, nil
}

func AnchorTupleForNode(node TCLNode) (string, string) {
	if node.ANCHOR == nil {
		return "", ""
	}
	return strings.TrimSpace(node.ANCHOR.Version), strings.TrimSpace(node.ANCHOR.CanonicalKey())
}

func BuildSemanticProjection(anchorVersion string, anchorKey string, signatureSet SignatureSet) SemanticProjection {
	return SemanticProjection{
		AnchorVersion:   strings.TrimSpace(anchorVersion),
		AnchorKey:       strings.TrimSpace(anchorKey),
		ExactSignature:  strings.TrimSpace(signatureSet.Exact),
		FamilySignature: strings.TrimSpace(signatureSet.Family),
		RiskMotifs:      append([]RiskMotif(nil), signatureSet.RiskMotifs...),
	}
}
