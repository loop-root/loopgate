package loopgate

import (
	"encoding/json"
	"fmt"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"strings"

	"loopgate/internal/identifiers"
)

func classifyCapabilityResult(capability string) (controlapipkg.ResultClassification, string) {
	switch capability {
	case "fs_list", "operator_mount.fs_list":
		return controlapipkg.ResultClassification{
			Exposure: controlapipkg.ResultExposureDisplay,
			Eligibility: controlapipkg.ResultEligibility{
				Prompt: true,
			},
		}, ""
	case "fs_write", "operator_mount.fs_write":
		return controlapipkg.ResultClassification{
			Exposure: controlapipkg.ResultExposureDisplay,
			Eligibility: controlapipkg.ResultEligibility{
				Prompt: true,
			},
		}, ""
	case "shell_exec":
		return controlapipkg.ResultClassification{
			Exposure: controlapipkg.ResultExposureDisplay,
			Eligibility: controlapipkg.ResultEligibility{
				Prompt: true,
			},
		}, ""
	case "fs_read", "operator_mount.fs_read":
		return controlapipkg.ResultClassification{
			Exposure: controlapipkg.ResultExposureDisplay,
			Eligibility: controlapipkg.ResultEligibility{
				Prompt: true,
			},
		}, ""
	case "fs_mkdir", "operator_mount.fs_mkdir":
		return controlapipkg.ResultClassification{
			Exposure: controlapipkg.ResultExposureDisplay,
			Eligibility: controlapipkg.ResultEligibility{
				Prompt: true,
			},
		}, ""
	default:
		return controlapipkg.ResultClassification{
			Exposure: controlapipkg.ResultExposureAudit,
			Quarantine: controlapipkg.ResultQuarantine{
				Quarantined: true,
			},
		}, ""
	}
}

func buildCapabilityResult(capability string, arguments map[string]string, output string) (map[string]interface{}, map[string]controlapipkg.ResultFieldMetadata, controlapipkg.ResultClassification, string, error) {
	switch capability {
	case "shell_exec":
		structuredResult := map[string]interface{}{
			"command": arguments["command"],
			"output":  output,
		}
		classification := controlapipkg.ResultClassification{
			Exposure: controlapipkg.ResultExposureDisplay,
			Eligibility: controlapipkg.ResultEligibility{
				Prompt: true,
			},
		}
		fieldsMeta, err := fieldsMetadataForStructuredResult(structuredResult, controlapipkg.ResultFieldOriginLocal, classification)
		if err != nil {
			return nil, nil, controlapipkg.ResultClassification{}, "", err
		}
		return structuredResult, fieldsMeta, classification, "", nil
	case "fs_list", "operator_mount.fs_list":
		structuredResult := map[string]interface{}{
			"path":    arguments["path"],
			"entries": []string{},
		}
		entries := []string{}
		if strings.TrimSpace(output) != "" {
			entries = strings.Split(output, "\n")
		}
		structuredResult["entries"] = entries
		classification := controlapipkg.ResultClassification{
			Exposure: controlapipkg.ResultExposureDisplay,
			Eligibility: controlapipkg.ResultEligibility{
				Prompt: true,
			},
		}
		fieldsMeta, err := fieldsMetadataForStructuredResult(structuredResult, controlapipkg.ResultFieldOriginLocal, classification)
		if err != nil {
			return nil, nil, controlapipkg.ResultClassification{}, "", err
		}
		return structuredResult, fieldsMeta, classification, "", nil
	case "fs_write", "operator_mount.fs_write":
		structuredResult := map[string]interface{}{
			"path":    arguments["path"],
			"bytes":   len(arguments["content"]),
			"message": output,
		}
		classification := controlapipkg.ResultClassification{
			Exposure: controlapipkg.ResultExposureDisplay,
			Eligibility: controlapipkg.ResultEligibility{
				Prompt: true,
			},
		}
		fieldsMeta, err := fieldsMetadataForStructuredResult(structuredResult, controlapipkg.ResultFieldOriginLocal, classification)
		if err != nil {
			return nil, nil, controlapipkg.ResultClassification{}, "", err
		}
		return structuredResult, fieldsMeta, classification, "", nil
	case "fs_read", "operator_mount.fs_read":
		structuredResult := map[string]interface{}{
			"path":    arguments["path"],
			"content": output,
			"bytes":   len(output),
		}
		classification := controlapipkg.ResultClassification{
			Exposure: controlapipkg.ResultExposureDisplay,
			Eligibility: controlapipkg.ResultEligibility{
				Prompt: true,
			},
		}
		fieldsMeta, err := fieldsMetadataForStructuredResult(structuredResult, controlapipkg.ResultFieldOriginLocal, classification)
		if err != nil {
			return nil, nil, controlapipkg.ResultClassification{}, "", err
		}
		return structuredResult, fieldsMeta, classification, "", nil
	default:
		classification, quarantineRef := classifyCapabilityResult(capability)
		if !classification.AuditOnly() {
			structuredResult := map[string]interface{}{
				"output": output,
			}
			fieldsMeta, err := fieldsMetadataForStructuredResult(structuredResult, controlapipkg.ResultFieldOriginLocal, classification)
			if err != nil {
				return nil, nil, controlapipkg.ResultClassification{}, "", err
			}
			return structuredResult, fieldsMeta, classification, quarantineRef, nil
		}
		return map[string]interface{}{}, nil, classification, quarantineRef, nil
	}
}

func normalizeResultClassification(classification controlapipkg.ResultClassification, quarantineRef string) (controlapipkg.ResultClassification, error) {
	classification.Quarantine.Ref = strings.TrimSpace(quarantineRef)
	if err := classification.Validate(); err != nil {
		return controlapipkg.ResultClassification{}, err
	}
	return classification, nil
}

func fieldsMetadataForStructuredResult(structuredResult map[string]interface{}, fieldOrigin string, classification controlapipkg.ResultClassification) (map[string]controlapipkg.ResultFieldMetadata, error) {
	fieldsMeta := make(map[string]controlapipkg.ResultFieldMetadata, len(structuredResult))
	for fieldName, fieldValue := range structuredResult {
		fieldMetadata, err := buildResultFieldMetadata(fieldValue, fieldOrigin, classification)
		if err != nil {
			return nil, fmt.Errorf("build fields_meta for %q: %w", fieldName, err)
		}
		fieldsMeta[fieldName] = fieldMetadata
	}
	return fieldsMeta, nil
}

func buildResultFieldMetadata(fieldValue interface{}, fieldOrigin string, classification controlapipkg.ResultClassification) (controlapipkg.ResultFieldMetadata, error) {
	fieldKind, fieldContentType, fieldSizeBytes, err := describeResultFieldValue(fieldValue)
	if err != nil {
		return controlapipkg.ResultFieldMetadata{}, err
	}
	fieldMetadata := controlapipkg.ResultFieldMetadata{
		Origin:         fieldOrigin,
		ContentType:    fieldContentType,
		Trust:          controlapipkg.ResultFieldTrustDeterministic,
		Sensitivity:    sensitivityForResultField(fieldValue),
		SizeBytes:      fieldSizeBytes,
		Kind:           fieldKind,
		ScalarSubclass: scalarSubclassForResultField(fieldValue),
		PromptEligible: classification.PromptEligible(),
	}
	if err := fieldMetadata.Validate(); err != nil {
		return controlapipkg.ResultFieldMetadata{}, err
	}
	return fieldMetadata, nil
}

func describeResultFieldValue(fieldValue interface{}) (string, string, int, error) {
	switch typedFieldValue := fieldValue.(type) {
	case string:
		return controlapipkg.ResultFieldKindScalar, "text/plain", len(typedFieldValue), nil
	case bool, float64, float32, int, int64, int32, uint, uint32, uint64:
		encodedFieldBytes, err := json.Marshal(typedFieldValue)
		if err != nil {
			return "", "", 0, fmt.Errorf("encode scalar field: %w", err)
		}
		return controlapipkg.ResultFieldKindScalar, "application/json", len(encodedFieldBytes), nil
	case []string, []interface{}:
		encodedFieldBytes, err := json.Marshal(typedFieldValue)
		if err != nil {
			return "", "", 0, fmt.Errorf("encode array field: %w", err)
		}
		return controlapipkg.ResultFieldKindArray, "application/json", len(encodedFieldBytes), nil
	case map[string]interface{}:
		encodedFieldBytes, err := json.Marshal(typedFieldValue)
		if err != nil {
			return "", "", 0, fmt.Errorf("encode object field: %w", err)
		}
		return controlapipkg.ResultFieldKindObject, "application/json", len(encodedFieldBytes), nil
	default:
		encodedFieldBytes, err := json.Marshal(typedFieldValue)
		if err != nil {
			return "", "", 0, fmt.Errorf("encode structured field: %w", err)
		}
		return controlapipkg.ResultFieldKindScalar, "application/json", len(encodedFieldBytes), nil
	}
}

func sensitivityForResultField(fieldValue interface{}) string {
	switch typedFieldValue := fieldValue.(type) {
	case string:
		if strings.TrimSpace(typedFieldValue) == "" {
			return controlapipkg.ResultFieldSensitivityBenign
		}
		return controlapipkg.ResultFieldSensitivityTaintedText
	case []string:
		if len(typedFieldValue) == 0 {
			return controlapipkg.ResultFieldSensitivityBenign
		}
		return controlapipkg.ResultFieldSensitivityTaintedText
	case []interface{}:
		if len(typedFieldValue) == 0 {
			return controlapipkg.ResultFieldSensitivityBenign
		}
		return controlapipkg.ResultFieldSensitivityTaintedText
	default:
		return controlapipkg.ResultFieldSensitivityBenign
	}
}

func scalarSubclassForResultField(fieldValue interface{}) string {
	switch typedFieldValue := fieldValue.(type) {
	case bool:
		return controlapipkg.ResultFieldScalarSubclassBoolean
	case float64, float32, int, int64, int32, uint, uint32, uint64:
		return controlapipkg.ResultFieldScalarSubclassValidatedNumber
	case string:
		if normalizedTimestamp, ok := normalizePromotableTimestamp(typedFieldValue); ok && normalizedTimestamp != "" {
			return controlapipkg.ResultFieldScalarSubclassTimestamp
		}
		if identifiers.ValidateSafeIdentifier("result field strict identifier", typedFieldValue) == nil {
			return controlapipkg.ResultFieldScalarSubclassStrictIdentifier
		}
		return controlapipkg.ResultFieldScalarSubclassShortTextLabel
	default:
		return ""
	}
}
