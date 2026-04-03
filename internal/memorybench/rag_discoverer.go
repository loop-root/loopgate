package memorybench

import (
	"context"
	"fmt"
	"strings"
)

type RAGSearchResult struct {
	DocumentID      string  `json:"document_id"`
	DocumentKind    string  `json:"document_kind"`
	Scope           string  `json:"scope"`
	CreatedAtUTC    string  `json:"created_at_utc"`
	Snippet         string  `json:"snippet"`
	ExactSignature  string  `json:"exact_signature"`
	FamilySignature string  `json:"family_signature"`
	ProvenanceRef   string  `json:"provenance_ref"`
	Score           float64 `json:"score"`
}

type RAGRetrieverClient interface {
	Search(ctx context.Context, scope string, query string, maxItems int) ([]RAGSearchResult, error)
}

type ragBaselineDiscoverer struct {
	ragBaselineConfig RAGBaselineConfig
	retrieverClient   RAGRetrieverClient
}

func NewRAGBaselineDiscoverer(ragBaselineConfig RAGBaselineConfig) (ProjectedNodeDiscoverer, error) {
	if err := ValidateRAGBaselineConfig(ragBaselineConfig); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("benchmark backend %q is configured but not wired yet", BackendRAGBaseline)
}

func NewRAGBaselineDiscovererWithClient(ragBaselineConfig RAGBaselineConfig, retrieverClient RAGRetrieverClient) (ProjectedNodeDiscoverer, error) {
	if err := ValidateRAGBaselineConfig(ragBaselineConfig); err != nil {
		return nil, err
	}
	if retrieverClient == nil {
		return nil, fmt.Errorf("rag baseline retriever client is required")
	}
	return ragBaselineDiscoverer{
		ragBaselineConfig: ragBaselineConfig,
		retrieverClient:   retrieverClient,
	}, nil
}

func (discoverer ragBaselineDiscoverer) DiscoverProjectedNodes(ctx context.Context, scope string, query string, maxItems int) ([]ProjectedNodeDiscoverItem, error) {
	trimmedQuery := strings.TrimSpace(query)
	if trimmedQuery == "" {
		return []ProjectedNodeDiscoverItem{}, nil
	}
	if maxItems <= 0 {
		maxItems = 5
	}

	searchResults, err := discoverer.retrieverClient.Search(ctx, scope, trimmedQuery, maxItems)
	if err != nil {
		return nil, fmt.Errorf("rag baseline search: %w", err)
	}
	projectedItems := make([]ProjectedNodeDiscoverItem, 0, len(searchResults))
	for _, searchResult := range searchResults {
		resultScope := strings.TrimSpace(searchResult.Scope)
		if strings.TrimSpace(scope) != "" && resultScope != "" && resultScope != scope {
			continue
		}
		projectedItems = append(projectedItems, ProjectedNodeDiscoverItem{
			NodeID:          searchResult.DocumentID,
			NodeKind:        strings.TrimSpace(searchResult.DocumentKind),
			Scope:           resultScope,
			CreatedAtUTC:    searchResult.CreatedAtUTC,
			State:           "active",
			HintText:        searchResult.Snippet,
			ExactSignature:  searchResult.ExactSignature,
			FamilySignature: searchResult.FamilySignature,
			ProvenanceEvent: searchResult.ProvenanceRef,
			MatchCount:      ragScoreToMatchCount(searchResult.Score),
		})
	}
	return projectedItems, nil
}

func ragScoreToMatchCount(score float64) int {
	switch {
	case score >= 0.95:
		return 3
	case score >= 0.5:
		return 2
	case score > 0:
		return 1
	default:
		return 0
	}
}
