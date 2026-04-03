package loopgate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"morph/internal/identifiers"
)

const (
	PromotionTargetDisplay = "display"
	PromotionTargetMemory  = "memory"
	PromotionTargetPrompt  = "prompt"

	promotionTransformationIdentityCopy = "identity_copy"
)

type promotionRequest struct {
	SourceQuarantineRef   string
	SourceContentSHA256   string
	PromotionTarget       string
	PromotedBy            string
	SelectedFieldPaths    []string
	TransformationType    string
	DerivedClassification ResultClassification
}

type derivedArtifactRecord struct {
	SchemaVersion         string                         `json:"schema_version"`
	DerivedArtifactID     string                         `json:"derived_artifact_id"`
	SourceQuarantineRef   string                         `json:"source_quarantine_ref"`
	SourceContentSHA256   string                         `json:"source_content_sha256"`
	PromotionTarget       string                         `json:"promotion_target"`
	PromotedBy            string                         `json:"promoted_by"`
	PromotedAtUTC         string                         `json:"promoted_at_utc"`
	SelectedFieldPaths    []string                       `json:"selected_field_paths,omitempty"`
	TransformationType    string                         `json:"transformation_type"`
	DerivedArtifact       map[string]interface{}         `json:"derived_artifact"`
	DerivedFieldsMeta     map[string]ResultFieldMetadata `json:"derived_fields_meta"`
	DerivedClassification ResultClassification           `json:"derived_classification"`
}

type promotionDuplicateFingerprint struct {
	SourceQuarantineRef      string   `json:"source_quarantine_ref"`
	SourceContentSHA256      string   `json:"source_content_sha256"`
	PromotionTarget          string   `json:"promotion_target"`
	PromotedBy               string   `json:"promoted_by"`
	SelectedFieldPaths       []string `json:"selected_field_paths"`
	TransformationType       string   `json:"transformation_type"`
	DerivedArtifactSHA256    string   `json:"derived_artifact_sha256"`
	DerivedFieldsMetaSHA256  string   `json:"derived_fields_meta_sha256"`
	DerivedClassificationSHA string   `json:"derived_classification_sha256"`
}

func (validatedPromotionRequest promotionRequest) Validate() error {
	if strings.TrimSpace(validatedPromotionRequest.SourceQuarantineRef) == "" {
		return fmt.Errorf("source_quarantine_ref is required")
	}
	if strings.TrimSpace(validatedPromotionRequest.SourceContentSHA256) == "" {
		return fmt.Errorf("source_content_sha256 is required")
	}
	switch validatedPromotionRequest.PromotionTarget {
	case PromotionTargetDisplay, PromotionTargetMemory, PromotionTargetPrompt:
	default:
		return fmt.Errorf("invalid promotion target %q", validatedPromotionRequest.PromotionTarget)
	}
	if err := identifiers.ValidateSafeIdentifier("promoted_by", strings.TrimSpace(validatedPromotionRequest.PromotedBy)); err != nil {
		return err
	}
	validatedFieldPaths, err := canonicalSelectedFieldPaths(validatedPromotionRequest.SelectedFieldPaths)
	if err != nil {
		return err
	}
	if len(validatedFieldPaths) == 0 {
		return fmt.Errorf("selected_field_paths is required")
	}
	if validatedPromotionRequest.TransformationType != promotionTransformationIdentityCopy {
		return fmt.Errorf("transformation_type must be %q", promotionTransformationIdentityCopy)
	}
	expectedDerivedClassification, err := canonicalDerivedClassificationForTarget(validatedPromotionRequest.PromotionTarget)
	if err != nil {
		return err
	}
	if validatedPromotionRequest.DerivedClassification != expectedDerivedClassification {
		return fmt.Errorf("derived_classification does not match promotion target %q", validatedPromotionRequest.PromotionTarget)
	}
	return nil
}

func (server *Server) promoteQuarantinedArtifact(validatedPromotionRequest promotionRequest) (derivedArtifactRecord, error) {
	if err := validatedPromotionRequest.Validate(); err != nil {
		return derivedArtifactRecord{}, err
	}

	server.promotionMu.Lock()
	defer server.promotionMu.Unlock()

	sourceRecord, err := server.loadQuarantinedPayloadRecord(validatedPromotionRequest.SourceQuarantineRef)
	if err != nil {
		return derivedArtifactRecord{}, err
	}
	if err := sourceRecord.requireBlobPresent("source_bytes_unavailable"); err != nil {
		return derivedArtifactRecord{}, err
	}
	if sourceRecord.RawPayloadSHA256 != validatedPromotionRequest.SourceContentSHA256 {
		return derivedArtifactRecord{}, fmt.Errorf("source_content_sha256 does not match quarantined source")
	}
	rawSourcePayload, err := server.loadQuarantinedSourceBytes(
		validatedPromotionRequest.SourceQuarantineRef,
		sourceRecord,
		"source_bytes_unavailable",
	)
	if err != nil {
		return derivedArtifactRecord{}, err
	}

	derivedArtifact, derivedFieldsMeta, err := materializeDerivedArtifactFromSource(
		rawSourcePayload,
		validatedPromotionRequest.SelectedFieldPaths,
		validatedPromotionRequest.PromotionTarget,
	)
	if err != nil {
		return derivedArtifactRecord{}, err
	}

	candidateRecord := derivedArtifactRecord{
		SchemaVersion:         "loopgate.derived_artifact.v1",
		SourceQuarantineRef:   validatedPromotionRequest.SourceQuarantineRef,
		SourceContentSHA256:   validatedPromotionRequest.SourceContentSHA256,
		PromotionTarget:       validatedPromotionRequest.PromotionTarget,
		PromotedBy:            validatedPromotionRequest.PromotedBy,
		SelectedFieldPaths:    append([]string(nil), validatedPromotionRequest.SelectedFieldPaths...),
		TransformationType:    validatedPromotionRequest.TransformationType,
		DerivedArtifact:       derivedArtifact,
		DerivedFieldsMeta:     derivedFieldsMeta,
		DerivedClassification: validatedPromotionRequest.DerivedClassification,
	}

	if err := server.ensurePromotionNotDuplicate(candidateRecord); err != nil {
		return derivedArtifactRecord{}, err
	}

	derivedArtifactID, err := randomHex(16)
	if err != nil {
		return derivedArtifactRecord{}, fmt.Errorf("generate derived artifact id: %w", err)
	}
	candidateRecord.DerivedArtifactID = derivedArtifactID
	candidateRecord.PromotedAtUTC = server.now().UTC().Format(time.RFC3339Nano)

	if err := writeDerivedArtifactRecord(server.derivedArtifactPath(derivedArtifactID), candidateRecord); err != nil {
		return derivedArtifactRecord{}, err
	}
	if err := server.logEvent("artifact.promoted", "", map[string]interface{}{
		"source_quarantine_ref":  candidateRecord.SourceQuarantineRef,
		"source_content_sha256":  candidateRecord.SourceContentSHA256,
		"promotion_target":       candidateRecord.PromotionTarget,
		"promoted_by":            candidateRecord.PromotedBy,
		"promoted_at_utc":        candidateRecord.PromotedAtUTC,
		"derived_artifact_ref":   server.derivedArtifactRef(derivedArtifactID),
		"selected_field_paths":   append([]string(nil), candidateRecord.SelectedFieldPaths...),
		"transformation_type":    candidateRecord.TransformationType,
		"derived_classification": candidateRecord.DerivedClassification,
	}); err != nil {
		_ = os.Remove(server.derivedArtifactPath(derivedArtifactID))
		return derivedArtifactRecord{}, err
	}
	return candidateRecord, nil
}

func materializeDerivedArtifactFromSource(rawSourcePayload string, selectedFieldPaths []string, promotionTarget string) (map[string]interface{}, map[string]ResultFieldMetadata, error) {
	validatedFieldPaths, err := canonicalSelectedFieldPaths(selectedFieldPaths)
	if err != nil {
		return nil, nil, err
	}

	var parsedSource map[string]interface{}
	decoder := json.NewDecoder(strings.NewReader(rawSourcePayload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parsedSource); err != nil {
		return nil, nil, fmt.Errorf("promotable source must be a top-level JSON object: %w", err)
	}

	derivedArtifact := make(map[string]interface{}, len(validatedFieldPaths))
	derivedFieldsMeta := make(map[string]ResultFieldMetadata, len(validatedFieldPaths))
	for _, selectedFieldPath := range validatedFieldPaths {
		sourceFieldValue, found := parsedSource[selectedFieldPath]
		if !found {
			return nil, nil, fmt.Errorf("selected field %q is missing from source payload", selectedFieldPath)
		}
		derivedFieldMetadata, err := derivePromotedFieldMetadata(sourceFieldValue, promotionTarget)
		if err != nil {
			return nil, nil, fmt.Errorf("selected field %q: %w", selectedFieldPath, err)
		}
		derivedArtifact[selectedFieldPath] = sourceFieldValue
		derivedFieldsMeta[selectedFieldPath] = derivedFieldMetadata
	}
	return derivedArtifact, derivedFieldsMeta, nil
}

func derivePromotedFieldMetadata(sourceFieldValue interface{}, promotionTarget string) (ResultFieldMetadata, error) {
	resultFieldMetadata := ResultFieldMetadata{
		Origin:      ResultFieldOriginRemote,
		Trust:       ResultFieldTrustDeterministic,
		Kind:        ResultFieldKindScalar,
		ContentType: contentTypeApplicationJSON,
		Sensitivity: ResultFieldSensitivityBenign,
	}

	switch typedSourceFieldValue := sourceFieldValue.(type) {
	case string:
		resultFieldMetadata.ContentType = "text/plain"
		resultFieldMetadata.SizeBytes = len(typedSourceFieldValue)
		if normalizedTimestamp, ok := normalizePromotableTimestamp(typedSourceFieldValue); ok {
			resultFieldMetadata.ScalarSubclass = ResultFieldScalarSubclassTimestamp
			resultFieldMetadata.SizeBytes = len(normalizedTimestamp)
			break
		}
		if identifiers.ValidateSafeIdentifier("promoted strict identifier", typedSourceFieldValue) == nil {
			resultFieldMetadata.ScalarSubclass = ResultFieldScalarSubclassStrictIdentifier
			break
		}
		resultFieldMetadata.Sensitivity = ResultFieldSensitivityTaintedText
		resultFieldMetadata.ScalarSubclass = ResultFieldScalarSubclassShortTextLabel
		if promotionTarget != PromotionTargetDisplay {
			return ResultFieldMetadata{}, fmt.Errorf("tainted scalar text is display-only in v1")
		}
	case bool:
		resultFieldMetadata.SizeBytes = len(fmt.Sprintf("%t", typedSourceFieldValue))
		resultFieldMetadata.ScalarSubclass = ResultFieldScalarSubclassBoolean
	case float64:
		marshaledNumber, err := json.Marshal(typedSourceFieldValue)
		if err != nil {
			return ResultFieldMetadata{}, fmt.Errorf("marshal numeric source field: %w", err)
		}
		resultFieldMetadata.SizeBytes = len(marshaledNumber)
		if promotionTarget != PromotionTargetDisplay {
			return ResultFieldMetadata{}, fmt.Errorf("unclassified numeric scalar is display-only until a validated_number policy exists")
		}
	case nil:
		resultFieldMetadata.SizeBytes = len("null")
		if promotionTarget != PromotionTargetDisplay {
			return ResultFieldMetadata{}, fmt.Errorf("null scalar is display-only in v1")
		}
	default:
		return ResultFieldMetadata{}, fmt.Errorf("non-scalar source fields are not promotable in v1")
	}

	switch promotionTarget {
	case PromotionTargetDisplay:
		resultFieldMetadata.PromptEligible = false
		resultFieldMetadata.MemoryEligible = false
	case PromotionTargetMemory:
		resultFieldMetadata.PromptEligible = false
		resultFieldMetadata.MemoryEligible = true
	case PromotionTargetPrompt:
		resultFieldMetadata.PromptEligible = true
		resultFieldMetadata.MemoryEligible = false
	default:
		return ResultFieldMetadata{}, fmt.Errorf("invalid promotion target %q", promotionTarget)
	}
	return resultFieldMetadata, nil
}

func normalizePromotableTimestamp(rawValue string) (string, bool) {
	trimmedValue := strings.TrimSpace(rawValue)
	if trimmedValue == "" {
		return "", false
	}
	parsedTimestamp, err := time.Parse(time.RFC3339, trimmedValue)
	if err != nil {
		return "", false
	}
	return parsedTimestamp.UTC().Format(time.RFC3339), true
}

func canonicalDerivedClassificationForTarget(promotionTarget string) (ResultClassification, error) {
	switch promotionTarget {
	case PromotionTargetDisplay:
		return normalizeResultClassification(ResultClassification{
			Exposure: ResultExposureDisplay,
		}, "")
	case PromotionTargetMemory:
		return normalizeResultClassification(ResultClassification{
			Exposure: ResultExposureNone,
			Eligibility: ResultEligibility{
				Memory: true,
			},
		}, "")
	case PromotionTargetPrompt:
		return normalizeResultClassification(ResultClassification{
			Exposure: ResultExposureNone,
			Eligibility: ResultEligibility{
				Prompt: true,
			},
		}, "")
	default:
		return ResultClassification{}, fmt.Errorf("invalid promotion target %q", promotionTarget)
	}
}

func canonicalSelectedFieldPaths(selectedFieldPaths []string) ([]string, error) {
	if len(selectedFieldPaths) == 0 {
		return nil, nil
	}
	seenFieldPaths := make(map[string]struct{}, len(selectedFieldPaths))
	canonicalFieldPaths := make([]string, 0, len(selectedFieldPaths))
	for _, rawSelectedFieldPath := range selectedFieldPaths {
		selectedFieldPath := strings.TrimSpace(rawSelectedFieldPath)
		if err := identifiers.ValidateSafeIdentifier("selected field path", selectedFieldPath); err != nil {
			return nil, err
		}
		if strings.Contains(selectedFieldPath, ".") {
			return nil, fmt.Errorf("selected field path %q must be top-level only in v1", selectedFieldPath)
		}
		if _, duplicate := seenFieldPaths[selectedFieldPath]; duplicate {
			return nil, fmt.Errorf("selected field path %q is duplicated", selectedFieldPath)
		}
		seenFieldPaths[selectedFieldPath] = struct{}{}
		canonicalFieldPaths = append(canonicalFieldPaths, selectedFieldPath)
	}
	sort.Strings(canonicalFieldPaths)
	return canonicalFieldPaths, nil
}

func (server *Server) ensurePromotionNotDuplicate(candidateRecord derivedArtifactRecord) error {
	candidateFingerprint, err := promotionDuplicateDigest(candidateRecord)
	if err != nil {
		return err
	}

	derivedArtifactEntries, err := os.ReadDir(server.derivedArtifactDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read derived artifact dir: %w", err)
	}
	for _, derivedArtifactEntry := range derivedArtifactEntries {
		if derivedArtifactEntry.IsDir() || filepath.Ext(derivedArtifactEntry.Name()) != ".json" {
			continue
		}
		existingRecordPath := filepath.Join(server.derivedArtifactDir, derivedArtifactEntry.Name())
		existingRecord, err := loadDerivedArtifactRecord(existingRecordPath)
		if err != nil {
			return err
		}
		existingFingerprint, err := promotionDuplicateDigest(existingRecord)
		if err != nil {
			return err
		}
		if existingFingerprint == candidateFingerprint {
			return fmt.Errorf("exact duplicate promotion is denied in v1")
		}
	}
	return nil
}

func promotionDuplicateDigest(derivedArtifactRecord derivedArtifactRecord) (string, error) {
	derivedArtifactDigest, err := canonicalJSONSHA256(derivedArtifactRecord.DerivedArtifact)
	if err != nil {
		return "", fmt.Errorf("digest derived artifact: %w", err)
	}
	derivedFieldsMetaDigest, err := canonicalJSONSHA256(derivedArtifactRecord.DerivedFieldsMeta)
	if err != nil {
		return "", fmt.Errorf("digest derived field metadata: %w", err)
	}
	derivedClassificationDigest, err := canonicalJSONSHA256(derivedArtifactRecord.DerivedClassification)
	if err != nil {
		return "", fmt.Errorf("digest derived classification: %w", err)
	}

	duplicateFingerprint := promotionDuplicateFingerprint{
		SourceQuarantineRef:      derivedArtifactRecord.SourceQuarantineRef,
		SourceContentSHA256:      derivedArtifactRecord.SourceContentSHA256,
		PromotionTarget:          derivedArtifactRecord.PromotionTarget,
		PromotedBy:               derivedArtifactRecord.PromotedBy,
		SelectedFieldPaths:       append([]string(nil), derivedArtifactRecord.SelectedFieldPaths...),
		TransformationType:       derivedArtifactRecord.TransformationType,
		DerivedArtifactSHA256:    derivedArtifactDigest,
		DerivedFieldsMetaSHA256:  derivedFieldsMetaDigest,
		DerivedClassificationSHA: derivedClassificationDigest,
	}
	return canonicalJSONSHA256(duplicateFingerprint)
}

func canonicalJSONSHA256(value interface{}) (string, error) {
	canonicalJSONBytes, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	valueDigest := sha256.Sum256(canonicalJSONBytes)
	return hex.EncodeToString(valueDigest[:]), nil
}

func loadDerivedArtifactRecord(derivedArtifactPath string) (derivedArtifactRecord, error) {
	derivedArtifactBytes, err := os.ReadFile(derivedArtifactPath)
	if err != nil {
		return derivedArtifactRecord{}, fmt.Errorf("read derived artifact: %w", err)
	}
	var loadedRecord derivedArtifactRecord
	decoder := json.NewDecoder(bytes.NewReader(derivedArtifactBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&loadedRecord); err != nil {
		return derivedArtifactRecord{}, fmt.Errorf("decode derived artifact: %w", err)
	}
	return loadedRecord, nil
}

func (server *Server) derivedArtifactRef(derivedArtifactID string) string {
	return "derived://artifacts/" + derivedArtifactID
}

func (server *Server) derivedArtifactPath(derivedArtifactID string) string {
	return filepath.Join(server.derivedArtifactDir, derivedArtifactID+".json")
}

func writeDerivedArtifactRecord(path string, derivedArtifactRecord derivedArtifactRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create derived artifact dir: %w", err)
	}

	jsonBytes, err := json.MarshalIndent(derivedArtifactRecord, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal derived artifact: %w", err)
	}
	if len(jsonBytes) == 0 || jsonBytes[len(jsonBytes)-1] != '\n' {
		jsonBytes = append(jsonBytes, '\n')
	}

	tempPath := path + ".tmp"
	derivedArtifactFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open derived artifact temp file: %w", err)
	}
	defer func() { _ = derivedArtifactFile.Close() }()

	if _, err := derivedArtifactFile.Write(jsonBytes); err != nil {
		return fmt.Errorf("write derived artifact temp file: %w", err)
	}
	if err := derivedArtifactFile.Sync(); err != nil {
		return fmt.Errorf("sync derived artifact temp file: %w", err)
	}
	if err := derivedArtifactFile.Close(); err != nil {
		return fmt.Errorf("close derived artifact temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("rename derived artifact temp file: %w", err)
	}
	if derivedArtifactDir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = derivedArtifactDir.Sync()
		_ = derivedArtifactDir.Close()
	}
	return nil
}
