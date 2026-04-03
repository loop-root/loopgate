package model

import (
	"strings"
	"testing"

	"morph/internal/tools"
)

func TestBuildNativeToolDefs_ReturnsOnlyAllowlistedTools(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&tools.FSRead{RepoRoot: "/tmp"})
	reg.Register(&tools.FSList{RepoRoot: "/tmp"})
	reg.Register(&tools.FSWrite{RepoRoot: "/tmp"})
	reg.Register(&tools.JournalWrite{Root: "/tmp"})
	reg.Register(&tools.MemoryRemember{})
	reg.Register(&tools.NoteCreate{StateDir: "/tmp"})
	reg.Register(&tools.TodoAdd{})
	reg.Register(&tools.TodoComplete{})
	reg.Register(&tools.TodoList{})

	defs := BuildNativeToolDefs(reg)
	if len(defs) != 9 {
		t.Fatalf("expected 9 defs, got %d", len(defs))
	}

	names := map[string]bool{}
	for _, d := range defs {
		names[d.Name] = true
	}
	if !names["fs_read"] {
		t.Error("expected fs_read in defs")
	}
	if !names["fs_list"] {
		t.Error("expected fs_list in defs")
	}
	if !names["fs_write"] {
		t.Error("expected fs_write in defs")
	}
	if !names["journal.write"] {
		t.Error("expected journal.write in defs")
	}
	if !names["memory.remember"] {
		t.Error("expected memory.remember in defs")
	}
	if !names["note.create"] {
		t.Error("expected note.create in defs")
	}
	if !names["todo.add"] {
		t.Error("expected todo.add in defs")
	}
	if !names["todo.complete"] {
		t.Error("expected todo.complete in defs")
	}
	if !names["todo.list"] {
		t.Error("expected todo.list in defs")
	}
}

func TestBuildNativeToolDefsForAllowedNames_FiltersToGrantedCapabilities(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&tools.FSRead{RepoRoot: "/tmp"})
	reg.Register(&tools.NotesWrite{Root: "/tmp"})
	reg.Register(&tools.MemoryRemember{})
	reg.Register(&tools.ShellExec{WorkDir: "/tmp"})

	defs := BuildNativeToolDefsForAllowedNames(reg, []string{
		"fs_read",
		"memory.remember",
	})

	if len(defs) != 2 {
		t.Fatalf("expected 2 defs, got %d", len(defs))
	}

	names := map[string]bool{}
	for _, definition := range defs {
		names[definition.Name] = true
	}

	if !names["fs_read"] {
		t.Fatal("expected fs_read in defs")
	}
	if !names["memory.remember"] {
		t.Fatal("expected memory.remember in defs")
	}
	if names["notes.write"] {
		t.Fatal("did not expect notes.write without a granted capability")
	}
	if names["shell_exec"] {
		t.Fatal("did not expect shell_exec without a granted capability")
	}
}

func TestBuildNativeToolDefs_NilRegistry(t *testing.T) {
	defs := BuildNativeToolDefs(nil)
	if defs != nil {
		t.Errorf("expected nil for nil registry, got %v", defs)
	}
}

func TestBuildNativeToolDefs_EmptyRegistry(t *testing.T) {
	reg := tools.NewRegistry()
	defs := BuildNativeToolDefs(reg)
	if len(defs) != 0 {
		t.Errorf("expected 0 defs for empty registry, got %d", len(defs))
	}
}

func TestBuildNativeToolDefs_SchemaShape(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&tools.FSRead{RepoRoot: "/tmp"})

	defs := BuildNativeToolDefs(reg)
	if len(defs) != 1 {
		t.Fatalf("expected 1 def, got %d", len(defs))
	}

	def := defs[0]
	if def.Name != "fs_read" {
		t.Errorf("expected name 'fs_read', got %q", def.Name)
	}
	if def.Description == "" {
		t.Error("expected non-empty description")
	}
	if def.InputSchema == nil {
		t.Fatal("expected non-nil InputSchema")
	}
	if def.InputSchema["type"] != "object" {
		t.Errorf("expected schema type 'object', got %v", def.InputSchema["type"])
	}

	// Check required field
	required, ok := def.InputSchema["required"].([]string)
	if !ok {
		t.Fatal("expected required to be []string")
	}
	foundPath := false
	for _, r := range required {
		if r == "path" {
			foundPath = true
		}
	}
	if !foundPath {
		t.Error("expected 'path' in required fields")
	}

	// Check properties
	props, ok := def.InputSchema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties to be a map")
	}
	pathProp, ok := props["path"].(map[string]interface{})
	if !ok {
		t.Fatal("expected path property to be a map")
	}
	if pathProp["type"] != "string" {
		t.Errorf("expected path type 'string', got %v", pathProp["type"])
	}

	// Check additionalProperties is false
	if def.InputSchema["additionalProperties"] != false {
		t.Errorf("expected additionalProperties=false, got %v", def.InputSchema["additionalProperties"])
	}
}

func TestBuildNativeToolDefs_CompactNativeTools_SingleDef(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&tools.FSRead{RepoRoot: "/tmp"})
	reg.Register(&tools.FSList{RepoRoot: "/tmp"})
	reg.Register(&tools.InvokeCapability{})

	defs := BuildNativeToolDefsForAllowedNamesWithOptions(reg, []string{"fs_read", "fs_list"}, NativeToolDefBuildOptions{
		CompactNativeTools: true,
	})
	if len(defs) != 1 {
		t.Fatalf("expected 1 def, got %d", len(defs))
	}
	if defs[0].Name != "invoke_capability" {
		t.Fatalf("expected invoke_capability, got %q", defs[0].Name)
	}
	if !strings.Contains(defs[0].Description, "fs_list") || !strings.Contains(defs[0].Description, "fs_read") {
		t.Fatalf("expected allowed names in description: %q", defs[0].Description)
	}
	props, ok := defs[0].InputSchema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties")
	}
	if _, ok := props["capability"]; !ok {
		t.Fatal("expected capability property")
	}
	if _, ok := props["arguments_json"]; !ok {
		t.Fatal("expected arguments_json property")
	}
	req, ok := defs[0].InputSchema["required"].([]string)
	if !ok || len(req) < 2 {
		t.Fatalf("expected capability and arguments_json in required, got %#v", defs[0].InputSchema["required"])
	}
	seen := map[string]bool{}
	for _, r := range req {
		seen[r] = true
	}
	if !seen["capability"] || !seen["arguments_json"] {
		t.Fatalf("expected required capability+arguments_json, got %v", req)
	}
}

func TestBuildNativeToolDefsForAllowedNamesWithOptions_HavenGuardsAppendToDescription(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&tools.MemoryRemember{})
	reg.Register(&tools.TodoAdd{})
	reg.Register(&tools.JournalWrite{Root: "/tmp"})

	defs := BuildNativeToolDefsForAllowedNamesWithOptions(reg, []string{"memory.remember", "todo.add", "journal.write"}, NativeToolDefBuildOptions{
		HavenUserIntentGuards: true,
	})
	if len(defs) != 3 {
		t.Fatalf("expected 3 defs, got %d", len(defs))
	}
	for _, d := range defs {
		if !strings.Contains(d.Description, "Only call when the user explicitly asked") {
			t.Fatalf("expected guard suffix on %q, got description %q", d.Name, d.Description)
		}
	}
}
