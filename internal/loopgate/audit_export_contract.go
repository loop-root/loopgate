package loopgate

import (
	"fmt"
	"os"
	"strings"
	"time"

	"morph/internal/ledger"
)

const adminNodeAuditIngestSchemaVersion = "1"

type adminNodeAuditIngestRequest struct {
	SchemaVersion  string                     `json:"schema_version"`
	GeneratedAtUTC string                     `json:"generated_at_utc"`
	Source         adminNodeAuditIngestSource `json:"source"`
	Batch          adminNodeAuditIngestBatch  `json:"batch"`
}

type adminNodeAuditIngestSource struct {
	LoopgateVersion    string `json:"loopgate_version"`
	TransportProfile   string `json:"transport_profile"`
	SourceHostname     string `json:"source_hostname"`
	DeploymentTenantID string `json:"deployment_tenant_id,omitempty"`
	DeploymentUserID   string `json:"deployment_user_id,omitempty"`
	DestinationKind    string `json:"destination_kind"`
	DestinationLabel   string `json:"destination_label"`
}

type adminNodeAuditIngestBatch struct {
	FromAuditSequence    uint64         `json:"from_audit_sequence"`
	ThroughAuditSequence uint64         `json:"through_audit_sequence"`
	ThroughEventHash     string         `json:"through_event_hash"`
	EventCount           int            `json:"event_count"`
	ApproxBytes          int            `json:"approx_bytes"`
	Events               []ledger.Event `json:"events"`
}

type adminNodeAuditIngestResponse struct {
	SchemaVersion        string `json:"schema_version"`
	Status               string `json:"status"`
	ThroughAuditSequence uint64 `json:"through_audit_sequence"`
	ThroughEventHash     string `json:"through_event_hash"`
}

func (server *Server) buildAdminNodeAuditIngestRequest(exportBatch auditExportBatch) (adminNodeAuditIngestRequest, error) {
	if exportBatch.EventCount <= 0 || len(exportBatch.Events) == 0 {
		return adminNodeAuditIngestRequest{}, fmt.Errorf("admin-node audit ingest requires a non-empty export batch")
	}
	if exportBatch.EventCount != len(exportBatch.Events) {
		return adminNodeAuditIngestRequest{}, fmt.Errorf("admin-node audit ingest event_count mismatch")
	}
	if exportBatch.FromAuditSequence == 0 || exportBatch.ThroughAuditSequence == 0 {
		return adminNodeAuditIngestRequest{}, fmt.Errorf("admin-node audit ingest requires a non-zero audit sequence range")
	}
	if strings.TrimSpace(exportBatch.ThroughEventHash) == "" {
		return adminNodeAuditIngestRequest{}, fmt.Errorf("admin-node audit ingest requires through_event_hash")
	}

	sourceHostname, hostnameErr := os.Hostname()
	if hostnameErr != nil || strings.TrimSpace(sourceHostname) == "" {
		sourceHostname = "unknown-host"
	}

	return adminNodeAuditIngestRequest{
		SchemaVersion:  adminNodeAuditIngestSchemaVersion,
		GeneratedAtUTC: server.now().UTC().Format(time.RFC3339Nano),
		Source: adminNodeAuditIngestSource{
			LoopgateVersion:    statusVersion,
			TransportProfile:   "local_http_over_uds",
			SourceHostname:     strings.TrimSpace(sourceHostname),
			DeploymentTenantID: strings.TrimSpace(server.runtimeConfig.Tenancy.DeploymentTenantID),
			DeploymentUserID:   strings.TrimSpace(server.runtimeConfig.Tenancy.DeploymentUserID),
			DestinationKind:    strings.TrimSpace(exportBatch.DestinationKind),
			DestinationLabel:   strings.TrimSpace(exportBatch.DestinationLabel),
		},
		Batch: adminNodeAuditIngestBatch{
			FromAuditSequence:    exportBatch.FromAuditSequence,
			ThroughAuditSequence: exportBatch.ThroughAuditSequence,
			ThroughEventHash:     strings.TrimSpace(exportBatch.ThroughEventHash),
			EventCount:           exportBatch.EventCount,
			ApproxBytes:          exportBatch.ApproxBytes,
			Events:               append([]ledger.Event(nil), exportBatch.Events...),
		},
	}, nil
}
