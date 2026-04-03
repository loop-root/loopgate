package identifiers

import (
	"fmt"
	"regexp"
	"strings"
)

var safeIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)

// ValidateSafeIdentifier denies traversal-like and shell-like identifier input.
// This is for labels, usernames, scopes, and account names that should be inert metadata,
// not paths, commands, or expressions.
func ValidateSafeIdentifier(fieldName string, rawValue string) error {
	trimmedValue := strings.TrimSpace(rawValue)
	if trimmedValue == "" {
		return fmt.Errorf("%s is required", fieldName)
	}
	if len(trimmedValue) > 64 {
		return fmt.Errorf("%s exceeds maximum length", fieldName)
	}
	if strings.Contains(trimmedValue, "..") {
		return fmt.Errorf("%s contains traversal pattern", fieldName)
	}
	for _, forbiddenFragment := range []string{
		"/", "\\", "~", "$", "`", ";", "|", "&", ">", "<", "(", ")", "{", "}", "[", "]", "*", "?", "!", "\"", "'",
	} {
		if strings.Contains(trimmedValue, forbiddenFragment) {
			return fmt.Errorf("%s contains forbidden characters", fieldName)
		}
	}
	if !safeIdentifierPattern.MatchString(trimmedValue) {
		return fmt.Errorf("%s must match %s", fieldName, safeIdentifierPattern.String())
	}
	return nil
}
