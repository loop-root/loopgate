package loopgate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"loopgate/internal/config"
)

const auditExportStateSchemaVersion = "1"

type auditExportStateFile struct {
	SchemaVersion             string `json:"schema_version"`
	DestinationKind           string `json:"destination_kind"`
	DestinationLabel          string `json:"destination_label"`
	LastAttemptAtUTC          string `json:"last_attempt_at_utc,omitempty"`
	LastSuccessAtUTC          string `json:"last_success_at_utc,omitempty"`
	LastExportedAtUTC         string `json:"last_exported_at_utc,omitempty"`
	LastExportedAuditSequence uint64 `json:"last_exported_audit_sequence,omitempty"`
	LastExportedEventHash     string `json:"last_exported_event_hash,omitempty"`
	ConsecutiveFailures       int    `json:"consecutive_failures,omitempty"`
	LastErrorClass            string `json:"last_error_class,omitempty"`
}

func newAuditExportStateForRuntimeConfig(runtimeConfig config.RuntimeConfig) auditExportStateFile {
	return auditExportStateFile{
		SchemaVersion:    auditExportStateSchemaVersion,
		DestinationKind:  strings.TrimSpace(runtimeConfig.Logging.AuditExport.DestinationKind),
		DestinationLabel: strings.TrimSpace(runtimeConfig.Logging.AuditExport.DestinationLabel),
	}
}

func (server *Server) loadOrInitAuditExportState() error {
	if !server.runtimeConfig.Logging.AuditExport.Enabled {
		return nil
	}

	server.auditExportMu.Lock()
	defer server.auditExportMu.Unlock()

	stateFile, err := server.loadAuditExportStateLocked()
	if err == nil {
		if strings.TrimSpace(stateFile.DestinationKind) == strings.TrimSpace(server.runtimeConfig.Logging.AuditExport.DestinationKind) &&
			strings.TrimSpace(stateFile.DestinationLabel) == strings.TrimSpace(server.runtimeConfig.Logging.AuditExport.DestinationLabel) {
			return nil
		}
		resetStateFile := newAuditExportStateForRuntimeConfig(server.runtimeConfig)
		return server.saveAuditExportStateLocked(resetStateFile)
	}
	if !os.IsNotExist(err) {
		corruptPath := server.auditExportStatePath + ".corrupt." + server.now().UTC().Format("20060102-150405")
		_ = os.Rename(server.auditExportStatePath, corruptPath)
	}
	return server.saveAuditExportStateLocked(newAuditExportStateForRuntimeConfig(server.runtimeConfig))
}

func (server *Server) loadAuditExportStateLocked() (auditExportStateFile, error) {
	rawStateBytes, err := os.ReadFile(server.auditExportStatePath)
	if err != nil {
		return auditExportStateFile{}, err
	}
	var stateFile auditExportStateFile
	decoder := json.NewDecoder(bytes.NewReader(rawStateBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stateFile); err != nil {
		return auditExportStateFile{}, fmt.Errorf("decode audit export state: %w", err)
	}
	if schemaVersion := strings.TrimSpace(stateFile.SchemaVersion); schemaVersion != "" && schemaVersion != auditExportStateSchemaVersion {
		return auditExportStateFile{}, fmt.Errorf("unsupported audit export state schema version %q", schemaVersion)
	}
	if strings.TrimSpace(stateFile.SchemaVersion) == "" {
		stateFile.SchemaVersion = auditExportStateSchemaVersion
	}
	return stateFile, nil
}

func (server *Server) saveAuditExportStateLocked(stateFile auditExportStateFile) error {
	if strings.TrimSpace(stateFile.SchemaVersion) == "" {
		stateFile.SchemaVersion = auditExportStateSchemaVersion
	}
	stateBytes, err := json.MarshalIndent(stateFile, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal audit export state: %w", err)
	}
	if err := atomicWritePrivateJSON(server.auditExportStatePath, stateBytes); err != nil {
		return fmt.Errorf("save audit export state: %w", err)
	}
	return nil
}

func (server *Server) markAuditExportAttempt(destinationErrorClass string) error {
	if !server.runtimeConfig.Logging.AuditExport.Enabled {
		return nil
	}
	server.auditExportMu.Lock()
	defer server.auditExportMu.Unlock()

	stateFile, err := server.loadAuditExportStateLocked()
	if err != nil {
		return err
	}
	stateFile.LastAttemptAtUTC = server.now().UTC().Format(time.RFC3339Nano)
	stateFile.LastErrorClass = strings.TrimSpace(destinationErrorClass)
	if stateFile.LastErrorClass != "" {
		stateFile.ConsecutiveFailures++
	} else {
		stateFile.ConsecutiveFailures = 0
	}
	return server.saveAuditExportStateLocked(stateFile)
}

func (server *Server) markAuditExportSuccess(exportedSequence uint64, exportedEventHash string) error {
	if !server.runtimeConfig.Logging.AuditExport.Enabled {
		return nil
	}
	server.auditExportMu.Lock()
	defer server.auditExportMu.Unlock()

	stateFile, err := server.loadAuditExportStateLocked()
	if err != nil {
		return err
	}
	nowUTC := server.now().UTC().Format(time.RFC3339Nano)
	stateFile.LastAttemptAtUTC = nowUTC
	stateFile.LastSuccessAtUTC = nowUTC
	stateFile.LastExportedAtUTC = nowUTC
	stateFile.LastExportedAuditSequence = exportedSequence
	stateFile.LastExportedEventHash = strings.TrimSpace(exportedEventHash)
	stateFile.ConsecutiveFailures = 0
	stateFile.LastErrorClass = ""
	return server.saveAuditExportStateLocked(stateFile)
}
