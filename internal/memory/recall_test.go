package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecallByKeys_ReturnsExactRememberedKey(t *testing.T) {
	tempDir := t.TempDir()
	keysPath := filepath.Join(tempDir, "keys")
	if err := os.MkdirAll(keysPath, 0700); err != nil {
		t.Fatalf("mkdir keys: %v", err)
	}

	writeResonateKeyForTest(t, filepath.Join(keysPath, "s-test.json"), resonateKeyDocument{
		ID:           "rk-s-test",
		SessionID:    "s-test",
		Scope:        MemoryScopeGlobal,
		StartedAtUTC: "2026-03-08T18:00:00Z",
		EndedAtUTC:   "2026-03-08T18:30:00Z",
		TurnCount:    4,
		Tags:         []string{"github", "status"},
	})

	recallResponse, err := RecallByKeys(RecallPaths{KeysPath: keysPath}, RecallRequest{
		RequestedKeys: []string{"rk-s-test"},
	})
	if err != nil {
		t.Fatalf("recall by key: %v", err)
	}
	if recallResponse.Scope != MemoryScopeGlobal {
		t.Fatalf("unexpected scope: %q", recallResponse.Scope)
	}
	if len(recallResponse.Items) != 1 {
		t.Fatalf("unexpected recall items: %#v", recallResponse.Items)
	}
	if recallResponse.Items[0].KeyID != "rk-s-test" {
		t.Fatalf("unexpected recalled key: %#v", recallResponse.Items[0])
	}
	if recallResponse.Items[0].EpistemicFlavor != EpistemicFlavorRemembered {
		t.Fatalf("unexpected epistemic flavor: %#v", recallResponse.Items[0].EpistemicFlavor)
	}
	if recallResponse.Items[0].Scope != MemoryScopeGlobal {
		t.Fatalf("expected default global scope, got %#v", recallResponse.Items[0])
	}
	if recallResponse.ApproxTokenCount == 0 || recallResponse.MaxTokens != DefaultRecallMaxTokens {
		t.Fatalf("expected recall token metadata, got %#v", recallResponse)
	}
}

func TestRecallByKeys_DeniesMissingRequestedKey(t *testing.T) {
	tempDir := t.TempDir()
	_, err := RecallByKeys(RecallPaths{KeysPath: filepath.Join(tempDir, "missing")}, RecallRequest{
		RequestedKeys: []string{"rk-missing"},
	})
	if err == nil {
		t.Fatal("expected missing key denial")
	}
	if !strings.Contains(err.Error(), ErrRecallKeyNotFound.Error()) {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestRecallByKeys_DeniesInvalidRequest(t *testing.T) {
	tempDir := t.TempDir()
	_, err := RecallByKeys(RecallPaths{KeysPath: tempDir}, RecallRequest{})
	if err == nil {
		t.Fatal("expected invalid request error")
	}
	if !strings.Contains(err.Error(), ErrRecallInvalidRequest.Error()) {
		t.Fatalf("expected invalid request error, got %v", err)
	}
}

func TestRecallByKeys_DeniesRequestsThatExceedTokenBudget(t *testing.T) {
	tempDir := t.TempDir()
	keysPath := filepath.Join(tempDir, "keys")
	if err := os.MkdirAll(keysPath, 0700); err != nil {
		t.Fatalf("mkdir keys: %v", err)
	}

	writeResonateKeyForTest(t, filepath.Join(keysPath, "s-test.json"), resonateKeyDocument{
		ID:           "rk-s-test",
		SessionID:    "s-test",
		Scope:        MemoryScopeGlobal,
		StartedAtUTC: "2026-03-08T18:00:00Z",
		EndedAtUTC:   "2026-03-08T18:30:00Z",
		TurnCount:    4,
		Tags:         []string{"github", "incident", "monitoring", "status"},
	})

	_, err := RecallByKeys(RecallPaths{KeysPath: keysPath}, RecallRequest{
		RequestedKeys: []string{"rk-s-test"},
		MaxTokens:     3,
	})
	if err == nil {
		t.Fatal("expected token-budget denial")
	}
	if !strings.Contains(err.Error(), "max_tokens") {
		t.Fatalf("expected max_tokens denial, got %v", err)
	}
}

func TestFormatRecallResponse_StatesHistoricalContinuity(t *testing.T) {
	formattedRecall := FormatRecallResponse(RecallResponse{
		Scope:     MemoryScopeGlobal,
		MaxTokens: 100,
		Items: []RecallItem{{
			KeyID:           "rk-s-test",
			SessionID:       "s-test",
			Scope:           MemoryScopeGlobal,
			StartedAtUTC:    "2026-03-08T18:00:00Z",
			EndedAtUTC:      "2026-03-08T18:30:00Z",
			TurnCount:       4,
			Tags:            []string{"github", "status"},
			EpistemicFlavor: EpistemicFlavorRemembered,
		}},
	})
	if !strings.Contains(formattedRecall, "historical memory, not freshly checked state") {
		t.Fatalf("expected historical warning, got %q", formattedRecall)
	}
	if !strings.Contains(formattedRecall, "max_tokens: 100") {
		t.Fatalf("expected max_tokens line, got %q", formattedRecall)
	}
	if !strings.Contains(formattedRecall, "key_id: rk-s-test") {
		t.Fatalf("expected recalled key output, got %q", formattedRecall)
	}
	if !strings.Contains(formattedRecall, "tags: github, status") {
		t.Fatalf("expected tags output, got %q", formattedRecall)
	}
}

func writeResonateKeyForTest(t *testing.T, keyPath string, keyDocument resonateKeyDocument) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
		t.Fatalf("mkdir key parent: %v", err)
	}
	keyBytes, err := json.MarshalIndent(keyDocument, "", "  ")
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	if err := writePrivateJSONAtomically(keyPath, keyBytes); err != nil {
		t.Fatalf("write key: %v", err)
	}
}
