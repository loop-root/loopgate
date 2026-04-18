package loopgate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"path/filepath"
	"strings"
	"time"

	"loopgate/internal/identifiers"
)

const quarantineRefPrefix = "quarantine://payloads/"

const (
	quarantineStorageStateBlobPresent = "blob_present"
	quarantineStorageStateBlobPruned  = "blob_pruned"
	quarantineBlobRetentionPeriod     = 7 * 24 * time.Hour

	quarantineTrustStateQuarantined            = "quarantined"
	quarantineContentAvailabilityBlobAvailable = "blob_available"
	quarantineContentAvailabilityMetadataOnly  = "metadata_only"
)

var (
	errQuarantinedSourceNotFound      = errors.New("quarantined source not found")
	errQuarantinedSourceBytesRetained = errors.New("quarantined source bytes are no longer retained; metadata remains available")
	errQuarantinePruneNotEligible     = errors.New("quarantined source blob is not eligible for pruning")
)

type quarantinedPayloadRecord struct {
	SchemaVersion        string `json:"schema_version"`
	QuarantineID         string `json:"quarantine_id"`
	StoredAtUTC          string `json:"stored_at_utc"`
	StorageState         string `json:"storage_state"`
	RequestID            string `json:"request_id"`
	Capability           string `json:"capability"`
	NormalizedArgHash    string `json:"normalized_argument_hash"`
	RawPayloadSHA256     string `json:"raw_payload_sha256"`
	RawPayloadByteLength int    `json:"raw_payload_byte_length"`
	BlobPrunedAtUTC      string `json:"blob_pruned_at_utc,omitempty"`
}

type QuarantineLookupRequest struct {
	QuarantineRef string `json:"quarantine_ref"`
}

type QuarantineMetadataResponse struct {
	QuarantineRef       string `json:"quarantine_ref"`
	RequestID           string `json:"request_id"`
	Capability          string `json:"capability"`
	TrustState          string `json:"trust_state"`
	ContentAvailability string `json:"content_availability"`
	StoredAtUTC         string `json:"stored_at_utc"`
	ContentType         string `json:"content_type"`
	ContentSHA256       string `json:"content_sha256"`
	SizeBytes           int    `json:"size_bytes"`
	StorageState        string `json:"storage_state"`
	BlobPrunedAtUTC     string `json:"blob_pruned_at_utc,omitempty"`
	PruneEligible       bool   `json:"prune_eligible"`
	PruneEligibleAtUTC  string `json:"prune_eligible_at_utc,omitempty"`
	NormalizedArgHash   string `json:"normalized_argument_hash"`
}

type QuarantineViewResponse struct {
	Metadata   QuarantineMetadataResponse `json:"metadata"`
	RawPayload string                     `json:"raw_payload"`
}

func (server *Server) storeQuarantinedPayload(capabilityRequest controlapipkg.CapabilityRequest, rawPayload string) (string, error) {
	quarantineID, err := randomHex(16)
	if err != nil {
		return "", fmt.Errorf("generate quarantine id: %w", err)
	}

	quarantinedPayloadRecord := quarantinedPayloadRecord{
		SchemaVersion:        "loopgate.quarantine.v1",
		QuarantineID:         quarantineID,
		StoredAtUTC:          server.now().UTC().Format(time.RFC3339Nano),
		StorageState:         quarantineStorageStateBlobPresent,
		RequestID:            capabilityRequest.RequestID,
		Capability:           capabilityRequest.Capability,
		NormalizedArgHash:    normalizedArgumentHash(capabilityRequest.Arguments),
		RawPayloadSHA256:     payloadSHA256(rawPayload),
		RawPayloadByteLength: len(rawPayload),
	}

	quarantinePath, err := quarantinePathFromRef(server.repoRoot, quarantineRefForID(quarantineID))
	if err != nil {
		return "", err
	}
	blobPath, err := quarantineBlobPathFromRef(server.repoRoot, quarantineRefForID(quarantineID))
	if err != nil {
		return "", err
	}
	if err := writeQuarantinedPayloadBlob(blobPath, rawPayload); err != nil {
		return "", err
	}
	if err := writeQuarantinedPayloadRecord(quarantinePath, quarantinedPayloadRecord); err != nil {
		_ = os.Remove(blobPath)
		return "", err
	}
	return quarantineRefForID(quarantineID), nil
}

func quarantineRefForID(quarantineID string) string {
	return quarantineRefPrefix + quarantineID
}

func quarantinePathFromRef(repoRoot string, quarantineRef string) (string, error) {
	if !strings.HasPrefix(quarantineRef, quarantineRefPrefix) {
		return "", fmt.Errorf("invalid quarantine ref prefix")
	}
	quarantineID := strings.TrimPrefix(quarantineRef, quarantineRefPrefix)
	if err := identifiers.ValidateSafeIdentifier("quarantine id", quarantineID); err != nil {
		return "", err
	}
	return filepath.Join(repoRoot, "runtime", "state", "quarantine", quarantineID+".json"), nil
}

func quarantineBlobPathFromRef(repoRoot string, quarantineRef string) (string, error) {
	if !strings.HasPrefix(quarantineRef, quarantineRefPrefix) {
		return "", fmt.Errorf("invalid quarantine ref prefix")
	}
	quarantineID := strings.TrimPrefix(quarantineRef, quarantineRefPrefix)
	if err := identifiers.ValidateSafeIdentifier("quarantine id", quarantineID); err != nil {
		return "", err
	}
	return filepath.Join(repoRoot, "runtime", "state", "quarantine", "blobs", quarantineID+".blob"), nil
}

func writeQuarantinedPayloadRecord(path string, quarantinedPayloadRecord quarantinedPayloadRecord) error {
	if err := quarantinedPayloadRecord.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create quarantine dir: %w", err)
	}

	jsonBytes, err := json.MarshalIndent(quarantinedPayloadRecord, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal quarantined payload: %w", err)
	}

	tempPath := path + ".tmp"
	tempFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open quarantine temp file: %w", err)
	}
	defer func() { _ = tempFile.Close() }()

	if _, err := tempFile.Write(jsonBytes); err != nil {
		return fmt.Errorf("write quarantine temp file: %w", err)
	}
	if len(jsonBytes) == 0 || jsonBytes[len(jsonBytes)-1] != '\n' {
		if _, err := io.WriteString(tempFile, "\n"); err != nil {
			return fmt.Errorf("write quarantine newline: %w", err)
		}
	}
	if err := tempFile.Sync(); err != nil {
		return fmt.Errorf("sync quarantine temp file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close quarantine temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("rename quarantine temp file: %w", err)
	}
	if quarantineDir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = quarantineDir.Sync()
		_ = quarantineDir.Close()
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod quarantine file: %w", err)
	}
	return nil
}

func writeQuarantinedPayloadBlob(path string, rawPayload string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create quarantine blob dir: %w", err)
	}

	tempPath := path + ".tmp"
	tempFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open quarantine blob temp file: %w", err)
	}
	defer func() { _ = tempFile.Close() }()

	if _, err := io.WriteString(tempFile, rawPayload); err != nil {
		return fmt.Errorf("write quarantine blob temp file: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		return fmt.Errorf("sync quarantine blob temp file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close quarantine blob temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("rename quarantine blob temp file: %w", err)
	}
	if blobDir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = blobDir.Sync()
		_ = blobDir.Close()
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod quarantine blob file: %w", err)
	}
	return nil
}

func payloadSHA256(rawPayload string) string {
	payloadHash := sha256.Sum256([]byte(rawPayload))
	return hex.EncodeToString(payloadHash[:])
}

func (server *Server) pruneQuarantinedPayload(quarantineRef string, pruneReason string) error {
	server.promotionMu.Lock()
	defer server.promotionMu.Unlock()

	quarantinePath, err := quarantinePathFromRef(server.repoRoot, quarantineRef)
	if err != nil {
		return err
	}
	blobPath, err := quarantineBlobPathFromRef(server.repoRoot, quarantineRef)
	if err != nil {
		return err
	}
	originalRecord, err := server.loadQuarantinedPayloadRecord(quarantineRef)
	if err != nil {
		return err
	}
	if originalRecord.StorageState == quarantineStorageStateBlobPruned {
		return fmt.Errorf("%w", errQuarantinePruneNotEligible)
	}
	if err := server.validateQuarantinePruneEligibility(originalRecord); err != nil {
		return err
	}
	if _, err := readQuarantinedPayloadBlob(blobPath, originalRecord); err != nil {
		return err
	}

	prunedBlobPath := blobPath + ".pruned"
	if err := os.Rename(blobPath, prunedBlobPath); err != nil {
		return fmt.Errorf("stage quarantined blob prune: %w", err)
	}

	prunedRecord := originalRecord
	prunedRecord.StorageState = quarantineStorageStateBlobPruned
	prunedRecord.BlobPrunedAtUTC = server.now().UTC().Format(time.RFC3339Nano)
	if err := writeQuarantinedPayloadRecord(quarantinePath, prunedRecord); err != nil {
		_ = os.Rename(prunedBlobPath, blobPath)
		return err
	}
	if err := server.logEvent("artifact.blob_pruned", "", map[string]interface{}{
		"quarantine_ref":      quarantineRef,
		"content_sha256":      prunedRecord.RawPayloadSHA256,
		"blob_size_bytes":     prunedRecord.RawPayloadByteLength,
		"content_type":        "application/octet-stream",
		"prior_storage_state": originalRecord.StorageState,
		"new_storage_state":   prunedRecord.StorageState,
		"blob_pruned_at_utc":  prunedRecord.BlobPrunedAtUTC,
		"reason":              strings.TrimSpace(pruneReason),
	}); err != nil {
		_ = writeQuarantinedPayloadRecord(quarantinePath, originalRecord)
		_ = os.Rename(prunedBlobPath, blobPath)
		return err
	}
	if err := os.Remove(prunedBlobPath); err != nil {
		return fmt.Errorf("remove pruned quarantine blob: %w", err)
	}
	return nil
}

func (validatedQuarantineLookupRequest QuarantineLookupRequest) Validate() error {
	if strings.TrimSpace(validatedQuarantineLookupRequest.QuarantineRef) == "" {
		return fmt.Errorf("quarantine_ref is required")
	}
	_, err := quarantinePathFromRef(".", validatedQuarantineLookupRequest.QuarantineRef)
	if err != nil {
		return err
	}
	return nil
}

func (server *Server) loadQuarantinedPayloadRecord(quarantineRef string) (quarantinedPayloadRecord, error) {
	quarantinePath, err := quarantinePathFromRef(server.repoRoot, quarantineRef)
	if err != nil {
		return quarantinedPayloadRecord{}, err
	}
	recordBytes, err := os.ReadFile(quarantinePath)
	if err != nil {
		if os.IsNotExist(err) {
			return quarantinedPayloadRecord{}, fmt.Errorf("%w", errQuarantinedSourceNotFound)
		}
		return quarantinedPayloadRecord{}, fmt.Errorf("read quarantined source: %w", err)
	}
	var sourceRecord quarantinedPayloadRecord
	decoder := json.NewDecoder(strings.NewReader(string(recordBytes)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&sourceRecord); err != nil {
		return quarantinedPayloadRecord{}, fmt.Errorf("decode quarantined source: %w", err)
	}
	if err := sourceRecord.Validate(); err != nil {
		return quarantinedPayloadRecord{}, fmt.Errorf("invalid quarantined source record: %w", err)
	}
	return sourceRecord, nil
}

func (server *Server) quarantineMetadata(quarantineRef string) (QuarantineMetadataResponse, error) {
	sourceRecord, err := server.loadQuarantinedPayloadRecord(quarantineRef)
	if err != nil {
		return QuarantineMetadataResponse{}, err
	}
	return server.quarantineMetadataFromRecord(quarantineRef, sourceRecord), nil
}

func (server *Server) viewQuarantinedPayload(quarantineRef string) (QuarantineViewResponse, error) {
	sourceRecord, err := server.loadQuarantinedPayloadRecord(quarantineRef)
	if err != nil {
		return QuarantineViewResponse{}, err
	}
	rawPayload, err := server.loadQuarantinedSourceBytes(quarantineRef, sourceRecord, "source_bytes_unavailable")
	if err != nil {
		return QuarantineViewResponse{}, err
	}
	return QuarantineViewResponse{
		Metadata:   server.quarantineMetadataFromRecord(quarantineRef, sourceRecord),
		RawPayload: rawPayload,
	}, nil
}

func quarantinePruneEligibleAtUTC(sourceRecord quarantinedPayloadRecord) time.Time {
	storedAtUTC, err := time.Parse(time.RFC3339Nano, sourceRecord.StoredAtUTC)
	if err != nil {
		return time.Time{}
	}
	return storedAtUTC.Add(quarantineBlobRetentionPeriod)
}

func (server *Server) quarantineMetadataFromRecord(quarantineRef string, sourceRecord quarantinedPayloadRecord) QuarantineMetadataResponse {
	pruneEligibleAtUTC := quarantinePruneEligibleAtUTC(sourceRecord)
	pruneEligible := sourceRecord.StorageState == quarantineStorageStateBlobPresent &&
		!pruneEligibleAtUTC.IsZero() &&
		!server.now().UTC().Before(pruneEligibleAtUTC)
	pruneEligibleAtValue := ""
	if !pruneEligibleAtUTC.IsZero() {
		pruneEligibleAtValue = pruneEligibleAtUTC.Format(time.RFC3339Nano)
	}
	contentAvailability := quarantineContentAvailabilityMetadataOnly
	if sourceRecord.StorageState == quarantineStorageStateBlobPresent {
		contentAvailability = quarantineContentAvailabilityBlobAvailable
	}
	return QuarantineMetadataResponse{
		QuarantineRef:       quarantineRef,
		RequestID:           sourceRecord.RequestID,
		Capability:          sourceRecord.Capability,
		TrustState:          quarantineTrustStateQuarantined,
		ContentAvailability: contentAvailability,
		StoredAtUTC:         sourceRecord.StoredAtUTC,
		ContentType:         "application/octet-stream",
		ContentSHA256:       sourceRecord.RawPayloadSHA256,
		SizeBytes:           sourceRecord.RawPayloadByteLength,
		StorageState:        sourceRecord.StorageState,
		BlobPrunedAtUTC:     sourceRecord.BlobPrunedAtUTC,
		PruneEligible:       pruneEligible,
		PruneEligibleAtUTC:  pruneEligibleAtValue,
		NormalizedArgHash:   sourceRecord.NormalizedArgHash,
	}
}

func (server *Server) pruneQuarantinedPayloadAndLoadMetadata(quarantineRef string, pruneReason string) (QuarantineMetadataResponse, error) {
	if err := server.pruneQuarantinedPayload(quarantineRef, pruneReason); err != nil {
		return QuarantineMetadataResponse{}, err
	}
	return server.quarantineMetadata(quarantineRef)
}

func (server *Server) validateQuarantinePruneEligibility(sourceRecord quarantinedPayloadRecord) error {
	if sourceRecord.StorageState != quarantineStorageStateBlobPresent {
		return fmt.Errorf("%w", errQuarantinePruneNotEligible)
	}
	pruneEligibleAtUTC := quarantinePruneEligibleAtUTC(sourceRecord)
	if pruneEligibleAtUTC.IsZero() {
		return fmt.Errorf("%w: invalid stored_at_utc", errQuarantinePruneNotEligible)
	}
	if server.now().UTC().Before(pruneEligibleAtUTC) {
		return fmt.Errorf("%w: quarantine blob is retained until %s", errQuarantinePruneNotEligible, pruneEligibleAtUTC.Format(time.RFC3339Nano))
	}
	return nil
}

func (quarantinedPayloadRecord quarantinedPayloadRecord) Validate() error {
	switch quarantinedPayloadRecord.StorageState {
	case quarantineStorageStateBlobPresent:
		if quarantinedPayloadRecord.BlobPrunedAtUTC != "" {
			return fmt.Errorf("blob_present quarantine record must not set blob_pruned_at_utc")
		}
	case quarantineStorageStateBlobPruned:
		if quarantinedPayloadRecord.BlobPrunedAtUTC == "" {
			return fmt.Errorf("blob_pruned quarantine record requires blob_pruned_at_utc")
		}
	default:
		return fmt.Errorf("invalid quarantine storage state %q", quarantinedPayloadRecord.StorageState)
	}
	if quarantinedPayloadRecord.RawPayloadByteLength < 0 {
		return fmt.Errorf("quarantine raw_payload_byte_length must be non-negative")
	}
	if strings.TrimSpace(quarantinedPayloadRecord.RawPayloadSHA256) == "" {
		return fmt.Errorf("quarantine raw_payload_sha256 is required")
	}
	return nil
}

func (quarantinedPayloadRecord quarantinedPayloadRecord) requireBlobPresent(failureReason string) error {
	if quarantinedPayloadRecord.StorageState != quarantineStorageStateBlobPresent {
		return fmt.Errorf("%s: blob_pruned: %w", failureReason, errQuarantinedSourceBytesRetained)
	}
	return nil
}

func (server *Server) loadQuarantinedSourceBytes(quarantineRef string, sourceRecord quarantinedPayloadRecord, failureReason string) (string, error) {
	if err := sourceRecord.requireBlobPresent(failureReason); err != nil {
		return "", err
	}
	blobPath, err := quarantineBlobPathFromRef(server.repoRoot, quarantineRef)
	if err != nil {
		return "", err
	}
	return readQuarantinedPayloadBlob(blobPath, sourceRecord)
}

func readQuarantinedPayloadBlob(blobPath string, sourceRecord quarantinedPayloadRecord) (string, error) {
	blobBytes, err := os.ReadFile(blobPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("source_verification_failed: %w", errQuarantinedSourceNotFound)
		}
		return "", fmt.Errorf("read quarantined blob: %w", err)
	}
	if len(blobBytes) != sourceRecord.RawPayloadByteLength {
		return "", fmt.Errorf("source_verification_failed: quarantine blob length does not match recorded metadata")
	}
	rawPayload := string(blobBytes)
	if payloadSHA256(rawPayload) != sourceRecord.RawPayloadSHA256 {
		return "", fmt.Errorf("source_verification_failed: quarantine blob hash does not match recorded metadata")
	}
	return rawPayload, nil
}
