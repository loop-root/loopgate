package secrets

import (
	"fmt"
	"sort"
	"time"

	"loopgate/internal/audit"
	"loopgate/internal/ledger"
)

// AppendSecretMetadataEvent records secret reference + lifecycle metadata only.
// This function is ledger-safe by design and never writes raw secret material.
func AppendSecretMetadataEvent(
	ledgerPath string,
	sessionID string,
	eventType string,
	validatedRef SecretRef,
	secretMetadata SecretMetadata,
	rawDetails map[string]interface{},
) error {
	if err := validatedRef.Validate(); err != nil {
		return fmt.Errorf("validate secret ref: %w", err)
	}
	if eventType == "" {
		return fmt.Errorf("%w: missing event type", ErrSecretValidation)
	}

	ledgerData := map[string]interface{}{
		"secret_ref": map[string]interface{}{
			"id":           validatedRef.ID,
			"backend":      validatedRef.Backend,
			"account_name": validatedRef.AccountName,
			"scope":        validatedRef.Scope,
		},
		"metadata": secretMetadataToLedgerMap(secretMetadata),
	}

	if len(rawDetails) > 0 {
		ledgerData["details"] = ledgerSafeDetailSummary(rawDetails)
	}

	ledgerEvent := ledger.Event{
		TS:      time.Now().UTC().Format(time.RFC3339Nano),
		Type:    eventType,
		Session: sessionID,
		Data:    ledgerData,
	}
	if err := audit.RecordMustPersist(ledgerPath, ledgerEvent); err != nil {
		return fmt.Errorf("append ledger event: %w", err)
	}
	return nil
}

func ledgerSafeDetailSummary(rawDetails map[string]interface{}) map[string]interface{} {
	keys := make([]string, 0, len(rawDetails))
	for rawKey := range rawDetails {
		keys = append(keys, rawKey)
	}
	sort.Strings(keys)
	return map[string]interface{}{
		"redacted": true,
		"keys":     keys,
	}
}

func secretMetadataToLedgerMap(secretMetadata SecretMetadata) map[string]interface{} {
	metadataMap := map[string]interface{}{
		"status":      secretMetadata.Status,
		"scope":       secretMetadata.Scope,
		"fingerprint": secretMetadata.Fingerprint,
	}
	if !secretMetadata.CreatedAt.IsZero() {
		metadataMap["created_at_utc"] = secretMetadata.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	if !secretMetadata.LastUsedAt.IsZero() {
		metadataMap["last_used_at_utc"] = secretMetadata.LastUsedAt.UTC().Format(time.RFC3339Nano)
	}
	if !secretMetadata.LastRotatedAt.IsZero() {
		metadataMap["last_rotated_at_utc"] = secretMetadata.LastRotatedAt.UTC().Format(time.RFC3339Nano)
	}
	if secretMetadata.ExpiresAt != nil {
		metadataMap["expires_at_utc"] = secretMetadata.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	return metadataMap
}
