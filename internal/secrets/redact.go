package secrets

import (
	"regexp"
	"strings"
)

const redactedPlaceholder = "[REDACTED]"

var textRedactionPatterns = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	{
		pattern:     regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)[^\r\n]+`),
		replacement: `${1}[REDACTED]`,
	},
	{
		pattern:     regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9\-\._~+/]+=*`),
		replacement: `Bearer [REDACTED]`,
	},
	{
		pattern:     regexp.MustCompile(`(?i)\b(password|secret|token|refresh_token|api[_-]?key|client_secret|private_key)\b\s*[:=]\s*"[^"]*"`),
		replacement: `${1}=[REDACTED]`,
	},
	{
		pattern:     regexp.MustCompile(`(?i)\b(password|secret|token|refresh_token|api[_-]?key|client_secret|private_key)\b\s*[:=]\s*'[^']*'`),
		replacement: `${1}=[REDACTED]`,
	},
	{
		pattern:     regexp.MustCompile(`(?i)\bbasic\s+[A-Za-z0-9+/=]+`),
		replacement: `Basic [REDACTED]`,
	},
	{
		pattern:     regexp.MustCompile(`(?i)\b(password|secret|token|refresh_token|api[_-]?key|client_secret|private_key)\b\s*[:=]\s*["']?[^"',;\s]+["']?`),
		replacement: `${1}=[REDACTED]`,
	},
	{
		pattern:     regexp.MustCompile(`(?i)([?&](?:access[_-]?token|refresh[_-]?token|api[_-]?key|client_secret|token|secret|password)=)[^&#\s]+`),
		replacement: `${1}[REDACTED]`,
	},
	{
		pattern:     regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`),
		replacement: `[REDACTED]`,
	},
}

func RedactText(rawText string) string {
	redactedText := rawText
	for _, rule := range textRedactionPatterns {
		redactedText = rule.pattern.ReplaceAllString(redactedText, rule.replacement)
	}
	return redactedText
}

func RedactStructuredFields(rawFields map[string]interface{}) map[string]interface{} {
	if rawFields == nil {
		return nil
	}
	redactedFields := make(map[string]interface{}, len(rawFields))
	for rawKey, rawValue := range rawFields {
		if isSensitiveFieldKey(rawKey) {
			redactedFields[rawKey] = redactedPlaceholder
			continue
		}
		redactedFields[rawKey] = redactValue(rawValue)
	}
	return redactedFields
}

func RedactStringMap(rawFields map[string]string) map[string]interface{} {
	if rawFields == nil {
		return nil
	}
	redactedFields := make(map[string]interface{}, len(rawFields))
	for rawKey, rawValue := range rawFields {
		if isSensitiveFieldKey(rawKey) {
			redactedFields[rawKey] = redactedPlaceholder
			continue
		}
		redactedFields[rawKey] = RedactText(rawValue)
	}
	return redactedFields
}

func redactValue(rawValue interface{}) interface{} {
	switch typedValue := rawValue.(type) {
	case string:
		return RedactText(typedValue)
	case map[string]interface{}:
		return RedactStructuredFields(typedValue)
	case map[string]string:
		return RedactStringMap(typedValue)
	case []string:
		redactedSlice := make([]interface{}, 0, len(typedValue))
		for _, rawString := range typedValue {
			redactedSlice = append(redactedSlice, RedactText(rawString))
		}
		return redactedSlice
	case []interface{}:
		redactedSlice := make([]interface{}, 0, len(typedValue))
		for _, nestedValue := range typedValue {
			redactedSlice = append(redactedSlice, redactValue(nestedValue))
		}
		return redactedSlice
	default:
		return typedValue
	}
}

func isSensitiveFieldKey(rawKey string) bool {
	normalizedKey := strings.ToLower(strings.TrimSpace(rawKey))
	if normalizedKey == "" {
		return false
	}
	sensitiveFragments := []string{
		"secret",
		"token",
		"authorization",
		"password",
		"private_key",
		"apikey",
		"api_key",
		"client_secret",
		"refresh",
		"credential",
	}
	for _, fragment := range sensitiveFragments {
		if strings.Contains(normalizedKey, fragment) {
			return true
		}
	}
	return false
}
