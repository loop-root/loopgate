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
	"time"

	"loopgate/internal/config"
	"loopgate/internal/controlruntime"
	"loopgate/internal/ledger"
	"loopgate/internal/loopdiag"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"loopgate/internal/sandbox"
	"loopgate/internal/secrets"
)

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
		resolvePeerIdentity:      peerIdentityFromConn,
		resolveExePath:           resolveExecutablePath,
		processExists:            processExists,
		resolveUserHomeDir:       os.UserHomeDir,
		httpRequestSlots:         make(chan struct{}, runtimeConfig.ControlPlane.MaxInFlightHTTPRequests),
		capabilityExecutionSlots: make(chan struct{}, runtimeConfig.ControlPlane.MaxInFlightCapabilityExecutions),
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
	server.nonceReplayStore = controlruntime.NewAppendOnlyNonceReplayStore(server.noncePath, filepath.Join(repoRoot, "runtime", "state", "nonce_replay.json"), requestReplayWindow)
	if pin := normalizeSessionExecutablePinPath(runtimeConfig.ControlPlane.ExpectedSessionClientExecutable); pin != "" {
		server.expectedClientPath = pin
	}
	initialPolicyRuntime, err := server.buildPolicyRuntime(policyLoadResult, cloneConfiguredCapabilities(configuredCapabilities))
	if err != nil {
		return nil, err
	}
	server.storePolicyRuntime(initialPolicyRuntime)
	initialOperatorOverrideRuntime, err := server.reloadOperatorOverrideRuntimeFromDisk()
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARN: operator overrides unavailable class=%s\n", secrets.LoopgateOperatorErrorClass(err))
		server.storeOperatorOverrideRuntime(serverOperatorOverrideRuntime{
			document: config.OperatorOverrideDocument{
				Version: "1",
				Grants:  []config.OperatorOverrideGrant{},
			},
		})
	} else {
		server.storeOperatorOverrideRuntime(initialOperatorOverrideRuntime)
	}
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
	server.registerRoutes(mux)

	diagnostic, diagErr := loopdiag.Open(repoRoot, server.runtimeConfig.Logging.Diagnostic)
	if diagErr != nil {
		return nil, fmt.Errorf("open diagnostic logs: %w", diagErr)
	}
	server.diagnostic = diagnostic
	handler := server.wrapHTTPHandler(mux)
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
	saveNonceReplayErr := server.saveNonceReplayState()
	if saveNonceReplayErr != nil {
		if server.diagnostic != nil && server.diagnostic.Server != nil {
			server.diagnostic.Server.Error("nonce_replay_shutdown_save_failed",
				"operator_error_class", "state_persist_failed",
				"error", saveNonceReplayErr.Error(),
			)
		}
		saveNonceReplayErr = fmt.Errorf("save nonce replay state on shutdown: %w", saveNonceReplayErr)
		if serveErr == nil || serveErr == http.ErrServerClosed {
			return saveNonceReplayErr
		}
		return errors.Join(serveErr, saveNonceReplayErr)
	}
	if serveErr == nil || serveErr == http.ErrServerClosed {
		return nil
	}
	return serveErr
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
