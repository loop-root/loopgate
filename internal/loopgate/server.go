package loopgate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
	"loopgate/internal/loopdiag"
	policypkg "loopgate/internal/policy"
	"loopgate/internal/sandbox"
	"loopgate/internal/secrets"
	toolspkg "loopgate/internal/tools"
)

const statusVersion = "0.1.0"

type requestContextKey string

const peerIdentityContextKey requestContextKey = "loopgate_peer_identity"

type peerIdentity struct {
	UID  uint32
	PID  int
	EPID int
}

type auditState struct {
	// mu serializes the authoritative append-only audit chain state.
	//
	// Protected fields:
	//   - sequence
	//   - lastHash
	//   - eventsSinceCheckpoint
	//
	// Why this lock exists:
	//   - hash-chain sequencing and disk append must behave like one logical commit
	//   - two concurrent writers must never assign the same sequence or previous hash
	//
	// Sequencing rule:
	//   - treat audit.mu as a leaf lock
	//   - resolve any control-session-derived metadata before taking audit.mu
	//   - never acquire control-plane mu while holding audit.mu
	mu sync.Mutex

	sequence              uint64
	lastHash              string
	eventsSinceCheckpoint int
}

type uiState struct {
	// mu protects only derived UI event projection state.
	//
	// Protected fields:
	//   - sequence
	//   - events
	//   - subscribers
	//   - nextSubscriberID
	//
	// Why this lock exists:
	//   - UI replay/event-stream buffers are convenience projections, not source of truth
	//   - keeping them separate prevents chatty subscribers from contending on the
	//     primary control-plane lock
	//
	// Sequencing rule:
	//   - build authoritative data first if needed
	//   - then emit/update UI projections under ui.mu
	mu sync.Mutex

	sequence         uint64
	events           []controlapipkg.UIEventEnvelope
	subscribers      map[int]uiEventSubscriber
	nextSubscriberID int
}

type providerRuntimeState struct {
	// mu protects live provider/integration runtime state.
	//
	// Protected fields:
	//   - tokens
	//   - tokenFetches
	//   - tokenGenerations
	//   - configGeneration
	//   - configuredConnections
	//   - configuredCapabilities
	//
	// Why this lock exists:
	//   - these maps back outbound integration execution and runtime config reloads
	//   - separating them from the primary control-plane lock keeps provider churn
	//     away from sessions, approvals, and replay state
	//
	// Sequencing rule:
	//   - snapshot provider/configured state under providerRuntime.mu
	//   - release it before policy evaluation, HTTP calls, or audit emission
	mu sync.Mutex

	tokens                 map[string]providerAccessToken
	tokenFetches           map[string]*providerTokenFetch
	tokenGenerations       map[string]uint64
	configGeneration       uint64
	configuredConnections  map[string]configuredConnection
	configuredCapabilities map[string]configuredCapability
}

type pkceRuntimeState struct {
	// mu protects short-lived PKCE/OAuth browser handoff sessions.
	//
	// Protected fields:
	//   - sessions
	//
	// Why this lock exists:
	//   - PKCE state is time-bounded, independent from normal control-session state,
	//     and frequently pruned/consumed by connection-specific flows
	//   - keeping it separate avoids polluting mu with OAuth handoff bookkeeping
	//
	// Sequencing rule:
	//   - prune or snapshot PKCE state under pkceRuntime.mu
	//   - release it before connection mutation, audit emission, or network I/O
	mu sync.Mutex

	sessions map[string]pendingPKCESession
}

type hostAccessRuntimeState struct {
	// mu protects temporary host-access planning state.
	//
	// Protected fields:
	//   - plans
	//   - appliedPlanAt
	//
	// Why this lock exists:
	//   - host access plan drafting and plan-apply recovery have their own TTL and
	//     duplicate-detection semantics
	//   - keeping them separate prevents plan bookkeeping from inflating the
	//     primary control-plane critical section
	//
	// Sequencing rule:
	//   - mutate plan/tombstone state under hostAccessRuntime.mu
	//   - release it before filesystem work, audit emission, or capability execution
	mu sync.Mutex

	plans         map[string]*hostAccessStoredPlan
	appliedPlanAt map[string]time.Time
}

type connectionRuntimeState struct {
	// mu protects persisted integration connection records loaded from runtime state.
	//
	// Protected fields:
	//   - records
	//
	// Why this lock exists:
	//   - operator store/rotate/validate/delete flows rewrite the persisted
	//     connection-record snapshot
	//   - this state is separate from control-session authority and from the live
	//     provider token/configured capability runtime
	//
	// Sequencing rule:
	//   - clone/mutate connection records under connectionRuntime.mu
	//   - release it before audit emission or secret-store I/O where possible
	mu sync.Mutex

	records map[string]connectionRecord
}

type claudeHookRuntimeState struct {
	// mu protects repo-local Claude hook session and approval cache state.
	//
	// Why this lock exists:
	//   - hook session/approval bookkeeping is harness-local metadata with its own
	//     filesystem state and lifecycle
	//   - keeping it separate prevents hook cache churn from contending with the
	//     primary control-plane state machine
	//
	// Sequencing rule:
	//   - perform hook state load/save under claudeHookRuntime.mu
	//   - release it before audit emission or other control-plane work
	mu sync.Mutex
}

type approvalControlState struct {
	// records holds authoritative approval lifecycle state under server.mu.
	records map[string]pendingApproval
	// tokenIndex maps SHA-256(approval token) to control session ID under server.mu.
	tokenIndex map[string]string
}

type replayControlState struct {
	// seenRequests tracks accepted request IDs for duplicate suppression under server.mu.
	seenRequests map[string]seenRequest
	// seenAuthNonces tracks accepted signed-request nonces for replay suppression under server.mu.
	seenAuthNonces map[string]seenRequest
	// usedTokens tracks consumed single-use capability tokens under server.mu.
	usedTokens map[string]usedToken
	// sessionReadCounts tracks fs_read timestamps per control session under server.mu.
	sessionReadCounts map[string][]time.Time
	// authDeniedBursts tracks repeated auth-denial bursts so Loopgate can
	// preserve first-failure auditability without forcing a disk sync for every
	// repeated bad-token request in the same short window.
	authDeniedBursts map[string]authDeniedBurst
	// hookPreValidateCounts tracks PreToolUse hook timestamps per peer UID under server.mu.
	hookPreValidateCounts map[uint32][]time.Time
	// hookPeerAuthFailureCounts tracks repeated hook peer-binding failures under server.mu.
	hookPeerAuthFailureCounts map[string][]time.Time
}

type authDeniedBurst struct {
	WindowStartedAt time.Time
	LastSeenAt      time.Time
	SuppressedCount int
}

type sessionControlState struct {
	// sessions holds authoritative control session lifecycle state under server.mu.
	sessions map[string]controlSession
	// tokens holds active capability tokens bound to control sessions under server.mu.
	tokens map[string]capabilityToken
	// openByUID tracks recent session-open timestamps per peer UID under server.mu.
	openByUID map[uint32]time.Time
}

// Locking invariant for Server state:
//   - Prefer holding exactly one of these mutex families at a time.
//   - Current production code treats audit.mu, ui.mu, claudeHookRuntime.mu, connectionRuntime.mu,
//     hostAccessRuntime.mu, providerRuntime.mu,
//     pkceRuntime.mu, and policyRuntimeMu as leaf-domain locks. Callers
//     snapshot state under one lock, release it, and only then cross into
//     another domain.
//   - mu is the primary state lock for sessions, tokens, approvals, replay
//     tables, and other authoritative control-plane state.
//   - audit.mu is a strict leaf lock for append-only audit sequencing and disk
//     persistence. Never acquire mu while holding audit.mu. logEvent* helpers
//     intentionally resolve tenancy before taking audit.mu so we never invert
//     into audit.mu -> mu.
//   - If future code truly must hold more than one of these at once, document
//     the exact acquisition order in the same change before merging. Do not
//     introduce ad hoc nested locking.
type Server struct {
	repoRoot                   string
	socketPath                 string
	auditPath                  string
	auditExportStatePath       string
	noncePath                  string
	quarantineDir              string
	derivedArtifactDir         string
	connectionPath             string
	claudeHookSessionsPath     string
	claudeHookSessionsRoot     string
	mcpGatewayManifests        map[string]mcpGatewayServerManifest
	mcpGatewayApprovalRequests map[string]pendingMCPGatewayApprovalRequest
	mcpGatewayLaunchedServers  map[string]*mcpGatewayLaunchedServer
	sandboxPaths               sandbox.Paths
	policy                     config.Policy
	runtimeConfig              config.RuntimeConfig
	registry                   *toolspkg.Registry
	checker                    *policypkg.Checker
	now                        func() time.Time
	appendAuditEvent           func(string, ledger.Event) error
	// auditLedgerRuntime owns the append-chain cache for this server's audit
	// ledger reads/appends so audit state does not rely on package-global cache mutation.
	auditLedgerRuntime       *ledger.AppendRuntime
	resolveSecretStore       func(secrets.SecretRef) (secrets.SecretStore, error)
	reportResponseWriteError func(httpStatus int, cause error)
	reportSecurityWarning    func(eventCode string, cause error)
	resolvePeerIdentity      func(net.Conn) (peerIdentity, error)
	resolveExePath           func(int) (string, error)
	processExists            func(int) (bool, error)
	resolveUserHomeDir       func() (string, error)
	expectedClientPath       string
	nonceReplayStore         authNonceReplayStore
	server                   *http.Server
	// diagnostic is optional operator text logging (runtime/logs); not authoritative audit.
	diagnostic *loopdiag.Manager

	// audit owns the append-only audit chain sequencing state.
	// See auditState above and docs/design_overview/loopgate_locking.md.
	audit auditState

	// auditExportMu protects export-side progress state that is derived from the
	// immutable ledger but persisted independently for batching/retry.
	//
	// Why this lock exists:
	//   - audit export is not authoritative audit state
	//   - export cursors and "last exported" bookkeeping can advance independently
	//     of live request handling without contending on audit.mu or mu
	//
	// Sequencing rule:
	//   - do not hold auditExportMu while calling logEvent
	//   - snapshot/export work should read ledger state separately, then update the
	//     export cursor under auditExportMu
	auditExportMu sync.Mutex

	// promotionMu serializes promotion/quarantine file workflows so a single
	// derived artifact path is promoted exactly once per logical action.
	//
	// Why this lock exists:
	//   - promotion is request-driven but file-system side effects plus duplicate
	//     fingerprint indexing must not race each other and produce
	//     duplicate/misaligned derived artifacts
	//
	// Sequencing rule:
	//   - keep promotionMu isolated from mu/audit.mu; collect control-plane context
	//     first, then enter promotion code
	//
	// Protected fields:
	//   - promotionDuplicateIndex
	//   - promotionDuplicateIndexLoaded
	promotionMu sync.Mutex
	// promotionDuplicateIndex maps a canonical duplicate fingerprint to the
	// derived artifact ID that claimed it. Guarded by promotionMu.
	promotionDuplicateIndex map[string]string
	// promotionDuplicateIndexLoaded reports whether promotionDuplicateIndex has
	// been lazily rebuilt from derivedArtifactDir for this process. Guarded by
	// promotionMu.
	promotionDuplicateIndexLoaded bool

	// ui owns derived UI event projection state.
	// See uiState above and docs/design_overview/loopgate_locking.md.
	ui uiState

	// claudeHookRuntime owns repo-local hook session caches used by the Claude
	// harness integration. See claudeHookRuntimeState above and
	// docs/design_overview/loopgate_locking.md.
	claudeHookRuntime claudeHookRuntimeState

	// connectionRuntime owns persisted integration connection records.
	// See connectionRuntimeState above and docs/design_overview/loopgate_locking.md.
	connectionRuntime connectionRuntimeState

	// hostAccessRuntime owns temporary host-access planning state and
	// applied-plan tombstones used for duplicate recovery.
	// See hostAccessRuntimeState above and docs/design_overview/loopgate_locking.md.
	hostAccessRuntime hostAccessRuntimeState

	configStateDir string

	// providerRuntime owns live provider token state plus configured connection and
	// configured capability maps.
	// See providerRuntimeState above and docs/design_overview/loopgate_locking.md.
	providerRuntime     providerRuntimeState
	httpClient          *http.Client
	policyRuntime       serverPolicyRuntime
	policyContentSHA256 string

	// policyRuntimeMu protects the immutable-ish serverPolicyRuntime snapshot used
	// by request paths. Reads are far more common than writes, so this is the one
	// RWMutex in the package.
	//
	// Why this lock exists:
	//   - hot policy reloads need a coherent runtime bundle (policy + checker +
	//     registry + manifests + HTTP client)
	//   - request handlers need cheap concurrent read access to that bundle
	//
	// Sequencing rule:
	//   - read/copy the runtime snapshot under policyRuntimeMu, then release it
	//   - never hold policyRuntimeMu across capability execution or audit append
	policyRuntimeMu sync.RWMutex

	// pkceRuntime owns short-lived PKCE/OAuth browser handoff sessions.
	// See pkceRuntimeState above and docs/design_overview/loopgate_locking.md.
	pkceRuntime pkceRuntimeState

	// mu is the primary authoritative control-plane lock.
	//
	// Protected fields:
	//   - sessionState.sessions
	//   - sessionState.tokens
	//   - approvalState.records
	//   - replayState.seenRequests
	//   - replayState.seenAuthNonces
	//   - replayState.usedTokens
	//   - sessionState.openByUID
	//   - approvalState.tokenIndex
	//   - replayState.sessionReadCounts
	//   - expiry scheduling / caps / session MAC rotation master
	//
	// Why this lock exists:
	//   - these maps participate in auth, replay prevention, approval state, and
	//     session lifecycle invariants
	//   - mutations across them must be reasoned about as one authoritative state machine
	//
	// Sequencing rule:
	//   - capture a single now() snapshot while holding mu for auth/expiry decisions
	//   - do not split a logical state transition across multiple mu lock/unlock pairs
	//   - prefer: gather authoritative state under mu -> unlock -> audit/UI/IO work
	mu            sync.Mutex
	sessionState  sessionControlState
	approvalState approvalControlState
	replayState   replayControlState

	sessionOpenMinInterval    time.Duration
	maxActiveSessionsPerUID   int
	expirySweepMaxInterval    time.Duration
	fsReadRateLimit           int
	hookPreValidateRateLimit  int
	hookPreValidateRateWindow time.Duration
	// hookPeerAuthFailureRateLimit throttles repeated hook auth failures so a
	// local hammering loop cannot turn peer-binding rejects into an easy CPU path.
	hookPeerAuthFailureRateLimit int
	hookPeerAuthFailureWindow    time.Duration
	nextExpirySweepAt            time.Time
	// maxPendingApprovalsPerControlSession limits pending (state=pending) approvals per session.
	maxPendingApprovalsPerControlSession int
	maxSeenRequestReplayEntries          int
	maxAuthNonceReplayEntries            int
	// maxTotalControlSessions caps active control sessions (fail closed when full).
	maxTotalControlSessions int
	// maxTotalApprovalRecords caps server.approvalState.records including terminal rows until pruned.
	maxTotalApprovalRecords int
	// sessionMACRotationMaster is 32 bytes of server-held entropy used to derive per-epoch
	// session MAC keys (see session_mac_rotation.go). Loaded or created under runtime/state.
	sessionMACRotationMaster []byte
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
	ApprovedExecution bool
	BoundCapability   string
	BoundArgumentHash string
	ParentTokenID     string
}

type controlSession struct {
	ID                 string
	ActorLabel         string
	ClientSessionLabel string
	WorkspaceID        string
	// OperatorMountPaths are absolute host directories bound from a pinned operator client
	// at session open. They scope operator_mount.fs_* tools — never from model text or a
	// generic unpinned local client.
	OperatorMountPaths       []string
	PrimaryOperatorMountPath string
	OperatorMountWriteGrants map[string]time.Time
	RequestedCapabilities    map[string]struct{}
	ApprovalToken            string
	ApprovalTokenID          string
	SessionMACKey            string
	PeerIdentity             peerIdentity
	// TenantID/UserID are copied from config.Tenancy at session open (never from the client body).
	TenantID  string
	UserID    string
	ExpiresAt time.Time
	CreatedAt time.Time
}

const (
	sessionTTL  = 1 * time.Hour
	approvalTTL = 5 * time.Minute
	// Replay-tracked request IDs, auth nonces, used single-use tokens, and terminal approval
	// rows only need to outlive the authoritative session lifetime. Keeping them longer than the
	// 1-hour control-session TTL inflates in-memory state without adding meaningful protection.
	requestReplayWindow                 = sessionTTL
	requestSignatureSkew                = 2 * time.Minute
	maxOpenSessionBodyBytes             = 16 * 1024
	maxCapabilityBodyBytes              = 512 * 1024
	maxApprovalBodyBytes                = 8 * 1024
	maxHeaderBytes                      = 8 * 1024
	defaultSessionOpenMinInterval       = 500 * time.Millisecond
	defaultMaxActiveSessionsPerUID      = 8
	defaultExpirySweepMaxInterval       = 250 * time.Millisecond
	defaultFsReadRateLimit              = 60 // reads per minute per session
	fsReadRateWindow                    = 1 * time.Minute
	defaultHookPreValidateRateLimit     = 600 // hook checks per minute per peer UID
	hookPreValidateRateWindow           = 1 * time.Minute
	defaultHookPeerAuthFailureRateLimit = 60
	hookPeerAuthFailureRateWindow       = 1 * time.Minute
	defaultCapabilityExecutionTimeout   = 30 * time.Second
	// Shutdown needs to outlive the default capability execution deadline so in-flight
	// local tool executions can finish and emit their terminal audit event.
	defaultServerShutdownGracePeriod = defaultCapabilityExecutionTimeout + (5 * time.Second)
	// In-memory bounds (DoS): single-session pending approvals, replay maps, and global tables.
	defaultMaxPendingApprovalsPerControlSession = 64
	defaultMaxSeenRequestReplayEntries          = 65536
	defaultMaxAuthNonceReplayEntries            = 65536
	defaultMaxTotalControlSessions              = 512
	defaultMaxTotalApprovalRecords              = 4096
)

func normalizeSessionExecutablePinPath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	return filepath.Clean(trimmed)
}

func NewServer(repoRoot string, socketPath string) (*Server, error) {
	return NewServerWithOptions(repoRoot, socketPath)
}

// NewServerWithOptions constructs the Loopgate server for the local Unix-socket control plane.
func NewServerWithOptions(repoRoot string, socketPath string) (*Server, error) {
	if err := verifySupportedExecutionPlatform(); err != nil {
		return nil, err
	}
	validatedSocketPath, err := validateServerSocketPath(repoRoot, socketPath)
	if err != nil {
		return nil, fmt.Errorf("validate socket path: %w", err)
	}

	configStateDir := filepath.Join(repoRoot, "runtime", "state", "config")

	// Load policy directly from the repository YAML. Do not use
	// runtime/state/config/policy.json — a stale frozen copy can silently weaken
	// mounted-write approval semantics and other security boundaries.
	policyLoadResult, err := config.LoadPolicyWithHash(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("load policy: %w", err)
	}
	policy := policyLoadResult.Policy

	// Load runtime config from YAML (and optional diagnostic override). Do not use
	// runtime/state/config/runtime.json — operators and config PUT edit config/runtime.yaml.
	runtimeConfig, err := config.LoadRuntimeConfig(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("load runtime config: %w", err)
	}

	// Load connections: JSON state → YAML seed → empty.
	configuredConnections, configuredCapabilities, err := loadConfiguredConnectionsWithSeed(configStateDir, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("load configured connections: %w", err)
	}
	server := &Server{
		repoRoot:                   repoRoot,
		socketPath:                 validatedSocketPath,
		configStateDir:             configStateDir,
		auditPath:                  filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"),
		auditExportStatePath:       filepath.Join(repoRoot, runtimeConfig.Logging.AuditExport.StatePath),
		noncePath:                  filepath.Join(repoRoot, "runtime", "state", "nonce_replay.jsonl"),
		quarantineDir:              filepath.Join(repoRoot, "runtime", "state", "quarantine"),
		derivedArtifactDir:         filepath.Join(repoRoot, "runtime", "state", "derived_artifacts"),
		connectionPath:             filepath.Join(repoRoot, "runtime", "state", "loopgate_connections.json"),
		claudeHookSessionsPath:     filepath.Join(repoRoot, "runtime", "state", "claude_hook_sessions.json"),
		claudeHookSessionsRoot:     filepath.Join(repoRoot, "runtime", "state", "claude_hook_sessions"),
		mcpGatewayManifests:        map[string]mcpGatewayServerManifest{},
		mcpGatewayApprovalRequests: make(map[string]pendingMCPGatewayApprovalRequest),
		mcpGatewayLaunchedServers:  make(map[string]*mcpGatewayLaunchedServer),
		sandboxPaths:               sandbox.PathsForRepo(repoRoot),
		policy:                     policy,
		policyContentSHA256:        policyLoadResult.ContentSHA256,
		runtimeConfig:              runtimeConfig,
		registry:                   nil,
		checker:                    nil,
		now:                        time.Now,
		auditLedgerRuntime:         ledger.NewAppendRuntime(),
		resolveSecretStore:         secrets.NewStoreForRef,
		reportResponseWriteError: func(httpStatus int, cause error) {
			fmt.Fprintf(os.Stderr, "ERROR: response_write status=%d class=%s\n", httpStatus, secrets.LoopgateOperatorErrorClass(cause))
		},
		reportSecurityWarning: func(eventCode string, cause error) {
			fmt.Fprintf(os.Stderr, "WARN: security event=%s class=%s\n", eventCode, secrets.LoopgateOperatorErrorClass(cause))
		},
		resolvePeerIdentity: peerIdentityFromConn,
		resolveExePath:      resolveExecutablePath,
		processExists:       processExists,
		resolveUserHomeDir:  os.UserHomeDir,
		providerRuntime: providerRuntimeState{
			tokens:                 make(map[string]providerAccessToken),
			configuredConnections:  configuredConnections,
			configuredCapabilities: configuredCapabilities,
		},
		httpClient: &http.Client{Timeout: time.Duration(policy.Tools.HTTP.TimeoutSeconds) * time.Second},
		pkceRuntime: pkceRuntimeState{
			sessions: make(map[string]pendingPKCESession),
		},
		sessionState: sessionControlState{
			sessions:  make(map[string]controlSession),
			tokens:    make(map[string]capabilityToken),
			openByUID: make(map[uint32]time.Time),
		},
		approvalState: approvalControlState{
			records:    make(map[string]pendingApproval),
			tokenIndex: make(map[string]string),
		},
		replayState: replayControlState{
			seenRequests:              make(map[string]seenRequest),
			seenAuthNonces:            make(map[string]seenRequest),
			usedTokens:                make(map[string]usedToken),
			sessionReadCounts:         make(map[string][]time.Time),
			authDeniedBursts:          make(map[string]authDeniedBurst),
			hookPreValidateCounts:     make(map[uint32][]time.Time),
			hookPeerAuthFailureCounts: make(map[string][]time.Time),
		},
		sessionOpenMinInterval:               defaultSessionOpenMinInterval,
		maxActiveSessionsPerUID:              defaultMaxActiveSessionsPerUID,
		expirySweepMaxInterval:               defaultExpirySweepMaxInterval,
		fsReadRateLimit:                      defaultFsReadRateLimit,
		hookPreValidateRateLimit:             defaultHookPreValidateRateLimit,
		hookPreValidateRateWindow:            hookPreValidateRateWindow,
		hookPeerAuthFailureRateLimit:         defaultHookPeerAuthFailureRateLimit,
		hookPeerAuthFailureWindow:            hookPeerAuthFailureRateWindow,
		maxPendingApprovalsPerControlSession: defaultMaxPendingApprovalsPerControlSession,
		maxSeenRequestReplayEntries:          defaultMaxSeenRequestReplayEntries,
		maxAuthNonceReplayEntries:            defaultMaxAuthNonceReplayEntries,
		maxTotalControlSessions:              defaultMaxTotalControlSessions,
		maxTotalApprovalRecords:              defaultMaxTotalApprovalRecords,
		hostAccessRuntime: hostAccessRuntimeState{
			plans:         make(map[string]*hostAccessStoredPlan),
			appliedPlanAt: make(map[string]time.Time),
		},
	}
	server.nonceReplayStore = appendOnlyNonceReplayStore{
		path:               server.noncePath,
		legacySnapshotPath: filepath.Join(repoRoot, "runtime", "state", "nonce_replay.json"),
	}
	if pin := normalizeSessionExecutablePinPath(runtimeConfig.ControlPlane.ExpectedSessionClientExecutable); pin != "" {
		server.expectedClientPath = pin
	}
	initialPolicyRuntime, err := server.buildPolicyRuntime(policyLoadResult, cloneConfiguredCapabilities(configuredCapabilities))
	if err != nil {
		return nil, err
	}
	server.storePolicyRuntime(initialPolicyRuntime)
	server.appendAuditEvent = func(path string, auditEvent ledger.Event) error {
		if server.auditLedgerRuntime != nil {
			return server.auditLedgerRuntime.AppendWithRotation(path, auditEvent, server.auditLedgerRotationSettings())
		}
		return ledger.AppendWithRotation(path, auditEvent, server.auditLedgerRotationSettings())
	}
	if err := server.sandboxPaths.Ensure(); err != nil {
		return nil, fmt.Errorf("ensure sandbox paths: %w", err)
	}
	loadedConnections, err := loadConnectionRecords(server.connectionPath)
	if err != nil {
		return nil, fmt.Errorf("load connection records: %w", err)
	}
	server.connectionRuntime.records = loadedConnections
	if err := server.ensureDefaultAuditLedgerCheckpointSecret(context.Background()); err != nil {
		return nil, fmt.Errorf("ensure default audit checkpoint secret: %w", err)
	}
	if err := server.loadAuditChainState(); err != nil {
		return nil, fmt.Errorf("load audit chain state: %w", err)
	}
	if err := server.loadOrInitAuditExportState(); err != nil {
		return nil, fmt.Errorf("load audit export state: %w", err)
	}
	if err := server.loadNonceReplayState(); err != nil {
		return nil, fmt.Errorf("load nonce replay state: %w", err)
	}
	if err := server.loadOrCreateSessionMACRotationMaster(); err != nil {
		return nil, fmt.Errorf("session mac rotation master: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", server.handleHealth)
	mux.HandleFunc("/v1/status", server.handleStatus)
	mux.HandleFunc("/v1/control/approvals", server.handleControlApprovals)
	mux.HandleFunc("/v1/control/approvals/", server.handleControlApprovalDecision)
	mux.HandleFunc("/v1/ui/status", server.handleUIStatus)
	mux.HandleFunc("/v1/ui/operator-mount-write-grants", server.handleUIOperatorMountWriteGrants)
	mux.HandleFunc("/v1/ui/events", server.handleUIEvents)
	mux.HandleFunc("/v1/ui/events/recent", server.handleUIRecentEvents)
	mux.HandleFunc("/v1/ui/approvals", server.handleUIApprovals)
	mux.HandleFunc("/v1/ui/approvals/", server.handleUIApprovalDecision)
	mux.HandleFunc("/v1/ui/folder-access", server.handleFolderAccess)
	mux.HandleFunc("/v1/ui/folder-access/sync", server.handleFolderAccessSync)
	mux.HandleFunc("/v1/ui/shared-folder", server.handleSharedFolderStatus)
	mux.HandleFunc("/v1/ui/shared-folder/sync", server.handleSharedFolderSync)
	mux.HandleFunc("/v1/diagnostic/report", server.handleDiagnosticReport)
	mux.HandleFunc("/v1/mcp-gateway/inventory", server.handleMCPGatewayInventory)
	mux.HandleFunc("/v1/mcp-gateway/server/status", server.handleMCPGatewayServerStatus)
	mux.HandleFunc("/v1/mcp-gateway/decision", server.handleMCPGatewayDecision)
	mux.HandleFunc("/v1/mcp-gateway/server/ensure-launched", server.handleMCPGatewayEnsureLaunched)
	mux.HandleFunc("/v1/mcp-gateway/server/stop", server.handleMCPGatewayServerStop)
	mux.HandleFunc("/v1/mcp-gateway/invocation/validate", server.handleMCPGatewayInvocationValidate)
	mux.HandleFunc("/v1/mcp-gateway/invocation/request-approval", server.handleMCPGatewayInvocationRequestApproval)
	mux.HandleFunc("/v1/mcp-gateway/invocation/decide-approval", server.handleMCPGatewayInvocationDecideApproval)
	mux.HandleFunc("/v1/mcp-gateway/invocation/validate-execution", server.handleMCPGatewayInvocationValidateExecution)
	mux.HandleFunc("/v1/mcp-gateway/invocation/execute", server.handleMCPGatewayInvocationExecute)
	mux.HandleFunc("/v1/audit/export/flush", server.handleAuditExportFlush)
	mux.HandleFunc("/v1/audit/export/trust-check", server.handleAuditExportTrustCheck)
	mux.HandleFunc("/v1/session/open", server.handleSessionOpen)
	mux.HandleFunc("/v1/session/close", server.handleSessionClose)
	mux.HandleFunc("/v1/session/mac-keys", server.handleSessionMACKeys)
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
	mux.HandleFunc("/v1/quarantine/metadata", server.handleQuarantineMetadata)
	mux.HandleFunc("/v1/quarantine/view", server.handleQuarantineView)
	mux.HandleFunc("/v1/quarantine/prune", server.handleQuarantinePrune)
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
	if err := removeStaleSocketPath(server.socketPath); err != nil {
		return fmt.Errorf("remove stale socket: %w", err)
	}

	listener, err := listenPrivateUnixSocket(server.socketPath)
	if err != nil {
		return fmt.Errorf("listen unix socket: %w", err)
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
		shutdownContext, cancel := context.WithTimeout(context.Background(), defaultServerShutdownGracePeriod)
		defer cancel()
		_ = server.server.Shutdown(shutdownContext)
	}()

	serveErr := server.server.Serve(listener)
	// Give the nonce replay store a chance to compact or checkpoint durable state on shutdown.
	_ = server.saveNonceReplayState()
	if serveErr == nil || serveErr == http.ErrServerClosed {
		return nil
	}
	return serveErr
}

func (server *Server) executeCapabilityRequest(ctx context.Context, tokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest, allowApprovalCreation bool) controlapipkg.CapabilityResponse {
	normalizedRequest, earlyResponse := server.prepareCapabilityRequestExecution(tokenClaims, capabilityRequest, allowApprovalCreation)
	if earlyResponse != nil {
		return *earlyResponse
	}
	capabilityRequest = normalizedRequest

	policyRuntime := server.currentPolicyRuntime()
	tool, earlyResponse := server.resolveCapabilityExecutionTool(policyRuntime, tokenClaims, capabilityRequest)
	if earlyResponse != nil {
		return *earlyResponse
	}

	originalPolicyDecision, policyDecision, lowRiskHostPlanAutoAllowed := server.evaluateCapabilityPolicyDecision(policyRuntime, tool, tokenClaims, capabilityRequest)
	if originalPolicyDecision.Decision == policypkg.NeedsApproval && policyDecision.Decision == policypkg.Allow && lowRiskHostPlanAutoAllowed {
		if err := server.logEvent("capability.low_risk_host_plan_auto_allow", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
		}); err != nil {
			return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
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
		return controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "control-plane audit is unavailable",
			DenialCode:   controlapipkg.DenialCodeAuditUnavailable,
		}
	}

	if policyDecision.Decision == policypkg.Deny {
		if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"reason":               secrets.RedactText(policyDecision.Reason),
			"denial_code":          controlapipkg.DenialCodePolicyDenied,
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
		}); err != nil {
			return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
		}
		deniedResponse := controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: policyDecision.Reason,
			DenialCode:   controlapipkg.DenialCodePolicyDenied,
		}
		server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, deniedResponse.DenialCode, deniedResponse.DenialReason)
		return deniedResponse
	}

	if policyDecision.Decision == policypkg.NeedsApproval && allowApprovalCreation {
		return server.createCapabilityApprovalResponse(tokenClaims, capabilityRequest, policyDecision)
	}
	if policyDecision.Decision == policypkg.NeedsApproval && !allowApprovalCreation && !tokenClaims.ApprovedExecution {
		if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"reason":               "capability requires approval and this route does not support approval creation",
			"denial_code":          controlapipkg.DenialCodeApprovalRequired,
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
		}); err != nil {
			return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
		}
		deniedResponse := controlapipkg.CapabilityResponse{
			RequestID:        capabilityRequest.RequestID,
			Status:           controlapipkg.ResponseStatusDenied,
			DenialReason:     "capability requires approval and this route does not support approval creation",
			DenialCode:       controlapipkg.DenialCodeApprovalRequired,
			ApprovalRequired: true,
		}
		server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, deniedResponse.DenialCode, deniedResponse.DenialReason)
		return deniedResponse
	}

	effectiveTokenClaims, earlyResponse := server.prepareCapabilityExecution(tokenClaims, capabilityRequest, policyDecision, tool)
	if earlyResponse != nil {
		return *earlyResponse
	}

	if specialResponse, handled := server.dispatchDirectCapabilityExecution(effectiveTokenClaims, capabilityRequest); handled {
		return specialResponse
	}

	output, earlyResponse := server.executeCapabilityTool(ctx, tool, effectiveTokenClaims, capabilityRequest)
	if earlyResponse != nil {
		return *earlyResponse
	}

	return server.finalizeCapabilityExecution(effectiveTokenClaims, capabilityRequest, output)
}

func (server *Server) prepareCapabilityRequestExecution(tokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest, allowApprovalCreation bool) (controlapipkg.CapabilityRequest, *controlapipkg.CapabilityResponse) {
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
		return capabilityRequest, &controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   controlapipkg.DenialCodeMalformedRequest,
		}
	}
	if allowApprovalCreation {
		if replayDenied := server.recordRequest(tokenClaims.ControlSessionID, capabilityRequest); replayDenied != nil {
			return capabilityRequest, replayDenied
		}
	}
	return capabilityRequest, nil
}

func (server *Server) resolveCapabilityExecutionTool(policyRuntime serverPolicyRuntime, tokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest) (toolspkg.Tool, *controlapipkg.CapabilityResponse) {
	tool := policyRuntime.registry.Get(capabilityRequest.Capability)
	if server.capabilityProhibitsRawSecretExport(tool, capabilityRequest.Capability) {
		if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"reason":               "raw secret export is prohibited",
			"denial_code":          controlapipkg.DenialCodeSecretExportProhibited,
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
		}); err != nil {
			auditUnavailable := auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
			return nil, &auditUnavailable
		}
		deniedResponse := controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "raw secret export is prohibited",
			DenialCode:   controlapipkg.DenialCodeSecretExportProhibited,
			Redacted:     true,
		}
		server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, deniedResponse.DenialCode, deniedResponse.DenialReason)
		return nil, &deniedResponse
	}
	if len(tokenClaims.AllowedCapabilities) > 0 {
		if _, allowed := tokenClaims.AllowedCapabilities[capabilityRequest.Capability]; !allowed {
			if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
				"request_id":           capabilityRequest.RequestID,
				"capability":           capabilityRequest.Capability,
				"reason":               "capability token scope denied requested capability",
				"denial_code":          controlapipkg.DenialCodeCapabilityTokenScopeDenied,
				"actor_label":          tokenClaims.ActorLabel,
				"client_session_label": tokenClaims.ClientSessionLabel,
				"control_session_id":   tokenClaims.ControlSessionID,
			}); err != nil {
				auditUnavailable := auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
				return nil, &auditUnavailable
			}
			deniedResponse := controlapipkg.CapabilityResponse{
				RequestID:    capabilityRequest.RequestID,
				Status:       controlapipkg.ResponseStatusDenied,
				DenialReason: "capability token scope denied requested capability",
				DenialCode:   controlapipkg.DenialCodeCapabilityTokenScopeDenied,
			}
			server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, deniedResponse.DenialCode, deniedResponse.DenialReason)
			return nil, &deniedResponse
		}
	}
	if tool == nil {
		if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"reason":               "unknown capability",
			"denial_code":          controlapipkg.DenialCodeUnknownCapability,
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
		}); err != nil {
			auditUnavailable := auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
			return nil, &auditUnavailable
		}
		deniedResponse := controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "unknown capability",
			DenialCode:   controlapipkg.DenialCodeUnknownCapability,
		}
		server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, deniedResponse.DenialCode, deniedResponse.DenialReason)
		return nil, &deniedResponse
	}
	if err := tool.Schema().Validate(capabilityRequest.Arguments); err != nil {
		if auditErr := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"reason":               secrets.RedactText(err.Error()),
			"denial_code":          controlapipkg.DenialCodeInvalidCapabilityArguments,
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
		}); auditErr != nil {
			auditUnavailable := auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
			return nil, &auditUnavailable
		}
		errorResponse := controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   controlapipkg.DenialCodeInvalidCapabilityArguments,
			Redacted:     true,
		}
		server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, errorResponse.DenialCode, errorResponse.DenialReason)
		return nil, &errorResponse
	}
	return tool, nil
}

func (server *Server) evaluateCapabilityPolicyDecision(policyRuntime serverPolicyRuntime, tool toolspkg.Tool, tokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest) (policypkg.CheckResult, policypkg.CheckResult, bool) {
	policyDecision := policyRuntime.checker.Check(tool)
	if argumentValidator, ok := tool.(toolspkg.PolicyArgumentValidator); ok && policyDecision.Decision != policypkg.Deny {
		if err := argumentValidator.ValidatePolicyArguments(capabilityRequest.Arguments); err != nil {
			policyDecision = policypkg.CheckResult{
				Decision: policypkg.Deny,
				Reason:   err.Error(),
			}
		}
	}
	originalPolicyDecision := policyDecision
	lowRiskHostPlanAutoAllowed := false
	if operatorMountGrant, granted, grantErr := operatorMountWriteGrantForRequest(server, tokenClaims.ControlSessionID, capabilityRequest); grantErr == nil && granted {
		policyDecision = policypkg.CheckResult{
			Decision: policypkg.Allow,
			Reason:   "active operator-mounted write grant for " + operatorMountGrant.root,
		}
	}
	if adjustedDecision, adjusted := server.autoAllowLowRiskHostPlanApply(tokenClaims.ControlSessionID, capabilityRequest, policyDecision); adjusted {
		policyDecision = adjustedDecision
		lowRiskHostPlanAutoAllowed = true
	}
	return originalPolicyDecision, policyDecision, lowRiskHostPlanAutoAllowed
}

func (server *Server) createCapabilityApprovalResponse(tokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest, policyDecision policypkg.CheckResult) controlapipkg.CapabilityResponse {
	approvalID, err := randomHex(8)
	if err != nil {
		return controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "failed to create approval request",
			DenialCode:   controlapipkg.DenialCodeApprovalCreationFailed,
		}
	}

	decisionNonce, err := randomHex(16)
	if err != nil {
		return controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "failed to create approval decision nonce",
			DenialCode:   controlapipkg.DenialCodeApprovalCreationFailed,
		}
	}

	metadata := server.approvalMetadata(tokenClaims.ControlSessionID, capabilityRequest)
	approvalReason := approvalReasonForCapability(policyDecision, metadata, capabilityRequest)
	expiresAt := server.now().UTC().Add(approvalTTL)
	manifestSHA256, bodySHA256, manifestErr := buildCapabilityApprovalManifest(capabilityRequest, expiresAt.UnixMilli())
	if manifestErr != nil {
		return controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "failed to compute approval manifest",
			DenialCode:   controlapipkg.DenialCodeApprovalCreationFailed,
		}
	}
	server.mu.Lock()
	server.pruneExpiredLocked()
	if len(server.approvalState.records) >= server.maxTotalApprovalRecords {
		server.mu.Unlock()
		if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"reason":               "control-plane approval store is at capacity",
			"denial_code":          controlapipkg.DenialCodeControlPlaneStateSaturated,
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
		}); err != nil {
			return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
		}
		deniedResponse := controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "control-plane approval store is at capacity",
			DenialCode:   controlapipkg.DenialCodeControlPlaneStateSaturated,
		}
		server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, deniedResponse.DenialCode, deniedResponse.DenialReason)
		return deniedResponse
	}
	if server.countPendingApprovalsForSessionLocked(tokenClaims.ControlSessionID) >= server.maxPendingApprovalsPerControlSession {
		server.mu.Unlock()
		if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"reason":               "pending approval limit reached for control session",
			"denial_code":          controlapipkg.DenialCodePendingApprovalLimitReached,
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
		}); err != nil {
			return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
		}
		deniedResponse := controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "pending approval limit reached for control session",
			DenialCode:   controlapipkg.DenialCodePendingApprovalLimitReached,
		}
		server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, deniedResponse.DenialCode, deniedResponse.DenialReason)
		return deniedResponse
	}
	createdApproval := pendingApproval{
		ID:               approvalID,
		Request:          cloneCapabilityRequest(capabilityRequest),
		CreatedAt:        server.now().UTC(),
		ExpiresAt:        expiresAt,
		Metadata:         metadata,
		Reason:           approvalReason,
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
	server.approvalState.records[approvalID] = createdApproval
	server.noteExpiryCandidateLocked(expiresAt)

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
		"tenant_id":                tokenClaims.TenantID,
		"user_id":                  tokenClaims.UserID,
	}
	if approvalClass, ok := metadata["approval_class"].(string); ok && strings.TrimSpace(approvalClass) != "" {
		approvalCreatedAuditData["approval_class"] = approvalClass
	}
	if err := server.logEvent("approval.created", tokenClaims.ControlSessionID, approvalCreatedAuditData); err != nil {
		delete(server.approvalState.records, approvalID)
		server.mu.Unlock()
		return controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "control-plane audit is unavailable",
			DenialCode:   controlapipkg.DenialCodeAuditUnavailable,
		}
	}
	server.mu.Unlock()

	metadata["approval_reason"] = approvalReason
	metadata["approval_expires_at_utc"] = expiresAt.Format(time.RFC3339Nano)
	metadata["approval_decision_nonce"] = decisionNonce
	metadata["approval_manifest_sha256"] = manifestSHA256
	pendingResponse := controlapipkg.CapabilityResponse{
		RequestID:              capabilityRequest.RequestID,
		Status:                 controlapipkg.ResponseStatusPendingApproval,
		DenialCode:             controlapipkg.DenialCodeApprovalRequired,
		ApprovalRequired:       true,
		ApprovalRequestID:      approvalID,
		ApprovalManifestSHA256: manifestSHA256,
		Metadata:               metadata,
	}
	createdApproval.Metadata = metadata
	createdApproval.Reason = approvalReason
	server.emitUIApprovalPending(createdApproval)
	return pendingResponse
}

func (server *Server) prepareCapabilityExecution(tokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest, policyDecision policypkg.CheckResult, tool toolspkg.Tool) (capabilityToken, *controlapipkg.CapabilityResponse) {
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
			auditUnavailable := auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
			return effectiveTokenClaims, &auditUnavailable
		}
		server.emitUIToolDenied(effectiveTokenClaims.ControlSessionID, capabilityRequest, denialResponse.DenialCode, denialResponse.DenialReason)
		return effectiveTokenClaims, &denialResponse
	}
	if capabilityRequest.Capability == "fs_read" || capabilityRequest.Capability == "operator_mount.fs_read" {
		if denied := server.checkFsReadRateLimit(effectiveTokenClaims.ControlSessionID); denied {
			if auditErr := server.logEvent("capability.denied", effectiveTokenClaims.ControlSessionID, map[string]interface{}{
				"request_id":           capabilityRequest.RequestID,
				"capability":           capabilityRequest.Capability,
				"reason":               "fs_read rate limit exceeded",
				"denial_code":          controlapipkg.DenialCodeFsReadRateLimitExceeded,
				"actor_label":          effectiveTokenClaims.ActorLabel,
				"client_session_label": effectiveTokenClaims.ClientSessionLabel,
				"control_session_id":   effectiveTokenClaims.ControlSessionID,
			}); auditErr != nil {
				auditUnavailable := auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
				return effectiveTokenClaims, &auditUnavailable
			}
			deniedResponse := controlapipkg.CapabilityResponse{
				RequestID:    capabilityRequest.RequestID,
				Status:       controlapipkg.ResponseStatusDenied,
				DenialReason: "fs_read rate limit exceeded",
				DenialCode:   controlapipkg.DenialCodeFsReadRateLimitExceeded,
			}
			return effectiveTokenClaims, &deniedResponse
		}
	}
	return effectiveTokenClaims, nil
}

func (server *Server) dispatchDirectCapabilityExecution(effectiveTokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest) (controlapipkg.CapabilityResponse, bool) {
	switch capabilityRequest.Capability {
	case "host.folder.list":
		return server.executeHostFolderListCapability(effectiveTokenClaims, capabilityRequest), true
	case "host.folder.read":
		return server.executeHostFolderReadCapability(effectiveTokenClaims, capabilityRequest), true
	case "host.organize.plan":
		return server.executeHostOrganizePlanCapability(effectiveTokenClaims, capabilityRequest), true
	case "host.plan.apply":
		return server.executeHostPlanApplyCapability(effectiveTokenClaims, capabilityRequest), true
	default:
		return controlapipkg.CapabilityResponse{}, false
	}
}

func (server *Server) executeCapabilityTool(ctx context.Context, tool toolspkg.Tool, effectiveTokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest) (string, *controlapipkg.CapabilityResponse) {
	executionContext := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		executionContext, cancel = context.WithTimeout(ctx, defaultCapabilityExecutionTimeout)
		defer cancel()
	}
	executionContext = withOperatorMountControlSession(executionContext, effectiveTokenClaims.ControlSessionID)

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
			auditUnavailable := auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
			return "", &auditUnavailable
		}
		errorResponse := controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   controlapipkg.DenialCodeExecutionFailed,
			Redacted:     true,
		}
		server.emitUIEvent(effectiveTokenClaims.ControlSessionID, controlapipkg.UIEventTypeWarning, controlapipkg.UIEventWarning{
			Message: "capability execution failed: " + secrets.RedactText(err.Error()),
		})
		return "", &errorResponse
	}
	return output, nil
}

func (server *Server) finalizeCapabilityExecution(effectiveTokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest, output string) controlapipkg.CapabilityResponse {
	var (
		quarantineRef string
		err           error
	)
	if _, configuredCapability := server.configuredCapabilitySnapshot(capabilityRequest.Capability); configuredCapability {
		quarantineRef, err = server.storeQuarantinedPayload(capabilityRequest, output)
		if err != nil {
			return server.capabilityQuarantinePersistenceFailureResponse(effectiveTokenClaims, capabilityRequest, err)
		}
	}

	structuredResult, fieldsMeta, classification, builtQuarantineRef, err := server.buildCapabilityResult(capabilityRequest, output, quarantineRef)
	if err != nil {
		return controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   controlapipkg.DenialCodeExecutionFailed,
			Redacted:     true,
		}
	}
	if quarantineRef == "" && classification.Quarantined() {
		quarantineRef, err = server.storeQuarantinedPayload(capabilityRequest, output)
		if err != nil {
			return server.capabilityQuarantinePersistenceFailureResponse(effectiveTokenClaims, capabilityRequest, err)
		}
	}
	if strings.TrimSpace(builtQuarantineRef) != "" {
		quarantineRef = builtQuarantineRef
	}
	classification, err = normalizeResultClassification(classification, quarantineRef)
	if err != nil {
		return controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "capability result classification is invalid",
			DenialCode:   controlapipkg.DenialCodeExecutionFailed,
			Redacted:     true,
		}
	}
	resultMetadata := server.capabilityProvenanceMetadata(capabilityRequest.Capability, quarantineRef)
	if resultMetadata == nil {
		resultMetadata = make(map[string]interface{})
	}
	resultMetadata["prompt_eligible"] = classification.PromptEligible()
	resultMetadata["display_only"] = classification.DisplayOnly()
	resultMetadata["audit_only"] = classification.AuditOnly()
	resultMetadata["quarantined"] = classification.Quarantined()
	if err := server.logEvent("capability.executed", effectiveTokenClaims.ControlSessionID, map[string]interface{}{
		"request_id":            capabilityRequest.RequestID,
		"capability":            capabilityRequest.Capability,
		"status":                controlapipkg.ResponseStatusSuccess,
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
	successResponse := controlapipkg.CapabilityResponse{
		RequestID:        capabilityRequest.RequestID,
		Status:           controlapipkg.ResponseStatusSuccess,
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

func (server *Server) capabilityQuarantinePersistenceFailureResponse(effectiveTokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest, quarantineErr error) controlapipkg.CapabilityResponse {
	wrappedErr := fmt.Errorf("quarantine persistence failed: %w", quarantineErr)
	if auditErr := server.logEvent("capability.error", effectiveTokenClaims.ControlSessionID, map[string]interface{}{
		"request_id":           capabilityRequest.RequestID,
		"capability":           capabilityRequest.Capability,
		"error":                secrets.RedactText(wrappedErr.Error()),
		"operator_error_class": secrets.LoopgateOperatorErrorClass(wrappedErr),
		"actor_label":          effectiveTokenClaims.ActorLabel,
		"client_session_label": effectiveTokenClaims.ClientSessionLabel,
		"control_session_id":   effectiveTokenClaims.ControlSessionID,
		"token_id":             effectiveTokenClaims.TokenID,
		"parent_token_id":      effectiveTokenClaims.ParentTokenID,
	}); auditErr != nil {
		return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
	}
	server.emitUIEvent(effectiveTokenClaims.ControlSessionID, controlapipkg.UIEventTypeWarning, controlapipkg.UIEventWarning{
		Message: "capability quarantine persistence failed",
	})
	return controlapipkg.CapabilityResponse{
		RequestID:    capabilityRequest.RequestID,
		Status:       controlapipkg.ResponseStatusError,
		DenialReason: "capability quarantine persistence failed",
		DenialCode:   controlapipkg.DenialCodeExecutionFailed,
		Redacted:     true,
	}
}

func (server *Server) capabilitySummaries() []controlapipkg.CapabilitySummary {
	registeredTools := server.currentPolicyRuntime().registry.All()
	capabilities := make([]controlapipkg.CapabilitySummary, 0, len(registeredTools))
	for _, registeredTool := range registeredTools {
		capabilities = append(capabilities, controlapipkg.CapabilitySummary{
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

func (server *Server) activeSessionsForPeerUIDLocked(peerUID uint32) int {
	activeSessionCount := 0
	for _, activeSession := range server.sessionState.sessions {
		if activeSession.PeerIdentity.UID == peerUID {
			activeSessionCount++
		}
	}
	return activeSessionCount
}

func (server *Server) checkFsReadRateLimit(controlSessionID string) bool {
	server.mu.Lock()
	defer server.mu.Unlock()

	if server.fsReadRateLimit <= 0 {
		return false
	}

	nowUTC := server.now().UTC()
	cutoff := nowUTC.Add(-fsReadRateWindow)

	timestamps := server.replayState.sessionReadCounts[controlSessionID]
	// Prune old entries.
	pruned := make([]time.Time, 0, len(timestamps))
	for _, ts := range timestamps {
		if ts.After(cutoff) {
			pruned = append(pruned, ts)
		}
	}
	if len(pruned) >= server.fsReadRateLimit {
		server.replayState.sessionReadCounts[controlSessionID] = pruned
		return true
	}
	server.replayState.sessionReadCounts[controlSessionID] = append(pruned, nowUTC)
	return false
}

func (server *Server) checkHookPreValidateRateLimit(peerUID uint32) bool {
	server.mu.Lock()
	defer server.mu.Unlock()

	if server.hookPreValidateRateLimit <= 0 || server.hookPreValidateRateWindow <= 0 {
		return false
	}

	nowUTC := server.now().UTC()
	cutoff := nowUTC.Add(-server.hookPreValidateRateWindow)

	timestamps := server.replayState.hookPreValidateCounts[peerUID]
	pruned := make([]time.Time, 0, len(timestamps))
	for _, timestamp := range timestamps {
		if timestamp.After(cutoff) {
			pruned = append(pruned, timestamp)
		}
	}
	if len(pruned) >= server.hookPreValidateRateLimit {
		server.replayState.hookPreValidateCounts[peerUID] = pruned
		return true
	}
	server.replayState.hookPreValidateCounts[peerUID] = append(pruned, nowUTC)
	return false
}

func (server *Server) checkHookPeerAuthFailureRateLimit(rateLimitKey string) bool {
	server.mu.Lock()
	defer server.mu.Unlock()

	if server.hookPeerAuthFailureRateLimit <= 0 || server.hookPeerAuthFailureWindow <= 0 {
		return false
	}

	trimmedRateLimitKey := strings.TrimSpace(rateLimitKey)
	if trimmedRateLimitKey == "" {
		trimmedRateLimitKey = "unknown"
	}

	nowUTC := server.now().UTC()
	cutoff := nowUTC.Add(-server.hookPeerAuthFailureWindow)

	timestamps := server.replayState.hookPeerAuthFailureCounts[trimmedRateLimitKey]
	pruned := make([]time.Time, 0, len(timestamps))
	for _, timestamp := range timestamps {
		if timestamp.After(cutoff) {
			pruned = append(pruned, timestamp)
		}
	}
	if len(pruned) >= server.hookPeerAuthFailureRateLimit {
		server.replayState.hookPeerAuthFailureCounts[trimmedRateLimitKey] = pruned
		return true
	}
	server.replayState.hookPeerAuthFailureCounts[trimmedRateLimitKey] = append(pruned, nowUTC)
	return false
}

func randomHex(byteCount int) (string, error) {
	randomBytes := make([]byte, byteCount)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(randomBytes), nil
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
		return controlapipkg.DenialCodeQuarantinePruneNotEligible
	case errors.Is(quarantineErr, errQuarantinedSourceBytesRetained):
		return controlapipkg.DenialCodeSourceBytesUnavailable
	default:
		return controlapipkg.DenialCodeExecutionFailed
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
