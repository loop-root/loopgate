package orchestrator

import (
	"strings"
	"testing"

	"loopgate/internal/model"
	"loopgate/internal/tools"
)

// testTool is a minimal Tool implementation for testing.
type testTool struct {
	name      string
	category  string
	operation string
	schema    tools.Schema
}

func (t *testTool) Name() string      { return t.name }
func (t *testTool) Category() string  { return t.category }
func (t *testTool) Operation() string { return t.operation }
func (t *testTool) Schema() tools.Schema {
	if t.schema.Description != "" || len(t.schema.Args) > 0 {
		return t.schema
	}
	return tools.Schema{
		Description: t.name,
		Args: []tools.ArgDef{
			{Name: "path", Description: "file path", Required: true, Type: "path"},
		},
	}
}
func (t *testTool) Execute(_ interface{}, _ map[string]string) (string, error) {
	return "", nil
}

func newTestRegistry() *tools.Registry {
	reg := tools.NewRegistry()
	reg.Register(&tools.FSRead{RepoRoot: "/tmp"})
	reg.Register(&tools.FSList{RepoRoot: "/tmp"})
	reg.Register(&tools.InvokeCapability{})
	return reg
}

func TestExtractStructuredCalls_InvokeCapability_ExpandsToInnerTool(t *testing.T) {
	reg := newTestRegistry()
	blocks := []model.ToolUseBlock{
		{
			ID:   "call_ic1",
			Name: "invoke_capability",
			Input: map[string]string{
				"capability":     "fs_read",
				"arguments_json": `{"path":"README.md"}`,
			},
		},
	}
	calls, errs := ExtractStructuredCalls(blocks, reg)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %#v", errs)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "fs_read" {
		t.Fatalf("expected fs_read, got %q", calls[0].Name)
	}
	if calls[0].Args["path"] != "README.md" {
		t.Fatalf("expected path README.md, got %q", calls[0].Args["path"])
	}
}

func TestExtractStructuredCalls_InvokeCapability_MissingArgumentsJSONRejected(t *testing.T) {
	reg := newTestRegistry()
	blocks := []model.ToolUseBlock{
		{
			ID:   "call_no_args",
			Name: "invoke_capability",
			Input: map[string]string{
				"capability": "fs_read",
			},
		},
	}
	calls, errs := ExtractStructuredCalls(blocks, reg)
	if len(calls) != 0 {
		t.Fatalf("expected 0 calls, got %d", len(calls))
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %#v", errs)
	}
	if !strings.Contains(errs[0].Error(), "arguments_json") && !strings.Contains(errs[0].Error(), "missing required") {
		t.Fatalf("expected missing arguments_json error, got %v", errs[0])
	}
}

func TestExtractStructuredCalls_InvokeCapability_NestedRejected(t *testing.T) {
	reg := newTestRegistry()
	blocks := []model.ToolUseBlock{
		{
			ID:   "bad",
			Name: "invoke_capability",
			Input: map[string]string{
				"capability":     "invoke_capability",
				"arguments_json": `{}`,
			},
		},
	}
	calls, errs := ExtractStructuredCalls(blocks, reg)
	if len(calls) != 0 {
		t.Fatalf("expected 0 calls, got %d", len(calls))
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "nested") {
		t.Fatalf("expected nested error, got %#v", errs)
	}
}

func TestExtractStructuredCalls_FSRead(t *testing.T) {
	reg := newTestRegistry()
	blocks := []model.ToolUseBlock{
		{
			ID:    "toolu_01abc",
			Name:  "fs_read",
			Input: map[string]string{"path": "README.md"},
		},
	}

	calls, errs := ExtractStructuredCalls(blocks, reg)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}

	call := calls[0]
	if call.ID != "toolu_01abc" {
		t.Errorf("expected ID 'toolu_01abc', got %q", call.ID)
	}
	if call.Name != "fs_read" {
		t.Errorf("expected name 'fs_read', got %q", call.Name)
	}
	if call.Args["path"] != "README.md" {
		t.Errorf("expected path 'README.md', got %q", call.Args["path"])
	}
}

func TestExtractStructuredCalls_FSList(t *testing.T) {
	reg := newTestRegistry()
	blocks := []model.ToolUseBlock{
		{
			ID:    "toolu_02def",
			Name:  "fs_list",
			Input: map[string]string{"path": "src/"},
		},
	}

	calls, errs := ExtractStructuredCalls(blocks, reg)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "fs_list" {
		t.Errorf("expected name 'fs_list', got %q", calls[0].Name)
	}
	if calls[0].Args["path"] != "src/" {
		t.Errorf("expected path 'src/', got %q", calls[0].Args["path"])
	}
}

func TestExtractStructuredCalls_ListAliasToFSList(t *testing.T) {
	reg := newTestRegistry()
	blocks := []model.ToolUseBlock{
		{ID: "toolu_list", Name: "list", Input: map[string]string{"path": "."}},
	}
	calls, errs := ExtractStructuredCalls(blocks, reg)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %#v", errs)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "fs_list" {
		t.Fatalf("expected fs_list, got %q", calls[0].Name)
	}
}

func TestExtractStructuredCalls_AnthropicStyleDottedNamesAsUnderscores(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&tools.HostFolderList{})
	blocks := []model.ToolUseBlock{
		{ID: "tu1", Name: "host_folder_list", Input: map[string]string{"folder_name": "downloads"}},
	}
	calls, errs := ExtractStructuredCalls(blocks, reg)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %#v", errs)
	}
	if len(calls) != 1 || calls[0].Name != "host.folder.list" {
		t.Fatalf("expected canonical host.folder.list, got %#v", calls)
	}
}

func TestExtractStructuredCalls_HostFolderUnderscoreAliases(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&tools.HostFolderList{})
	reg.Register(&tools.HostOrganizePlan{})
	blocks := []model.ToolUseBlock{
		{ID: "a1", Name: "host_folder_list", Input: map[string]string{"folder_name": "downloads"}},
		{ID: "a2", Name: "host_organize_plan", Input: map[string]string{
			"folder_name": "downloads",
			"plan_json":   `[{"kind":"mkdir","path":"x"}]`,
			"summary":     "test",
		}},
	}
	calls, errs := ExtractStructuredCalls(blocks, reg)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %#v", errs)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].Name != "host.folder.list" || calls[1].Name != "host.organize.plan" {
		t.Fatalf("unexpected names: %#v", calls)
	}
}

func TestExtractStructuredCalls_UnknownTool_Rejected(t *testing.T) {
	reg := newTestRegistry()
	blocks := []model.ToolUseBlock{
		{
			ID:    "toolu_bad",
			Name:  "exec_shell",
			Input: map[string]string{"command": "rm -rf /"},
		},
	}

	calls, errs := ExtractStructuredCalls(blocks, reg)
	if len(calls) != 0 {
		t.Fatalf("expected 0 calls for unknown tool, got %d", len(calls))
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if !contains(errs[0].Error(), "unknown tool") {
		t.Errorf("expected 'unknown tool' error, got %q", errs[0].Error())
	}
}

func TestExtractStructuredCalls_ReservedMorphCommand_Rejected(t *testing.T) {
	reg := newTestRegistry()
	blocks := []model.ToolUseBlock{
		{
			ID:    "toolu_cmd",
			Name:  "model",
			Input: map[string]string{"action": "add"},
		},
	}

	calls, errs := ExtractStructuredCalls(blocks, reg)
	if len(calls) != 0 {
		t.Fatalf("expected 0 calls for reserved command, got %d", len(calls))
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if !contains(errs[0].Error(), "reserved local command") {
		t.Errorf("expected 'reserved local command' error, got %q", errs[0].Error())
	}
}

func TestExtractStructuredCalls_MalformedInput_MissingRequired(t *testing.T) {
	reg := newTestRegistry()
	blocks := []model.ToolUseBlock{
		{
			ID:    "toolu_nope",
			Name:  "fs_read",
			Input: map[string]string{}, // missing required "path"
		},
	}

	calls, errs := ExtractStructuredCalls(blocks, reg)
	if len(calls) != 0 {
		t.Fatalf("expected 0 calls for missing required arg, got %d", len(calls))
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if !contains(errs[0].Error(), "invalid input") {
		t.Errorf("expected 'invalid input' error, got %q", errs[0].Error())
	}
}

func TestExtractStructuredCalls_NilInput_MissingRequired(t *testing.T) {
	reg := newTestRegistry()
	blocks := []model.ToolUseBlock{
		{
			ID:    "toolu_nil",
			Name:  "fs_read",
			Input: nil,
		},
	}

	calls, errs := ExtractStructuredCalls(blocks, reg)
	if len(calls) != 0 {
		t.Fatalf("expected 0 calls for nil input, got %d", len(calls))
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
}

func TestExtractStructuredCalls_EmptyName_Rejected(t *testing.T) {
	reg := newTestRegistry()
	blocks := []model.ToolUseBlock{
		{
			ID:    "toolu_empty",
			Name:  "",
			Input: map[string]string{"path": "foo"},
		},
	}

	calls, errs := ExtractStructuredCalls(blocks, reg)
	if len(calls) != 0 {
		t.Fatalf("expected 0 calls for empty name, got %d", len(calls))
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if !contains(errs[0].Error(), "empty name") {
		t.Errorf("expected 'empty name' error, got %q", errs[0].Error())
	}
}

func TestExtractStructuredCalls_EmptyBlocks(t *testing.T) {
	reg := newTestRegistry()
	calls, errs := ExtractStructuredCalls(nil, reg)
	if len(calls) != 0 {
		t.Errorf("expected 0 calls for nil blocks, got %d", len(calls))
	}
	if len(errs) != 0 {
		t.Errorf("expected 0 errors for nil blocks, got %d", len(errs))
	}
}

func TestExtractStructuredCalls_MultipleBlocks_MixedValidity(t *testing.T) {
	reg := newTestRegistry()
	blocks := []model.ToolUseBlock{
		{
			ID:    "toolu_good",
			Name:  "fs_read",
			Input: map[string]string{"path": "ok.txt"},
		},
		{
			ID:    "toolu_bad",
			Name:  "unknown_tool",
			Input: map[string]string{"x": "y"},
		},
		{
			ID:    "toolu_also_good",
			Name:  "fs_list",
			Input: map[string]string{"path": "."},
		},
	}

	calls, errs := ExtractStructuredCalls(blocks, reg)
	if len(calls) != 2 {
		t.Fatalf("expected 2 valid calls, got %d", len(calls))
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if calls[0].Name != "fs_read" {
		t.Errorf("expected first call 'fs_read', got %q", calls[0].Name)
	}
	if calls[1].Name != "fs_list" {
		t.Errorf("expected second call 'fs_list', got %q", calls[1].Name)
	}
}

func TestExtractStructuredCalls_GeneratesIDWhenMissing(t *testing.T) {
	reg := newTestRegistry()
	blocks := []model.ToolUseBlock{
		{
			ID:    "", // no provider ID
			Name:  "fs_read",
			Input: map[string]string{"path": "test.txt"},
		},
	}

	calls, errs := ExtractStructuredCalls(blocks, reg)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].ID == "" {
		t.Error("expected generated call ID, got empty string")
	}
	if !contains(calls[0].ID, "call_") {
		t.Errorf("expected generated ID to start with 'call_', got %q", calls[0].ID)
	}
}

func TestExtractStructuredCalls_SlashPrefixName_Rejected(t *testing.T) {
	reg := newTestRegistry()
	blocks := []model.ToolUseBlock{
		{
			ID:    "toolu_slash",
			Name:  "/exit",
			Input: map[string]string{},
		},
	}

	calls, errs := ExtractStructuredCalls(blocks, reg)
	if len(calls) != 0 {
		t.Fatalf("expected 0 calls for slash-prefix name, got %d", len(calls))
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
}
