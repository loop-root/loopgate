package loopgate

import "context"

const (
	memoryBackendContinuityTCL = "continuity_tcl"
	memoryBackendRAGBaseline   = "rag_baseline"
	memoryBackendHybrid        = "hybrid"
)

// MemoryBackend is the internal Loopgate boundary for governed memory storage,
// wake-state assembly, and bounded retrieval. Haven should continue using the
// existing Loopgate memory API regardless of which backend implementation is active.
type MemoryBackend interface {
	Name() string
	SyncAuthoritativeState(ctx context.Context, authoritativeState continuityMemoryState) error

	StoreInspection(ctx context.Context, inspectionRecord continuityInspectionRecord) error
	StoreDistillate(ctx context.Context, distillateRecord continuityDistillateRecord) error
	StoreExplicitRememberedFact(ctx context.Context, distillateRecord continuityDistillateRecord) error

	// Write-side commands receive an already-authenticated control-session context.
	// The backend owns memory mutation from this seam downward; transport auth and
	// capability checks stay outside the backend.
	RememberFact(ctx context.Context, authenticatedSession capabilityToken, request MemoryRememberRequest) (MemoryRememberResponse, error)
	InspectContinuity(ctx context.Context, authenticatedSession capabilityToken, request ContinuityInspectRequest) (ContinuityInspectResponse, error)
	ReviewContinuityInspection(ctx context.Context, authenticatedSession capabilityToken, inspectionID string, request MemoryInspectionReviewRequest) (MemoryInspectionGovernanceResponse, error)
	TombstoneContinuityInspection(ctx context.Context, authenticatedSession capabilityToken, inspectionID string, request MemoryInspectionLineageRequest) (MemoryInspectionGovernanceResponse, error)
	PurgeContinuityInspection(ctx context.Context, authenticatedSession capabilityToken, inspectionID string, request MemoryInspectionLineageRequest) (MemoryInspectionGovernanceResponse, error)

	BuildWakeState(ctx context.Context, request MemoryWakeStateRequest) (MemoryWakeStateResponse, error)
	Discover(ctx context.Context, request MemoryDiscoverRequest) (MemoryDiscoverResponse, error)
	Recall(ctx context.Context, request MemoryRecallRequest) (MemoryRecallResponse, error)
}

// MemoryWakeStateRequest is the internal backend request shape for bounded wake-state
// construction. It keeps the backend boundary independent of the current HTTP route
// shape while still returning the public Loopgate wake-state response.
type MemoryWakeStateRequest struct {
	Scope string
}

type ProjectedNodeDiscoverRequest struct {
	Scope    string
	Query    string
	MaxItems int
}

type ProjectedNodeDiscoverItem struct {
	NodeID          string
	NodeKind        string
	SourceKind      string
	CanonicalKey    string
	AnchorTupleKey  string
	Scope           string
	CreatedAtUTC    string
	State           string
	HintText        string
	ExactSignature  string
	FamilySignature string
	ProvenanceEvent string
	MatchCount      int
}

type ProjectedNodeCandidateTrace struct {
	CandidateID                string
	NodeKind                   string
	SourceKind                 string
	CanonicalKey               string
	AnchorTupleKey             string
	MatchCount                 int
	RankBeforeSlotPreference   int
	RankBeforeTruncation       int
	FinalKeptRank              int
	SlotPreferenceTargetAnchor string
	SlotPreferenceApplied      bool
}

type ProjectedNodeDiscoverTraceBackend interface {
	TraceProjectedNodeCandidates(ctx context.Context, rawRequest ProjectedNodeDiscoverRequest) ([]ProjectedNodeCandidateTrace, error)
}
