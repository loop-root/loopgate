package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"morph/internal/loopgate"
)

// workspaceFakeClient extends fakeLoopgateClient with configurable SandboxList/Import responses.
type workspaceFakeClient struct {
	fakeLoopgateClient
	listResponse   loopgate.SandboxListResponse
	listErr        error
	importResponse loopgate.SandboxOperationResponse
	importErr      error
}

func (w *workspaceFakeClient) SandboxList(_ context.Context, _ loopgate.SandboxListRequest) (loopgate.SandboxListResponse, error) {
	return w.listResponse, w.listErr
}

func (w *workspaceFakeClient) SandboxImport(_ context.Context, req loopgate.SandboxImportRequest) (loopgate.SandboxOperationResponse, error) {
	return w.importResponse, w.importErr
}

func TestWorkspaceList_MapsDirectoryNames(t *testing.T) {
	// Root listing now uses sandboxHome directly, not Loopgate.
	// Create a temp sandbox home with the expected directories.
	sandboxHome := t.TempDir()
	for _, dir := range []string{"workspace", "imports", "outputs", "scratch", "agents"} {
		if err := os.MkdirAll(filepath.Join(sandboxHome, dir), 0o755); err != nil {
			t.Fatalf("create sandbox dir %s: %v", dir, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(sandboxHome, "imports", "shared"), 0o755); err != nil {
		t.Fatalf("create shared mirror dir: %v", err)
	}

	app, _ := testApp(t, &fakeLoopgateClient{})
	app.sandboxHome = sandboxHome

	resp, err := app.WorkspaceList("")
	if err != nil {
		t.Fatalf("workspace list: %v", err)
	}

	expected := map[string]string{
		"workspace": "projects",
		"imports":   "imports",
		"outputs":   "artifacts",
		"scratch":   "research",
		"agents":    "agents",
		"shared":    "shared",
	}

	if len(resp.Entries) != len(expected) {
		t.Fatalf("expected %d entries, got %d: %+v", len(expected), len(resp.Entries), resp.Entries)
	}

	// Verify specific mappings.
	nameSet := make(map[string]bool)
	for _, e := range resp.Entries {
		nameSet[e.Name] = true
	}
	for _, havenName := range expected {
		if !nameSet[havenName] {
			t.Errorf("expected Haven name %q in entries", havenName)
		}
	}
}

func TestWorkspaceImportPath_EmptyRejected(t *testing.T) {
	app, _ := testApp(t, &fakeLoopgateClient{})

	resp := app.WorkspaceImportPath("")
	if resp.Imported {
		t.Error("expected empty path to be rejected")
	}
	if resp.Error == "" {
		t.Error("expected error message for empty path")
	}
}

func TestWorkspaceImportPath_DelegatesToSandboxImport(t *testing.T) {
	client := &workspaceFakeClient{
		importResponse: loopgate.SandboxOperationResponse{
			Action:              "import",
			EntryType:           "file",
			SandboxRelativePath: "imports/notes.txt",
			SandboxAbsolutePath: "/morph/home/imports/notes.txt",
		},
	}
	app, _ := testApp(t, &client.fakeLoopgateClient)
	app.loopgateClient = client

	resp := app.WorkspaceImportPath("/Users/test/notes.txt")
	if !resp.Imported {
		t.Fatalf("expected import to succeed, got error: %s", resp.Error)
	}
	if resp.Name != "notes.txt" {
		t.Errorf("expected name 'notes.txt', got %q", resp.Name)
	}
	if resp.Path != "imports/notes.txt" {
		t.Errorf("expected path 'imports/notes.txt', got %q", resp.Path)
	}
}

func TestWorkspaceCreateDir_UsesLoopgateFSMkdir(t *testing.T) {
	client := &fakeLoopgateClient{}
	app, emitter := testApp(t, client)

	resp := app.WorkspaceCreateDir("projects/new-folder")
	if !resp.Created {
		t.Fatalf("expected create dir to succeed, got error: %s", resp.Error)
	}

	requests := client.recordedCapabilityRequests()
	if len(requests) != 1 {
		t.Fatalf("expected one capability request, got %d", len(requests))
	}
	if requests[0].Capability != "fs_mkdir" {
		t.Fatalf("expected fs_mkdir capability, got %q", requests[0].Capability)
	}
	if requests[0].Arguments["path"] != "workspace/new-folder" {
		t.Fatalf("expected sandbox path workspace/new-folder, got %#v", requests[0].Arguments)
	}
	fileChangedEvents := emitter.eventsByName("haven:file_changed")
	if len(fileChangedEvents) != 1 {
		t.Fatalf("expected one haven:file_changed event, got %d", len(fileChangedEvents))
	}
	eventData, ok := fileChangedEvents[0].Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected file_changed event payload map, got %#v", fileChangedEvents[0].Data)
	}
	if eventData["path"] != "projects/new-folder" {
		t.Fatalf("expected Haven-facing event path projects/new-folder, got %#v", eventData)
	}
}

func TestWorkspaceDelete_StillUsesDirectFilesystemBypass(t *testing.T) {
	client := &fakeLoopgateClient{}
	app, _ := testApp(t, client)
	app.sandboxHome = t.TempDir()

	targetDir := filepath.Join(app.sandboxHome, "workspace")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("create workspace dir: %v", err)
	}
	targetFile := filepath.Join(targetDir, "delete-me.txt")
	if err := os.WriteFile(targetFile, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	resp := app.WorkspaceDelete("projects/delete-me.txt")
	if !resp.Deleted {
		t.Fatalf("expected delete to succeed, got error: %s", resp.Error)
	}
	if _, err := os.Stat(targetFile); !os.IsNotExist(err) {
		t.Fatalf("expected file to be deleted, stat err=%v", err)
	}
	if len(client.recordedCapabilityRequests()) != 0 {
		t.Fatalf("expected no Loopgate capability calls for delete bypass, got %#v", client.recordedCapabilityRequests())
	}
}

func TestWorkspaceDelete_NonEmptyDirectoryReturnsFriendlyError(t *testing.T) {
	client := &fakeLoopgateClient{}
	app, _ := testApp(t, client)
	app.sandboxHome = t.TempDir()

	targetDir := filepath.Join(app.sandboxHome, "workspace", "non-empty")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("create non-empty dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "child.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write child file: %v", err)
	}

	resp := app.WorkspaceDelete("projects/non-empty")
	if resp.Deleted {
		t.Fatal("expected delete to fail for non-empty directory")
	}
	if resp.Error != "directory is not empty" {
		t.Fatalf("expected non-empty directory error, got %q", resp.Error)
	}
	if _, err := os.Stat(targetDir); err != nil {
		t.Fatalf("expected directory to remain after failed delete: %v", err)
	}
}

func TestIsNotEmptyError(t *testing.T) {
	if !isNotEmptyError(syscall.ENOTEMPTY) {
		t.Fatal("expected ENOTEMPTY to be recognized")
	}
	if !isNotEmptyError(&os.PathError{Op: "remove", Path: "/tmp/example", Err: syscall.ENOTEMPTY}) {
		t.Fatal("expected wrapped ENOTEMPTY to be recognized")
	}
	if isNotEmptyError(errors.New("other")) {
		t.Fatal("did not expect arbitrary error to be recognized as not empty")
	}
}

func TestWorkspaceRename_StillUsesDirectFilesystemBypass(t *testing.T) {
	client := &fakeLoopgateClient{}
	app, _ := testApp(t, client)
	app.sandboxHome = t.TempDir()

	targetDir := filepath.Join(app.sandboxHome, "workspace")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("create workspace dir: %v", err)
	}
	originalFile := filepath.Join(targetDir, "old.txt")
	if err := os.WriteFile(originalFile, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	resp := app.WorkspaceRename("projects/old.txt", "new.txt")
	if !resp.Renamed {
		t.Fatalf("expected rename to succeed, got error: %s", resp.Error)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "new.txt")); err != nil {
		t.Fatalf("expected renamed file to exist: %v", err)
	}
	if _, err := os.Stat(originalFile); !os.IsNotExist(err) {
		t.Fatalf("expected old file to be gone, stat err=%v", err)
	}
	if len(client.recordedCapabilityRequests()) != 0 {
		t.Fatalf("expected no Loopgate capability calls for rename bypass, got %#v", client.recordedCapabilityRequests())
	}
}

func TestMapHavenPathToSandbox(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{".", ""},
		{"projects", "workspace"},
		{"projects/myapp", "workspace/myapp"},
		{"shared", "imports/shared"},
		{"shared/brief.md", "imports/shared/brief.md"},
		{"artifacts", "outputs"},
		{"artifacts/report.pdf", "outputs/report.pdf"},
		{"research", "scratch"},
		{"imports", "imports"},
		{"agents", "agents"}, // unmapped
	}

	for _, tc := range tests {
		got := mapHavenPathToSandbox(tc.input)
		if got != tc.expected {
			t.Errorf("mapHavenPathToSandbox(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestMapSandboxPathToHaven(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{".", ""},
		{"workspace", "projects"},
		{"workspace/myapp", "projects/myapp"},
		{"imports/shared", "shared"},
		{"imports/shared/brief.md", "shared/brief.md"},
		{"outputs", "artifacts"},
		{"outputs/report.pdf", "artifacts/report.pdf"},
		{"scratch", "research"},
		{"imports", "imports"},
		{"agents", "agents"}, // unmapped
	}

	for _, tc := range tests {
		got := mapSandboxPathToHaven(tc.input)
		if got != tc.expected {
			t.Errorf("mapSandboxPathToHaven(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}
