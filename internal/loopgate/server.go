package loopgate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
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
	modelpkg "loopgate/internal/model"
	modelruntime "loopgate/internal/modelruntime"
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

// Locking invariant for Server state:
//   - Prefer holding exactly one of these mutex families at a time.
//   - Current production code treats auditMu, uiMu, connectionsMu,
//     modelConnectionsMu, hostAccessPlansMu, providerTokenMu, pkceMu, and
//     policyRuntimeMu as leaf-domain locks. Callers snapshot state under one
//     lock, release it, and only then cross into another domain.
//   - mu is the primary state lock for sessions, tokens, approvals, replay
//     tables, and other authoritative control-plane state.
//   - auditMu is a strict leaf lock for append-only audit sequencing and disk
//     persistence. Never acquire mu while holding auditMu. logEvent* helpers
//     intentionally resolve tenancy before taking auditMu so we never invert
//     into auditMu -> mu.
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
	modelConnectionPath        string
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
	resolveSecretStore         func(secrets.SecretRef) (secrets.SecretStore, error)
	reportResponseWriteError   func(httpStatus int, cause error)
	reportSecurityWarning      func(eventCode string, cause error)
	resolvePeerIdentity        func(net.Conn) (peerIdentity, error)
	resolveExePath             func(int) (string, error)
	processExists              func(int) (bool, error)
	resolveUserHomeDir         func() (string, error)
	expectedClientPath         string
	newModelClientFromConfig   func(modelruntime.Config) (*modelpkg.Client, modelruntime.Config, error)
	nonceReplayStore           authNonceReplayStore
	server                     *http.Server
	// diagnostic is optional operator text logging (runtime/logs); not authoritative audit.
	diagnostic *loopdiag.Manager

	auditMu                    sync.Mutex
	auditSequence              uint64
	lastAuditHash              string
	auditEventsSinceCheckpoint int

	auditExportMu sync.Mutex

	promotionMu sync.Mutex

	uiMu               sync.Mutex
	uiSequence         uint64
	uiEvents           []UIEventEnvelope
	uiSubscribers      map[int]uiEventSubscriber
	nextUISubscriberID int

	claudeHookSessionsMu sync.Mutex

	connectionsMu sync.Mutex
	connections   map[string]connectionRecord

	modelConnectionsMu sync.Mutex
	modelConnections   map[string]modelConnectionRecord

	hostAccessPlansMu sync.Mutex
	hostAccessPlans   map[string]*hostAccessStoredPlan
	// hostAccessAppliedPlanAt records plan IDs that completed host.plan.apply
	// successfully so a second apply with the same id returns a clear recovery
	// message instead of a generic "unknown" error.
	hostAccessAppliedPlanAt map[string]time.Time

	configStateDir string

	providerTokenMu        sync.Mutex
	providerTokens         map[string]providerAccessToken
	configuredConnections  map[string]configuredConnection
	configuredCapabilities map[string]configuredCapability
	httpClient             *http.Client
	policyRuntime          serverPolicyRuntime
	policyContentSHA256    string
	policyRuntimeMu        sync.RWMutex

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
	// maxTotalControlSessions caps active control sessions (fail closed when full).
	maxTotalControlSessions int
	// maxTotalApprovalRecords caps server.approvals map size including terminal rows until pruned.
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
	requestReplayWindow            = sessionTTL
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
		modelConnectionPath:        filepath.Join(repoRoot, "runtime", "state", "loopgate_model_connections.json"),
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
		resolveSecretStore:         secrets.NewStoreForRef,
		reportResponseWriteError: func(httpStatus int, cause error) {
			fmt.Fprintf(os.Stderr, "ERROR: response_write status=%d class=%s\n", httpStatus, secrets.LoopgateOperatorErrorClass(cause))
		},
		reportSecurityWarning: func(eventCode string, cause error) {
			fmt.Fprintf(os.Stderr, "WARN: security event=%s class=%s\n", eventCode, secrets.LoopgateOperatorErrorClass(cause))
		},
		resolvePeerIdentity:                  peerIdentityFromConn,
		resolveExePath:                       resolveExecutablePath,
		processExists:                        processExists,
		resolveUserHomeDir:                   os.UserHomeDir,
		providerTokens:                       make(map[string]providerAccessToken),
		configuredConnections:                configuredConnections,
		configuredCapabilities:               configuredCapabilities,
		httpClient:                           &http.Client{Timeout: time.Duration(policy.Tools.HTTP.TimeoutSeconds) * time.Second},
		pkceSessions:                         make(map[string]pendingPKCESession),
		sessions:                             make(map[string]controlSession),
		tokens:                               make(map[string]capabilityToken),
		approvals:                            make(map[string]pendingApproval),
		seenRequests:                         make(map[string]seenRequest),
		seenAuthNonces:                       make(map[string]seenRequest),
		usedTokens:                           make(map[string]usedToken),
		sessionOpenByUID:                     make(map[uint32]time.Time),
		approvalTokenIndex:                   make(map[string]string),
		sessionReadCounts:                    make(map[string][]time.Time),
		sessionOpenMinInterval:               defaultSessionOpenMinInterval,
		maxActiveSessionsPerUID:              defaultMaxActiveSessionsPerUID,
		expirySweepMaxInterval:               defaultExpirySweepMaxInterval,
		maxPendingApprovalsPerControlSession: defaultMaxPendingApprovalsPerControlSession,
		maxSeenRequestReplayEntries:          defaultMaxSeenRequestReplayEntries,
		maxAuthNonceReplayEntries:            defaultMaxAuthNonceReplayEntries,
		maxTotalControlSessions:              defaultMaxTotalControlSessions,
		maxTotalApprovalRecords:              defaultMaxTotalApprovalRecords,
		hostAccessPlans:                      make(map[string]*hostAccessStoredPlan),
		hostAccessAppliedPlanAt:              make(map[string]time.Time),
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
		return ledger.AppendWithRotation(path, auditEvent, server.auditLedgerRotationSettings())
	}
	server.newModelClientFromConfig = server.newModelClientFromRuntimeConfig
	if err := server.sandboxPaths.Ensure(); err != nil {
		return nil, fmt.Errorf("ensure sandbox paths: %w", err)
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
	mux.HandleFunc("/v1/model/reply", server.handleModelReply)
	mux.HandleFunc("/v1/model/validate", server.handleModelValidate)
	mux.HandleFunc("/v1/model/connections/store", server.handleModelConnectionStore)
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
	// Give the nonce replay store a chance to compact or checkpoint durable state on shutdown.
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

	policyRuntime := server.currentPolicyRuntime()
	tool := policyRuntime.registry.Get(capabilityRequest.Capability)
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

		metadata := server.approvalMetadata(tokenClaims.ControlSessionID, capabilityRequest)
		approvalReason := approvalReasonForCapability(policyDecision, metadata, capabilityRequest)
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
		if len(server.approvals) >= server.maxTotalApprovalRecords {
			server.mu.Unlock()
			if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
				"request_id":           capabilityRequest.RequestID,
				"capability":           capabilityRequest.Capability,
				"reason":               "control-plane approval store is at capacity",
				"denial_code":          DenialCodeControlPlaneStateSaturated,
				"actor_label":          tokenClaims.ActorLabel,
				"client_session_label": tokenClaims.ClientSessionLabel,
				"control_session_id":   tokenClaims.ControlSessionID,
			}); err != nil {
				return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
			}
			deniedResponse := CapabilityResponse{
				RequestID:    capabilityRequest.RequestID,
				Status:       ResponseStatusDenied,
				DenialReason: "control-plane approval store is at capacity",
				DenialCode:   DenialCodeControlPlaneStateSaturated,
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

		metadata["approval_reason"] = approvalReason
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
			Reason:           approvalReason,
			ControlSessionID: tokenClaims.ControlSessionID,
		})
		return pendingResponse
	}
	if policyDecision.Decision == policypkg.NeedsApproval && !allowApprovalCreation && !tokenClaims.ApprovedExecution {
		if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"request_id":           capabilityRequest.RequestID,
			"capability":           capabilityRequest.Capability,
			"reason":               "capability requires approval and this route does not support approval creation",
			"denial_code":          DenialCodeApprovalRequired,
			"actor_label":          tokenClaims.ActorLabel,
			"client_session_label": tokenClaims.ClientSessionLabel,
			"control_session_id":   tokenClaims.ControlSessionID,
		}); err != nil {
			return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
		}
		deniedResponse := CapabilityResponse{
			RequestID:        capabilityRequest.RequestID,
			Status:           ResponseStatusDenied,
			DenialReason:     "capability requires approval and this route does not support approval creation",
			DenialCode:       DenialCodeApprovalRequired,
			ApprovalRequired: true,
		}
		server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, deniedResponse.DenialCode, deniedResponse.DenialReason)
		return deniedResponse
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

	// Per-session rate limiting for fs_read and operator_mount.fs_read.
	if capabilityRequest.Capability == "fs_read" || capabilityRequest.Capability == "operator_mount.fs_read" {
		if denied := server.checkFsReadRateLimit(effectiveTokenClaims.ControlSessionID); denied {
			if auditErr := server.logEvent("capability.denied", effectiveTokenClaims.ControlSessionID, map[string]interface{}{
				"request_id":           capabilityRequest.RequestID,
				"capability":           capabilityRequest.Capability,
				"reason":               "fs_read rate limit exceeded",
				"denial_code":          DenialCodeFsReadRateLimitExceeded,
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
				DenialCode:   DenialCodeFsReadRateLimitExceeded,
			}
		}
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

func (server *Server) capabilitySummaries() []CapabilitySummary {
	registeredTools := server.currentPolicyRuntime().registry.All()
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

func (server *Server) activeSessionsForPeerUIDLocked(peerUID uint32) int {
	activeSessionCount := 0
	for _, activeSession := range server.sessions {
		if activeSession.PeerIdentity.UID == peerUID {
			activeSessionCount++
		}
	}
	return activeSessionCount
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
