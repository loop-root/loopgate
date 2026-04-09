package loopgate

import (
	"errors"
	"fmt"
	"strings"

	"morph/internal/config"
	tclpkg "morph/internal/tcl"
)

type analyzedRememberFactCandidate struct {
	ValidatedRequest         MemoryRememberRequest
	ValidatedCandidateResult memoryValidatedCandidate
	ValidatedCandidate       tclpkg.ValidatedMemoryCandidate
}

func (backend *continuityTCLMemoryBackend) normalizeRememberRequest(rawRequest MemoryRememberRequest) (MemoryRememberRequest, error) {
	return normalizeMemoryRememberRequestForRuntime(backend.server.runtimeConfig, rawRequest)
}

func (backend *continuityTCLMemoryBackend) buildValidatedRememberCandidate(validatedRequest MemoryRememberRequest) (memoryValidatedCandidate, error) {
	// Keep the injectable builder seam for targeted TCL-failure tests, but route the
	// live path through the backend so candidate analysis no longer depends on Server ownership.
	return backend.server.buildValidatedMemoryRememberCandidate(validatedRequest)
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
