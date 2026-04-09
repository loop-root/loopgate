package loopgate

import "sort"

const (
	controlCapabilityConfigRead    = "config.read"
	controlCapabilityConfigWrite   = "config.write"
	controlCapabilityGoalSet       = "goal.set"
	controlCapabilityGoalClose     = "goal.close"
	controlCapabilityMemoryRead    = "memory.read"
	controlCapabilityMemoryWrite   = "memory.write"
	controlCapabilityMemoryReview  = "memory.review"
	controlCapabilityMemoryLineage = "memory.lineage"
)

var internalControlCapabilityCatalog = map[string]CapabilitySummary{
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
	controlCapabilityGoalSet: {
		Name:        controlCapabilityGoalSet,
		Category:    "continuity",
		Operation:   "write",
		Description: "Set a named persistent goal tracked across sessions in the continuity system.",
	},
	controlCapabilityGoalClose: {
		Name:        controlCapabilityGoalClose,
		Category:    "continuity",
		Operation:   "write",
		Description: "Close a goal when the objective has been achieved or is no longer relevant.",
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
}

func isInternalControlCapability(capabilityName string) bool {
	_, found := internalControlCapabilityCatalog[capabilityName]
	return found
}

func (server *Server) recognizesCapability(capabilityName string) bool {
	if server.registry != nil && server.registry.Has(capabilityName) {
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
