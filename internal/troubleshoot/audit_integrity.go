package troubleshoot

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
	"loopgate/internal/secrets"
)

// AuditLedgerCheckpointReport is a derived operator-facing status view for
// optional HMAC checkpoints on the authoritative audit ledger.
type AuditLedgerCheckpointReport struct {
	Enabled                            bool   `json:"enabled"`
	Configured                         bool   `json:"configured"`
	OK                                 bool   `json:"ok"`
	Status                             string `json:"status,omitempty"`
	Error                              string `json:"error,omitempty"`
	CheckpointCount                    int    `json:"checkpoint_count,omitempty"`
	LastCheckpointThroughAuditSequence int64  `json:"last_checkpoint_through_audit_sequence,omitempty"`
	LastCheckpointTimestampUTC         string `json:"last_checkpoint_timestamp_utc,omitempty"`
	OrdinaryEventsSinceLastCheckpoint  int    `json:"ordinary_events_since_last_checkpoint,omitempty"`
}

func VerifyAuditLedgerCheckpoints(repoRoot string, runtimeConfig config.RuntimeConfig) (AuditLedgerCheckpointReport, error) {
	report := AuditLedgerCheckpointReport{
		Enabled: runtimeConfig.Logging.AuditLedger.HMACCheckpoint.Enabled,
	}
	if !report.Enabled {
		report.OK = true
		report.Status = "disabled"
		return report, nil
	}

	hmacSecretRef := runtimeConfig.Logging.AuditLedger.HMACCheckpoint.SecretRef
	if hmacSecretRef == nil {
		report.Status = "misconfigured"
		report.Error = "hmac checkpoint secret_ref is missing"
		return report, fmt.Errorf("audit ledger hmac checkpoint secret_ref is missing")
	}

	validatedSecretRef := secrets.SecretRef{
		ID:          strings.TrimSpace(hmacSecretRef.ID),
		Backend:     strings.TrimSpace(hmacSecretRef.Backend),
		AccountName: strings.TrimSpace(hmacSecretRef.AccountName),
		Scope:       strings.TrimSpace(hmacSecretRef.Scope),
	}
	if err := validatedSecretRef.Validate(); err != nil {
		report.Status = "misconfigured"
		report.Error = err.Error()
		return report, fmt.Errorf("validate audit ledger hmac checkpoint secret ref: %w", err)
	}
	report.Configured = true

	orderedPaths, err := ledger.OrderedSegmentedPaths(
		ActiveAuditPath(repoRoot),
		AuditRotationSettings(repoRoot, runtimeConfig),
	)
	if err != nil {
		report.Status = "error"
		report.Error = err.Error()
		return report, fmt.Errorf("ordered audit ledger paths: %w", err)
	}

	secretStore, err := secrets.NewStoreForRef(validatedSecretRef)
	if err != nil {
		report.Status = "error"
		report.Error = err.Error()
		return report, fmt.Errorf("resolve audit ledger hmac checkpoint secret store: %w", err)
	}

	rawSecretBytes, _, err := secretStore.Get(context.Background(), validatedSecretRef)
	if err != nil {
		if errors.Is(err, secrets.ErrSecretNotFound) && config.IsDefaultAuditLedgerHMACSecretRef(hmacSecretRef) && len(orderedPaths) == 0 {
			report.OK = true
			report.Status = "bootstrap_pending"
			report.Error = "default keychain-backed audit checkpoint key will be created on first Loopgate start"
			return report, nil
		}
		report.Status = "error"
		report.Error = err.Error()
		return report, fmt.Errorf("load audit ledger hmac checkpoint secret: %w", err)
	}
	defer zeroSecretBytes(rawSecretBytes)
	if len(rawSecretBytes) == 0 {
		report.Status = "error"
		report.Error = "audit ledger hmac checkpoint secret is empty"
		return report, fmt.Errorf("audit ledger hmac checkpoint secret is empty")
	}

	inspection, err := ledger.InspectAuditHMACCheckpoints(orderedPaths, rawSecretBytes)
	if err != nil {
		report.Status = "error"
		report.Error = err.Error()
		return report, fmt.Errorf("verify audit ledger hmac checkpoints: %w", err)
	}

	report.OK = true
	report.Status = "verified"
	report.CheckpointCount = inspection.CheckpointCount
	report.LastCheckpointThroughAuditSequence = inspection.LastCheckpointThroughAuditSequence
	report.LastCheckpointTimestampUTC = inspection.LastCheckpointTimestampUTC
	report.OrdinaryEventsSinceLastCheckpoint = inspection.OrdinaryEventsSinceLastCheckpoint
	return report, nil
}

func zeroSecretBytes(rawSecretBytes []byte) {
	for index := range rawSecretBytes {
		rawSecretBytes[index] = 0
	}
}
