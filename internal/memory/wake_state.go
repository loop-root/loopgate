package memory

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	wakeStateScopeGlobal = "global"
)

type WakeStatePaths struct {
	LedgerPath    string
	WakeStatePath string
}

type WakeState struct {
	ID                 string                `json:"id"`
	Scope              string                `json:"scope"`
	CreatedAtUTC       string                `json:"created_at_utc"`
	PersonaRef         string                `json:"persona_ref"`
	SourceRefs         []WakeStateSourceRef  `json:"source_refs"`
	ActiveGoals        []string              `json:"active_goals"`
	UnresolvedItems    []WakeStateOpenItem   `json:"unresolved_items"`
	RecentFacts        []WakeStateRecentFact `json:"recent_facts"`
	ResonateKeys       []string              `json:"resonate_keys"`
	PromptTokenBudget  int                   `json:"prompt_token_budget,omitempty"`
	ApproxPromptTokens int                   `json:"approx_prompt_tokens,omitempty"`
}

type WakeStateSourceRef struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

type WakeStateOpenItem struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type WakeStateRecentFact struct {
	Name            string      `json:"name"`
	Value           interface{} `json:"value"`
	SourceRef       string      `json:"source_ref"`
	EpistemicFlavor string      `json:"epistemic_flavor"`
}

type continuityEventRecord struct {
	Type            string
	Scope           string
	ThreadID        string
	EpistemicFlavor string
	SourceRefs      []ContinuitySourceRef
	Payload         map[string]interface{}
}

func LoadWakeState(wakeStatePath string) (WakeState, error) {
	rawWakeStateBytes, err := os.ReadFile(wakeStatePath)
	if err != nil {
		if os.IsNotExist(err) {
			return WakeState{}, nil
		}
		return WakeState{}, err
	}

	var wakeState WakeState
	decoder := json.NewDecoder(bytes.NewReader(rawWakeStateBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wakeState); err != nil {
		return WakeState{}, err
	}
	return wakeState, nil
}

func (wakeState WakeState) Empty() bool {
	return strings.TrimSpace(wakeState.ID) == "" &&
		len(wakeState.ActiveGoals) == 0 &&
		len(wakeState.UnresolvedItems) == 0 &&
		len(wakeState.RecentFacts) == 0 &&
		len(wakeState.ResonateKeys) == 0
}

func FormatWakeStateForPrompt(wakeState WakeState) string {
	if wakeState.Empty() {
		return ""
	}

	formattedLines := []string{
		"Remembered continuity follows. This is historical continuity, not fresh verification.",
		fmt.Sprintf("scope: %s", wakeState.Scope),
	}
	for _, activeGoal := range wakeState.ActiveGoals {
		formattedLines = append(formattedLines, "active_goal: "+strings.TrimSpace(activeGoal))
	}
	for _, unresolvedItem := range wakeState.UnresolvedItems {
		if strings.TrimSpace(unresolvedItem.ID) == "" && strings.TrimSpace(unresolvedItem.Text) == "" {
			continue
		}
		formattedLines = append(formattedLines, fmt.Sprintf("unresolved_item: %s %s", strings.TrimSpace(unresolvedItem.ID), strings.TrimSpace(unresolvedItem.Text)))
	}
	for _, recentFact := range wakeState.RecentFacts {
		formattedLines = append(formattedLines, fmt.Sprintf(
			"remembered_fact: %s=%v (%s via %s)",
			strings.TrimSpace(recentFact.Name),
			recentFact.Value,
			strings.TrimSpace(recentFact.EpistemicFlavor),
			strings.TrimSpace(recentFact.SourceRef),
		))
	}
	if len(wakeState.ResonateKeys) > 0 {
		formattedLines = append(formattedLines, "resonate_keys: "+strings.Join(wakeState.ResonateKeys, ", "))
	}
	return strings.Join(formattedLines, "\n")
}

func BuildGlobalWakeState(paths WakeStatePaths, personaRef string) error {
	ledgerFileHandle, err := openVerifiedMemoryLedger(paths.LedgerPath)
	if err != nil {
		return err
	}
	defer ledgerFileHandle.Close()

	ledgerScanner := bufio.NewScanner(ledgerFileHandle)
	ledgerScanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	factsByName := map[string]WakeStateRecentFact{}
	goalsByID := map[string]string{}
	recentFactNames := make([]string, 0, wakeStateSoftMaxRecentFacts)
	goalOrder := make([]string, 0, wakeStateSoftMaxActiveGoals)
	resonateKeysSeen := map[string]struct{}{}
	resonateKeys := make([]string, 0, wakeStateSoftMaxResonateKeys)
	sourceRefsSeen := map[string]WakeStateSourceRef{}
	sourceRefOrder := make([]string, 0, wakeStateSoftMaxSourceReferences)
	unresolvedItemsByID := map[string]WakeStateOpenItem{}
	unresolvedItemOrder := make([]string, 0, wakeStateSoftMaxUnresolvedItems)

	for ledgerScanner.Scan() {
		var rawLedgerEvent map[string]interface{}
		if err := json.Unmarshal(ledgerScanner.Bytes(), &rawLedgerEvent); err != nil {
			return fmt.Errorf("%w: malformed ledger line in wake-state build: %v", ErrLedgerIntegrity, err)
		}
		rawEventData, _ := rawLedgerEvent["data"].(map[string]interface{})
		continuityEvent, foundContinuityEvent := parseContinuityEvent(rawEventData)
		if !foundContinuityEvent {
			continue
		}
		if strings.TrimSpace(continuityEvent.Scope) != MemoryScopeGlobal {
			continue
		}
		switch strings.TrimSpace(continuityEvent.Type) {
		case ContinuityEventTypeProviderFactObserved:
			candidateFacts, _ := continuityEvent.Payload["facts"].(map[string]interface{})
			if len(candidateFacts) == 0 {
				continue
			}
			sourceRef := wakeStateSourceRefForLedgerEvent(rawLedgerEvent, rawEventData)
			recordWakeStateSourceRef(sourceRefsSeen, &sourceRefOrder, sourceRef)
			factNames := make([]string, 0, len(candidateFacts))
			for factName := range candidateFacts {
				factNames = append(factNames, factName)
			}
			sort.Strings(factNames)
			for _, factName := range factNames {
				recordRecentFact(factsByName, &recentFactNames, WakeStateRecentFact{
					Name:            factName,
					Value:           candidateFacts[factName],
					SourceRef:       sourceRef.Ref,
					EpistemicFlavor: strings.TrimSpace(continuityEvent.EpistemicFlavor),
				})
			}
		case ContinuityEventTypeResonateKeyCreated:
			keyID, _ := continuityEvent.Payload["key_id"].(string)
			keyID = strings.TrimSpace(keyID)
			if keyID == "" {
				continue
			}
			if _, alreadySeen := resonateKeysSeen[keyID]; !alreadySeen {
				resonateKeysSeen[keyID] = struct{}{}
				resonateKeys = append(resonateKeys, keyID)
			}
			recordWakeStateSourceRef(sourceRefsSeen, &sourceRefOrder, WakeStateSourceRef{
				Kind: "resonate_key",
				Ref:  keyID,
			})
		case ContinuityEventTypeGoalOpened:
			goalID := strings.TrimSpace(stringValue(continuityEvent.Payload["goal_id"]))
			goalText := strings.TrimSpace(stringValue(continuityEvent.Payload["text"]))
			if goalID == "" || goalText == "" {
				continue
			}
			goalsByID[goalID] = goalText
			removeWakeStateOrderEntry(&goalOrder, goalID)
			goalOrder = append(goalOrder, goalID)
			recordWakeStateSourceRef(sourceRefsSeen, &sourceRefOrder, wakeStateSourceRefForLedgerEvent(rawLedgerEvent, rawEventData))
		case ContinuityEventTypeGoalClosed:
			goalID := strings.TrimSpace(stringValue(continuityEvent.Payload["goal_id"]))
			if goalID == "" {
				continue
			}
			delete(goalsByID, goalID)
			removeWakeStateOrderEntry(&goalOrder, goalID)
		case ContinuityEventTypeUnresolvedItemOpened:
			itemID := strings.TrimSpace(stringValue(continuityEvent.Payload["item_id"]))
			itemText := strings.TrimSpace(stringValue(continuityEvent.Payload["text"]))
			if itemID == "" || itemText == "" {
				continue
			}
			unresolvedItemsByID[itemID] = WakeStateOpenItem{ID: itemID, Text: itemText}
			removeWakeStateOrderEntry(&unresolvedItemOrder, itemID)
			unresolvedItemOrder = append(unresolvedItemOrder, itemID)
			recordWakeStateSourceRef(sourceRefsSeen, &sourceRefOrder, wakeStateSourceRefForLedgerEvent(rawLedgerEvent, rawEventData))
		case ContinuityEventTypeUnresolvedItemClosed:
			itemID := strings.TrimSpace(stringValue(continuityEvent.Payload["item_id"]))
			if itemID == "" {
				continue
			}
			delete(unresolvedItemsByID, itemID)
			removeWakeStateOrderEntry(&unresolvedItemOrder, itemID)
		}
	}
	if err := ledgerScanner.Err(); err != nil {
		return err
	}

	trimWakeStateOrder(&goalOrder, wakeStateSoftMaxActiveGoals)
	trimWakeStateOrder(&recentFactNames, wakeStateSoftMaxRecentFacts)
	trimWakeStateOrder(&resonateKeys, wakeStateSoftMaxResonateKeys)
	trimWakeStateOrder(&sourceRefOrder, wakeStateSoftMaxSourceReferences)
	trimWakeStateOrder(&unresolvedItemOrder, wakeStateSoftMaxUnresolvedItems)

	activeGoals := make([]string, 0, len(goalOrder))
	for _, goalID := range goalOrder {
		activeGoalText, found := goalsByID[goalID]
		if !found {
			continue
		}
		activeGoals = append(activeGoals, activeGoalText)
	}
	recentFacts := make([]WakeStateRecentFact, 0, len(recentFactNames))
	for _, factName := range recentFactNames {
		recentFacts = append(recentFacts, factsByName[factName])
	}
	sourceRefs := make([]WakeStateSourceRef, 0, len(sourceRefOrder))
	for _, sourceRefKey := range sourceRefOrder {
		sourceRefs = append(sourceRefs, sourceRefsSeen[sourceRefKey])
	}
	unresolvedItems := make([]WakeStateOpenItem, 0, len(unresolvedItemOrder))
	for _, unresolvedItemID := range unresolvedItemOrder {
		unresolvedItem, found := unresolvedItemsByID[unresolvedItemID]
		if !found {
			continue
		}
		unresolvedItems = append(unresolvedItems, unresolvedItem)
	}

	activeGoals, unresolvedItems, recentFacts, resonateKeys, approxPromptTokens := trimWakeStateToPromptBudget(
		activeGoals,
		unresolvedItems,
		recentFacts,
		resonateKeys,
		DefaultWakeStatePromptTokenBudget,
	)

	if len(activeGoals) == 0 && len(unresolvedItems) == 0 && len(recentFacts) == 0 && len(resonateKeys) == 0 {
		return nil
	}

	globalWakeState := WakeState{
		ID:                 "wake_global_" + time.Now().UTC().Format("20060102T150405Z"),
		Scope:              wakeStateScopeGlobal,
		CreatedAtUTC:       time.Now().UTC().Format(time.RFC3339),
		PersonaRef:         strings.TrimSpace(personaRef),
		SourceRefs:         sourceRefs,
		ActiveGoals:        activeGoals,
		UnresolvedItems:    unresolvedItems,
		RecentFacts:        recentFacts,
		ResonateKeys:       resonateKeys,
		PromptTokenBudget:  DefaultWakeStatePromptTokenBudget,
		ApproxPromptTokens: approxPromptTokens,
	}

	if err := os.MkdirAll(filepath.Dir(paths.WakeStatePath), 0700); err != nil {
		return err
	}
	wakeStateBytes, err := json.MarshalIndent(globalWakeState, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateJSONAtomically(paths.WakeStatePath, wakeStateBytes)
}

func parseContinuityEvent(rawEventData map[string]interface{}) (continuityEventRecord, bool) {
	rawContinuityEvent, _ := rawEventData[ContinuityEventKey].(map[string]interface{})
	if len(rawContinuityEvent) > 0 {
		continuityPayload, _ := rawContinuityEvent["payload"].(map[string]interface{})
		return continuityEventRecord{
			Type:            strings.TrimSpace(stringValue(rawContinuityEvent["type"])),
			Scope:           strings.TrimSpace(stringValue(rawContinuityEvent["scope"])),
			ThreadID:        strings.TrimSpace(stringValue(rawContinuityEvent["thread_id"])),
			EpistemicFlavor: strings.TrimSpace(stringValue(rawContinuityEvent["epistemic_flavor"])),
			SourceRefs:      parseContinuitySourceRefs(rawContinuityEvent["source_refs"]),
			Payload:         continuityPayload,
		}, true
	}

	rawMemoryCandidate, _ := rawEventData[MemoryCandidateKey].(map[string]interface{})
	if len(rawMemoryCandidate) == 0 {
		return continuityEventRecord{}, false
	}
	legacyPayload, _ := rawMemoryCandidate["data"].(map[string]interface{})
	switch strings.TrimSpace(stringValue(rawMemoryCandidate["type"])) {
	case MemoryCandidateTypeProviderStructuredFact:
		return continuityEventRecord{
			Type:            ContinuityEventTypeProviderFactObserved,
			Scope:           strings.TrimSpace(stringValue(rawMemoryCandidate["scope"])),
			EpistemicFlavor: strings.TrimSpace(stringValue(rawMemoryCandidate["epistemic_flavor"])),
			ThreadID:        "",
			Payload:         legacyPayload,
		}, true
	case MemoryCandidateTypeResonateKey:
		return continuityEventRecord{
			Type:            ContinuityEventTypeResonateKeyCreated,
			Scope:           strings.TrimSpace(stringValue(rawMemoryCandidate["scope"])),
			EpistemicFlavor: strings.TrimSpace(stringValue(rawMemoryCandidate["epistemic_flavor"])),
			ThreadID:        "",
			Payload:         legacyPayload,
		}, true
	default:
		return continuityEventRecord{}, false
	}
}

func parseContinuitySourceRefs(rawSourceRefs interface{}) []ContinuitySourceRef {
	rawSourceRefList, _ := rawSourceRefs.([]interface{})
	parsedSourceRefs := make([]ContinuitySourceRef, 0, len(rawSourceRefList))
	for _, rawSourceRef := range rawSourceRefList {
		rawSourceRefMap, _ := rawSourceRef.(map[string]interface{})
		if len(rawSourceRefMap) == 0 {
			continue
		}
		parsedSourceRefs = append(parsedSourceRefs, ContinuitySourceRef{
			Kind:   strings.TrimSpace(stringValue(rawSourceRefMap["kind"])),
			Ref:    strings.TrimSpace(stringValue(rawSourceRefMap["ref"])),
			SHA256: strings.TrimSpace(stringValue(rawSourceRefMap["sha256"])),
		})
	}
	return parsedSourceRefs
}

func stringValue(rawValue interface{}) string {
	typedValue, _ := rawValue.(string)
	return typedValue
}

func wakeStateSourceRefForLedgerEvent(rawLedgerEvent map[string]interface{}, rawEventData map[string]interface{}) WakeStateSourceRef {
	ledgerEventType, _ := rawLedgerEvent["type"].(string)
	if requestID, _ := rawEventData["request_id"].(string); strings.TrimSpace(requestID) != "" {
		return WakeStateSourceRef{
			Kind: "ledger_event",
			Ref:  strings.TrimSpace(ledgerEventType) + ":" + strings.TrimSpace(requestID),
		}
	}
	return WakeStateSourceRef{
		Kind: "ledger_event",
		Ref:  strings.TrimSpace(ledgerEventType),
	}
}

func recordRecentFact(
	factsByName map[string]WakeStateRecentFact,
	recentFactNames *[]string,
	recentFact WakeStateRecentFact,
) {
	factsByName[recentFact.Name] = recentFact
	removeWakeStateOrderEntry(recentFactNames, recentFact.Name)
	*recentFactNames = append(*recentFactNames, recentFact.Name)
}

func recordWakeStateSourceRef(
	sourceRefsSeen map[string]WakeStateSourceRef,
	sourceRefOrder *[]string,
	sourceRef WakeStateSourceRef,
) {
	sourceRefKey := sourceRef.Kind + ":" + sourceRef.Ref
	sourceRefsSeen[sourceRefKey] = sourceRef
	removeWakeStateOrderEntry(sourceRefOrder, sourceRefKey)
	*sourceRefOrder = append(*sourceRefOrder, sourceRefKey)
}

func removeWakeStateOrderEntry(order *[]string, key string) {
	filteredOrder := (*order)[:0]
	for _, existingKey := range *order {
		if existingKey == key {
			continue
		}
		filteredOrder = append(filteredOrder, existingKey)
	}
	*order = filteredOrder
}

func trimWakeStateOrder(order *[]string, limit int) {
	if len(*order) <= limit {
		return
	}
	*order = append([]string(nil), (*order)[len(*order)-limit:]...)
}

func trimWakeStateToPromptBudget(
	activeGoals []string,
	unresolvedItems []WakeStateOpenItem,
	recentFacts []WakeStateRecentFact,
	resonateKeys []string,
	tokenBudget int,
) ([]string, []WakeStateOpenItem, []WakeStateRecentFact, []string, int) {
	trimmedActiveGoals := append([]string(nil), activeGoals...)
	trimmedUnresolvedItems := append([]WakeStateOpenItem(nil), unresolvedItems...)
	trimmedRecentFacts := append([]WakeStateRecentFact(nil), recentFacts...)
	trimmedResonateKeys := append([]string(nil), resonateKeys...)

	approxPromptTokens := approximateWakeStatePromptTokens(
		trimmedActiveGoals,
		trimmedUnresolvedItems,
		trimmedRecentFacts,
		trimmedResonateKeys,
	)
	for approxPromptTokens > tokenBudget {
		switch {
		case len(trimmedResonateKeys) > 0:
			trimmedResonateKeys = append([]string(nil), trimmedResonateKeys[1:]...)
		case len(trimmedRecentFacts) > 0:
			trimmedRecentFacts = append([]WakeStateRecentFact(nil), trimmedRecentFacts[1:]...)
		case len(trimmedActiveGoals) > 0:
			trimmedActiveGoals = append([]string(nil), trimmedActiveGoals[1:]...)
		case len(trimmedUnresolvedItems) > 0:
			trimmedUnresolvedItems = append([]WakeStateOpenItem(nil), trimmedUnresolvedItems[1:]...)
		default:
			return nil, nil, nil, nil, 0
		}
		approxPromptTokens = approximateWakeStatePromptTokens(
			trimmedActiveGoals,
			trimmedUnresolvedItems,
			trimmedRecentFacts,
			trimmedResonateKeys,
		)
	}
	return trimmedActiveGoals, trimmedUnresolvedItems, trimmedRecentFacts, trimmedResonateKeys, approxPromptTokens
}
