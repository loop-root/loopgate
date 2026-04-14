package loopgate

import (
	"net/http"

	"morph/internal/troubleshoot"
)

// handleDiagnosticReport returns aggregated operator diagnostics (JSON). Requires a valid capability token,
// Unix peer binding, and the same signed-request headers as other privileged GET routes (empty body hash).
// Does not return raw ledger lines.
func (server *Server) handleDiagnosticReport(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityDiagnosticRead) {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
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
	report.AuditExport = server.buildDiagnosticAuditExportReport(request.Context(), report.AuditExport)
	server.writeJSON(writer, http.StatusOK, report)
}

func (server *Server) handleAuditExportTrustCheck(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityDiagnosticRead) {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}
	trustCheckResponse := server.buildAuditExportTrustCheckResponse(request.Context())
	server.writeJSON(writer, http.StatusOK, trustCheckResponse)
}
