package model

import (
	"sort"
	"strings"

	"loopgate/internal/tools"
)

// NativeToolDefBuildOptions adjusts how provider-facing tool definitions are built.
type NativeToolDefBuildOptions struct {
	// UserIntentGuards appends a short constraint to descriptions of sensitive
	// capabilities so models do not call them for self-directed planning.
	UserIntentGuards bool
	// CompactNativeTools sends a single invoke_capability native tool definition whose
	// arguments_json encodes the target governed capability, shrinking provider
	// tool-schema TPM. Loopgate still authorizes the resolved capability name at
	// execution time.
	CompactNativeTools bool
}

const userIntentGuardSuffix = " Only call when the user explicitly asked for this outcome or it is strictly required to fulfill their current request; do not use for your own planning."

var nativeToolUserIntentGuards = map[string]bool{}

// nativeToolAllowlist defines which tools are eligible for the native
// structured tool-use API. Only tools in this list will be sent as
// NativeToolDef schemas to the provider. This is an explicit allowlist,
// not a generic "send everything" mechanism.
//
// Being in this list does NOT grant authorization. Loopgate remains
// the sole authority for capability execution.
var nativeToolAllowlist = map[string]bool{
	"fs_read":                 true,
	"fs_write":                true,
	"fs_list":                 true,
	"fs_mkdir":                true,
	"operator_mount.fs_read":  true,
	"operator_mount.fs_write": true,
	"operator_mount.fs_list":  true,
	"operator_mount.fs_mkdir": true,
	"shell_exec":              true,
	"host.folder.list":        true,
	"host.folder.read":        true,
	"host.organize.plan":      true,
	"host.plan.apply":         true,
	// Synthetic dispatcher schema used to compact provider-facing native tool defs.
	"invoke_capability": true,
}

// BuildNativeToolDefsForAllowedNames creates provider-API tool definitions from
// the tool registry for tools that are:
//  1. present in the registry
//  2. explicitly allowlisted for native structured tool use
//  3. present in the caller-provided allowed-name set
//
// Being present in the returned definitions does NOT grant authorization.
// Loopgate remains the sole authority for capability execution.
func BuildNativeToolDefsForAllowedNames(registry *tools.Registry, allowedNames []string) []NativeToolDef {
	return BuildNativeToolDefsForAllowedNamesWithOptions(registry, allowedNames, NativeToolDefBuildOptions{})
}

// BuildNativeToolDefsForAllowedNamesWithOptions is like BuildNativeToolDefsForAllowedNames with extra build flags.
func BuildNativeToolDefsForAllowedNamesWithOptions(registry *tools.Registry, allowedNames []string, opts NativeToolDefBuildOptions) []NativeToolDef {
	if registry == nil {
		return nil
	}

	allowedNameSet := make(map[string]struct{}, len(allowedNames))
	for _, allowedName := range allowedNames {
		allowedNameSet[allowedName] = struct{}{}
	}

	if opts.CompactNativeTools {
		return buildCompactInvokeNativeToolDefs(registry, allowedNameSet, opts)
	}

	var defs []NativeToolDef
	for _, name := range registry.List() {
		if !nativeToolAllowlist[name] {
			continue
		}
		if name == "invoke_capability" {
			continue
		}
		if len(allowedNameSet) > 0 {
			if _, allowed := allowedNameSet[name]; !allowed {
				continue
			}
		}

		tool := registry.Get(name)
		if tool == nil {
			continue
		}

		schema := tool.Schema()
		inputSchema := buildJSONSchema(schema)
		description := strings.TrimSpace(schema.Description)
		if opts.UserIntentGuards && nativeToolUserIntentGuards[name] {
			if description == "" {
				description = strings.TrimSpace(userIntentGuardSuffix)
			} else {
				description = description + userIntentGuardSuffix
			}
		}
		defs = append(defs, NativeToolDef{
			Name:        tool.Name(),
			Description: description,
			InputSchema: inputSchema,
		})
	}
	return defs
}

func buildCompactInvokeNativeToolDefs(registry *tools.Registry, allowedNameSet map[string]struct{}, _ NativeToolDefBuildOptions) []NativeToolDef {
	var allowed []string
	for _, name := range registry.List() {
		if name == "invoke_capability" {
			continue
		}
		if !nativeToolAllowlist[name] {
			continue
		}
		if len(allowedNameSet) > 0 {
			if _, ok := allowedNameSet[name]; !ok {
				continue
			}
		}
		allowed = append(allowed, name)
	}
	if len(allowed) == 0 {
		return nil
	}
	sort.Strings(allowed)
	dispatch := registry.Get("invoke_capability")
	if dispatch == nil {
		return nil
	}
	schema := dispatch.Schema()
	inputSchema := buildJSONSchema(schema)
	description := strings.TrimSpace(schema.Description)
	if len(allowed) > 0 {
		//nolint:gocritic // strings.Builder not worth it for bounded list
		description = description + "\n\nAllowed values for capability: " + strings.Join(allowed, ", ")
	}
	if len(description) > 8000 {
		description = description[:8000] + "…"
	}
	return []NativeToolDef{{
		Name:        "invoke_capability",
		Description: description,
		InputSchema: inputSchema,
	}}
}

// BuildNativeToolDefs creates provider-API tool definitions from the tool
// registry for tools that are both in the registry and in the native tool
// allowlist. Returns nil if no eligible tools are found.
//
// The returned schemas are narrow and explicit: required fields only, no
// freeform argument blobs. Each schema matches the tool's existing ArgDef
// declarations.
func BuildNativeToolDefs(registry *tools.Registry) []NativeToolDef {
	return BuildNativeToolDefsForAllowedNames(registry, nil)
}

// buildJSONSchema converts a tools.Schema into a JSON Schema object
// suitable for the provider's tool definition. Produces a strict object
// schema with explicit required fields and no additional properties.
func buildJSONSchema(schema tools.Schema) map[string]interface{} {
	properties := make(map[string]interface{})
	required := make([]string, 0)

	for _, arg := range schema.Args {
		prop := map[string]interface{}{
			"type":        jsonSchemaType(arg.Type),
			"description": arg.Description,
		}
		if arg.MaxLen > 0 {
			prop["maxLength"] = arg.MaxLen
		}
		properties[arg.Name] = prop
		if arg.Required {
			required = append(required, arg.Name)
		}
	}

	result := map[string]interface{}{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		result["required"] = required
	}
	return result
}

// jsonSchemaType maps tool arg types to JSON Schema types.
func jsonSchemaType(argType string) string {
	switch argType {
	case "int":
		return "integer"
	case "bool":
		return "boolean"
	case "path", "string", "":
		return "string"
	default:
		return "string"
	}
}
