package loopgate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"morph/internal/threadstore"
)

var _ io.Closer = (*benchmarkProductionParityControlPlaneBackend)(nil)

func OpenContinuityTCLProductionParityControlPlaneDiscoverBackend(repoRoot string, rememberedFactSeeds []BenchmarkRememberedFactSeed, observedThreadSeeds []BenchmarkObservedThreadSeed, todoSeeds []BenchmarkTodoSeed) (ProjectedNodeDiscoverBackend, error) {
	if len(rememberedFactSeeds) == 0 && len(observedThreadSeeds) == 0 && len(todoSeeds) == 0 {
		return nil, fmt.Errorf("at least one production-parity control-plane seed is required")
	}

	rememberedFactSeedsByScope := make(map[string][]BenchmarkRememberedFactSeed)
	for _, rememberedFactSeed := range rememberedFactSeeds {
		benchmarkScope := strings.TrimSpace(rememberedFactSeed.Scope)
		if benchmarkScope == "" {
			return nil, fmt.Errorf("benchmark remembered fact seed %q requires a non-empty scope", strings.TrimSpace(rememberedFactSeed.FactKey))
		}
		rememberedFactSeedsByScope[benchmarkScope] = append(rememberedFactSeedsByScope[benchmarkScope], rememberedFactSeed)
	}

	observedThreadSeedsByScope := make(map[string][]BenchmarkObservedThreadSeed)
	for _, observedThreadSeed := range observedThreadSeeds {
		benchmarkScope := strings.TrimSpace(observedThreadSeed.Scope)
		if benchmarkScope == "" {
			return nil, fmt.Errorf("benchmark observed thread seed requires a non-empty scope")
		}
		observedThreadSeedsByScope[benchmarkScope] = append(observedThreadSeedsByScope[benchmarkScope], observedThreadSeed)
	}

	todoSeedsByScope := make(map[string][]BenchmarkTodoSeed)
	for _, todoSeed := range todoSeeds {
		benchmarkScope := strings.TrimSpace(todoSeed.Scope)
		if benchmarkScope == "" {
			return nil, fmt.Errorf("benchmark todo seed requires a non-empty scope")
		}
		todoSeedsByScope[benchmarkScope] = append(todoSeedsByScope[benchmarkScope], todoSeed)
	}

	knownScopes := make(map[string]struct{}, len(rememberedFactSeedsByScope)+len(observedThreadSeedsByScope)+len(todoSeedsByScope))
	for benchmarkScope := range rememberedFactSeedsByScope {
		knownScopes[benchmarkScope] = struct{}{}
	}
	for benchmarkScope := range observedThreadSeedsByScope {
		knownScopes[benchmarkScope] = struct{}{}
	}
	for benchmarkScope := range todoSeedsByScope {
		knownScopes[benchmarkScope] = struct{}{}
	}
	if len(knownScopes) == 0 {
		return nil, fmt.Errorf("production-parity control-plane seeding produced no scenario scopes")
	}

	orderedScopes := make([]string, 0, len(knownScopes))
	for benchmarkScope := range knownScopes {
		orderedScopes = append(orderedScopes, benchmarkScope)
	}
	sort.Strings(orderedScopes)

	controlPlaneBackend := &benchmarkProductionParityControlPlaneBackend{
		scenarioStates: make(map[string]benchmarkControlPlaneScenarioState, len(orderedScopes)),
	}
	for _, benchmarkScope := range orderedScopes {
		scenarioState, err := seedBenchmarkScenarioOverControlPlane(repoRoot, benchmarkScope, rememberedFactSeedsByScope[benchmarkScope], observedThreadSeedsByScope[benchmarkScope], todoSeedsByScope[benchmarkScope])
		if err != nil {
			_ = controlPlaneBackend.Close()
			return nil, err
		}
		controlPlaneBackend.scenarioStates[benchmarkScope] = scenarioState
	}
	return controlPlaneBackend, nil
}

func (backend *benchmarkProductionParityControlPlaneBackend) DiscoverProjectedNodes(ctx context.Context, rawRequest ProjectedNodeDiscoverRequest) ([]ProjectedNodeDiscoverItem, error) {
	benchmarkScope := strings.TrimSpace(rawRequest.Scope)
	scenarioState, found := backend.scenarioStates[benchmarkScope]
	if !found {
		return nil, fmt.Errorf("production-parity control-plane scope %q is not seeded", benchmarkScope)
	}
	maxItems := rawRequest.MaxItems
	if maxItems <= 0 {
		maxItems = 5
	}

	// Each benchmark scenario gets its own isolated Loopgate runtime. That keeps the
	// benchmark on the product-valid global memory scope while still preserving the
	// benchmark's per-scenario isolation model without synthetic scope rewriting.
	discoverResponse, err := scenarioState.client.DiscoverMemory(ctx, MemoryDiscoverRequest{
		Scope:    memoryScopeGlobal,
		Query:    strings.TrimSpace(rawRequest.Query),
		MaxItems: maxItems,
	})
	if err != nil {
		return nil, fmt.Errorf("discover benchmark scenario scope %q through control plane: %w", benchmarkScope, err)
	}
	if len(discoverResponse.Items) == 0 {
		return []ProjectedNodeDiscoverItem{}, nil
	}

	requestedKeyIDs := make([]string, 0, len(discoverResponse.Items))
	for _, discoveredItem := range discoverResponse.Items {
		requestedKeyIDs = append(requestedKeyIDs, discoveredItem.KeyID)
	}
	recallResponse, err := scenarioState.client.RecallMemory(ctx, MemoryRecallRequest{
		Scope:         memoryScopeGlobal,
		RequestedKeys: requestedKeyIDs,
		MaxItems:      len(requestedKeyIDs),
		MaxTokens:     2000,
	})
	if err != nil {
		return nil, fmt.Errorf("recall benchmark scenario scope %q through control plane: %w", benchmarkScope, err)
	}

	recalledItemsByKeyID := make(map[string]MemoryRecallItem, len(recallResponse.Items))
	for _, recalledItem := range recallResponse.Items {
		recalledItemsByKeyID[strings.TrimSpace(recalledItem.KeyID)] = recalledItem
	}

	projectedItems := make([]ProjectedNodeDiscoverItem, 0, len(discoverResponse.Items))
	for _, discoveredItem := range discoverResponse.Items {
		recalledItem, found := recalledItemsByKeyID[strings.TrimSpace(discoveredItem.KeyID)]
		if !found {
			return nil, fmt.Errorf("discover/recall mismatch for benchmark key %q in scope %q", discoveredItem.KeyID, benchmarkScope)
		}
		projectedItems = append(projectedItems, projectedNodeDiscoverItemFromControlPlane(discoveredItem, recalledItem, benchmarkScope))
	}
	return projectedItems, nil
}

func (backend *benchmarkProductionParityControlPlaneBackend) Close() error {
	for benchmarkScope, scenarioState := range backend.scenarioStates {
		if scenarioState.cleanup != nil {
			scenarioState.cleanup()
		}
		delete(backend.scenarioStates, benchmarkScope)
	}
	return nil
}

type benchmarkControlPlaneScenarioState struct {
	scope            string
	isolatedRepoRoot string
	controlSessionID string
	server           *Server
	client           *Client
	cleanup          func()
}

type benchmarkProductionParityControlPlaneBackend struct {
	scenarioStates map[string]benchmarkControlPlaneScenarioState
}

func seedBenchmarkScenarioOverControlPlane(repoRoot string, benchmarkScope string, rememberedFactSeeds []BenchmarkRememberedFactSeed, observedThreadSeeds []BenchmarkObservedThreadSeed, todoSeeds []BenchmarkTodoSeed) (benchmarkControlPlaneScenarioState, error) {
	if len(rememberedFactSeeds) == 0 && len(observedThreadSeeds) == 0 && len(todoSeeds) == 0 {
		return benchmarkControlPlaneScenarioState{}, fmt.Errorf("scenario scope %q produced no control-plane seeds", benchmarkScope)
	}

	isolatedBenchmarkRepoRoot, err := prepareIsolatedBenchmarkServerRepoRoot(repoRoot)
	if err != nil {
		return benchmarkControlPlaneScenarioState{}, fmt.Errorf("prepare isolated benchmark repo root: %w", err)
	}

	benchmarkServer, benchmarkClient, stopBenchmarkServer, err := startBenchmarkControlPlaneServerWithSession(
		isolatedBenchmarkRepoRoot,
		"haven",
		[]string{controlCapabilityUIWrite, controlCapabilityMemoryRead, controlCapabilityMemoryWrite, "todo.add", "todo.complete"},
	)
	if err != nil {
		_ = os.RemoveAll(isolatedBenchmarkRepoRoot)
		return benchmarkControlPlaneScenarioState{}, fmt.Errorf("open loopgate server for benchmark scenario scope %q: %w", benchmarkScope, err)
	}
	cleanup := func() {
		benchmarkClient.CloseIdleConnections()
		stopBenchmarkServer()
		_ = os.RemoveAll(isolatedBenchmarkRepoRoot)
	}

	baseSeedTimeUTC := time.Now().UTC().Truncate(time.Second)
	for rememberedFactSeedIndex, rememberedFactSeed := range rememberedFactSeeds {
		if strings.TrimSpace(rememberedFactSeed.Scope) != benchmarkScope {
			cleanup()
			return benchmarkControlPlaneScenarioState{}, fmt.Errorf("benchmark remembered fact scope %q does not match scenario scope %q", rememberedFactSeed.Scope, benchmarkScope)
		}
		rememberedSeedTimeUTC := baseSeedTimeUTC.Add(time.Duration(rememberedFactSeedIndex) * time.Second)
		benchmarkServer.SetNowForTest(func() time.Time {
			return rememberedSeedTimeUTC
		})
		if _, err := benchmarkClient.RememberMemoryFact(context.Background(), MemoryRememberRequest{
			Scope:           memoryScopeGlobal,
			FactKey:         strings.TrimSpace(rememberedFactSeed.FactKey),
			FactValue:       strings.TrimSpace(rememberedFactSeed.FactValue),
			SourceText:      strings.TrimSpace(rememberedFactSeed.SourceText),
			CandidateSource: memoryCandidateSourceExplicitFact,
			SourceChannel:   nonEmptyBenchmarkSourceChannel(rememberedFactSeed.SourceChannel),
		}); err != nil {
			cleanup()
			return benchmarkControlPlaneScenarioState{}, fmt.Errorf("seed remembered fact %q through control plane: %w", rememberedFactSeed.FactKey, err)
		}
	}

	threadStoreRoot, workspaceID, err := benchmarkThreadStoreRoot(benchmarkServer, isolatedBenchmarkRepoRoot)
	if err != nil {
		cleanup()
		return benchmarkControlPlaneScenarioState{}, err
	}
	threadStore, err := threadstore.NewStore(threadStoreRoot, workspaceID)
	if err != nil {
		cleanup()
		return benchmarkControlPlaneScenarioState{}, fmt.Errorf("open benchmark thread store: %w", err)
	}
	for observedThreadSeedIndex, observedThreadSeed := range observedThreadSeeds {
		if strings.TrimSpace(observedThreadSeed.Scope) != benchmarkScope {
			cleanup()
			return benchmarkControlPlaneScenarioState{}, fmt.Errorf("benchmark observed thread scope %q does not match scenario scope %q", observedThreadSeed.Scope, benchmarkScope)
		}
		inspectionSeedTimeUTC := baseSeedTimeUTC.Add(time.Duration(len(rememberedFactSeeds)+observedThreadSeedIndex) * time.Second)
		threadSummary, err := threadStore.NewThread()
		if err != nil {
			cleanup()
			return benchmarkControlPlaneScenarioState{}, fmt.Errorf("create benchmark thread for scope %q: %w", benchmarkScope, err)
		}
		threadID := threadSummary.ThreadID
		if strings.TrimSpace(observedThreadSeed.ThreadID) != "" {
			threadID = strings.TrimSpace(observedThreadSeed.ThreadID)
		}
		for eventIndex, eventSeed := range observedThreadSeed.Events {
			eventTimestampUTC := strings.TrimSpace(eventSeed.TimestampUTC)
			if eventTimestampUTC == "" {
				eventTimestampUTC = inspectionSeedTimeUTC.Add(time.Duration(eventIndex) * time.Millisecond).Format(time.RFC3339Nano)
			}
			if err := threadStore.AppendEvent(threadID, benchmarkThreadstoreEventFromSeed(eventSeed, eventTimestampUTC, observedThreadSeedIndex, eventIndex)); err != nil {
				cleanup()
				return benchmarkControlPlaneScenarioState{}, fmt.Errorf("append benchmark thread event for scope %q: %w", benchmarkScope, err)
			}
		}
		benchmarkServer.SetNowForTest(func() time.Time {
			return inspectionSeedTimeUTC
		})
		if _, err := benchmarkClient.SubmitHavenContinuityInspectionForThread(context.Background(), threadID); err != nil {
			cleanup()
			return benchmarkControlPlaneScenarioState{}, fmt.Errorf("inspect benchmark thread %q for scope %q: %w", threadID, benchmarkScope, err)
		}
	}

	for todoSeedIndex, todoSeed := range todoSeeds {
		if strings.TrimSpace(todoSeed.Scope) != benchmarkScope {
			cleanup()
			return benchmarkControlPlaneScenarioState{}, fmt.Errorf("benchmark todo seed scope %q does not match scenario scope %q", todoSeed.Scope, benchmarkScope)
		}
		todoSeedTimeUTC := baseSeedTimeUTC.Add(time.Duration(len(rememberedFactSeeds)+len(observedThreadSeeds)+todoSeedIndex) * time.Second)
		benchmarkServer.SetNowForTest(func() time.Time {
			return todoSeedTimeUTC
		})
		addResponse, err := benchmarkClient.ExecuteCapability(context.Background(), CapabilityRequest{
			RequestID:  benchmarkTodoCapabilityRequestID(benchmarkScope, todoSeedIndex, "add"),
			Capability: "todo.add",
			Arguments: map[string]string{
				"text":        strings.TrimSpace(todoSeed.Text),
				"task_kind":   nonEmptyBenchmarkTodoTaskKind(todoSeed.TaskKind),
				"source_kind": nonEmptyBenchmarkTodoSourceKind(todoSeed.SourceKind),
				"next_step":   strings.TrimSpace(todoSeed.NextStep),
			},
		})
		if err != nil {
			cleanup()
			return benchmarkControlPlaneScenarioState{}, fmt.Errorf("seed todo item %d for scope %q through control plane: %w", todoSeedIndex, benchmarkScope, err)
		}
		if addResponse.Status != ResponseStatusSuccess {
			cleanup()
			return benchmarkControlPlaneScenarioState{}, fmt.Errorf("seed todo item %d for scope %q returned status=%q denial_code=%q reason=%q", todoSeedIndex, benchmarkScope, addResponse.Status, addResponse.DenialCode, addResponse.DenialReason)
		}
		itemID, _ := addResponse.StructuredResult["item_id"].(string)
		if strings.TrimSpace(itemID) == "" {
			cleanup()
			return benchmarkControlPlaneScenarioState{}, fmt.Errorf("seed todo item %d for scope %q returned no item_id in structured result %#v", todoSeedIndex, benchmarkScope, addResponse.StructuredResult)
		}
		switch strings.TrimSpace(todoSeed.FinalStatus) {
		case "", explicitTodoWorkflowStatusTodo:
			continue
		case explicitTodoWorkflowStatusDone:
			benchmarkServer.SetNowForTest(func() time.Time {
				return todoSeedTimeUTC.Add(500 * time.Millisecond)
			})
			if _, err := benchmarkClient.ExecuteCapability(context.Background(), CapabilityRequest{
				RequestID:  benchmarkTodoCapabilityRequestID(benchmarkScope, todoSeedIndex, "complete"),
				Capability: "todo.complete",
				Arguments: map[string]string{
					"item_id": itemID,
				},
			}); err != nil {
				cleanup()
				return benchmarkControlPlaneScenarioState{}, fmt.Errorf("complete seeded todo item %q for scope %q: %w", itemID, benchmarkScope, err)
			}
		default:
			cleanup()
			return benchmarkControlPlaneScenarioState{}, fmt.Errorf("unsupported benchmark todo final status %q for scope %q", todoSeed.FinalStatus, benchmarkScope)
		}
	}

	return benchmarkControlPlaneScenarioState{
		scope:            benchmarkScope,
		isolatedRepoRoot: isolatedBenchmarkRepoRoot,
		controlSessionID: benchmarkClient.controlSessionID,
		server:           benchmarkServer,
		client:           benchmarkClient,
		cleanup:          cleanup,
	}, nil
}

func benchmarkThreadStoreRoot(benchmarkServer *Server, isolatedBenchmarkRepoRoot string) (string, string, error) {
	homeDir, err := benchmarkServer.resolveUserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("resolve benchmark home directory: %w", err)
	}
	threadStoreRoot := filepath.Join(homeDir, ".haven", "threads")
	workspaceID := strings.TrimSpace(benchmarkServer.deriveWorkspaceIDFromRepoRoot())
	if workspaceID == "" {
		workspaceID = strings.TrimSpace(filepath.Base(isolatedBenchmarkRepoRoot))
	}
	return threadStoreRoot, workspaceID, nil
}

func benchmarkThreadstoreEventFromSeed(eventSeed BenchmarkObservedThreadEventSeed, eventTimestampUTC string, threadSeedIndex int, eventIndex int) threadstore.ConversationEvent {
	eventData := make(map[string]interface{})
	if trimmedText := strings.TrimSpace(eventSeed.Text); trimmedText != "" {
		eventData["text"] = trimmedText
	}
	if trimmedOutput := strings.TrimSpace(eventSeed.Output); trimmedOutput != "" {
		eventData["output"] = trimmedOutput
	}
	if trimmedCapability := strings.TrimSpace(eventSeed.Capability); trimmedCapability != "" {
		eventData["capability"] = trimmedCapability
	}
	if trimmedStatus := strings.TrimSpace(eventSeed.Status); trimmedStatus != "" {
		eventData["status"] = trimmedStatus
	}
	if trimmedReason := strings.TrimSpace(eventSeed.Reason); trimmedReason != "" {
		eventData["reason"] = trimmedReason
	}
	if trimmedDenialCode := strings.TrimSpace(eventSeed.DenialCode); trimmedDenialCode != "" {
		eventData["denial_code"] = trimmedDenialCode
	}
	callID := strings.TrimSpace(eventSeed.CallID)
	if callID == "" {
		callID = fmt.Sprintf("benchmark-call-%02d-%02d", threadSeedIndex, eventIndex)
	}
	eventData["call_id"] = callID
	if len(eventSeed.Facts) > 0 {
		factKeys := make([]string, 0, len(eventSeed.Facts))
		for factKey := range eventSeed.Facts {
			factKeys = append(factKeys, factKey)
		}
		sort.Strings(factKeys)
		factValues := make(map[string]interface{}, len(factKeys))
		for _, factKey := range factKeys {
			factValues[factKey] = strings.TrimSpace(eventSeed.Facts[factKey])
		}
		eventData["facts"] = factValues
	}
	return threadstore.ConversationEvent{
		TS:   strings.TrimSpace(eventTimestampUTC),
		Type: benchmarkThreadstoreEventType(eventSeed.EventType),
		Data: eventData,
	}
}

func benchmarkThreadstoreEventType(rawEventType string) string {
	switch strings.TrimSpace(rawEventType) {
	case "", threadstore.EventOrchToolResult:
		return threadstore.EventOrchToolResult
	case threadstore.EventUserMessage:
		return threadstore.EventUserMessage
	case threadstore.EventAssistantMessage:
		return threadstore.EventAssistantMessage
	default:
		return strings.TrimSpace(rawEventType)
	}
}

func projectedNodeDiscoverItemFromControlPlane(discoveredItem MemoryDiscoverItem, recalledItem MemoryRecallItem, benchmarkScope string) ProjectedNodeDiscoverItem {
	nodeKind := "memory_recall_item"
	sourceKind := "control_plane_memory_recall"
	canonicalKey := ""
	provenanceEvent := strings.TrimSpace(recalledItem.DistillateID)
	factHintValues := make([]string, 0, len(recalledItem.Facts))
	hasWorkflowItems := len(recalledItem.UnresolvedItems) > 0 || len(recalledItem.ActiveGoals) > 0
	if hasWorkflowItems {
		sourceKind = "todo_workflow_control_plane"
		nodeKind = sqliteNodeKindWorkflowTransition
	}
	for _, recalledFact := range recalledItem.Facts {
		if canonicalKey == "" && strings.TrimSpace(recalledFact.Name) != "" {
			canonicalKey = strings.TrimSpace(recalledFact.Name)
		}
		if provenanceEvent == "" && strings.TrimSpace(recalledFact.SourceRef) != "" {
			provenanceEvent = strings.TrimSpace(recalledFact.SourceRef)
		}
		switch strings.TrimSpace(recalledFact.StateClass) {
		case memoryFactStateClassAuthoritative:
			nodeKind = sqliteNodeKindExplicitRememberedFact
			sourceKind = explicitProfileFactSourceKind
		case memoryFactStateClassDerived:
			if sourceKind == "control_plane_memory_recall" {
				sourceKind = "observed_thread_continuity"
			}
		}
		switch typedValue := recalledFact.Value.(type) {
		case string:
			trimmedValue := strings.TrimSpace(typedValue)
			if trimmedValue != "" {
				factHintValues = append(factHintValues, trimmedValue)
			}
		default:
			stringValue := strings.TrimSpace(fmt.Sprint(typedValue))
			if stringValue != "" {
				factHintValues = append(factHintValues, stringValue)
			}
		}
	}
	hintParts := make([]string, 0, len(factHintValues)+len(recalledItem.ActiveGoals)+len(recalledItem.UnresolvedItems)*2)
	for _, factHintValue := range factHintValues {
		hintParts = appendUniqueBenchmarkHintPart(hintParts, factHintValue)
	}
	for _, activeGoal := range recalledItem.ActiveGoals {
		hintParts = appendUniqueBenchmarkHintPart(hintParts, activeGoal)
	}
	for _, unresolvedItem := range recalledItem.UnresolvedItems {
		// Task-resumption benchmarks need the open item text plus next-step metadata
		// because the real operator-facing resume surface depends on both. Preserve
		// that shape here so the control-plane benchmark does not silently collapse
		// workflow state into a weaker text-only retrieval test.
		hintParts = appendUniqueBenchmarkHintPart(hintParts, unresolvedItem.Text)
		hintParts = appendUniqueBenchmarkHintPart(hintParts, unresolvedItem.NextStep)
	}
	return ProjectedNodeDiscoverItem{
		NodeID:          strings.TrimSpace(discoveredItem.KeyID),
		NodeKind:        nodeKind,
		SourceKind:      sourceKind,
		CanonicalKey:    canonicalKey,
		AnchorTupleKey:  "",
		Scope:           benchmarkScope,
		CreatedAtUTC:    strings.TrimSpace(discoveredItem.CreatedAtUTC),
		State:           "active",
		HintText:        strings.Join(hintParts, "\n"),
		ExactSignature:  "",
		FamilySignature: "",
		ProvenanceEvent: provenanceEvent,
		MatchCount:      discoveredItem.MatchCount,
	}
}

func appendUniqueBenchmarkHintPart(hintParts []string, rawHintPart string) []string {
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

func benchmarkTodoCapabilityRequestID(benchmarkScope string, todoSeedIndex int, action string) string {
	scopeFingerprint := sha256.Sum256([]byte(strings.TrimSpace(benchmarkScope)))
	return fmt.Sprintf("mb_todo_%s_%02d_%s", action, todoSeedIndex, hex.EncodeToString(scopeFingerprint[:])[:10])
}

func nonEmptyBenchmarkTodoTaskKind(rawTaskKind string) string {
	trimmedTaskKind := strings.TrimSpace(rawTaskKind)
	if trimmedTaskKind == "" {
		return taskKindCarryOver
	}
	return trimmedTaskKind
}

func nonEmptyBenchmarkTodoSourceKind(rawSourceKind string) string {
	trimmedSourceKind := strings.TrimSpace(rawSourceKind)
	if trimmedSourceKind == "" {
		return "benchmark_task_resumption"
	}
	return trimmedSourceKind
}
