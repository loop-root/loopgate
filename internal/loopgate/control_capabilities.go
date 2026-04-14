package loopgate

import "sort"

const (
	controlCapabilityAuditExport             = "audit.export"
	controlCapabilityConfigRead              = "config.read"
	controlCapabilityConfigWrite             = "config.write"
	controlCapabilityConnectionRead          = "connection.read"
	controlCapabilityConnectionWrite         = "connection.write"
	controlCapabilityDiagnosticRead          = "diagnostic.read"
	controlCapabilityFolderAccessRead        = "folder_access.read"
	controlCapabilityFolderAccessWrite       = "folder_access.write"
	controlCapabilityMemoryRead              = "memory.read"
	controlCapabilityMemoryWrite             = "memory.write"
	controlCapabilityMemoryReset             = "memory.reset"
	controlCapabilityMemoryReview            = "memory.review"
	controlCapabilityMemoryLineage           = "memory.lineage"
	controlCapabilityMCPGatewayWrite         = "mcp_gateway.write"
	controlCapabilityModelReply              = "model.reply"
	controlCapabilityModelSettingsRead       = "model.settings.read"
	controlCapabilityModelSettingsWrite      = "model.settings.write"
	controlCapabilityModelValidate           = "model.validate"
	controlCapabilityOperatorMountWriteGrant = "operator_mount.write_grant"
	controlCapabilityQuarantineRead          = "quarantine.read"
	controlCapabilityQuarantineWrite         = "quarantine.write"
	controlCapabilitySiteInspect             = "site.inspect"
	controlCapabilitySiteTrustWrite          = "site.trust.write"
	controlCapabilityUIRead                  = "ui.read"
	controlCapabilityUIWrite                 = "ui.write"
)

var internalControlCapabilityCatalog = map[string]CapabilitySummary{
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
	controlCapabilityMemoryRead: {
		Name:        controlCapabilityMemoryRead,
		Category:    "memory",
		Operation:   "read",
		Description: "Read Loopgate wake state, discovery results, and recall outputs through the local control plane.",
	},
	controlCapabilityMemoryWrite: {
		Name:        controlCapabilityMemoryWrite,
		Category:    "memory",
		Operation:   "write",
		Description: "Submit explicit or continuity-derived memory candidates for governed persistence through the local control plane.",
	},
	controlCapabilityMemoryReset: {
		Name:        controlCapabilityMemoryReset,
		Category:    "memory",
		Operation:   "write",
		Description: "Archive and reinitialize authoritative memory state through the local control plane.",
	},
	controlCapabilityMemoryReview: {
		Name:        controlCapabilityMemoryReview,
		Category:    "memory",
		Operation:   "review",
		Description: "Review a pending memory inspection lineage decision through the local control plane.",
	},
	controlCapabilityMemoryLineage: {
		Name:        controlCapabilityMemoryLineage,
		Category:    "memory",
		Operation:   "write",
		Description: "Apply lineage transitions such as tombstone or purge to governed memory artifacts through the local control plane.",
	},
	controlCapabilityMCPGatewayWrite: {
		Name:        controlCapabilityMCPGatewayWrite,
		Category:    "mcp_gateway",
		Operation:   "write",
		Description: "Prepare governed MCP approvals, launch or stop declared MCP servers, and execute approved MCP tool calls through the local control plane.",
	},
	controlCapabilityModelReply: {
		Name:        controlCapabilityModelReply,
		Category:    "model",
		Operation:   "execute",
		Description: "Run a model round-trip through the Loopgate-governed local control plane.",
	},
	controlCapabilityModelSettingsRead: {
		Name:        controlCapabilityModelSettingsRead,
		Category:    "model",
		Operation:   "read",
		Description: "Read Haven-facing model settings through the local control plane.",
	},
	controlCapabilityModelSettingsWrite: {
		Name:        controlCapabilityModelSettingsWrite,
		Category:    "model",
		Operation:   "write",
		Description: "Update Haven-facing model settings through the local control plane.",
	},
	controlCapabilityModelValidate: {
		Name:        controlCapabilityModelValidate,
		Category:    "model",
		Operation:   "validate",
		Description: "Validate runtime model configuration through the local control plane without executing a model round-trip.",
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
		Description: "Read display-safe Loopgate and Haven UI projections through the local control plane.",
	},
	controlCapabilityUIWrite: {
		Name:        controlCapabilityUIWrite,
		Category:    "ui",
		Operation:   "write",
		Description: "Update non-authoritative Loopgate and Haven UI state projections through the local control plane.",
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

func controlCapabilitySummaries() []CapabilitySummary {
	capabilityNames := make([]string, 0, len(internalControlCapabilityCatalog))
	for capabilityName := range internalControlCapabilityCatalog {
		capabilityNames = append(capabilityNames, capabilityName)
	}
	sort.Strings(capabilityNames)

	summaries := make([]CapabilitySummary, 0, len(capabilityNames))
	for _, capabilityName := range capabilityNames {
		summaries = append(summaries, internalControlCapabilityCatalog[capabilityName])
	}
	return summaries
}
