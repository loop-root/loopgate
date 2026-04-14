package ui

import "loopgate/internal/secrets"

// Safe runs s through the secrets redaction pipeline before it reaches any
// render function. Call it at the boundary between untrusted caller data and
// UI output to ensure no raw credentials appear in terminal output.
//
//	ui.Approval(ui.ApprovalRequest{Preview: ui.Safe(rawContent)})
//
// It is intentionally a thin wrapper so that all redaction logic stays in the
// secrets package (single responsibility) while the UI package provides a
// named, auditable entry-point that reviewers can grep for.
func Safe(s string) string {
	return secrets.RedactText(s)
}
