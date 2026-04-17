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
	modelruntime "loopgate/internal/modelruntime"
	"loopgate/internal/secrets"
)

type modelConnectionRecord struct {
	ConnectionID       string            `json:"connection_id"`
	ProviderName       string            `json:"provider_name"`
	BaseURL            string            `json:"base_url"`
	Credential         secrets.SecretRef `json:"credential"`
	Status             string            `json:"status"`
	CreatedAtUTC       string            `json:"created_at_utc"`
	LastValidatedAtUTC string            `json:"last_validated_at_utc,omitempty"`
	LastUsedAtUTC      string            `json:"last_used_at_utc,omitempty"`
	LastRotatedAtUTC   string            `json:"last_rotated_at_utc,omitempty"`
}

type modelConnectionStateFile struct {
	ModelConnections []modelConnectionRecord `json:"model_connections"`
}

func (request ModelConnectionStoreRequest) Validate() error {
	if err := identifiers.ValidateSafeIdentifier("model connection id", request.ConnectionID); err != nil {
		return err
	}
	if err := identifiers.ValidateSafeIdentifier("model provider", request.ProviderName); err != nil {
		return err
	}
	// SecretValue is validated separately in StoreModelConnection to keep the
	// plaintext key out of the normalized struct (and therefore error messages).
	if err := modelruntime.ValidateBaseURL(strings.TrimSpace(request.BaseURL)); err != nil {
		return err
	}
	if modelruntime.IsLoopbackModelBaseURL(strings.TrimSpace(request.BaseURL)) {
		return fmt.Errorf("loopback model base url %q does not require a stored model connection", request.BaseURL)
	}
	switch strings.TrimSpace(request.ProviderName) {
	case "openai_compatible", "anthropic":
	default:
		return fmt.Errorf("unsupported model connection provider %q", request.ProviderName)
	}
	return nil
}

func (record modelConnectionRecord) Validate() error {
	if err := identifiers.ValidateSafeIdentifier("model connection id", record.ConnectionID); err != nil {
		return err
	}
	if err := identifiers.ValidateSafeIdentifier("model provider", record.ProviderName); err != nil {
		return err
	}
	if err := modelruntime.ValidateBaseURL(strings.TrimSpace(record.BaseURL)); err != nil {
		return err
	}
	if err := record.Credential.Validate(); err != nil {
		return err
	}
	return nil
}

func (record modelConnectionRecord) statusSummary() ModelConnectionStatus {
	return ModelConnectionStatus{
		ConnectionID:       record.ConnectionID,
		ProviderName:       record.ProviderName,
		BaseURL:            record.BaseURL,
		Status:             record.Status,
		SecureStoreRefID:   record.Credential.ID,
		LastValidatedAtUTC: record.LastValidatedAtUTC,
		LastUsedAtUTC:      record.LastUsedAtUTC,
		LastRotatedAtUTC:   record.LastRotatedAtUTC,
	}
}

func loadModelConnectionRecords(path string) (map[string]modelConnectionRecord, error) {
	rawStateBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]modelConnectionRecord{}, nil
		}
		return nil, fmt.Errorf("read model connection records: %w", err)
	}

	var stateFile modelConnectionStateFile
	decoder := json.NewDecoder(bytes.NewReader(rawStateBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stateFile); err != nil {
		return nil, fmt.Errorf("decode model connection records: %w", err)
	}

	modelConnections := make(map[string]modelConnectionRecord, len(stateFile.ModelConnections))
	for _, loadedRecord := range stateFile.ModelConnections {
		if err := loadedRecord.Validate(); err != nil {
			return nil, fmt.Errorf("validate model connection record: %w", err)
		}
		modelConnections[loadedRecord.ConnectionID] = loadedRecord
	}
	return modelConnections, nil
}

func saveModelConnectionRecords(path string, modelConnections map[string]modelConnectionRecord) error {
	records := make([]modelConnectionRecord, 0, len(modelConnections))
	for _, modelConnection := range modelConnections {
		if err := modelConnection.Validate(); err != nil {
			return fmt.Errorf("validate model connection record: %w", err)
		}
		records = append(records, modelConnection)
	}
	sort.Slice(records, func(leftIndex int, rightIndex int) bool {
		return records[leftIndex].ConnectionID < records[rightIndex].ConnectionID
	})

	stateFile := modelConnectionStateFile{ModelConnections: records}
	jsonBytes, err := json.MarshalIndent(stateFile, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal model connection records: %w", err)
	}
	if len(jsonBytes) == 0 || jsonBytes[len(jsonBytes)-1] != '\n' {
		jsonBytes = append(jsonBytes, '\n')
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create model connection state dir: %w", err)
	}

	tempPath := path + ".tmp"
	connectionFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open temp model connection state: %w", err)
	}
	defer func() { _ = connectionFile.Close() }()

	if _, err := connectionFile.Write(jsonBytes); err != nil {
		return fmt.Errorf("write temp model connection state: %w", err)
	}
	if err := connectionFile.Sync(); err != nil {
		return fmt.Errorf("sync temp model connection state: %w", err)
	}
	if err := connectionFile.Close(); err != nil {
		return fmt.Errorf("close temp model connection state: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("rename temp model connection state: %w", err)
	}
	if connectionDir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = connectionDir.Sync()
		_ = connectionDir.Close()
	}
	_ = os.Chmod(path, 0o600)
	return nil
}

func modelConnectionSecretRef(connectionID string) secrets.SecretRef {
	return secrets.SecretRef{
		ID:          "model-" + connectionID,
		Backend:     secrets.BackendSecure,
		AccountName: "model." + connectionID,
		Scope:       "model_inference." + connectionID,
	}
}

func (server *Server) StoreModelConnection(ctx context.Context, request ModelConnectionStoreRequest) (ModelConnectionStatus, error) {
	// Extract the secret to []byte immediately and clear the struct field so the
	// plaintext key cannot appear in error messages, logs, or panic dumps.
	rawSecretBytes := []byte(request.SecretValue)
	request.SecretValue = ""

	if strings.TrimSpace(string(rawSecretBytes)) == "" {
		return ModelConnectionStatus{}, fmt.Errorf("%w: model api key is empty", secrets.ErrSecretValidation)
	}

	normalizedRequest := ModelConnectionStoreRequest{
		ConnectionID: strings.TrimSpace(request.ConnectionID),
		ProviderName: strings.TrimSpace(request.ProviderName),
		BaseURL:      strings.TrimSpace(request.BaseURL),
	}
	if err := normalizedRequest.Validate(); err != nil {
		return ModelConnectionStatus{}, err
	}

	server.modelConnectionRuntime.mu.Lock()
	_, foundExistingRecord := server.modelConnectionRuntime.records[normalizedRequest.ConnectionID]
	server.modelConnectionRuntime.mu.Unlock()
	if foundExistingRecord {
		return ModelConnectionStatus{}, fmt.Errorf("model connection %q already exists; explicit rotation flow required", normalizedRequest.ConnectionID)
	}

	secretRef := modelConnectionSecretRef(normalizedRequest.ConnectionID)
	secretStore, err := server.secretStoreForRef(secretRef)
	if err != nil {
		return ModelConnectionStatus{}, err
	}
	secretMetadata, err := secretStore.Put(ctx, secretRef, []byte(strings.TrimSpace(string(rawSecretBytes))))
	if err != nil {
		return ModelConnectionStatus{}, fmt.Errorf("store model connection secret: %w", err)
	}

	nowUTC := server.now().UTC()
	record := modelConnectionRecord{
		ConnectionID:       normalizedRequest.ConnectionID,
		ProviderName:       normalizedRequest.ProviderName,
		BaseURL:            normalizedRequest.BaseURL,
		Credential:         secretRef,
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

	server.modelConnectionRuntime.mu.Lock()
	updatedModelConnections := cloneModelConnectionRecords(server.modelConnectionRuntime.records)
	updatedModelConnections[record.ConnectionID] = record
	if err := saveModelConnectionRecords(server.modelConnectionPath, updatedModelConnections); err != nil {
		server.modelConnectionRuntime.mu.Unlock()
		cleanupErr := deleteModelConnectionSecretForRollback(ctx, secretStore, secretRef)
		return ModelConnectionStatus{}, errors.Join(err, cleanupErr)
	}
	server.modelConnectionRuntime.records = updatedModelConnections
	server.modelConnectionRuntime.mu.Unlock()

	if err := server.logEvent("model.connection_stored", "", map[string]interface{}{
		"connection_id":       record.ConnectionID,
		"provider":            record.ProviderName,
		"base_url":            record.BaseURL,
		"secure_store_ref_id": record.Credential.ID,
		"status":              record.Status,
	}); err != nil {
		server.modelConnectionRuntime.mu.Lock()
		rollbackConnections := cloneModelConnectionRecords(server.modelConnectionRuntime.records)
		delete(rollbackConnections, record.ConnectionID)
		saveErr := saveModelConnectionRecords(server.modelConnectionPath, rollbackConnections)
		if saveErr == nil {
			server.modelConnectionRuntime.records = rollbackConnections
		}
		server.modelConnectionRuntime.mu.Unlock()
		cleanupErr := deleteModelConnectionSecretForRollback(ctx, secretStore, secretRef)
		return ModelConnectionStatus{}, errors.Join(err, saveErr, cleanupErr)
	}

	return record.statusSummary(), nil
}

func deleteModelConnectionSecretForRollback(ctx context.Context, secretStore secrets.SecretStore, secretRef secrets.SecretRef) error {
	if err := secretStore.Delete(ctx, secretRef); err != nil {
		return fmt.Errorf("delete model connection secret during rollback: %w", err)
	}
	return nil
}

func (server *Server) ValidateModelConnection(ctx context.Context, connectionID string) (ModelConnectionStatus, error) {
	trimmedConnectionID := strings.TrimSpace(connectionID)
	if err := identifiers.ValidateSafeIdentifier("model connection id", trimmedConnectionID); err != nil {
		return ModelConnectionStatus{}, err
	}

	server.modelConnectionRuntime.mu.Lock()
	modelConnectionRecord, found := server.modelConnectionRuntime.records[trimmedConnectionID]
	server.modelConnectionRuntime.mu.Unlock()
	if !found {
		return ModelConnectionStatus{}, fmt.Errorf("model connection %q not found", trimmedConnectionID)
	}

	secretStore, err := server.secretStoreForRef(modelConnectionRecord.Credential)
	if err != nil {
		return ModelConnectionStatus{}, err
	}
	secretMetadata, err := secretStore.Metadata(ctx, modelConnectionRecord.Credential)
	if err != nil {
		return ModelConnectionStatus{}, fmt.Errorf("validate model connection secret ref: %w", err)
	}

	updatedRecord := modelConnectionRecord
	nowUTC := server.now().UTC().Format(time.RFC3339Nano)
	updatedRecord.LastValidatedAtUTC = nowUTC
	if strings.TrimSpace(secretMetadata.Status) != "" {
		updatedRecord.Status = secretMetadata.Status
	}
	if !secretMetadata.LastUsedAt.IsZero() {
		updatedRecord.LastUsedAtUTC = secretMetadata.LastUsedAt.UTC().Format(time.RFC3339Nano)
	}
	if !secretMetadata.LastRotatedAt.IsZero() {
		updatedRecord.LastRotatedAtUTC = secretMetadata.LastRotatedAt.UTC().Format(time.RFC3339Nano)
	}

	server.modelConnectionRuntime.mu.Lock()
	updatedModelConnections := cloneModelConnectionRecords(server.modelConnectionRuntime.records)
	updatedModelConnections[trimmedConnectionID] = updatedRecord
	if err := saveModelConnectionRecords(server.modelConnectionPath, updatedModelConnections); err != nil {
		server.modelConnectionRuntime.mu.Unlock()
		return ModelConnectionStatus{}, err
	}
	server.modelConnectionRuntime.records = updatedModelConnections
	server.modelConnectionRuntime.mu.Unlock()

	return updatedRecord.statusSummary(), nil
}

func (server *Server) resolveModelConnection(connectionID string) (modelConnectionRecord, error) {
	trimmedConnectionID := strings.TrimSpace(connectionID)
	if err := identifiers.ValidateSafeIdentifier("model connection id", trimmedConnectionID); err != nil {
		return modelConnectionRecord{}, err
	}

	server.modelConnectionRuntime.mu.Lock()
	record, found := server.modelConnectionRuntime.records[trimmedConnectionID]
	server.modelConnectionRuntime.mu.Unlock()
	if !found {
		return modelConnectionRecord{}, fmt.Errorf("model connection %q not found", trimmedConnectionID)
	}
	return record, nil
}

func cloneModelConnectionRecords(source map[string]modelConnectionRecord) map[string]modelConnectionRecord {
	clonedRecords := make(map[string]modelConnectionRecord, len(source))
	for connectionID, connectionRecord := range source {
		clonedRecords[connectionID] = connectionRecord
	}
	return clonedRecords
}
