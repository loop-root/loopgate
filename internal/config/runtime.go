package config

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"loopgate/internal/identifiers"
	"loopgate/internal/secrets"

	"gopkg.in/yaml.v3"
)

const runtimeConfigVersion = "1"

const DefaultSupersededLineageRetentionWindow = 30 * 24 * time.Hour
const expectedSessionClientExecutableEnv = "LOOPGATE_EXPECTED_SESSION_CLIENT_EXECUTABLE"
const DefaultAuditLedgerHMACCheckpointIntervalEvents = 256
const defaultAuditLedgerHMACSecretID = "audit_ledger_hmac"
const defaultAuditLedgerHMACSecretBackend = secrets.BackendMacOSKeychain
const defaultAuditLedgerHMACSecretAccountName = "loopgate.audit_ledger_hmac"
const defaultAuditLedgerHMACSecretScope = "local"

// DiagnosticLogging configures optional text log files (slog) for local troubleshooting.
// DefaultLevel: error | warn | info | debug | trace (trace is finer than debug).
// Per-channel levels in Levels override DefaultLevel for that channel key:
// audit, server, client, socket, ledger, model.
type DiagnosticLogging struct {
	Enabled      bool              `yaml:"enabled" json:"enabled"`
	DefaultLevel string            `yaml:"default_level" json:"default_level"`
	Directory    string            `yaml:"directory" json:"directory"`
	Files        DiagnosticFiles   `yaml:"files" json:"files"`
	Levels       map[string]string `yaml:"levels" json:"levels,omitempty"`
}

// DiagnosticFiles names operator log basenames (created under Directory).
type DiagnosticFiles struct {
	Audit  string `yaml:"audit" json:"audit"`
	Server string `yaml:"server" json:"server"`
	Client string `yaml:"client" json:"client"`
	Socket string `yaml:"socket" json:"socket"`
	Ledger string `yaml:"ledger" json:"ledger"`
	Model  string `yaml:"model" json:"model"`
}

// AuditLedgerHMACCheckpoint configures optional HMAC-signed checkpoint lines in the control-plane audit JSONL.
// When enabled, Loopgate appends audit.ledger.hmac_checkpoint after every IntervalEvents non-checkpoint audit events.
// IntervalEvents zero/unset defaults to 256 in applyRuntimeConfigDefaults; negative values are rejected at validate.
// The signing key is loaded via secret_ref (macOS: macos_keychain; CI/tests: env with account_name = env var name).
type AuditLedgerHMACCheckpoint struct {
	Enabled        bool                      `yaml:"enabled" json:"enabled"`
	IntervalEvents int                       `yaml:"interval_events" json:"interval_events"`
	SecretRef      *AuditLedgerHMACSecretRef `yaml:"secret_ref,omitempty" json:"secret_ref,omitempty"`
}

// AuditLedgerHMACSecretRef references secret material for audit ledger checkpoint HMAC (same shape as secrets.SecretRef).
type AuditLedgerHMACSecretRef struct {
	ID          string `yaml:"id" json:"id"`
	Backend     string `yaml:"backend" json:"backend"`
	AccountName string `yaml:"account_name" json:"account_name"`
	Scope       string `yaml:"scope" json:"scope"`
}

type AuditExport struct {
	Enabled                 bool                     `yaml:"enabled" json:"enabled"`
	DestinationKind         string                   `yaml:"destination_kind" json:"destination_kind"`
	DestinationLabel        string                   `yaml:"destination_label" json:"destination_label"`
	EndpointURL             string                   `yaml:"endpoint_url" json:"endpoint_url"`
	Authorization           AuditExportAuthorization `yaml:"authorization" json:"authorization"`
	TLS                     AuditExportTLS           `yaml:"tls" json:"tls"`
	StatePath               string                   `yaml:"state_path" json:"state_path"`
	MaxBatchEvents          int                      `yaml:"max_batch_events" json:"max_batch_events"`
	MaxBatchBytes           int                      `yaml:"max_batch_bytes" json:"max_batch_bytes"`
	MinFlushIntervalSeconds int                      `yaml:"min_flush_interval_seconds" json:"min_flush_interval_seconds"`
}

type AuditExportAuthorization struct {
	Scheme    string             `yaml:"scheme" json:"scheme"`
	SecretRef *secrets.SecretRef `yaml:"secret_ref,omitempty" json:"secret_ref,omitempty"`
}

type AuditExportTLS struct {
	Enabled                         bool               `yaml:"enabled" json:"enabled"`
	ServerName                      string             `yaml:"server_name" json:"server_name"`
	PinnedServerPublicKeySHA256     string             `yaml:"pinned_server_public_key_sha256" json:"pinned_server_public_key_sha256"`
	MinimumRemainingValiditySeconds int                `yaml:"minimum_remaining_validity_seconds" json:"minimum_remaining_validity_seconds"`
	RootCASecretRef                 *secrets.SecretRef `yaml:"root_ca_secret_ref,omitempty" json:"root_ca_secret_ref,omitempty"`
	ClientCertificateSecretRef      *secrets.SecretRef `yaml:"client_certificate_secret_ref,omitempty" json:"client_certificate_secret_ref,omitempty"`
	ClientPrivateKeySecretRef       *secrets.SecretRef `yaml:"client_private_key_secret_ref,omitempty" json:"client_private_key_secret_ref,omitempty"`
}

// ResolvedDirectory returns the log directory relative to repo root (after defaults).
func (d DiagnosticLogging) ResolvedDirectory() string {
	dir := strings.TrimSpace(d.Directory)
	if dir == "" {
		return "runtime/logs"
	}
	return dir
}

// LevelForChannel returns the configured level for a channel or DefaultLevel.
func (d DiagnosticLogging) LevelForChannel(channel string) string {
	if d.Levels != nil {
		if v, ok := d.Levels[channel]; ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return d.DefaultLevel
}

type RuntimeConfig struct {
	Version string `yaml:"version" json:"version"`
	Logging struct {
		AuditLedger struct {
			MaxEventBytes                 int                       `yaml:"max_event_bytes" json:"max_event_bytes"`
			RotateAtBytes                 int64                     `yaml:"rotate_at_bytes" json:"rotate_at_bytes"`
			SegmentDir                    string                    `yaml:"segment_dir" json:"segment_dir"`
			ManifestPath                  string                    `yaml:"manifest_path" json:"manifest_path"`
			VerifyClosedSegmentsOnStartup *bool                     `yaml:"verify_closed_segments_on_startup" json:"verify_closed_segments_on_startup"`
			HMACCheckpoint                AuditLedgerHMACCheckpoint `yaml:"hmac_checkpoint" json:"hmac_checkpoint"`
		} `yaml:"audit_ledger" json:"audit_ledger"`
		AuditExport AuditExport `yaml:"audit_export" json:"audit_export"`
		// Diagnostic is non-authoritative operator telemetry (text files under runtime/logs or runtime/state).
		// It must not replace loopgate_events.jsonl; never log secrets or raw tokens.
		Diagnostic DiagnosticLogging `yaml:"diagnostic" json:"diagnostic"`
	} `yaml:"logging" json:"logging"`
	// Tenancy holds deployment-scoped identity for single-node enterprise prep.
	// Values are applied at control-session open (never taken from untrusted client JSON).
	Tenancy struct {
		DeploymentTenantID string `yaml:"deployment_tenant_id" json:"deployment_tenant_id"`
		DeploymentUserID   string `yaml:"deployment_user_id" json:"deployment_user_id"`
	} `yaml:"tenancy" json:"tenancy"`
	// ControlPlane holds optional hardening for the local Unix-socket control plane.
	ControlPlane struct {
		// ExpectedSessionClientExecutable, when non-empty, requires POST /v1/session/open peers to
		// resolve to this absolute executable path (after filepath.Clean). Empty disables pinning.
		ExpectedSessionClientExecutable string `yaml:"expected_session_client_executable" json:"expected_session_client_executable"`
	} `yaml:"control_plane" json:"control_plane"`
}

func LoadRuntimeConfig(repoRoot string) (RuntimeConfig, error) {
	path := filepath.Join(repoRoot, "config", "runtime.yaml")
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			runtimeConfig := defaultRuntimeConfig()
			if err := applyExpectedSessionClientExecutableOverride(&runtimeConfig); err != nil {
				return RuntimeConfig{}, err
			}
			if err := ApplyDiagnosticLoggingOverride(repoRoot, &runtimeConfig); err != nil {
				return RuntimeConfig{}, err
			}
			if err := validateRuntimeConfig(repoRoot, runtimeConfig); err != nil {
				return RuntimeConfig{}, err
			}
			return runtimeConfig, nil
		}
		return RuntimeConfig{}, err
	}

	var runtimeConfig RuntimeConfig
	decoder := yaml.NewDecoder(bytes.NewReader(rawBytes))
	decoder.KnownFields(true)
	if err := decoder.Decode(&runtimeConfig); err != nil {
		return RuntimeConfig{}, err
	}
	applyRuntimeConfigDefaults(&runtimeConfig)
	if err := applyExpectedSessionClientExecutableOverride(&runtimeConfig); err != nil {
		return RuntimeConfig{}, err
	}
	if err := ApplyDiagnosticLoggingOverride(repoRoot, &runtimeConfig); err != nil {
		return RuntimeConfig{}, err
	}
	if err := validateRuntimeConfig(repoRoot, runtimeConfig); err != nil {
		return RuntimeConfig{}, err
	}
	return runtimeConfig, nil
}

func defaultRuntimeConfig() RuntimeConfig {
	return DefaultRuntimeConfig()
}

// DefaultRuntimeConfig returns a RuntimeConfig with all defaults applied.
func DefaultRuntimeConfig() RuntimeConfig {
	runtimeConfig := RuntimeConfig{}
	applyRuntimeConfigDefaults(&runtimeConfig)
	return runtimeConfig
}

func DefaultAuditLedgerHMACSecretRef() AuditLedgerHMACSecretRef {
	return AuditLedgerHMACSecretRef{
		ID:          defaultAuditLedgerHMACSecretID,
		Backend:     defaultAuditLedgerHMACSecretBackend,
		AccountName: defaultAuditLedgerHMACSecretAccountName,
		Scope:       defaultAuditLedgerHMACSecretScope,
	}
}

func DefaultAuditLedgerHMACCheckpoint() AuditLedgerHMACCheckpoint {
	secretRef := DefaultAuditLedgerHMACSecretRef()
	return AuditLedgerHMACCheckpoint{
		Enabled:        true,
		IntervalEvents: DefaultAuditLedgerHMACCheckpointIntervalEvents,
		SecretRef:      &secretRef,
	}
}

func IsDefaultAuditLedgerHMACSecretRef(secretRef *AuditLedgerHMACSecretRef) bool {
	if secretRef == nil {
		return false
	}
	defaultSecretRef := DefaultAuditLedgerHMACSecretRef()
	return strings.TrimSpace(secretRef.ID) == defaultSecretRef.ID &&
		strings.TrimSpace(secretRef.Backend) == defaultSecretRef.Backend &&
		strings.TrimSpace(secretRef.AccountName) == defaultSecretRef.AccountName &&
		strings.TrimSpace(secretRef.Scope) == defaultSecretRef.Scope
}

func applyRuntimeConfigDefaults(runtimeConfig *RuntimeConfig) {
	if strings.TrimSpace(runtimeConfig.Version) == "" {
		runtimeConfig.Version = runtimeConfigVersion
	}
	if runtimeConfig.Logging.AuditLedger.MaxEventBytes <= 0 {
		runtimeConfig.Logging.AuditLedger.MaxEventBytes = 256 * 1024
	}
	if runtimeConfig.Logging.AuditLedger.RotateAtBytes <= 0 {
		runtimeConfig.Logging.AuditLedger.RotateAtBytes = 128 * 1024 * 1024
	}
	if strings.TrimSpace(runtimeConfig.Logging.AuditLedger.SegmentDir) == "" {
		runtimeConfig.Logging.AuditLedger.SegmentDir = "runtime/state/loopgate_event_segments"
	}
	if strings.TrimSpace(runtimeConfig.Logging.AuditLedger.ManifestPath) == "" {
		runtimeConfig.Logging.AuditLedger.ManifestPath = "runtime/state/loopgate_event_segments/manifest.jsonl"
	}
	if runtimeConfig.Logging.AuditLedger.VerifyClosedSegmentsOnStartup == nil {
		defaultVerifyClosedSegments := true
		runtimeConfig.Logging.AuditLedger.VerifyClosedSegmentsOnStartup = &defaultVerifyClosedSegments
	}
	if strings.TrimSpace(runtimeConfig.Logging.AuditExport.StatePath) == "" {
		runtimeConfig.Logging.AuditExport.StatePath = "runtime/state/audit_export_state.json"
	}
	if runtimeConfig.Logging.AuditExport.MaxBatchEvents <= 0 {
		runtimeConfig.Logging.AuditExport.MaxBatchEvents = 500
	}
	if runtimeConfig.Logging.AuditExport.MaxBatchBytes <= 0 {
		runtimeConfig.Logging.AuditExport.MaxBatchBytes = 1024 * 1024
	}
	if runtimeConfig.Logging.AuditExport.MinFlushIntervalSeconds <= 0 {
		runtimeConfig.Logging.AuditExport.MinFlushIntervalSeconds = 5
	}
	if strings.TrimSpace(runtimeConfig.Logging.AuditExport.Authorization.Scheme) == "" {
		runtimeConfig.Logging.AuditExport.Authorization.Scheme = "bearer"
	}
	if strings.TrimSpace(runtimeConfig.Logging.AuditExport.TLS.ServerName) == "" {
		runtimeConfig.Logging.AuditExport.TLS.ServerName = ""
	}
	hc := &runtimeConfig.Logging.AuditLedger.HMACCheckpoint
	// Default only the unset (zero) case so negative values fail validation instead of being coerced.
	if hc.Enabled && hc.IntervalEvents == 0 {
		hc.IntervalEvents = DefaultAuditLedgerHMACCheckpointIntervalEvents
	}
	d := &runtimeConfig.Logging.Diagnostic
	if strings.TrimSpace(d.DefaultLevel) == "" {
		d.DefaultLevel = "info"
	}
	if strings.TrimSpace(d.Directory) == "" {
		d.Directory = "runtime/logs"
	}
	if strings.TrimSpace(d.Files.Audit) == "" {
		d.Files.Audit = "audit.log"
	}
	if strings.TrimSpace(d.Files.Server) == "" {
		d.Files.Server = "server.log"
	}
	if strings.TrimSpace(d.Files.Client) == "" {
		d.Files.Client = "client.log"
	}
	if strings.TrimSpace(d.Files.Socket) == "" {
		d.Files.Socket = "socket.log"
	}
	if strings.TrimSpace(d.Files.Ledger) == "" {
		d.Files.Ledger = "ledger.log"
	}
	if strings.TrimSpace(d.Files.Model) == "" {
		d.Files.Model = "model.log"
	}
}

func validateRuntimeConfig(repoRoot string, runtimeConfig RuntimeConfig) error {
	if strings.TrimSpace(runtimeConfig.Version) == "" {
		return fmt.Errorf("version is required")
	}
	if runtimeConfig.Logging.AuditLedger.MaxEventBytes <= 0 {
		return fmt.Errorf("logging.audit_ledger.max_event_bytes must be positive")
	}
	if runtimeConfig.Logging.AuditLedger.RotateAtBytes <= 0 {
		return fmt.Errorf("logging.audit_ledger.rotate_at_bytes must be positive")
	}
	if err := validateRuntimeInternalPath(runtimeConfig.Logging.AuditLedger.SegmentDir, true); err != nil {
		return fmt.Errorf("logging.audit_ledger.segment_dir %w", err)
	}
	if err := validateRuntimeInternalPath(runtimeConfig.Logging.AuditLedger.ManifestPath, false); err != nil {
		return fmt.Errorf("logging.audit_ledger.manifest_path %w", err)
	}
	if err := validateAuditExport(runtimeConfig.Logging.AuditExport); err != nil {
		return err
	}
	if runtimeConfig.Logging.Diagnostic.Enabled {
		if err := validateDiagnosticLogDirectory(runtimeConfig.Logging.Diagnostic.Directory); err != nil {
			return fmt.Errorf("logging.diagnostic.directory %w", err)
		}
		if err := validateDiagnosticLevel(runtimeConfig.Logging.Diagnostic.DefaultLevel); err != nil {
			return fmt.Errorf("logging.diagnostic.default_level: %w", err)
		}
		for channel, level := range runtimeConfig.Logging.Diagnostic.Levels {
			if err := validateDiagnosticLevel(level); err != nil {
				return fmt.Errorf("logging.diagnostic.levels[%s]: %w", channel, err)
			}
		}
	}
	if err := validateOptionalDeploymentIdentity("tenancy.deployment_tenant_id", runtimeConfig.Tenancy.DeploymentTenantID); err != nil {
		return err
	}
	if err := validateOptionalDeploymentIdentity("tenancy.deployment_user_id", runtimeConfig.Tenancy.DeploymentUserID); err != nil {
		return err
	}
	if err := validateExpectedSessionClientExecutable(runtimeConfig.ControlPlane.ExpectedSessionClientExecutable); err != nil {
		return err
	}
	if err := validateAuditLedgerHMACCheckpoint(runtimeConfig.Logging.AuditLedger.HMACCheckpoint); err != nil {
		return err
	}
	return nil
}

func validateAuditLedgerHMACCheckpoint(hc AuditLedgerHMACCheckpoint) error {
	if !hc.Enabled {
		return nil
	}
	if hc.IntervalEvents <= 0 {
		return fmt.Errorf("logging.audit_ledger.hmac_checkpoint.interval_events must be positive when enabled")
	}
	if hc.SecretRef == nil {
		return fmt.Errorf("logging.audit_ledger.hmac_checkpoint.secret_ref is required when enabled")
	}
	sr := hc.SecretRef
	if err := identifiers.ValidateSafeIdentifier("logging.audit_ledger.hmac_checkpoint.secret_ref.id", sr.ID); err != nil {
		return err
	}
	if err := identifiers.ValidateSafeIdentifier("logging.audit_ledger.hmac_checkpoint.secret_ref.backend", sr.Backend); err != nil {
		return err
	}
	if err := identifiers.ValidateSafeIdentifier("logging.audit_ledger.hmac_checkpoint.secret_ref.account_name", sr.AccountName); err != nil {
		return err
	}
	if err := identifiers.ValidateSafeIdentifier("logging.audit_ledger.hmac_checkpoint.secret_ref.scope", sr.Scope); err != nil {
		return err
	}
	return nil
}

func validateAuditExport(auditExport AuditExport) error {
	if err := validateRuntimeInternalPath(auditExport.StatePath, false); err != nil {
		return fmt.Errorf("logging.audit_export.state_path %w", err)
	}
	if auditExport.MaxBatchEvents <= 0 {
		return fmt.Errorf("logging.audit_export.max_batch_events must be positive")
	}
	if auditExport.MaxBatchBytes <= 0 {
		return fmt.Errorf("logging.audit_export.max_batch_bytes must be positive")
	}
	if auditExport.MinFlushIntervalSeconds <= 0 {
		return fmt.Errorf("logging.audit_export.min_flush_interval_seconds must be positive")
	}
	trimmedDestinationKind := strings.TrimSpace(auditExport.DestinationKind)
	switch trimmedDestinationKind {
	case "", "admin_node", "splunk_hec", "datadog_http":
	default:
		return fmt.Errorf("logging.audit_export.destination_kind must be one of: admin_node, splunk_hec, datadog_http")
	}
	trimmedDestinationLabel := strings.TrimSpace(auditExport.DestinationLabel)
	if trimmedDestinationLabel != "" {
		if err := identifiers.ValidateSafeIdentifier("logging.audit_export.destination_label", trimmedDestinationLabel); err != nil {
			return err
		}
	}
	trimmedEndpointURL := strings.TrimSpace(auditExport.EndpointURL)
	if trimmedEndpointURL != "" {
		if err := validateAuditExportEndpointURL(trimmedEndpointURL); err != nil {
			return err
		}
	}
	if err := validateAuditExportAuthorization(auditExport.Authorization); err != nil {
		return err
	}
	if err := validateAuditExportTLS(auditExport.TLS); err != nil {
		return err
	}
	if !auditExport.Enabled {
		return nil
	}
	if trimmedDestinationKind == "" {
		return fmt.Errorf("logging.audit_export.destination_kind is required when enabled")
	}
	if trimmedDestinationLabel == "" {
		return fmt.Errorf("logging.audit_export.destination_label is required when enabled")
	}
	if trimmedEndpointURL == "" {
		return fmt.Errorf("logging.audit_export.endpoint_url is required when enabled")
	}
	if trimmedDestinationKind == "admin_node" && auditExport.Authorization.SecretRef == nil {
		return fmt.Errorf("logging.audit_export.authorization.secret_ref is required when enabled for admin_node")
	}
	if trimmedDestinationKind == "admin_node" {
		parsedURL, err := url.Parse(trimmedEndpointURL)
		if err != nil {
			return fmt.Errorf("logging.audit_export.endpoint_url parse: %w", err)
		}
		hostname := strings.TrimSpace(parsedURL.Hostname())
		if !isLocalhostRuntimeHost(hostname) && !auditExport.TLS.Enabled {
			return fmt.Errorf("logging.audit_export.tls.enabled is required for non-loopback admin_node export")
		}
	}
	return nil
}

func validateAuditExportAuthorization(authorization AuditExportAuthorization) error {
	trimmedScheme := strings.ToLower(strings.TrimSpace(authorization.Scheme))
	switch trimmedScheme {
	case "", "bearer":
	default:
		return fmt.Errorf("logging.audit_export.authorization.scheme must be one of: bearer")
	}
	if authorization.SecretRef == nil {
		return nil
	}
	if err := authorization.SecretRef.Validate(); err != nil {
		return fmt.Errorf("logging.audit_export.authorization.secret_ref: %w", err)
	}
	return nil
}

func validateAuditExportTLS(tlsConfig AuditExportTLS) error {
	if strings.TrimSpace(tlsConfig.ServerName) != "" {
		if err := identifiers.ValidateSafeIdentifier("logging.audit_export.tls.server_name", tlsConfig.ServerName); err != nil {
			return err
		}
	}
	trimmedPinnedServerPublicKeySHA256 := strings.ToLower(strings.TrimSpace(tlsConfig.PinnedServerPublicKeySHA256))
	if trimmedPinnedServerPublicKeySHA256 != "" {
		if len(trimmedPinnedServerPublicKeySHA256) != 64 {
			return fmt.Errorf("logging.audit_export.tls.pinned_server_public_key_sha256 must be a 64-character hex sha256")
		}
		if _, err := hex.DecodeString(trimmedPinnedServerPublicKeySHA256); err != nil {
			return fmt.Errorf("logging.audit_export.tls.pinned_server_public_key_sha256 must be valid hex")
		}
	}
	if tlsConfig.MinimumRemainingValiditySeconds < 0 {
		return fmt.Errorf("logging.audit_export.tls.minimum_remaining_validity_seconds must be non-negative")
	}
	if !tlsConfig.Enabled {
		return nil
	}
	if tlsConfig.RootCASecretRef == nil {
		return fmt.Errorf("logging.audit_export.tls.root_ca_secret_ref is required when tls.enabled")
	}
	if err := tlsConfig.RootCASecretRef.Validate(); err != nil {
		return fmt.Errorf("logging.audit_export.tls.root_ca_secret_ref: %w", err)
	}
	if tlsConfig.ClientCertificateSecretRef == nil {
		return fmt.Errorf("logging.audit_export.tls.client_certificate_secret_ref is required when tls.enabled")
	}
	if err := tlsConfig.ClientCertificateSecretRef.Validate(); err != nil {
		return fmt.Errorf("logging.audit_export.tls.client_certificate_secret_ref: %w", err)
	}
	if tlsConfig.ClientPrivateKeySecretRef == nil {
		return fmt.Errorf("logging.audit_export.tls.client_private_key_secret_ref is required when tls.enabled")
	}
	if err := tlsConfig.ClientPrivateKeySecretRef.Validate(); err != nil {
		return fmt.Errorf("logging.audit_export.tls.client_private_key_secret_ref: %w", err)
	}
	return nil
}

func validateAuditExportEndpointURL(rawURL string) error {
	parsedURL, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("logging.audit_export.endpoint_url parse: %w", err)
	}
	if parsedURL.User != nil {
		return fmt.Errorf("logging.audit_export.endpoint_url must not include embedded credentials")
	}
	hostname := strings.TrimSpace(parsedURL.Hostname())
	if hostname == "" {
		return fmt.Errorf("logging.audit_export.endpoint_url hostname is required")
	}
	scheme := strings.ToLower(strings.TrimSpace(parsedURL.Scheme))
	if scheme == "" {
		return fmt.Errorf("logging.audit_export.endpoint_url scheme is required")
	}
	if scheme != "https" && !isLocalhostRuntimeHost(hostname) {
		return fmt.Errorf("logging.audit_export.endpoint_url must use https unless hostname is loopback")
	}
	return nil
}

func isLocalhostRuntimeHost(hostname string) bool {
	if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" {
		return true
	}
	parsedIP := net.ParseIP(hostname)
	return parsedIP != nil && parsedIP.IsLoopback()
}

const maxExpectedSessionClientExecutableRunes = 4096

func validateExpectedSessionClientExecutable(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	if len(trimmed) > maxExpectedSessionClientExecutableRunes {
		return fmt.Errorf("control_plane.expected_session_client_executable exceeds maximum length (%d)", maxExpectedSessionClientExecutableRunes)
	}
	if strings.ContainsAny(trimmed, "\x00\n\r") {
		return fmt.Errorf("control_plane.expected_session_client_executable contains control characters")
	}
	if !filepath.IsAbs(filepath.Clean(trimmed)) {
		return fmt.Errorf("control_plane.expected_session_client_executable must be an absolute path when set")
	}
	return nil
}

func applyExpectedSessionClientExecutableOverride(runtimeConfig *RuntimeConfig) error {
	if runtimeConfig == nil {
		return nil
	}
	if strings.TrimSpace(runtimeConfig.ControlPlane.ExpectedSessionClientExecutable) != "" {
		return nil
	}
	rawOverride := strings.TrimSpace(os.Getenv(expectedSessionClientExecutableEnv))
	if rawOverride == "" {
		return nil
	}
	if err := validateExpectedSessionClientExecutable(rawOverride); err != nil {
		return err
	}
	runtimeConfig.ControlPlane.ExpectedSessionClientExecutable = rawOverride
	return nil
}

const maxDeploymentIdentityRunes = 256

// validateOptionalDeploymentIdentity allows empty (personal / unset) or a bounded opaque string
// without control characters. Enterprise IDs may be UUIDs, opaque slugs, or future email-shaped
// subjects; we deliberately avoid the stricter ValidateSafeIdentifier rules here.
func validateOptionalDeploymentIdentity(fieldName string, raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	if len(trimmed) > maxDeploymentIdentityRunes {
		return fmt.Errorf("%s exceeds maximum length (%d)", fieldName, maxDeploymentIdentityRunes)
	}
	if strings.ContainsAny(trimmed, "\x00\n\r") {
		return fmt.Errorf("%s contains control characters", fieldName)
	}
	return nil
}

func validateDiagnosticLevel(raw string) error {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "error", "warn", "info", "debug", "trace":
		return nil
	default:
		return fmt.Errorf("must be one of error, warn, info, debug, trace")
	}
}

func validateDiagnosticLogDirectory(rawPath string) error {
	trimmedPath := strings.TrimSpace(rawPath)
	if trimmedPath == "" {
		return fmt.Errorf("is required when diagnostic logging is enabled")
	}
	if filepath.IsAbs(trimmedPath) {
		return fmt.Errorf("must be relative to the repository root")
	}
	cleanedPath := filepath.Clean(trimmedPath)
	if cleanedPath == "." || cleanedPath == ".." || strings.HasPrefix(cleanedPath, ".."+string(filepath.Separator)) {
		return fmt.Errorf("must stay under runtime/logs or runtime/state")
	}
	logsPrefix := "runtime" + string(filepath.Separator) + "logs"
	statePrefix := "runtime" + string(filepath.Separator) + "state"
	if cleanedPath == logsPrefix || strings.HasPrefix(cleanedPath, logsPrefix+string(filepath.Separator)) {
		return nil
	}
	if cleanedPath == statePrefix || strings.HasPrefix(cleanedPath, statePrefix+string(filepath.Separator)) {
		return nil
	}
	return fmt.Errorf("must be under runtime/logs or runtime/state")
}

func validateRuntimeInternalPath(rawPath string, requireDirectory bool) error {
	trimmedPath := strings.TrimSpace(rawPath)
	if trimmedPath == "" {
		return fmt.Errorf("is required")
	}
	if filepath.IsAbs(trimmedPath) {
		return fmt.Errorf("must be relative to the repository root")
	}
	cleanedPath := filepath.Clean(trimmedPath)
	if cleanedPath == "." || cleanedPath == ".." || strings.HasPrefix(cleanedPath, ".."+string(filepath.Separator)) {
		return fmt.Errorf("must stay within runtime/state")
	}
	if cleanedPath != trimmedPath {
		return fmt.Errorf("must be normalized and must not contain path traversal")
	}
	runtimeStatePrefix := "runtime" + string(filepath.Separator) + "state" + string(filepath.Separator)
	if cleanedPath != "runtime/state" && !strings.HasPrefix(cleanedPath, runtimeStatePrefix) {
		return fmt.Errorf("must stay within runtime/state")
	}
	if requireDirectory && strings.HasSuffix(cleanedPath, ".jsonl") {
		return fmt.Errorf("must be a directory path")
	}
	if !requireDirectory && strings.HasSuffix(cleanedPath, string(filepath.Separator)) {
		return fmt.Errorf("must be a file path")
	}
	return nil
}
