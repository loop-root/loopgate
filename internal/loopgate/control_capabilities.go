package loopgate

import (
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"sort"
)

const (
	controlCapabilityApprovalRead            = "approval.read"
	controlCapabilityApprovalWrite           = "approval.write"
	controlCapabilityAuditExport             = "audit.export"
	controlCapabilityConfigRead              = "config.read"
	controlCapabilityConfigWrite             = "config.write"
	controlCapabilityConnectionRead          = "connection.read"
	controlCapabilityConnectionWrite         = "connection.write"
	controlCapabilityDiagnosticRead          = "diagnostic.read"
	controlCapabilityFolderAccessRead        = "folder_access.read"
	controlCapabilityFolderAccessWrite       = "folder_access.write"
	controlCapabilityMCPGatewayWrite         = "mcp_gateway.write"
	controlCapabilityOperatorMountWriteGrant = "operator_mount.write_grant"
	controlCapabilityQuarantineRead          = "quarantine.read"
	controlCapabilityQuarantineWrite         = "quarantine.write"
	controlCapabilitySiteInspect             = "site.inspect"
	controlCapabilitySiteTrustWrite          = "site.trust.write"
	controlCapabilityUIRead                  = "ui.read"
	controlCapabilityUIWrite                 = "ui.write"
)

var internalControlCapabilityCatalog = map[string]controlapipkg.CapabilitySummary{
	controlCapabilityApprovalRead: {
		Name:        controlCapabilityApprovalRead,
		Category:    "approval",
		Operation:   "read",
		Description: "Read pending approval inventory through the local control plane.",
	},
	controlCapabilityApprovalWrite: {
		Name:        controlCapabilityApprovalWrite,
		Category:    "approval",
		Operation:   "write",
		Description: "Approve or deny pending approval requests through the local control plane.",
	},
	controlCapabilityAuditExport: {
		Name:        controlCapabilityAuditExport,
		Category:    "audit",
		Operation:   "write",
		Description: "Trigger one local-first audit export flush to the configured downstream destination through the local control plane.",
	},
	controlCapabilityConfigRead: {
		Name:        controlCapabilityConfigRead,
		Category:    "control",
		Operation:   "read",
		Description: "Read Loopgate configuration state through the local control plane.",
	},
	controlCapabilityConfigWrite: {
		Name:        controlCapabilityConfigWrite,
		Category:    "control",
		Operation:   "write",
		Description: "Write Loopgate configuration state through the local control plane.",
	},
	controlCapabilityConnectionRead: {
		Name:        controlCapabilityConnectionRead,
		Category:    "connection",
		Operation:   "read",
		Description: "Read connection summaries and provider status through the local control plane.",
	},
	controlCapabilityConnectionWrite: {
		Name:        controlCapabilityConnectionWrite,
		Category:    "connection",
		Operation:   "write",
		Description: "Validate or update provider connection state, including OAuth PKCE helper flows, through the local control plane.",
	},
	controlCapabilityDiagnosticRead: {
		Name:        controlCapabilityDiagnosticRead,
		Category:    "diagnostic",
		Operation:   "read",
		Description: "Read aggregated operator diagnostic projections through the local control plane.",
	},
	controlCapabilityFolderAccessRead: {
		Name:        controlCapabilityFolderAccessRead,
		Category:    "filesystem",
		Operation:   "read",
		Description: "Read folder-access and shared-folder status projections through the local control plane.",
	},
	controlCapabilityFolderAccessWrite: {
		Name:        controlCapabilityFolderAccessWrite,
		Category:    "filesystem",
		Operation:   "write",
		Description: "Update or sync folder-access and shared-folder state through the local control plane.",
	},
	controlCapabilityMCPGatewayWrite: {
		Name:        controlCapabilityMCPGatewayWrite,
		Category:    "mcp_gateway",
		Operation:   "write",
		Description: "Prepare governed MCP approvals, launch or stop declared MCP servers, and execute approved MCP tool calls through the local control plane.",
	},
	controlCapabilityOperatorMountWriteGrant: {
		Name:        controlCapabilityOperatorMountWriteGrant,
		Category:    "filesystem",
		Operation:   "write",
		Description: "Update session-scoped operator mount write-grant state through the local control plane.",
	},
	controlCapabilityQuarantineRead: {
		Name:        controlCapabilityQuarantineRead,
		Category:    "quarantine",
		Operation:   "read",
		Description: "Read quarantined payload metadata and bounded payload views through the local control plane.",
	},
	controlCapabilityQuarantineWrite: {
		Name:        controlCapabilityQuarantineWrite,
		Category:    "quarantine",
		Operation:   "write",
		Description: "Prune quarantined payload blobs through the local control plane while preserving authoritative metadata.",
	},
	controlCapabilitySiteInspect: {
		Name:        controlCapabilitySiteInspect,
		Category:    "site",
		Operation:   "read",
		Description: "Inspect a remote site through the local control plane using the bounded site-inspection path.",
	},
	controlCapabilitySiteTrustWrite: {
		Name:        controlCapabilitySiteTrustWrite,
		Category:    "site",
		Operation:   "write",
		Description: "Create site trust drafts through the local control plane.",
	},
	controlCapabilityUIRead: {
		Name:        controlCapabilityUIRead,
		Category:    "ui",
		Operation:   "read",
		Description: "Read display-safe Loopgate UI projections through the local control plane.",
	},
	controlCapabilityUIWrite: {
		Name:        controlCapabilityUIWrite,
		Category:    "ui",
		Operation:   "write",
		Description: "Update non-authoritative Loopgate UI state projections through the local control plane.",
	},
}

func isInternalControlCapability(capabilityName string) bool {
	_, found := internalControlCapabilityCatalog[capabilityName]
	return found
}

func (server *Server) recognizesCapability(capabilityName string) bool {
	registry := server.currentPolicyRuntime().registry
	if registry != nil && registry.Has(capabilityName) {
		return true
	}
	return isInternalControlCapability(capabilityName)
}

func capabilityScopeAllowed(tokenClaims capabilityToken, capabilityName string) bool {
	if len(tokenClaims.AllowedCapabilities) == 0 {
		return true
	}
	_, allowed := tokenClaims.AllowedCapabilities[capabilityName]
	return allowed
}

func controlCapabilitySummaries() []controlapipkg.CapabilitySummary {
	capabilityNames := make([]string, 0, len(internalControlCapabilityCatalog))
	for capabilityName := range internalControlCapabilityCatalog {
		capabilityNames = append(capabilityNames, capabilityName)
	}
	sort.Strings(capabilityNames)

	summaries := make([]controlapipkg.CapabilitySummary, 0, len(capabilityNames))
	for _, capabilityName := range capabilityNames {
		summaries = append(summaries, internalControlCapabilityCatalog[capabilityName])
	}
	return summaries
}
