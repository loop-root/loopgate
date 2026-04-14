package loopgate

import (
	"errors"
	"fmt"
	"strings"

	"loopgate/internal/config"
	tclpkg "loopgate/internal/tcl"
)

type analyzedRememberFactCandidate struct {
	ValidatedRequest         MemoryRememberRequest
	ValidatedCandidateResult memoryValidatedCandidate
	ValidatedCandidate       tclpkg.ValidatedMemoryCandidate
}

// analyzedContinuityFactCandidate is the backend-owned, raw-text-free continuity fact
// contract. It stays narrower than the explicit validated write contract because
// continuity still permits bounded unanchored facts that should not be treated as
// explicit remembered state.
type analyzedContinuityFactCandidate struct {
	CanonicalFactKey   string
	CanonicalFactValue string
	SemanticProjection *tclpkg.SemanticProjection
}

func (backend *continuityTCLMemoryBackend) normalizeRememberRequest(rawRequest MemoryRememberRequest) (MemoryRememberRequest, error) {
	return normalizeMemoryRememberRequestForRuntime(backend.server.runtimeConfig, rawRequest)
}

func (backend *continuityTCLMemoryBackend) buildValidatedRememberCandidate(validatedRequest MemoryRememberRequest) (memoryValidatedCandidate, error) {
	if backend.buildValidatedRememberCandidateFn == nil {
		return memoryValidatedCandidate{}, fmt.Errorf("continuity backend validated remember candidate builder is not configured")
	}
	// Keep the injectable builder seam for targeted TCL-failure tests, but keep
	// the seam backend-owned so the live memory authority path does not depend on
	// a Server-level hook.
	return backend.buildValidatedRememberCandidateFn(validatedRequest)
}

func (backend *continuityTCLMemoryBackend) analyzeRememberFactCandidate(authenticatedSession capabilityToken, rawRequest MemoryRememberRequest) (analyzedRememberFactCandidate, error) {
	validatedRequest, err := backend.normalizeRememberRequest(rawRequest)
	if err != nil {
		var governanceError continuityGovernanceError
		if errors.As(err, &governanceError) && governanceError.denialCode == DenialCodeMemoryCandidateInvalid {
			auditRequest := sanitizeDeniedMemoryRememberRequest(rawRequest)
			if auditErr := backend.server.logDeniedMemoryRememberCandidate(authenticatedSession.ControlSessionID, auditRequest, governanceError.denialCode, governanceError.denialCode, map[string]interface{}{
				"tcl_source_channel":   auditRequest.SourceChannel,
				"tcl_candidate_source": auditRequest.CandidateSource,
				"tcl_reason_code":      governanceError.denialCode,
			}); auditErr != nil {
				return analyzedRememberFactCandidate{}, continuityGovernanceError{
					httpStatus:     503,
					responseStatus: ResponseStatusError,
					denialCode:     DenialCodeAuditUnavailable,
					reason:         "control-plane audit is unavailable",
				}
			}
		}
		return analyzedRememberFactCandidate{}, err
	}

	validatedCandidateResult, err := backend.buildValidatedRememberCandidate(validatedRequest)
	if err != nil {
		auditRequest := sanitizeDeniedMemoryRememberRequest(validatedRequest)
		if auditErr := backend.server.logDeniedMemoryRememberCandidate(authenticatedSession.ControlSessionID, auditRequest, DenialCodeMemoryCandidateInvalid, DenialCodeMemoryCandidateInvalid, map[string]interface{}{
			"tcl_source_channel":   auditRequest.SourceChannel,
			"tcl_candidate_source": auditRequest.CandidateSource,
			"tcl_reason_code":      DenialCodeMemoryCandidateInvalid,
		}); auditErr != nil {
			return analyzedRememberFactCandidate{}, continuityGovernanceError{
				httpStatus:     503,
				responseStatus: ResponseStatusError,
				denialCode:     DenialCodeAuditUnavailable,
				reason:         "control-plane audit is unavailable",
			}
		}
		return analyzedRememberFactCandidate{}, continuityGovernanceError{
			httpStatus:     400,
			responseStatus: ResponseStatusDenied,
			denialCode:     DenialCodeMemoryCandidateInvalid,
			reason:         "explicit memory write could not be analyzed safely and was not stored",
		}
	}
	if err := tclpkg.ValidateMemoryCandidateContract(validatedCandidateResult.ValidatedCandidate); err != nil {
		auditRequest := sanitizeDeniedMemoryRememberRequest(validatedRequest)
		if auditErr := backend.server.logDeniedMemoryRememberCandidate(authenticatedSession.ControlSessionID, auditRequest, DenialCodeMemoryCandidateInvalid, DenialCodeMemoryCandidateInvalid, map[string]interface{}{
			"tcl_source_channel":   auditRequest.SourceChannel,
			"tcl_candidate_source": auditRequest.CandidateSource,
			"tcl_reason_code":      DenialCodeMemoryCandidateInvalid,
		}); auditErr != nil {
			return analyzedRememberFactCandidate{}, continuityGovernanceError{
				httpStatus:     503,
				responseStatus: ResponseStatusError,
				denialCode:     DenialCodeAuditUnavailable,
				reason:         "control-plane audit is unavailable",
			}
		}
		return analyzedRememberFactCandidate{}, continuityGovernanceError{
			httpStatus:     400,
			responseStatus: ResponseStatusDenied,
			denialCode:     DenialCodeMemoryCandidateInvalid,
			reason:         "explicit memory write could not be validated safely and was not stored",
		}
	}

	return analyzedRememberFactCandidate{
		ValidatedRequest:         validatedRequest,
		ValidatedCandidateResult: validatedCandidateResult,
		// Revalidate the seam result so tests, shims, and future adapters cannot bypass the contract by
		// returning a half-populated candidate after the legacy request has already been accepted.
		ValidatedCandidate: validatedCandidateResult.ValidatedCandidate,
	}, nil
}

func normalizeMemoryRememberRequestForRuntime(runtimeConfig config.RuntimeConfig, rawRequest MemoryRememberRequest) (MemoryRememberRequest, error) {
	validatedRequest := rawRequest
	validatedRequest.Scope = strings.TrimSpace(validatedRequest.Scope)
	if validatedRequest.Scope == "" {
		validatedRequest.Scope = memoryScopeGlobal
	}
	validatedRequest.FactKey = strings.TrimSpace(validatedRequest.FactKey)
	validatedRequest.FactValue = strings.TrimSpace(validatedRequest.FactValue)
	validatedRequest.Reason = strings.TrimSpace(validatedRequest.Reason)
	validatedRequest.SourceText = strings.TrimSpace(validatedRequest.SourceText)
	validatedRequest.CandidateSource = strings.TrimSpace(validatedRequest.CandidateSource)
	validatedRequest.SourceChannel = strings.TrimSpace(validatedRequest.SourceChannel)
	if err := validatedRequest.Validate(); err != nil {
		return MemoryRememberRequest{}, err
	}
	if validatedRequest.Scope != memoryScopeGlobal {
		return MemoryRememberRequest{}, fmt.Errorf("scope must be %q", memoryScopeGlobal)
	}
	if len([]byte(validatedRequest.FactValue)) > runtimeConfig.Memory.ExplicitFactWrites.MaxValueBytes {
		return MemoryRememberRequest{}, fmt.Errorf("fact_value exceeds max_value_bytes")
	}
	return validatedRequest, nil
}

func sanitizeDeniedMemoryRememberRequest(rawRequest MemoryRememberRequest) MemoryRememberRequest {
	sanitizedRequest := rawRequest
	sanitizedRequest.Scope = strings.TrimSpace(sanitizedRequest.Scope)
	if sanitizedRequest.Scope == "" {
		sanitizedRequest.Scope = memoryScopeGlobal
	}
	sanitizedRequest.FactKey = strings.TrimSpace(sanitizedRequest.FactKey)
	sanitizedRequest.CandidateSource = strings.TrimSpace(sanitizedRequest.CandidateSource)
	if sanitizedRequest.CandidateSource == "" {
		sanitizedRequest.CandidateSource = memoryCandidateSourceExplicitFact
	}
	sanitizedRequest.SourceChannel = strings.TrimSpace(sanitizedRequest.SourceChannel)
	if sanitizedRequest.SourceChannel == "" {
		sanitizedRequest.SourceChannel = memorySourceChannelUnknown
	}
	sanitizedRequest.FactValue = ""
	sanitizedRequest.SourceText = ""
	sanitizedRequest.Reason = ""
	return sanitizedRequest
}

func (backend *continuityTCLMemoryBackend) analyzeContinuityFactCandidate(rawFactKey string, rawFactValue interface{}) (analyzedContinuityFactCandidate, bool) {
	normalizedFactKey := strings.TrimSpace(rawFactKey)
	if normalizedFactKey == "" {
		return analyzedContinuityFactCandidate{}, false
	}

	normalizedFactValue, ok := normalizeContinuityFactValueForPersistence(rawFactValue)
	if !ok {
		return analyzedContinuityFactCandidate{}, false
	}

	analysisResult, err := tclpkg.AnalyzeMemoryCandidate(tclpkg.MemoryCandidateInput{
		Source:              tclpkg.CandidateSourceContinuity,
		SourceChannel:       "continuity_inspection",
		NormalizedFactKey:   normalizedFactKey,
		NormalizedFactValue: normalizedFactValue,
		Trust:               tclpkg.TrustInferred,
		Actor:               tclpkg.ObjectSystem,
	})
	if err != nil {
		return analyzedContinuityFactCandidate{}, false
	}
	if analysisResult.PolicyDecision.HardDeny || analysisResult.PolicyDecision.DISP != tclpkg.DispositionKeep {
		return analyzedContinuityFactCandidate{}, false
	}

	canonicalFactKey := tclpkg.CanonicalizeExplicitMemoryFactKey(normalizedFactKey)
	if canonicalFactKey == "" {
		canonicalFactKey = normalizedFactKey
	}

	return analyzedContinuityFactCandidate{
		CanonicalFactKey:   canonicalFactKey,
		CanonicalFactValue: normalizedFactValue,
		SemanticProjection: cloneSemanticProjection(&analysisResult.Projection),
	}, true
}

func normalizeContinuityFactValueForPersistence(rawFactValue interface{}) (string, bool) {
	switch typedFactValue := rawFactValue.(type) {
	case string:
		normalizedFactValue := strings.TrimSpace(typedFactValue)
		if normalizedFactValue == "" || strings.ContainsAny(normalizedFactValue, "\r\n") {
			return "", false
		}
		return normalizedFactValue, true
	case bool:
		if typedFactValue {
			return "true", true
		}
		return "false", true
	case int:
		return fmt.Sprintf("%d", typedFactValue), true
	case int8:
		return fmt.Sprintf("%d", typedFactValue), true
	case int16:
		return fmt.Sprintf("%d", typedFactValue), true
	case int32:
		return fmt.Sprintf("%d", typedFactValue), true
	case int64:
		return fmt.Sprintf("%d", typedFactValue), true
	case uint:
		return fmt.Sprintf("%d", typedFactValue), true
	case uint8:
		return fmt.Sprintf("%d", typedFactValue), true
	case uint16:
		return fmt.Sprintf("%d", typedFactValue), true
	case uint32:
		return fmt.Sprintf("%d", typedFactValue), true
	case uint64:
		return fmt.Sprintf("%d", typedFactValue), true
	case float32:
		return strings.TrimSpace(fmt.Sprintf("%g", typedFactValue)), true
	case float64:
		return strings.TrimSpace(fmt.Sprintf("%g", typedFactValue)), true
	default:
		// Continuity facts remain bounded scalar context. Persisting nested payloads here
		// would quietly turn inspect packets into an arbitrary structured storage channel.
		return "", false
	}
}
