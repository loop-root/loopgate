package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"morph/internal/audit"
	"morph/internal/ledger"
)

func TestBuildGlobalWakeState_WritesBoundedStructuredArtifact(t *testing.T) {
	tempDir := t.TempDir()
	ledgerPath := filepath.Join(tempDir, "ledger.jsonl")
	wakeStatePath := filepath.Join(tempDir, "wake_states", "global.json")

	appendLedgerEvent(t, ledgerPath, map[string]interface{}{
		"ts":      "2026-03-08T17:50:00Z",
		"type":    "memory.goal.opened",
		"session": "s-test",
		"data": map[string]interface{}{
			"goal_id": "goal_status",
			"text":    "monitor service health",
			"continuity_event": map[string]interface{}{
				"type":             ContinuityEventTypeGoalOpened,
				"scope":            MemoryScopeGlobal,
				"epistemic_flavor": EpistemicFlavorRemembered,
				"payload": map[string]interface{}{
					"goal_id": "goal_status",
					"text":    "monitor service health",
				},
			},
		},
	})
	appendLedgerEvent(t, ledgerPath, map[string]interface{}{
		"ts":      "2026-03-08T17:55:00Z",
		"type":    "memory.unresolved_item.opened",
		"session": "s-test",
		"data": map[string]interface{}{
			"item_id": "todo_followup",
			"text":    "review repo issue backlog",
			"continuity_event": map[string]interface{}{
				"type":             ContinuityEventTypeUnresolvedItemOpened,
				"scope":            MemoryScopeGlobal,
				"epistemic_flavor": EpistemicFlavorRemembered,
				"payload": map[string]interface{}{
					"item_id": "todo_followup",
					"text":    "review repo issue backlog",
				},
			},
		},
	})
	appendLedgerEvent(t, ledgerPath, map[string]interface{}{
		"ts":      "2026-03-08T18:00:00Z",
		"type":    "loopgate.capability.result",
		"session": "s-test",
		"data": map[string]interface{}{
			"request_id": "req-status",
			"continuity_event": map[string]interface{}{
				"type":             ContinuityEventTypeProviderFactObserved,
				"scope":            MemoryScopeGlobal,
				"epistemic_flavor": EpistemicFlavorFreshlyChecked,
				"payload": map[string]interface{}{
					"facts": map[string]interface{}{
						"service_id":     "stripe_status",
						"incident_count": 0,
					},
				},
			},
		},
	})
	appendLedgerEvent(t, ledgerPath, map[string]interface{}{
		"ts":      "2026-03-08T18:05:00Z",
		"type":    "memory.resonate_key.created",
		"session": "s-test",
		"data": map[string]interface{}{
			"continuity_event": map[string]interface{}{
				"type":             ContinuityEventTypeResonateKeyCreated,
				"scope":            MemoryScopeGlobal,
				"epistemic_flavor": EpistemicFlavorRemembered,
				"payload": map[string]interface{}{
					"key_id": "rk-s-test",
				},
			},
		},
	})

	if err := BuildGlobalWakeState(WakeStatePaths{
		LedgerPath:    ledgerPath,
		WakeStatePath: wakeStatePath,
	}, "persona:Morph@v0.2.0"); err != nil {
		t.Fatalf("build wake state: %v", err)
	}

	wakeStateBytes, err := os.ReadFile(wakeStatePath)
	if err != nil {
		t.Fatalf("read wake state: %v", err)
	}
	var wakeState WakeState
	if err := json.Unmarshal(wakeStateBytes, &wakeState); err != nil {
		t.Fatalf("parse wake state: %v", err)
	}
	if wakeState.Scope != wakeStateScopeGlobal {
		t.Fatalf("unexpected wake state scope: %#v", wakeState.Scope)
	}
	if wakeState.PersonaRef != "persona:Morph@v0.2.0" {
		t.Fatalf("unexpected persona ref: %#v", wakeState.PersonaRef)
	}
	if len(wakeState.ActiveGoals) != 1 || wakeState.ActiveGoals[0] != "monitor service health" {
		t.Fatalf("unexpected active goals: %#v", wakeState.ActiveGoals)
	}
	if len(wakeState.UnresolvedItems) != 1 || wakeState.UnresolvedItems[0].ID != "todo_followup" {
		t.Fatalf("unexpected unresolved items: %#v", wakeState.UnresolvedItems)
	}
	if len(wakeState.RecentFacts) != 2 {
		t.Fatalf("expected two recent facts, got %#v", wakeState.RecentFacts)
	}
	if wakeState.RecentFacts[0].Name != "incident_count" && wakeState.RecentFacts[1].Name != "incident_count" {
		t.Fatalf("expected incident_count fact, got %#v", wakeState.RecentFacts)
	}
	if len(wakeState.ResonateKeys) != 1 || wakeState.ResonateKeys[0] != "rk-s-test" {
		t.Fatalf("unexpected resonate keys: %#v", wakeState.ResonateKeys)
	}
	if len(wakeState.SourceRefs) == 0 {
		t.Fatalf("expected source refs, got %#v", wakeState.SourceRefs)
	}
}

func TestBuildGlobalWakeState_SkipsWriteWhenNoMemoryCandidatesExist(t *testing.T) {
	tempDir := t.TempDir()
	ledgerPath := filepath.Join(tempDir, "ledger.jsonl")
	wakeStatePath := filepath.Join(tempDir, "wake_states", "global.json")

	appendLedgerEvent(t, ledgerPath, map[string]interface{}{
		"ts":      "2026-03-08T18:00:00Z",
		"type":    "loopgate.capability.result",
		"session": "s-test",
		"data": map[string]interface{}{
			"request_id": "req-display",
		},
	})

	if err := BuildGlobalWakeState(WakeStatePaths{
		LedgerPath:    ledgerPath,
		WakeStatePath: wakeStatePath,
	}, "persona:Morph@v0.2.0"); err != nil {
		t.Fatalf("build wake state: %v", err)
	}
	if _, err := os.Stat(wakeStatePath); !os.IsNotExist(err) {
		t.Fatalf("expected no wake state when no memory candidates exist, got stat err %v", err)
	}
}

func TestLoadAndFormatWakeStateForPrompt(t *testing.T) {
	tempDir := t.TempDir()
	wakeStatePath := filepath.Join(tempDir, "wake_states", "global.json")
	wakeState := WakeState{
		ID:           "wake_global_20260308T180700Z",
		Scope:        wakeStateScopeGlobal,
		CreatedAtUTC: "2026-03-08T18:07:00Z",
		PersonaRef:   "persona:Morph@v0.2.0",
		ActiveGoals:  []string{"monitor service health"},
		UnresolvedItems: []WakeStateOpenItem{{
			ID:   "todo_followup",
			Text: "review repo issue backlog",
		}},
		RecentFacts: []WakeStateRecentFact{
			{
				Name:            "service_id",
				Value:           "stripe_status",
				SourceRef:       "loopgate.capability.result:req-status",
				EpistemicFlavor: EpistemicFlavorFreshlyChecked,
			},
		},
		ResonateKeys: []string{"rk-s-test"},
	}
	if err := os.MkdirAll(filepath.Dir(wakeStatePath), 0700); err != nil {
		t.Fatalf("mkdir wake state dir: %v", err)
	}
	wakeStateBytes, err := json.MarshalIndent(wakeState, "", "  ")
	if err != nil {
		t.Fatalf("marshal wake state: %v", err)
	}
	if err := writePrivateJSONAtomically(wakeStatePath, wakeStateBytes); err != nil {
		t.Fatalf("write wake state: %v", err)
	}

	loadedWakeState, err := LoadWakeState(wakeStatePath)
	if err != nil {
		t.Fatalf("load wake state: %v", err)
	}
	formattedWakeState := FormatWakeStateForPrompt(loadedWakeState)
	if !strings.Contains(formattedWakeState, "historical continuity, not fresh verification") {
		t.Fatalf("expected historical continuity warning, got %q", formattedWakeState)
	}
	if !strings.Contains(formattedWakeState, "remembered_fact: service_id=stripe_status") {
		t.Fatalf("expected remembered fact, got %q", formattedWakeState)
	}
	if !strings.Contains(formattedWakeState, "resonate_keys: rk-s-test") {
		t.Fatalf("expected resonate keys, got %q", formattedWakeState)
	}
	if !strings.Contains(formattedWakeState, "active_goal: monitor service health") {
		t.Fatalf("expected active goal, got %q", formattedWakeState)
	}
	if !strings.Contains(formattedWakeState, "unresolved_item: todo_followup review repo issue backlog") {
		t.Fatalf("expected unresolved item, got %q", formattedWakeState)
	}
}

func TestBuildGlobalWakeState_TrimsToPromptBudgetDeterministically(t *testing.T) {
	tempDir := t.TempDir()
	ledgerPath := filepath.Join(tempDir, "ledger.jsonl")
	wakeStatePath := filepath.Join(tempDir, "wake_states", "global.json")

	appendLedgerEvent(t, ledgerPath, map[string]interface{}{
		"ts":      "2026-03-08T17:50:00Z",
		"type":    "memory.unresolved_item.opened",
		"session": "s-test",
		"data": map[string]interface{}{
			"item_id": "todo_followup",
			"text":    strings.Repeat("followup ", 200),
			"continuity_event": map[string]interface{}{
				"type":             ContinuityEventTypeUnresolvedItemOpened,
				"scope":            MemoryScopeGlobal,
				"epistemic_flavor": EpistemicFlavorRemembered,
				"payload": map[string]interface{}{
					"item_id": "todo_followup",
					"text":    strings.Repeat("followup ", 200),
				},
			},
		},
	})
	for factIndex := 0; factIndex < 12; factIndex++ {
		appendLedgerEvent(t, ledgerPath, map[string]interface{}{
			"ts":      "2026-03-08T18:00:00Z",
			"type":    "loopgate.capability.result",
			"session": "s-test",
			"data": map[string]interface{}{
				"request_id": "req-status",
				"continuity_event": map[string]interface{}{
					"type":             ContinuityEventTypeProviderFactObserved,
					"scope":            MemoryScopeGlobal,
					"epistemic_flavor": EpistemicFlavorFreshlyChecked,
					"payload": map[string]interface{}{
						"facts": map[string]interface{}{
							"fact_name_" + string(rune('a'+factIndex)): strings.Repeat("status ", 200),
						},
					},
				},
			},
		})
	}
	appendLedgerEvent(t, ledgerPath, map[string]interface{}{
		"ts":      "2026-03-08T18:05:00Z",
		"type":    "memory.resonate_key.created",
		"session": "s-test",
		"data": map[string]interface{}{
			"continuity_event": map[string]interface{}{
				"type":             ContinuityEventTypeResonateKeyCreated,
				"scope":            MemoryScopeGlobal,
				"epistemic_flavor": EpistemicFlavorRemembered,
				"payload": map[string]interface{}{
					"key_id": "rk-s-test",
				},
			},
		},
	})

	if err := BuildGlobalWakeState(WakeStatePaths{
		LedgerPath:    ledgerPath,
		WakeStatePath: wakeStatePath,
	}, "persona:Morph@v0.2.0"); err != nil {
		t.Fatalf("build wake state with budget trimming: %v", err)
	}

	wakeStateBytes, err := os.ReadFile(wakeStatePath)
	if err != nil {
		t.Fatalf("read wake state: %v", err)
	}
	var wakeState WakeState
	if err := json.Unmarshal(wakeStateBytes, &wakeState); err != nil {
		t.Fatalf("parse wake state: %v", err)
	}
	if wakeState.ApproxPromptTokens > DefaultWakeStatePromptTokenBudget {
		t.Fatalf("expected wake state to fit prompt budget, got %#v", wakeState)
	}
	if len(wakeState.UnresolvedItems) != 1 {
		t.Fatalf("expected unresolved items to survive trimming, got %#v", wakeState.UnresolvedItems)
	}
	if len(wakeState.RecentFacts) >= 12 {
		t.Fatalf("expected lower-priority items to trim under budget, got %#v", wakeState.RecentFacts)
	}
}

func appendLedgerEvent(t *testing.T, ledgerPath string, ledgerEvent map[string]interface{}) {
	t.Helper()

	rawEventData, _ := ledgerEvent["data"].(map[string]interface{})
	if err := audit.RecordMustPersist(ledgerPath, ledger.Event{
		TS:      strings.TrimSpace(stringValue(ledgerEvent["ts"])),
		Type:    strings.TrimSpace(stringValue(ledgerEvent["type"])),
		Session: strings.TrimSpace(stringValue(ledgerEvent["session"])),
		Data:    rawEventData,
	}); err != nil {
		t.Fatalf("append ledger event: %v", err)
	}
}
