package loopgate

import (
	"context"
	"fmt"
	"strings"

	"morph/internal/config"
)

const memoryDiscoverRetrievalModeHybrid = "hybrid_continuity_state_plus_rag_evidence"

type hybridMemoryBackend struct {
	continuityBackend *continuityTCLMemoryBackend
	evidenceRetriever memoryEvidenceRetriever
	maxEvidenceItems  int
	maxEvidenceBytes  int
}

func (backend *hybridMemoryBackend) Name() string {
	return memoryBackendHybrid
}

func (backend *hybridMemoryBackend) SyncAuthoritativeState(ctx context.Context, authoritativeState continuityMemoryState) error {
	return backend.continuityBackend.SyncAuthoritativeState(ctx, authoritativeState)
}

func (backend *hybridMemoryBackend) RememberFact(ctx context.Context, authenticatedSession capabilityToken, request MemoryRememberRequest) (MemoryRememberResponse, error) {
	return backend.continuityBackend.RememberFact(ctx, authenticatedSession, request)
}

func (backend *hybridMemoryBackend) InspectObservedContinuity(ctx context.Context, authenticatedSession capabilityToken, request ObservedContinuityInspectRequest) (ContinuityInspectResponse, error) {
	return backend.continuityBackend.InspectObservedContinuity(ctx, authenticatedSession, request)
}

func (backend *hybridMemoryBackend) ReviewContinuityInspection(ctx context.Context, authenticatedSession capabilityToken, inspectionID string, request MemoryInspectionReviewRequest) (MemoryInspectionGovernanceResponse, error) {
	return backend.continuityBackend.ReviewContinuityInspection(ctx, authenticatedSession, inspectionID, request)
}

func (backend *hybridMemoryBackend) TombstoneContinuityInspection(ctx context.Context, authenticatedSession capabilityToken, inspectionID string, request MemoryInspectionLineageRequest) (MemoryInspectionGovernanceResponse, error) {
	return backend.continuityBackend.TombstoneContinuityInspection(ctx, authenticatedSession, inspectionID, request)
}

func (backend *hybridMemoryBackend) PurgeContinuityInspection(ctx context.Context, authenticatedSession capabilityToken, inspectionID string, request MemoryInspectionLineageRequest) (MemoryInspectionGovernanceResponse, error) {
	return backend.continuityBackend.PurgeContinuityInspection(ctx, authenticatedSession, inspectionID, request)
}

func (backend *hybridMemoryBackend) BuildWakeState(ctx context.Context, request MemoryWakeStateRequest) (MemoryWakeStateResponse, error) {
	return backend.continuityBackend.BuildWakeState(ctx, request)
}

func (backend *hybridMemoryBackend) Recall(ctx context.Context, request MemoryRecallRequest) (MemoryRecallResponse, error) {
	// Recall stays authoritative-only. Hybrid evidence belongs on discovery, where
	// the caller is explicitly asking for bounded supporting context instead of
	// recalling durable memory artifacts by key.
	return backend.continuityBackend.Recall(ctx, request)
}

func (backend *hybridMemoryBackend) Discover(ctx context.Context, request MemoryDiscoverRequest) (MemoryDiscoverResponse, error) {
	discoverResponse, err := backend.continuityBackend.Discover(ctx, request)
	if err != nil {
		return MemoryDiscoverResponse{}, err
	}
	discoverResponse.RetrievalMode = memoryDiscoverRetrievalModeHybrid
	if backend.evidenceRetriever == nil {
		return MemoryDiscoverResponse{}, fmt.Errorf("hybrid evidence retriever is not configured")
	}

	relatedStateKeys, relatedStateHints, err := backend.hybridStateRelationHints(ctx, request.Scope, discoverResponse.Items)
	if err != nil {
		return MemoryDiscoverResponse{}, err
	}
	evidenceQuery := buildHybridEvidenceLookupQuery(request.Query, relatedStateHints)
	discoverResponse.EvidenceQuery = evidenceQuery

	evidenceSearchResults, err := backend.evidenceRetriever.Search(ctx, request.Scope, evidenceQuery, maxInt(backend.maxEvidenceItems*3, backend.maxEvidenceItems))
	if err != nil {
		return MemoryDiscoverResponse{}, fmt.Errorf("hybrid evidence retrieval failed: %w", err)
	}
	rerankedEvidenceResults := rerankHybridEvidenceSearchResults(evidenceSearchResults, evidenceQuery, relatedStateHints, backend.maxEvidenceItems)
	discoverResponse.Evidence = boundedMemoryEvidenceItems(rerankedEvidenceResults, relatedStateKeys, backend.maxEvidenceBytes)
	return discoverResponse, nil
}

func (backend *hybridMemoryBackend) hybridStateRelationHints(ctx context.Context, scope string, discoveredStateItems []MemoryDiscoverItem) ([]string, []string, error) {
	if len(discoveredStateItems) == 0 {
		return nil, nil, nil
	}
	requestedKeyIDs := make([]string, 0, minInt(2, len(discoveredStateItems)))
	for _, discoveredStateItem := range discoveredStateItems {
		if strings.TrimSpace(discoveredStateItem.KeyID) == "" {
			continue
		}
		requestedKeyIDs = append(requestedKeyIDs, discoveredStateItem.KeyID)
		if len(requestedKeyIDs) >= 2 {
			break
		}
	}
	if len(requestedKeyIDs) == 0 {
		return nil, nil, nil
	}
	recallResponse, err := backend.continuityBackend.Recall(ctx, MemoryRecallRequest{
		Scope:         scope,
		RequestedKeys: requestedKeyIDs,
		MaxItems:      len(requestedKeyIDs),
		MaxTokens:     768,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("recall hybrid state anchors: %w", err)
	}
	relatedStateHints := make([]string, 0, len(recallResponse.Items))
	for _, recalledItem := range recallResponse.Items {
		hintText := hybridStateHintTextFromRecallItem(recalledItem)
		if hintText == "" {
			continue
		}
		relatedStateHints = append(relatedStateHints, hintText)
		if len(relatedStateHints) >= 2 {
			break
		}
	}
	return requestedKeyIDs, relatedStateHints, nil
}

func hybridStateHintTextFromRecallItem(recalledItem MemoryRecallItem) string {
	return memoryRecallItemHintText(recalledItem)
}

func appendUniqueMemoryHintPart(hintParts []string, rawHintPart string) []string {
	trimmedHintPart := strings.TrimSpace(rawHintPart)
	if trimmedHintPart == "" {
		return hintParts
	}
	for _, existingHintPart := range hintParts {
		if existingHintPart == trimmedHintPart {
			return hintParts
		}
	}
	return append(hintParts, trimmedHintPart)
}

func buildHybridEvidenceLookupQuery(rawQuery string, relatedStateHints []string) string {
	trimmedQuery := strings.TrimSpace(rawQuery)
	trimmedRelatedStateHints := make([]string, 0, len(relatedStateHints))
	for _, relatedStateHint := range relatedStateHints {
		trimmedRelatedStateHint := strings.TrimSpace(relatedStateHint)
		if trimmedRelatedStateHint == "" {
			continue
		}
		trimmedRelatedStateHints = append(trimmedRelatedStateHints, trimmedRelatedStateHint)
	}
	if len(trimmedRelatedStateHints) == 0 {
		return trimmedQuery
	}
	// Carry only the already-selected current state into evidence lookup. This
	// keeps the graph bounded: one continuity handle expands into a small related
	// neighborhood instead of flooding the model with the entire memory graph.
	return trimmedQuery + "\nRelated current state:\n" + strings.Join(trimmedRelatedStateHints, "\n")
}

func rerankHybridEvidenceSearchResults(searchResults []memoryEvidenceSearchResult, evidenceQuery string, relatedStateHints []string, maxItems int) []memoryEvidenceSearchResult {
	if len(searchResults) == 0 {
		return nil
	}
	relationTokens := hybridRelationTokenSetFromTexts(append([]string{evidenceQuery}, relatedStateHints...)...)
	remainingSearchResults := append([]memoryEvidenceSearchResult(nil), searchResults...)
	rerankedSearchResults := make([]memoryEvidenceSearchResult, 0, len(remainingSearchResults))
	coveredRelationTokens := map[string]struct{}{}
	for len(remainingSearchResults) > 0 {
		bestIndex := 0
		bestMarginalCoverage := -1
		bestTotalOverlap := -1
		bestMatchCount := -1
		bestEvidenceID := ""
		for currentIndex, currentItem := range remainingSearchResults {
			currentMarginalCoverage := hybridRelationTokenMarginalCoverage(currentItem.Snippet, relationTokens, coveredRelationTokens)
			currentTotalOverlap := hybridRelationTokenOverlapCount(currentItem.Snippet, relationTokens)
			currentEvidenceID := strings.TrimSpace(currentItem.EvidenceID)
			switch {
			case currentMarginalCoverage > bestMarginalCoverage:
				bestIndex = currentIndex
				bestMarginalCoverage = currentMarginalCoverage
				bestTotalOverlap = currentTotalOverlap
				bestMatchCount = currentItem.MatchCount
				bestEvidenceID = currentEvidenceID
			case currentMarginalCoverage == bestMarginalCoverage && currentTotalOverlap > bestTotalOverlap:
				bestIndex = currentIndex
				bestMarginalCoverage = currentMarginalCoverage
				bestTotalOverlap = currentTotalOverlap
				bestMatchCount = currentItem.MatchCount
				bestEvidenceID = currentEvidenceID
			case currentMarginalCoverage == bestMarginalCoverage && currentTotalOverlap == bestTotalOverlap && currentItem.MatchCount > bestMatchCount:
				bestIndex = currentIndex
				bestMarginalCoverage = currentMarginalCoverage
				bestTotalOverlap = currentTotalOverlap
				bestMatchCount = currentItem.MatchCount
				bestEvidenceID = currentEvidenceID
			case currentMarginalCoverage == bestMarginalCoverage && currentTotalOverlap == bestTotalOverlap && currentItem.MatchCount == bestMatchCount && currentEvidenceID < bestEvidenceID:
				bestIndex = currentIndex
				bestMarginalCoverage = currentMarginalCoverage
				bestTotalOverlap = currentTotalOverlap
				bestMatchCount = currentItem.MatchCount
				bestEvidenceID = currentEvidenceID
			}
		}
		bestItem := remainingSearchResults[bestIndex]
		rerankedSearchResults = append(rerankedSearchResults, bestItem)
		for coveredToken := range hybridRelationTokenSetFromTexts(bestItem.Snippet) {
			if _, relationToken := relationTokens[coveredToken]; relationToken {
				coveredRelationTokens[coveredToken] = struct{}{}
			}
		}
		remainingSearchResults = append(remainingSearchResults[:bestIndex], remainingSearchResults[bestIndex+1:]...)
		if maxItems > 0 && len(rerankedSearchResults) >= maxItems {
			break
		}
	}
	return rerankedSearchResults
}

func boundedMemoryEvidenceItems(searchResults []memoryEvidenceSearchResult, relatedStateKeys []string, maxHintBytes int) []MemoryEvidenceItem {
	if len(searchResults) == 0 {
		return nil
	}
	if maxHintBytes <= 0 {
		maxHintBytes = config.DefaultHybridEvidenceMaxHintBytes
	}
	evidenceItems := make([]MemoryEvidenceItem, 0, len(searchResults))
	usedHintBytes := 0
	seenEvidenceKeys := map[string]struct{}{}
	for resultIndex, searchResult := range searchResults {
		evidenceKey := strings.TrimSpace(searchResult.EvidenceID) + "::" + strings.TrimSpace(searchResult.ProvenanceRef)
		if _, seenEvidence := seenEvidenceKeys[evidenceKey]; seenEvidence {
			continue
		}
		seenEvidenceKeys[evidenceKey] = struct{}{}

		snippetText := strings.TrimSpace(searchResult.Snippet)
		if snippetText == "" {
			continue
		}
		if usedHintBytes >= maxHintBytes {
			break
		}
		remainingHintBytes := maxHintBytes - usedHintBytes
		if remainingHintBytes <= 0 {
			break
		}
		if len([]byte(snippetText)) > remainingHintBytes {
			// Keep the first evidence item useful even when one snippet is larger than
			// the budget. Later items are dropped instead of silently widening the prompt.
			if resultIndex > 0 {
				break
			}
			snippetText = truncateStringToByteLimit(snippetText, remainingHintBytes)
		}
		evidenceItems = append(evidenceItems, MemoryEvidenceItem{
			EvidenceID:       strings.TrimSpace(searchResult.EvidenceID),
			SourceKind:       strings.TrimSpace(searchResult.SourceKind),
			Scope:            strings.TrimSpace(searchResult.Scope),
			CreatedAtUTC:     strings.TrimSpace(searchResult.CreatedAtUTC),
			Snippet:          snippetText,
			ProvenanceRef:    strings.TrimSpace(searchResult.ProvenanceRef),
			MatchCount:       searchResult.MatchCount,
			RelatedStateKeys: append([]string(nil), relatedStateKeys...),
		})
		usedHintBytes += len([]byte(snippetText))
	}
	return evidenceItems
}

func truncateStringToByteLimit(rawText string, byteLimit int) string {
	if byteLimit <= 0 {
		return ""
	}
	trimmedText := strings.TrimSpace(rawText)
	if len([]byte(trimmedText)) <= byteLimit {
		return trimmedText
	}
	var builder strings.Builder
	for _, currentRune := range trimmedText {
		currentRuneBytes := len([]byte(string(currentRune)))
		if builder.Len()+currentRuneBytes > byteLimit {
			break
		}
		builder.WriteRune(currentRune)
	}
	return strings.TrimSpace(builder.String())
}

var hybridRelationStopTokens = map[string]struct{}{
	"a": {}, "an": {}, "and": {}, "are": {}, "as": {}, "at": {}, "be": {}, "by": {}, "do": {}, "does": {}, "during": {},
	"find": {}, "follow": {}, "for": {}, "from": {}, "how": {}, "in": {}, "instead": {}, "into": {}, "is": {}, "it": {},
	"keep": {}, "next": {}, "note": {}, "of": {}, "on": {}, "or": {}, "our": {}, "so": {}, "state": {}, "step": {},
	"stays": {}, "still": {}, "task": {}, "that": {}, "the": {}, "their": {}, "them": {}, "then": {}, "this": {},
	"through": {}, "to": {}, "up": {}, "what": {}, "why": {}, "while": {}, "with": {}, "work": {},
}

func hybridRelationTokenSetFromTexts(rawTexts ...string) map[string]struct{} {
	tokenSet := map[string]struct{}{}
	for _, rawText := range rawTexts {
		for _, normalizedToken := range tokenizeLoopgateMemoryText(rawText) {
			if _, ignoredToken := hybridRelationStopTokens[normalizedToken]; ignoredToken {
				continue
			}
			tokenSet[normalizedToken] = struct{}{}
		}
	}
	return tokenSet
}

func hybridRelationTokenOverlapCount(rawText string, relationTokenSet map[string]struct{}) int {
	if len(relationTokenSet) == 0 {
		return 0
	}
	overlapCount := 0
	for _, candidateToken := range tokenizeLoopgateMemoryText(rawText) {
		if _, ignoredToken := hybridRelationStopTokens[candidateToken]; ignoredToken {
			continue
		}
		if _, found := relationTokenSet[candidateToken]; found {
			overlapCount++
		}
	}
	return overlapCount
}

func hybridRelationTokenMarginalCoverage(rawText string, relationTokenSet map[string]struct{}, coveredRelationTokens map[string]struct{}) int {
	if len(relationTokenSet) == 0 {
		return 0
	}
	marginalCoverageCount := 0
	newlyCoveredTokens := map[string]struct{}{}
	for _, candidateToken := range tokenizeLoopgateMemoryText(rawText) {
		if _, ignoredToken := hybridRelationStopTokens[candidateToken]; ignoredToken {
			continue
		}
		if _, relationToken := relationTokenSet[candidateToken]; !relationToken {
			continue
		}
		if _, alreadyCovered := coveredRelationTokens[candidateToken]; alreadyCovered {
			continue
		}
		if _, alreadyCounted := newlyCoveredTokens[candidateToken]; alreadyCounted {
			continue
		}
		newlyCoveredTokens[candidateToken] = struct{}{}
		marginalCoverageCount++
	}
	return marginalCoverageCount
}

func minInt(leftValue int, rightValue int) int {
	if leftValue < rightValue {
		return leftValue
	}
	return rightValue
}

func maxInt(leftValue int, rightValue int) int {
	if leftValue > rightValue {
		return leftValue
	}
	return rightValue
}
