// Package troubleshoot builds operator-facing diagnostic reports and on-disk bundles.
// Output is derived from local state; it is not an authority surface and must not embed secrets.
package troubleshoot

import (
	"bufio"
	"bytes"
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

const (
	maxTopEventTypes = 25
	// These diagnostics mirror Loopgate's current replay window and in-memory nonce cap.
	// Keep them aligned with internal/loopgate defaults so doctor/report stays honest.
	nonceReplayRetentionWindow          = 1 * time.Hour
	defaultNonceReplayCapacity          = 65536
	nonceReplayUtilizationWarningPct    = 80
	nonceReplayLogGrowthWarningMinLines = 1024
	nonceReplayLogGrowthWarningFactor   = 4
)

// Report is JSON-serializable metadata for operators and in-app doctor UIs.
type Report struct {
	GeneratedAtRFC3339 string `json:"generated_at"`
	RepoRoot           string `json:"repo_root"`
	RuntimeVersion     string `json:"runtime_config_version"`
	LedgerVerify       struct {
		OK                  bool                        `json:"ok"`
		Error               string                      `json:"error,omitempty"`
		LastAuditSequence   int64                       `json:"last_audit_sequence,omitempty"`
		LastEventHashPrefix string                      `json:"last_event_hash_prefix,omitempty"`
		HMACCheckpoints     AuditLedgerCheckpointReport `json:"hmac_checkpoints"`
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
	NonceReplay NonceReplayReport `json:"nonce_replay"`
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

type NonceReplayReport struct {
	StoreKind          string   `json:"store_kind"`
	Status             string   `json:"status"`
	ActiveEntries      int      `json:"active_entries"`
	Capacity           int      `json:"capacity"`
	UtilizationPercent int      `json:"utilization_percent"`
	PersistedLineCount int      `json:"persisted_line_count"`
	PersistedFileBytes int64    `json:"persisted_file_bytes"`
	OldestSeenAtUTC    string   `json:"oldest_seen_at_utc,omitempty"`
	NewestSeenAtUTC    string   `json:"newest_seen_at_utc,omitempty"`
	Warnings           []string `json:"warnings,omitempty"`
	SummaryError       string   `json:"summary_error,omitempty"`
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
	nowUTC := time.Now().UTC()
	rep.GeneratedAtRFC3339 = nowUTC.Format(time.RFC3339Nano)
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
	checkpointReport, checkpointErr := VerifyAuditLedgerCheckpoints(repoRoot, rc)
	rep.LedgerVerify.HMACCheckpoints = checkpointReport
	if checkpointErr != nil && rep.LedgerVerify.Error == "" {
		rep.LedgerVerify.OK = false
		rep.LedgerVerify.Error = checkpointErr.Error()
	}

	lineCount, topTypes, sumErr := summarizeActiveLedger(activePath)
	if sumErr != nil {
		rep.LedgerActive.SummaryError = sumErr.Error()
	}
	rep.LedgerActive.ActiveFile = activePath
	rep.LedgerActive.LineCount = lineCount
	rep.LedgerActive.TopTypes = topTypes
	rep.NonceReplay = buildNonceReplayReport(
		repoRoot,
		nowUTC,
		defaultNonceReplayCapacity,
		nonceReplayUtilizationWarningPct,
		nonceReplayLogGrowthWarningMinLines,
		nonceReplayLogGrowthWarningFactor,
	)

	return rep, nil
}

type nonceReplaySnapshotFile struct {
	Nonces map[string]nonceReplayPersistedNonce `json:"nonces"`
}

type nonceReplayPersistedNonce struct {
	ControlSessionID string `json:"control_session_id"`
	SeenAt           string `json:"seen_at"`
}

type nonceReplayLogRecord struct {
	NonceKey         string `json:"nonce_key"`
	ControlSessionID string `json:"control_session_id"`
	SeenAt           string `json:"seen_at"`
}

func buildNonceReplayReport(repoRoot string, nowUTC time.Time, capacity int, utilizationWarningPercent int, logGrowthWarningMinLines int, logGrowthWarningFactor int) NonceReplayReport {
	report := NonceReplayReport{
		StoreKind: "append_only_log",
		Status:    "ok",
		Capacity:  capacity,
	}

	logPath := filepath.Join(repoRoot, "runtime", "state", "nonce_replay.jsonl")
	legacySnapshotPath := filepath.Join(repoRoot, "runtime", "state", "nonce_replay.json")

	if logFileInfo, err := os.Stat(logPath); err == nil {
		report.PersistedFileBytes = logFileInfo.Size()
		lineCount, countErr := countNonEmptyLines(logPath)
		if countErr != nil {
			report.Status = "error"
			report.SummaryError = fmt.Sprintf("count nonce replay log lines: %v", countErr)
			return report
		}
		report.PersistedLineCount = lineCount
	} else if !os.IsNotExist(err) {
		report.Status = "error"
		report.SummaryError = fmt.Sprintf("stat nonce replay log: %v", err)
		return report
	}

	activeNonces, storeKind, loadErr := loadNonceReplayForReport(logPath, legacySnapshotPath, nowUTC)
	report.StoreKind = storeKind
	if loadErr != nil {
		report.Status = "error"
		report.SummaryError = loadErr.Error()
		return report
	}

	report.ActiveEntries = len(activeNonces)
	if capacity > 0 {
		report.UtilizationPercent = (report.ActiveEntries * 100) / capacity
	}
	oldestSeenAt, newestSeenAt := nonceReplayAgeBounds(activeNonces)
	if !oldestSeenAt.IsZero() {
		report.OldestSeenAtUTC = oldestSeenAt.UTC().Format(time.RFC3339Nano)
	}
	if !newestSeenAt.IsZero() {
		report.NewestSeenAtUTC = newestSeenAt.UTC().Format(time.RFC3339Nano)
	}

	if storeKind == "legacy_snapshot" {
		report.Status = "warning"
		report.Warnings = append(report.Warnings, "legacy_snapshot_fallback_active")
	}
	if capacity > 0 && report.ActiveEntries*100 >= capacity*utilizationWarningPercent {
		report.Status = "warning"
		report.Warnings = append(report.Warnings, "active_entries_high_utilization")
	}
	if report.PersistedLineCount >= logGrowthWarningMinLines && report.PersistedLineCount > logGrowthWarningFactor*maxInt(1, report.ActiveEntries) {
		report.Status = "warning"
		report.Warnings = append(report.Warnings, "append_only_log_growth_visible")
	}

	return report
}

func loadNonceReplayForReport(logPath string, legacySnapshotPath string, nowUTC time.Time) (map[string]time.Time, string, error) {
	if _, err := os.Stat(logPath); err == nil {
		activeNonces, err := loadAppendOnlyNonceReplayLogForReport(logPath, nowUTC)
		if err != nil {
			return nil, "append_only_log", fmt.Errorf("load nonce replay log: %w", err)
		}
		return activeNonces, "append_only_log", nil
	} else if !os.IsNotExist(err) {
		return nil, "append_only_log", fmt.Errorf("stat nonce replay log: %w", err)
	}
	if _, err := os.Stat(legacySnapshotPath); err == nil {
		activeNonces, err := loadNonceReplaySnapshotForReport(legacySnapshotPath, nowUTC)
		if err != nil {
			return nil, "legacy_snapshot", fmt.Errorf("load legacy nonce replay snapshot: %w", err)
		}
		return activeNonces, "legacy_snapshot", nil
	} else if !os.IsNotExist(err) {
		return nil, "append_only_log", fmt.Errorf("stat legacy nonce replay snapshot: %w", err)
	}
	return map[string]time.Time{}, "append_only_log", nil
}

func loadNonceReplaySnapshotForReport(path string, nowUTC time.Time) (map[string]time.Time, error) {
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(rawBytes) == 0 {
		return map[string]time.Time{}, nil
	}

	var snapshot nonceReplaySnapshotFile
	if err := json.Unmarshal(rawBytes, &snapshot); err != nil {
		return nil, err
	}

	activeNonces := make(map[string]time.Time, len(snapshot.Nonces))
	for nonceKey, persistedNonce := range snapshot.Nonces {
		seenAt, parseErr := time.Parse(time.RFC3339Nano, persistedNonce.SeenAt)
		if parseErr != nil {
			continue
		}
		if nowUTC.Sub(seenAt.UTC()) > nonceReplayRetentionWindow {
			continue
		}
		activeNonces[nonceKey] = seenAt.UTC()
	}
	return activeNonces, nil
}

func loadAppendOnlyNonceReplayLogForReport(path string, nowUTC time.Time) (map[string]time.Time, error) {
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(rawBytes) == 0 {
		return map[string]time.Time{}, nil
	}

	lines := bytes.Split(rawBytes, []byte{'\n'})
	hasTrailingNewline := len(rawBytes) > 0 && rawBytes[len(rawBytes)-1] == '\n'
	activeNonces := make(map[string]time.Time)
	for lineIndex, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		isLastLine := lineIndex == len(lines)-1
		var record nonceReplayLogRecord
		if err := json.Unmarshal(line, &record); err != nil {
			if isLastLine && !hasTrailingNewline {
				continue
			}
			return nil, fmt.Errorf("decode line %d: %w", lineIndex+1, err)
		}
		seenAt, err := time.Parse(time.RFC3339Nano, record.SeenAt)
		if err != nil {
			if isLastLine && !hasTrailingNewline {
				continue
			}
			return nil, fmt.Errorf("parse timestamp on line %d: %w", lineIndex+1, err)
		}
		seenAt = seenAt.UTC()
		if nowUTC.Sub(seenAt) > nonceReplayRetentionWindow {
			continue
		}
		activeNonces[record.NonceKey] = seenAt
	}
	return activeNonces, nil
}

func nonceReplayAgeBounds(activeNonces map[string]time.Time) (time.Time, time.Time) {
	var oldestSeenAt time.Time
	var newestSeenAt time.Time
	for _, seenAt := range activeNonces {
		if oldestSeenAt.IsZero() || seenAt.Before(oldestSeenAt) {
			oldestSeenAt = seenAt
		}
		if newestSeenAt.IsZero() || seenAt.After(newestSeenAt) {
			newestSeenAt = seenAt
		}
	}
	return oldestSeenAt, newestSeenAt
}

func countNonEmptyLines(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	lineCount := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		if len(strings.TrimSpace(scanner.Text())) == 0 {
			continue
		}
		lineCount++
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return lineCount, nil
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
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
