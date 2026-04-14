package loopgate

import (
	"context"
	"crypto/x509"
	"net/url"
	"strings"
	"time"

	"loopgate/internal/secrets"
	"loopgate/internal/troubleshoot"
)

func (server *Server) buildAuditExportTrustCheckResponse(ctx context.Context) AuditExportTrustCheckResponse {
	auditExportReport := server.buildDiagnosticAuditExportReport(ctx, troubleshoot.AuditExportReport{})
	return buildAuditExportTrustCheckResponseFromReport(auditExportReport)
}

func (server *Server) buildDiagnosticAuditExportReport(ctx context.Context, report troubleshoot.AuditExportReport) troubleshoot.AuditExportReport {
	report.Enabled = server.runtimeConfig.Logging.AuditExport.Enabled
	report.DestinationKind = strings.TrimSpace(server.runtimeConfig.Logging.AuditExport.DestinationKind)
	report.DestinationLabel = strings.TrimSpace(server.runtimeConfig.Logging.AuditExport.DestinationLabel)
	report.AuthorizationConfigured = server.runtimeConfig.Logging.AuditExport.Authorization.SecretRef != nil
	report.TLSEnabled = server.runtimeConfig.Logging.AuditExport.TLS.Enabled
	report.MinimumRemainingValiditySeconds = server.runtimeConfig.Logging.AuditExport.TLS.MinimumRemainingValiditySeconds
	trimmedPinnedServerPublicKeySHA256 := strings.TrimSpace(server.runtimeConfig.Logging.AuditExport.TLS.PinnedServerPublicKeySHA256)
	report.PinnedServerPublicKeyConfigured = trimmedPinnedServerPublicKeySHA256 != ""
	if trimmedPinnedServerPublicKeySHA256 != "" {
		report.PinnedServerPublicKeySHA256Prefix = truncateAuditExportHash(trimmedPinnedServerPublicKeySHA256)
	}
	if parsedEndpointURL, err := url.Parse(strings.TrimSpace(server.runtimeConfig.Logging.AuditExport.EndpointURL)); err == nil {
		report.EndpointScheme = strings.TrimSpace(parsedEndpointURL.Scheme)
		report.EndpointHost = strings.TrimSpace(parsedEndpointURL.Hostname())
	}

	if server.runtimeConfig.Logging.AuditExport.Enabled {
		server.auditExportMu.Lock()
		stateFile, err := server.loadAuditExportStateLocked()
		server.auditExportMu.Unlock()
		if err == nil {
			report.LastAttemptAtUTC = strings.TrimSpace(stateFile.LastAttemptAtUTC)
			report.LastSuccessAtUTC = strings.TrimSpace(stateFile.LastSuccessAtUTC)
			report.LastExportedAuditSequence = stateFile.LastExportedAuditSequence
			report.ConsecutiveFailures = stateFile.ConsecutiveFailures
			report.LastErrorClass = strings.TrimSpace(stateFile.LastErrorClass)
		}
	}

	report.Trust = server.auditExportTrustDiagnosticReport(ctx)
	server.emitAuditExportTrustWarnings(report.Trust)
	return report
}

func buildAuditExportTrustCheckResponseFromReport(report troubleshoot.AuditExportReport) AuditExportTrustCheckResponse {
	response := AuditExportTrustCheckResponse{
		DestinationKind:     strings.TrimSpace(report.DestinationKind),
		DestinationLabel:    strings.TrimSpace(report.DestinationLabel),
		EndpointScheme:      strings.TrimSpace(report.EndpointScheme),
		EndpointHost:        strings.TrimSpace(report.EndpointHost),
		LastErrorClass:      strings.TrimSpace(report.LastErrorClass),
		ConsecutiveFailures: report.ConsecutiveFailures,
		Trust:               report.Trust,
	}

	if !report.Enabled {
		response.Status = "disabled"
		response.Summary = "Audit export is disabled."
		return response
	}

	if !report.TLSEnabled && isLoopbackAuditExportHost(response.EndpointHost) {
		response.Status = "local_test_mode"
		response.Summary = "Audit export is configured for a local loopback sink without TLS."
		return response
	}

	if response.LastErrorClass == "server_identity_mismatch" {
		response.Status = "action_required"
		response.ActionNeeded = true
		response.Summary = "The configured admin-node identity no longer matches the pinned server key."
		response.RecommendedAction = "Review the admin-node certificate rotation and update the pinned server public key only if the new identity is expected."
		return response
	}

	switch report.Trust.RootCA.Status {
	case "missing":
		response.Status = "action_required"
		response.ActionNeeded = true
		response.Summary = "The audit-export root CA is not configured."
		response.RecommendedAction = "Configure the audit-export root CA secret ref before relying on remote admin-node export."
		return response
	case "unavailable", "invalid", "expired":
		response.Status = "action_required"
		response.ActionNeeded = true
		response.Summary = "The audit-export root CA is not usable."
		response.RecommendedAction = "Repair or replace the configured audit-export root CA bundle, then rerun the trust check before exporting."
		return response
	}

	switch report.Trust.ClientCertificate.Status {
	case "missing":
		response.Status = "action_required"
		response.ActionNeeded = true
		response.Summary = "The audit-export client certificate is not configured."
		response.RecommendedAction = "Configure the audit-export client certificate and private key secret refs before relying on remote admin-node export."
		return response
	case "unavailable", "invalid", "expired":
		response.Status = "action_required"
		response.ActionNeeded = true
		response.Summary = "The audit-export client certificate is not usable."
		response.RecommendedAction = "Repair or replace the configured audit-export client certificate and private key, then rerun the trust check before exporting."
		return response
	}

	if report.Trust.OverallStatus == "expiring_soon" || report.Trust.RootCA.RenewalWindowActive || report.Trust.ClientCertificate.RenewalWindowActive {
		response.Status = "action_required"
		response.ActionNeeded = true
		response.Summary = "Audit export trust material is inside the configured renewal window."
		response.RecommendedAction = "Rotate the root CA or client certificate before the renewal window closes and verify the new material with another trust check."
		return response
	}

	if report.Trust.OverallStatus == "tls_disabled" {
		response.Status = "degraded"
		response.ActionNeeded = true
		response.Summary = "Audit export trust is running without TLS."
		response.RecommendedAction = "Enable mTLS for non-loopback admin-node export or keep this configuration limited to local test sinks."
		return response
	}

	if report.Trust.OverallStatus == "degraded" {
		response.Status = "action_required"
		response.ActionNeeded = true
		response.Summary = "Audit export trust is degraded."
		response.RecommendedAction = "Inspect the trust warnings and last export error class, then repair the configured trust material before exporting."
		return response
	}

	response.Status = "healthy"
	response.Summary = "Audit export trust is healthy."
	return response
}

func isLoopbackAuditExportHost(hostname string) bool {
	trimmedHostname := strings.TrimSpace(strings.ToLower(hostname))
	return trimmedHostname == "" || trimmedHostname == "localhost" || trimmedHostname == "127.0.0.1" || trimmedHostname == "::1"
}

func (server *Server) auditExportTrustDiagnosticReport(ctx context.Context) troubleshoot.AuditExportTrustReport {
	if !server.runtimeConfig.Logging.AuditExport.Enabled {
		return troubleshoot.AuditExportTrustReport{OverallStatus: "disabled"}
	}
	if !server.runtimeConfig.Logging.AuditExport.TLS.Enabled {
		return troubleshoot.AuditExportTrustReport{OverallStatus: "tls_disabled"}
	}

	minimumRemainingValiditySeconds := server.runtimeConfig.Logging.AuditExport.TLS.MinimumRemainingValiditySeconds
	nowUTC := server.now().UTC()

	rootCAStatus, rootCAWarnings := server.auditExportCertificateDiagnosticStatus(
		ctx,
		nowUTC,
		server.runtimeConfig.Logging.AuditExport.TLS.RootCASecretRef,
		minimumRemainingValiditySeconds,
		"audit export root CA",
		"root_ca",
	)
	clientCertificateStatus, clientCertificateWarnings := server.auditExportCertificateDiagnosticStatus(
		ctx,
		nowUTC,
		server.runtimeConfig.Logging.AuditExport.TLS.ClientCertificateSecretRef,
		minimumRemainingValiditySeconds,
		"audit export client certificate",
		"client_certificate",
	)

	allWarnings := append(rootCAWarnings, clientCertificateWarnings...)
	return troubleshoot.AuditExportTrustReport{
		OverallStatus:     deriveAuditExportTrustOverallStatus(rootCAStatus.Status, clientCertificateStatus.Status),
		Warnings:          allWarnings,
		RootCA:            rootCAStatus,
		ClientCertificate: clientCertificateStatus,
	}
}

func (server *Server) auditExportCertificateDiagnosticStatus(
	ctx context.Context,
	nowUTC time.Time,
	secretRef *secrets.SecretRef,
	minimumRemainingValiditySeconds int,
	secretLabel string,
	warningPrefix string,
) (troubleshoot.AuditExportCertificateStatus, []string) {
	diagnosticStatus := troubleshoot.AuditExportCertificateStatus{Configured: secretRef != nil}
	if secretRef == nil {
		diagnosticStatus.Status = "missing"
		return diagnosticStatus, []string{warningPrefix + "_missing"}
	}

	rawSecretBytes, err := server.loadAuditExportSecretValue(ctx, secretRef, secretLabel)
	if err != nil {
		diagnosticStatus.Status = "unavailable"
		return diagnosticStatus, []string{warningPrefix + "_unavailable"}
	}
	defer zeroSecretBytes(rawSecretBytes)

	parsedCertificates, err := parseCertificatesFromPEM(rawSecretBytes)
	if err != nil {
		diagnosticStatus.Status = "invalid"
		return diagnosticStatus, []string{warningPrefix + "_invalid"}
	}
	representativeCertificate := earliestExpiringCertificate(parsedCertificates)
	if representativeCertificate == nil {
		diagnosticStatus.Status = "invalid"
		return diagnosticStatus, []string{warningPrefix + "_invalid"}
	}

	diagnosticStatus.Subject = strings.TrimSpace(representativeCertificate.Subject.String())
	diagnosticStatus.NotBeforeUTC = representativeCertificate.NotBefore.UTC().Format(time.RFC3339)
	diagnosticStatus.NotAfterUTC = representativeCertificate.NotAfter.UTC().Format(time.RFC3339)
	if nowUTC.Before(representativeCertificate.NotAfter.UTC()) {
		diagnosticStatus.RemainingValiditySeconds = int64(representativeCertificate.NotAfter.UTC().Sub(nowUTC).Seconds())
	}
	if minimumRemainingValiditySeconds > 0 {
		renewalThresholdAtUTC := representativeCertificate.NotAfter.UTC().Add(-time.Duration(minimumRemainingValiditySeconds) * time.Second)
		diagnosticStatus.RenewalThresholdAtUTC = renewalThresholdAtUTC.Format(time.RFC3339)
		secondsUntilRenewalThreshold := int64(renewalThresholdAtUTC.Sub(nowUTC).Seconds())
		diagnosticStatus.SecondsUntilRenewalThreshold = secondsUntilRenewalThreshold
		diagnosticStatus.DaysUntilRenewalThreshold = int64(renewalThresholdAtUTC.Sub(nowUTC) / (24 * time.Hour))
		diagnosticStatus.RenewalWindowActive = !nowUTC.Before(renewalThresholdAtUTC)
	}

	if nowUTC.Before(representativeCertificate.NotBefore.UTC()) || !nowUTC.Before(representativeCertificate.NotAfter.UTC()) {
		diagnosticStatus.Status = "expired"
		return diagnosticStatus, []string{warningPrefix + "_expired"}
	}
	if minimumRemainingValiditySeconds > 0 {
		minimumRemainingDuration := time.Duration(minimumRemainingValiditySeconds) * time.Second
		if representativeCertificate.NotAfter.UTC().Sub(nowUTC) < minimumRemainingDuration {
			diagnosticStatus.Status = "expiring_soon"
			return diagnosticStatus, []string{warningPrefix + "_expiring_soon"}
		}
	}
	diagnosticStatus.Status = "ok"
	return diagnosticStatus, nil
}

func earliestExpiringCertificate(parsedCertificates []*x509.Certificate) *x509.Certificate {
	var earliestCertificate *x509.Certificate
	for _, parsedCertificate := range parsedCertificates {
		if parsedCertificate == nil {
			continue
		}
		if earliestCertificate == nil || parsedCertificate.NotAfter.UTC().Before(earliestCertificate.NotAfter.UTC()) {
			earliestCertificate = parsedCertificate
		}
	}
	return earliestCertificate
}

func deriveAuditExportTrustOverallStatus(statuses ...string) string {
	overallStatus := "ok"
	for _, status := range statuses {
		switch strings.TrimSpace(status) {
		case "", "ok":
			continue
		case "expiring_soon":
			if overallStatus == "ok" {
				overallStatus = "expiring_soon"
			}
		default:
			return "degraded"
		}
	}
	return overallStatus
}

func (server *Server) emitAuditExportTrustWarnings(trustReport troubleshoot.AuditExportTrustReport) {
	for _, warningCode := range trustReport.Warnings {
		server.emitAuditExportTrustWarningCode(warningCode)
	}
}

func (server *Server) emitAuditExportTrustWarningCode(warningCode string) {
	if server == nil || server.diagnostic == nil || server.diagnostic.Server == nil {
		return
	}
	server.diagnostic.Server.Warn("audit_export_trust_warning",
		"warning", warningCode,
		"destination_kind", strings.TrimSpace(server.runtimeConfig.Logging.AuditExport.DestinationKind),
		"destination_label", strings.TrimSpace(server.runtimeConfig.Logging.AuditExport.DestinationLabel),
	)
}
