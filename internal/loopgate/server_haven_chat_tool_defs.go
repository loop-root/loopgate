package loopgate

import (
	"sort"
	"strings"

	modelpkg "morph/internal/model"
)

// havenFilterOutCapability returns a copy of summaries without any entry whose Name matches name.
func havenFilterOutCapability(summaries []CapabilitySummary, name string) []CapabilitySummary {
	filtered := make([]CapabilitySummary, 0, len(summaries))
	for _, summary := range summaries {
		if summary.Name != name {
			filtered = append(filtered, summary)
		}
	}
	return filtered
}

func filterHavenCapabilitySummaries(availableCapabilities []CapabilitySummary, allowedCapabilities map[string]struct{}) []CapabilitySummary {
	if len(allowedCapabilities) == 0 {
		return append([]CapabilitySummary(nil), availableCapabilities...)
	}
	filteredCapabilities := make([]CapabilitySummary, 0, len(availableCapabilities))
	for _, availableCapability := range availableCapabilities {
		if _, allowed := allowedCapabilities[availableCapability.Name]; !allowed {
			continue
		}
		filteredCapabilities = append(filteredCapabilities, availableCapability)
	}
	return filteredCapabilities
}

func buildHavenToolDefinitions(capabilitySummaries []CapabilitySummary) []modelpkg.ToolDefinition {
	toolDefinitions := make([]modelpkg.ToolDefinition, 0, len(capabilitySummaries))
	for _, capabilitySummary := range capabilitySummaries {
		toolDefinitions = append(toolDefinitions, modelpkg.ToolDefinition{
			Name:        capabilitySummary.Name,
			Operation:   capabilitySummary.Operation,
			Description: capabilitySummary.Description,
		})
	}
	return toolDefinitions
}

func buildCompactInvokeCapabilityToolDefinitions(allowedCapabilityNames []string) []modelpkg.ToolDefinition {
	sortedCapabilityNames := append([]string(nil), allowedCapabilityNames...)
	sort.Strings(sortedCapabilityNames)
	allowedListing := strings.Join(sortedCapabilityNames, ", ")
	if len(allowedListing) > 8000 {
		allowedListing = allowedListing[:8000] + "…"
	}
	return []modelpkg.ToolDefinition{{
		Name:        "invoke_capability",
		Operation:   "dispatch",
		Description: "Single native structured tool for this session. Set capability to one of these exact ids and pass that tool's parameters as a JSON object in arguments_json. Allowed capability names: " + allowedListing,
	}}
}

func capabilityNamesFromSummaries(capabilitySummaries []CapabilitySummary) []string {
	capabilityNames := make([]string, 0, len(capabilitySummaries))
	for _, capabilitySummary := range capabilitySummaries {
		capabilityNames = append(capabilityNames, capabilitySummary.Name)
	}
	return capabilityNames
}
