package loopgate

import (
	"context"

	toolspkg "morph/internal/tools"
)

const httpMethodGet = "GET"

type configuredCapabilityTool struct {
	definition configuredCapability
	executeFn  func(context.Context, string, map[string]string) (string, error)
}

func (configuredCapabilityTool *configuredCapabilityTool) Name() string {
	return configuredCapabilityTool.definition.Name
}

func (configuredCapabilityTool *configuredCapabilityTool) Category() string {
	return "http"
}

func (configuredCapabilityTool *configuredCapabilityTool) Operation() string {
	return toolspkg.OpRead
}

func (configuredCapabilityTool *configuredCapabilityTool) Schema() toolspkg.Schema {
	return toolspkg.Schema{
		Description: configuredCapabilityTool.definition.Description,
		Args:        []toolspkg.ArgDef{},
	}
}

func (configuredCapabilityTool *configuredCapabilityTool) Execute(ctx context.Context, args map[string]string) (string, error) {
	return configuredCapabilityTool.executeFn(ctx, configuredCapabilityTool.definition.Name, args)
}

// RawSecretExportProhibited ties configured integration names to the same legacy name heuristic used
// for unregistered capabilities, so operator-defined capabilities cannot bypass export blocking by
// merely being registered.
func (configuredCapabilityTool *configuredCapabilityTool) RawSecretExportProhibited() bool {
	return secretExportCapabilityNameHeuristic(configuredCapabilityTool.definition.Name)
}
