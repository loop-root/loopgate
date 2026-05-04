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
	"loopgate/internal/ledger"
	"loopgate/internal/loopdiag"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	policypkg "loopgate/internal/policy"
	"loopgate/internal/sandbox"
	"loopgate/internal/secrets"
	toolspkg "loopgate/internal/tools"
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
		resolvePeerIdentity: peerIdentityFromConn,
		resolveExePath:      resolveExecutablePath,
		processExists:       processExists,
		resolveUserHomeDir:  os.UserHomeDir,
		httpRequestSlots:    make(chan struct{}, runtimeConfig.ControlPlane.MaxInFlightHTTPRequests),
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
		requestID, err := randomHex(8)
		if err != nil {
			return capabilityRequest, &controlapipkg.CapabilityResponse{
				Status:       controlapipkg.ResponseStatusError,
				DenialReason: "allocate request_id: " + err.Error(),
				DenialCode:   controlapipkg.DenialCodeExecutionFailed,
			}
		}
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
