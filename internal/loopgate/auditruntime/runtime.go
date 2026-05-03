package auditruntime

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"time"

	"loopgate/internal/audit"
	"loopgate/internal/config"
	"loopgate/internal/ledger"
)

type AppendFunc func(string, ledger.Event) error
type CheckpointSecretLoader func(context.Context) ([]byte, error)
type HMACCheckpointConfigFunc func() config.AuditLedgerHMACCheckpoint
type NowFunc func() time.Time
type ObserverFunc func(ledger.Event)
type TenancyLookupFunc func(sessionID string) (tenantID, userID string)

type Options struct {
	Path                 string
	AnchorPath           string
	Append               AppendFunc
	LedgerRuntime        *ledger.AppendRuntime
	Now                  NowFunc
	HMACCheckpointConfig HMACCheckpointConfigFunc
	LoadCheckpointSecret CheckpointSecretLoader
	TenancyForSession    TenancyLookupFunc
	AfterAppend          ObserverFunc
}

type State struct {
	Sequence              uint64
	LastHash              string
	EventsSinceCheckpoint int
}

type Runtime struct {
	mu sync.Mutex

	path                 string
	anchorPath           string
	append               AppendFunc
	ledgerRuntime        *ledger.AppendRuntime
	now                  NowFunc
	hmacCheckpointConfig HMACCheckpointConfigFunc
	loadCheckpointSecret CheckpointSecretLoader
	tenancyForSession    TenancyLookupFunc
	afterAppend          ObserverFunc

	sequence              uint64
	lastHash              string
	eventsSinceCheckpoint int
}

func New(options Options) *Runtime {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Runtime{
		path:                 options.Path,
		anchorPath:           options.AnchorPath,
		append:               options.Append,
		ledgerRuntime:        options.LedgerRuntime,
		now:                  now,
		hmacCheckpointConfig: options.HMACCheckpointConfig,
		loadCheckpointSecret: options.LoadCheckpointSecret,
		tenancyForSession:    options.TenancyForSession,
		afterAppend:          options.AfterAppend,
	}
}

func (runtime *Runtime) Load(ctx context.Context, rotationSettings ledger.RotationSettings) error {
	if runtime == nil {
		return fmt.Errorf("audit runtime is nil")
	}

	var (
		lastAuditSequence int64
		lastAuditHash     string
		err               error
	)
	if runtime.ledgerRuntime != nil {
		lastAuditSequence, lastAuditHash, err = runtime.ledgerRuntime.ReadSegmentedChainState(runtime.path, "audit_sequence", rotationSettings)
	} else {
		lastAuditSequence, lastAuditHash, err = ledger.ReadSegmentedChainState(runtime.path, "audit_sequence", rotationSettings)
	}
	if err != nil {
		return err
	}

	runtime.mu.Lock()
	runtime.sequence = uint64(lastAuditSequence)
	runtime.lastHash = lastAuditHash
	runtime.eventsSinceCheckpoint = 0
	runtime.mu.Unlock()

	hmacCheckpointConfig := runtime.currentHMACCheckpointConfig()
	if !hmacCheckpointConfig.Enabled {
		return nil
	}

	rawSecretBytes, err := runtime.loadCheckpointSecretBytes(ctx)
	if err != nil {
		return err
	}
	defer zeroSecretBytes(rawSecretBytes)

	anchor, anchorFound, err := runtime.loadAndVerifyAnchor(rawSecretBytes, uint64(lastAuditSequence), lastAuditHash)
	if err != nil {
		return err
	}
	eventsSinceCheckpoint := 0
	if anchorFound {
		eventsSinceCheckpoint = anchor.EventsSinceCheckpoint
	} else {
		orderedPaths, err := ledger.OrderedSegmentedPaths(runtime.path, rotationSettings)
		if err != nil {
			return fmt.Errorf("ordered audit ledger paths: %w", err)
		}
		checkpointInspection, err := ledger.InspectAuditHMACCheckpoints(orderedPaths, rawSecretBytes)
		if err != nil {
			return fmt.Errorf("inspect audit hmac checkpoints: %w", err)
		}
		eventsSinceCheckpoint = checkpointInspection.OrdinaryEventsSinceLastCheckpoint
		if err := runtime.storeAnchor(rawSecretBytes, uint64(lastAuditSequence), lastAuditHash, eventsSinceCheckpoint); err != nil {
			return err
		}
	}

	runtime.mu.Lock()
	runtime.eventsSinceCheckpoint = eventsSinceCheckpoint
	runtime.mu.Unlock()
	return nil
}

func (runtime *Runtime) Record(eventType string, sessionID string, data map[string]interface{}) (string, error) {
	if runtime == nil {
		return "", fmt.Errorf("audit runtime is nil")
	}
	if runtime.append == nil {
		return "", fmt.Errorf("audit append function is nil")
	}

	safeData := copyInterfaceMap(data)
	_, hasTenant := safeData["tenant_id"]
	_, hasUser := safeData["user_id"]
	if (!hasTenant || !hasUser) && runtime.tenancyForSession != nil {
		lookupTenantID, lookupUserID := runtime.tenancyForSession(sessionID)
		if !hasTenant {
			safeData["tenant_id"] = lookupTenantID
		}
		if !hasUser {
			safeData["user_id"] = lookupUserID
		}
	}

	runtime.mu.Lock()
	defer runtime.mu.Unlock()

	nextSequence := runtime.sequence + 1
	safeData["audit_sequence"] = nextSequence
	safeData["ledger_sequence"] = nextSequence
	safeData["previous_event_hash"] = runtime.lastHash

	auditEvent := ledger.Event{
		TS:      runtime.now().UTC().Format(time.RFC3339Nano),
		Type:    eventType,
		Session: sessionID,
		Data:    safeData,
	}
	eventHash, err := hashAuditEvent(auditEvent)
	if err != nil {
		return "", fmt.Errorf("hash audit event: %w", err)
	}
	auditEvent.Data["event_hash"] = eventHash

	if err := audit.NewLedgerWriter(runtime.append, nil).Record(runtime.path, audit.ClassMustPersist, auditEvent); err != nil {
		return "", err
	}
	runtime.sequence = nextSequence
	runtime.lastHash = eventHash
	runtime.eventsSinceCheckpoint++
	runtime.emitAfterAppend(auditEvent)
	if err := runtime.appendHMACCheckpointIfDueLocked(); err != nil {
		return "", err
	}
	if err := runtime.storeAnchorLocked(); err != nil {
		return "", err
	}
	return eventHash, nil
}

func (runtime *Runtime) Snapshot() State {
	if runtime == nil {
		return State{}
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return State{
		Sequence:              runtime.sequence,
		LastHash:              runtime.lastHash,
		EventsSinceCheckpoint: runtime.eventsSinceCheckpoint,
	}
}

func IntegrityModeMessage(hmacCheckpointConfig config.AuditLedgerHMACCheckpoint) string {
	if hmacCheckpointConfig.Enabled {
		interval := hmacCheckpointConfig.IntervalEvents
		if interval <= 0 {
			interval = config.DefaultAuditLedgerHMACCheckpointIntervalEvents
		}
		return fmt.Sprintf("Audit integrity: hash-chain + HMAC checkpoints (every %d events)", interval)
	}
	return "Audit integrity: hash-chain only (HMAC checkpoints disabled)"
}

func (runtime *Runtime) appendHMACCheckpointIfDueLocked() error {
	hmacCheckpointConfig := runtime.currentHMACCheckpointConfig()
	if !hmacCheckpointConfig.Enabled || hmacCheckpointConfig.IntervalEvents <= 0 {
		return nil
	}
	if runtime.eventsSinceCheckpoint < hmacCheckpointConfig.IntervalEvents {
		return nil
	}

	rawSecretBytes, err := runtime.loadCheckpointSecretBytes(context.Background())
	if err != nil {
		return err
	}
	defer zeroSecretBytes(rawSecretBytes)

	checkpointTimestampUTC := runtime.now().UTC().Format(time.RFC3339Nano)
	checkpointMAC := ledger.ComputeAuditLedgerCheckpointHMAC(
		rawSecretBytes,
		ledger.BuildAuditLedgerCheckpointHMACMessageV1(
			int64(runtime.sequence),
			runtime.lastHash,
			checkpointTimestampUTC,
		),
	)
	checkpointData := map[string]interface{}{
		"checkpoint_schema_version": int64(ledger.AuditLedgerCheckpointSchemaVersion),
		"through_audit_sequence":    int64(runtime.sequence),
		"through_event_hash":        runtime.lastHash,
		"checkpoint_timestamp_utc":  checkpointTimestampUTC,
		"checkpoint_hmac_sha256":    hex.EncodeToString(checkpointMAC),
	}

	nextSequence := runtime.sequence + 1
	checkpointData["audit_sequence"] = nextSequence
	checkpointData["ledger_sequence"] = nextSequence
	checkpointData["previous_event_hash"] = runtime.lastHash

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

	if err := audit.NewLedgerWriter(runtime.append, nil).Record(runtime.path, audit.ClassMustPersist, checkpointEvent); err != nil {
		return err
	}
	runtime.sequence = nextSequence
	runtime.lastHash = checkpointHash
	runtime.eventsSinceCheckpoint = 0
	runtime.emitAfterAppend(checkpointEvent)
	return nil
}

func (runtime *Runtime) loadAndVerifyAnchor(key []byte, lastAuditSequence uint64, lastAuditHash string) (auditLedgerAnchor, bool, error) {
	if runtime.anchorPath == "" {
		return auditLedgerAnchor{}, false, nil
	}
	anchor, found, err := loadAuditLedgerAnchor(runtime.anchorPath, key)
	if err != nil {
		return auditLedgerAnchor{}, false, err
	}
	if found {
		if anchor.AuditSequence != lastAuditSequence || anchor.LastEventHash != lastAuditHash {
			return auditLedgerAnchor{}, true, fmt.Errorf(
				"%w: audit ledger anchor head mismatch (anchor sequence=%d, ledger sequence=%d)",
				ledger.ErrLedgerIntegrity,
				anchor.AuditSequence,
				lastAuditSequence,
			)
		}
		activeSizeBytes, err := runtime.activeLedgerSizeBytes()
		if err != nil {
			return auditLedgerAnchor{}, true, err
		}
		if anchor.ActiveSizeBytes != activeSizeBytes {
			return auditLedgerAnchor{}, true, fmt.Errorf(
				"%w: audit ledger anchor active size mismatch (anchor bytes=%d, ledger bytes=%d)",
				ledger.ErrLedgerIntegrity,
				anchor.ActiveSizeBytes,
				activeSizeBytes,
			)
		}
		return anchor, true, nil
	}
	return auditLedgerAnchor{}, false, nil
}

func (runtime *Runtime) storeAnchorLocked() error {
	hmacCheckpointConfig := runtime.currentHMACCheckpointConfig()
	if !hmacCheckpointConfig.Enabled || runtime.anchorPath == "" {
		return nil
	}
	rawSecretBytes, err := runtime.loadCheckpointSecretBytes(context.Background())
	if err != nil {
		return err
	}
	defer zeroSecretBytes(rawSecretBytes)
	return runtime.storeAnchor(rawSecretBytes, runtime.sequence, runtime.lastHash, runtime.eventsSinceCheckpoint)
}

func (runtime *Runtime) storeAnchor(key []byte, auditSequence uint64, lastEventHash string, eventsSinceCheckpoint int) error {
	activeSizeBytes, err := runtime.activeLedgerSizeBytes()
	if err != nil {
		return err
	}
	return storeAuditLedgerAnchor(runtime.anchorPath, key, runtime.now().UTC(), auditSequence, lastEventHash, eventsSinceCheckpoint, activeSizeBytes)
}

func (runtime *Runtime) activeLedgerSizeBytes() (int64, error) {
	activeSizeBytes := int64(0)
	if runtime.path != "" {
		fileInfo, err := os.Stat(runtime.path)
		if err != nil {
			if !os.IsNotExist(err) {
				return 0, fmt.Errorf("stat audit ledger for anchor: %w", err)
			}
		} else {
			activeSizeBytes = fileInfo.Size()
		}
	}
	return activeSizeBytes, nil
}

func (runtime *Runtime) currentHMACCheckpointConfig() config.AuditLedgerHMACCheckpoint {
	if runtime == nil || runtime.hmacCheckpointConfig == nil {
		return config.AuditLedgerHMACCheckpoint{}
	}
	return runtime.hmacCheckpointConfig()
}

func (runtime *Runtime) loadCheckpointSecretBytes(ctx context.Context) ([]byte, error) {
	if runtime == nil || runtime.loadCheckpointSecret == nil {
		return nil, fmt.Errorf("audit ledger hmac checkpoint secret loader is nil")
	}
	return runtime.loadCheckpointSecret(ctx)
}

func (runtime *Runtime) emitAfterAppend(auditEvent ledger.Event) {
	if runtime.afterAppend != nil {
		runtime.afterAppend(auditEvent)
	}
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

func zeroSecretBytes(rawSecretBytes []byte) {
	for index := range rawSecretBytes {
		rawSecretBytes[index] = 0
	}
}
