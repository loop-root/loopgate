package loopgate

import (
	"encoding/json"
	"fmt"
	"strings"

	"morph/internal/identifiers"
)

func classifyCapabilityResult(capability string) (ResultClassification, string) {
	switch capability {
	case "fs_list", "operator_mount.fs_list":
		return ResultClassification{
			Exposure: ResultExposureDisplay,
			Eligibility: ResultEligibility{
				Prompt: true,
				Memory: true,
			},
		}, ""
	case "fs_write", "operator_mount.fs_write":
		return ResultClassification{
			Exposure: ResultExposureDisplay,
			Eligibility: ResultEligibility{
				Prompt: true,
				Memory: false,
			},
		}, ""
	case "shell_exec":
		return ResultClassification{
			Exposure: ResultExposureDisplay,
			Eligibility: ResultEligibility{
				Prompt: true,
				Memory: false,
			},
		}, ""
	case "fs_read", "operator_mount.fs_read":
		return ResultClassification{
			Exposure: ResultExposureDisplay,
			Eligibility: ResultEligibility{
				Prompt: true,
				Memory: false,
			},
		}, ""
	case "fs_mkdir", "operator_mount.fs_mkdir":
		return ResultClassification{
			Exposure: ResultExposureDisplay,
			Eligibility: ResultEligibility{
				Prompt: true,
				Memory: false,
			},
		}, ""
	case "journal.read", "journal.write", "journal.list",
		"haven.operator_context",
		"notes.read", "notes.write", "notes.list",
		"note.create", "paint.save", "paint.list",
		"desktop.organize":
		return ResultClassification{
			Exposure: ResultExposureDisplay,
			Eligibility: ResultEligibility{
				Prompt: true,
				Memory: false,
			},
		}, ""
	default:
		return ResultClassification{
			Exposure: ResultExposureAudit,
			Quarantine: ResultQuarantine{
				Quarantined: true,
			},
		}, ""
	}
}

func buildCapabilityResult(capability string, arguments map[string]string, output string) (map[string]interface{}, map[string]ResultFieldMetadata, ResultClassification, string, error) {
	switch capability {
	case "shell_exec":
		structuredResult := map[string]interface{}{
			"command": arguments["command"],
			"output":  output,
		}
		classification := ResultClassification{
			Exposure: ResultExposureDisplay,
			Eligibility: ResultEligibility{
				Prompt: true,
				Memory: false,
			},
		}
		fieldsMeta, err := fieldsMetadataForStructuredResult(structuredResult, ResultFieldOriginLocal, classification)
		if err != nil {
			return nil, nil, ResultClassification{}, "", err
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
		classification := ResultClassification{
			Exposure: ResultExposureDisplay,
			Eligibility: ResultEligibility{
				Prompt: true,
				Memory: true,
			},
		}
		fieldsMeta, err := fieldsMetadataForStructuredResult(structuredResult, ResultFieldOriginLocal, classification)
		if err != nil {
			return nil, nil, ResultClassification{}, "", err
		}
		return structuredResult, fieldsMeta, classification, "", nil
	case "fs_write", "operator_mount.fs_write":
		structuredResult := map[string]interface{}{
			"path":    arguments["path"],
			"bytes":   len(arguments["content"]),
			"message": output,
		}
		classification := ResultClassification{
			Exposure: ResultExposureDisplay,
			Eligibility: ResultEligibility{
				Prompt: true,
				Memory: false,
			},
		}
		fieldsMeta, err := fieldsMetadataForStructuredResult(structuredResult, ResultFieldOriginLocal, classification)
		if err != nil {
			return nil, nil, ResultClassification{}, "", err
		}
		return structuredResult, fieldsMeta, classification, "", nil
	case "fs_read", "operator_mount.fs_read":
		structuredResult := map[string]interface{}{
			"path":    arguments["path"],
			"content": output,
			"bytes":   len(output),
		}
		classification := ResultClassification{
			Exposure: ResultExposureDisplay,
			Eligibility: ResultEligibility{
				Prompt: true,
				Memory: false,
			},
		}
		fieldsMeta, err := fieldsMetadataForStructuredResult(structuredResult, ResultFieldOriginLocal, classification)
		if err != nil {
			return nil, nil, ResultClassification{}, "", err
		}
		return structuredResult, fieldsMeta, classification, "", nil
	default:
		classification, quarantineRef := classifyCapabilityResult(capability)
		if !classification.AuditOnly() {
			structuredResult := map[string]interface{}{
				"output": output,
			}
			fieldsMeta, err := fieldsMetadataForStructuredResult(structuredResult, ResultFieldOriginLocal, classification)
			if err != nil {
				return nil, nil, ResultClassification{}, "", err
			}
			return structuredResult, fieldsMeta, classification, quarantineRef, nil
		}
		return map[string]interface{}{}, nil, classification, quarantineRef, nil
	}
}

func normalizeResultClassification(classification ResultClassification, quarantineRef string) (ResultClassification, error) {
	classification.Quarantine.Ref = strings.TrimSpace(quarantineRef)
	if err := classification.Validate(); err != nil {
		return ResultClassification{}, err
	}
	return classification, nil
}

func fieldsMetadataForStructuredResult(structuredResult map[string]interface{}, fieldOrigin string, classification ResultClassification) (map[string]ResultFieldMetadata, error) {
	fieldsMeta := make(map[string]ResultFieldMetadata, len(structuredResult))
	for fieldName, fieldValue := range structuredResult {
		fieldMetadata, err := buildResultFieldMetadata(fieldValue, fieldOrigin, classification)
		if err != nil {
			return nil, fmt.Errorf("build fields_meta for %q: %w", fieldName, err)
		}
		fieldsMeta[fieldName] = fieldMetadata
	}
	return fieldsMeta, nil
}

func buildResultFieldMetadata(fieldValue interface{}, fieldOrigin string, classification ResultClassification) (ResultFieldMetadata, error) {
	fieldKind, fieldContentType, fieldSizeBytes, err := describeResultFieldValue(fieldValue)
	if err != nil {
		return ResultFieldMetadata{}, err
	}
	fieldMetadata := ResultFieldMetadata{
		Origin:         fieldOrigin,
		ContentType:    fieldContentType,
		Trust:          ResultFieldTrustDeterministic,
		Sensitivity:    sensitivityForResultField(fieldValue),
		SizeBytes:      fieldSizeBytes,
		Kind:           fieldKind,
		ScalarSubclass: scalarSubclassForResultField(fieldValue),
		PromptEligible: classification.PromptEligible(),
		MemoryEligible: classification.MemoryEligible(),
	}
	if err := fieldMetadata.Validate(); err != nil {
		return ResultFieldMetadata{}, err
	}
	return fieldMetadata, nil
}

func describeResultFieldValue(fieldValue interface{}) (string, string, int, error) {
	switch typedFieldValue := fieldValue.(type) {
	case string:
		return ResultFieldKindScalar, "text/plain", len(typedFieldValue), nil
	case bool, float64, float32, int, int64, int32, uint, uint32, uint64:
		encodedFieldBytes, err := json.Marshal(typedFieldValue)
		if err != nil {
			return "", "", 0, fmt.Errorf("encode scalar field: %w", err)
		}
		return ResultFieldKindScalar, "application/json", len(encodedFieldBytes), nil
	case []string, []interface{}:
		encodedFieldBytes, err := json.Marshal(typedFieldValue)
		if err != nil {
			return "", "", 0, fmt.Errorf("encode array field: %w", err)
		}
		return ResultFieldKindArray, "application/json", len(encodedFieldBytes), nil
	case map[string]interface{}:
		encodedFieldBytes, err := json.Marshal(typedFieldValue)
		if err != nil {
			return "", "", 0, fmt.Errorf("encode object field: %w", err)
		}
		return ResultFieldKindObject, "application/json", len(encodedFieldBytes), nil
	default:
		encodedFieldBytes, err := json.Marshal(typedFieldValue)
		if err != nil {
			return "", "", 0, fmt.Errorf("encode structured field: %w", err)
		}
		return ResultFieldKindScalar, "application/json", len(encodedFieldBytes), nil
	}
}

func sensitivityForResultField(fieldValue interface{}) string {
	switch typedFieldValue := fieldValue.(type) {
	case string:
		if strings.TrimSpace(typedFieldValue) == "" {
			return ResultFieldSensitivityBenign
		}
		return ResultFieldSensitivityTaintedText
	case []string:
		if len(typedFieldValue) == 0 {
			return ResultFieldSensitivityBenign
		}
		return ResultFieldSensitivityTaintedText
	case []interface{}:
		if len(typedFieldValue) == 0 {
			return ResultFieldSensitivityBenign
		}
		return ResultFieldSensitivityTaintedText
	default:
		return ResultFieldSensitivityBenign
	}
}

func scalarSubclassForResultField(fieldValue interface{}) string {
	switch typedFieldValue := fieldValue.(type) {
	case bool:
		return ResultFieldScalarSubclassBoolean
	case float64, float32, int, int64, int32, uint, uint32, uint64:
		return ResultFieldScalarSubclassValidatedNumber
	case string:
		if normalizedTimestamp, ok := normalizePromotableTimestamp(typedFieldValue); ok && normalizedTimestamp != "" {
			return ResultFieldScalarSubclassTimestamp
		}
		if identifiers.ValidateSafeIdentifier("result field strict identifier", typedFieldValue) == nil {
			return ResultFieldScalarSubclassStrictIdentifier
		}
		return ResultFieldScalarSubclassShortTextLabel
	default:
		return ""
	}
}
