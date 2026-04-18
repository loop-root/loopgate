package controlapi

import "loopgate/internal/troubleshoot"

type AuditExportFlushResponse struct {
	FlushRequestID       string `json:"flush_request_id"`
	Status               string `json:"status"`
	DestinationKind      string `json:"destination_kind,omitempty"`
	DestinationLabel     string `json:"destination_label,omitempty"`
	EventCount           int    `json:"event_count"`
	ApproxBytes          int    `json:"approx_bytes,omitempty"`
	FromAuditSequence    uint64 `json:"from_audit_sequence,omitempty"`
	ThroughAuditSequence uint64 `json:"through_audit_sequence,omitempty"`
	ThroughEventHash     string `json:"through_event_hash,omitempty"`
}

type AuditExportTrustCheckResponse struct {
	Status              string                              `json:"status"`
	ActionNeeded        bool                                `json:"action_needed"`
	Summary             string                              `json:"summary"`
	RecommendedAction   string                              `json:"recommended_action,omitempty"`
	DestinationKind     string                              `json:"destination_kind,omitempty"`
	DestinationLabel    string                              `json:"destination_label,omitempty"`
	EndpointScheme      string                              `json:"endpoint_scheme,omitempty"`
	EndpointHost        string                              `json:"endpoint_host,omitempty"`
	LastErrorClass      string                              `json:"last_error_class,omitempty"`
	ConsecutiveFailures int                                 `json:"consecutive_failures,omitempty"`
	Trust               troubleshoot.AuditExportTrustReport `json:"trust"`
}
