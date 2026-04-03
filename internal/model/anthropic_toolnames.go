package model

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const anthropicToolNameMaxLen = 128

// MessagesAPIToolName returns a tool name safe for the Anthropic Messages API.
// Tool names must match ^[a-zA-Z0-9_-]{1,128}$ (see API error: tools.*.custom.name).
//
// Capability ids use dotted segments (e.g. host.folder.list); dots are mapped to
// underscores. Any other rune outside [A-Za-z0-9_-] is replaced with '_' so we
// never send characters the API rejects (Unicode letters, punctuation, spaces).
func MessagesAPIToolName(canonical string) string {
	s := sanitizeAnthropicToolNameRunes(strings.TrimSpace(canonical))
	if len(s) > anthropicToolNameMaxLen {
		s = s[:anthropicToolNameMaxLen]
	}
	if s == "" {
		sum := sha256.Sum256([]byte(canonical))
		// Deterministic fallback; must still match [a-zA-Z0-9_-]+
		return "morph_" + hex.EncodeToString(sum[:4])
	}
	return s
}

func sanitizeAnthropicToolNameRunes(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune(r)
		case r == '.':
			b.WriteByte('_')
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
