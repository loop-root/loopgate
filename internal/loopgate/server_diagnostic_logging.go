package loopgate

import (
	"strconv"
	"strings"

	"loopgate/internal/ledger"
)

func (server *Server) diagnosticServerControlPlaneFromAuditEvent(ev ledger.Event) {
	if server.diagnostic == nil || server.diagnostic.Server == nil {
		return
	}
	data := ev.Data
	switch ev.Type {
	case "session.opened":
		server.diagnostic.Server.Debug("control_plane", diagAppendTenantAttrs([]any{
			"event", ev.Type,
			"session", ev.Session,
			"actor_label", diagDataString(data, "actor_label"),
			"client_session_label", diagDataString(data, "client_session_label"),
		}, data)...)
	case "capability.requested":
		server.diagnostic.Server.Debug("control_plane", diagAppendTenantAttrs([]any{
			"event", ev.Type,
			"session", ev.Session,
			"request_id", diagDataString(data, "request_id"),
			"capability", diagDataString(data, "capability"),
			"policy_decision", diagDataString(data, "decision"),
		}, data)...)
	case "capability.denied":
		server.diagnostic.Server.Debug("control_plane", diagAppendTenantAttrs([]any{
			"event", ev.Type,
			"session", ev.Session,
			"request_id", diagDataString(data, "request_id"),
			"capability", diagDataString(data, "capability"),
			"denial_code", diagDataString(data, "denial_code"),
		}, data)...)
	case "capability.executed":
		server.diagnostic.Server.Debug("control_plane", diagAppendTenantAttrs([]any{
			"event", ev.Type,
			"session", ev.Session,
			"request_id", diagDataString(data, "request_id"),
			"capability", diagDataString(data, "capability"),
			"response_status", diagDataString(data, "status"),
		}, data)...)
	case "capability.error":
		server.diagnostic.Server.Warn("control_plane", diagAppendTenantAttrs([]any{
			"event", ev.Type,
			"session", ev.Session,
			"request_id", diagDataString(data, "request_id"),
			"capability", diagDataString(data, "capability"),
			"operator_error_class", diagOperatorErrorClass(data),
		}, data)...)
	case "approval.created":
		server.diagnostic.Server.Debug("control_plane", diagAppendTenantAttrs([]any{
			"event", ev.Type,
			"session", ev.Session,
			"approval_request_id", diagDataString(data, "approval_request_id"),
			"capability", diagDataString(data, "capability"),
			"approval_class", diagDataString(data, "approval_class"),
		}, data)...)
	case "approval.granted":
		server.diagnostic.Server.Debug("control_plane", diagAppendTenantAttrs([]any{
			"event", ev.Type,
			"session", ev.Session,
			"approval_request_id", diagDataString(data, "approval_request_id"),
			"capability", diagDataString(data, "capability"),
		}, data)...)
	case "approval.denied":
		server.diagnostic.Server.Debug("control_plane", diagAppendTenantAttrs([]any{
			"event", ev.Type,
			"session", ev.Session,
			"approval_request_id", diagDataString(data, "approval_request_id"),
			"capability", diagDataString(data, "capability"),
			"denial_code", diagDataString(data, "denial_code"),
		}, data)...)
	case "audit_export.requested", "audit_export.completed", "audit_export.noop":
		server.diagnostic.Server.Debug("control_plane", diagAppendTenantAttrs([]any{
			"event", ev.Type,
			"session", ev.Session,
			"request_id", diagDataString(data, "request_id"),
			"destination_kind", diagDataString(data, "destination_kind"),
			"through_audit_sequence", diagDataString(data, "through_audit_sequence"),
			"status", diagDataString(data, "status"),
		}, data)...)
	case "audit_export.failed":
		server.diagnostic.Server.Warn("control_plane", diagAppendTenantAttrs([]any{
			"event", ev.Type,
			"session", ev.Session,
			"request_id", diagDataString(data, "request_id"),
			"destination_kind", diagDataString(data, "destination_kind"),
			"operator_error_class", diagOperatorErrorClass(data),
		}, data)...)
	default:
		return
	}
}

// diagAppendTenantAttrs appends tenant_id/user_id from the audit payload so operators can
// filter diagnostic text logs the same way they filter structured audit exports.
func diagAppendTenantAttrs(attrs []any, data map[string]interface{}) []any {
	return append(attrs,
		"tenant_id", diagDataString(data, "tenant_id"),
		"user_id", diagDataString(data, "user_id"),
	)
}

func diagOperatorErrorClass(data map[string]interface{}) string {
	s := strings.TrimSpace(diagDataString(data, "operator_error_class"))
	if s == "" {
		return "legacy_unclassified"
	}
	return s
}

func diagDataString(data map[string]interface{}, key string) string {
	if data == nil {
		return ""
	}
	raw, ok := data[key]
	if !ok || raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case bool:
		return strconv.FormatBool(v)
	default:
		// Do not fmt.Sprint arbitrary audit payload types into diagnostic logs.
		return ""
	}
}
