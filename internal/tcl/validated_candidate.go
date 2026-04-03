package tcl

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

const ValidatedMemoryCandidateVersion1 = "v1"

var (
	ErrUnsupportedValidatedWriteSource    = errors.New("validated memory write source is unsupported")
	ErrUnsupportedValidatedWriteCandidate = errors.New("validated memory write candidate is unsupported")
	ErrInvalidMemoryCandidateContract     = errors.New("validated memory candidate contract is invalid")

	validatedSourceChannelPattern = regexp.MustCompile(`^[a-z0-9_]+$`)
)

type ValidatedMemoryDecision struct {
	Disposition     Disposition `json:"disposition"`
	HardDeny        bool        `json:"hard_deny,omitempty"`
	ReviewRequired  bool        `json:"review_required,omitempty"`
	Risky           bool        `json:"risky,omitempty"`
	PoisonCandidate bool        `json:"poison_candidate,omitempty"`
	ReasonCode      string      `json:"reason_code"`
}

// ValidatedMemoryCandidate is the raw-text-free memory write contract that downstream persistence
// can trust later. It keeps only canonicalized content and structured metadata so overwrite and
// audit paths do not need to reinterpret user, tool, or model prose.
type ValidatedMemoryCandidate struct {
	Version       string                  `json:"version"`
	Source        CandidateSource         `json:"source"`
	SourceChannel string                  `json:"source_channel"`
	CanonicalKey  string                  `json:"canonical_key"`
	FactValue     string                  `json:"fact_value"`
	Trust         Trust                   `json:"trust"`
	Actor         Object                  `json:"actor"`
	AnchorVersion string                  `json:"anchor_version,omitempty"`
	AnchorKey     string                  `json:"anchor_key,omitempty"`
	Signatures    SignatureSet            `json:"signatures"`
	Decision      ValidatedMemoryDecision `json:"decision"`
	Projection    SemanticProjection      `json:"projection"`
}

func (validatedCandidate ValidatedMemoryCandidate) AnchorTupleKey() string {
	anchorVersion := strings.TrimSpace(validatedCandidate.AnchorVersion)
	anchorKey := strings.TrimSpace(validatedCandidate.AnchorKey)
	if anchorVersion == "" || anchorKey == "" {
		return ""
	}
	return anchorVersion + ":" + anchorKey
}

func BuildValidatedMemoryCandidate(candidateInput MemoryCandidateInput) (ValidatedMemoryCandidate, error) {
	if !isSupportedValidatedWriteSource(candidateInput.Source) {
		return ValidatedMemoryCandidate{}, fmt.Errorf("%w: %q", ErrUnsupportedValidatedWriteSource, candidateInput.Source)
	}

	canonicalFactKey := CanonicalizeExplicitMemoryFactKey(candidateInput.NormalizedFactKey)
	if canonicalFactKey == "" {
		return ValidatedMemoryCandidate{}, fmt.Errorf("%w: canonical key %q is not supported by the validated write contract", ErrUnsupportedValidatedWriteCandidate, candidateInput.NormalizedFactKey)
	}

	normalizedFactValue := strings.TrimSpace(candidateInput.NormalizedFactValue)
	analysisInput := candidateInput
	analysisInput.NormalizedFactKey = canonicalFactKey
	analysisInput.NormalizedFactValue = normalizedFactValue
	analysisResult, err := AnalyzeMemoryCandidate(analysisInput)
	if err != nil {
		return ValidatedMemoryCandidate{}, err
	}

	validatedCandidate := ValidatedMemoryCandidate{
		Version:       ValidatedMemoryCandidateVersion1,
		Source:        candidateInput.Source,
		SourceChannel: strings.TrimSpace(analysisResult.Node.META.SOURCE),
		CanonicalKey:  canonicalFactKey,
		FactValue:     normalizedFactValue,
		Trust:         analysisResult.Node.META.TRUST,
		Actor:         analysisResult.Node.META.ACTOR,
		AnchorVersion: strings.TrimSpace(analysisResult.AnchorVersion),
		AnchorKey:     strings.TrimSpace(analysisResult.AnchorKey),
		Signatures: SignatureSet{
			Exact:      strings.TrimSpace(analysisResult.Signatures.Exact),
			Family:     strings.TrimSpace(analysisResult.Signatures.Family),
			RiskMotifs: append([]RiskMotif(nil), analysisResult.Signatures.RiskMotifs...),
		},
		Decision: flattenValidatedMemoryDecision(analysisResult.PolicyDecision),
		Projection: SemanticProjection{
			AnchorVersion:   strings.TrimSpace(analysisResult.Projection.AnchorVersion),
			AnchorKey:       strings.TrimSpace(analysisResult.Projection.AnchorKey),
			ExactSignature:  strings.TrimSpace(analysisResult.Projection.ExactSignature),
			FamilySignature: strings.TrimSpace(analysisResult.Projection.FamilySignature),
			RiskMotifs:      append([]RiskMotif(nil), analysisResult.Projection.RiskMotifs...),
		},
	}
	if err := ValidateMemoryCandidateContract(validatedCandidate); err != nil {
		return ValidatedMemoryCandidate{}, err
	}
	return validatedCandidate, nil
}

func ValidateMemoryCandidateContract(validatedCandidate ValidatedMemoryCandidate) error {
	if strings.TrimSpace(validatedCandidate.Version) != ValidatedMemoryCandidateVersion1 {
		return fmt.Errorf("%w: unsupported version %q", ErrInvalidMemoryCandidateContract, validatedCandidate.Version)
	}
	if !isSupportedValidatedWriteSource(validatedCandidate.Source) {
		return fmt.Errorf("%w: unsupported source %q", ErrInvalidMemoryCandidateContract, validatedCandidate.Source)
	}
	if err := validateValidatedSourceChannel(validatedCandidate.SourceChannel); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidMemoryCandidateContract, err)
	}

	canonicalFactKey := strings.TrimSpace(validatedCandidate.CanonicalKey)
	if canonicalFactKey == "" {
		return fmt.Errorf("%w: canonical key is required", ErrInvalidMemoryCandidateContract)
	}
	if CanonicalizeExplicitMemoryFactKey(canonicalFactKey) != canonicalFactKey {
		return fmt.Errorf("%w: canonical key %q is not supported", ErrInvalidMemoryCandidateContract, validatedCandidate.CanonicalKey)
	}

	trimmedFactValue := strings.TrimSpace(validatedCandidate.FactValue)
	if trimmedFactValue == "" {
		return fmt.Errorf("%w: fact value is required", ErrInvalidMemoryCandidateContract)
	}
	if len([]byte(trimmedFactValue)) > 256 {
		return fmt.Errorf("%w: fact value exceeds maximum length", ErrInvalidMemoryCandidateContract)
	}
	if strings.ContainsAny(validatedCandidate.FactValue, "\r\n") {
		return fmt.Errorf("%w: fact value must be a single line", ErrInvalidMemoryCandidateContract)
	}
	if !isValidTrust(validatedCandidate.Trust) {
		return fmt.Errorf("%w: invalid trust %q", ErrInvalidMemoryCandidateContract, validatedCandidate.Trust)
	}
	if !isValidObject(validatedCandidate.Actor) {
		return fmt.Errorf("%w: invalid actor %q", ErrInvalidMemoryCandidateContract, validatedCandidate.Actor)
	}

	anchorVersion := strings.TrimSpace(validatedCandidate.AnchorVersion)
	anchorKey := strings.TrimSpace(validatedCandidate.AnchorKey)
	switch {
	case anchorVersion == "" && anchorKey == "":
	case anchorVersion == "" || anchorKey == "":
		return fmt.Errorf("%w: anchor version and key must both be present or absent", ErrInvalidMemoryCandidateContract)
	default:
		if anchorVersion != "v1" {
			return fmt.Errorf("%w: unsupported anchor version %q", ErrInvalidMemoryCandidateContract, validatedCandidate.AnchorVersion)
		}
		if !tclAnchorKeyPattern.MatchString(anchorKey) {
			return fmt.Errorf("%w: invalid anchor key %q", ErrInvalidMemoryCandidateContract, validatedCandidate.AnchorKey)
		}
	}
	expectedConflictAnchor := DeriveExplicitMemoryConflictAnchor(canonicalFactKey, trimmedFactValue)
	if expectedConflictAnchor == nil {
		if anchorVersion != "" || anchorKey != "" {
			return fmt.Errorf("%w: canonical key %q must remain unanchored", ErrInvalidMemoryCandidateContract, validatedCandidate.CanonicalKey)
		}
	} else {
		expectedAnchorVersion := strings.TrimSpace(expectedConflictAnchor.Version)
		expectedAnchorKey := strings.TrimSpace(expectedConflictAnchor.CanonicalKey())
		if anchorVersion != expectedAnchorVersion || anchorKey != expectedAnchorKey {
			return fmt.Errorf("%w: anchor tuple %q:%q does not match canonical key %q", ErrInvalidMemoryCandidateContract, anchorVersion, anchorKey, validatedCandidate.CanonicalKey)
		}
	}

	if err := validateSemanticSignature("exact signature", validatedCandidate.Signatures.Exact); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidMemoryCandidateContract, err)
	}
	if err := validateSemanticSignature("family signature", validatedCandidate.Signatures.Family); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidMemoryCandidateContract, err)
	}
	for _, riskMotif := range validatedCandidate.Signatures.RiskMotifs {
		if !isValidRiskMotif(riskMotif) {
			return fmt.Errorf("%w: invalid risk motif %q", ErrInvalidMemoryCandidateContract, riskMotif)
		}
	}

	if err := validateValidatedDecision(validatedCandidate.Decision); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidMemoryCandidateContract, err)
	}
	if err := ValidateSemanticProjection(validatedCandidate.Projection); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidMemoryCandidateContract, err)
	}
	if strings.TrimSpace(validatedCandidate.Projection.AnchorVersion) != anchorVersion || strings.TrimSpace(validatedCandidate.Projection.AnchorKey) != anchorKey {
		return fmt.Errorf("%w: projection anchor tuple does not match validated anchor tuple", ErrInvalidMemoryCandidateContract)
	}
	if strings.TrimSpace(validatedCandidate.Projection.ExactSignature) != strings.TrimSpace(validatedCandidate.Signatures.Exact) {
		return fmt.Errorf("%w: projection exact signature does not match validated signatures", ErrInvalidMemoryCandidateContract)
	}
	if strings.TrimSpace(validatedCandidate.Projection.FamilySignature) != strings.TrimSpace(validatedCandidate.Signatures.Family) {
		return fmt.Errorf("%w: projection family signature does not match validated signatures", ErrInvalidMemoryCandidateContract)
	}
	if !sameRiskMotifSet(validatedCandidate.Projection.RiskMotifs, validatedCandidate.Signatures.RiskMotifs) {
		return fmt.Errorf("%w: projection risk motifs do not match validated signatures", ErrInvalidMemoryCandidateContract)
	}

	return nil
}

func flattenValidatedMemoryDecision(policyDecision PolicyDecision) ValidatedMemoryDecision {
	return ValidatedMemoryDecision{
		Disposition:     policyDecision.DISP,
		HardDeny:        policyDecision.HardDeny,
		ReviewRequired:  policyDecision.REVIEW_REQUIRED,
		Risky:           policyDecision.RISKY,
		PoisonCandidate: policyDecision.POISON_CANDIDATE,
		ReasonCode:      strings.TrimSpace(policyDecision.REASON),
	}
}

// The validated write contract stays narrower than the analysis pipeline so new sources
// cannot reach persistence semantics until they have explicit validation and tests.
func isSupportedValidatedWriteSource(source CandidateSource) bool {
	return source == CandidateSourceExplicitFact
}

func validateValidatedSourceChannel(rawSourceChannel string) error {
	sourceChannel := strings.TrimSpace(rawSourceChannel)
	if sourceChannel == "" {
		return fmt.Errorf("source channel is required")
	}
	if len(sourceChannel) > 64 {
		return fmt.Errorf("source channel exceeds maximum length")
	}
	if !validatedSourceChannelPattern.MatchString(sourceChannel) {
		return fmt.Errorf("source channel %q must contain only lowercase ascii, digits, or underscore", rawSourceChannel)
	}
	return nil
}

func validateValidatedDecision(validatedDecision ValidatedMemoryDecision) error {
	if !isValidDisposition(validatedDecision.Disposition) {
		return fmt.Errorf("invalid disposition %q", validatedDecision.Disposition)
	}
	if strings.TrimSpace(validatedDecision.ReasonCode) == "" {
		return fmt.Errorf("reason code is required")
	}
	return nil
}

func sameRiskMotifSet(leftRiskMotifs []RiskMotif, rightRiskMotifs []RiskMotif) bool {
	normalizedLeftRiskMotifs := append([]RiskMotif(nil), leftRiskMotifs...)
	normalizedRightRiskMotifs := append([]RiskMotif(nil), rightRiskMotifs...)
	slices.Sort(normalizedLeftRiskMotifs)
	slices.Sort(normalizedRightRiskMotifs)
	return slices.Equal(normalizedLeftRiskMotifs, normalizedRightRiskMotifs)
}
