package loopgate

import (
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
	"loopgate/internal/loopdiag"
	"loopgate/internal/loopgate/auditruntime"
	controlapipkg "loopgate/internal/loopgate/controlapi"
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
//   - auditRuntime owns append-only audit sequencing and disk persistence.
//     Its internal lock is a strict leaf lock: never acquire mu from inside
//     audit runtime callbacks. logEvent* resolves tenancy before audit commit
//     so we never invert into audit runtime -> mu.
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
	// httpRequestSlots bounds concurrent HTTP handler execution so a local
	// hammering loop cannot create unbounded goroutines waiting on audit/fsync,
	// control-plane locks, or capability execution.
	httpRequestSlotsMu sync.RWMutex
	httpRequestSlots   chan struct{}

	// auditRuntime owns append-only audit chain sequencing state and keeps each
	// hash-chain append as one logical commit.
	auditRuntime *auditruntime.Runtime

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
	//   - keep promotionMu isolated from mu/audit runtime; collect control-plane
	//     context first, then enter promotion code
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
	providerRuntime         providerRuntimeState
	httpClient              *http.Client
	policyRuntime           serverPolicyRuntime
	policyContentSHA256     string
	operatorOverrideRuntime serverOperatorOverrideRuntime

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
	// operatorOverrideRuntimeMu protects the current signed operator override
	// document snapshot. This stays separate from policyRuntimeMu so the checked-in
	// signed parent policy remains distinguishable from the local delegated layer.
	operatorOverrideRuntimeMu sync.RWMutex

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
