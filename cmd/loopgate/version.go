package main

import (
	"fmt"
	"io"
	"strings"

	"loopgate/internal/config"
	"loopgate/internal/loopgate"
)

var (
	buildVersion = "dev"
	buildCommit  = "unknown"
	buildDate    = "unknown"
)

func printVersion(w io.Writer) {
	fmt.Fprintf(w, "Loopgate %s\n", strings.TrimSpace(buildVersion))
	if trimmedCommit := strings.TrimSpace(buildCommit); trimmedCommit != "" && trimmedCommit != "unknown" {
		fmt.Fprintf(w, "commit: %s\n", trimmedCommit)
	}
	if trimmedDate := strings.TrimSpace(buildDate); trimmedDate != "" && trimmedDate != "unknown" {
		fmt.Fprintf(w, "built_at: %s\n", trimmedDate)
	}
}

func printStartupSummary(w io.Writer, repoRoot string, socketPath string, server *loopgate.Server) {
	if w == nil || server == nil {
		return
	}

	policyKeyID := "unknown"
	if signatureFile, err := config.LoadPolicySignatureFile(repoRoot); err == nil && strings.TrimSpace(signatureFile.KeyID) != "" {
		policyKeyID = strings.TrimSpace(signatureFile.KeyID)
	}
	auditIntegrity := strings.TrimSpace(server.AuditIntegrityModeMessage())
	auditIntegrity = strings.TrimPrefix(auditIntegrity, "Audit integrity: ")

	fmt.Fprintln(w, "Loopgate started")
	fmt.Fprintf(w, "  version:         %s\n", strings.TrimSpace(buildVersion))
	fmt.Fprintf(w, "  socket:          %s\n", socketPath)
	fmt.Fprintln(w, "  policy:          core/policy/policy.yaml")
	fmt.Fprintf(w, "  policy key:      %s\n", policyKeyID)
	fmt.Fprintf(w, "  audit integrity: %s\n", auditIntegrity)
}
