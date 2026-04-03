package loopgate

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"morph/internal/config"
)

type continuityTCLMemoryBackend struct {
	server                          *Server
	partition                       *memoryPartition
	store                           *continuitySQLiteStore
	productionParityMaterialization []productionParityMaterializedFactDebugRecord
}

const continuityTCLStoreFilename = "continuity_tcl.sqlite"

func newMemoryBackendForPartition(server *Server, partition *memoryPartition) (MemoryBackend, error) {
	selectedBackendName := strings.TrimSpace(server.runtimeConfig.Memory.Backend)
	if selectedBackendName == "" {
		selectedBackendName = config.DefaultMemoryBackend
	}
	switch selectedBackendName {
	case memoryBackendContinuityTCL:
		storePath := filepath.Join(partition.rootPath, continuityTCLStoreFilename)
		sqliteStore, err := openContinuitySQLiteStore(storePath)
		if err != nil {
			return nil, fmt.Errorf("open continuity sqlite store: %w", err)
		}
		return &continuityTCLMemoryBackend{
			server:    server,
			partition: partition,
			store:     sqliteStore,
		}, nil
	case memoryBackendRAGBaseline, memoryBackendHybrid:
		return nil, fmt.Errorf("memory backend %q is configured but not implemented yet", selectedBackendName)
	default:
		return nil, fmt.Errorf("unknown memory backend %q", selectedBackendName)
	}
}

func (backend *continuityTCLMemoryBackend) Name() string {
	return memoryBackendContinuityTCL
}

func (backend *continuityTCLMemoryBackend) SyncAuthoritativeState(ctx context.Context, authoritativeState continuityMemoryState) error {
	return backend.store.replaceProjectedNodes(authoritativeState)
}

func (backend *continuityTCLMemoryBackend) StoreInspection(ctx context.Context, inspectionRecord continuityInspectionRecord) error {
	return fmt.Errorf("memory backend %q store inspection is not wired yet", backend.Name())
}

func (backend *continuityTCLMemoryBackend) StoreDistillate(ctx context.Context, distillateRecord continuityDistillateRecord) error {
	return fmt.Errorf("memory backend %q store distillate is not wired yet", backend.Name())
}

func (backend *continuityTCLMemoryBackend) StoreExplicitRememberedFact(ctx context.Context, distillateRecord continuityDistillateRecord) error {
	return fmt.Errorf("memory backend %q store explicit remembered fact is not wired yet", backend.Name())
}

func (backend *continuityTCLMemoryBackend) BuildWakeState(ctx context.Context, request MemoryWakeStateRequest) (MemoryWakeStateResponse, error) {
	if backend.partition == nil {
		return MemoryWakeStateResponse{}, fmt.Errorf("memory backend partition is not bound")
	}
	backend.server.memoryMu.Lock()
	defer backend.server.memoryMu.Unlock()
	return cloneMemoryWakeStateResponse(backend.partition.state.WakeState), nil
}

func (backend *continuityTCLMemoryBackend) Discover(ctx context.Context, request MemoryDiscoverRequest) (MemoryDiscoverResponse, error) {
	if backend.partition == nil {
		return MemoryDiscoverResponse{}, fmt.Errorf("memory backend partition is not bound")
	}
	return backend.server.discoverMemoryFromPartitionState(backend.partition, request)
}

func (backend *continuityTCLMemoryBackend) Recall(ctx context.Context, request MemoryRecallRequest) (MemoryRecallResponse, error) {
	if backend.partition == nil {
		return MemoryRecallResponse{}, fmt.Errorf("memory backend partition is not bound")
	}
	return backend.server.recallMemoryFromPartitionState(backend.partition, request)
}

func (backend *continuityTCLMemoryBackend) DiscoverProjectedNodes(ctx context.Context, rawRequest ProjectedNodeDiscoverRequest) ([]ProjectedNodeDiscoverItem, error) {
	validatedRequest := rawRequest
	if strings.TrimSpace(validatedRequest.Scope) == "" {
		validatedRequest.Scope = memoryScopeGlobal
	}
	if validatedRequest.MaxItems <= 0 {
		validatedRequest.MaxItems = 5
	}

	projectedNodes, err := backend.store.searchProjectedNodes(validatedRequest.Scope, validatedRequest.Query, validatedRequest.MaxItems)
	if err != nil {
		return nil, err
	}
	discoveredItems := make([]ProjectedNodeDiscoverItem, 0, len(projectedNodes))
	for _, projectedNode := range projectedNodes {
		discoveredItems = append(discoveredItems, ProjectedNodeDiscoverItem{
			NodeID:          projectedNode.NodeID,
			NodeKind:        projectedNode.NodeKind,
			SourceKind:      projectedNodeSearchMetadata(projectedNode).SourceKind,
			CanonicalKey:    projectedNodeSearchMetadata(projectedNode).FactKey,
			AnchorTupleKey:  anchorTupleKey(projectedNode.AnchorVersion, projectedNode.AnchorKey),
			Scope:           projectedNode.Scope,
			CreatedAtUTC:    projectedNode.CreatedAtUTC,
			State:           projectedNode.State,
			HintText:        projectedNode.HintText,
			ExactSignature:  projectedNode.ExactSignature,
			FamilySignature: projectedNode.FamilySignature,
			ProvenanceEvent: projectedNode.ProvenanceEvent,
			MatchCount:      projectedNode.MatchCount,
		})
	}
	return discoveredItems, nil
}

func (backend *continuityTCLMemoryBackend) TraceProjectedNodeCandidates(ctx context.Context, rawRequest ProjectedNodeDiscoverRequest) ([]ProjectedNodeCandidateTrace, error) {
	_ = ctx
	validatedRequest := rawRequest
	if strings.TrimSpace(validatedRequest.Scope) == "" {
		validatedRequest.Scope = memoryScopeGlobal
	}
	if validatedRequest.MaxItems <= 0 {
		validatedRequest.MaxItems = 5
	}

	searchDebugReport, err := backend.store.debugSearchProjectedNodes(validatedRequest.Scope, validatedRequest.Query, validatedRequest.MaxItems)
	if err != nil {
		return nil, err
	}
	candidateTrace := make([]ProjectedNodeCandidateTrace, 0, len(searchDebugReport.Nodes))
	for _, debugNode := range searchDebugReport.Nodes {
		if debugNode.AdmissionResult != "matched_query_overlap" {
			continue
		}
		candidateTrace = append(candidateTrace, ProjectedNodeCandidateTrace{
			CandidateID:                debugNode.NodeID,
			NodeKind:                   debugNode.NodeKind,
			SourceKind:                 debugNode.SourceKind,
			CanonicalKey:               debugNode.CanonicalKey,
			AnchorTupleKey:             debugNode.AnchorTupleKey,
			MatchCount:                 debugNode.MatchCount,
			RankBeforeSlotPreference:   debugNode.RankBeforeSlotPreference,
			RankBeforeTruncation:       debugNode.RankBeforeTruncation,
			FinalKeptRank:              debugNode.FinalKeptRank,
			SlotPreferenceTargetAnchor: debugNode.SlotPreferenceTargetAnchor,
			SlotPreferenceApplied:      debugNode.SlotPreferenceApplied,
		})
	}
	return candidateTrace, nil
}

func (backend *continuityTCLMemoryBackend) debugProductionParityMaterializedFacts(scope string) []productionParityMaterializedFactDebugRecord {
	trimmedScope := strings.TrimSpace(scope)
	if trimmedScope == "" {
		trimmedScope = memoryScopeGlobal
	}
	debugRecords := make([]productionParityMaterializedFactDebugRecord, 0, len(backend.productionParityMaterialization))
	for _, debugRecord := range backend.productionParityMaterialization {
		if strings.TrimSpace(debugRecord.Scope) != trimmedScope {
			continue
		}
		debugRecords = append(debugRecords, debugRecord)
	}
	return debugRecords
}
