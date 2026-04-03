package loopgate

import (
	"net/http"

	"morph/internal/troubleshoot"
)

// handleDiagnosticReport returns aggregated operator diagnostics (JSON). Requires a valid capability token
// and Unix peer binding — same trust model as other privileged routes. Does not return raw ledger lines.
func (server *Server) handleDiagnosticReport(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, ok := server.authenticate(writer, request); !ok {
		return
	}
	report, err := troubleshoot.BuildReport(server.repoRoot, server.runtimeConfig)
	if err != nil {
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeExecutionFailed,
			Redacted:     true,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, report)
}
