package loopgate

import (
	"sort"
	"strings"
)

func (server *Server) morphlingStatus(tokenClaims capabilityToken, statusRequest MorphlingStatusRequest) (MorphlingStatusResponse, error) {
	if err := server.expirePendingMorphlingApprovals(); err != nil {
		return MorphlingStatusResponse{}, err
	}
	if err := server.expirePendingMorphlingReviews(); err != nil {
		return MorphlingStatusResponse{}, err
	}
	policyRuntime := server.currentPolicyRuntime()

	server.morphlingsMu.Lock()
	defer server.morphlingsMu.Unlock()

	if strings.TrimSpace(statusRequest.MorphlingID) != "" {
		morphlingRecord, found := server.morphlings[strings.TrimSpace(statusRequest.MorphlingID)]
		if !found {
			return MorphlingStatusResponse{}, errMorphlingNotFound
		}
		if morphlingRecord.ParentControlSessionID != tokenClaims.ControlSessionID {
			return MorphlingStatusResponse{}, errMorphlingNotFound
		}
		if morphlingTenantDenied(morphlingRecord, tokenClaims) {
			return MorphlingStatusResponse{}, errMorphlingNotFound
		}
		if !statusRequest.IncludeTerminated && morphlingRecord.State == morphlingStateTerminated {
			return MorphlingStatusResponse{
				SpawnEnabled: policyRuntime.policy.Tools.Morphlings.SpawnEnabled,
				MaxActive:    policyRuntime.policy.Tools.Morphlings.MaxActive,
				ActiveCount:  activeMorphlingCountLocked(server.morphlings),
				Morphlings:   []MorphlingSummary{},
			}, nil
		}
		return MorphlingStatusResponse{
			SpawnEnabled: policyRuntime.policy.Tools.Morphlings.SpawnEnabled,
			MaxActive:    policyRuntime.policy.Tools.Morphlings.MaxActive,
			ActiveCount:  activeMorphlingCountLocked(server.morphlings),
			Morphlings:   []MorphlingSummary{morphlingSummaryFromRecord(morphlingRecord)},
		}, nil
	}

	morphlingIDs := make([]string, 0, len(server.morphlings))
	for morphlingID := range server.morphlings {
		morphlingIDs = append(morphlingIDs, morphlingID)
	}
	sort.Strings(morphlingIDs)

	morphlingSummaries := make([]MorphlingSummary, 0, len(morphlingIDs))
	for _, morphlingID := range morphlingIDs {
		morphlingRecord := server.morphlings[morphlingID]
		if morphlingRecord.ParentControlSessionID != tokenClaims.ControlSessionID {
			continue
		}
		if morphlingTenantDenied(morphlingRecord, tokenClaims) {
			continue
		}
		if !statusRequest.IncludeTerminated && morphlingRecord.State == morphlingStateTerminated {
			continue
		}
		morphlingSummaries = append(morphlingSummaries, morphlingSummaryFromRecord(morphlingRecord))
	}
	return MorphlingStatusResponse{
		SpawnEnabled:       policyRuntime.policy.Tools.Morphlings.SpawnEnabled,
		MaxActive:          policyRuntime.policy.Tools.Morphlings.MaxActive,
		ActiveCount:        activeMorphlingCountLocked(server.morphlings),
		PendingReviewCount: pendingReviewCountLocked(server.morphlings, tokenClaims.ControlSessionID),
		Morphlings:         morphlingSummaries,
	}, nil
}
