package loopgate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"loopgate/internal/identifiers"
	"loopgate/internal/secrets"
)

type connectionRegistration struct {
	Provider   string
	GrantType  string
	Subject    string
	Scopes     []string
	Credential secrets.SecretRef
}

type connectionRecord struct {
	Provider           string            `json:"provider"`
	GrantType          string            `json:"grant_type"`
	Subject            string            `json:"subject"`
	Scopes             []string          `json:"scopes"`
	Credential         secrets.SecretRef `json:"credential"`
	Status             string            `json:"status"`
	CreatedAtUTC       string            `json:"created_at_utc"`
	LastValidatedAtUTC string            `json:"last_validated_at_utc,omitempty"`
	LastUsedAtUTC      string            `json:"last_used_at_utc,omitempty"`
	LastRotatedAtUTC   string            `json:"last_rotated_at_utc,omitempty"`
}

type connectionStateFile struct {
	Connections []connectionRecord `json:"connections"`
}

func isPublicReadGrantType(rawGrantType string) bool {
	return strings.TrimSpace(rawGrantType) == GrantTypePublicRead
}

func secretRefIsEmpty(secretRef secrets.SecretRef) bool {
	return strings.TrimSpace(secretRef.ID) == "" &&
		strings.TrimSpace(secretRef.Backend) == "" &&
		strings.TrimSpace(secretRef.AccountName) == "" &&
		strings.TrimSpace(secretRef.Scope) == ""
}

func (connectionRegistration connectionRegistration) Validate() error {
	if err := identifiers.ValidateSafeIdentifier("connection provider", connectionRegistration.Provider); err != nil {
		return err
	}
	if strings.TrimSpace(connectionRegistration.GrantType) != "" {
		if err := ValidateGrantType(connectionRegistration.GrantType); err != nil {
			return err
		}
	}
	if strings.TrimSpace(connectionRegistration.Subject) != "" {
		if err := identifiers.ValidateSafeIdentifier("connection subject", connectionRegistration.Subject); err != nil {
			return err
		}
	}
	for _, rawScope := range connectionRegistration.Scopes {
		if err := identifiers.ValidateSafeIdentifier("connection scope", rawScope); err != nil {
			return err
		}
	}
	if isPublicReadGrantType(connectionRegistration.GrantType) {
		if !secretRefIsEmpty(connectionRegistration.Credential) {
			return fmt.Errorf("public_read connections must not define a secret ref")
		}
		return nil
	}
	return connectionRegistration.Credential.Validate()
}

func (connectionRecord connectionRecord) Validate() error {
	if err := identifiers.ValidateSafeIdentifier("connection provider", connectionRecord.Provider); err != nil {
		return err
	}
	if strings.TrimSpace(connectionRecord.GrantType) != "" {
		if err := ValidateGrantType(connectionRecord.GrantType); err != nil {
			return err
		}
	}
	if strings.TrimSpace(connectionRecord.Subject) != "" {
		if err := identifiers.ValidateSafeIdentifier("connection subject", connectionRecord.Subject); err != nil {
			return err
		}
	}
	for _, rawScope := range connectionRecord.Scopes {
		if err := identifiers.ValidateSafeIdentifier("connection scope", rawScope); err != nil {
			return err
		}
	}
	if strings.TrimSpace(connectionRecord.Status) != "" {
		if err := identifiers.ValidateSafeIdentifier("connection status", connectionRecord.Status); err != nil {
			return err
		}
	}
	if isPublicReadGrantType(connectionRecord.GrantType) {
		if !secretRefIsEmpty(connectionRecord.Credential) {
			return fmt.Errorf("public_read connection records must not define a secret ref")
		}
		return nil
	}
	return connectionRecord.Credential.Validate()
}

func (connectionRecord connectionRecord) statusSummary() ConnectionStatus {
	secureStoreRefID := connectionRecord.Credential.ID
	if isPublicReadGrantType(connectionRecord.GrantType) {
		secureStoreRefID = "none"
	}
	return ConnectionStatus{
		Provider:           connectionRecord.Provider,
		GrantType:          connectionRecord.GrantType,
		Subject:            connectionRecord.Subject,
		Scopes:             append([]string(nil), connectionRecord.Scopes...),
		Status:             connectionRecord.Status,
		SecureStoreRefID:   secureStoreRefID,
		LastValidatedAtUTC: connectionRecord.LastValidatedAtUTC,
		LastUsedAtUTC:      connectionRecord.LastUsedAtUTC,
		LastRotatedAtUTC:   connectionRecord.LastRotatedAtUTC,
	}
}

func connectionRecordKey(provider string, subject string) string {
	return provider + ":" + subject
}

func normalizedConnectionScopes(rawScopes []string) []string {
	seenScopes := make(map[string]struct{}, len(rawScopes))
	normalizedScopes := make([]string, 0, len(rawScopes))
	for _, rawScope := range rawScopes {
		trimmedScope := strings.TrimSpace(rawScope)
		if trimmedScope == "" {
			continue
		}
		if _, found := seenScopes[trimmedScope]; found {
			continue
		}
		seenScopes[trimmedScope] = struct{}{}
		normalizedScopes = append(normalizedScopes, trimmedScope)
	}
	sort.Strings(normalizedScopes)
	return normalizedScopes
}

func normalizeConnectionRegistration(connectionRegistration connectionRegistration) connectionRegistration {
	connectionRegistration.Provider = strings.TrimSpace(connectionRegistration.Provider)
	connectionRegistration.GrantType = strings.TrimSpace(connectionRegistration.GrantType)
	connectionRegistration.Subject = strings.TrimSpace(connectionRegistration.Subject)
	connectionRegistration.Scopes = normalizedConnectionScopes(connectionRegistration.Scopes)
	return connectionRegistration
}

func loadConnectionRecords(path string) (map[string]connectionRecord, error) {
	rawConnectionBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]connectionRecord{}, nil
		}
		return nil, fmt.Errorf("read connection records: %w", err)
	}

	var stateFile connectionStateFile
	decoder := json.NewDecoder(bytes.NewReader(rawConnectionBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stateFile); err != nil {
		return nil, fmt.Errorf("decode connection records: %w", err)
	}

	connectionRecords := make(map[string]connectionRecord, len(stateFile.Connections))
	for _, loadedRecord := range stateFile.Connections {
		if err := loadedRecord.Validate(); err != nil {
			return nil, fmt.Errorf("validate connection record: %w", err)
		}
		connectionRecords[connectionRecordKey(loadedRecord.Provider, loadedRecord.Subject)] = loadedRecord
	}
	return connectionRecords, nil
}

func saveConnectionRecords(path string, connectionRecords map[string]connectionRecord) error {
	records := make([]connectionRecord, 0, len(connectionRecords))
	for _, connectionRecord := range connectionRecords {
		if err := connectionRecord.Validate(); err != nil {
			return fmt.Errorf("validate connection record: %w", err)
		}
		records = append(records, connectionRecord)
	}
	sort.Slice(records, func(leftIndex int, rightIndex int) bool {
		leftRecord := records[leftIndex]
		rightRecord := records[rightIndex]
		if leftRecord.Provider == rightRecord.Provider {
			return leftRecord.Subject < rightRecord.Subject
		}
		return leftRecord.Provider < rightRecord.Provider
	})

	stateFile := connectionStateFile{Connections: records}
	jsonBytes, err := json.MarshalIndent(stateFile, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal connection records: %w", err)
	}
	if len(jsonBytes) == 0 || jsonBytes[len(jsonBytes)-1] != '\n' {
		jsonBytes = append(jsonBytes, '\n')
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create connection state dir: %w", err)
	}

	connectionFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp connection state: %w", err)
	}
	tempPath := connectionFile.Name()
	cleanupTemp := func() {
		_ = connectionFile.Close()
		_ = os.Remove(tempPath)
	}
	if err := connectionFile.Chmod(0o600); err != nil {
		cleanupTemp()
		return fmt.Errorf("chmod temp connection state: %w", err)
	}

	if _, err := connectionFile.Write(jsonBytes); err != nil {
		cleanupTemp()
		return fmt.Errorf("write temp connection state: %w", err)
	}
	if err := connectionFile.Sync(); err != nil {
		cleanupTemp()
		return fmt.Errorf("sync temp connection state: %w", err)
	}
	if err := connectionFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("close temp connection state: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("rename temp connection state: %w", err)
	}
	if connectionDir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = connectionDir.Sync()
		_ = connectionDir.Close()
	}
	return nil
}

func (server *Server) connectionStatuses() []ConnectionStatus {
	server.connectionRuntime.mu.Lock()
	defer server.connectionRuntime.mu.Unlock()

	connectionStatuses := make([]ConnectionStatus, 0, len(server.connectionRuntime.records)+len(server.providerRuntime.configuredConnections))
	for _, connectionRecord := range server.connectionRuntime.records {
		connectionStatuses = append(connectionStatuses, connectionRecord.statusSummary())
	}
	for connectionKey, configuredConnectionDefinition := range server.providerRuntime.configuredConnections {
		if !isPublicReadGrantType(configuredConnectionDefinition.Registration.GrantType) {
			continue
		}
		if _, found := server.connectionRuntime.records[connectionKey]; found {
			continue
		}
		connectionStatuses = append(connectionStatuses, ConnectionStatus{
			Provider:           configuredConnectionDefinition.Registration.Provider,
			GrantType:          configuredConnectionDefinition.Registration.GrantType,
			Subject:            configuredConnectionDefinition.Registration.Subject,
			Scopes:             append([]string(nil), configuredConnectionDefinition.Registration.Scopes...),
			Status:             "public_configured",
			SecureStoreRefID:   "none",
			LastValidatedAtUTC: "never",
			LastUsedAtUTC:      "never",
			LastRotatedAtUTC:   "never",
		})
	}
	sort.Slice(connectionStatuses, func(leftIndex int, rightIndex int) bool {
		leftStatus := connectionStatuses[leftIndex]
		rightStatus := connectionStatuses[rightIndex]
		if leftStatus.Provider == rightStatus.Provider {
			return leftStatus.Subject < rightStatus.Subject
		}
		return leftStatus.Provider < rightStatus.Provider
	})
	return connectionStatuses
}

func (server *Server) RegisterConnection(ctx context.Context, registration connectionRegistration) (ConnectionStatus, error) {
	normalizedRegistration := normalizeConnectionRegistration(registration)
	if err := normalizedRegistration.Validate(); err != nil {
		return ConnectionStatus{}, err
	}

	secretStore, err := server.secretStoreForRef(normalizedRegistration.Credential)
	if err != nil {
		return ConnectionStatus{}, err
	}
	secretMetadata, err := secretStore.Metadata(ctx, normalizedRegistration.Credential)
	if err != nil {
		return ConnectionStatus{}, fmt.Errorf("validate connection secret ref: %w", err)
	}

	nowUTC := server.now().UTC()
	record := connectionRecord{
		Provider:           normalizedRegistration.Provider,
		GrantType:          normalizedRegistration.GrantType,
		Subject:            normalizedRegistration.Subject,
		Scopes:             append([]string(nil), normalizedRegistration.Scopes...),
		Credential:         normalizedRegistration.Credential,
		Status:             defaultLabel(secretMetadata.Status, "configured"),
		CreatedAtUTC:       nowUTC.Format(time.RFC3339Nano),
		LastValidatedAtUTC: nowUTC.Format(time.RFC3339Nano),
	}
	if !secretMetadata.LastUsedAt.IsZero() {
		record.LastUsedAtUTC = secretMetadata.LastUsedAt.UTC().Format(time.RFC3339Nano)
	}
	if !secretMetadata.LastRotatedAt.IsZero() {
		record.LastRotatedAtUTC = secretMetadata.LastRotatedAt.UTC().Format(time.RFC3339Nano)
	}

	server.connectionRuntime.mu.Lock()
	existingRecord, found := server.connectionRuntime.records[connectionRecordKey(record.Provider, record.Subject)]
	if found {
		record.CreatedAtUTC = existingRecord.CreatedAtUTC
		if existingRecord.LastUsedAtUTC != "" && record.LastUsedAtUTC == "" {
			record.LastUsedAtUTC = existingRecord.LastUsedAtUTC
		}
		if existingRecord.LastRotatedAtUTC != "" && record.LastRotatedAtUTC == "" {
			record.LastRotatedAtUTC = existingRecord.LastRotatedAtUTC
		}
	}
	updatedConnections := cloneConnectionRecords(server.connectionRuntime.records)
	updatedConnections[connectionRecordKey(record.Provider, record.Subject)] = record
	if err := saveConnectionRecords(server.connectionPath, updatedConnections); err != nil {
		server.connectionRuntime.mu.Unlock()
		return ConnectionStatus{}, err
	}
	server.connectionRuntime.records = updatedConnections
	server.connectionRuntime.mu.Unlock()
	server.invalidateProviderAccessToken(connectionRecordKey(record.Provider, record.Subject))

	return record.statusSummary(), nil
}

func (server *Server) UpsertConnectionCredential(ctx context.Context, registration connectionRegistration, rawSecretBytes []byte) (ConnectionStatus, error) {
	normalizedRegistration := normalizeConnectionRegistration(registration)
	if err := normalizedRegistration.Validate(); err != nil {
		return ConnectionStatus{}, err
	}
	if len(rawSecretBytes) == 0 {
		return ConnectionStatus{}, fmt.Errorf("%w: connection secret value is empty", secrets.ErrSecretValidation)
	}

	connectionKey := connectionRecordKey(normalizedRegistration.Provider, normalizedRegistration.Subject)

	server.connectionRuntime.mu.Lock()
	_, foundExistingRecord := server.connectionRuntime.records[connectionKey]
	server.connectionRuntime.mu.Unlock()
	if foundExistingRecord {
		return ConnectionStatus{}, fmt.Errorf("connection credential already exists for provider %q subject %q; explicit rotation flow required", normalizedRegistration.Provider, normalizedRegistration.Subject)
	}

	secretStore, err := server.secretStoreForRef(normalizedRegistration.Credential)
	if err != nil {
		return ConnectionStatus{}, err
	}
	secretMetadata, err := secretStore.Put(ctx, normalizedRegistration.Credential, rawSecretBytes)
	if err != nil {
		return ConnectionStatus{}, fmt.Errorf("store connection secret: %w", err)
	}

	nowUTC := server.now().UTC()
	record := connectionRecord{
		Provider:           normalizedRegistration.Provider,
		GrantType:          normalizedRegistration.GrantType,
		Subject:            normalizedRegistration.Subject,
		Scopes:             append([]string(nil), normalizedRegistration.Scopes...),
		Credential:         normalizedRegistration.Credential,
		Status:             defaultLabel(secretMetadata.Status, "stored"),
		CreatedAtUTC:       nowUTC.Format(time.RFC3339Nano),
		LastValidatedAtUTC: nowUTC.Format(time.RFC3339Nano),
	}
	if !secretMetadata.LastUsedAt.IsZero() {
		record.LastUsedAtUTC = secretMetadata.LastUsedAt.UTC().Format(time.RFC3339Nano)
	}
	if !secretMetadata.LastRotatedAt.IsZero() {
		record.LastRotatedAtUTC = secretMetadata.LastRotatedAt.UTC().Format(time.RFC3339Nano)
	} else {
		record.LastRotatedAtUTC = nowUTC.Format(time.RFC3339Nano)
	}

	server.connectionRuntime.mu.Lock()
	updatedConnections := cloneConnectionRecords(server.connectionRuntime.records)
	updatedConnections[connectionKey] = record
	if err := saveConnectionRecords(server.connectionPath, updatedConnections); err != nil {
		server.connectionRuntime.mu.Unlock()
		cleanupErr := deleteConnectionSecretForRollback(ctx, secretStore, normalizedRegistration.Credential)
		return ConnectionStatus{}, errors.Join(err, cleanupErr)
	}
	server.connectionRuntime.records = updatedConnections
	server.connectionRuntime.mu.Unlock()

	if err := server.logEvent("connection.credential_upserted", "", map[string]interface{}{
		"provider":            record.Provider,
		"subject":             record.Subject,
		"grant_type":          record.GrantType,
		"scope_count":         len(record.Scopes),
		"secure_store_ref_id": record.Credential.ID,
		"status":              record.Status,
	}); err != nil {
		server.connectionRuntime.mu.Lock()
		rollbackConnections := cloneConnectionRecords(server.connectionRuntime.records)
		delete(rollbackConnections, connectionKey)
		saveErr := saveConnectionRecords(server.connectionPath, rollbackConnections)
		if saveErr == nil {
			server.connectionRuntime.records = rollbackConnections
		}
		server.connectionRuntime.mu.Unlock()
		cleanupErr := deleteConnectionSecretForRollback(ctx, secretStore, normalizedRegistration.Credential)
		return ConnectionStatus{}, errors.Join(err, saveErr, cleanupErr)
	}
	server.invalidateProviderAccessToken(connectionKey)

	return record.statusSummary(), nil
}

func (server *Server) RotateConnectionCredential(ctx context.Context, registration connectionRegistration, rawSecretBytes []byte) (ConnectionStatus, error) {
	normalizedRegistration := normalizeConnectionRegistration(registration)
	if err := normalizedRegistration.Validate(); err != nil {
		return ConnectionStatus{}, err
	}
	if len(rawSecretBytes) == 0 {
		return ConnectionStatus{}, fmt.Errorf("%w: connection secret value is empty", secrets.ErrSecretValidation)
	}

	connectionKey := connectionRecordKey(normalizedRegistration.Provider, normalizedRegistration.Subject)

	server.connectionRuntime.mu.Lock()
	existingRecord, foundExistingRecord := server.connectionRuntime.records[connectionKey]
	server.connectionRuntime.mu.Unlock()
	if !foundExistingRecord {
		return ConnectionStatus{}, fmt.Errorf("connection credential not found for provider %q subject %q", normalizedRegistration.Provider, normalizedRegistration.Subject)
	}
	if existingRecord.Credential != normalizedRegistration.Credential {
		return ConnectionStatus{}, fmt.Errorf("connection credential rotation requires the existing secret ref for provider %q subject %q", normalizedRegistration.Provider, normalizedRegistration.Subject)
	}

	secretStore, err := server.secretStoreForRef(normalizedRegistration.Credential)
	if err != nil {
		return ConnectionStatus{}, err
	}
	previousSecretBytes, _, err := secretStore.Get(ctx, normalizedRegistration.Credential)
	if err != nil {
		return ConnectionStatus{}, fmt.Errorf("read existing connection secret for rotation: %w", err)
	}
	secretMetadata, err := secretStore.Put(ctx, normalizedRegistration.Credential, rawSecretBytes)
	if err != nil {
		return ConnectionStatus{}, fmt.Errorf("store rotated connection secret: %w", err)
	}

	nowUTC := server.now().UTC()
	updatedRecord := connectionRecord{
		Provider:           normalizedRegistration.Provider,
		GrantType:          normalizedRegistration.GrantType,
		Subject:            normalizedRegistration.Subject,
		Scopes:             append([]string(nil), normalizedRegistration.Scopes...),
		Credential:         normalizedRegistration.Credential,
		Status:             defaultLabel(secretMetadata.Status, "stored"),
		CreatedAtUTC:       existingRecord.CreatedAtUTC,
		LastValidatedAtUTC: nowUTC.Format(time.RFC3339Nano),
		LastUsedAtUTC:      existingRecord.LastUsedAtUTC,
	}
	if !secretMetadata.LastUsedAt.IsZero() {
		updatedRecord.LastUsedAtUTC = secretMetadata.LastUsedAt.UTC().Format(time.RFC3339Nano)
	}
	if !secretMetadata.LastRotatedAt.IsZero() {
		updatedRecord.LastRotatedAtUTC = secretMetadata.LastRotatedAt.UTC().Format(time.RFC3339Nano)
	} else {
		updatedRecord.LastRotatedAtUTC = nowUTC.Format(time.RFC3339Nano)
	}

	server.connectionRuntime.mu.Lock()
	updatedConnections := cloneConnectionRecords(server.connectionRuntime.records)
	updatedConnections[connectionKey] = updatedRecord
	if err := saveConnectionRecords(server.connectionPath, updatedConnections); err != nil {
		server.connectionRuntime.mu.Unlock()
		if restoreErr := server.restoreConnectionSecret(ctx, secretStore, normalizedRegistration.Credential, previousSecretBytes); restoreErr != nil {
			return ConnectionStatus{}, errors.Join(err, restoreErr)
		}
		return ConnectionStatus{}, err
	}
	server.connectionRuntime.records = updatedConnections
	server.connectionRuntime.mu.Unlock()

	if err := server.logEvent("connection.credential_rotated", "", map[string]interface{}{
		"provider":            updatedRecord.Provider,
		"subject":             updatedRecord.Subject,
		"grant_type":          updatedRecord.GrantType,
		"scope_count":         len(updatedRecord.Scopes),
		"secure_store_ref_id": updatedRecord.Credential.ID,
		"status":              updatedRecord.Status,
	}); err != nil {
		server.connectionRuntime.mu.Lock()
		rollbackConnections := cloneConnectionRecords(server.connectionRuntime.records)
		rollbackConnections[connectionKey] = existingRecord
		saveErr := saveConnectionRecords(server.connectionPath, rollbackConnections)
		if saveErr == nil {
			server.connectionRuntime.records = rollbackConnections
		}
		server.connectionRuntime.mu.Unlock()
		restoreErr := server.restoreConnectionSecret(ctx, secretStore, normalizedRegistration.Credential, previousSecretBytes)
		return ConnectionStatus{}, errors.Join(err, saveErr, restoreErr)
	}
	server.invalidateProviderAccessToken(connectionKey)

	return updatedRecord.statusSummary(), nil
}

func (server *Server) ResolveConnectionSecret(ctx context.Context, provider string, subject string) ([]byte, secrets.SecretMetadata, ConnectionStatus, error) {
	trimmedProvider := strings.TrimSpace(provider)
	trimmedSubject := strings.TrimSpace(subject)
	if err := identifiers.ValidateSafeIdentifier("connection provider", trimmedProvider); err != nil {
		return nil, secrets.SecretMetadata{}, ConnectionStatus{}, err
	}
	if trimmedSubject != "" {
		if err := identifiers.ValidateSafeIdentifier("connection subject", trimmedSubject); err != nil {
			return nil, secrets.SecretMetadata{}, ConnectionStatus{}, err
		}
	}

	server.connectionRuntime.mu.Lock()
	connectionRecord, found := server.connectionRuntime.records[connectionRecordKey(trimmedProvider, trimmedSubject)]
	server.connectionRuntime.mu.Unlock()
	if !found {
		return nil, secrets.SecretMetadata{}, ConnectionStatus{}, fmt.Errorf("connection not found for provider %q", trimmedProvider)
	}

	secretStore, err := server.secretStoreForRef(connectionRecord.Credential)
	if err != nil {
		return nil, secrets.SecretMetadata{}, ConnectionStatus{}, err
	}
	rawSecretBytes, secretMetadata, err := secretStore.Get(ctx, connectionRecord.Credential)
	if err != nil {
		return nil, secrets.SecretMetadata{}, ConnectionStatus{}, fmt.Errorf("resolve connection secret: %w", err)
	}

	nowUTC := server.now().UTC().Format(time.RFC3339Nano)
	server.connectionRuntime.mu.Lock()
	updatedConnections := cloneConnectionRecords(server.connectionRuntime.records)
	updatedRecord := updatedConnections[connectionRecordKey(trimmedProvider, trimmedSubject)]
	if !secretMetadata.LastUsedAt.IsZero() {
		updatedRecord.LastUsedAtUTC = secretMetadata.LastUsedAt.UTC().Format(time.RFC3339Nano)
	} else {
		updatedRecord.LastUsedAtUTC = nowUTC
	}
	if strings.TrimSpace(secretMetadata.Status) != "" {
		updatedRecord.Status = secretMetadata.Status
	}
	updatedConnections[connectionRecordKey(trimmedProvider, trimmedSubject)] = updatedRecord
	if err := saveConnectionRecords(server.connectionPath, updatedConnections); err != nil {
		server.connectionRuntime.mu.Unlock()
		return nil, secrets.SecretMetadata{}, ConnectionStatus{}, err
	}
	server.connectionRuntime.records = updatedConnections
	server.connectionRuntime.mu.Unlock()

	return rawSecretBytes, secretMetadata, updatedRecord.statusSummary(), nil
}

func (server *Server) ValidateConnection(ctx context.Context, provider string, subject string) (ConnectionStatus, error) {
	trimmedProvider := strings.TrimSpace(provider)
	trimmedSubject := strings.TrimSpace(subject)
	if err := identifiers.ValidateSafeIdentifier("connection provider", trimmedProvider); err != nil {
		return ConnectionStatus{}, err
	}
	if trimmedSubject != "" {
		if err := identifiers.ValidateSafeIdentifier("connection subject", trimmedSubject); err != nil {
			return ConnectionStatus{}, err
		}
	}

	server.connectionRuntime.mu.Lock()
	connectionRecord, found := server.connectionRuntime.records[connectionRecordKey(trimmedProvider, trimmedSubject)]
	server.connectionRuntime.mu.Unlock()
	if !found {
		configuredConnectionDefinition, configuredFound := server.providerRuntime.configuredConnections[connectionRecordKey(trimmedProvider, trimmedSubject)]
		if configuredFound && isPublicReadGrantType(configuredConnectionDefinition.Registration.GrantType) {
			return ConnectionStatus{
				Provider:           configuredConnectionDefinition.Registration.Provider,
				GrantType:          configuredConnectionDefinition.Registration.GrantType,
				Subject:            configuredConnectionDefinition.Registration.Subject,
				Scopes:             append([]string(nil), configuredConnectionDefinition.Registration.Scopes...),
				Status:             "public_configured",
				SecureStoreRefID:   "none",
				LastValidatedAtUTC: server.now().UTC().Format(time.RFC3339Nano),
				LastUsedAtUTC:      "never",
				LastRotatedAtUTC:   "never",
			}, nil
		}
		return ConnectionStatus{}, fmt.Errorf("connection not found for provider %q", trimmedProvider)
	}

	if isPublicReadGrantType(connectionRecord.GrantType) {
		nowUTC := server.now().UTC().Format(time.RFC3339Nano)
		server.connectionRuntime.mu.Lock()
		updatedConnections := cloneConnectionRecords(server.connectionRuntime.records)
		updatedRecord := updatedConnections[connectionRecordKey(trimmedProvider, trimmedSubject)]
		updatedRecord.LastValidatedAtUTC = nowUTC
		updatedRecord.Status = defaultLabel(updatedRecord.Status, "public_configured")
		if err := saveConnectionRecords(server.connectionPath, updatedConnections); err != nil {
			server.connectionRuntime.mu.Unlock()
			return ConnectionStatus{}, err
		}
		server.connectionRuntime.records = updatedConnections
		server.connectionRuntime.mu.Unlock()
		if err := server.logEvent("connection.validated", "", map[string]interface{}{
			"provider":            updatedRecord.Provider,
			"subject":             updatedRecord.Subject,
			"grant_type":          updatedRecord.GrantType,
			"scope_count":         len(updatedRecord.Scopes),
			"secure_store_ref_id": "none",
			"status":              updatedRecord.Status,
		}); err != nil {
			return ConnectionStatus{}, err
		}
		return updatedRecord.statusSummary(), nil
	}

	secretStore, err := server.secretStoreForRef(connectionRecord.Credential)
	if err != nil {
		return ConnectionStatus{}, err
	}
	secretMetadata, err := secretStore.Metadata(ctx, connectionRecord.Credential)
	if err != nil {
		return ConnectionStatus{}, fmt.Errorf("validate connection secret ref: %w", err)
	}

	nowUTC := server.now().UTC()
	server.connectionRuntime.mu.Lock()
	updatedConnections := cloneConnectionRecords(server.connectionRuntime.records)
	updatedRecord := updatedConnections[connectionRecordKey(trimmedProvider, trimmedSubject)]
	updatedRecord.LastValidatedAtUTC = nowUTC.Format(time.RFC3339Nano)
	updatedRecord.Status = defaultLabel(secretMetadata.Status, "validated")
	if !secretMetadata.LastUsedAt.IsZero() {
		updatedRecord.LastUsedAtUTC = secretMetadata.LastUsedAt.UTC().Format(time.RFC3339Nano)
	}
	if !secretMetadata.LastRotatedAt.IsZero() {
		updatedRecord.LastRotatedAtUTC = secretMetadata.LastRotatedAt.UTC().Format(time.RFC3339Nano)
	}
	if err := saveConnectionRecords(server.connectionPath, updatedConnections); err != nil {
		server.connectionRuntime.mu.Unlock()
		return ConnectionStatus{}, err
	}
	server.connectionRuntime.records = updatedConnections
	server.connectionRuntime.mu.Unlock()

	if err := server.logEvent("connection.validated", "", map[string]interface{}{
		"provider":            updatedRecord.Provider,
		"subject":             updatedRecord.Subject,
		"grant_type":          updatedRecord.GrantType,
		"scope_count":         len(updatedRecord.Scopes),
		"secure_store_ref_id": updatedRecord.Credential.ID,
		"status":              updatedRecord.Status,
	}); err != nil {
		return ConnectionStatus{}, err
	}

	return updatedRecord.statusSummary(), nil
}

func (server *Server) secretStoreForRef(validatedRef secrets.SecretRef) (secrets.SecretStore, error) {
	if server.resolveSecretStore == nil {
		return nil, fmt.Errorf("%w: secret store resolver is unavailable", secrets.ErrSecretBackendUnavailable)
	}
	return server.resolveSecretStore(validatedRef)
}

func cloneConnectionRecords(source map[string]connectionRecord) map[string]connectionRecord {
	clonedRecords := make(map[string]connectionRecord, len(source))
	for recordKey, sourceRecord := range source {
		clonedRecords[recordKey] = sourceRecord
	}
	return clonedRecords
}

func (server *Server) restoreConnectionSecret(ctx context.Context, secretStore secrets.SecretStore, validatedRef secrets.SecretRef, rawSecretBytes []byte) error {
	if _, err := secretStore.Put(ctx, validatedRef, rawSecretBytes); err != nil {
		return fmt.Errorf("restore previous connection secret after failed rotation: %w", err)
	}
	return nil
}

func deleteConnectionSecretForRollback(ctx context.Context, secretStore secrets.SecretStore, validatedRef secrets.SecretRef) error {
	if err := secretStore.Delete(ctx, validatedRef); err != nil {
		return fmt.Errorf("delete connection secret during rollback: %w", err)
	}
	return nil
}

func (server *Server) invalidateProviderAccessToken(connectionKey string) {
	server.providerRuntime.mu.Lock()
	delete(server.providerRuntime.tokens, connectionKey)
	server.providerRuntime.mu.Unlock()
}
