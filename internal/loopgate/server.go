package loopgate

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"morph/internal/audit"
	"morph/internal/config"
	"morph/internal/identifiers"
	"morph/internal/ledger"
	"morph/internal/loopdiag"
	modelpkg "morph/internal/model"
	modelruntime "morph/internal/modelruntime"
	policypkg "morph/internal/policy"
	"morph/internal/sandbox"
	"morph/internal/secrets"
	toolspkg "morph/internal/tools"
)

const statusVersion = "0.1.0"

type requestContextKey string

const peerIdentityContextKey requestContextKey = "loopgate_peer_identity"

type peerIdentity struct {
	UID  uint32
	PID  int
	EPID int
}

type Server struct {
	repoRoot                              string
	socketPath                            string
	auditPath                             string
	noncePath                             string
	quarantineDir                         string
	derivedArtifactDir                    string
	connectionPath                        string
	modelConnectionPath                   string
	morphlingPath                         string
	memoryBasePath                        string
	memoryLegacyPath                      string
	memoryPartitions                      map[string]*memoryPartition
	sandboxPaths                          sandbox.Paths
	policy                                config.Policy
	runtimeConfig                         config.RuntimeConfig
	goalAliases                           config.GoalAliases
	morphlingClassPolicy                  morphlingClassPolicy
	registry                              *toolspkg.Registry
	checker                               *policypkg.Checker
	now                                   func() time.Time
	buildValidatedMemoryRememberCandidate func(MemoryRememberRequest) (memoryValidatedCandidate, error)
	appendAuditEvent                      func(string, ledger.Event) error
	saveMemoryState                       func(string, continuityMemoryState, config.RuntimeConfig) error
	resolveSecretStore                    func(secrets.SecretRef) (secrets.SecretStore, error)
	reportResponseWriteError              func(httpStatus int, cause error)
	reportSecurityWarning                 func(eventCode string, cause error)
	resolvePeerIdentity                   func(net.Conn) (peerIdentity, error)
	resolveExePath                        func(int) (string, error)
	resolveUserHomeDir                    func() (string, error)
	expectedClientPath                    string
	newModelClientFromConfig              func(modelruntime.Config) (*modelpkg.Client, modelruntime.Config, error)
	server                                *http.Server
	// diagnostic is optional operator text logging (runtime/logs); not authoritative audit.
	diagnostic *loopdiag.Manager

	auditMu       sync.Mutex
	auditSequence uint64
	lastAuditHash string

	promotionMu sync.Mutex

	uiMu               sync.Mutex
	uiSequence         uint64
	uiEvents           []UIEventEnvelope
	uiSubscribers      map[int]uiEventSubscriber
	nextUISubscriberID int

	// havenDeskNotesMu serializes load/save of runtime/state/haven_desk_notes.json for /v1/ui/desk-notes.
	havenDeskNotesMu sync.Mutex

	// havenPreferencesMu serializes load/save of runtime/state/haven_preferences.json.
	havenPreferencesMu sync.Mutex

	connectionsMu sync.Mutex
	connections   map[string]connectionRecord

	modelConnectionsMu sync.Mutex
	modelConnections   map[string]modelConnectionRecord

	morphlingsMu      sync.Mutex
	morphlings        map[string]morphlingRecord
	morphlingStateKey []byte
	morphlingKeyPath  string

	memoryMu                  sync.Mutex
	memoryFactWritesBySession map[string][]time.Time
	memoryFactWritesByUID     map[uint32][]time.Time

	// taskPlansMu protects taskPlans, taskLeases, and taskExecutions.
	// Lock ordering: server.mu → (release) → taskPlansMu → (release) → auditMu.
	// Exception: validateAndRecordApprovalDecision holds server.mu while calling logEvent
	// (which acquires auditMu) so approval state cannot advance until grant/deny audit
	// append succeeds. Safe because appendAuditEvent does not re-enter server.mu.
	// server.mu and taskPlansMu are NEVER held simultaneously.
	taskPlansMu    sync.Mutex
	taskPlans      map[string]*taskPlanRecord
	taskLeases     map[string]*taskLeaseRecord
	taskExecutions map[string]*taskExecutionRecord

	hostAccessPlansMu sync.Mutex
	hostAccessPlans   map[string]*hostAccessStoredPlan
	// hostAccessAppliedPlanAt records plan IDs that completed host.plan.apply
	// successfully so a second apply with the same id returns a clear recovery
	// message instead of a generic "unknown" error.
	hostAccessAppliedPlanAt map[string]time.Time

	configStateDir string

	morphlingWorkerLaunches map[string]morphlingWorkerLaunch
	morphlingWorkerSessions map[string]morphlingWorkerSession

	providerTokenMu        sync.Mutex
	providerTokens         map[string]providerAccessToken
	configuredConnections  map[string]configuredConnection
	configuredCapabilities map[string]configuredCapability
	httpClient             *http.Client

	pkceMu       sync.Mutex
	pkceSessions map[string]pendingPKCESession

	mu                 sync.Mutex
	sessions           map[string]controlSession
	tokens             map[string]capabilityToken
	approvals          map[string]pendingApproval
	seenRequests       map[string]seenRequest
	seenAuthNonces     map[string]seenRequest
	usedTokens         map[string]usedToken
	sessionOpenByUID   map[uint32]time.Time
	approvalTokenIndex map[string]string      // SHA-256(approval token) → control session ID
	sessionReadCounts  map[string][]time.Time // control session ID → timestamps of fs_read executions

	sessionOpenMinInterval  time.Duration
	maxActiveSessionsPerUID int
	expirySweepMaxInterval  time.Duration
	nextExpirySweepAt       time.Time
	// maxPendingApprovalsPerControlSession limits pending (state=pending) approvals per session.
	maxPendingApprovalsPerControlSession int
	maxSeenRequestReplayEntries          int
	maxAuthNonceReplayEntries            int
}

type capabilityToken struct {
	TokenID             string
	Token               string
	ControlSessionID    string
	ActorLabel          string
	ClientSessionLabel  string
	AllowedCapabilities map[string]struct{}
	PeerIdentity        peerIdentity
	// TenantID and UserID mirror the authoritative control session (from runtime tenancy at session open).
	// They exist so derived execution tokens and in-memory approval contexts carry the same namespace.
	TenantID          string
	UserID            string
	ExpiresAt         time.Time
	SingleUse         bool
	BoundCapability   string
	BoundArgumentHash string
	ParentTokenID     string
}

type controlSession struct {
	ID                    string
	ActorLabel            string
	ClientSessionLabel    string
	WorkspaceID           string
	RequestedCapabilities map[string]struct{}
	ApprovalToken         string
	ApprovalTokenID       string
	SessionMACKey         string
	PeerIdentity          peerIdentity
	// TenantID/UserID are copied from config.Tenancy at session open (never from the client body).
	TenantID  string
	UserID    string
	ExpiresAt time.Time
	CreatedAt time.Time
}

type approvalExecutionContext struct {
	ControlSessionID    string
	ActorLabel          string
	ClientSessionLabel  string
	AllowedCapabilities map[string]struct{}
	TenantID            string
	UserID              string
}

type pendingApproval struct {
	ID                  string
	Request             CapabilityRequest
	CreatedAt           time.Time
	ExpiresAt           time.Time
	Metadata            map[string]interface{}
	Reason              string
	ControlSessionID    string
	DecisionNonce       string
	DecisionSubmittedAt time.Time
	ExecutedAt          time.Time
	ExecutionContext    approvalExecutionContext
	State               string
	// ApprovalManifestSHA256 is the canonical approval manifest hash per AMP RFC 0005 §6,
	// computed at approval creation time from the action class, subject, execution method,
	// path, request body hash, scope, and expiry. Verified against the operator-submitted
	// hash at decision time to bind the decision to the exact approved action.
	ApprovalManifestSHA256 string
	// ExecutionBodySHA256 is the SHA256 of the serialized CapabilityRequest body, stored at
	// approval creation time. At execution time (PR 1b), the live request body hash is
	// verified against this value along with the method and path to confirm exact match.
	ExecutionBodySHA256 string
}

type seenRequest struct {
	ControlSessionID string
	SeenAt           time.Time
}

type usedToken struct {
	TokenID           string
	ParentTokenID     string
	ControlSessionID  string
	Capability        string
	NormalizedArgHash string
	ConsumedAt        time.Time
}

const (
	sessionTTL                     = 1 * time.Hour
	approvalTTL                    = 5 * time.Minute
	requestReplayWindow            = 24 * time.Hour
	requestSignatureSkew           = 2 * time.Minute
	maxOpenSessionBodyBytes        = 16 * 1024
	maxCapabilityBodyBytes         = 512 * 1024
	maxApprovalBodyBytes           = 8 * 1024
	maxModelReplyBodyBytes         = 1024 * 1024
	maxHeaderBytes                 = 8 * 1024
	defaultSessionOpenMinInterval  = 500 * time.Millisecond
	defaultMaxActiveSessionsPerUID = 8
	defaultExpirySweepMaxInterval  = 250 * time.Millisecond
	defaultFsReadRateLimit         = 60 // reads per minute per session
	fsReadRateWindow               = 1 * time.Minute
	// In-memory bounds (F6 / DoS): single-session pending approvals and replay map sizes.
	defaultMaxPendingApprovalsPerControlSession = 64
	defaultMaxSeenRequestReplayEntries          = 65536
	defaultMaxAuthNonceReplayEntries            = 65536
)

const (
	approvalStatePending   = "pending"
	approvalStateDenied    = "denied"
	approvalStateExpired   = "expired"
	approvalStateCancelled = "cancelled"
	// approvalStateConsumed is set atomically when an approved decision is recorded, before
	// execution begins. A concurrent decision that finds this state returns
	// DenialCodeApprovalStateConflict rather than DenialCodeApprovalStateInvalid to distinguish
	// a lost execution race from a genuine state violation such as an expired or denied approval.
	approvalStateConsumed        = "consumed"
	approvalStateExecutionFailed = "execution_failed"
)

func NewServer(repoRoot string, socketPath string) (*Server, error) {
	return NewServerWithOptions(repoRoot, socketPath, false)
}

// NewServerWithOptions constructs the Loopgate server for the local Unix-socket control plane.
func NewServerWithOptions(repoRoot string, socketPath string, acceptPolicy bool) (*Server, error) {
	configStateDir := filepath.Join(repoRoot, "runtime", "state", "config")

	// Load policy: JSON state → YAML seed → fail.
	policy, err := config.LoadOrSeed(configStateDir, "policy",
		filepath.Join(repoRoot, "core", "policy", "policy.yaml"),
		func(_ string) (config.Policy, error) {
			return config.LoadPolicy(repoRoot)
		},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("load policy: %w", err)
	}

	// Verify policy hash integrity (hash of JSON state content).
	policyJSON, hashErr := config.PolicyToJSON(policy)
	if hashErr != nil {
		return nil, fmt.Errorf("serialize policy for hash: %w", hashErr)
	}
	policyHash := fmt.Sprintf("%x", sha256.Sum256(policyJSON))
	hashMatch, storedHash, hashErr := config.VerifyPolicyHash(repoRoot, policyHash)
	if hashErr != nil {
		return nil, fmt.Errorf("verify policy hash: %w", hashErr)
	}
	if !hashMatch {
		if acceptPolicy {
			if err := config.AcceptPolicyHash(repoRoot, policyHash); err != nil {
				return nil, fmt.Errorf("accept policy hash: %w", err)
			}
			fmt.Fprintf(os.Stderr, "WARN: policy hash updated (was %s, now %s)\n", storedHash, policyHash)
		} else {
			return nil, fmt.Errorf("policy file has changed (stored hash %s, current hash %s); restart with --accept-policy to accept", storedHash, policyHash)
		}
	}

	// Load runtime config from YAML (and optional diagnostic override). Do not use
	// runtime/state/config/runtime.json — operators and config PUT edit config/runtime.yaml.
	runtimeConfig, err := config.LoadRuntimeConfig(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("load runtime config: %w", err)
	}

	goalAliases, err := config.LoadGoalAliases(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("load goal aliases: %w", err)
	}

	// Root tool registry in the sandbox home so tool execution happens inside
	// Morph's virtual filesystem, not the host repo root.
	sandboxHome := sandbox.PathsForRepo(repoRoot).Home
	registry, err := toolspkg.NewSandboxRegistry(repoRoot, sandboxHome, policy)
	if err != nil {
		return nil, fmt.Errorf("create tool registry: %w", err)
	}

	// Load morphling class policy: JSON state → YAML seed → fail.
	morphlingClassPolicy, err := loadMorphlingClassPolicyWithSeed(configStateDir, repoRoot, registry)
	if err != nil {
		return nil, fmt.Errorf("load morphling class policy: %w", err)
	}

	// Load connections: JSON state → YAML seed → empty.
	configuredConnections, configuredCapabilities, err := loadConfiguredConnectionsWithSeed(configStateDir, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("load configured connections: %w", err)
	}
	server := &Server{
		repoRoot:                              repoRoot,
		socketPath:                            socketPath,
		configStateDir:                        configStateDir,
		auditPath:                             filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"),
		noncePath:                             filepath.Join(repoRoot, "runtime", "state", "nonce_replay.json"),
		quarantineDir:                         filepath.Join(repoRoot, "runtime", "state", "quarantine"),
		derivedArtifactDir:                    filepath.Join(repoRoot, "runtime", "state", "derived_artifacts"),
		connectionPath:                        filepath.Join(repoRoot, "runtime", "state", "loopgate_connections.json"),
		modelConnectionPath:                   filepath.Join(repoRoot, "runtime", "state", "loopgate_model_connections.json"),
		morphlingPath:                         filepath.Join(repoRoot, "runtime", "state", "loopgate_morphlings.json"),
		morphlingKeyPath:                      filepath.Join(repoRoot, "runtime", "state", "morphling_state_key"),
		memoryBasePath:                        filepath.Join(repoRoot, "runtime", "state", "memory"),
		memoryPartitions:                      make(map[string]*memoryPartition),
		memoryLegacyPath:                      filepath.Join(repoRoot, "runtime", "state", "loopgate_memory.json"),
		sandboxPaths:                          sandbox.PathsForRepo(repoRoot),
		policy:                                policy,
		runtimeConfig:                         runtimeConfig,
		goalAliases:                           goalAliases,
		morphlingClassPolicy:                  morphlingClassPolicy,
		registry:                              registry,
		checker:                               policypkg.NewChecker(policy),
		now:                                   time.Now,
		buildValidatedMemoryRememberCandidate: buildValidatedMemoryRememberCandidate,
		saveMemoryState:                       nil,
		resolveSecretStore:                    secrets.NewStoreForRef,
		reportResponseWriteError: func(httpStatus int, cause error) {
			fmt.Fprintf(os.Stderr, "ERROR: response_write status=%d class=%s\n", httpStatus, secrets.LoopgateOperatorErrorClass(cause))
		},
		reportSecurityWarning: func(eventCode string, cause error) {
			fmt.Fprintf(os.Stderr, "WARN: security event=%s class=%s\n", eventCode, secrets.LoopgateOperatorErrorClass(cause))
		},
		resolvePeerIdentity:       peerIdentityFromConn,
		resolveExePath:            resolveExecutablePath,
		resolveUserHomeDir:        os.UserHomeDir,
		providerTokens:            make(map[string]providerAccessToken),
		configuredConnections:     configuredConnections,
		configuredCapabilities:    configuredCapabilities,
		httpClient:                &http.Client{Timeout: time.Duration(policy.Tools.HTTP.TimeoutSeconds) * time.Second},
		pkceSessions:              make(map[string]pendingPKCESession),
		memoryFactWritesBySession: make(map[string][]time.Time),
		memoryFactWritesByUID:     make(map[uint32][]time.Time),
		morphlingWorkerLaunches:   make(map[string]morphlingWorkerLaunch),
		morphlingWorkerSessions:   make(map[string]morphlingWorkerSession),
		sessions:                  make(map[string]controlSession),
		tokens:                    make(map[string]capabilityToken),
		approvals:                 make(map[string]pendingApproval),
		seenRequests:              make(map[string]seenRequest),
		seenAuthNonces:            make(map[string]seenRequest),
		usedTokens:                make(map[string]usedToken),
		sessionOpenByUID:          make(map[uint32]time.Time),
		approvalTokenIndex:        make(map[string]string),
		sessionReadCounts:         make(map[string][]time.Time),
		sessionOpenMinInterval:    defaultSessionOpenMinInterval,
		maxActiveSessionsPerUID:   defaultMaxActiveSessionsPerUID,
		expirySweepMaxInterval:    defaultExpirySweepMaxInterval,
		maxPendingApprovalsPerControlSession: defaultMaxPendingApprovalsPerControlSession,
		maxSeenRequestReplayEntries:          defaultMaxSeenRequestReplayEntries,
		maxAuthNonceReplayEntries:            defaultMaxAuthNonceReplayEntries,
		taskPlans:                 make(map[string]*taskPlanRecord),
		taskLeases:                make(map[string]*taskLeaseRecord),
		taskExecutions:            make(map[string]*taskExecutionRecord),
		hostAccessPlans:           make(map[string]*hostAccessStoredPlan),
		hostAccessAppliedPlanAt:   make(map[string]time.Time),
	}
	server.saveMemoryState = func(path string, st continuityMemoryState, cfg config.RuntimeConfig) error {
		return saveContinuityMemoryState(path, st, cfg, server.now().UTC())
	}
	if err := maybeMigrateMemoryToPartitionedLayout(server.memoryBasePath); err != nil {
		return nil, fmt.Errorf("migrate memory layout: %w", err)
	}
	server.appendAuditEvent = func(path string, auditEvent ledger.Event) error {
		return ledger.AppendWithRotation(path, auditEvent, server.auditLedgerRotationSettings())
	}
	server.newModelClientFromConfig = server.newModelClientFromRuntimeConfig
	if err := server.registerConfiguredCapabilities(); err != nil {
		return nil, fmt.Errorf("register configured capabilities: %w", err)
	}
	if err := server.sandboxPaths.Ensure(); err != nil {
		return nil, fmt.Errorf("ensure sandbox paths: %w", err)
	}
	morphlingKey, err := loadOrCreateStateKey(server.morphlingKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load morphling state key: %w", err)
	}
	server.morphlingStateKey = morphlingKey
	loadedMorphlings, err := loadMorphlingRecords(server.morphlingPath, server.morphlingStateKey)
	if err != nil {
		return nil, fmt.Errorf("load morphling records: %w", err)
	}
	server.morphlings = loadedMorphlings
	server.memoryMu.Lock()
	if err := server.initDefaultMemoryPartitionLocked(); err != nil {
		server.memoryMu.Unlock()
		return nil, fmt.Errorf("init default memory partition: %w", err)
	}
	server.memoryMu.Unlock()
	if err := server.rebuildContinuityWakeStateFromAuthority(); err != nil {
		return nil, fmt.Errorf("rebuild continuity wake state: %w", err)
	}
	if err := server.syncMemoryBackendFromAuthority(); err != nil {
		return nil, fmt.Errorf("sync memory backend: %w", err)
	}
	loadedConnections, err := loadConnectionRecords(server.connectionPath)
	if err != nil {
		return nil, fmt.Errorf("load connection records: %w", err)
	}
	server.connections = loadedConnections
	loadedModelConnections, err := loadModelConnectionRecords(server.modelConnectionPath)
	if err != nil {
		return nil, fmt.Errorf("load model connection records: %w", err)
	}
	server.modelConnections = loadedModelConnections
	if err := server.loadAuditChainState(); err != nil {
		return nil, fmt.Errorf("load audit chain state: %w", err)
	}
	if err := server.recoverMorphlings(); err != nil {
		return nil, fmt.Errorf("recover morphlings: %w", err)
	}
	if err := server.loadNonceReplayState(); err != nil {
		return nil, fmt.Errorf("load nonce replay state: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", server.handleHealth)
	mux.HandleFunc("/v1/status", server.handleStatus)
	mux.HandleFunc("/v1/ui/status", server.handleUIStatus)
	mux.HandleFunc("/v1/ui/events", server.handleUIEvents)
	mux.HandleFunc("/v1/ui/approvals", server.handleUIApprovals)
	mux.HandleFunc("/v1/ui/approvals/", server.handleUIApprovalDecision)
	mux.HandleFunc("/v1/ui/folder-access", server.handleFolderAccess)
	mux.HandleFunc("/v1/ui/folder-access/sync", server.handleFolderAccessSync)
	mux.HandleFunc("/v1/ui/task-standing-grants", server.handleTaskStandingGrants)
	mux.HandleFunc("/v1/ui/shared-folder", server.handleSharedFolderStatus)
	mux.HandleFunc("/v1/ui/shared-folder/sync", server.handleSharedFolderSync)
	mux.HandleFunc("/v1/ui/desk-notes/dismiss", server.handleHavenDeskNotesDismiss)
	mux.HandleFunc("/v1/ui/desk-notes", server.handleHavenDeskNotes)
	mux.HandleFunc("/v1/ui/journal/entries", server.handleHavenJournalEntries)
	mux.HandleFunc("/v1/ui/journal/entry", server.handleHavenJournalEntry)
	mux.HandleFunc("/v1/ui/paint/gallery", server.handleHavenPaintGallery)
	mux.HandleFunc("/v1/ui/working-notes/save", server.handleHavenWorkingNotesSave)
	mux.HandleFunc("/v1/ui/working-notes/entry", server.handleHavenWorkingNotesEntry)
	mux.HandleFunc("/v1/ui/working-notes", server.handleHavenWorkingNotes)
	mux.HandleFunc("/v1/ui/workspace/list", server.handleHavenWorkspaceList)
	mux.HandleFunc("/v1/ui/workspace/preview", server.handleHavenWorkspacePreview)
	mux.HandleFunc("/v1/ui/memory/reset", server.handleHavenMemoryReset)
	mux.HandleFunc("/v1/ui/memory", server.handleHavenMemoryInventory)
	mux.HandleFunc("/v1/ui/morph-sleep", server.handleHavenMorphSleep)
	mux.HandleFunc("/v1/ui/presence", server.handleHavenPresence)
	mux.HandleFunc("/v1/diagnostic/report", server.handleDiagnosticReport)
	mux.HandleFunc("/v1/session/open", server.handleSessionOpen)
	mux.HandleFunc("/v1/model/reply", server.handleModelReply)
	mux.HandleFunc("/v1/model/validate", server.handleModelValidate)
	mux.HandleFunc("/v1/model/ollama/tags", server.handleOllamaTags)
	mux.HandleFunc("/v1/model/openai/models", server.handleOpenAICompatibleModels)
	mux.HandleFunc("/v1/model/connections/store", server.handleModelConnectionStore)
	mux.HandleFunc("/v1/chat", server.handleHavenChat)
	mux.HandleFunc("/v1/settings/shell-dev", server.handleHavenSettingsShellDev)
	mux.HandleFunc("/v1/settings/idle", server.handleHavenSettingsIdle)
	mux.HandleFunc("/v1/model/settings", server.handleHavenModelSettings)
	mux.HandleFunc("/v1/resident/journal-tick", server.handleHavenJournalResidentTick)
	mux.HandleFunc("/v1/agent/work-item/ensure", server.handleHavenAgentWorkItemEnsure)
	mux.HandleFunc("/v1/agent/work-item/complete", server.handleHavenAgentWorkItemComplete)
	mux.HandleFunc("/v1/continuity/inspect-thread", server.handleHavenContinuityInspectThread)
	// Deprecated compatibility aliases. Keep these until all local HTTP clients
	// have migrated off the historical /v1/haven/... prefix.
	mux.HandleFunc("/v1/haven/chat", server.handleHavenChat)
	mux.HandleFunc("/v1/haven/settings/shell-dev", server.handleHavenSettingsShellDev)
	mux.HandleFunc("/v1/haven/settings/idle", server.handleHavenSettingsIdle)
	mux.HandleFunc("/v1/haven/model-settings", server.handleHavenModelSettings)
	mux.HandleFunc("/v1/haven/resident/journal-tick", server.handleHavenJournalResidentTick)
	mux.HandleFunc("/v1/haven/agent/work-item/ensure", server.handleHavenAgentWorkItemEnsure)
	mux.HandleFunc("/v1/haven/agent/work-item/complete", server.handleHavenAgentWorkItemComplete)
	mux.HandleFunc("/v1/haven/continuity/inspect-thread", server.handleHavenContinuityInspectThread)
	mux.HandleFunc("/v1/capabilities/execute", server.handleCapabilityExecute)
	mux.HandleFunc("/v1/connections/status", server.handleConnectionsStatus)
	mux.HandleFunc("/v1/connections/validate", server.handleConnectionValidate)
	mux.HandleFunc("/v1/connections/pkce/start", server.handleConnectionPKCEStart)
	mux.HandleFunc("/v1/connections/pkce/complete", server.handleConnectionPKCEComplete)
	mux.HandleFunc("/v1/sites/inspect", server.handleSiteInspect)
	mux.HandleFunc("/v1/sites/trust-draft", server.handleSiteTrustDraft)
	mux.HandleFunc("/v1/sandbox/import", server.handleSandboxImport)
	mux.HandleFunc("/v1/sandbox/stage", server.handleSandboxStage)
	mux.HandleFunc("/v1/sandbox/metadata", server.handleSandboxMetadata)
	mux.HandleFunc("/v1/sandbox/export", server.handleSandboxExport)
	mux.HandleFunc("/v1/sandbox/list", server.handleSandboxList)
	mux.HandleFunc("/v1/continuity/inspect", server.handleContinuityInspect)
	mux.HandleFunc("/v1/memory/wake-state", server.handleMemoryWakeState)
	mux.HandleFunc("/v1/tasks", server.handleTasksCollection)
	mux.HandleFunc("/v1/tasks/", server.handleTasksSubpaths)
	mux.HandleFunc("/v1/memory/diagnostic-wake", server.handleMemoryDiagnosticWake)
	mux.HandleFunc("/v1/memory/discover", server.handleMemoryDiscover)
	mux.HandleFunc("/v1/memory/recall", server.handleMemoryRecall)
	mux.HandleFunc("/v1/memory/remember", server.handleMemoryRemember)
	mux.HandleFunc("/v1/memory/inspections/", server.handleMemoryInspectionGovernance)
	mux.HandleFunc("/v1/morphlings/spawn", server.handleMorphlingSpawn)
	mux.HandleFunc("/v1/morphlings/status", server.handleMorphlingStatus)
	mux.HandleFunc("/v1/morphlings/terminate", server.handleMorphlingTerminate)
	mux.HandleFunc("/v1/morphlings/review", server.handleMorphlingReview)
	mux.HandleFunc("/v1/morphlings/worker/launch", server.handleMorphlingWorkerLaunch)
	mux.HandleFunc("/v1/morphlings/worker/open", server.handleMorphlingWorkerOpen)
	mux.HandleFunc("/v1/morphlings/worker/start", server.handleMorphlingWorkerStart)
	mux.HandleFunc("/v1/morphlings/worker/update", server.handleMorphlingWorkerUpdate)
	mux.HandleFunc("/v1/morphlings/worker/complete", server.handleMorphlingWorkerComplete)
	mux.HandleFunc("/v1/quarantine/metadata", server.handleQuarantineMetadata)
	mux.HandleFunc("/v1/quarantine/view", server.handleQuarantineView)
	mux.HandleFunc("/v1/quarantine/prune", server.handleQuarantinePrune)
	mux.HandleFunc("/v1/task/plan", server.handleTaskPlanSubmit)
	mux.HandleFunc("/v1/task/lease", server.handleTaskLeaseRequest)
	mux.HandleFunc("/v1/task/execute", server.handleTaskLeaseExecute)
	mux.HandleFunc("/v1/task/complete", server.handleTaskLeaseComplete)
	mux.HandleFunc("/v1/task/result", server.handleTaskPlanResult)
	mux.HandleFunc("/v1/config/", server.handleConfig)
	mux.HandleFunc("/v1/approvals/", server.handleApprovalDecision)
	mux.HandleFunc("/v1/hook/pre-validate", server.handleHookPreValidate)

	handler := http.Handler(mux)
	diagnostic, diagErr := loopdiag.Open(repoRoot, server.runtimeConfig.Logging.Diagnostic)
	if diagErr != nil {
		return nil, fmt.Errorf("open diagnostic logs: %w", diagErr)
	}
	server.diagnostic = diagnostic
	if diagnostic != nil {
		handler = loopdiag.HTTPMiddleware(diagnostic.Client, mux)
	}
	server.reportResponseWriteError = func(httpStatus int, cause error) {
		fmt.Fprintf(os.Stderr, "ERROR: response_write status=%d class=%s\n", httpStatus, secrets.LoopgateOperatorErrorClass(cause))
		if server.diagnostic != nil && server.diagnostic.Server != nil {
			server.diagnostic.Server.Error("response_write",
				"http_status", httpStatus,
				"operator_error_class", secrets.LoopgateOperatorErrorClass(cause),
			)
		}
	}
	server.reportSecurityWarning = func(eventCode string, cause error) {
		fmt.Fprintf(os.Stderr, "WARN: security event=%s class=%s\n", eventCode, secrets.LoopgateOperatorErrorClass(cause))
		if server.diagnostic != nil && server.diagnostic.Server != nil {
			server.diagnostic.Server.Warn("security_warning",
				"event_code", eventCode,
				"operator_error_class", secrets.LoopgateOperatorErrorClass(cause),
			)
		}
	}

	server.server = &http.Server{
		Handler: handler,
		ConnContext: func(ctx context.Context, conn net.Conn) context.Context {
			peerCreds, err := server.resolvePeerIdentity(conn)
			if err != nil {
				if server.reportSecurityWarning != nil {
					server.reportSecurityWarning("unix_peer_resolve_failed", err)
				}
				return ctx
			}
			if server.diagnostic != nil && server.diagnostic.Socket != nil {
				server.diagnostic.Socket.Debug("unix_peer",
					"uid", peerCreds.UID,
					"pid", peerCreds.PID,
					"remote", conn.RemoteAddr().String(),
				)
			}
			return context.WithValue(ctx, peerIdentityContextKey, peerCreds)
		},
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    maxHeaderBytes,
	}

	return server, nil
}

// NewServerForIntegrationHarness is like NewServer but disables the minimum interval between
// session opens. The integration and shell test harnesses perform a bootstrap session open
// (to obtain a signed /v1/status) and tests open their own sessions immediately afterward;
// without this, the default interval produces flaky 429s under parallel execution.
func NewServerForIntegrationHarness(repoRoot string, socketPath string) (*Server, error) {
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		return nil, err
	}
	server.sessionOpenMinInterval = 0
	return server, nil
}

func (server *Server) loadAuditChainState() error {
	lastAuditSequence, lastAuditHash, err := ledger.ReadSegmentedChainState(server.auditPath, "audit_sequence", server.auditLedgerRotationSettings())
	if err != nil {
		return err
	}
	server.auditSequence = uint64(lastAuditSequence)
	server.lastAuditHash = lastAuditHash
	return nil
}

func (server *Server) auditLedgerRotationSettings() ledger.RotationSettings {
	segmentDir := filepath.Join(server.repoRoot, server.runtimeConfig.Logging.AuditLedger.SegmentDir)
	manifestPath := filepath.Join(server.repoRoot, server.runtimeConfig.Logging.AuditLedger.ManifestPath)
	verifyClosedSegmentsOnStartup := true
	if server.runtimeConfig.Logging.AuditLedger.VerifyClosedSegmentsOnStartup != nil {
		verifyClosedSegmentsOnStartup = *server.runtimeConfig.Logging.AuditLedger.VerifyClosedSegmentsOnStartup
	}
	return ledger.RotationSettings{
		MaxEventBytes:                 server.runtimeConfig.Logging.AuditLedger.MaxEventBytes,
		RotateAtBytes:                 server.runtimeConfig.Logging.AuditLedger.RotateAtBytes,
		SegmentDir:                    segmentDir,
		ManifestPath:                  manifestPath,
		VerifyClosedSegmentsOnStartup: verifyClosedSegmentsOnStartup,
	}
}

// SetNowForTest overrides the server's time function. For testing only.
func (server *Server) SetNowForTest(fn func() time.Time) {
	server.now = fn
}

func (server *Server) Serve(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(server.socketPath), 0o700); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(server.auditPath), 0o700); err != nil {
		return fmt.Errorf("create audit dir: %w", err)
	}
	if err := os.MkdirAll(server.quarantineDir, 0o700); err != nil {
		return fmt.Errorf("create quarantine dir: %w", err)
	}
	if err := os.MkdirAll(server.derivedArtifactDir, 0o700); err != nil {
		return fmt.Errorf("create derived artifact dir: %w", err)
	}
	if err := os.RemoveAll(server.socketPath); err != nil {
		return fmt.Errorf("remove stale socket: %w", err)
	}

	listener, err := net.Listen("unix", server.socketPath)
	if err != nil {
		return fmt.Errorf("listen unix socket: %w", err)
	}
	if err := os.Chmod(server.socketPath, 0o600); err != nil {
		_ = listener.Close()
		return fmt.Errorf("chmod socket: %w", err)
	}
	if server.diagnostic != nil {
		if server.diagnostic.Socket != nil {
			server.diagnostic.Socket.Info("socket_listen", "path", server.socketPath)
		}
		if server.diagnostic.Server != nil {
			server.diagnostic.Server.Info("loopgate_listen", "socket", server.socketPath, "audit_path", server.auditPath)
		}
	}

	go func() {
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.server.Shutdown(shutdownContext)
	}()

	serveErr := server.server.Serve(listener)
	// Persist nonce replay state on shutdown for cross-restart replay protection.
	_ = server.saveNonceReplayState()
	if serveErr == nil || serveErr == http.ErrServerClosed {
		return nil
	}
	return serveErr
}

func (server *Server) executeCapabilityRequest(ctx context.Context, tokenClaims capabilityToken, capabilityRequest CapabilityRequest, allowApprovalCreation bool) CapabilityResponse {
	if strings.TrimSpace(capabilityRequest.RequestID) == "" {
		requestID, _ := randomHex(8)
		capabilityRequest.RequestID = "req_" + requestID
	}
	if capabilityRequest.Arguments == nil {
		capabilityRequest.Arguments = make(map[string]string)
	}
	capabilityRequest = normalizeCapabilityRequest(capabilityRequest)
	capabilityRequest.Actor = tokenClaims.ActorLabel
	capabilityRequest.SessionID = tokenClaims.ControlSessionID
	if err := capabilityRequest.Validate(); err != nil {
		return CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		}
	}

	if allowApprovalCreation {
		if replayDenied := server.recordRequest(tokenClaims.ControlSessionID, capabilityRequest); replayDenied != nil {
			return *replayDenied
		}
	}

	tool := server.registry.Get(capabilityRequest.Capability)
	if server.capabilityProhibitsRawSecretExport(tool, capabilityRequest.Capability) {
		if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"reason":               "raw secret export is prohibited",
			"denial_code":          DenialCodeSecretExportProhibited,
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
		}); err != nil {
			return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
		}
		deniedResponse := CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusDenied,
			DenialReason: "raw secret export is prohibited",
			DenialCode:   DenialCodeSecretExportProhibited,
			Redacted:     true,
		}
		server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, deniedResponse.DenialCode, deniedResponse.DenialReason)
		return deniedResponse
	}

	if len(tokenClaims.AllowedCapabilities) > 0 {
		if _, allowed := tokenClaims.AllowedCapabilities[capabilityRequest.Capability]; !allowed {
			if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
				"request_id":           capabilityRequest.RequestID,
				"capability":           capabilityRequest.Capability,
				"reason":               "capability token scope denied requested capability",
				"denial_code":          DenialCodeCapabilityTokenScopeDenied,
				"actor_label":          tokenClaims.ActorLabel,
				"client_session_label": tokenClaims.ClientSessionLabel,
				"control_session_id":   tokenClaims.ControlSessionID,
			}); err != nil {
				return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
			}
			deniedResponse := CapabilityResponse{
				RequestID:    capabilityRequest.RequestID,
				Status:       ResponseStatusDenied,
				DenialReason: "capability token scope denied requested capability",
				DenialCode:   DenialCodeCapabilityTokenScopeDenied,
			}
			server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, deniedResponse.DenialCode, deniedResponse.DenialReason)
			return deniedResponse
		}
	}

	if tool == nil {
		if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"reason":               "unknown capability",
			"denial_code":          DenialCodeUnknownCapability,
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
		}); err != nil {
			return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
		}
		deniedResponse := CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusDenied,
			DenialReason: "unknown capability",
			DenialCode:   DenialCodeUnknownCapability,
		}
		server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, deniedResponse.DenialCode, deniedResponse.DenialReason)
		return deniedResponse
	}

	if err := tool.Schema().Validate(capabilityRequest.Arguments); err != nil {
		if auditErr := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"reason":               secrets.RedactText(err.Error()),
			"denial_code":          DenialCodeInvalidCapabilityArguments,
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
		}); auditErr != nil {
			return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
		}
		errorResponse := CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeInvalidCapabilityArguments,
			Redacted:     true,
		}
		server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, errorResponse.DenialCode, errorResponse.DenialReason)
		return errorResponse
	}

	policyDecision := server.checker.Check(tool)
	if shouldAutoAllowTrustedSandboxCapability(tokenClaims, tool, policyDecision) {
		policyDecision = policypkg.CheckResult{
			Decision: policypkg.Allow,
			Reason:   "trusted Haven-native sandbox capability",
		}
	}
	if err := server.logEvent("capability.requested", tokenClaims.ControlSessionID, map[string]interface{}{
		"request_id":           capabilityRequest.RequestID,
		"capability":           capabilityRequest.Capability,
		"decision":             policyDecision.Decision.String(),
		"reason":               secrets.RedactText(policyDecision.Reason),
		"actor_label":          tokenClaims.ActorLabel,
		"client_session_label": tokenClaims.ClientSessionLabel,
		"control_session_id":   tokenClaims.ControlSessionID,
	}); err != nil {
		return CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusError,
			DenialReason: "control-plane audit is unavailable",
			DenialCode:   DenialCodeAuditUnavailable,
		}
	}

	if policyDecision.Decision == policypkg.Deny {
		if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"reason":               secrets.RedactText(policyDecision.Reason),
			"denial_code":          DenialCodePolicyDenied,
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
		}); err != nil {
			return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
		}
		deniedResponse := CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusDenied,
			DenialReason: policyDecision.Reason,
			DenialCode:   DenialCodePolicyDenied,
		}
		server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, deniedResponse.DenialCode, deniedResponse.DenialReason)
		return deniedResponse
	}

	if policyDecision.Decision == policypkg.NeedsApproval && allowApprovalCreation {
		approvalID, err := randomHex(8)
		if err != nil {
			return CapabilityResponse{
				RequestID:    capabilityRequest.RequestID,
				Status:       ResponseStatusError,
				DenialReason: "failed to create approval request",
				DenialCode:   DenialCodeApprovalCreationFailed,
			}
		}

		decisionNonce, err := randomHex(16)
		if err != nil {
			return CapabilityResponse{
				RequestID:    capabilityRequest.RequestID,
				Status:       ResponseStatusError,
				DenialReason: "failed to create approval decision nonce",
				DenialCode:   DenialCodeApprovalCreationFailed,
			}
		}

		metadata := server.approvalMetadata(capabilityRequest)
		expiresAt := server.now().UTC().Add(approvalTTL)
		manifestSHA256, bodySHA256, manifestErr := buildCapabilityApprovalManifest(capabilityRequest, expiresAt.UnixMilli())
		if manifestErr != nil {
			return CapabilityResponse{
				RequestID:    capabilityRequest.RequestID,
				Status:       ResponseStatusError,
				DenialReason: "failed to compute approval manifest",
				DenialCode:   DenialCodeApprovalCreationFailed,
			}
		}
		server.mu.Lock()
		server.pruneExpiredLocked()
		if server.countPendingApprovalsForSessionLocked(tokenClaims.ControlSessionID) >= server.maxPendingApprovalsPerControlSession {
			server.mu.Unlock()
			if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
				"request_id":           capabilityRequest.RequestID,
				"capability":           capabilityRequest.Capability,
				"reason":               "pending approval limit reached for control session",
				"denial_code":          DenialCodePendingApprovalLimitReached,
				"actor_label":          tokenClaims.ActorLabel,
				"client_session_label": tokenClaims.ClientSessionLabel,
				"control_session_id":   tokenClaims.ControlSessionID,
			}); err != nil {
				return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
			}
			deniedResponse := CapabilityResponse{
				RequestID:    capabilityRequest.RequestID,
				Status:       ResponseStatusDenied,
				DenialReason: "pending approval limit reached for control session",
				DenialCode:   DenialCodePendingApprovalLimitReached,
			}
			server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, deniedResponse.DenialCode, deniedResponse.DenialReason)
			return deniedResponse
		}
		server.approvals[approvalID] = pendingApproval{
			ID:               approvalID,
			Request:          cloneCapabilityRequest(capabilityRequest),
			CreatedAt:        server.now().UTC(),
			ExpiresAt:        expiresAt,
			Metadata:         metadata,
			Reason:           policyDecision.Reason,
			ControlSessionID: tokenClaims.ControlSessionID,
			DecisionNonce:    decisionNonce,
			ExecutionContext: approvalExecutionContext{
				ControlSessionID:    tokenClaims.ControlSessionID,
				ActorLabel:          tokenClaims.ActorLabel,
				ClientSessionLabel:  tokenClaims.ClientSessionLabel,
				AllowedCapabilities: copyCapabilitySet(tokenClaims.AllowedCapabilities),
				TenantID:            tokenClaims.TenantID,
				UserID:              tokenClaims.UserID,
			},
			State:                  approvalStatePending,
			ApprovalManifestSHA256: manifestSHA256,
			ExecutionBodySHA256:    bodySHA256,
		}
		server.noteExpiryCandidateLocked(expiresAt)
		server.mu.Unlock()

		approvalCreatedAuditData := map[string]interface{}{
			"request_id":               capabilityRequest.RequestID,
			"approval_request_id":      approvalID,
			"capability":               capabilityRequest.Capability,
			"approval_class":           metadata["approval_class"],
			"approval_state":           approvalStatePending,
			"actor_label":              tokenClaims.ActorLabel,
			"client_session_label":     tokenClaims.ClientSessionLabel,
			"control_session_id":       tokenClaims.ControlSessionID,
			"approval_manifest_sha256": manifestSHA256,
		}
		if approvalClass, ok := metadata["approval_class"].(string); ok && strings.TrimSpace(approvalClass) != "" {
			approvalCreatedAuditData["approval_class"] = approvalClass
		}
		if err := server.logEvent("approval.created", tokenClaims.ControlSessionID, approvalCreatedAuditData); err != nil {
			server.mu.Lock()
			delete(server.approvals, approvalID)
			server.mu.Unlock()
			return CapabilityResponse{
				RequestID:    capabilityRequest.RequestID,
				Status:       ResponseStatusError,
				DenialReason: "control-plane audit is unavailable",
				DenialCode:   DenialCodeAuditUnavailable,
			}
		}

		metadata["approval_reason"] = policyDecision.Reason
		metadata["approval_expires_at_utc"] = expiresAt.Format(time.RFC3339Nano)
		metadata["approval_decision_nonce"] = decisionNonce
		// Include the manifest SHA256 in the response so the operator UI can display and
		// submit it back with the decision, binding the decision to the exact approved action.
		metadata["approval_manifest_sha256"] = manifestSHA256
		pendingResponse := CapabilityResponse{
			RequestID:              capabilityRequest.RequestID,
			Status:                 ResponseStatusPendingApproval,
			DenialCode:             DenialCodeApprovalRequired,
			ApprovalRequired:       true,
			ApprovalRequestID:      approvalID,
			ApprovalManifestSHA256: manifestSHA256,
			Metadata:               metadata,
		}
		server.emitUIApprovalPending(pendingApproval{
			ID:               approvalID,
			Request:          cloneCapabilityRequest(capabilityRequest),
			ExpiresAt:        server.now().UTC().Add(approvalTTL),
			Metadata:         metadata,
			Reason:           policyDecision.Reason,
			ControlSessionID: tokenClaims.ControlSessionID,
		})
		return pendingResponse
	}

	effectiveTokenClaims := tokenClaims
	if isHighRiskCapability(tool, policyDecision) && !tokenClaims.SingleUse {
		effectiveTokenClaims = deriveExecutionToken(tokenClaims, capabilityRequest)
	}
	if denialResponse, denied := server.consumeExecutionToken(effectiveTokenClaims, capabilityRequest); denied {
		if err := server.logEvent("capability.denied", effectiveTokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"reason":               secrets.RedactText(denialResponse.DenialReason),
			"denial_code":          denialResponse.DenialCode,
			"actor_label":          effectiveTokenClaims.ActorLabel,
			"client_session_label": effectiveTokenClaims.ClientSessionLabel,
			"control_session_id":   effectiveTokenClaims.ControlSessionID,
			"token_id":             effectiveTokenClaims.TokenID,
			"parent_token_id":      effectiveTokenClaims.ParentTokenID,
		}); err != nil {
			return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
		}
		server.emitUIToolDenied(effectiveTokenClaims.ControlSessionID, capabilityRequest, denialResponse.DenialCode, denialResponse.DenialReason)
		return denialResponse
	}

	// Per-session rate limiting for fs_read.
	if capabilityRequest.Capability == "fs_read" {
		if denied := server.checkFsReadRateLimit(effectiveTokenClaims.ControlSessionID); denied {
			if auditErr := server.logEvent("capability.denied", effectiveTokenClaims.ControlSessionID, map[string]interface{}{
				"request_id":           capabilityRequest.RequestID,
				"capability":           capabilityRequest.Capability,
				"reason":               "fs_read rate limit exceeded",
				"denial_code":          DenialCodeFsReadSizeLimitExceeded,
				"actor_label":          effectiveTokenClaims.ActorLabel,
				"client_session_label": effectiveTokenClaims.ClientSessionLabel,
				"control_session_id":   effectiveTokenClaims.ControlSessionID,
			}); auditErr != nil {
				return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
			}
			return CapabilityResponse{
				RequestID:    capabilityRequest.RequestID,
				Status:       ResponseStatusDenied,
				DenialReason: "fs_read rate limit exceeded",
				DenialCode:   DenialCodeFsReadSizeLimitExceeded,
			}
		}
	}

	if capabilityRequest.Capability == "memory.remember" {
		return server.executeMemoryRememberCapability(effectiveTokenClaims, capabilityRequest)
	}
	if capabilityRequest.Capability == "todo.add" {
		return server.executeTodoAddCapability(effectiveTokenClaims, capabilityRequest)
	}
	if capabilityRequest.Capability == "todo.complete" {
		return server.executeTodoCompleteCapability(effectiveTokenClaims, capabilityRequest)
	}
	if capabilityRequest.Capability == "todo.list" {
		return server.executeTodoListCapability(effectiveTokenClaims, capabilityRequest)
	}
	if capabilityRequest.Capability == "goal.set" {
		return server.executeGoalSetCapability(effectiveTokenClaims, capabilityRequest)
	}
	if capabilityRequest.Capability == "goal.close" {
		return server.executeGoalCloseCapability(effectiveTokenClaims, capabilityRequest)
	}
	if capabilityRequest.Capability == "host.folder.list" {
		return server.executeHostFolderListCapability(effectiveTokenClaims, capabilityRequest)
	}
	if capabilityRequest.Capability == "host.folder.read" {
		return server.executeHostFolderReadCapability(effectiveTokenClaims, capabilityRequest)
	}
	if capabilityRequest.Capability == "host.organize.plan" {
		return server.executeHostOrganizePlanCapability(effectiveTokenClaims, capabilityRequest)
	}
	if capabilityRequest.Capability == "host.plan.apply" {
		return server.executeHostPlanApplyCapability(effectiveTokenClaims, capabilityRequest)
	}

	executionContext := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		executionContext, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	output, err := tool.Execute(executionContext, capabilityRequest.Arguments)
	if err != nil {
		if auditErr := server.logEvent("capability.error", effectiveTokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"error":                secrets.RedactText(err.Error()),
			"operator_error_class": secrets.LoopgateOperatorErrorClass(err),
			"actor_label":          effectiveTokenClaims.ActorLabel,
			"client_session_label": effectiveTokenClaims.ClientSessionLabel,
			"control_session_id":   effectiveTokenClaims.ControlSessionID,
			"token_id":             effectiveTokenClaims.TokenID,
			"parent_token_id":      effectiveTokenClaims.ParentTokenID,
		}); auditErr != nil {
			return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
		}
		errorResponse := CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeExecutionFailed,
			Redacted:     true,
		}
		server.emitUIEvent(effectiveTokenClaims.ControlSessionID, UIEventTypeWarning, UIEventWarning{
			Message: "capability execution failed: " + secrets.RedactText(err.Error()),
		})
		return errorResponse
	}

	var quarantineRef string
	if _, configuredCapability := server.configuredCapabilities[capabilityRequest.Capability]; configuredCapability {
		quarantineRef, err = server.storeQuarantinedPayload(capabilityRequest, output)
		if err != nil {
			qErr := fmt.Errorf("quarantine persistence failed: %w", err)
			if auditErr := server.logEvent("capability.error", effectiveTokenClaims.ControlSessionID, map[string]interface{}{
				"request_id":           capabilityRequest.RequestID,
				"capability":           capabilityRequest.Capability,
				"error":                secrets.RedactText(qErr.Error()),
				"operator_error_class": secrets.LoopgateOperatorErrorClass(qErr),
				"actor_label":          effectiveTokenClaims.ActorLabel,
				"client_session_label": effectiveTokenClaims.ClientSessionLabel,
				"control_session_id":   effectiveTokenClaims.ControlSessionID,
				"token_id":             effectiveTokenClaims.TokenID,
				"parent_token_id":      effectiveTokenClaims.ParentTokenID,
			}); auditErr != nil {
				return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
			}
			server.emitUIEvent(effectiveTokenClaims.ControlSessionID, UIEventTypeWarning, UIEventWarning{
				Message: "capability quarantine persistence failed",
			})
			return CapabilityResponse{
				RequestID:    capabilityRequest.RequestID,
				Status:       ResponseStatusError,
				DenialReason: "capability quarantine persistence failed",
				DenialCode:   DenialCodeExecutionFailed,
				Redacted:     true,
			}
		}
	}

	structuredResult, fieldsMeta, classification, builtQuarantineRef, err := server.buildCapabilityResult(capabilityRequest, output, quarantineRef)
	if err != nil {
		return CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeExecutionFailed,
			Redacted:     true,
		}
	}
	if quarantineRef == "" && classification.Quarantined() {
		quarantineRef, err = server.storeQuarantinedPayload(capabilityRequest, output)
		if err != nil {
			qErr := fmt.Errorf("quarantine persistence failed: %w", err)
			if auditErr := server.logEvent("capability.error", effectiveTokenClaims.ControlSessionID, map[string]interface{}{
				"request_id":           capabilityRequest.RequestID,
				"capability":           capabilityRequest.Capability,
				"error":                secrets.RedactText(qErr.Error()),
				"operator_error_class": secrets.LoopgateOperatorErrorClass(qErr),
				"actor_label":          effectiveTokenClaims.ActorLabel,
				"client_session_label": effectiveTokenClaims.ClientSessionLabel,
				"control_session_id":   effectiveTokenClaims.ControlSessionID,
				"token_id":             effectiveTokenClaims.TokenID,
				"parent_token_id":      effectiveTokenClaims.ParentTokenID,
			}); auditErr != nil {
				return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
			}
			server.emitUIEvent(effectiveTokenClaims.ControlSessionID, UIEventTypeWarning, UIEventWarning{
				Message: "capability quarantine persistence failed",
			})
			return CapabilityResponse{
				RequestID:    capabilityRequest.RequestID,
				Status:       ResponseStatusError,
				DenialReason: "capability quarantine persistence failed",
				DenialCode:   DenialCodeExecutionFailed,
				Redacted:     true,
			}
		}
	}
	if strings.TrimSpace(builtQuarantineRef) != "" {
		quarantineRef = builtQuarantineRef
	}
	classification, err = normalizeResultClassification(classification, quarantineRef)
	if err != nil {
		return CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusError,
			DenialReason: "capability result classification is invalid",
			DenialCode:   DenialCodeExecutionFailed,
			Redacted:     true,
		}
	}
	resultMetadata := server.capabilityProvenanceMetadata(capabilityRequest.Capability, quarantineRef)
	if resultMetadata == nil {
		resultMetadata = make(map[string]interface{})
	}
	resultMetadata["prompt_eligible"] = classification.PromptEligible()
	resultMetadata["memory_eligible"] = classification.MemoryEligible()
	resultMetadata["display_only"] = classification.DisplayOnly()
	resultMetadata["audit_only"] = classification.AuditOnly()
	resultMetadata["quarantined"] = classification.Quarantined()
	if err := server.logEvent("capability.executed", effectiveTokenClaims.ControlSessionID, map[string]interface{}{
		"request_id":            capabilityRequest.RequestID,
		"capability":            capabilityRequest.Capability,
		"status":                ResponseStatusSuccess,
		"result_classification": classification,
		"result_provenance":     resultMetadata,
		"quarantine_ref":        quarantineRef,
		"actor_label":           effectiveTokenClaims.ActorLabel,
		"client_session_label":  effectiveTokenClaims.ClientSessionLabel,
		"control_session_id":    effectiveTokenClaims.ControlSessionID,
		"token_id":              effectiveTokenClaims.TokenID,
		"parent_token_id":       effectiveTokenClaims.ParentTokenID,
	}); err != nil {
		return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
	}
	successResponse := CapabilityResponse{
		RequestID:        capabilityRequest.RequestID,
		Status:           ResponseStatusSuccess,
		StructuredResult: structuredResult,
		FieldsMeta:       fieldsMeta,
		Classification:   classification,
		QuarantineRef:    quarantineRef,
		Metadata:         resultMetadata,
	}
	if !classification.AuditOnly() {
		server.emitUIToolResult(effectiveTokenClaims.ControlSessionID, capabilityRequest, successResponse)
	}
	return successResponse
}

func (server *Server) authenticate(writer http.ResponseWriter, request *http.Request) (capabilityToken, bool) {
	requestPeerIdentity, ok := peerIdentityFromContext(request.Context())
	if !ok {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "missing authenticated peer identity",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return capabilityToken{}, false
	}

	authorizationHeader := strings.TrimSpace(request.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(authorizationHeader), "bearer ") {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "missing capability token",
			DenialCode:   DenialCodeCapabilityTokenMissing,
		})
		return capabilityToken{}, false
	}

	tokenString := strings.TrimSpace(authorizationHeader[len("Bearer "):])

	// Take a consistent now snapshot and perform all expiry checks inside a
	// single lock acquisition to eliminate the TOCTOU window between reading
	// token/session state and calling now() on the outside.
	server.mu.Lock()
	nowUTC := server.now().UTC()
	tokenClaims, found := server.tokens[tokenString]
	var activeSession controlSession
	var sessionFound bool
	var tokenExpired, sessionExpired bool
	if found {
		activeSession, sessionFound = server.sessions[tokenClaims.ControlSessionID]
		tokenExpired = nowUTC.After(tokenClaims.ExpiresAt)
		sessionExpired = sessionFound && nowUTC.After(activeSession.ExpiresAt)
		if tokenExpired {
			delete(server.tokens, tokenString)
		}
		if sessionExpired {
			delete(server.sessions, tokenClaims.ControlSessionID)
			delete(server.tokens, tokenString)
		}
	}
	server.mu.Unlock()

	if !found {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "invalid capability token",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return capabilityToken{}, false
	}
	if tokenExpired {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "expired capability token",
			DenialCode:   DenialCodeCapabilityTokenExpired,
		})
		return capabilityToken{}, false
	}
	if sessionExpired {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "expired capability token",
			DenialCode:   DenialCodeCapabilityTokenExpired,
		})
		return capabilityToken{}, false
	}
	if !sessionFound {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "invalid capability token",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return capabilityToken{}, false
	}
	if tokenClaims.PeerIdentity != requestPeerIdentity {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "capability token peer binding mismatch",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return capabilityToken{}, false
	}

	return tokenClaims, true
}

// capabilityTokenForMorphlingApprovalFinalize builds a token for finalizeSpawnedMorphling after UI/API approval.
// It prefers the live control session (peer binding, expiry, tenancy) so execution matches the session that
// is still open when the operator approves; ExecutionContext carries a snapshot if the session is gone.
func (server *Server) capabilityTokenForMorphlingApprovalFinalize(pending pendingApproval) capabilityToken {
	server.mu.Lock()
	session, sessionFound := server.sessions[pending.ControlSessionID]
	server.mu.Unlock()
	token := capabilityToken{
		ControlSessionID:    pending.ExecutionContext.ControlSessionID,
		ActorLabel:          pending.ExecutionContext.ActorLabel,
		ClientSessionLabel:  pending.ExecutionContext.ClientSessionLabel,
		AllowedCapabilities: copyCapabilitySet(pending.ExecutionContext.AllowedCapabilities),
		TenantID:            pending.ExecutionContext.TenantID,
		UserID:              pending.ExecutionContext.UserID,
		ExpiresAt:           pending.ExpiresAt,
	}
	if sessionFound {
		token.PeerIdentity = session.PeerIdentity
		token.TenantID = session.TenantID
		token.UserID = session.UserID
		token.ExpiresAt = session.ExpiresAt
	}
	return token
}

func approvalTokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func (server *Server) authenticateApproval(writer http.ResponseWriter, request *http.Request) (controlSession, bool) {
	requestPeerIdentity, ok := peerIdentityFromContext(request.Context())
	if !ok {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "missing authenticated peer identity",
			DenialCode:   DenialCodeApprovalTokenInvalid,
		})
		return controlSession{}, false
	}

	approvalToken := strings.TrimSpace(request.Header.Get("X-Loopgate-Approval-Token"))
	if approvalToken == "" {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "missing approval token",
			DenialCode:   DenialCodeApprovalTokenMissing,
		})
		return controlSession{}, false
	}

	tokenHash := approvalTokenHash(approvalToken)

	server.mu.Lock()
	server.pruneExpiredLocked()
	controlSessionID, indexed := server.approvalTokenIndex[tokenHash]
	if !indexed {
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "invalid approval token",
			DenialCode:   DenialCodeApprovalTokenInvalid,
		})
		return controlSession{}, false
	}
	activeSession, sessionExists := server.sessions[controlSessionID]
	if !sessionExists {
		delete(server.approvalTokenIndex, tokenHash)
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "invalid approval token",
			DenialCode:   DenialCodeApprovalTokenInvalid,
		})
		return controlSession{}, false
	}

	// Constant-time comparison to prevent timing oracle on the raw token value.
	if subtle.ConstantTimeCompare([]byte(activeSession.ApprovalToken), []byte(approvalToken)) != 1 {
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "invalid approval token",
			DenialCode:   DenialCodeApprovalTokenInvalid,
		})
		return controlSession{}, false
	}

	if server.now().UTC().After(activeSession.ExpiresAt) {
		delete(server.sessions, controlSessionID)
		delete(server.approvalTokenIndex, tokenHash)
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "expired approval token",
			DenialCode:   DenialCodeApprovalTokenExpired,
		})
		return controlSession{}, false
	}
	if activeSession.PeerIdentity != requestPeerIdentity {
		server.mu.Unlock()
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "approval token peer binding mismatch",
			DenialCode:   DenialCodeApprovalTokenInvalid,
		})
		return controlSession{}, false
	}
	server.mu.Unlock()
	return activeSession, true
}

func (server *Server) capabilitySummaries() []CapabilitySummary {
	registeredTools := server.registry.All()
	capabilities := make([]CapabilitySummary, 0, len(registeredTools))
	for _, registeredTool := range registeredTools {
		capabilities = append(capabilities, CapabilitySummary{
			Name:        registeredTool.Name(),
			Category:    registeredTool.Category(),
			Operation:   registeredTool.Operation(),
			Description: registeredTool.Schema().Description,
		})
	}
	return capabilities
}

// filterGrantedCapabilities intersects client-requested capabilities with
// server-registered capabilities. Returns the granted list and any unknown names.
//
// Security invariant: capability scope is server-authoritative. The client's
// requested list is treated as a request, not as an authoritative grant.
// Only capabilities registered in the tool registry are granted.
func (server *Server) filterGrantedCapabilities(requested []string) (granted []string, unknown []string) {
	for _, capName := range requested {
		if server.recognizesCapability(capName) {
			granted = append(granted, capName)
		} else {
			unknown = append(unknown, capName)
		}
	}
	return granted, unknown
}

func (server *Server) writeJSON(writer http.ResponseWriter, statusCode int, payload interface{}) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	if err := encodeJSONResponse(writer, payload); err != nil && server.reportResponseWriteError != nil {
		server.reportResponseWriteError(statusCode, err)
	}
}

func encodeJSONResponse(writer http.ResponseWriter, payload interface{}) error {
	return json.NewEncoder(writer).Encode(payload)
}

func auditUnavailableCapabilityResponse(requestID string) CapabilityResponse {
	return CapabilityResponse{
		RequestID:    requestID,
		Status:       ResponseStatusError,
		DenialReason: "control-plane audit is unavailable",
		DenialCode:   DenialCodeAuditUnavailable,
	}
}

// mergeAuditTenancyFromControlSession stamps tenant_id/user_id when absent. Call this before
// logEvent from code that already holds server.mu — tenantUserForControlSession must not run
// under the same goroutine while server.mu is held (sync.Mutex is not reentrant).
func mergeAuditTenancyFromControlSession(auditData map[string]interface{}, session controlSession) {
	if auditData == nil {
		return
	}
	if _, exists := auditData["tenant_id"]; !exists {
		auditData["tenant_id"] = session.TenantID
	}
	if _, exists := auditData["user_id"]; !exists {
		auditData["user_id"] = session.UserID
	}
}

// tenantUserForControlSession returns tenancy fields for audit and diagnostic enrichment.
// It acquires server.mu without holding auditMu so logEvent stays free of lock-order inversions.
func (server *Server) tenantUserForControlSession(controlSessionID string) (tenantID string, userID string) {
	if strings.TrimSpace(controlSessionID) == "" {
		return "", ""
	}
	server.mu.Lock()
	session, found := server.sessions[controlSessionID]
	server.mu.Unlock()
	if !found {
		return "", ""
	}
	return session.TenantID, session.UserID
}

func (server *Server) logEvent(eventType string, sessionID string, data map[string]interface{}) error {
	safeData := copyInterfaceMap(data)
	_, hasTenant := safeData["tenant_id"]
	_, hasUser := safeData["user_id"]
	if !hasTenant || !hasUser {
		lookupTenantID, lookupUserID := server.tenantUserForControlSession(sessionID)
		if !hasTenant {
			safeData["tenant_id"] = lookupTenantID
		}
		if !hasUser {
			safeData["user_id"] = lookupUserID
		}
	}

	// auditMu is held for the full duration including the disk write.
	// This is intentional: the hash-chain requires that sequence numbers and
	// previous-event hashes are assigned, written, and committed atomically.
	// Splitting the lock would require a rollback protocol and creates
	// new failure modes. Acceptable because Loopgate is single-client and
	// all capability paths are request-driven (not concurrent hot paths).
	server.auditMu.Lock()
	defer server.auditMu.Unlock()

	nextSequence := server.auditSequence + 1
	safeData["audit_sequence"] = nextSequence
	// The shared ledger append path always assigns ledger_sequence before
	// hashing/writing the event. Keep the precomputed audit hash aligned with the
	// final stored bytes by setting the mirrored sequence value up front.
	safeData["ledger_sequence"] = nextSequence
	safeData["previous_event_hash"] = server.lastAuditHash
	canonicalData, err := canonicalizeAuditData(safeData)
	if err != nil {
		return fmt.Errorf("canonicalize audit event data: %w", err)
	}
	safeData = canonicalData

	auditEvent := ledger.Event{
		TS:      server.now().UTC().Format(time.RFC3339Nano),
		Type:    eventType,
		Session: sessionID,
		Data:    safeData,
	}
	eventHash, err := hashAuditEvent(auditEvent)
	if err != nil {
		return fmt.Errorf("hash audit event: %w", err)
	}
	auditEvent.Data["event_hash"] = eventHash

	if err := audit.NewLedgerWriter(server.appendAuditEvent, nil).Record(server.auditPath, audit.ClassMustPersist, auditEvent); err != nil {
		return err
	}
	server.auditSequence = nextSequence
	server.lastAuditHash = eventHash
	server.diagnosticTextAfterAuditEvent(auditEvent)
	return nil
}

func (server *Server) diagnosticTextAfterAuditEvent(auditEvent ledger.Event) {
	if server.diagnostic == nil {
		return
	}
	hashPrefix := ""
	if auditEvent.Data != nil {
		if h, ok := auditEvent.Data["event_hash"].(string); ok && h != "" {
			hashPrefix = h
			if len(hashPrefix) > 16 {
				hashPrefix = hashPrefix[:16]
			}
		}
	}
	var seq any
	if auditEvent.Data != nil {
		seq = auditEvent.Data["audit_sequence"]
	}
	diagTenantID, diagUserID := "", ""
	if auditEvent.Data != nil {
		if v, ok := auditEvent.Data["tenant_id"].(string); ok {
			diagTenantID = v
		}
		if v, ok := auditEvent.Data["user_id"].(string); ok {
			diagUserID = v
		}
	}
	if server.diagnostic.Audit != nil {
		server.diagnostic.Audit.Debug("audit_persisted",
			"type", auditEvent.Type,
			"session", auditEvent.Session,
			"tenant_id", diagTenantID,
			"user_id", diagUserID,
			"audit_sequence", seq,
			"event_hash_prefix", hashPrefix,
		)
	}
	if server.diagnostic.Ledger != nil {
		server.diagnostic.Ledger.Debug("ledger_append",
			"type", auditEvent.Type,
			"session", auditEvent.Session,
			"tenant_id", diagTenantID,
			"user_id", diagUserID,
			"audit_sequence", seq,
			"event_hash_prefix", hashPrefix,
		)
	}
	if strings.HasPrefix(auditEvent.Type, "memory.") && server.diagnostic.Memory != nil {
		server.diagnostic.Memory.Debug("memory_audit",
			"type", auditEvent.Type,
			"session", auditEvent.Session,
			"tenant_id", diagTenantID,
			"user_id", diagUserID,
			"audit_sequence", seq,
		)
	}
	server.diagnosticServerControlPlaneFromAuditEvent(auditEvent)
	server.diagnosticModelFromAuditEvent(auditEvent)
}

// CloseDiagnosticLogs closes optional text log files. Safe to call multiple times.
func (server *Server) CloseDiagnosticLogs() {
	if server == nil || server.diagnostic == nil {
		return
	}
	_ = server.diagnostic.Close()
	server.diagnostic = nil
}

// DiagnosticLogDirectoryMessage returns a stderr hint when operator diagnostic slog files are active.
// Those files (server.log, socket.log, client.log, …) are separate from shell-redirected stdout such as runtime/logs/loopgate.log.
func (server *Server) DiagnosticLogDirectoryMessage() string {
	if server == nil || server.diagnostic == nil {
		return ""
	}
	rel := server.runtimeConfig.Logging.Diagnostic.ResolvedDirectory()
	dir := filepath.Join(server.repoRoot, rel)
	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = dir
	}
	return fmt.Sprintf("Loopgate diagnostic slog files: %s (server.log, socket.log, client.log, …). "+
		"runtime/logs/loopgate.log is only start.sh stdout/stderr, not these.", absDir)
}

func buildApprovalGrantedAuditData(approvalID string, pendingApproval pendingApproval) map[string]interface{} {
	auditData := map[string]interface{}{
		"approval_request_id":  approvalID,
		"capability":           pendingApproval.Request.Capability,
		"approval_class":       pendingApproval.Metadata["approval_class"],
		"approval_state":       approvalStateConsumed,
		"control_session_id":   pendingApproval.ControlSessionID,
		"actor_label":          pendingApproval.ExecutionContext.ActorLabel,
		"client_session_label": pendingApproval.ExecutionContext.ClientSessionLabel,
	}
	if approvalClass, ok := pendingApproval.Metadata["approval_class"].(string); ok && strings.TrimSpace(approvalClass) != "" {
		auditData["approval_class"] = approvalClass
	}
	return auditData
}

func buildApprovalOperatorDeniedAuditData(approvalID string, pendingApproval pendingApproval) map[string]interface{} {
	auditData := map[string]interface{}{
		"approval_request_id":  approvalID,
		"capability":           pendingApproval.Request.Capability,
		"approval_class":       pendingApproval.Metadata["approval_class"],
		"approval_state":       approvalStateDenied,
		"control_session_id":   pendingApproval.ControlSessionID,
		"actor_label":          pendingApproval.ExecutionContext.ActorLabel,
		"client_session_label": pendingApproval.ExecutionContext.ClientSessionLabel,
	}
	if approvalClass, ok := pendingApproval.Metadata["approval_class"].(string); ok && strings.TrimSpace(approvalClass) != "" {
		auditData["approval_class"] = approvalClass
	}
	return auditData
}

// validatePendingApprovalDecisionLocked performs session, expiry, nonce, and manifest checks
// for a pending approval without writing audit events or changing approval state.
// Must be called with server.mu held.
func (server *Server) validatePendingApprovalDecisionLocked(controlSession controlSession, approvalID string, decisionRequest ApprovalDecisionRequest) (pendingApproval, CapabilityResponse, bool) {
	server.pruneExpiredLocked()
	pendingApproval, found := server.approvals[approvalID]
	if !found {
		return pendingApproval, CapabilityResponse{
			RequestID:    approvalID,
			Status:       ResponseStatusDenied,
			DenialReason: "approval request not found",
			DenialCode:   DenialCodeApprovalNotFound,
		}, false
	}

	if pendingApproval.ExpiresAt.Before(server.now().UTC()) {
		pendingApproval.State = approvalStateExpired
		server.approvals[approvalID] = pendingApproval
		return pendingApproval, CapabilityResponse{
			RequestID:         pendingApproval.Request.RequestID,
			Status:            ResponseStatusDenied,
			DenialReason:      "approval request expired",
			DenialCode:        DenialCodeApprovalDenied,
			ApprovalRequestID: approvalID,
		}, false
	}

	if controlSession.ID != pendingApproval.ControlSessionID {
		return pendingApproval, CapabilityResponse{
			RequestID:         pendingApproval.Request.RequestID,
			Status:            ResponseStatusDenied,
			DenialReason:      "approval token does not match approval owner",
			DenialCode:        DenialCodeApprovalOwnerMismatch,
			ApprovalRequestID: approvalID,
		}, false
	}
	pendingApproval = backfillApprovalManifestLocked(server.approvals, approvalID, pendingApproval)

	if pendingApproval.State != approvalStatePending {
		// A consumed or execution-failed state means a concurrent decision already won the race
		// and execution has begun or completed. Use DenialCodeApprovalStateConflict to
		// distinguish this from a genuine state violation (expired, denied, cancelled).
		denialCode := DenialCodeApprovalStateInvalid
		if pendingApproval.State == approvalStateConsumed || pendingApproval.State == approvalStateExecutionFailed {
			denialCode = DenialCodeApprovalStateConflict
		}
		return pendingApproval, CapabilityResponse{
			RequestID:         pendingApproval.Request.RequestID,
			Status:            ResponseStatusDenied,
			DenialReason:      "approval request is no longer pending",
			DenialCode:        denialCode,
			ApprovalRequestID: approvalID,
		}, false
	}

	decisionNonce := strings.TrimSpace(decisionRequest.DecisionNonce)
	if decisionNonce == "" {
		return pendingApproval, CapabilityResponse{
			RequestID:         pendingApproval.Request.RequestID,
			Status:            ResponseStatusDenied,
			DenialReason:      "approval decision nonce is required",
			DenialCode:        DenialCodeApprovalDecisionNonceMissing,
			ApprovalRequestID: approvalID,
		}, false
	}
	if decisionNonce != pendingApproval.DecisionNonce {
		return pendingApproval, CapabilityResponse{
			RequestID:         pendingApproval.Request.RequestID,
			Status:            ResponseStatusDenied,
			DenialReason:      "approval decision nonce is invalid",
			DenialCode:        DenialCodeApprovalDecisionNonceInvalid,
			ApprovalRequestID: approvalID,
		}, false
	}

	// When an approval decision is submitted with a manifest SHA256, verify it matches the
	// server-computed manifest. This binds the decision to the exact action class, subject,
	// execution method, path, and request body that was approved, preventing an operator
	// decision from being accepted for a different action than the one displayed to them.
	// Per AMP RFC 0005 §6, manifest verification applies to approvals (not denials), since
	// the manifest proves the operator reviewed the action they are authorizing.
	submittedManifest := strings.TrimSpace(decisionRequest.ApprovalManifestSHA256)
	if decisionRequest.Approved && pendingApproval.ApprovalManifestSHA256 != "" {
		if submittedManifest == "" {
			return pendingApproval, CapabilityResponse{
				RequestID:         pendingApproval.Request.RequestID,
				Status:            ResponseStatusDenied,
				DenialReason:      "approval manifest sha256 is required for this approval",
				DenialCode:        DenialCodeApprovalManifestMismatch,
				ApprovalRequestID: approvalID,
			}, false
		}
		if submittedManifest != pendingApproval.ApprovalManifestSHA256 {
			return pendingApproval, CapabilityResponse{
				RequestID:         pendingApproval.Request.RequestID,
				Status:            ResponseStatusDenied,
				DenialReason:      "approval manifest sha256 does not match the pending approval",
				DenialCode:        DenialCodeApprovalManifestMismatch,
				ApprovalRequestID: approvalID,
			}, false
		}
	}

	return pendingApproval, CapabilityResponse{}, true
}

// validatePendingApprovalDecision is the lock-acquiring wrapper for validatePendingApprovalDecisionLocked.
func (server *Server) validatePendingApprovalDecision(controlSession controlSession, approvalID string, decisionRequest ApprovalDecisionRequest) (pendingApproval, CapabilityResponse, bool) {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.validatePendingApprovalDecisionLocked(controlSession, approvalID, decisionRequest)
}

// commitApprovalGrantConsumed appends approval.granted and transitions the approval to consumed.
// Call after approval-scoped work succeeds (e.g. morphling spawn finalization) so a spawn failure
// does not leave a consumed approval with no morphling. If audit append fails after side effects,
// the operator may need manual recovery (rare).
func (server *Server) commitApprovalGrantConsumed(approvalID string, expectedDecisionNonce string) error {
	expectedDecisionNonce = strings.TrimSpace(expectedDecisionNonce)
	server.mu.Lock()
	defer server.mu.Unlock()

	pendingApproval, found := server.approvals[approvalID]
	if !found {
		return fmt.Errorf("approval request not found")
	}
	if pendingApproval.State != approvalStatePending {
		return fmt.Errorf("approval request is no longer pending")
	}
	if expectedDecisionNonce == "" || pendingApproval.DecisionNonce != expectedDecisionNonce {
		return fmt.Errorf("approval decision nonce mismatch")
	}

	grantAuditData := buildApprovalGrantedAuditData(approvalID, pendingApproval)
	if session, ok := server.sessions[pendingApproval.ControlSessionID]; ok {
		mergeAuditTenancyFromControlSession(grantAuditData, session)
	}
	if err := server.logEvent("approval.granted", pendingApproval.ControlSessionID, grantAuditData); err != nil {
		return err
	}
	pendingApproval.State = approvalStateConsumed
	pendingApproval.DecisionSubmittedAt = server.now().UTC()
	pendingApproval.DecisionNonce = ""
	server.approvals[approvalID] = pendingApproval
	return nil
}

func (server *Server) validateAndRecordApprovalDecision(controlSession controlSession, approvalID string, decisionRequest ApprovalDecisionRequest) (pendingApproval, CapabilityResponse, bool) {
	server.mu.Lock()
	defer server.mu.Unlock()

	pendingApproval, denialResponse, ok := server.validatePendingApprovalDecisionLocked(controlSession, approvalID, decisionRequest)
	if !ok {
		return pendingApproval, denialResponse, false
	}

	// Persist operator grant/deny to the hash-chained audit ledger before mutating approval
	// state. Otherwise a failed audit append leaves the approval consumed or denied with no
	// matching approval.granted / approval.denied record (audit observability invariant).
	if decisionRequest.Approved {
		grantAuditData := buildApprovalGrantedAuditData(approvalID, pendingApproval)
		mergeAuditTenancyFromControlSession(grantAuditData, controlSession)
		if err := server.logEvent("approval.granted", pendingApproval.ControlSessionID, grantAuditData); err != nil {
			return pendingApproval, CapabilityResponse{
				RequestID:         pendingApproval.Request.RequestID,
				Status:            ResponseStatusError,
				DenialReason:      "control-plane audit is unavailable",
				DenialCode:        DenialCodeAuditUnavailable,
				ApprovalRequestID: approvalID,
			}, false
		}
		// Transition directly to consumed, not an intermediate "approved" state. This makes
		// the decision and consumption atomic: a concurrent decision finds state != pending
		// and is rejected with DenialCodeApprovalStateConflict before execution can begin.
		pendingApproval.State = approvalStateConsumed
	} else {
		deniedAuditData := buildApprovalOperatorDeniedAuditData(approvalID, pendingApproval)
		mergeAuditTenancyFromControlSession(deniedAuditData, controlSession)
		if err := server.logEvent("approval.denied", pendingApproval.ControlSessionID, deniedAuditData); err != nil {
			return pendingApproval, CapabilityResponse{
				RequestID:         pendingApproval.Request.RequestID,
				Status:            ResponseStatusError,
				DenialReason:      "control-plane audit is unavailable",
				DenialCode:        DenialCodeAuditUnavailable,
				ApprovalRequestID: approvalID,
			}, false
		}
		pendingApproval.State = approvalStateDenied
	}
	pendingApproval.DecisionSubmittedAt = server.now().UTC()
	pendingApproval.DecisionNonce = ""
	server.approvals[approvalID] = pendingApproval
	return pendingApproval, CapabilityResponse{}, true
}

// backfillApprovalManifestLocked lazily computes and stores the approval manifest for any
// in-flight approval that was created before the manifest-binding change was deployed.
// Must be called with server.mu held.
func backfillApprovalManifestLocked(approvalRecords map[string]pendingApproval, approvalID string, approval pendingApproval) pendingApproval {
	if strings.TrimSpace(approval.ApprovalManifestSHA256) != "" {
		return approval
	}
	if strings.TrimSpace(approval.Request.Capability) == "" || approval.ExpiresAt.IsZero() {
		return approval
	}
	manifestSHA256, bodySHA256, err := buildCapabilityApprovalManifest(approval.Request, approval.ExpiresAt.UTC().UnixMilli())
	if err != nil {
		return approval
	}
	approval.ApprovalManifestSHA256 = manifestSHA256
	if strings.TrimSpace(approval.ExecutionBodySHA256) == "" {
		approval.ExecutionBodySHA256 = bodySHA256
	}
	approvalRecords[approvalID] = approval
	return approval
}

func (server *Server) markApprovalExecutionResult(approvalID string, executionStatus string) {
	server.mu.Lock()
	defer server.mu.Unlock()

	pendingApproval, found := server.approvals[approvalID]
	if !found {
		return
	}
	// The approval is already in approvalStateConsumed from the atomic decision transition.
	// Only update state on failure so the audit record reflects the execution outcome.
	if executionStatus != ResponseStatusSuccess {
		pendingApproval.State = approvalStateExecutionFailed
	}
	pendingApproval.ExecutedAt = server.now().UTC()
	server.approvals[approvalID] = pendingApproval
}

func (server *Server) approvalMetadata(capabilityRequest CapabilityRequest) map[string]interface{} {
	metadata := map[string]interface{}{
		"capability": capabilityRequest.Capability,
	}
	if approvalClass := server.approvalClassForCapability(capabilityRequest.Capability); approvalClass != "" {
		metadata["approval_class"] = approvalClass
	}
	if pathValue := strings.TrimSpace(capabilityRequest.Arguments["path"]); pathValue != "" {
		metadata["path"] = pathValue
	}
	if contentValue, hasContent := capabilityRequest.Arguments["content"]; hasContent {
		metadata["content_bytes"] = len(contentValue)
	}
	redactedArguments := secrets.RedactStringMap(capabilityRequest.Arguments)
	for argumentKey, argumentValue := range redactedArguments {
		if argumentKey == "path" || argumentKey == "content" {
			continue
		}
		metadata["arg_"+argumentKey] = argumentValue
	}
	if capabilityRequest.Capability == "host.plan.apply" {
		for k, v := range server.hostPlanApplyApprovalOperatorFields(capabilityRequest.Arguments["plan_id"]) {
			metadata[k] = v
		}
	}
	return metadata
}

func classifyCapabilityResult(capability string) (ResultClassification, string) {
	switch capability {
	case "fs_list":
		return ResultClassification{
			Exposure: ResultExposureDisplay,
			Eligibility: ResultEligibility{
				Prompt: true,
				Memory: true,
			},
		}, ""
	case "fs_write":
		return ResultClassification{
			Exposure: ResultExposureDisplay,
			Eligibility: ResultEligibility{
				Prompt: true,
				Memory: false,
			},
		}, ""
	case "shell_exec":
		return ResultClassification{
			Exposure: ResultExposureDisplay,
			Eligibility: ResultEligibility{
				Prompt: true,
				Memory: false,
			},
		}, ""
	case "fs_read":
		return ResultClassification{
			Exposure: ResultExposureDisplay,
			Eligibility: ResultEligibility{
				Prompt: true,
				Memory: false,
			},
		}, ""
	case "fs_mkdir":
		return ResultClassification{
			Exposure: ResultExposureDisplay,
			Eligibility: ResultEligibility{
				Prompt: true,
				Memory: false,
			},
		}, ""
	case "journal.read", "journal.write", "journal.list",
		"notes.read", "notes.write", "notes.list",
		"note.create", "paint.save", "paint.list",
		"desktop.organize":
		return ResultClassification{
			Exposure: ResultExposureDisplay,
			Eligibility: ResultEligibility{
				Prompt: true,
				Memory: false,
			},
		}, ""
	default:
		return ResultClassification{
			Exposure: ResultExposureAudit,
			Quarantine: ResultQuarantine{
				Quarantined: true,
			},
		}, ""
	}
}

func buildCapabilityResult(capability string, arguments map[string]string, output string) (map[string]interface{}, map[string]ResultFieldMetadata, ResultClassification, string, error) {
	switch capability {
	case "shell_exec":
		structuredResult := map[string]interface{}{
			"command": arguments["command"],
			"output":  output,
		}
		classification := ResultClassification{
			Exposure: ResultExposureDisplay,
			Eligibility: ResultEligibility{
				Prompt: true,
				Memory: false,
			},
		}
		fieldsMeta, err := fieldsMetadataForStructuredResult(structuredResult, ResultFieldOriginLocal, classification)
		if err != nil {
			return nil, nil, ResultClassification{}, "", err
		}
		return structuredResult, fieldsMeta, classification, "", nil
	case "fs_list":
		structuredResult := map[string]interface{}{
			"path":    arguments["path"],
			"entries": []string{},
		}
		entries := []string{}
		if strings.TrimSpace(output) != "" {
			entries = strings.Split(output, "\n")
		}
		structuredResult["entries"] = entries
		classification := ResultClassification{
			Exposure: ResultExposureDisplay,
			Eligibility: ResultEligibility{
				Prompt: true,
				Memory: true,
			},
		}
		fieldsMeta, err := fieldsMetadataForStructuredResult(structuredResult, ResultFieldOriginLocal, classification)
		if err != nil {
			return nil, nil, ResultClassification{}, "", err
		}
		return structuredResult, fieldsMeta, classification, "", nil
	case "fs_write":
		structuredResult := map[string]interface{}{
			"path":    arguments["path"],
			"bytes":   len(arguments["content"]),
			"message": output,
		}
		classification := ResultClassification{
			Exposure: ResultExposureDisplay,
			Eligibility: ResultEligibility{
				Prompt: true,
				Memory: false,
			},
		}
		fieldsMeta, err := fieldsMetadataForStructuredResult(structuredResult, ResultFieldOriginLocal, classification)
		if err != nil {
			return nil, nil, ResultClassification{}, "", err
		}
		return structuredResult, fieldsMeta, classification, "", nil
	case "fs_read":
		structuredResult := map[string]interface{}{
			"path":    arguments["path"],
			"content": output,
			"bytes":   len(output),
		}
		classification := ResultClassification{
			Exposure: ResultExposureDisplay,
			Eligibility: ResultEligibility{
				Prompt: true,
				Memory: false,
			},
		}
		fieldsMeta, err := fieldsMetadataForStructuredResult(structuredResult, ResultFieldOriginLocal, classification)
		if err != nil {
			return nil, nil, ResultClassification{}, "", err
		}
		return structuredResult, fieldsMeta, classification, "", nil
	default:
		classification, quarantineRef := classifyCapabilityResult(capability)
		if !classification.AuditOnly() {
			structuredResult := map[string]interface{}{
				"output": output,
			}
			fieldsMeta, err := fieldsMetadataForStructuredResult(structuredResult, ResultFieldOriginLocal, classification)
			if err != nil {
				return nil, nil, ResultClassification{}, "", err
			}
			return structuredResult, fieldsMeta, classification, quarantineRef, nil
		}
		return map[string]interface{}{}, nil, classification, quarantineRef, nil
	}
}

func normalizeResultClassification(classification ResultClassification, quarantineRef string) (ResultClassification, error) {
	classification.Quarantine.Ref = strings.TrimSpace(quarantineRef)
	if err := classification.Validate(); err != nil {
		return ResultClassification{}, err
	}
	return classification, nil
}

func fieldsMetadataForStructuredResult(structuredResult map[string]interface{}, fieldOrigin string, classification ResultClassification) (map[string]ResultFieldMetadata, error) {
	fieldsMeta := make(map[string]ResultFieldMetadata, len(structuredResult))
	for fieldName, fieldValue := range structuredResult {
		fieldMetadata, err := buildResultFieldMetadata(fieldValue, fieldOrigin, classification)
		if err != nil {
			return nil, fmt.Errorf("build fields_meta for %q: %w", fieldName, err)
		}
		fieldsMeta[fieldName] = fieldMetadata
	}
	return fieldsMeta, nil
}

func buildResultFieldMetadata(fieldValue interface{}, fieldOrigin string, classification ResultClassification) (ResultFieldMetadata, error) {
	fieldKind, fieldContentType, fieldSizeBytes, err := describeResultFieldValue(fieldValue)
	if err != nil {
		return ResultFieldMetadata{}, err
	}
	fieldMetadata := ResultFieldMetadata{
		Origin:         fieldOrigin,
		ContentType:    fieldContentType,
		Trust:          ResultFieldTrustDeterministic,
		Sensitivity:    sensitivityForResultField(fieldValue),
		SizeBytes:      fieldSizeBytes,
		Kind:           fieldKind,
		ScalarSubclass: scalarSubclassForResultField(fieldValue),
		PromptEligible: classification.PromptEligible(),
		MemoryEligible: classification.MemoryEligible(),
	}
	if err := fieldMetadata.Validate(); err != nil {
		return ResultFieldMetadata{}, err
	}
	return fieldMetadata, nil
}

func describeResultFieldValue(fieldValue interface{}) (string, string, int, error) {
	switch typedFieldValue := fieldValue.(type) {
	case string:
		return ResultFieldKindScalar, "text/plain", len(typedFieldValue), nil
	case bool, float64, float32, int, int64, int32, uint, uint32, uint64:
		encodedFieldBytes, err := json.Marshal(typedFieldValue)
		if err != nil {
			return "", "", 0, fmt.Errorf("encode scalar field: %w", err)
		}
		return ResultFieldKindScalar, "application/json", len(encodedFieldBytes), nil
	case []string, []interface{}:
		encodedFieldBytes, err := json.Marshal(typedFieldValue)
		if err != nil {
			return "", "", 0, fmt.Errorf("encode array field: %w", err)
		}
		return ResultFieldKindArray, "application/json", len(encodedFieldBytes), nil
	case map[string]interface{}:
		encodedFieldBytes, err := json.Marshal(typedFieldValue)
		if err != nil {
			return "", "", 0, fmt.Errorf("encode object field: %w", err)
		}
		return ResultFieldKindObject, "application/json", len(encodedFieldBytes), nil
	default:
		encodedFieldBytes, err := json.Marshal(typedFieldValue)
		if err != nil {
			return "", "", 0, fmt.Errorf("encode structured field: %w", err)
		}
		return ResultFieldKindScalar, "application/json", len(encodedFieldBytes), nil
	}
}

func sensitivityForResultField(fieldValue interface{}) string {
	switch typedFieldValue := fieldValue.(type) {
	case string:
		if strings.TrimSpace(typedFieldValue) == "" {
			return ResultFieldSensitivityBenign
		}
		return ResultFieldSensitivityTaintedText
	case []string:
		if len(typedFieldValue) == 0 {
			return ResultFieldSensitivityBenign
		}
		return ResultFieldSensitivityTaintedText
	case []interface{}:
		if len(typedFieldValue) == 0 {
			return ResultFieldSensitivityBenign
		}
		return ResultFieldSensitivityTaintedText
	default:
		return ResultFieldSensitivityBenign
	}
}

func scalarSubclassForResultField(fieldValue interface{}) string {
	switch typedFieldValue := fieldValue.(type) {
	case bool:
		return ResultFieldScalarSubclassBoolean
	case float64, float32, int, int64, int32, uint, uint32, uint64:
		return ResultFieldScalarSubclassValidatedNumber
	case string:
		if normalizedTimestamp, ok := normalizePromotableTimestamp(typedFieldValue); ok && normalizedTimestamp != "" {
			return ResultFieldScalarSubclassTimestamp
		}
		if identifiers.ValidateSafeIdentifier("result field strict identifier", typedFieldValue) == nil {
			return ResultFieldScalarSubclassStrictIdentifier
		}
		return ResultFieldScalarSubclassShortTextLabel
	default:
		return ""
	}
}

func isHighRiskCapability(tool toolspkg.Tool, policyDecision policypkg.CheckResult) bool {
	if policyDecision.Decision == policypkg.NeedsApproval {
		return true
	}
	if trustedTool, ok := tool.(interface{ TrustedSandboxLocal() bool }); ok && trustedTool.TrustedSandboxLocal() {
		return false
	}
	return tool.Operation() == toolspkg.OpWrite
}

func shouldAutoAllowTrustedSandboxCapability(tokenClaims capabilityToken, tool toolspkg.Tool, policyDecision policypkg.CheckResult) bool {
	if policyDecision.Decision != policypkg.NeedsApproval {
		return false
	}
	if tokenClaims.ActorLabel != "haven" {
		return false
	}
	trustedTool, ok := tool.(interface{ TrustedSandboxLocal() bool })
	return ok && trustedTool.TrustedSandboxLocal()
}

func deriveExecutionToken(baseToken capabilityToken, capabilityRequest CapabilityRequest) capabilityToken {
	derivedTokenID := "exec:" + baseToken.TokenID + ":" + capabilityRequest.RequestID
	return capabilityToken{
		TokenID:             derivedTokenID,
		ControlSessionID:    baseToken.ControlSessionID,
		ActorLabel:          baseToken.ActorLabel,
		ClientSessionLabel:  baseToken.ClientSessionLabel,
		AllowedCapabilities: capabilitySet([]string{capabilityRequest.Capability}),
		PeerIdentity:        baseToken.PeerIdentity,
		TenantID:            baseToken.TenantID,
		UserID:              baseToken.UserID,
		ExpiresAt:           baseToken.ExpiresAt,
		SingleUse:           true,
		BoundCapability:     capabilityRequest.Capability,
		BoundArgumentHash:   normalizedArgumentHash(capabilityRequest.Arguments),
		ParentTokenID:       baseToken.TokenID,
	}
}

func normalizedArgumentHash(arguments map[string]string) string {
	if len(arguments) == 0 {
		return ""
	}
	argumentKeys := make([]string, 0, len(arguments))
	for argumentKey := range arguments {
		argumentKeys = append(argumentKeys, argumentKey)
	}
	sort.Strings(argumentKeys)

	hasher := sha256.New()
	for _, argumentKey := range argumentKeys {
		_, _ = hasher.Write([]byte(argumentKey))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write([]byte(arguments[argumentKey]))
		_, _ = hasher.Write([]byte{'\n'})
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func normalizeCapabilityRequest(capabilityRequest CapabilityRequest) CapabilityRequest {
	capabilityRequest.RequestID = strings.TrimSpace(capabilityRequest.RequestID)
	capabilityRequest.SessionID = strings.TrimSpace(capabilityRequest.SessionID)
	capabilityRequest.Actor = strings.TrimSpace(capabilityRequest.Actor)
	capabilityRequest.Capability = strings.TrimSpace(capabilityRequest.Capability)
	capabilityRequest.CorrelationID = strings.TrimSpace(capabilityRequest.CorrelationID)
	capabilityRequest.EchoedNativeToolName = ""
	capabilityRequest.EchoedNativeToolNameSnake = ""
	capabilityRequest.EchoedNativeToolNameCamel = ""
	capabilityRequest.EchoedNativeToolUseID = ""
	capabilityRequest.EchoedNativeToolUseIDSnake = ""
	capabilityRequest.EchoedNativeToolCallID = ""
	capabilityRequest.EchoedNativeToolCallIDAlt = ""

	if capabilityRequest.Arguments == nil {
		return capabilityRequest
	}

	normalizedArguments := make(map[string]string, len(capabilityRequest.Arguments))
	for argumentKey, rawArgumentValue := range capabilityRequest.Arguments {
		normalizedValue := rawArgumentValue
		if argumentKey != "content" {
			normalizedValue = strings.TrimSpace(normalizedValue)
		}
		if argumentKey == "path" && strings.TrimSpace(normalizedValue) != "" {
			normalizedValue = filepath.Clean(normalizedValue)
		}
		normalizedArguments[argumentKey] = normalizedValue
	}
	capabilityRequest.Arguments = normalizedArguments
	return capabilityRequest
}

func capabilitySet(capabilities []string) map[string]struct{} {
	set := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		if trimmedCapability := strings.TrimSpace(capability); trimmedCapability != "" {
			set[trimmedCapability] = struct{}{}
		}
	}
	return set
}

func normalizedCapabilityList(capabilities []string) []string {
	normalized := make([]string, 0, len(capabilities))
	seenCapabilities := make(map[string]struct{}, len(capabilities))
	for _, rawCapability := range capabilities {
		trimmedCapability := strings.TrimSpace(rawCapability)
		if trimmedCapability == "" {
			continue
		}
		if _, seen := seenCapabilities[trimmedCapability]; seen {
			continue
		}
		seenCapabilities[trimmedCapability] = struct{}{}
		normalized = append(normalized, trimmedCapability)
	}
	return normalized
}

func (server *Server) decodeJSONBody(writer http.ResponseWriter, request *http.Request, maxBodyBytes int64, destination interface{}) error {
	request.Body = http.MaxBytesReader(writer, request.Body, maxBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("request body must contain a single JSON object")
	}
	return nil
}

func (server *Server) readAndVerifySignedBody(writer http.ResponseWriter, request *http.Request, maxBodyBytes int64, controlSessionID string) ([]byte, CapabilityResponse, bool) {
	request.Body = http.MaxBytesReader(writer, request.Body, maxBodyBytes)
	requestBodyBytes, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: fmt.Sprintf("invalid request body: %v", err),
			DenialCode:   DenialCodeMalformedRequest,
		}, false
	}
	if verificationResponse, ok := server.verifySignedRequest(request, requestBodyBytes, controlSessionID); !ok {
		return nil, verificationResponse, false
	}
	return requestBodyBytes, CapabilityResponse{}, true
}

func decodeJSONBytes(requestBodyBytes []byte, destination interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(requestBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("request body must contain a single JSON object")
	}
	return nil
}

func (server *Server) verifySignedRequest(request *http.Request, requestBodyBytes []byte, expectedControlSessionID string) (CapabilityResponse, bool) {
	controlSessionID := strings.TrimSpace(request.Header.Get("X-Loopgate-Control-Session"))
	requestTimestamp := strings.TrimSpace(request.Header.Get("X-Loopgate-Request-Timestamp"))
	requestNonce := strings.TrimSpace(request.Header.Get("X-Loopgate-Request-Nonce"))
	requestSignature := strings.TrimSpace(request.Header.Get("X-Loopgate-Request-Signature"))

	if controlSessionID == "" || requestTimestamp == "" || requestNonce == "" || requestSignature == "" {
		return CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "signed control-plane headers are required",
			DenialCode:   DenialCodeRequestSignatureMissing,
		}, false
	}
	if controlSessionID != expectedControlSessionID {
		return CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "control session binding is invalid",
			DenialCode:   DenialCodeControlSessionBindingInvalid,
		}, false
	}

	parsedTimestamp, err := time.Parse(time.RFC3339Nano, requestTimestamp)
	if err != nil {
		return CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "request timestamp is invalid",
			DenialCode:   DenialCodeRequestTimestampInvalid,
		}, false
	}
	if parsedTimestamp.Before(server.now().UTC().Add(-requestSignatureSkew)) || parsedTimestamp.After(server.now().UTC().Add(requestSignatureSkew)) {
		return CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "request timestamp is outside the allowed skew window",
			DenialCode:   DenialCodeRequestTimestampInvalid,
		}, false
	}

	server.mu.Lock()
	server.pruneExpiredLocked()
	activeSession, found := server.sessions[controlSessionID]
	server.mu.Unlock()
	if !found || strings.TrimSpace(activeSession.SessionMACKey) == "" {
		return CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "control session binding is invalid",
			DenialCode:   DenialCodeControlSessionBindingInvalid,
		}, false
	}

	return server.verifySignedRequestWithMACKey(request, requestBodyBytes, expectedControlSessionID, activeSession.SessionMACKey)
}

func (server *Server) verifySignedRequestWithMACKey(request *http.Request, requestBodyBytes []byte, expectedControlSessionID string, sessionMACKey string) (CapabilityResponse, bool) {
	controlSessionID := strings.TrimSpace(request.Header.Get("X-Loopgate-Control-Session"))
	requestTimestamp := strings.TrimSpace(request.Header.Get("X-Loopgate-Request-Timestamp"))
	requestNonce := strings.TrimSpace(request.Header.Get("X-Loopgate-Request-Nonce"))
	requestSignature := strings.TrimSpace(request.Header.Get("X-Loopgate-Request-Signature"))

	if controlSessionID == "" || requestTimestamp == "" || requestNonce == "" || requestSignature == "" {
		return CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "signed control-plane headers are required",
			DenialCode:   DenialCodeRequestSignatureMissing,
		}, false
	}
	if controlSessionID != expectedControlSessionID {
		return CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "control session binding is invalid",
			DenialCode:   DenialCodeControlSessionBindingInvalid,
		}, false
	}

	parsedTimestamp, err := time.Parse(time.RFC3339Nano, requestTimestamp)
	if err != nil {
		return CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "request timestamp is invalid",
			DenialCode:   DenialCodeRequestTimestampInvalid,
		}, false
	}
	if parsedTimestamp.Before(server.now().UTC().Add(-requestSignatureSkew)) || parsedTimestamp.After(server.now().UTC().Add(requestSignatureSkew)) {
		return CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "request timestamp is outside the allowed skew window",
			DenialCode:   DenialCodeRequestTimestampInvalid,
		}, false
	}

	expectedSignature := signRequest(sessionMACKey, request.Method, request.URL.Path, controlSessionID, requestTimestamp, requestNonce, requestBodyBytes)
	decodedRequestSignature, err := hex.DecodeString(requestSignature)
	if err != nil {
		return CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "request signature is invalid",
			DenialCode:   DenialCodeRequestSignatureInvalid,
		}, false
	}
	decodedExpectedSignature, err := hex.DecodeString(expectedSignature)
	if err != nil {
		return CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "request signature is invalid",
			DenialCode:   DenialCodeRequestSignatureInvalid,
		}, false
	}
	if !hmac.Equal(decodedExpectedSignature, decodedRequestSignature) {
		return CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "request signature is invalid",
			DenialCode:   DenialCodeRequestSignatureInvalid,
		}, false
	}

	if nonceDenial := server.recordAuthNonce(controlSessionID, requestNonce); nonceDenial != nil {
		return *nonceDenial, false
	}

	return CapabilityResponse{}, true
}

func (server *Server) pruneExpiredLocked() {
	nowUTC := server.now().UTC()
	if server.expirySweepMaxInterval > 0 && !server.nextExpirySweepAt.IsZero() && nowUTC.Before(server.nextExpirySweepAt) {
		return
	}

	earliestNextSweepAt := time.Time{}
	noteNextSweepCandidate := func(candidateTime time.Time) {
		if candidateTime.IsZero() {
			return
		}
		candidateTime = candidateTime.UTC()
		if earliestNextSweepAt.IsZero() || candidateTime.Before(earliestNextSweepAt) {
			earliestNextSweepAt = candidateTime
		}
	}

	for tokenString, tokenClaims := range server.tokens {
		if nowUTC.After(tokenClaims.ExpiresAt) {
			delete(server.tokens, tokenString)
			continue
		}
		noteNextSweepCandidate(tokenClaims.ExpiresAt)
	}
	for controlSessionID, activeSession := range server.sessions {
		if nowUTC.After(activeSession.ExpiresAt) {
			delete(server.sessions, controlSessionID)
			delete(server.approvalTokenIndex, approvalTokenHash(activeSession.ApprovalToken))
			continue
		}
		noteNextSweepCandidate(activeSession.ExpiresAt)
	}
	for approvalID, pendingApproval := range server.approvals {
		if nowUTC.After(pendingApproval.ExpiresAt) {
			if pendingApproval.State == approvalStatePending {
				pendingApproval.State = approvalStateExpired
				server.approvals[approvalID] = pendingApproval
				noteNextSweepCandidate(pendingApproval.ExpiresAt.Add(requestReplayWindow))
				continue
			}
			if nowUTC.Sub(pendingApproval.ExpiresAt) > requestReplayWindow {
				delete(server.approvals, approvalID)
				continue
			}
			noteNextSweepCandidate(pendingApproval.ExpiresAt.Add(requestReplayWindow))
			continue
		}
		noteNextSweepCandidate(pendingApproval.ExpiresAt)
	}
	for requestKey, seenRequest := range server.seenRequests {
		if nowUTC.Sub(seenRequest.SeenAt) > requestReplayWindow {
			delete(server.seenRequests, requestKey)
			continue
		}
		noteNextSweepCandidate(seenRequest.SeenAt.Add(requestReplayWindow))
	}
	for nonceKey, seenNonce := range server.seenAuthNonces {
		if nowUTC.Sub(seenNonce.SeenAt) > requestReplayWindow {
			delete(server.seenAuthNonces, nonceKey)
			continue
		}
		noteNextSweepCandidate(seenNonce.SeenAt.Add(requestReplayWindow))
	}
	for tokenID, consumedToken := range server.usedTokens {
		if nowUTC.Sub(consumedToken.ConsumedAt) > requestReplayWindow {
			delete(server.usedTokens, tokenID)
			continue
		}
		noteNextSweepCandidate(consumedToken.ConsumedAt.Add(requestReplayWindow))
	}
	for launchToken, workerLaunch := range server.morphlingWorkerLaunches {
		if nowUTC.After(workerLaunch.ExpiresAt) {
			delete(server.morphlingWorkerLaunches, launchToken)
			continue
		}
		noteNextSweepCandidate(workerLaunch.ExpiresAt)
	}
	for workerToken, workerSession := range server.morphlingWorkerSessions {
		if nowUTC.After(workerSession.ExpiresAt) {
			delete(server.morphlingWorkerSessions, workerToken)
			continue
		}
		noteNextSweepCandidate(workerSession.ExpiresAt)
	}

	if server.expirySweepMaxInterval <= 0 {
		server.nextExpirySweepAt = time.Time{}
		return
	}

	maxScheduledSweepAt := nowUTC.Add(server.expirySweepMaxInterval)
	switch {
	case earliestNextSweepAt.IsZero():
		server.nextExpirySweepAt = time.Time{}
	case earliestNextSweepAt.Before(nowUTC):
		server.nextExpirySweepAt = nowUTC
	case earliestNextSweepAt.Before(maxScheduledSweepAt):
		server.nextExpirySweepAt = earliestNextSweepAt
	default:
		server.nextExpirySweepAt = maxScheduledSweepAt
	}
}

func (server *Server) noteExpiryCandidateLocked(candidateTime time.Time) {
	if server.expirySweepMaxInterval <= 0 || candidateTime.IsZero() {
		return
	}
	candidateTime = candidateTime.UTC()
	if server.nextExpirySweepAt.IsZero() || candidateTime.Before(server.nextExpirySweepAt) {
		server.nextExpirySweepAt = candidateTime
	}
}

func (server *Server) noteReplayWindowCandidateLocked(seenAt time.Time) {
	if seenAt.IsZero() {
		return
	}
	server.noteExpiryCandidateLocked(seenAt.UTC().Add(requestReplayWindow))
}

type persistedNonce struct {
	ControlSessionID string `json:"control_session_id"`
	SeenAt           string `json:"seen_at"`
}

type nonceReplayFile struct {
	Nonces map[string]persistedNonce `json:"nonces"`
}

func (server *Server) loadNonceReplayState() error {
	rawBytes, err := os.ReadFile(server.noncePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read nonce replay state: %w", err)
	}
	var stateFile nonceReplayFile
	if err := json.Unmarshal(rawBytes, &stateFile); err != nil {
		return fmt.Errorf("decode nonce replay state: %w", err)
	}
	nowUTC := server.now().UTC()
	for nonceKey, entry := range stateFile.Nonces {
		seenAt, parseErr := time.Parse(time.RFC3339Nano, entry.SeenAt)
		if parseErr != nil {
			continue
		}
		if nowUTC.Sub(seenAt) > requestReplayWindow {
			continue
		}
		server.seenAuthNonces[nonceKey] = seenRequest{
			ControlSessionID: entry.ControlSessionID,
			SeenAt:           seenAt,
		}
	}
	return nil
}

func (server *Server) saveNonceReplayState() error {
	server.mu.Lock()
	if len(server.seenAuthNonces) == 0 {
		server.mu.Unlock()
		return nil
	}
	entries := make(map[string]persistedNonce, len(server.seenAuthNonces))
	for nonceKey, seen := range server.seenAuthNonces {
		entries[nonceKey] = persistedNonce{
			ControlSessionID: seen.ControlSessionID,
			SeenAt:           seen.SeenAt.UTC().Format(time.RFC3339Nano),
		}
	}
	server.mu.Unlock()

	stateFile := nonceReplayFile{Nonces: entries}
	jsonBytes, err := json.Marshal(stateFile)
	if err != nil {
		return fmt.Errorf("marshal nonce replay state: %w", err)
	}
	tempPath := server.noncePath + ".tmp"
	if err := os.WriteFile(tempPath, jsonBytes, 0o600); err != nil {
		return fmt.Errorf("write nonce replay temp: %w", err)
	}
	if err := os.Rename(tempPath, server.noncePath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("commit nonce replay state: %w", err)
	}
	return nil
}

func (server *Server) countPendingApprovalsForSessionLocked(controlSessionID string) int {
	pendingCount := 0
	for _, pendingApproval := range server.approvals {
		if pendingApproval.ControlSessionID != controlSessionID {
			continue
		}
		if pendingApproval.State == approvalStatePending {
			pendingCount++
		}
	}
	return pendingCount
}

// recordRequest returns nil when the request_id is accepted for replay tracking, or a denial
// when duplicate or when the replay map is saturated (fail closed — no eviction).
func (server *Server) recordRequest(controlSessionID string, capabilityRequest CapabilityRequest) *CapabilityResponse {
	requestKey := controlSessionID + ":" + capabilityRequest.RequestID
	server.mu.Lock()
	defer server.mu.Unlock()
	server.pruneExpiredLocked()
	if _, found := server.seenRequests[requestKey]; found {
		return &CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusDenied,
			DenialReason: "duplicate request_id was rejected",
			DenialCode:   DenialCodeRequestReplayDetected,
		}
	}
	if len(server.seenRequests) >= server.maxSeenRequestReplayEntries {
		return &CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusDenied,
			DenialReason: "request replay store is at capacity",
			DenialCode:   DenialCodeReplayStateSaturated,
		}
	}
	server.seenRequests[requestKey] = seenRequest{
		ControlSessionID: controlSessionID,
		SeenAt:           server.now().UTC(),
	}
	server.noteReplayWindowCandidateLocked(server.seenRequests[requestKey].SeenAt)
	return nil
}

func (server *Server) consumeExecutionToken(tokenClaims capabilityToken, capabilityRequest CapabilityRequest) (CapabilityResponse, bool) {
	if strings.TrimSpace(tokenClaims.BoundCapability) != "" && tokenClaims.BoundCapability != capabilityRequest.Capability {
		return CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusDenied,
			DenialReason: "capability token binding does not match requested capability",
			DenialCode:   DenialCodeCapabilityTokenBindingInvalid,
		}, true
	}
	if strings.TrimSpace(tokenClaims.BoundArgumentHash) != "" && tokenClaims.BoundArgumentHash != normalizedArgumentHash(capabilityRequest.Arguments) {
		return CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusDenied,
			DenialReason: "capability token binding does not match normalized arguments",
			DenialCode:   DenialCodeCapabilityTokenBindingInvalid,
		}, true
	}
	if !tokenClaims.SingleUse {
		return CapabilityResponse{}, false
	}

	server.mu.Lock()
	defer server.mu.Unlock()

	server.pruneExpiredLocked()
	if _, alreadyUsed := server.usedTokens[tokenClaims.TokenID]; alreadyUsed {
		return CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusDenied,
			DenialReason: "single-use capability token was already consumed",
			DenialCode:   DenialCodeCapabilityTokenReused,
		}, true
	}
	server.usedTokens[tokenClaims.TokenID] = usedToken{
		TokenID:           tokenClaims.TokenID,
		ParentTokenID:     tokenClaims.ParentTokenID,
		ControlSessionID:  tokenClaims.ControlSessionID,
		Capability:        capabilityRequest.Capability,
		NormalizedArgHash: normalizedArgumentHash(capabilityRequest.Arguments),
		ConsumedAt:        server.now().UTC(),
	}
	server.noteReplayWindowCandidateLocked(server.usedTokens[tokenClaims.TokenID].ConsumedAt)
	return CapabilityResponse{}, false
}

// recordAuthNonce returns nil if the nonce is new and recorded, a denial for replay, or a
// denial when the nonce map is saturated (fail closed).
func (server *Server) recordAuthNonce(controlSessionID string, requestNonce string) *CapabilityResponse {
	nonceKey := controlSessionID + ":" + requestNonce
	server.mu.Lock()
	defer server.mu.Unlock()
	server.pruneExpiredLocked()
	if _, found := server.seenAuthNonces[nonceKey]; found {
		return &CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "request nonce replay was rejected",
			DenialCode:   DenialCodeRequestNonceReplayDetected,
		}
	}
	if len(server.seenAuthNonces) >= server.maxAuthNonceReplayEntries {
		return &CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "request nonce replay store is at capacity",
			DenialCode:   DenialCodeReplayStateSaturated,
		}
	}
	server.seenAuthNonces[nonceKey] = seenRequest{
		ControlSessionID: controlSessionID,
		SeenAt:           server.now().UTC(),
	}
	server.noteReplayWindowCandidateLocked(server.seenAuthNonces[nonceKey].SeenAt)
	return nil
}

func httpStatusForResponse(response CapabilityResponse) int {
	switch response.Status {
	case ResponseStatusSuccess:
		return http.StatusOK
	case ResponseStatusPendingApproval:
		return http.StatusAccepted
	case ResponseStatusDenied:
		switch response.DenialCode {
		case DenialCodeCapabilityTokenMissing, DenialCodeCapabilityTokenInvalid, DenialCodeCapabilityTokenExpired,
			DenialCodeApprovalTokenMissing, DenialCodeApprovalTokenInvalid, DenialCodeApprovalTokenExpired,
			DenialCodeRequestSignatureMissing, DenialCodeRequestSignatureInvalid, DenialCodeRequestTimestampInvalid,
			DenialCodeRequestNonceReplayDetected, DenialCodeControlSessionBindingInvalid:
			return http.StatusUnauthorized
		case DenialCodeApprovalNotFound, DenialCodeMorphlingNotFound, DenialCodeContinuityInspectionNotFound:
			return http.StatusNotFound
		case DenialCodeRequestReplayDetected, DenialCodeCapabilityTokenReused, DenialCodeApprovalStateConflict,
			DenialCodeQuarantinePruneNotEligible:
			return http.StatusConflict
		case DenialCodeSessionOpenRateLimited, DenialCodeSessionActiveLimitReached, DenialCodeReplayStateSaturated, DenialCodePendingApprovalLimitReached:
			return http.StatusTooManyRequests
		case DenialCodeMalformedRequest, DenialCodeInvalidCapabilityArguments, DenialCodeSiteURLInvalid,
			DenialCodeSiteInspectionUnsupportedType, DenialCodeSandboxPathInvalid, DenialCodeSandboxHostDestinationInvalid,
			DenialCodeMorphlingInputInvalid, DenialCodeMorphlingArtifactInvalid, DenialCodeMorphlingReviewInvalid,
			DenialCodeMorphlingWorkerLaunchInvalid, DenialCodeApprovalDecisionNonceMissing, DenialCodeApprovalDecisionNonceInvalid,
			DenialCodeApprovalManifestMismatch:
			return http.StatusBadRequest
		default:
			return http.StatusForbidden
		}
	case ResponseStatusError:
		switch response.DenialCode {
		case DenialCodeMalformedRequest, DenialCodeSiteURLInvalid, DenialCodeSiteInspectionUnsupportedType,
			DenialCodeSandboxPathInvalid, DenialCodeSandboxHostDestinationInvalid:
			return http.StatusBadRequest
		case DenialCodeAuditUnavailable:
			return http.StatusServiceUnavailable
		default:
			return http.StatusInternalServerError
		}
	default:
		return http.StatusInternalServerError
	}
}

func (server *Server) activeSessionsForPeerUIDLocked(peerUID uint32) int {
	activeSessionCount := 0
	for _, activeSession := range server.sessions {
		if activeSession.PeerIdentity.UID == peerUID {
			activeSessionCount++
		}
	}
	return activeSessionCount
}

func isSecretExportCapabilityHeuristic(capability string) bool {
	lowerCapability := strings.ToLower(strings.TrimSpace(capability))
	if lowerCapability == "" {
		return false
	}

	sensitivePrefixes := []string{
		"secret.",
		"token.",
		"credential.",
		"credentials.",
		"key.",
	}
	for _, sensitivePrefix := range sensitivePrefixes {
		if strings.HasPrefix(lowerCapability, sensitivePrefix) {
			return true
		}
	}

	if strings.Contains(lowerCapability, "export") && (strings.Contains(lowerCapability, "token") || strings.Contains(lowerCapability, "secret") || strings.Contains(lowerCapability, "credential") || strings.Contains(lowerCapability, "key")) {
		return true
	}
	return false
}

func (server *Server) checkFsReadRateLimit(controlSessionID string) bool {
	server.mu.Lock()
	defer server.mu.Unlock()

	nowUTC := server.now().UTC()
	cutoff := nowUTC.Add(-fsReadRateWindow)

	timestamps := server.sessionReadCounts[controlSessionID]
	// Prune old entries.
	pruned := timestamps[:0]
	for _, ts := range timestamps {
		if ts.After(cutoff) {
			pruned = append(pruned, ts)
		}
	}
	if len(pruned) >= defaultFsReadRateLimit {
		server.sessionReadCounts[controlSessionID] = pruned
		return true
	}
	server.sessionReadCounts[controlSessionID] = append(pruned, nowUTC)
	return false
}

func loadOrCreateStateKey(keyPath string) ([]byte, error) {
	keyBytes, err := os.ReadFile(keyPath)
	if err == nil && len(keyBytes) >= 32 {
		return keyBytes[:32], nil
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read state key: %w", err)
	}
	newKey := make([]byte, 32)
	if _, err := rand.Read(newKey); err != nil {
		return nil, fmt.Errorf("generate state key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return nil, fmt.Errorf("create state key dir: %w", err)
	}
	if err := os.WriteFile(keyPath, newKey, 0o600); err != nil {
		return nil, fmt.Errorf("write state key: %w", err)
	}
	return newKey, nil
}

func randomHex(byteCount int) (string, error) {
	randomBytes := make([]byte, byteCount)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(randomBytes), nil
}

func peerIdentityFromContext(ctx context.Context) (peerIdentity, bool) {
	peerCreds, ok := ctx.Value(peerIdentityContextKey).(peerIdentity)
	return peerCreds, ok
}

func approvalDecisionHTTPStatus(denialCode string) int {
	switch denialCode {
	case DenialCodeApprovalNotFound:
		return http.StatusNotFound
	case DenialCodeApprovalTokenMissing, DenialCodeApprovalTokenInvalid, DenialCodeApprovalTokenExpired:
		return http.StatusUnauthorized
	default:
		return http.StatusForbidden
	}
}

func signedRequestHTTPStatus(denialCode string) int {
	switch denialCode {
	case DenialCodeRequestSignatureMissing, DenialCodeRequestSignatureInvalid, DenialCodeRequestTimestampInvalid, DenialCodeRequestNonceReplayDetected, DenialCodeControlSessionBindingInvalid:
		return http.StatusUnauthorized
	case DenialCodeReplayStateSaturated:
		return http.StatusTooManyRequests
	default:
		return http.StatusBadRequest
	}
}

func quarantineHTTPStatus(quarantineErr error) int {
	switch {
	case errors.Is(quarantineErr, errQuarantinedSourceNotFound):
		return http.StatusNotFound
	case errors.Is(quarantineErr, errQuarantinePruneNotEligible):
		return http.StatusConflict
	case errors.Is(quarantineErr, errQuarantinedSourceBytesRetained):
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}

func quarantineDenialCode(quarantineErr error) string {
	switch {
	case errors.Is(quarantineErr, errQuarantinePruneNotEligible):
		return DenialCodeQuarantinePruneNotEligible
	case errors.Is(quarantineErr, errQuarantinedSourceBytesRetained):
		return DenialCodeSourceBytesUnavailable
	default:
		return DenialCodeExecutionFailed
	}
}

func redactQuarantineError(quarantineErr error) string {
	return secrets.RedactText(quarantineErr.Error())
}

func defaultLabel(rawValue string, fallback string) string {
	trimmedValue := strings.TrimSpace(rawValue)
	if trimmedValue == "" {
		return fallback
	}
	return trimmedValue
}

func copyCapabilitySet(input map[string]struct{}) map[string]struct{} {
	if len(input) == 0 {
		return map[string]struct{}{}
	}
	copied := make(map[string]struct{}, len(input))
	for capability := range input {
		copied[capability] = struct{}{}
	}
	return copied
}

func copyInterfaceMap(input map[string]interface{}) map[string]interface{} {
	if len(input) == 0 {
		return map[string]interface{}{}
	}
	copied := make(map[string]interface{}, len(input))
	for key, value := range input {
		copied[key] = value
	}
	return copied
}

func canonicalizeAuditData(input map[string]interface{}) (map[string]interface{}, error) {
	payloadBytes, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var canonicalData map[string]interface{}
	if err := json.Unmarshal(payloadBytes, &canonicalData); err != nil {
		return nil, err
	}
	return canonicalData, nil
}

func hashAuditEvent(auditEvent ledger.Event) (string, error) {
	if auditEvent.V == 0 {
		auditEvent.V = ledger.SchemaVersion
	}
	payloadBytes, err := json.Marshal(auditEvent)
	if err != nil {
		return "", err
	}
	payloadHash := sha256.Sum256(payloadBytes)
	return hex.EncodeToString(payloadHash[:]), nil
}

func signRequest(sessionMACKey string, method string, path string, controlSessionID string, requestTimestamp string, requestNonce string, requestBodyBytes []byte) string {
	bodyHash := sha256.Sum256(requestBodyBytes)
	signingPayload := strings.Join([]string{
		method,
		path,
		controlSessionID,
		requestTimestamp,
		requestNonce,
		hex.EncodeToString(bodyHash[:]),
	}, "\n")

	mac := hmac.New(sha256.New, []byte(sessionMACKey))
	_, _ = mac.Write([]byte(signingPayload))
	return hex.EncodeToString(mac.Sum(nil))
}
