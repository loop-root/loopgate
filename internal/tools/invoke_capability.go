package tools

import (
	"context"
	"fmt"
)

// InvokeCapability is a synthetic registry entry used only for compact native
// tool schemas: the model calls invoke_capability with a target capability name
// and JSON arguments. The current Loopgate path does not execute this tool
// directly.
type InvokeCapability struct{}

func (t *InvokeCapability) Name() string      { return "invoke_capability" }
func (t *InvokeCapability) Category() string  { return "dispatch" }
func (t *InvokeCapability) Operation() string { return OpRead }

func (t *InvokeCapability) Schema() Schema {
	return Schema{
		Description: "Dispatch exactly one Loopgate capability. Set capability to the registry id (must match an id you were given). Set arguments_json to a string containing one JSON object whose keys are that tool's parameters. Examples: fs_read -> arguments_json '{\"path\":\"workspace/README.md\"}'. host.folder.list -> '{\"folder_name\":\"downloads\",\"path\":\".\"}'. host.organize.plan -> '{\"folder_name\":\"downloads\",\"plan_json\":[{\"kind\":\"mkdir\",\"path\":\"a\"}],\"summary\":\"...\"}' -- plan_json may be a JSON array inside that object or a string holding the same array text. host.plan.apply -> '{\"plan_id\":\"...\"}'. Omitting arguments_json or passing non-JSON text fails validation.",
		Args: []ArgDef{
			{
				Name:        "capability",
				Description: "Exact capability id to invoke (e.g. fs_read, host.folder.list)",
				Required:    true,
				Type:        "string",
				MaxLen:      128,
			},
			{
				Name:        "arguments_json",
				Description: "Required: string body that parses as one JSON object of arguments for the target capability (not the outer tool call's raw object unless your client coerces it to this string).",
				Required:    true,
				Type:        "string",
				MaxLen:      65536,
			},
		},
	}
}

func (t *InvokeCapability) Execute(ctx context.Context, args map[string]string) (string, error) {
	_ = ctx
	_ = args
	return "", fmt.Errorf("invoke_capability is not executed directly on the current Loopgate path")
}
