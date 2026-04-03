package memorybench

import (
	"context"
	"errors"
	"testing"
)

type fakeRAGRetrieverClient struct {
	searchResults []RAGSearchResult
	err           error
	lastScope     string
	lastQuery     string
	lastMaxItems  int
}

func (client *fakeRAGRetrieverClient) Search(ctx context.Context, scope string, query string, maxItems int) ([]RAGSearchResult, error) {
	if client.err != nil {
		return nil, client.err
	}
	client.lastScope = scope
	client.lastQuery = query
	client.lastMaxItems = maxItems
	return append([]RAGSearchResult(nil), client.searchResults...), nil
}

func TestNewRAGBaselineDiscovererWithClient_RequiresClient(t *testing.T) {
	_, err := NewRAGBaselineDiscovererWithClient(RAGBaselineConfig{
		QdrantURL:      "http://127.0.0.1:6333",
		CollectionName: "memorybench_default",
	}, nil)
	if err == nil {
		t.Fatal("expected missing retriever client to fail")
	}
}

func TestRAGBaselineDiscoverer_TranslatesSearchResults(t *testing.T) {
	fakeRetrieverClient := &fakeRAGRetrieverClient{
		searchResults: []RAGSearchResult{
			{
				DocumentID:      "doc-1",
				DocumentKind:    "rag_chunk",
				Scope:           "global",
				CreatedAtUTC:    "2026-03-27T00:00:00Z",
				Snippet:         "Grace is the current preferred name",
				ExactSignature:  "sha256:1111111111111111111111111111111111111111111111111111111111111111",
				FamilySignature: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
				ProvenanceRef:   "rag:doc-1",
				Score:           0.97,
			},
		},
	}
	discoverer, err := NewRAGBaselineDiscovererWithClient(RAGBaselineConfig{
		QdrantURL:      "http://127.0.0.1:6333",
		CollectionName: "memorybench_default",
	}, fakeRetrieverClient)
	if err != nil {
		t.Fatalf("NewRAGBaselineDiscovererWithClient: %v", err)
	}

	projectedItems, err := discoverer.DiscoverProjectedNodes(context.Background(), "global", "Grace", 5)
	if err != nil {
		t.Fatalf("DiscoverProjectedNodes: %v", err)
	}
	if len(projectedItems) != 1 {
		t.Fatalf("expected one projected item, got %#v", projectedItems)
	}
	projectedItem := projectedItems[0]
	if projectedItem.NodeID != "doc-1" || projectedItem.NodeKind != "rag_chunk" {
		t.Fatalf("unexpected projected item identity: %#v", projectedItem)
	}
	if projectedItem.HintText != "Grace is the current preferred name" {
		t.Fatalf("unexpected projected item hint: %#v", projectedItem)
	}
	if projectedItem.MatchCount != 3 {
		t.Fatalf("expected score bucket match count 3, got %#v", projectedItem)
	}
	if fakeRetrieverClient.lastScope != "global" || fakeRetrieverClient.lastQuery != "Grace" || fakeRetrieverClient.lastMaxItems != 5 {
		t.Fatalf("expected retriever search inputs to be forwarded, got scope=%q query=%q maxItems=%d", fakeRetrieverClient.lastScope, fakeRetrieverClient.lastQuery, fakeRetrieverClient.lastMaxItems)
	}
}

func TestRAGBaselineDiscoverer_FiltersDifferentScope(t *testing.T) {
	fakeRetrieverClient := &fakeRAGRetrieverClient{
		searchResults: []RAGSearchResult{
			{DocumentID: "doc-1", Scope: "global", Snippet: "Grace", Score: 0.7},
			{DocumentID: "doc-2", Scope: "project", Snippet: "Ada", Score: 0.9},
		},
	}
	discoverer, err := NewRAGBaselineDiscovererWithClient(RAGBaselineConfig{
		QdrantURL:      "http://127.0.0.1:6333",
		CollectionName: "memorybench_default",
	}, fakeRetrieverClient)
	if err != nil {
		t.Fatalf("NewRAGBaselineDiscovererWithClient: %v", err)
	}

	projectedItems, err := discoverer.DiscoverProjectedNodes(context.Background(), "project", "Ada", 5)
	if err != nil {
		t.Fatalf("DiscoverProjectedNodes: %v", err)
	}
	if len(projectedItems) != 1 || projectedItems[0].NodeID != "doc-2" {
		t.Fatalf("expected scoped projected item, got %#v", projectedItems)
	}
	if fakeRetrieverClient.lastScope != "project" {
		t.Fatalf("expected scope to be forwarded to retriever client, got %q", fakeRetrieverClient.lastScope)
	}
}

func TestRAGBaselineDiscoverer_PropagatesSearchError(t *testing.T) {
	discoverer, err := NewRAGBaselineDiscovererWithClient(RAGBaselineConfig{
		QdrantURL:      "http://127.0.0.1:6333",
		CollectionName: "memorybench_default",
	}, &fakeRAGRetrieverClient{err: errors.New("search unavailable")})
	if err != nil {
		t.Fatalf("NewRAGBaselineDiscovererWithClient: %v", err)
	}

	_, err = discoverer.DiscoverProjectedNodes(context.Background(), "global", "Grace", 5)
	if err == nil {
		t.Fatal("expected search error to propagate")
	}
}
