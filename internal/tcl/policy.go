package tcl

func EvaluatePolicy(node TCLNode, signatureSet SignatureSet) PolicyDecision {
	if containsRiskMotif(signatureSet.RiskMotifs, RiskMotifPrivateExternalMemoryWrite) {
		return PolicyDecision{
			TCLDecision: TCLDecision{
				DISP:             DispositionQuarantine,
				REVIEW_REQUIRED:  true,
				RISKY:            true,
				POISON_CANDIDATE: true,
				REASON:           "known_dangerous_semantic_family.private_external_memory_write",
			},
			HardDeny: true,
		}
	}

	return PolicyDecision{
		TCLDecision: TCLDecision{
			DISP:             DispositionKeep,
			REVIEW_REQUIRED:  false,
			RISKY:            false,
			POISON_CANDIDATE: false,
			REASON:           "candidate_signal_allowed",
		},
		HardDeny: false,
	}
}

func containsRiskMotif(riskMotifs []RiskMotif, wantedMotif RiskMotif) bool {
	for _, riskMotif := range riskMotifs {
		if riskMotif == wantedMotif {
			return true
		}
	}
	return false
}
