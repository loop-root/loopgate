package tools

import (
	"context"
	"fmt"
)

// HostFolderList lists entries in a granted host folder (real filesystem).
// Execution is implemented in Loopgate; the registry entry exists for discovery and schema validation.
type HostFolderList struct{}

func (t *HostFolderList) Name() string      { return "host.folder.list" }
func (t *HostFolderList) Category() string  { return "host" }
func (t *HostFolderList) Operation() string { return OpRead }

func (t *HostFolderList) Schema() Schema {
	return Schema{
		Description: "List files and directories in a user-granted host folder (e.g. downloads, desktop). folder_name is the preset id (shared, downloads, desktop, documents) or a matching folder label. Optional path is a relative subpath inside that folder.",
		Args: []ArgDef{
			{Name: "folder_name", Description: "Folder preset id or display name (e.g. downloads)", Required: true, Type: "string", MaxLen: 64},
			{Name: "path", Description: "Optional subdirectory relative to the granted folder root", Required: false, Type: "string", MaxLen: 512},
		},
	}
}

func (t *HostFolderList) Execute(context.Context, map[string]string) (string, error) {
	return "", fmt.Errorf("host.folder.list must be executed through loopgate host access handling")
}

// HostFolderRead reads a text file from a granted host folder.
type HostFolderRead struct{}

func (t *HostFolderRead) Name() string      { return "host.folder.read" }
func (t *HostFolderRead) Category() string  { return "host" }
func (t *HostFolderRead) Operation() string { return OpRead }

func (t *HostFolderRead) Schema() Schema {
	return Schema{
		Description: "Read a file from a granted host folder. path is relative to the folder root.",
		Args: []ArgDef{
			{Name: "folder_name", Description: "Folder preset id or display name", Required: true, Type: "string", MaxLen: 64},
			{Name: "path", Description: "File path relative to the granted folder", Required: true, Type: "string", MaxLen: 1024},
		},
	}
}

func (t *HostFolderRead) Execute(context.Context, map[string]string) (string, error) {
	return "", fmt.Errorf("host.folder.read must be executed through loopgate host access handling")
}

// HostOrganizePlan records a proposed organization plan for a granted folder (no host mutations).
type HostOrganizePlan struct{}

func (t *HostOrganizePlan) Name() string      { return "host.organize.plan" }
func (t *HostOrganizePlan) Category() string  { return "host" }
func (t *HostOrganizePlan) Operation() string { return OpRead }

func (t *HostOrganizePlan) Schema() Schema {
	return Schema{
		Description: "Submit a plan of move/mkdir operations relative to a granted host folder. Returns a plan_id for host.plan.apply after operator approval. plan_json is a JSON array of {\"kind\":\"mkdir\",\"path\":\"rel\"} or {\"kind\":\"move\",\"from\":\"rel\",\"to\":\"rel\"} (paths relative to folder root). You may pass that array directly inside invoke_capability's arguments_json object, or as a string containing the same JSON array.",
		Args: []ArgDef{
			{Name: "folder_name", Description: "Folder preset id or display name (e.g. downloads, desktop)", Required: true, Type: "string", MaxLen: 64},
			{Name: "plan_json", Description: "JSON array of organize operations (or stringified JSON array of those objects)", Required: true, Type: "string", MaxLen: 65536},
			{Name: "summary", Description: "Short human summary of the plan", Required: false, Type: "string", MaxLen: 512},
		},
	}
}

func (t *HostOrganizePlan) Execute(context.Context, map[string]string) (string, error) {
	return "", fmt.Errorf("host.organize.plan must be executed through loopgate host access handling")
}

// HostPlanApply applies a previously stored organization plan on the host.
type HostPlanApply struct{}

func (t *HostPlanApply) Name() string      { return "host.plan.apply" }
func (t *HostPlanApply) Category() string  { return "host" }
func (t *HostPlanApply) Operation() string { return OpWrite }

func (t *HostPlanApply) Schema() Schema {
	return Schema{
		Description: "Apply a stored organization plan by plan_id. Requires operator approval. Re-validates paths under the granted folder before executing. Each plan_id is single-use: after a successful apply the id is retired—call host.organize.plan again for a new id if you need further changes or if apply failed with a stale id.",
		Args: []ArgDef{
			{Name: "plan_id", Description: "Plan id returned by host.organize.plan (single-use after successful apply)", Required: true, Type: "string", MaxLen: 64},
		},
	}
}

func (t *HostPlanApply) Execute(context.Context, map[string]string) (string, error) {
	return "", fmt.Errorf("host.plan.apply must be executed through loopgate host access handling")
}
