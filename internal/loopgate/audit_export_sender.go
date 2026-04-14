package loopgate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"morph/internal/secrets"
)

var (
	errAuditExportClientCertificateExpired      = errors.New("audit export client certificate expired")
	errAuditExportClientCertificateExpiringSoon = errors.New("audit export client certificate expires too soon")
	errAuditExportRootCAExpired                 = errors.New("audit export root ca expired")
	errAuditExportRootCAExpiringSoon            = errors.New("audit export root ca expires too soon")
	errAuditExportServerCertificateExpired      = errors.New("audit export server certificate expired")
	errAuditExportServerCertificateExpiringSoon = errors.New("audit export server certificate expires too soon")
	errAuditExportServerIdentityMismatch        = errors.New("audit export server identity mismatch")
)

func (server *Server) flushAuditExportToConfiguredDestination(ctx context.Context) error {
	if !server.runtimeConfig.Logging.AuditExport.Enabled {
		return nil
	}

	exportBatch, err := server.prepareNextAuditExportBatch()
	if err != nil {
		_ = server.markAuditExportAttempt("prepare_batch_failed")
		return err
	}
	if exportBatch.EventCount == 0 {
		return nil
	}

	return server.flushAuditExportBatchToConfiguredDestination(ctx, exportBatch)
}

func (server *Server) flushAuditExportBatchToConfiguredDestination(ctx context.Context, exportBatch auditExportBatch) error {
	switch strings.TrimSpace(server.runtimeConfig.Logging.AuditExport.DestinationKind) {
	case "admin_node":
		return server.flushAuditExportToAdminNode(ctx, exportBatch)
	default:
		_ = server.markAuditExportAttempt("unsupported_destination_kind")
		return fmt.Errorf("unsupported audit export destination kind %q", server.runtimeConfig.Logging.AuditExport.DestinationKind)
	}
}

func (server *Server) flushAuditExportToAdminNode(ctx context.Context, exportBatch auditExportBatch) error {
	ingestRequest, err := server.buildAdminNodeAuditIngestRequest(exportBatch)
	if err != nil {
		_ = server.markAuditExportAttempt("build_ingest_request_failed")
		return err
	}

	requestBytes, err := json.Marshal(ingestRequest)
	if err != nil {
		_ = server.markAuditExportAttempt("marshal_ingest_request_failed")
		return fmt.Errorf("marshal admin-node audit ingest request: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, server.runtimeConfig.Logging.AuditExport.EndpointURL, bytes.NewReader(requestBytes))
	if err != nil {
		_ = server.markAuditExportAttempt("request_build_failed")
		return fmt.Errorf("build admin-node audit ingest request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	authorizationHeader, err := server.resolveAuditExportAuthorizationHeader(ctx)
	if err != nil {
		_ = server.markAuditExportAttempt("authorization_resolve_failed")
		return err
	}
	if strings.TrimSpace(authorizationHeader) != "" {
		httpRequest.Header.Set("Authorization", authorizationHeader)
	}

	httpClient, err := server.auditExportHTTPClient(ctx)
	if err != nil {
		_ = server.markAuditExportAttempt(classifyAuditExportTLSBuildError(err))
		return err
	}

	httpResponse, err := httpClient.Do(httpRequest)
	if err != nil {
		_ = server.markAuditExportAttempt(classifyAuditExportTransportError(err))
		return fmt.Errorf("post admin-node audit ingest request: %w", err)
	}
	defer httpResponse.Body.Close()

	if httpResponse.StatusCode != http.StatusOK && httpResponse.StatusCode != http.StatusAccepted {
		_ = server.markAuditExportAttempt(fmt.Sprintf("http_status_%d", httpResponse.StatusCode))
		return fmt.Errorf("admin-node audit ingest returned http %d", httpResponse.StatusCode)
	}

	var ingestResponse adminNodeAuditIngestResponse
	decoder := json.NewDecoder(httpResponse.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ingestResponse); err != nil {
		_ = server.markAuditExportAttempt("decode_response_failed")
		return fmt.Errorf("decode admin-node audit ingest response: %w", err)
	}
	if strings.TrimSpace(ingestResponse.Status) != "accepted" {
		_ = server.markAuditExportAttempt("unexpected_response_status")
		return fmt.Errorf("admin-node audit ingest response status %q", ingestResponse.Status)
	}
	if ingestResponse.ThroughAuditSequence != exportBatch.ThroughAuditSequence {
		_ = server.markAuditExportAttempt("sequence_mismatch")
		return fmt.Errorf("admin-node audit ingest through_audit_sequence mismatch")
	}
	if strings.TrimSpace(ingestResponse.ThroughEventHash) != strings.TrimSpace(exportBatch.ThroughEventHash) {
		_ = server.markAuditExportAttempt("event_hash_mismatch")
		return fmt.Errorf("admin-node audit ingest through_event_hash mismatch")
	}

	if err := server.markAuditExportSuccess(exportBatch.ThroughAuditSequence, exportBatch.ThroughEventHash); err != nil {
		return fmt.Errorf("mark audit export success: %w", err)
	}
	if server.diagnostic != nil && server.diagnostic.Audit != nil {
		server.diagnostic.Audit.Info("audit_export_flushed",
			"destination_kind", strings.TrimSpace(server.runtimeConfig.Logging.AuditExport.DestinationKind),
			"destination_label", strings.TrimSpace(server.runtimeConfig.Logging.AuditExport.DestinationLabel),
			"through_audit_sequence", exportBatch.ThroughAuditSequence,
			"event_count", exportBatch.EventCount,
			"through_event_hash_prefix", truncateAuditExportHash(exportBatch.ThroughEventHash),
		)
	}
	return nil
}

func truncateAuditExportHash(rawHash string) string {
	trimmedHash := strings.TrimSpace(rawHash)
	if len(trimmedHash) <= 16 {
		return trimmedHash
	}
	return trimmedHash[:16]
}

func (server *Server) auditExportHTTPClient(ctx context.Context) (*http.Client, error) {
	policyRuntime := server.currentPolicyRuntime()
	baseTransport := http.DefaultTransport.(*http.Transport).Clone()
	if existingTransport, ok := policyRuntime.httpClient.Transport.(*http.Transport); ok && existingTransport != nil {
		baseTransport = existingTransport.Clone()
	}
	if server.runtimeConfig.Logging.AuditExport.TLS.Enabled {
		tlsConfig, err := server.buildAuditExportTLSConfig(ctx)
		if err != nil {
			return nil, err
		}
		baseTransport.TLSClientConfig = tlsConfig
	}
	return &http.Client{
		Timeout:   policyRuntime.httpClient.Timeout,
		Transport: baseTransport,
	}, nil
}

func (server *Server) buildAuditExportTLSConfig(ctx context.Context) (*tls.Config, error) {
	auditExportTLSConfig := server.runtimeConfig.Logging.AuditExport.TLS
	if !auditExportTLSConfig.Enabled {
		return nil, nil
	}
	nowUTC := server.now().UTC()

	rootCABytes, err := server.loadAuditExportSecretValue(ctx, auditExportTLSConfig.RootCASecretRef, "audit export root CA")
	if err != nil {
		return nil, err
	}
	defer zeroSecretBytes(rootCABytes)

	clientCertificateBytes, err := server.loadAuditExportSecretValue(ctx, auditExportTLSConfig.ClientCertificateSecretRef, "audit export client certificate")
	if err != nil {
		return nil, err
	}
	defer zeroSecretBytes(clientCertificateBytes)

	clientPrivateKeyBytes, err := server.loadAuditExportSecretValue(ctx, auditExportTLSConfig.ClientPrivateKeySecretRef, "audit export client private key")
	if err != nil {
		return nil, err
	}
	defer zeroSecretBytes(clientPrivateKeyBytes)

	rootPool := x509.NewCertPool()
	if !rootPool.AppendCertsFromPEM(rootCABytes) {
		return nil, fmt.Errorf("%w: audit export root CA PEM is invalid", secrets.ErrSecretValidation)
	}
	rootCertificates, err := parseCertificatesFromPEM(rootCABytes)
	if err != nil {
		return nil, fmt.Errorf("parse audit export root ca pem: %w", err)
	}
	for _, rootCertificate := range rootCertificates {
		if err := validateAuditExportCertificateValidity(nowUTC, rootCertificate, auditExportTLSConfig.MinimumRemainingValiditySeconds, errAuditExportRootCAExpired, errAuditExportRootCAExpiringSoon); err != nil {
			server.emitAuditExportTrustWarningCode(classifyAuditExportTLSBuildError(err))
			return nil, err
		}
	}

	clientCertificate, err := tls.X509KeyPair(clientCertificateBytes, clientPrivateKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("parse audit export mTLS client certificate: %w", err)
	}
	if len(clientCertificate.Certificate) == 0 {
		return nil, fmt.Errorf("%w: audit export client certificate chain is empty", secrets.ErrSecretValidation)
	}
	clientLeafCertificate, err := x509.ParseCertificate(clientCertificate.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("parse audit export client certificate leaf: %w", err)
	}
	if err := validateAuditExportCertificateValidity(nowUTC, clientLeafCertificate, auditExportTLSConfig.MinimumRemainingValiditySeconds, errAuditExportClientCertificateExpired, errAuditExportClientCertificateExpiringSoon); err != nil {
		server.emitAuditExportTrustWarningCode(classifyAuditExportTLSBuildError(err))
		return nil, err
	}

	serverName := strings.TrimSpace(auditExportTLSConfig.ServerName)
	if serverName == "" {
		parsedURL, err := url.Parse(strings.TrimSpace(server.runtimeConfig.Logging.AuditExport.EndpointURL))
		if err != nil {
			return nil, fmt.Errorf("parse audit export endpoint url for tls: %w", err)
		}
		serverName = strings.TrimSpace(parsedURL.Hostname())
	}

	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		ServerName:   serverName,
		RootCAs:      rootPool,
		Certificates: []tls.Certificate{clientCertificate},
		VerifyConnection: func(connectionState tls.ConnectionState) error {
			if len(connectionState.PeerCertificates) == 0 {
				return fmt.Errorf("%w: missing peer certificate", errAuditExportServerIdentityMismatch)
			}
			peerLeafCertificate := connectionState.PeerCertificates[0]
			if err := validateAuditExportCertificateValidity(nowUTC, peerLeafCertificate, auditExportTLSConfig.MinimumRemainingValiditySeconds, errAuditExportServerCertificateExpired, errAuditExportServerCertificateExpiringSoon); err != nil {
				server.emitAuditExportTrustWarningCode(classifyAuditExportTransportError(err))
				return err
			}
			if err := verifyAuditExportPinnedServerPublicKey(peerLeafCertificate, auditExportTLSConfig.PinnedServerPublicKeySHA256); err != nil {
				server.emitAuditExportTrustWarningCode(classifyAuditExportTransportError(err))
				return err
			}
			return nil
		},
	}, nil
}

func (server *Server) resolveAuditExportAuthorizationHeader(ctx context.Context) (string, error) {
	authorizationConfig := server.runtimeConfig.Logging.AuditExport.Authorization
	if authorizationConfig.SecretRef == nil {
		return "", nil
	}

	rawSecretBytes, err := server.loadAuditExportSecretValue(ctx, authorizationConfig.SecretRef, "audit export authorization")
	if err != nil {
		return "", err
	}
	defer zeroSecretBytes(rawSecretBytes)

	trimmedSecretValue := strings.TrimSpace(string(rawSecretBytes))
	if trimmedSecretValue == "" {
		return "", fmt.Errorf("%w: audit export authorization secret is empty", secrets.ErrSecretValidation)
	}

	switch strings.ToLower(strings.TrimSpace(authorizationConfig.Scheme)) {
	case "", "bearer":
		return "Bearer " + trimmedSecretValue, nil
	default:
		return "", fmt.Errorf("%w: unsupported audit export authorization scheme", secrets.ErrSecretValidation)
	}
}

func (server *Server) loadAuditExportSecretValue(ctx context.Context, secretRef *secrets.SecretRef, secretLabel string) ([]byte, error) {
	if secretRef == nil {
		return nil, fmt.Errorf("%w: %s secret ref is missing", secrets.ErrSecretValidation, secretLabel)
	}

	validatedSecretRef := *secretRef
	if err := validatedSecretRef.Validate(); err != nil {
		return nil, fmt.Errorf("validate %s secret ref: %w", secretLabel, err)
	}
	secretStore, err := server.secretStoreForRef(validatedSecretRef)
	if err != nil {
		return nil, fmt.Errorf("resolve %s secret store: %w", secretLabel, err)
	}
	rawSecretBytes, _, err := secretStore.Get(ctx, validatedSecretRef)
	if err != nil {
		return nil, fmt.Errorf("load %s secret: %w", secretLabel, err)
	}
	return rawSecretBytes, nil
}

func zeroSecretBytes(rawSecretBytes []byte) {
	for index := range rawSecretBytes {
		rawSecretBytes[index] = 0
	}
}

func parseCertificatesFromPEM(rawPEM []byte) ([]*x509.Certificate, error) {
	remainingPEM := rawPEM
	parsedCertificates := make([]*x509.Certificate, 0, 1)
	for len(remainingPEM) > 0 {
		decodedBlock, trailingPEM := pem.Decode(remainingPEM)
		if decodedBlock == nil {
			break
		}
		remainingPEM = trailingPEM
		if decodedBlock.Type != "CERTIFICATE" {
			continue
		}
		parsedCertificate, err := x509.ParseCertificate(decodedBlock.Bytes)
		if err != nil {
			return nil, err
		}
		parsedCertificates = append(parsedCertificates, parsedCertificate)
	}
	if len(parsedCertificates) == 0 {
		return nil, fmt.Errorf("%w: no certificates found in PEM", secrets.ErrSecretValidation)
	}
	return parsedCertificates, nil
}

func validateAuditExportCertificateValidity(nowUTC time.Time, certificate *x509.Certificate, minimumRemainingValiditySeconds int, expiredError error, expiringSoonError error) error {
	if certificate == nil {
		return fmt.Errorf("%w: missing certificate", secrets.ErrSecretValidation)
	}
	if nowUTC.Before(certificate.NotBefore.UTC()) {
		return fmt.Errorf("%w: certificate is not valid before %s", expiredError, certificate.NotBefore.UTC().Format(time.RFC3339))
	}
	if !nowUTC.Before(certificate.NotAfter.UTC()) {
		return fmt.Errorf("%w: certificate expired at %s", expiredError, certificate.NotAfter.UTC().Format(time.RFC3339))
	}
	if minimumRemainingValiditySeconds <= 0 {
		return nil
	}
	minimumRemainingDuration := time.Duration(minimumRemainingValiditySeconds) * time.Second
	if certificate.NotAfter.UTC().Sub(nowUTC) < minimumRemainingDuration {
		return fmt.Errorf("%w: certificate expires at %s", expiringSoonError, certificate.NotAfter.UTC().Format(time.RFC3339))
	}
	return nil
}

func verifyAuditExportPinnedServerPublicKey(peerLeafCertificate *x509.Certificate, rawPinnedServerPublicKeySHA256 string) error {
	trimmedPinnedServerPublicKeySHA256 := strings.ToLower(strings.TrimSpace(rawPinnedServerPublicKeySHA256))
	if trimmedPinnedServerPublicKeySHA256 == "" {
		return nil
	}
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(peerLeafCertificate.PublicKey)
	if err != nil {
		return fmt.Errorf("marshal audit export server public key: %w", err)
	}
	computedServerPublicKeySHA256 := sha256.Sum256(publicKeyBytes)
	computedServerPublicKeySHA256Hex := hex.EncodeToString(computedServerPublicKeySHA256[:])
	if subtle.ConstantTimeCompare([]byte(computedServerPublicKeySHA256Hex), []byte(trimmedPinnedServerPublicKeySHA256)) != 1 {
		return fmt.Errorf("%w: pinned server public key sha256 mismatch", errAuditExportServerIdentityMismatch)
	}
	return nil
}

func classifyAuditExportTLSBuildError(err error) string {
	switch {
	case errors.Is(err, errAuditExportClientCertificateExpired):
		return "client_certificate_expired"
	case errors.Is(err, errAuditExportClientCertificateExpiringSoon):
		return "client_certificate_expiring_soon"
	case errors.Is(err, errAuditExportRootCAExpired):
		return "root_ca_expired"
	case errors.Is(err, errAuditExportRootCAExpiringSoon):
		return "root_ca_expiring_soon"
	default:
		return "tls_client_build_failed"
	}
}

func classifyAuditExportTransportError(err error) string {
	switch {
	case errors.Is(err, errAuditExportServerIdentityMismatch):
		return "server_identity_mismatch"
	case errors.Is(err, errAuditExportServerCertificateExpired):
		return "server_certificate_expired"
	case errors.Is(err, errAuditExportServerCertificateExpiringSoon):
		return "server_certificate_expiring_soon"
	default:
		return "transport_error"
	}
}
