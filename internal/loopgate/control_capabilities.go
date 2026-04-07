package loopgate

const (
	controlCapabilityConfigRead  = "config.read"
	controlCapabilityConfigWrite = "config.write"
	controlCapabilityGoalSet     = "goal.set"
	controlCapabilityGoalClose   = "goal.close"
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
