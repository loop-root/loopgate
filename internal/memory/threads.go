package memory

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"morph/internal/identifiers"
)

const (
	continuityThreadsSchemaVersion = "1"

	ContinuityThreadStateOpen      = "open"
	ContinuityThreadStateSealed    = "sealed"
	ContinuityThreadStateInspected = "inspected"
	ContinuityThreadStateTombstone = "tombstoned"

	continuityProjectionTokenBudget = 1500
	continuityProjectionMaxGoals    = 3
	continuityProjectionMaxItems    = 4
	continuityProjectionMaxFacts    = 5
)

type ContinuityInspectionThresholds struct {
	SubmitPreviousMinEvents       int `json:"submit_previous_min_events"`
	SubmitPreviousMinPayloadBytes int `json:"submit_previous_min_payload_bytes"`
	SubmitPreviousMinPromptTokens int `json:"submit_previous_min_prompt_tokens"`
}

type ContinuityThreadRecord struct {
	ThreadID                 string   `json:"thread_id"`
	Scope                    string   `json:"scope"`
	State                    string   `json:"state"`
	CreatedAtUTC             string   `json:"created_at_utc"`
	SealedAtUTC              string   `json:"sealed_at_utc,omitempty"`
	LastContinuityEventAtUTC string   `json:"last_continuity_event_at_utc,omitempty"`
	SealReason               string   `json:"seal_reason,omitempty"`
	EventCount               int      `json:"event_count,omitempty"`
	ApproxPayloadBytes       int      `json:"approx_payload_bytes,omitempty"`
	ApproxPromptTokens       int      `json:"approx_prompt_tokens,omitempty"`
	InspectionID             string   `json:"inspection_id,omitempty"`
	InspectionCompletedAtUTC string   `json:"inspection_completed_at_utc,omitempty"`
	DerivedDistillateIDs     []string `json:"derived_distillate_ids,omitempty"`
	DerivedResonateKeyIDs    []string `json:"derived_resonate_key_ids,omitempty"`
	TombstonedAtUTC          string   `json:"tombstoned_at_utc,omitempty"`
}

type ContinuityThreadsState struct {
	SchemaVersion    string
	CurrentThreadID  string
	NextThreadID     string
	PreviousThreadID string
	Thresholds       ContinuityInspectionThresholds
	Threads          map[string]ContinuityThreadRecord
}

type continuityThreadsStateFile struct {
	SchemaVersion    string                         `json:"schema_version"`
	CurrentThreadID  string                         `json:"current_thread_id"`
	NextThreadID     string                         `json:"next_thread_id"`
	PreviousThreadID string                         `json:"previous_thread_id,omitempty"`
	Thresholds       ContinuityInspectionThresholds `json:"thresholds"`
	Threads          []ContinuityThreadRecord       `json:"threads"`
}

type ContinuitySourceRef struct {
	Kind   string `json:"kind"`
	Ref    string `json:"ref"`
	SHA256 string `json:"sha256,omitempty"`
}

type ContinuityThreadEvent struct {
	TimestampUTC    string                 `json:"ts_utc"`
	SessionID       string                 `json:"session_id"`
	Type            string                 `json:"type"`
	Scope           string                 `json:"scope"`
	ThreadID        string                 `json:"thread_id"`
	EpistemicFlavor string                 `json:"epistemic_flavor"`
	LedgerSequence  int64                  `json:"ledger_sequence"`
	EventHash       string                 `json:"event_hash"`
	SourceRefs      []ContinuitySourceRef  `json:"source_refs,omitempty"`
	Payload         map[string]interface{} `json:"payload,omitempty"`
}

type ContinuityThreadSummary struct {
	ThreadID           string
	Scope              string
	EventCount         int
	ApproxPayloadBytes int
	ApproxPromptTokens int
	LastEventAtUTC     string
	Tags               []string
	ActiveGoals        []string
	UnresolvedItems    []WakeStateOpenItem
	RecentFacts        []WakeStateRecentFact
	SourceRefs         []WakeStateSourceRef
}

type ContinuityProjection struct {
	Current  *ContinuityThreadSummary
	Next     *ContinuityThreadSummary
	Previous *ContinuityThreadSummary
}

type ContinuityInspectCandidate struct {
	InspectionID       string
	ThreadID           string
	Scope              string
	SealedAtUTC        string
	EventCount         int
	ApproxPayloadBytes int
	ApproxPromptTokens int
	Tags               []string
	Events             []ContinuityThreadEvent
}

func LoadOrInitContinuityThreads(path string, thresholds ContinuityInspectionThresholds, now time.Time) (ContinuityThreadsState, error) {
	rawStateBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			initialState := newContinuityThreadsState(now, thresholds)
			return initialState, SaveContinuityThreads(path, initialState)
		}
		return ContinuityThreadsState{}, err
	}

	var parsedStateFile continuityThreadsStateFile
	decoder := json.NewDecoder(bytes.NewReader(rawStateBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parsedStateFile); err != nil {
		corruptPath := path + ".corrupt." + now.UTC().Format("20060102-150405")
		_ = os.Rename(path, corruptPath)

		freshState := newContinuityThreadsState(now, thresholds)
		if saveErr := SaveContinuityThreads(path, freshState); saveErr != nil {
			return ContinuityThreadsState{}, saveErr
		}
		return freshState, nil
	}

	loadedState := ContinuityThreadsState{
		SchemaVersion:    strings.TrimSpace(parsedStateFile.SchemaVersion),
		CurrentThreadID:  strings.TrimSpace(parsedStateFile.CurrentThreadID),
		NextThreadID:     strings.TrimSpace(parsedStateFile.NextThreadID),
		PreviousThreadID: strings.TrimSpace(parsedStateFile.PreviousThreadID),
		Thresholds:       parsedStateFile.Thresholds,
		Threads:          make(map[string]ContinuityThreadRecord, len(parsedStateFile.Threads)),
	}
	if loadedState.SchemaVersion == "" {
		loadedState.SchemaVersion = continuityThreadsSchemaVersion
	}
	if loadedState.Thresholds.SubmitPreviousMinEvents == 0 {
		loadedState.Thresholds = thresholds
	}
	for _, threadRecord := range parsedStateFile.Threads {
		loadedState.Threads[threadRecord.ThreadID] = cloneContinuityThreadRecord(threadRecord)
	}
	if err := loadedState.Validate(); err != nil {
		corruptPath := path + ".corrupt." + now.UTC().Format("20060102-150405")
		_ = os.Rename(path, corruptPath)

		freshState := newContinuityThreadsState(now, thresholds)
		if saveErr := SaveContinuityThreads(path, freshState); saveErr != nil {
			return ContinuityThreadsState{}, saveErr
		}
		return freshState, nil
	}
	return loadedState, nil
}

func SaveContinuityThreads(path string, continuityState ContinuityThreadsState) error {
	if err := continuityState.Validate(); err != nil {
		return err
	}

	threadIDs := make([]string, 0, len(continuityState.Threads))
	for threadID := range continuityState.Threads {
		threadIDs = append(threadIDs, threadID)
	}
	sort.Strings(threadIDs)

	stateFile := continuityThreadsStateFile{
		SchemaVersion:    continuityState.SchemaVersion,
		CurrentThreadID:  continuityState.CurrentThreadID,
		NextThreadID:     continuityState.NextThreadID,
		PreviousThreadID: continuityState.PreviousThreadID,
		Thresholds:       continuityState.Thresholds,
		Threads:          make([]ContinuityThreadRecord, 0, len(threadIDs)),
	}
	for _, threadID := range threadIDs {
		stateFile.Threads = append(stateFile.Threads, cloneContinuityThreadRecord(continuityState.Threads[threadID]))
	}

	stateBytes, err := json.MarshalIndent(stateFile, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateJSONAtomically(path, stateBytes)
}

func (continuityState ContinuityThreadsState) Validate() error {
	if strings.TrimSpace(continuityState.SchemaVersion) == "" {
		return fmt.Errorf("schema_version is required")
	}
	if continuityState.Thresholds.SubmitPreviousMinEvents < 0 {
		return fmt.Errorf("submit_previous_min_events must be non-negative")
	}
	if continuityState.Thresholds.SubmitPreviousMinPayloadBytes < 0 {
		return fmt.Errorf("submit_previous_min_payload_bytes must be non-negative")
	}
	if continuityState.Thresholds.SubmitPreviousMinPromptTokens < 0 {
		return fmt.Errorf("submit_previous_min_prompt_tokens must be non-negative")
	}
	if len(continuityState.Threads) == 0 {
		return fmt.Errorf("threads is required")
	}
	currentThreadRecord, foundCurrent := continuityState.Threads[continuityState.CurrentThreadID]
	if !foundCurrent {
		return fmt.Errorf("current_thread_id %q is missing", continuityState.CurrentThreadID)
	}
	nextThreadRecord, foundNext := continuityState.Threads[continuityState.NextThreadID]
	if !foundNext {
		return fmt.Errorf("next_thread_id %q is missing", continuityState.NextThreadID)
	}
	if continuityState.CurrentThreadID == continuityState.NextThreadID {
		return fmt.Errorf("current_thread_id and next_thread_id must differ")
	}
	if currentThreadRecord.State != ContinuityThreadStateOpen {
		return fmt.Errorf("current thread must remain open")
	}
	if nextThreadRecord.State != ContinuityThreadStateOpen {
		return fmt.Errorf("next thread must remain open")
	}
	if continuityState.PreviousThreadID != "" {
		previousThreadRecord, foundPrevious := continuityState.Threads[continuityState.PreviousThreadID]
		if !foundPrevious {
			return fmt.Errorf("previous_thread_id %q is missing", continuityState.PreviousThreadID)
		}
		if previousThreadRecord.State == ContinuityThreadStateOpen {
			return fmt.Errorf("previous thread must not remain open")
		}
	}
	for threadID, threadRecord := range continuityState.Threads {
		if threadID != threadRecord.ThreadID {
			return fmt.Errorf("thread map key %q does not match record %q", threadID, threadRecord.ThreadID)
		}
		if err := validateContinuityThreadRecord(threadRecord); err != nil {
			return fmt.Errorf("validate thread %q: %w", threadID, err)
		}
	}
	return nil
}

func CloneContinuityThreadsState(continuityState ContinuityThreadsState) ContinuityThreadsState {
	clonedState := ContinuityThreadsState{
		SchemaVersion:    continuityState.SchemaVersion,
		CurrentThreadID:  continuityState.CurrentThreadID,
		NextThreadID:     continuityState.NextThreadID,
		PreviousThreadID: continuityState.PreviousThreadID,
		Thresholds:       continuityState.Thresholds,
		Threads:          make(map[string]ContinuityThreadRecord, len(continuityState.Threads)),
	}
	for threadID, threadRecord := range continuityState.Threads {
		clonedState.Threads[threadID] = cloneContinuityThreadRecord(threadRecord)
	}
	return clonedState
}

func BuildContinuityProjection(ledgerPath string, continuityState ContinuityThreadsState) (ContinuityProjection, error) {
	projection := ContinuityProjection{}

	if strings.TrimSpace(continuityState.CurrentThreadID) != "" {
		currentSummary, _, err := SummarizeContinuityThread(ledgerPath, continuityState.CurrentThreadID)
		if err != nil {
			return ContinuityProjection{}, err
		}
		projection.Current = &currentSummary
	}
	if strings.TrimSpace(continuityState.NextThreadID) != "" {
		nextSummary, _, err := SummarizeContinuityThread(ledgerPath, continuityState.NextThreadID)
		if err != nil {
			return ContinuityProjection{}, err
		}
		projection.Next = &nextSummary
	}
	if strings.TrimSpace(continuityState.PreviousThreadID) != "" {
		previousSummary, _, err := SummarizeContinuityThread(ledgerPath, continuityState.PreviousThreadID)
		if err != nil {
			return ContinuityProjection{}, err
		}
		projection.Previous = &previousSummary
	}
	return projection, nil
}

func FormatContinuityProjection(projection ContinuityProjection) string {
	lines := []string{"continuity threads are append-only local stream projections. they are historical context, not authority."}
	if projection.Current != nil {
		lines = append(lines, formatContinuityThreadSummary("current", *projection.Current)...)
	}
	if projection.Previous != nil && projection.Previous.EventCount > 0 {
		lines = append(lines, formatContinuityThreadSummary("previous", *projection.Previous)...)
	}
	if projection.Next != nil {
		lines = append(lines, formatContinuityThreadSummary("next", *projection.Next)...)
	}

	trimmedLines := append([]string(nil), lines...)
	for approximateTokenCount(strings.Join(trimmedLines, "\n")) > continuityProjectionTokenBudget {
		if len(trimmedLines) <= 1 {
			return ""
		}
		trimmedLines = append([]string(nil), trimmedLines[:len(trimmedLines)-1]...)
	}
	if len(trimmedLines) <= 1 {
		return ""
	}
	return strings.Join(trimmedLines, "\n")
}

func SummarizeContinuityThread(ledgerPath string, threadID string) (ContinuityThreadSummary, []ContinuityThreadEvent, error) {
	validatedThreadID := strings.TrimSpace(threadID)
	if validatedThreadID == "" {
		return ContinuityThreadSummary{}, nil, fmt.Errorf("thread_id is required")
	}

	ledgerFileHandle, err := openVerifiedMemoryLedger(ledgerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ContinuityThreadSummary{ThreadID: validatedThreadID, Scope: MemoryScopeGlobal}, nil, nil
		}
		return ContinuityThreadSummary{}, nil, err
	}
	defer ledgerFileHandle.Close()

	ledgerScanner := bufio.NewScanner(ledgerFileHandle)
	ledgerScanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	threadSummary := ContinuityThreadSummary{
		ThreadID:        validatedThreadID,
		Scope:           MemoryScopeGlobal,
		ActiveGoals:     []string{},
		UnresolvedItems: []WakeStateOpenItem{},
		RecentFacts:     []WakeStateRecentFact{},
		SourceRefs:      []WakeStateSourceRef{},
	}
	threadEvents := make([]ContinuityThreadEvent, 0, 16)

	factsByName := map[string]WakeStateRecentFact{}
	recentFactNames := make([]string, 0, wakeStateSoftMaxRecentFacts)
	goalsByID := map[string]string{}
	goalOrder := make([]string, 0, wakeStateSoftMaxActiveGoals)
	unresolvedItemsByID := map[string]WakeStateOpenItem{}
	unresolvedItemOrder := make([]string, 0, wakeStateSoftMaxUnresolvedItems)
	sourceRefsSeen := map[string]WakeStateSourceRef{}
	sourceRefOrder := make([]string, 0, wakeStateSoftMaxSourceReferences)
	discoveredTags := map[string]struct{}{}

	for ledgerScanner.Scan() {
		var rawLedgerEvent map[string]interface{}
		if err := json.Unmarshal(ledgerScanner.Bytes(), &rawLedgerEvent); err != nil {
			return ContinuityThreadSummary{}, nil, fmt.Errorf("%w: malformed ledger line in continuity thread summary: %v", ErrLedgerIntegrity, err)
		}

		rawEventData, _ := rawLedgerEvent["data"].(map[string]interface{})
		continuityEvent, foundContinuityEvent := parseContinuityEvent(rawEventData)
		if !foundContinuityEvent {
			continue
		}
		if continuityEvent.ThreadID != validatedThreadID {
			continue
		}

		threadSummary.EventCount++
		threadSummary.ApproxPayloadBytes += len(ledgerScanner.Bytes())
		threadSummary.ApproxPromptTokens += approximateTokenCount(string(ledgerScanner.Bytes()))
		threadSummary.LastEventAtUTC = strings.TrimSpace(stringValue(rawLedgerEvent["ts"]))
		if continuityEvent.Scope != "" {
			threadSummary.Scope = continuityEvent.Scope
		}

		sourceRef := wakeStateSourceRefForLedgerEvent(rawLedgerEvent, rawEventData)
		recordWakeStateSourceRef(sourceRefsSeen, &sourceRefOrder, sourceRef)
		recordContinuityTags(discoveredTags, continuityEvent.Scope)

		switch continuityEvent.Type {
		case ContinuityEventTypeProviderFactObserved:
			candidateFacts, _ := continuityEvent.Payload["facts"].(map[string]interface{})
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
					EpistemicFlavor: continuityEvent.EpistemicFlavor,
				})
				recordContinuityTags(discoveredTags, factName)
				if stringValue, isString := candidateFacts[factName].(string); isString {
					recordContinuityTags(discoveredTags, stringValue)
				}
			}
		case ContinuityEventTypeGoalOpened:
			goalID := strings.TrimSpace(stringValue(continuityEvent.Payload["goal_id"]))
			goalText := strings.TrimSpace(stringValue(continuityEvent.Payload["text"]))
			if goalID != "" && goalText != "" {
				goalsByID[goalID] = goalText
				removeWakeStateOrderEntry(&goalOrder, goalID)
				goalOrder = append(goalOrder, goalID)
				recordContinuityTags(discoveredTags, goalID, goalText)
			}
		case ContinuityEventTypeGoalClosed:
			goalID := strings.TrimSpace(stringValue(continuityEvent.Payload["goal_id"]))
			if goalID != "" {
				delete(goalsByID, goalID)
				removeWakeStateOrderEntry(&goalOrder, goalID)
			}
		case ContinuityEventTypeUnresolvedItemOpened:
			itemID := strings.TrimSpace(stringValue(continuityEvent.Payload["item_id"]))
			itemText := strings.TrimSpace(stringValue(continuityEvent.Payload["text"]))
			if itemID != "" && itemText != "" {
				unresolvedItemsByID[itemID] = WakeStateOpenItem{ID: itemID, Text: itemText}
				removeWakeStateOrderEntry(&unresolvedItemOrder, itemID)
				unresolvedItemOrder = append(unresolvedItemOrder, itemID)
				recordContinuityTags(discoveredTags, itemID, itemText)
			}
		case ContinuityEventTypeUnresolvedItemClosed:
			itemID := strings.TrimSpace(stringValue(continuityEvent.Payload["item_id"]))
			if itemID != "" {
				delete(unresolvedItemsByID, itemID)
				removeWakeStateOrderEntry(&unresolvedItemOrder, itemID)
			}
		}

		threadEvents = append(threadEvents, ContinuityThreadEvent{
			TimestampUTC:    strings.TrimSpace(stringValue(rawLedgerEvent["ts"])),
			SessionID:       strings.TrimSpace(stringValue(rawLedgerEvent["session"])),
			Type:            continuityEvent.Type,
			Scope:           continuityEvent.Scope,
			ThreadID:        continuityEvent.ThreadID,
			EpistemicFlavor: continuityEvent.EpistemicFlavor,
			LedgerSequence:  int64Value(rawEventData["ledger_sequence"]),
			EventHash:       strings.TrimSpace(stringValue(rawEventData["event_hash"])),
			SourceRefs:      cloneContinuitySourceRefs(continuityEvent.SourceRefs),
			Payload:         cloneCandidatePayload(continuityEvent.Payload),
		})
	}
	if err := ledgerScanner.Err(); err != nil {
		return ContinuityThreadSummary{}, nil, err
	}

	trimWakeStateOrder(&goalOrder, continuityProjectionMaxGoals)
	trimWakeStateOrder(&unresolvedItemOrder, continuityProjectionMaxItems)
	trimWakeStateOrder(&recentFactNames, continuityProjectionMaxFacts)
	trimWakeStateOrder(&sourceRefOrder, wakeStateSoftMaxSourceReferences)

	activeGoals := make([]string, 0, len(goalOrder))
	for _, goalID := range goalOrder {
		goalText, found := goalsByID[goalID]
		if !found {
			continue
		}
		activeGoals = append(activeGoals, goalText)
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
	for _, itemID := range unresolvedItemOrder {
		if unresolvedItem, found := unresolvedItemsByID[itemID]; found {
			unresolvedItems = append(unresolvedItems, unresolvedItem)
		}
	}

	trimmedActiveGoals, trimmedUnresolvedItems, trimmedRecentFacts, _, _ := trimWakeStateToPromptBudget(
		activeGoals,
		unresolvedItems,
		recentFacts,
		nil,
		continuityProjectionTokenBudget/2,
	)

	threadSummary.ActiveGoals = trimmedActiveGoals
	threadSummary.UnresolvedItems = trimmedUnresolvedItems
	threadSummary.RecentFacts = trimmedRecentFacts
	threadSummary.SourceRefs = sourceRefs
	threadSummary.Tags = normalizedContinuityTagSet(discoveredTags)
	return threadSummary, threadEvents, nil
}

func RolloverContinuityThreads(currentState ContinuityThreadsState, ledgerPath string, now time.Time, reason string) (ContinuityThreadsState, *ContinuityInspectCandidate, error) {
	workingState := CloneContinuityThreadsState(currentState)

	currentSummary, threadEvents, err := SummarizeContinuityThread(ledgerPath, workingState.CurrentThreadID)
	if err != nil {
		return ContinuityThreadsState{}, nil, err
	}
	if currentSummary.EventCount == 0 {
		return workingState, nil, nil
	}

	nowUTC := now.UTC()
	sealedAtUTC := nowUTC.Format(time.RFC3339Nano)
	currentThreadRecord := workingState.Threads[workingState.CurrentThreadID]
	currentThreadRecord.State = ContinuityThreadStateSealed
	currentThreadRecord.SealedAtUTC = sealedAtUTC
	currentThreadRecord.LastContinuityEventAtUTC = currentSummary.LastEventAtUTC
	currentThreadRecord.SealReason = strings.TrimSpace(reason)
	currentThreadRecord.EventCount = currentSummary.EventCount
	currentThreadRecord.ApproxPayloadBytes = currentSummary.ApproxPayloadBytes
	currentThreadRecord.ApproxPromptTokens = currentSummary.ApproxPromptTokens
	workingState.Threads[currentThreadRecord.ThreadID] = currentThreadRecord

	workingState.PreviousThreadID = currentThreadRecord.ThreadID
	workingState.CurrentThreadID = workingState.NextThreadID

	newNextThread := newContinuityThreadRecord(nowUTC)
	workingState.NextThreadID = newNextThread.ThreadID
	workingState.Threads[newNextThread.ThreadID] = newNextThread

	var inspectCandidate *ContinuityInspectCandidate
	if continuityThresholdExceeded(currentSummary, workingState.Thresholds) {
		inspectCandidate = &ContinuityInspectCandidate{
			InspectionID:       continuityInspectionIDForThread(currentThreadRecord.ThreadID),
			ThreadID:           currentThreadRecord.ThreadID,
			Scope:              currentSummary.Scope,
			SealedAtUTC:        sealedAtUTC,
			EventCount:         currentSummary.EventCount,
			ApproxPayloadBytes: currentSummary.ApproxPayloadBytes,
			ApproxPromptTokens: currentSummary.ApproxPromptTokens,
			Tags:               append([]string(nil), currentSummary.Tags...),
			Events:             append([]ContinuityThreadEvent(nil), threadEvents...),
		}
	}

	return workingState, inspectCandidate, nil
}

func MarkContinuityThreadInspected(currentState ContinuityThreadsState, inspectCandidate ContinuityInspectCandidate, completedAt time.Time, derivedDistillateIDs []string, derivedResonateKeyIDs []string) (ContinuityThreadsState, error) {
	workingState := CloneContinuityThreadsState(currentState)

	threadRecord, found := workingState.Threads[inspectCandidate.ThreadID]
	if !found {
		return ContinuityThreadsState{}, fmt.Errorf("inspected thread %q not found", inspectCandidate.ThreadID)
	}
	if threadRecord.State == ContinuityThreadStateTombstone {
		return ContinuityThreadsState{}, fmt.Errorf("thread %q is tombstoned", inspectCandidate.ThreadID)
	}
	if threadRecord.State != ContinuityThreadStateSealed && threadRecord.State != ContinuityThreadStateInspected {
		return ContinuityThreadsState{}, fmt.Errorf("thread %q is not sealed", inspectCandidate.ThreadID)
	}
	threadRecord.State = ContinuityThreadStateInspected
	threadRecord.InspectionID = inspectCandidate.InspectionID
	threadRecord.InspectionCompletedAtUTC = completedAt.UTC().Format(time.RFC3339Nano)
	threadRecord.DerivedDistillateIDs = append([]string(nil), derivedDistillateIDs...)
	threadRecord.DerivedResonateKeyIDs = append([]string(nil), derivedResonateKeyIDs...)
	workingState.Threads[inspectCandidate.ThreadID] = threadRecord
	return workingState, nil
}

func continuityInspectionIDForThread(threadID string) string {
	return "inspect_" + strings.TrimSpace(threadID)
}

func continuityThresholdExceeded(threadSummary ContinuityThreadSummary, thresholds ContinuityInspectionThresholds) bool {
	if thresholds.SubmitPreviousMinEvents > 0 && threadSummary.EventCount >= thresholds.SubmitPreviousMinEvents {
		return true
	}
	if thresholds.SubmitPreviousMinPayloadBytes > 0 && threadSummary.ApproxPayloadBytes >= thresholds.SubmitPreviousMinPayloadBytes {
		return true
	}
	if thresholds.SubmitPreviousMinPromptTokens > 0 && threadSummary.ApproxPromptTokens >= thresholds.SubmitPreviousMinPromptTokens {
		return true
	}
	return false
}

func newContinuityThreadsState(now time.Time, thresholds ContinuityInspectionThresholds) ContinuityThreadsState {
	currentThread := newContinuityThreadRecord(now.UTC())
	nextThread := newContinuityThreadRecord(now.UTC())
	return ContinuityThreadsState{
		SchemaVersion:   continuityThreadsSchemaVersion,
		CurrentThreadID: currentThread.ThreadID,
		NextThreadID:    nextThread.ThreadID,
		Thresholds:      thresholds,
		Threads: map[string]ContinuityThreadRecord{
			currentThread.ThreadID: currentThread,
			nextThread.ThreadID:    nextThread,
		},
	}
}

func newContinuityThreadRecord(now time.Time) ContinuityThreadRecord {
	return ContinuityThreadRecord{
		ThreadID:     newContinuityThreadID(now),
		Scope:        MemoryScopeGlobal,
		State:        ContinuityThreadStateOpen,
		CreatedAtUTC: now.UTC().Format(time.RFC3339Nano),
	}
}

func newContinuityThreadID(now time.Time) string {
	randomBytes := make([]byte, 4)
	_, _ = rand.Read(randomBytes)
	return fmt.Sprintf("thread_%s_%s", now.UTC().Format("20060102T150405"), hex.EncodeToString(randomBytes))
}

func validateContinuityThreadRecord(threadRecord ContinuityThreadRecord) error {
	if err := identifiers.ValidateSafeIdentifier("thread_id", threadRecord.ThreadID); err != nil {
		return err
	}
	threadRecord.Scope = defaultMemoryScope(threadRecord.Scope)
	if threadRecord.Scope != MemoryScopeGlobal {
		return fmt.Errorf("unsupported continuity scope %q", threadRecord.Scope)
	}
	switch threadRecord.State {
	case ContinuityThreadStateOpen, ContinuityThreadStateSealed, ContinuityThreadStateInspected, ContinuityThreadStateTombstone:
	default:
		return fmt.Errorf("invalid continuity thread state %q", threadRecord.State)
	}
	if strings.TrimSpace(threadRecord.CreatedAtUTC) == "" {
		return fmt.Errorf("created_at_utc is required")
	}
	if _, err := time.Parse(time.RFC3339Nano, threadRecord.CreatedAtUTC); err != nil {
		return fmt.Errorf("created_at_utc is invalid: %w", err)
	}
	if strings.TrimSpace(threadRecord.SealedAtUTC) != "" {
		if _, err := time.Parse(time.RFC3339Nano, threadRecord.SealedAtUTC); err != nil {
			return fmt.Errorf("sealed_at_utc is invalid: %w", err)
		}
	}
	if strings.TrimSpace(threadRecord.LastContinuityEventAtUTC) != "" {
		if _, err := time.Parse(time.RFC3339Nano, threadRecord.LastContinuityEventAtUTC); err != nil {
			return fmt.Errorf("last_continuity_event_at_utc is invalid: %w", err)
		}
	}
	if strings.TrimSpace(threadRecord.InspectionCompletedAtUTC) != "" {
		if _, err := time.Parse(time.RFC3339Nano, threadRecord.InspectionCompletedAtUTC); err != nil {
			return fmt.Errorf("inspection_completed_at_utc is invalid: %w", err)
		}
	}
	if strings.TrimSpace(threadRecord.TombstonedAtUTC) != "" {
		if _, err := time.Parse(time.RFC3339Nano, threadRecord.TombstonedAtUTC); err != nil {
			return fmt.Errorf("tombstoned_at_utc is invalid: %w", err)
		}
	}
	if threadRecord.EventCount < 0 {
		return fmt.Errorf("event_count must be non-negative")
	}
	if threadRecord.ApproxPayloadBytes < 0 {
		return fmt.Errorf("approx_payload_bytes must be non-negative")
	}
	if threadRecord.ApproxPromptTokens < 0 {
		return fmt.Errorf("approx_prompt_tokens must be non-negative")
	}
	if threadRecord.State == ContinuityThreadStateOpen {
		if threadRecord.EventCount != 0 && strings.TrimSpace(threadRecord.SealedAtUTC) != "" {
			return fmt.Errorf("open thread must not set sealed_at_utc")
		}
	}
	for _, derivedDistillateID := range threadRecord.DerivedDistillateIDs {
		if err := identifiers.ValidateSafeIdentifier("derived_distillate_id", derivedDistillateID); err != nil {
			return err
		}
	}
	for _, derivedResonateKeyID := range threadRecord.DerivedResonateKeyIDs {
		if err := identifiers.ValidateSafeIdentifier("derived_resonate_key_id", derivedResonateKeyID); err != nil {
			return err
		}
	}
	if strings.TrimSpace(threadRecord.InspectionID) != "" {
		if err := identifiers.ValidateSafeIdentifier("inspection_id", threadRecord.InspectionID); err != nil {
			return err
		}
	}
	return nil
}

func cloneContinuityThreadRecord(threadRecord ContinuityThreadRecord) ContinuityThreadRecord {
	threadRecord.DerivedDistillateIDs = append([]string(nil), threadRecord.DerivedDistillateIDs...)
	threadRecord.DerivedResonateKeyIDs = append([]string(nil), threadRecord.DerivedResonateKeyIDs...)
	return threadRecord
}

func cloneContinuitySourceRefs(sourceRefs []ContinuitySourceRef) []ContinuitySourceRef {
	clonedSourceRefs := make([]ContinuitySourceRef, 0, len(sourceRefs))
	clonedSourceRefs = append(clonedSourceRefs, sourceRefs...)
	return clonedSourceRefs
}

func normalizedContinuityTagSet(rawTagSet map[string]struct{}) []string {
	normalizedTags := make([]string, 0, len(rawTagSet))
	for rawTag := range rawTagSet {
		normalizedTags = append(normalizedTags, rawTag)
	}
	sort.Strings(normalizedTags)
	return normalizedTags
}

func formatContinuityThreadSummary(role string, threadSummary ContinuityThreadSummary) []string {
	summaryLines := []string{
		fmt.Sprintf("%s_thread_id: %s", role, threadSummary.ThreadID),
		fmt.Sprintf("%s_scope: %s", role, defaultMemoryScope(threadSummary.Scope)),
		fmt.Sprintf("%s_event_count: %d", role, threadSummary.EventCount),
	}
	if threadSummary.EventCount == 0 {
		summaryLines = append(summaryLines, fmt.Sprintf("%s_status: empty", role))
		return summaryLines
	}
	if strings.TrimSpace(threadSummary.LastEventAtUTC) != "" {
		summaryLines = append(summaryLines, fmt.Sprintf("%s_last_event_at_utc: %s", role, threadSummary.LastEventAtUTC))
	}
	for _, goalText := range threadSummary.ActiveGoals {
		summaryLines = append(summaryLines, fmt.Sprintf("%s_active_goal: %s", role, goalText))
	}
	for _, unresolvedItem := range threadSummary.UnresolvedItems {
		summaryLines = append(summaryLines, fmt.Sprintf("%s_unresolved_item: %s %s", role, unresolvedItem.ID, unresolvedItem.Text))
	}
	for _, recentFact := range threadSummary.RecentFacts {
		summaryLines = append(summaryLines, fmt.Sprintf("%s_recent_fact: %s=%v (%s)", role, recentFact.Name, recentFact.Value, recentFact.EpistemicFlavor))
	}
	if len(threadSummary.Tags) > 0 {
		summaryLines = append(summaryLines, fmt.Sprintf("%s_tags: %s", role, strings.Join(threadSummary.Tags, ", ")))
	}
	return summaryLines
}

func int64Value(rawValue interface{}) int64 {
	switch typedValue := rawValue.(type) {
	case float64:
		return int64(typedValue)
	case int64:
		return typedValue
	case int:
		return int64(typedValue)
	default:
		return 0
	}
}
