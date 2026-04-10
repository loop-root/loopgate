package loopgate

import (
	"context"
	"fmt"
	"strings"

	"morph/internal/identifiers"
)

const (
	memoryArtifactKindState = "state_artifact"
	memoryArtifactRefPrefix = "memory://state/"
)

func buildStateMemoryArtifactRef(keyID string) string {
	return memoryArtifactRefPrefix + strings.TrimSpace(keyID)
}

func parseStateMemoryArtifactRef(rawArtifactRef string) (string, error) {
	trimmedArtifactRef := strings.TrimSpace(rawArtifactRef)
	if !strings.HasPrefix(trimmedArtifactRef, memoryArtifactRefPrefix) {
		return "", fmt.Errorf("artifact_ref %q is not a supported state artifact ref", rawArtifactRef)
	}
	keyID := strings.TrimSpace(strings.TrimPrefix(trimmedArtifactRef, memoryArtifactRefPrefix))
	if err := identifiers.ValidateSafeIdentifier("artifact_ref key_id", keyID); err != nil {
		return "", err
	}
	return keyID, nil
}

func memoryRecallItemHintText(recalledItem MemoryRecallItem) string {
	hintParts := make([]string, 0, len(recalledItem.Facts)+len(recalledItem.ActiveGoals)+len(recalledItem.UnresolvedItems)*2)
	for _, recalledFact := range recalledItem.Facts {
		switch typedValue := recalledFact.Value.(type) {
		case string:
			hintParts = appendUniqueMemoryHintPart(hintParts, typedValue)
		default:
			hintParts = appendUniqueMemoryHintPart(hintParts, fmt.Sprint(typedValue))
		}
	}
	for _, activeGoal := range recalledItem.ActiveGoals {
		hintParts = appendUniqueMemoryHintPart(hintParts, activeGoal)
	}
	for _, unresolvedItem := range recalledItem.UnresolvedItems {
		hintParts = appendUniqueMemoryHintPart(hintParts, unresolvedItem.Text)
		hintParts = appendUniqueMemoryHintPart(hintParts, unresolvedItem.NextStep)
	}
	return strings.Join(hintParts, "\n")
}

func memoryArtifactStateClass(recalledItem MemoryRecallItem) string {
	if len(recalledItem.ActiveGoals) > 0 || len(recalledItem.UnresolvedItems) > 0 {
		return memoryFactStateClassAuthoritative
	}
	for _, recalledFact := range recalledItem.Facts {
		if strings.TrimSpace(recalledFact.StateClass) == memoryFactStateClassAuthoritative {
			return memoryFactStateClassAuthoritative
		}
	}
	for _, recalledFact := range recalledItem.Facts {
		if strings.TrimSpace(recalledFact.StateClass) != "" {
			return strings.TrimSpace(recalledFact.StateClass)
		}
	}
	return ""
}

func memoryArtifactTitleAndSummary(recalledItem MemoryRecallItem) (string, string) {
	title := strings.TrimSpace(recalledItem.KeyID)
	switch {
	case len(recalledItem.UnresolvedItems) > 0:
		title = "Task: " + strings.TrimSpace(recalledItem.UnresolvedItems[0].Text)
	case len(recalledItem.ActiveGoals) > 0:
		title = "Goal: " + strings.TrimSpace(recalledItem.ActiveGoals[0])
	case len(recalledItem.Facts) > 0:
		title = fmt.Sprintf("Fact: %s = %s", strings.TrimSpace(recalledItem.Facts[0].Name), truncateStringToByteLimit(fmt.Sprint(recalledItem.Facts[0].Value), 80))
	}

	summary := truncateStringToByteLimit(memoryRecallItemHintText(recalledItem), 240)
	if summary == "" {
		summary = title
	}
	return truncateStringToByteLimit(title, 120), summary
}

func memoryArtifactRefFromRecallItem(recalledItem MemoryRecallItem) MemoryArtifactRef {
	title, summary := memoryArtifactTitleAndSummary(recalledItem)
	return MemoryArtifactRef{
		ArtifactRef:  buildStateMemoryArtifactRef(recalledItem.KeyID),
		Kind:         memoryArtifactKindState,
		StateClass:   memoryArtifactStateClass(recalledItem),
		Scope:        recalledItem.Scope,
		KeyID:        recalledItem.KeyID,
		ThreadID:     recalledItem.ThreadID,
		DistillateID: recalledItem.DistillateID,
		CreatedAtUTC: recalledItem.CreatedAtUTC,
		Title:        title,
		Summary:      summary,
		Tags:         append([]string(nil), recalledItem.Tags...),
	}
}

func (server *Server) lookupMemoryArtifacts(tenantID string, rawRequest MemoryArtifactLookupRequest) (MemoryArtifactLookupResponse, error) {
	validatedRequest := rawRequest
	if err := validatedRequest.Validate(); err != nil {
		return MemoryArtifactLookupResponse{}, err
	}
	discoverResponse, err := server.discoverMemory(tenantID, MemoryDiscoverRequest{
		Scope:    validatedRequest.Scope,
		Query:    validatedRequest.Query,
		MaxItems: validatedRequest.MaxItems,
	})
	if err != nil {
		return MemoryArtifactLookupResponse{}, err
	}
	requestedKeyIDs := make([]string, 0, len(discoverResponse.Items))
	for _, discoveredItem := range discoverResponse.Items {
		if strings.TrimSpace(discoveredItem.KeyID) == "" {
			continue
		}
		requestedKeyIDs = append(requestedKeyIDs, discoveredItem.KeyID)
	}
	if len(requestedKeyIDs) == 0 {
		return MemoryArtifactLookupResponse{
			Scope:         discoverResponse.Scope,
			Query:         discoverResponse.Query,
			RetrievalMode: discoverResponse.RetrievalMode,
		}, nil
	}
	recallResponse, err := server.recallMemory(tenantID, MemoryRecallRequest{
		Scope:         discoverResponse.Scope,
		MaxItems:      len(requestedKeyIDs),
		MaxTokens:     minInt(2000, 256*len(requestedKeyIDs)),
		RequestedKeys: requestedKeyIDs,
	})
	if err != nil {
		return MemoryArtifactLookupResponse{}, err
	}
	recalledItemsByKeyID := make(map[string]MemoryRecallItem, len(recallResponse.Items))
	for _, recalledItem := range recallResponse.Items {
		recalledItemsByKeyID[recalledItem.KeyID] = recalledItem
	}
	artifactRefs := make([]MemoryArtifactRef, 0, len(discoverResponse.Items))
	for _, discoveredItem := range discoverResponse.Items {
		recalledItem, found := recalledItemsByKeyID[discoveredItem.KeyID]
		if !found {
			continue
		}
		artifactRefs = append(artifactRefs, memoryArtifactRefFromRecallItem(recalledItem))
	}
	return MemoryArtifactLookupResponse{
		Scope:         discoverResponse.Scope,
		Query:         discoverResponse.Query,
		RetrievalMode: discoverResponse.RetrievalMode,
		ArtifactRefs:  artifactRefs,
	}, nil
}

func (server *Server) getMemoryArtifacts(tenantID string, rawRequest MemoryArtifactGetRequest) (MemoryArtifactGetResponse, error) {
	validatedRequest := rawRequest
	if err := validatedRequest.Validate(); err != nil {
		return MemoryArtifactGetResponse{}, err
	}
	requestedKeyIDs := make([]string, 0, len(validatedRequest.ArtifactRefs))
	for _, rawArtifactRef := range validatedRequest.ArtifactRefs {
		requestedKeyID, err := parseStateMemoryArtifactRef(rawArtifactRef)
		if err != nil {
			return MemoryArtifactGetResponse{}, err
		}
		requestedKeyIDs = append(requestedKeyIDs, requestedKeyID)
	}
	recallResponse, err := server.recallMemory(tenantID, MemoryRecallRequest{
		Scope:         validatedRequest.Scope,
		MaxItems:      validatedRequest.MaxItems,
		MaxTokens:     validatedRequest.MaxTokens,
		RequestedKeys: requestedKeyIDs,
	})
	if err != nil {
		return MemoryArtifactGetResponse{}, err
	}
	items := make([]MemoryArtifactGetItem, 0, len(recallResponse.Items))
	for _, recalledItem := range recallResponse.Items {
		items = append(items, MemoryArtifactGetItem{
			Ref:             memoryArtifactRefFromRecallItem(recalledItem),
			ContentText:     truncateStringToByteLimit(memoryRecallItemHintText(recalledItem), 512),
			ActiveGoals:     append([]string(nil), recalledItem.ActiveGoals...),
			UnresolvedItems: append([]MemoryWakeStateOpenItem(nil), recalledItem.UnresolvedItems...),
			Facts:           append([]MemoryRecallFact(nil), recalledItem.Facts...),
			EpistemicFlavor: recalledItem.EpistemicFlavor,
		})
	}
	return MemoryArtifactGetResponse{
		Scope:            recallResponse.Scope,
		MaxItems:         recallResponse.MaxItems,
		MaxTokens:        recallResponse.MaxTokens,
		ApproxTokenCount: recallResponse.ApproxTokenCount,
		Items:            items,
	}, nil
}

func (server *Server) lookupMemoryArtifactsForAuthenticatedSession(ctx context.Context, tokenClaims capabilityToken, rawRequest MemoryArtifactLookupRequest) (MemoryArtifactLookupResponse, error) {
	_ = ctx
	return server.lookupMemoryArtifacts(tokenClaims.TenantID, rawRequest)
}

func (server *Server) getMemoryArtifactsForAuthenticatedSession(ctx context.Context, tokenClaims capabilityToken, rawRequest MemoryArtifactGetRequest) (MemoryArtifactGetResponse, error) {
	_ = ctx
	return server.getMemoryArtifacts(tokenClaims.TenantID, rawRequest)
}
