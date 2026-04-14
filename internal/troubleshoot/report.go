// Package troubleshoot builds operator-facing diagnostic reports and on-disk bundles.
// Output is derived from local state; it is not an authority surface and must not embed secrets.
package troubleshoot

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
)

const maxTopEventTypes = 25

// Report is JSON-serializable metadata for operators and in-app doctor UIs.
type Report struct {
	GeneratedAtRFC3339 string `json:"generated_at"`
	RepoRoot           string `json:"repo_root"`
	RuntimeVersion     string `json:"runtime_config_version"`
	LedgerVerify       struct {
		OK                  bool   `json:"ok"`
		Error               string `json:"error,omitempty"`
		LastAuditSequence   int64  `json:"last_audit_sequence,omitempty"`
		LastEventHashPrefix string `json:"last_event_hash_prefix,omitempty"`
	} `json:"ledger_verify"`
	LedgerActive struct {
		ActiveFile   string      `json:"active_file"`
		LineCount    int         `json:"line_count"`
		TopTypes     []TypeCount `json:"top_event_types"`
		SummaryError string      `json:"summary_error,omitempty"`
	} `json:"ledger_active"`
	Diagnostics struct {
		Enabled              bool   `json:"diagnostic_logging_enabled"`
		DirectoryRelative    string `json:"diagnostic_directory_relative,omitempty"`
		DiagnosticDefaultLog string `json:"diagnostic_default_level,omitempty"`
	} `json:"diagnostics"`
	AuditExport AuditExportReport `json:"audit_export"`
}

type AuditExportReport struct {
	Enabled                           bool                   `json:"enabled"`
	DestinationKind                   string                 `json:"destination_kind,omitempty"`
	DestinationLabel                  string                 `json:"destination_label,omitempty"`
	EndpointScheme                    string                 `json:"endpoint_scheme,omitempty"`
	EndpointHost                      string                 `json:"endpoint_host,omitempty"`
	AuthorizationConfigured           bool                   `json:"authorization_configured,omitempty"`
	TLSEnabled                        bool                   `json:"tls_enabled,omitempty"`
	PinnedServerPublicKeyConfigured   bool                   `json:"pinned_server_public_key_configured,omitempty"`
	PinnedServerPublicKeySHA256Prefix string                 `json:"pinned_server_public_key_sha256_prefix,omitempty"`
	MinimumRemainingValiditySeconds   int                    `json:"minimum_remaining_validity_seconds,omitempty"`
	LastAttemptAtUTC                  string                 `json:"last_attempt_at_utc,omitempty"`
	LastSuccessAtUTC                  string                 `json:"last_success_at_utc,omitempty"`
	LastExportedAuditSequence         uint64                 `json:"last_exported_audit_sequence,omitempty"`
	ConsecutiveFailures               int                    `json:"consecutive_failures,omitempty"`
	LastErrorClass                    string                 `json:"last_error_class,omitempty"`
	Trust                             AuditExportTrustReport `json:"trust"`
}

type AuditExportTrustReport struct {
	OverallStatus     string                       `json:"overall_status,omitempty"`
	Warnings          []string                     `json:"warnings,omitempty"`
	RootCA            AuditExportCertificateStatus `json:"root_ca"`
	ClientCertificate AuditExportCertificateStatus `json:"client_certificate"`
}

type AuditExportCertificateStatus struct {
	Configured                   bool   `json:"configured"`
	Status                       string `json:"status,omitempty"`
	Subject                      string `json:"subject,omitempty"`
	NotBeforeUTC                 string `json:"not_before_utc,omitempty"`
	NotAfterUTC                  string `json:"not_after_utc,omitempty"`
	RemainingValiditySeconds     int64  `json:"remaining_validity_seconds,omitempty"`
	RenewalThresholdAtUTC        string `json:"renewal_threshold_at_utc,omitempty"`
	SecondsUntilRenewalThreshold int64  `json:"seconds_until_renewal_threshold,omitempty"`
	DaysUntilRenewalThreshold    int64  `json:"days_until_renewal_threshold,omitempty"`
	RenewalWindowActive          bool   `json:"renewal_window_active,omitempty"`
}

// TypeCount is an event type histogram bucket.
type TypeCount struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

// LoadEffectiveRuntimeConfig mirrors Loopgate startup: config/runtime.yaml plus optional diagnostic override.
func LoadEffectiveRuntimeConfig(repoRoot string) (config.RuntimeConfig, error) {
	return config.LoadRuntimeConfig(repoRoot)
}

// AuditRotationSettings matches Loopgate segmented ledger configuration.
func AuditRotationSettings(repoRoot string, rc config.RuntimeConfig) ledger.RotationSettings {
	verifyClosed := true
	if rc.Logging.AuditLedger.VerifyClosedSegmentsOnStartup != nil {
		verifyClosed = *rc.Logging.AuditLedger.VerifyClosedSegmentsOnStartup
	}
	return ledger.RotationSettings{
		MaxEventBytes:                 rc.Logging.AuditLedger.MaxEventBytes,
		RotateAtBytes:                 rc.Logging.AuditLedger.RotateAtBytes,
		SegmentDir:                    filepath.Join(repoRoot, rc.Logging.AuditLedger.SegmentDir),
		ManifestPath:                  filepath.Join(repoRoot, rc.Logging.AuditLedger.ManifestPath),
		VerifyClosedSegmentsOnStartup: verifyClosed,
	}
}

// ActiveAuditPath returns the active Loopgate audit JSONL path.
func ActiveAuditPath(repoRoot string) string {
	return filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl")
}

func hashPrefix(full string) string {
	if len(full) <= 16 {
		return full
	}
	return full[:16]
}

// BuildReport aggregates ledger verification and active-file histograms.
func BuildReport(repoRoot string, rc config.RuntimeConfig) (Report, error) {
	var rep Report
	rep.GeneratedAtRFC3339 = time.Now().UTC().Format(time.RFC3339Nano)
	rep.RepoRoot = repoRoot
	rep.RuntimeVersion = strings.TrimSpace(rc.Version)
	rep.Diagnostics.Enabled = rc.Logging.Diagnostic.Enabled
	rep.Diagnostics.DirectoryRelative = strings.TrimSpace(rc.Logging.Diagnostic.Directory)
	if rep.Diagnostics.DirectoryRelative == "" {
		rep.Diagnostics.DirectoryRelative = rc.Logging.Diagnostic.ResolvedDirectory()
	}
	rep.Diagnostics.DiagnosticDefaultLog = strings.TrimSpace(rc.Logging.Diagnostic.DefaultLevel)
	rep.AuditExport.Enabled = rc.Logging.AuditExport.Enabled
	rep.AuditExport.DestinationKind = strings.TrimSpace(rc.Logging.AuditExport.DestinationKind)
	rep.AuditExport.DestinationLabel = strings.TrimSpace(rc.Logging.AuditExport.DestinationLabel)
	rep.AuditExport.AuthorizationConfigured = rc.Logging.AuditExport.Authorization.SecretRef != nil
	rep.AuditExport.TLSEnabled = rc.Logging.AuditExport.TLS.Enabled
	rep.AuditExport.MinimumRemainingValiditySeconds = rc.Logging.AuditExport.TLS.MinimumRemainingValiditySeconds
	trimmedPinnedServerPublicKeySHA256 := strings.TrimSpace(rc.Logging.AuditExport.TLS.PinnedServerPublicKeySHA256)
	rep.AuditExport.PinnedServerPublicKeyConfigured = trimmedPinnedServerPublicKeySHA256 != ""
	if trimmedPinnedServerPublicKeySHA256 != "" {
		rep.AuditExport.PinnedServerPublicKeySHA256Prefix = hashPrefix(trimmedPinnedServerPublicKeySHA256)
	}
	if parsedEndpointURL, err := url.Parse(strings.TrimSpace(rc.Logging.AuditExport.EndpointURL)); err == nil {
		rep.AuditExport.EndpointScheme = strings.TrimSpace(parsedEndpointURL.Scheme)
		rep.AuditExport.EndpointHost = strings.TrimSpace(parsedEndpointURL.Hostname())
	}
	if !rep.AuditExport.Enabled {
		rep.AuditExport.Trust.OverallStatus = "disabled"
	} else if !rep.AuditExport.TLSEnabled {
		rep.AuditExport.Trust.OverallStatus = "tls_disabled"
	} else {
		rep.AuditExport.Trust.OverallStatus = "unknown"
	}

	activePath := ActiveAuditPath(repoRoot)
	rotation := AuditRotationSettings(repoRoot, rc)
	lastSeq, lastHash, err := ledger.ReadSegmentedChainState(activePath, "audit_sequence", rotation)
	if err != nil {
		rep.LedgerVerify.OK = false
		rep.LedgerVerify.Error = err.Error()
	} else {
		rep.LedgerVerify.OK = true
		rep.LedgerVerify.LastAuditSequence = lastSeq
		rep.LedgerVerify.LastEventHashPrefix = hashPrefix(lastHash)
	}

	lineCount, topTypes, sumErr := summarizeActiveLedger(activePath)
	if sumErr != nil {
		rep.LedgerActive.SummaryError = sumErr.Error()
	}
	rep.LedgerActive.ActiveFile = activePath
	rep.LedgerActive.LineCount = lineCount
	rep.LedgerActive.TopTypes = topTypes

	return rep, nil
}

func summarizeActiveLedger(activePath string) (lineCount int, top []TypeCount, err error) {
	f, err := os.Open(activePath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil, nil
		}
		return 0, nil, err
	}
	defer f.Close()

	counts := make(map[string]int)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		lineCount++
		ev, ok := ledger.ParseEvent(scanner.Bytes())
		if !ok {
			return lineCount, nil, fmt.Errorf("malformed ledger line %d", lineCount)
		}
		counts[ev.Type]++
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return lineCount, nil, scanErr
	}

	type pair struct {
		t string
		n int
	}
	var list []pair
	for t, n := range counts {
		list = append(list, pair{t: t, n: n})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].n != list[j].n {
			return list[i].n > list[j].n
		}
		return list[i].t < list[j].t
	})
	for i := range list {
		if i >= maxTopEventTypes {
			break
		}
		top = append(top, TypeCount{Type: list[i].t, Count: list[i].n})
	}
	return lineCount, top, nil
}

// WriteReportJSON writes report.json into dir (creates dir with 0700).
func WriteReportJSON(dir string, rep Report) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir report dir: %w", err)
	}
	path := filepath.Join(dir, "report.json")
	raw, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}
