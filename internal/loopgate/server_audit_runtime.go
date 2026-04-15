package loopgate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"loopgate/internal/audit"
	"loopgate/internal/ledger"
	"loopgate/internal/secrets"
)

func (server *Server) loadAuditChainState() error {
	lastAuditSequence, lastAuditHash, err := ledger.ReadSegmentedChainState(server.auditPath, "audit_sequence", server.auditLedgerRotationSettings())
	if err != nil {
		return err
	}
	server.auditSequence = uint64(lastAuditSequence)
	server.lastAuditHash = lastAuditHash
	server.auditEventsSinceCheckpoint = 0
	if !server.runtimeConfig.Logging.AuditLedger.HMACCheckpoint.Enabled {
		return nil
	}

	orderedPaths, err := ledger.OrderedSegmentedPaths(server.auditPath, server.auditLedgerRotationSettings())
	if err != nil {
		return fmt.Errorf("ordered audit ledger paths: %w", err)
	}
	rawSecretBytes, err := server.loadAuditLedgerCheckpointSecret(context.Background())
	if err != nil {
		return err
	}
	defer zeroSecretBytes(rawSecretBytes)

	checkpointInspection, err := ledger.InspectAuditHMACCheckpoints(orderedPaths, rawSecretBytes)
	if err != nil {
		return fmt.Errorf("inspect audit hmac checkpoints: %w", err)
	}
	server.auditEventsSinceCheckpoint = checkpointInspection.OrdinaryEventsSinceLastCheckpoint
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
	server.auditEventsSinceCheckpoint++
	server.diagnosticTextAfterAuditEvent(auditEvent)
	if err := server.appendAuditHMACCheckpointIfDueLocked(); err != nil {
		return err
	}
	return nil
}

func (server *Server) appendAuditHMACCheckpointIfDueLocked() error {
	hmacCheckpointConfig := server.runtimeConfig.Logging.AuditLedger.HMACCheckpoint
	if !hmacCheckpointConfig.Enabled || hmacCheckpointConfig.IntervalEvents <= 0 {
		return nil
	}
	if server.auditEventsSinceCheckpoint < hmacCheckpointConfig.IntervalEvents {
		return nil
	}

	rawSecretBytes, err := server.loadAuditLedgerCheckpointSecret(context.Background())
	if err != nil {
		return err
	}
	defer zeroSecretBytes(rawSecretBytes)

	checkpointTimestampUTC := server.now().UTC().Format(time.RFC3339Nano)
	checkpointMAC := ledger.ComputeAuditLedgerCheckpointHMAC(
		rawSecretBytes,
		ledger.BuildAuditLedgerCheckpointHMACMessageV1(
			int64(server.auditSequence),
			server.lastAuditHash,
			checkpointTimestampUTC,
		),
	)
	checkpointData := map[string]interface{}{
		"checkpoint_schema_version": int64(ledger.AuditLedgerCheckpointSchemaVersion),
		"through_audit_sequence":    int64(server.auditSequence),
		"through_event_hash":        server.lastAuditHash,
		"checkpoint_timestamp_utc":  checkpointTimestampUTC,
		"checkpoint_hmac_sha256":    hex.EncodeToString(checkpointMAC),
	}

	nextSequence := server.auditSequence + 1
	checkpointData["audit_sequence"] = nextSequence
	checkpointData["ledger_sequence"] = nextSequence
	checkpointData["previous_event_hash"] = server.lastAuditHash
	canonicalData, err := canonicalizeAuditData(checkpointData)
	if err != nil {
		return fmt.Errorf("canonicalize audit checkpoint data: %w", err)
	}
	checkpointData = canonicalData

	checkpointEvent := ledger.Event{
		TS:      checkpointTimestampUTC,
		Type:    ledger.AuditLedgerHMACCheckpointEventType,
		Session: "",
		Data:    checkpointData,
	}
	checkpointHash, err := hashAuditEvent(checkpointEvent)
	if err != nil {
		return fmt.Errorf("hash audit checkpoint event: %w", err)
	}
	checkpointEvent.Data["event_hash"] = checkpointHash

	if err := audit.NewLedgerWriter(server.appendAuditEvent, nil).Record(server.auditPath, audit.ClassMustPersist, checkpointEvent); err != nil {
		return err
	}
	server.auditSequence = nextSequence
	server.lastAuditHash = checkpointHash
	server.auditEventsSinceCheckpoint = 0
	server.diagnosticTextAfterAuditEvent(checkpointEvent)
	return nil
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
	relativeDir := server.runtimeConfig.Logging.Diagnostic.ResolvedDirectory()
	diagnosticDir := filepath.Join(server.repoRoot, relativeDir)
	absoluteDiagnosticDir, err := filepath.Abs(diagnosticDir)
	if err != nil {
		absoluteDiagnosticDir = diagnosticDir
	}
	return fmt.Sprintf("Loopgate diagnostic slog files: %s (server.log, socket.log, client.log, …). "+
		"runtime/logs/loopgate.log is only start.sh stdout/stderr, not these.", absoluteDiagnosticDir)
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
