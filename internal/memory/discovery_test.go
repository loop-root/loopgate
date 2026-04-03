package memory

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverResonateKeys_ReturnsTagMatchesInPriorityOrder(t *testing.T) {
	tempDir := t.TempDir()
	keysPath := filepath.Join(tempDir, "keys")

	writeResonateKeyForTest(t, filepath.Join(keysPath, "recent.json"), resonateKeyDocument{
		ID:           "rk-recent",
		SessionID:    "s-recent",
		Scope:        MemoryScopeGlobal,
		StartedAtUTC: "2026-03-08T18:00:00Z",
		EndedAtUTC:   "2026-03-08T18:30:00Z",
		TurnCount:    4,
		Tags:         []string{"github", "incident", "status"},
	})
	writeResonateKeyForTest(t, filepath.Join(keysPath, "older.json"), resonateKeyDocument{
		ID:           "rk-older",
		SessionID:    "s-older",
		Scope:        MemoryScopeGlobal,
		StartedAtUTC: "2026-03-07T18:00:00Z",
		EndedAtUTC:   "2026-03-07T18:30:00Z",
		TurnCount:    2,
		Tags:         []string{"github", "status"},
	})

	discoveryResponse, err := DiscoverResonateKeys(RecallPaths{KeysPath: keysPath}, DiscoveryRequest{
		Query: "github incident status",
	})
	if err != nil {
		t.Fatalf("discover resonate keys: %v", err)
	}
	if len(discoveryResponse.Items) != 2 {
		t.Fatalf("expected two discovery results, got %#v", discoveryResponse.Items)
	}
	if discoveryResponse.Items[0].KeyID != "rk-recent" {
		t.Fatalf("expected highest overlap result first, got %#v", discoveryResponse.Items)
	}
	if discoveryResponse.Items[0].MatchCount <= discoveryResponse.Items[1].MatchCount {
		t.Fatalf("expected strictly higher match count first, got %#v", discoveryResponse.Items)
	}
}

func TestDiscoverResonateKeys_DeniesMeaninglessQuery(t *testing.T) {
	tempDir := t.TempDir()
	_, err := DiscoverResonateKeys(RecallPaths{KeysPath: filepath.Join(tempDir, "keys")}, DiscoveryRequest{
		Query: "a 12 _",
	})
	if err == nil {
		t.Fatal("expected invalid discovery request")
	}
	if !strings.Contains(err.Error(), ErrDiscoveryInvalidRequest.Error()) {
		t.Fatalf("expected invalid discovery request error, got %v", err)
	}
}
