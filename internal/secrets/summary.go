package secrets

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// RedactedSummary contains a redacted, truncated preview of a string alongside
// its unredacted SHA256 hash and byte length. Used for audit persistence and
// thread event storage where raw content must not be stored.
type RedactedSummary struct {
	Preview   string
	SHA256    string
	Bytes     int
	Truncated bool
	Redacted  bool
}

// SummarizeForPersistence produces a RedactedSummary suitable for audit ledger
// entries and conversation thread events. The preview is redacted via RedactText
// and truncated to maxPreview bytes. The SHA256 is computed from the original
// unredacted input so that identical raw content produces identical hashes
// regardless of redaction changes.
func SummarizeForPersistence(rawInput string, maxPreview int) RedactedSummary {
	normalizedInput := strings.TrimSpace(rawInput)
	redactedPreview := RedactText(normalizedInput)
	wasRedacted := redactedPreview != normalizedInput

	preview := redactedPreview
	truncated := false
	if maxPreview > 0 && len(preview) > maxPreview {
		preview = preview[:maxPreview] + "... (truncated)"
		truncated = true
	}

	hash := sha256.Sum256([]byte(rawInput))
	return RedactedSummary{
		Preview:   preview,
		SHA256:    hex.EncodeToString(hash[:]),
		Bytes:     len(rawInput),
		Truncated: truncated,
		Redacted:  wasRedacted,
	}
}
