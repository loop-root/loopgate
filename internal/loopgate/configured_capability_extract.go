package loopgate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"loopgate/internal/identifiers"
)

func (server *Server) buildCapabilityResult(capabilityRequest CapabilityRequest, output string, sourceQuarantineRef string) (map[string]interface{}, map[string]ResultFieldMetadata, ResultClassification, string, error) {
	capability := capabilityRequest.Capability
	arguments := capabilityRequest.Arguments
	if configuredCapabilityDefinition, found := server.providerRuntime.configuredCapabilities[capability]; found {
		structuredResult := map[string]interface{}{
			"capability": capability,
		}
		fieldsMeta := map[string]ResultFieldMetadata{
			"capability": {
				Origin:         ResultFieldOriginLocal,
				ContentType:    "text/plain",
				Trust:          ResultFieldTrustDeterministic,
				Sensitivity:    ResultFieldSensitivityBenign,
				SizeBytes:      len(capability),
				Kind:           ResultFieldKindScalar,
				ScalarSubclass: ResultFieldScalarSubclassStrictIdentifier,
				PromptEligible: false,
			},
		}
		extractedFieldValues, err := extractConfiguredResponseFields(configuredCapabilityDefinition, output)
		if err != nil {
			return nil, nil, ResultClassification{}, "", err
		}
		for _, responseField := range configuredCapabilityDefinition.ResponseFields {
			fieldValue, found := extractedFieldValues[responseField.Name]
			if !found {
				return nil, nil, ResultClassification{}, "", fmt.Errorf("configured response field %q was not extracted", responseField.Name)
			}
			fieldKind, fieldContentType, fieldSizeBytes, err := describeResultFieldValue(fieldValue)
			if err != nil {
				return nil, nil, ResultClassification{}, "", fmt.Errorf("describe configured response field %q: %w", responseField.Name, err)
			}
			allowArrayField := configuredCapabilityDefinition.Extractor == extractorJSONObjectList && fieldKind == ResultFieldKindArray
			if fieldKind != ResultFieldKindScalar && !allowArrayField {
				return nil, nil, ResultClassification{}, "", fmt.Errorf("configured response field %q must be scalar, got %s", responseField.Name, fieldKind)
			}
			if fieldSizeBytes > responseField.MaxInlineBytes {
				if !responseField.AllowBlobRefFallback {
					return nil, nil, ResultClassification{}, "", fmt.Errorf("configured response field %q exceeded max_inline_bytes", responseField.Name)
				}
				if strings.TrimSpace(sourceQuarantineRef) == "" {
					return nil, nil, ResultClassification{}, "", fmt.Errorf("configured response field %q requires quarantine ref for blob_ref fallback", responseField.Name)
				}
				blobReferenceValue, err := buildBlobRefValue(sourceQuarantineRef, responseField.Name, fieldValue, fieldContentType, fieldSizeBytes)
				if err != nil {
					return nil, nil, ResultClassification{}, "", fmt.Errorf("build blob_ref for configured response field %q: %w", responseField.Name, err)
				}
				structuredResult[responseField.Name] = blobReferenceValue
				fieldsMeta[responseField.Name] = ResultFieldMetadata{
					Origin:         ResultFieldOriginRemote,
					ContentType:    fieldContentType,
					Trust:          ResultFieldTrustDeterministic,
					Sensitivity:    responseField.Sensitivity,
					SizeBytes:      fieldSizeBytes,
					Kind:           ResultFieldKindBlobRef,
					PromptEligible: false,
				}
				continue
			}
			structuredResult[responseField.Name] = fieldValue
			resultFieldMetadata := ResultFieldMetadata{
				Origin:         ResultFieldOriginRemote,
				ContentType:    fieldContentType,
				Trust:          ResultFieldTrustDeterministic,
				Sensitivity:    responseField.Sensitivity,
				SizeBytes:      fieldSizeBytes,
				Kind:           fieldKind,
				PromptEligible: false,
			}
			if fieldKind == ResultFieldKindScalar {
				resultFieldMetadata.ScalarSubclass = scalarSubclassForConfiguredFieldValue(fieldValue, responseField.Sensitivity)
			}
			fieldsMeta[responseField.Name] = resultFieldMetadata
		}
		classification := ResultClassification{
			Exposure: ResultExposureDisplay,
			Quarantine: ResultQuarantine{
				Quarantined: true,
			},
		}
		if err := validateConfiguredFieldsMetadata(structuredResult, fieldsMeta); err != nil {
			return nil, nil, ResultClassification{}, "", err
		}
		return structuredResult, fieldsMeta, classification, "", nil
	}
	return buildCapabilityResult(capability, arguments, output)
}

func buildBlobRefValue(sourceQuarantineRef string, fieldPath string, fieldValue interface{}, fieldContentType string, fieldSizeBytes int) (map[string]interface{}, error) {
	fieldValueHash, err := canonicalResultFieldValueSHA256(fieldValue)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"kind":           ResultFieldKindBlobRef,
		"quarantine_ref": sourceQuarantineRef,
		"field_path":     fieldPath,
		"content_sha256": fieldValueHash,
		"content_type":   fieldContentType,
		"size_bytes":     fieldSizeBytes,
		"storage_state":  quarantineStorageStateBlobPresent,
	}, nil
}

func scalarSubclassForConfiguredFieldValue(fieldValue interface{}, fieldSensitivity string) string {
	switch typedFieldValue := fieldValue.(type) {
	case bool:
		return ResultFieldScalarSubclassBoolean
	case float64, float32, int, int64, int32, uint, uint32, uint64:
		return ResultFieldScalarSubclassValidatedNumber
	case string:
		if normalizedTimestamp, ok := normalizePromotableTimestamp(typedFieldValue); ok && normalizedTimestamp != "" {
			return ResultFieldScalarSubclassTimestamp
		}
		if fieldSensitivity == ResultFieldSensitivityBenign && identifiers.ValidateSafeIdentifier("configured response strict identifier", typedFieldValue) == nil {
			return ResultFieldScalarSubclassStrictIdentifier
		}
		return ResultFieldScalarSubclassShortTextLabel
	default:
		return ""
	}
}

func canonicalResultFieldValueSHA256(fieldValue interface{}) (string, error) {
	canonicalJSONBytes, err := json.Marshal(fieldValue)
	if err != nil {
		return "", fmt.Errorf("marshal result field value: %w", err)
	}
	return payloadSHA256(string(canonicalJSONBytes)), nil
}

func extractConfiguredResponseFields(configuredCapabilityDefinition configuredCapability, rawOutput string) (map[string]interface{}, error) {
	switch {
	case configuredCapabilityDefinition.ContentClass == contentClassStructuredJSONConfig && configuredCapabilityDefinition.Extractor == extractorJSONFieldAllowlistConfig:
		var parsedResponse map[string]interface{}
		if err := json.Unmarshal([]byte(rawOutput), &parsedResponse); err != nil {
			return nil, fmt.Errorf("decode configured capability response: %w", err)
		}
		extractedFieldValues := make(map[string]interface{}, len(configuredCapabilityDefinition.ResponseFields))
		for _, responseField := range configuredCapabilityDefinition.ResponseFields {
			fieldValue, found := parsedResponse[responseField.JSONField]
			if !found {
				return nil, fmt.Errorf("configured response field %q is missing source field %q", responseField.Name, responseField.JSONField)
			}
			extractedFieldValues[responseField.Name] = fieldValue
		}
		return extractedFieldValues, nil
	case configuredCapabilityDefinition.ContentClass == contentClassStructuredJSONConfig && configuredCapabilityDefinition.Extractor == extractorJSONNestedSelectorConfig:
		var parsedResponse map[string]interface{}
		if err := json.Unmarshal([]byte(rawOutput), &parsedResponse); err != nil {
			return nil, fmt.Errorf("decode configured capability response: %w", err)
		}
		extractedFieldValues := make(map[string]interface{}, len(configuredCapabilityDefinition.ResponseFields))
		for _, responseField := range configuredCapabilityDefinition.ResponseFields {
			fieldValue, err := extractNestedJSONField(parsedResponse, responseField.JSONPath)
			if err != nil {
				return nil, fmt.Errorf("configured response field %q: %w", responseField.Name, err)
			}
			extractedFieldValues[responseField.Name] = fieldValue
		}
		return extractedFieldValues, nil
	case configuredCapabilityDefinition.ContentClass == contentClassStructuredJSONConfig && configuredCapabilityDefinition.Extractor == extractorJSONObjectList:
		var parsedResponse map[string]interface{}
		if err := json.Unmarshal([]byte(rawOutput), &parsedResponse); err != nil {
			return nil, fmt.Errorf("decode configured capability response: %w", err)
		}
		extractedFieldValues := make(map[string]interface{}, len(configuredCapabilityDefinition.ResponseFields))
		for _, responseField := range configuredCapabilityDefinition.ResponseFields {
			fieldValue, err := extractNestedJSONObjectListField(parsedResponse, responseField)
			if err != nil {
				return nil, fmt.Errorf("configured response field %q: %w", responseField.Name, err)
			}
			extractedFieldValues[responseField.Name] = fieldValue
		}
		return extractedFieldValues, nil
	case configuredCapabilityDefinition.ContentClass == contentClassMarkdownConfig && configuredCapabilityDefinition.Extractor == extractorMarkdownFrontmatterKeys:
		parsedFrontmatter, err := parseMarkdownFrontmatter(rawOutput)
		if err != nil {
			return nil, err
		}
		extractedFieldValues := make(map[string]interface{}, len(configuredCapabilityDefinition.ResponseFields))
		for _, responseField := range configuredCapabilityDefinition.ResponseFields {
			fieldValue, found := parsedFrontmatter[responseField.FrontmatterKey]
			if !found {
				return nil, fmt.Errorf("configured response field %q is missing frontmatter_key %q", responseField.Name, responseField.FrontmatterKey)
			}
			extractedFieldValues[responseField.Name] = fieldValue
		}
		return extractedFieldValues, nil
	case configuredCapabilityDefinition.ContentClass == contentClassMarkdownConfig && configuredCapabilityDefinition.Extractor == extractorMarkdownSectionSelector:
		extractedFieldValues := make(map[string]interface{}, len(configuredCapabilityDefinition.ResponseFields))
		for _, responseField := range configuredCapabilityDefinition.ResponseFields {
			fieldValue, err := extractMarkdownSection(rawOutput, responseField.HeadingPath)
			if err != nil {
				return nil, fmt.Errorf("configured response field %q: %w", responseField.Name, err)
			}
			extractedFieldValues[responseField.Name] = fieldValue
		}
		return extractedFieldValues, nil
	case configuredCapabilityDefinition.ContentClass == contentClassHTMLConfig && configuredCapabilityDefinition.Extractor == extractorHTMLMetaAllowlistConfig:
		parsedHTMLMetadata, err := parseHTMLMetadata(rawOutput)
		if err != nil {
			return nil, err
		}
		extractedFieldValues := make(map[string]interface{}, len(configuredCapabilityDefinition.ResponseFields))
		for _, responseField := range configuredCapabilityDefinition.ResponseFields {
			switch {
			case responseField.HTMLTitle:
				if len(parsedHTMLMetadata.Titles) == 0 {
					return nil, fmt.Errorf("configured response field %q is missing html title", responseField.Name)
				}
				if len(parsedHTMLMetadata.Titles) > 1 {
					return nil, fmt.Errorf("configured response field %q matched duplicate html title tags", responseField.Name)
				}
				extractedFieldValues[responseField.Name] = parsedHTMLMetadata.Titles[0]
			case responseField.MetaName != "":
				fieldValue, found := parsedHTMLMetadata.MetaNameValues[responseField.MetaName]
				if !found {
					return nil, fmt.Errorf("configured response field %q is missing meta_name %q", responseField.Name, responseField.MetaName)
				}
				if len(fieldValue.Values) > 1 {
					return nil, fmt.Errorf("configured response field %q matched duplicate meta_name %q", responseField.Name, responseField.MetaName)
				}
				extractedFieldValues[responseField.Name] = fieldValue.Values[0]
			case responseField.MetaProperty != "":
				fieldValue, found := parsedHTMLMetadata.MetaPropertyValues[responseField.MetaProperty]
				if !found {
					return nil, fmt.Errorf("configured response field %q is missing meta_property %q", responseField.Name, responseField.MetaProperty)
				}
				if len(fieldValue.Values) > 1 {
					return nil, fmt.Errorf("configured response field %q matched duplicate meta_property %q", responseField.Name, responseField.MetaProperty)
				}
				extractedFieldValues[responseField.Name] = fieldValue.Values[0]
			default:
				return nil, fmt.Errorf("configured response field %q does not define an html selector", responseField.Name)
			}
		}
		return extractedFieldValues, nil
	default:
		return nil, fmt.Errorf("unsupported configured extractor %q for content_class %q", configuredCapabilityDefinition.Extractor, configuredCapabilityDefinition.ContentClass)
	}
}

func extractNestedJSONObjectListField(parsedResponse map[string]interface{}, responseField configuredCapabilityField) ([]interface{}, error) {
	rawListValue, err := extractNestedJSONValue(parsedResponse, responseField.JSONPath)
	if err != nil {
		return nil, err
	}
	listItems, ok := rawListValue.([]interface{})
	if !ok {
		return nil, fmt.Errorf("json_path %q resolved to non-array value", responseField.JSONPath)
	}
	boundedCount := len(listItems)
	if boundedCount > responseField.MaxItems {
		boundedCount = responseField.MaxItems
	}
	extractedItems := make([]interface{}, 0, boundedCount)
	for itemIndex := 0; itemIndex < boundedCount; itemIndex++ {
		rawItemObject, ok := listItems[itemIndex].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("json_path %q item %d is not an object", responseField.JSONPath, itemIndex)
		}
		extractedItem := make(map[string]interface{}, len(responseField.JSONListItemFields))
		for _, itemFieldName := range responseField.JSONListItemFields {
			itemFieldValue, found := rawItemObject[itemFieldName]
			if !found {
				return nil, fmt.Errorf("json_path %q item %d is missing field %q", responseField.JSONPath, itemIndex, itemFieldName)
			}
			switch itemFieldValue.(type) {
			case map[string]interface{}, []interface{}:
				return nil, fmt.Errorf("json_path %q item %d field %q resolved to non-scalar value", responseField.JSONPath, itemIndex, itemFieldName)
			}
			extractedItem[itemFieldName] = itemFieldValue
		}
		extractedItems = append(extractedItems, extractedItem)
	}
	return extractedItems, nil
}

func extractNestedJSONValue(parsedResponse map[string]interface{}, rawJSONPath string) (interface{}, error) {
	pathParts := strings.Split(strings.TrimSpace(rawJSONPath), ".")
	if len(pathParts) < 2 {
		return nil, fmt.Errorf("json_path %q must contain at least one nested field", rawJSONPath)
	}

	var currentValue interface{} = parsedResponse
	for _, pathPart := range pathParts {
		currentObject, ok := currentValue.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("json_path %q traversed into non-object value", rawJSONPath)
		}
		nextValue, found := currentObject[pathPart]
		if !found {
			return nil, fmt.Errorf("json_path %q is missing field %q", rawJSONPath, pathPart)
		}
		currentValue = nextValue
	}
	return currentValue, nil
}

func extractNestedJSONField(parsedResponse map[string]interface{}, rawJSONPath string) (interface{}, error) {
	currentValue, err := extractNestedJSONValue(parsedResponse, rawJSONPath)
	if err != nil {
		return nil, err
	}
	switch currentValue.(type) {
	case map[string]interface{}, []interface{}:
		return nil, fmt.Errorf("json_path %q resolved to non-scalar value", rawJSONPath)
	default:
		return currentValue, nil
	}
}

type htmlMetadataValues struct {
	Values []string
}

type parsedHTMLMetadata struct {
	Titles             []string
	MetaNameValues     map[string]htmlMetadataValues
	MetaPropertyValues map[string]htmlMetadataValues
}

func parseHTMLMetadata(rawHTML string) (parsedHTMLMetadata, error) {
	normalizedHTML := strings.ReplaceAll(rawHTML, "\r\n", "\n")
	headHTML, err := extractHTMLHead(normalizedHTML)
	if err != nil {
		return parsedHTMLMetadata{}, err
	}
	titleValues, err := extractHTMLTitleValues(headHTML)
	if err != nil {
		return parsedHTMLMetadata{}, err
	}
	metaNameValues := make(map[string]htmlMetadataValues)
	metaPropertyValues := make(map[string]htmlMetadataValues)
	metaTagTexts, err := extractHTMLMetaTagTexts(headHTML)
	if err != nil {
		return parsedHTMLMetadata{}, err
	}
	for _, rawMetaTagText := range metaTagTexts {
		parsedAttributes, err := parseHTMLTagAttributes(rawMetaTagText)
		if err != nil {
			return parsedHTMLMetadata{}, fmt.Errorf("parse html meta tag attributes: %w", err)
		}
		metaContentValue, hasContentValue := parsedAttributes["content"]
		if !hasContentValue {
			continue
		}
		normalizedContentValue := canonicalizeExtractedText(metaContentValue)
		if metaNameValue, hasMetaName := parsedAttributes["name"]; hasMetaName {
			existingValues := metaNameValues[metaNameValue]
			existingValues.Values = append(existingValues.Values, normalizedContentValue)
			metaNameValues[metaNameValue] = existingValues
		}
		if metaPropertyValue, hasMetaProperty := parsedAttributes["property"]; hasMetaProperty {
			existingValues := metaPropertyValues[metaPropertyValue]
			existingValues.Values = append(existingValues.Values, normalizedContentValue)
			metaPropertyValues[metaPropertyValue] = existingValues
		}
	}
	return parsedHTMLMetadata{
		Titles:             titleValues,
		MetaNameValues:     metaNameValues,
		MetaPropertyValues: metaPropertyValues,
	}, nil
}

func extractHTMLHead(normalizedHTML string) (string, error) {
	lowercaseHTML := strings.ToLower(normalizedHTML)
	headStart := strings.Index(lowercaseHTML, "<head")
	if headStart < 0 {
		return "", fmt.Errorf("html head is required")
	}
	headTagEndRelative := strings.Index(lowercaseHTML[headStart:], ">")
	if headTagEndRelative < 0 {
		return "", fmt.Errorf("html head start tag is malformed")
	}
	headContentStart := headStart + headTagEndRelative + 1
	headEndRelative := strings.Index(lowercaseHTML[headContentStart:], "</head>")
	if headEndRelative < 0 {
		return "", fmt.Errorf("html head closing tag is required")
	}
	return normalizedHTML[headContentStart : headContentStart+headEndRelative], nil
}

func extractHTMLTitleValues(headHTML string) ([]string, error) {
	lowercaseHeadHTML := strings.ToLower(headHTML)
	titleValues := make([]string, 0, 1)
	searchOffset := 0
	for {
		titleStartRelative := strings.Index(lowercaseHeadHTML[searchOffset:], "<title")
		if titleStartRelative < 0 {
			return titleValues, nil
		}
		titleStart := searchOffset + titleStartRelative
		titleTagEndRelative := strings.Index(lowercaseHeadHTML[titleStart:], ">")
		if titleTagEndRelative < 0 {
			return nil, fmt.Errorf("html title start tag is malformed")
		}
		titleContentStart := titleStart + titleTagEndRelative + 1
		titleEndRelative := strings.Index(lowercaseHeadHTML[titleContentStart:], "</title>")
		if titleEndRelative < 0 {
			return nil, fmt.Errorf("html title closing tag is required")
		}
		titleContentEnd := titleContentStart + titleEndRelative
		titleValues = append(titleValues, canonicalizeExtractedText(headHTML[titleContentStart:titleContentEnd]))
		searchOffset = titleContentEnd + len("</title>")
	}
}

func extractHTMLMetaTagTexts(headHTML string) ([]string, error) {
	lowercaseHeadHTML := strings.ToLower(headHTML)
	metaTagTexts := make([]string, 0)
	searchOffset := 0
	for {
		metaStartRelative := strings.Index(lowercaseHeadHTML[searchOffset:], "<meta")
		if metaStartRelative < 0 {
			return metaTagTexts, nil
		}
		metaStart := searchOffset + metaStartRelative
		metaTagEnd, err := findHTMLTagEnd(headHTML, metaStart)
		if err != nil {
			return nil, err
		}
		metaTagTexts = append(metaTagTexts, headHTML[metaStart:metaTagEnd+1])
		searchOffset = metaTagEnd + 1
	}
}

func findHTMLTagEnd(rawHTML string, tagStart int) (int, error) {
	inSingleQuotedValue := false
	inDoubleQuotedValue := false
	for position := tagStart; position < len(rawHTML); position++ {
		switch rawHTML[position] {
		case '\'':
			if !inDoubleQuotedValue {
				inSingleQuotedValue = !inSingleQuotedValue
			}
		case '"':
			if !inSingleQuotedValue {
				inDoubleQuotedValue = !inDoubleQuotedValue
			}
		case '>':
			if !inSingleQuotedValue && !inDoubleQuotedValue {
				return position, nil
			}
		}
	}
	return -1, fmt.Errorf("html tag is malformed")
}

func parseHTMLTagAttributes(rawTagText string) (map[string]string, error) {
	trimmedTagText := strings.TrimSpace(rawTagText)
	if !strings.HasPrefix(trimmedTagText, "<") || !strings.HasSuffix(trimmedTagText, ">") {
		return nil, fmt.Errorf("html tag must start with '<' and end with '>'")
	}
	trimmedTagText = strings.TrimSuffix(strings.TrimPrefix(trimmedTagText, "<"), ">")
	trimmedTagText = strings.TrimSpace(strings.TrimSuffix(trimmedTagText, "/"))
	if trimmedTagText == "" {
		return nil, fmt.Errorf("html tag is empty")
	}
	position := 0
	for position < len(trimmedTagText) && !isHTMLAttributeSpace(trimmedTagText[position]) {
		position++
	}
	parsedAttributes := make(map[string]string)
	for position < len(trimmedTagText) {
		for position < len(trimmedTagText) && isHTMLAttributeSpace(trimmedTagText[position]) {
			position++
		}
		if position >= len(trimmedTagText) {
			break
		}
		attributeStart := position
		for position < len(trimmedTagText) && !isHTMLAttributeSpace(trimmedTagText[position]) && trimmedTagText[position] != '=' {
			position++
		}
		if attributeStart == position {
			return nil, fmt.Errorf("html attribute name is empty")
		}
		attributeName := strings.ToLower(trimmedTagText[attributeStart:position])
		for position < len(trimmedTagText) && isHTMLAttributeSpace(trimmedTagText[position]) {
			position++
		}
		attributeValue := ""
		if position < len(trimmedTagText) && trimmedTagText[position] == '=' {
			position++
			for position < len(trimmedTagText) && isHTMLAttributeSpace(trimmedTagText[position]) {
				position++
			}
			if position >= len(trimmedTagText) {
				return nil, fmt.Errorf("html attribute %q is missing value", attributeName)
			}
			switch trimmedTagText[position] {
			case '"', '\'':
				quoteCharacter := trimmedTagText[position]
				position++
				valueStart := position
				for position < len(trimmedTagText) && trimmedTagText[position] != quoteCharacter {
					position++
				}
				if position >= len(trimmedTagText) {
					return nil, fmt.Errorf("html attribute %q has unterminated quoted value", attributeName)
				}
				attributeValue = trimmedTagText[valueStart:position]
				position++
			default:
				valueStart := position
				for position < len(trimmedTagText) && !isHTMLAttributeSpace(trimmedTagText[position]) {
					position++
				}
				attributeValue = trimmedTagText[valueStart:position]
			}
		}
		parsedAttributes[attributeName] = canonicalizeExtractedText(attributeValue)
	}
	return parsedAttributes, nil
}

func isHTMLAttributeSpace(character byte) bool {
	return character == ' ' || character == '\n' || character == '\r' || character == '\t'
}

func canonicalizeExtractedText(rawValue string) string {
	return strings.TrimSpace(strings.ReplaceAll(rawValue, "\r\n", "\n"))
}

func parseMarkdownFrontmatter(rawMarkdown string) (map[string]interface{}, error) {
	normalizedMarkdown := strings.ReplaceAll(rawMarkdown, "\r\n", "\n")
	if !strings.HasPrefix(normalizedMarkdown, "---\n") {
		return nil, fmt.Errorf("markdown frontmatter is required")
	}
	remainingMarkdown := strings.TrimPrefix(normalizedMarkdown, "---\n")
	frontmatterEnd := strings.Index(remainingMarkdown, "\n---\n")
	if frontmatterEnd < 0 {
		return nil, fmt.Errorf("markdown frontmatter closing delimiter is required")
	}
	frontmatterBytes := []byte(remainingMarkdown[:frontmatterEnd])
	var parsedFrontmatter map[string]interface{}
	decoder := yaml.NewDecoder(bytes.NewReader(frontmatterBytes))
	decoder.KnownFields(false)
	if err := decoder.Decode(&parsedFrontmatter); err != nil {
		return nil, fmt.Errorf("decode markdown frontmatter: %w", err)
	}
	if len(parsedFrontmatter) == 0 {
		return nil, fmt.Errorf("markdown frontmatter must not be empty")
	}
	return parsedFrontmatter, nil
}

type markdownHeading struct {
	Level int
	Label string
	Line  int
}

func extractMarkdownSection(rawMarkdown string, headingPath []string) (string, error) {
	normalizedMarkdown := strings.ReplaceAll(rawMarkdown, "\r\n", "\n")
	markdownLines := strings.Split(normalizedMarkdown, "\n")
	parsedHeadings := parseMarkdownHeadings(markdownLines)
	if len(parsedHeadings) == 0 {
		return "", fmt.Errorf("markdown heading_path not found")
	}

	matchedHeadingIndex := -1
	matchCount := 0
	for headingIndex := range parsedHeadings {
		if !markdownHeadingPathMatches(parsedHeadings, headingIndex, headingPath) {
			continue
		}
		matchedHeadingIndex = headingIndex
		matchCount++
	}
	if matchCount == 0 {
		return "", fmt.Errorf("markdown heading_path not found")
	}
	if matchCount > 1 {
		return "", fmt.Errorf("markdown heading_path matched ambiguously")
	}

	matchedHeading := parsedHeadings[matchedHeadingIndex]
	sectionStartLine := matchedHeading.Line + 1
	sectionEndLine := len(markdownLines)
	for nextHeadingIndex := matchedHeadingIndex + 1; nextHeadingIndex < len(parsedHeadings); nextHeadingIndex++ {
		if parsedHeadings[nextHeadingIndex].Level <= matchedHeading.Level {
			sectionEndLine = parsedHeadings[nextHeadingIndex].Line
			break
		}
	}
	if sectionStartLine > sectionEndLine {
		return "", fmt.Errorf("markdown heading_path matched empty section")
	}
	return strings.Join(markdownLines[sectionStartLine:sectionEndLine], "\n"), nil
}

func parseMarkdownHeadings(markdownLines []string) []markdownHeading {
	parsedHeadings := make([]markdownHeading, 0)
	for lineIndex, rawLine := range markdownLines {
		trimmedLine := strings.TrimSpace(rawLine)
		if !strings.HasPrefix(trimmedLine, "#") {
			continue
		}
		hashPrefixLength := 0
		for hashPrefixLength < len(trimmedLine) && trimmedLine[hashPrefixLength] == '#' {
			hashPrefixLength++
		}
		if hashPrefixLength == 0 || hashPrefixLength > 6 {
			continue
		}
		if len(trimmedLine) <= hashPrefixLength || trimmedLine[hashPrefixLength] != ' ' {
			continue
		}
		headingLabel := strings.TrimSpace(trimmedLine[hashPrefixLength+1:])
		if headingLabel == "" {
			continue
		}
		parsedHeadings = append(parsedHeadings, markdownHeading{
			Level: hashPrefixLength,
			Label: headingLabel,
			Line:  lineIndex,
		})
	}
	return parsedHeadings
}

func markdownHeadingPathMatches(parsedHeadings []markdownHeading, headingIndex int, headingPath []string) bool {
	if len(headingPath) == 0 || headingIndex < 0 || headingIndex >= len(parsedHeadings) {
		return false
	}
	pathCursor := len(headingPath) - 1
	currentLevel := parsedHeadings[headingIndex].Level
	for currentHeadingIndex := headingIndex; currentHeadingIndex >= 0 && pathCursor >= 0; currentHeadingIndex-- {
		currentHeading := parsedHeadings[currentHeadingIndex]
		if currentHeading.Level > currentLevel {
			continue
		}
		if currentHeading.Level == currentLevel && currentHeadingIndex != headingIndex {
			continue
		}
		if currentHeading.Label != headingPath[pathCursor] {
			if currentHeading.Level == currentLevel {
				return false
			}
			continue
		}
		pathCursor--
		currentLevel = currentHeading.Level
	}
	return pathCursor == -1
}
