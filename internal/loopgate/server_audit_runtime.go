package loopgate

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"loopgate/internal/auditruntime"
	"loopgate/internal/config"
	"loopgate/internal/ledger"
	"loopgate/internal/secrets"
)

func (server *Server) loadAuditChainState() error {
	server.configureAuditRuntime()
	return server.auditRuntime.Load(context.Background(), server.auditLedgerRotationSettings())
}

func (server *Server) configureAuditRuntime() {
	if server == nil {
		return
	}
	server.auditRuntime = auditruntime.New(auditruntime.Options{
		Path:          server.auditPath,
		AnchorPath:    filepath.Join(server.repoRoot, "runtime", "state", "audit_ledger_anchor.json"),
		LedgerRuntime: server.auditLedgerRuntime,
		Now:           server.now,
		Append: func(path string, auditEvent ledger.Event) error {
			if server.appendAuditEvent == nil {
				return fmt.Errorf("audit append function is nil")
			}
			return server.appendAuditEvent(path, auditEvent)
		},
		HMACCheckpointConfig: func() config.AuditLedgerHMACCheckpoint {
			return server.runtimeConfig.Logging.AuditLedger.HMACCheckpoint
		},
		LoadCheckpointSecret: server.loadAuditLedgerCheckpointSecret,
		TenancyForSession:    server.tenantUserForControlSession,
		AfterAppend:          server.diagnosticTextAfterAuditEvent,
	})
}

func (server *Server) ensureDefaultAuditLedgerCheckpointSecret(ctx context.Context) error {
	if server == nil {
		return nil
	}
	hmacCheckpointConfig := server.runtimeConfig.Logging.AuditLedger.HMACCheckpoint
	if !hmacCheckpointConfig.Enabled || !config.IsDefaultAuditLedgerHMACSecretRef(hmacCheckpointConfig.SecretRef) {
		return nil
	}

	validatedSecretRef := secrets.SecretRef{
		ID:          strings.TrimSpace(hmacCheckpointConfig.SecretRef.ID),
		Backend:     strings.TrimSpace(hmacCheckpointConfig.SecretRef.Backend),
		AccountName: strings.TrimSpace(hmacCheckpointConfig.SecretRef.AccountName),
		Scope:       strings.TrimSpace(hmacCheckpointConfig.SecretRef.Scope),
	}
	if err := validatedSecretRef.Validate(); err != nil {
		return fmt.Errorf("validate default audit ledger hmac checkpoint secret ref: %w", err)
	}

	secretStore, err := server.secretStoreForRef(validatedSecretRef)
	if err != nil {
		return fmt.Errorf("resolve default audit ledger hmac checkpoint secret store: %w", err)
	}
	rawSecretBytes, _, err := secretStore.Get(ctx, validatedSecretRef)
	if err == nil {
		zeroSecretBytes(rawSecretBytes)
		return nil
	}
	if !errors.Is(err, secrets.ErrSecretNotFound) {
		return fmt.Errorf("load default audit ledger hmac checkpoint secret: %w", err)
	}

	bootstrapSecretBytes := make([]byte, 32)
	if _, err := rand.Read(bootstrapSecretBytes); err != nil {
		return fmt.Errorf("generate default audit ledger hmac checkpoint secret: %w", err)
	}
	defer zeroSecretBytes(bootstrapSecretBytes)
	if _, err := secretStore.Put(ctx, validatedSecretRef, bootstrapSecretBytes); err != nil {
		return fmt.Errorf("store default audit ledger hmac checkpoint secret: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Loopgate bootstrapped the default audit HMAC checkpoint key in macOS Keychain.")
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

// mergeAuditTenancyFromControlSession stamps tenant_id/user_id when absent. Call this before
// logEvent from code that already holds server.mu. tenantUserForControlSession must not run
// under the same goroutine while server.mu is held because sync.Mutex is not reentrant.
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
// It acquires server.mu before auditRuntime takes its internal commit lock so logEvent
// stays free of lock-order inversions.
func (server *Server) tenantUserForControlSession(controlSessionID string) (tenantID string, userID string) {
	if strings.TrimSpace(controlSessionID) == "" {
		return "", ""
	}
	server.mu.Lock()
	session, found := server.sessionState.sessions[controlSessionID]
	server.mu.Unlock()
	if !found {
		return "", ""
	}
	return session.TenantID, session.UserID
}

func (server *Server) logEvent(eventType string, sessionID string, data map[string]interface{}) error {
	_, err := server.logEventWithHash(eventType, sessionID, data)
	return err
}

func (server *Server) logEventWithHash(eventType string, sessionID string, data map[string]interface{}) (string, error) {
	if server.auditRuntime == nil {
		server.configureAuditRuntime()
	}
	return server.auditRuntime.Record(eventType, sessionID, data)
}

func (server *Server) loadAuditLedgerCheckpointSecret(ctx context.Context) ([]byte, error) {
	hmacSecretRef := server.runtimeConfig.Logging.AuditLedger.HMACCheckpoint.SecretRef
	if hmacSecretRef == nil {
		return nil, fmt.Errorf("%w: audit ledger hmac checkpoint secret ref is missing", secrets.ErrSecretValidation)
	}

	validatedSecretRef := secrets.SecretRef{
		ID:          strings.TrimSpace(hmacSecretRef.ID),
		Backend:     strings.TrimSpace(hmacSecretRef.Backend),
		AccountName: strings.TrimSpace(hmacSecretRef.AccountName),
		Scope:       strings.TrimSpace(hmacSecretRef.Scope),
	}
	if err := validatedSecretRef.Validate(); err != nil {
		return nil, fmt.Errorf("validate audit ledger hmac checkpoint secret ref: %w", err)
	}
	secretStore, err := server.secretStoreForRef(validatedSecretRef)
	if err != nil {
		return nil, fmt.Errorf("resolve audit ledger hmac checkpoint secret store: %w", err)
	}
	rawSecretBytes, _, err := secretStore.Get(ctx, validatedSecretRef)
	if err != nil {
		return nil, fmt.Errorf("load audit ledger hmac checkpoint secret: %w", err)
	}
	if len(rawSecretBytes) == 0 {
		return nil, fmt.Errorf("%w: audit ledger hmac checkpoint secret is empty", secrets.ErrSecretValidation)
	}
	return rawSecretBytes, nil
}

func (server *Server) diagnosticTextAfterAuditEvent(auditEvent ledger.Event) {
	if server.diagnostic == nil {
		return
	}
	hashPrefix := ""
	if auditEvent.Data != nil {
		if hashValue, ok := auditEvent.Data["event_hash"].(string); ok && hashValue != "" {
			hashPrefix = hashValue
			if len(hashPrefix) > 16 {
				hashPrefix = hashPrefix[:16]
			}
		}
	}
	var auditSequence any
	if auditEvent.Data != nil {
		auditSequence = auditEvent.Data["audit_sequence"]
	}
	tenantID, userID := "", ""
	if auditEvent.Data != nil {
		if value, ok := auditEvent.Data["tenant_id"].(string); ok {
			tenantID = value
		}
		if value, ok := auditEvent.Data["user_id"].(string); ok {
			userID = value
		}
	}
	if server.diagnostic.Audit != nil {
		server.diagnostic.Audit.Debug("audit_persisted",
			"type", auditEvent.Type,
			"session", auditEvent.Session,
			"tenant_id", tenantID,
			"user_id", userID,
			"audit_sequence", auditSequence,
			"event_hash_prefix", hashPrefix,
		)
	}
	if server.diagnostic.Ledger != nil {
		server.diagnostic.Ledger.Debug("ledger_append",
			"type", auditEvent.Type,
			"session", auditEvent.Session,
			"tenant_id", tenantID,
			"user_id", userID,
			"audit_sequence", auditSequence,
			"event_hash_prefix", hashPrefix,
		)
	}
	server.diagnosticServerControlPlaneFromAuditEvent(auditEvent)
}

// CloseDiagnosticLogs closes optional text log files. Safe to call multiple times.
func (server *Server) CloseDiagnosticLogs() {
	if server == nil || server.diagnostic == nil {
		return
	}
	_ = server.diagnostic.Close()
	server.diagnostic = nil
}

// AuditIntegrityModeMessage returns a one-line stdout message describing the current
// audit integrity posture so operators know which mode is active at startup.
//
// Two modes exist:
//   - hash-chain only (default): each event commits a SHA-256 digest of its predecessor;
//     ordering changes and corruption are detectable on read, but a same-user attacker
//     who controls the log directory can replace the file with a new consistent chain.
//   - hash-chain + HMAC checkpoints: additionally binds cumulative chain state to an
//     out-of-band secret; replacement requires forging a keyed MAC.
func (server *Server) AuditIntegrityModeMessage() string {
	if server == nil {
		return ""
	}
	return auditruntime.IntegrityModeMessage(server.runtimeConfig.Logging.AuditLedger.HMACCheckpoint)
}

// DiagnosticLogDirectoryMessage returns a stderr hint when operator diagnostic slog files are active.
// Those files (server.log, socket.log, client.log, …) are separate from shell-redirected stdout such as runtime/logs/loopgate.log.
func (server *Server) DiagnosticLogDirectoryMessage() string {
	if server == nil || server.diagnostic == nil {
		return ""
	}
	relativeDir := server.runtimeConfig.Logging.Diagnostic.ResolvedDirectory()
	diagnosticDir := filepath.Join(server.repoRoot, relativeDir)
	absoluteDiagnosticDir, err := filepath.Abs(diagnosticDir)
	if err != nil {
		absoluteDiagnosticDir = diagnosticDir
	}
	return fmt.Sprintf("Loopgate diagnostic slog files: %s (server.log, socket.log, client.log, …). "+
		"runtime/logs/loopgate.log is only start.sh stdout/stderr, not these.", absoluteDiagnosticDir)
}

func (server *Server) auditRuntimeSnapshot() auditruntime.State {
	if server == nil || server.auditRuntime == nil {
		return auditruntime.State{}
	}
	return server.auditRuntime.Snapshot()
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

func hashAuditEvent(auditEvent ledger.Event) (string, error) {
	return ledger.ComputeEventHash(auditEvent)
}
