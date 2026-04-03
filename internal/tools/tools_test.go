package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFSRead_Success(t *testing.T) {
	// Create a temp directory and file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	content := "hello world"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &FSRead{
		RepoRoot:     tmpDir,
		AllowedRoots: []string{"."},
		DeniedPaths:  []string{},
	}

	result, err := tool.Execute(context.Background(), map[string]string{"path": "test.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != content {
		t.Errorf("expected %q, got %q", content, result)
	}
}

func TestFSRead_ExceedsSizeLimit(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "large.txt")
	// Create a file larger than the custom limit.
	largeContent := make([]byte, 1024)
	for i := range largeContent {
		largeContent[i] = 'x'
	}
	if err := os.WriteFile(testFile, largeContent, 0644); err != nil {
		t.Fatal(err)
	}

	tool := &FSRead{
		RepoRoot:     tmpDir,
		AllowedRoots: []string{"."},
		DeniedPaths:  []string{},
		MaxReadBytes: 512, // 512 bytes limit
	}

	_, err := tool.Execute(context.Background(), map[string]string{"path": "large.txt"})
	if err == nil {
		t.Fatal("expected error for file exceeding size limit")
	}
	if !contains(err.Error(), "exceeds maximum read size") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestFSRead_WithinSizeLimit(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "small.txt")
	if err := os.WriteFile(testFile, []byte("small content"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &FSRead{
		RepoRoot:     tmpDir,
		AllowedRoots: []string{"."},
		DeniedPaths:  []string{},
		MaxReadBytes: 1024,
	}

	result, err := tool.Execute(context.Background(), map[string]string{"path": "small.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "small content" {
		t.Fatalf("unexpected content: %q", result)
	}
}

func TestFSRead_DeniedPath(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file in a denied subdirectory
	secretDir := filepath.Join(tmpDir, "secret")
	if err := os.MkdirAll(secretDir, 0755); err != nil {
		t.Fatal(err)
	}
	secretFile := filepath.Join(secretDir, "password.txt")
	if err := os.WriteFile(secretFile, []byte("hunter2"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &FSRead{
		RepoRoot:     tmpDir,
		AllowedRoots: []string{"."},
		DeniedPaths:  []string{"secret"},
	}

	_, err := tool.Execute(context.Background(), map[string]string{"path": "secret/password.txt"})
	if err == nil {
		t.Error("expected error for denied path")
	}
}

func TestFSRead_Directory(t *testing.T) {
	tmpDir := t.TempDir()

	tool := &FSRead{
		RepoRoot:     tmpDir,
		AllowedRoots: []string{"."},
		DeniedPaths:  []string{},
	}

	_, err := tool.Execute(context.Background(), map[string]string{"path": "."})
	if err == nil {
		t.Error("expected error when reading directory")
	}
}

func TestFSRead_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()

	tool := &FSRead{
		RepoRoot:     tmpDir,
		AllowedRoots: []string{"."},
		DeniedPaths:  []string{},
	}

	_, err := tool.Execute(context.Background(), map[string]string{"path": "nonexistent.txt"})
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestFSWrite_Success(t *testing.T) {
	tmpDir := t.TempDir()

	tool := &FSWrite{
		RepoRoot:     tmpDir,
		AllowedRoots: []string{"."},
		DeniedPaths:  []string{},
	}

	content := "new content"
	result, err := tool.Execute(context.Background(), map[string]string{
		"path":    "output.txt",
		"content": content,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}

	// Verify file was written
	written, err := os.ReadFile(filepath.Join(tmpDir, "output.txt"))
	if err != nil {
		t.Fatalf("file not written: %v", err)
	}
	if string(written) != content {
		t.Errorf("expected %q, got %q", content, string(written))
	}

	info, err := os.Stat(filepath.Join(tmpDir, "output.txt"))
	if err != nil {
		t.Fatalf("stat written file: %v", err)
	}
	if perms := info.Mode().Perm(); perms != 0o600 {
		t.Fatalf("expected written file mode 0600, got %o", perms)
	}
}

func TestUniqueJournalEntryFileNameUniqueness(t *testing.T) {
	a := UniqueJournalEntryFileName(time.Unix(1_700_000_000, 100))
	b := UniqueJournalEntryFileName(time.Unix(1_700_000_000, 200))
	if a == b {
		t.Fatalf("expected distinct filenames, got %q and %q", a, b)
	}
}

func TestJournalTools_WriteListAndRead(t *testing.T) {
	tmpDir := t.TempDir()

	journalWriter := &JournalWrite{Root: tmpDir}
	writeResult, err := journalWriter.Execute(context.Background(), map[string]string{
		"title": "Morning check-in",
		"body":  "I am settling into Haven and taking stock of the workspace.",
	})
	if err != nil {
		t.Fatalf("write journal entry: %v", err)
	}
	if !strings.Contains(writeResult, "research/journal/") {
		t.Fatalf("unexpected journal write result: %s", writeResult)
	}
	savedTo, ok := strings.CutPrefix(writeResult, "Journal entry saved to ")
	if !ok {
		t.Fatalf("unexpected journal write result prefix: %s", writeResult)
	}
	firstFile := filepath.Base(strings.TrimSpace(savedTo))
	if !strings.HasSuffix(firstFile, ".md") {
		t.Fatalf("expected .md in write result path, got %q", writeResult)
	}

	writeResult2, err := journalWriter.Execute(context.Background(), map[string]string{
		"body": "Second entry same session.",
	})
	if err != nil {
		t.Fatalf("write second journal entry: %v", err)
	}
	if writeResult2 == writeResult {
		t.Fatalf("expected distinct write results, got %q", writeResult)
	}

	journalLister := &JournalList{Root: tmpDir}
	listResult, err := journalLister.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("list journal entries: %v", err)
	}
	if !strings.Contains(listResult, firstFile) {
		t.Fatalf("expected listed journal entry %s, got %s", firstFile, listResult)
	}
	entryLines := 0
	for _, line := range strings.Split(listResult, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "research/journal/") && strings.Contains(trimmed, ".md") {
			entryLines++
		}
	}
	if entryLines < 2 {
		t.Fatalf("expected at least 2 journal files in list, got %q", listResult)
	}

	journalReader := &JournalRead{Root: tmpDir}
	readResult, err := journalReader.Execute(context.Background(), map[string]string{"path": "research/journal/" + firstFile})
	if err != nil {
		t.Fatalf("read journal entry: %v", err)
	}
	if !strings.Contains(readResult, "Morning check-in") || !strings.Contains(readResult, "settling into Haven") {
		t.Fatalf("unexpected journal content: %s", readResult)
	}
}

func TestNotesTools_WriteListAndRead(t *testing.T) {
	tmpDir := t.TempDir()

	notesWriter := &NotesWrite{Root: tmpDir}
	writeResult, err := notesWriter.Execute(context.Background(), map[string]string{
		"title": "Downloads Cleanup",
		"body":  "# Downloads Cleanup\nGroup screenshots first.\n",
	})
	if err != nil {
		t.Fatalf("write note: %v", err)
	}
	if !strings.Contains(writeResult, "research/notes/downloads-cleanup.md") {
		t.Fatalf("unexpected notes write result: %s", writeResult)
	}

	notesLister := &NotesList{Root: tmpDir}
	listResult, err := notesLister.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("list working notes: %v", err)
	}
	if !strings.Contains(listResult, "downloads-cleanup.md") {
		t.Fatalf("expected listed note entry, got %s", listResult)
	}

	notesReader := &NotesRead{Root: tmpDir}
	readResult, err := notesReader.Execute(context.Background(), map[string]string{"path": "downloads-cleanup"})
	if err != nil {
		t.Fatalf("read note: %v", err)
	}
	if !strings.Contains(readResult, "Downloads Cleanup") || !strings.Contains(readResult, "Group screenshots") {
		t.Fatalf("unexpected note content: %s", readResult)
	}
}

func TestPaintTools_SaveAndList(t *testing.T) {
	tmpDir := t.TempDir()

	paintSaver := &PaintSave{Root: tmpDir}
	saveResult, err := paintSaver.Execute(context.Background(), map[string]string{
		"title":      `Quiet <script>alert("x")</script> Orbit`,
		"background": "#F6F1E7",
		"strokes_json": `[
			{"color":"#8E6C4B","width":6,"points":[{"x":120,"y":160},{"x":320,"y":190},{"x":480,"y":120}]},
			{"color":"#D4622A","width":10,"points":[{"x":640,"y":260}]}
		]`,
	})
	if err != nil {
		t.Fatalf("save painting: %v", err)
	}
	if !strings.Contains(saveResult, "artifacts/paintings/") {
		t.Fatalf("unexpected paint save result: %s", saveResult)
	}

	paintDirectoryPath := filepath.Join(tmpDir, filepath.FromSlash(paintDirectoryRelativePath))
	directoryEntries, err := os.ReadDir(paintDirectoryPath)
	if err != nil {
		t.Fatalf("read paint directory: %v", err)
	}
	if len(directoryEntries) != 1 {
		t.Fatalf("expected one saved painting, got %d", len(directoryEntries))
	}
	paintBytes, err := os.ReadFile(filepath.Join(paintDirectoryPath, directoryEntries[0].Name()))
	if err != nil {
		t.Fatalf("read saved painting: %v", err)
	}
	paintContent := string(paintBytes)
	if !strings.Contains(paintContent, `<path d="M 120 160 L 320 190 L 480 120"`) {
		t.Fatalf("expected stroke path in saved svg, got %s", paintContent)
	}
	if !strings.Contains(paintContent, `<circle cx="640" cy="260" r="5" fill="#D4622A"/>`) {
		t.Fatalf("expected single-point stroke circle in saved svg, got %s", paintContent)
	}
	if strings.Contains(paintContent, `<script>alert("x")</script>`) {
		t.Fatalf("expected title to be escaped in svg, got %s", paintContent)
	}
	if !strings.Contains(paintContent, `&lt;script&gt;`) {
		t.Fatalf("expected escaped title markup in svg, got %s", paintContent)
	}

	paintLister := &PaintList{Root: tmpDir}
	listResult, err := paintLister.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("list paintings: %v", err)
	}
	if !strings.Contains(listResult, ".svg") {
		t.Fatalf("expected svg entry in gallery listing, got %s", listResult)
	}
}

func TestPaintSave_RejectsInvalidStrokePayload(t *testing.T) {
	tmpDir := t.TempDir()

	paintSaver := &PaintSave{Root: tmpDir}
	_, err := paintSaver.Execute(context.Background(), map[string]string{
		"title":        "Out of Bounds",
		"strokes_json": `[{"color":"#8E6C4B","width":6,"points":[{"x":-10,"y":40}]}]`,
	})
	if err == nil {
		t.Fatal("expected invalid stroke payload to be rejected")
	}
	if !strings.Contains(err.Error(), "outside the canvas") {
		t.Fatalf("unexpected error: %v", err)
	}

	paintDirectoryPath := filepath.Join(tmpDir, filepath.FromSlash(paintDirectoryRelativePath))
	directoryEntries, readErr := os.ReadDir(paintDirectoryPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("read paint directory: %v", readErr)
	}
	if len(directoryEntries) != 0 {
		t.Fatalf("expected no saved paintings, got %d", len(directoryEntries))
	}
}

func TestPaintSave_RejectsUnknownStrokeFields(t *testing.T) {
	paintSaver := &PaintSave{Root: t.TempDir()}
	_, err := paintSaver.Execute(context.Background(), map[string]string{
		"title":        "Unknown Field",
		"strokes_json": `[{"color":"#8E6C4B","width":6,"points":[{"x":12,"y":24}],"opacity":0.5}]`,
	})
	if err == nil {
		t.Fatal("expected unknown stroke fields to be rejected")
	}
	if !strings.Contains(err.Error(), "invalid strokes_json") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPaintSave_RejectsTrailingStrokeJSONContent(t *testing.T) {
	paintSaver := &PaintSave{Root: t.TempDir()}
	_, err := paintSaver.Execute(context.Background(), map[string]string{
		"title":        "Trailing Content",
		"strokes_json": `[{"color":"#8E6C4B","width":6,"points":[{"x":12,"y":24}]}]oops`,
	})
	if err == nil {
		t.Fatal("expected trailing stroke JSON content to be rejected")
	}
	if !strings.Contains(err.Error(), "invalid strokes_json") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestShellExec_DoesNotInheritAmbientSecrets(t *testing.T) {
	t.Setenv("MORPH_SECRET_TEST", "super-secret-value")

	tmpDir := t.TempDir()
	tool := &ShellExec{WorkDir: tmpDir}
	result, err := tool.Execute(context.Background(), map[string]string{
		"command": `printf 'secret=%s home=%s' "${MORPH_SECRET_TEST}" "${HOME}"`,
	})
	if err != nil {
		t.Fatalf("execute shell command: %v", err)
	}
	if strings.Contains(result, "super-secret-value") {
		t.Fatalf("expected ambient secret env var to be absent, got %q", result)
	}
	if !strings.Contains(result, "home="+tmpDir) {
		t.Fatalf("expected HOME to be sandbox workdir %q, got %q", tmpDir, result)
	}
}

func TestNoteCreate_PersistsDeskNoteState(t *testing.T) {
	tmpDir := t.TempDir()

	noteCreator := &NoteCreate{StateDir: tmpDir}
	result, err := noteCreator.Execute(context.Background(), map[string]string{
		"kind":  "reminder",
		"title": "Gym bag",
		"body":  "Don't forget your shoes and your shaker bottle.",
	})
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if !strings.Contains(result, "Sticky note created") {
		t.Fatalf("unexpected note create result: %s", result)
	}

	rawStateBytes, err := os.ReadFile(filepath.Join(tmpDir, "haven_desk_notes.json"))
	if err != nil {
		t.Fatalf("read desk note state: %v", err)
	}

	var stateFile persistedDeskNoteStateFile
	if err := json.Unmarshal(rawStateBytes, &stateFile); err != nil {
		t.Fatalf("decode desk note state: %v", err)
	}
	if len(stateFile.Notes) != 1 {
		t.Fatalf("expected one persisted note, got %d", len(stateFile.Notes))
	}
	if stateFile.Notes[0].Title != "Gym bag" {
		t.Fatalf("unexpected note title: %s", stateFile.Notes[0].Title)
	}
}

func TestTodoTools_ExposeSchemasAndRequireLoopgate(t *testing.T) {
	todoAdder := &TodoAdd{}
	if todoAdder.Name() != "todo.add" {
		t.Fatalf("unexpected todo add name %q", todoAdder.Name())
	}
	if todoAdder.Operation() != OpWrite {
		t.Fatalf("unexpected todo add operation %q", todoAdder.Operation())
	}
	if _, err := todoAdder.Execute(context.Background(), map[string]string{"text": "Pack the gym bag"}); err == nil {
		t.Fatal("expected todo.add to require loopgate execution")
	}

	todoCompleter := &TodoComplete{}
	if todoCompleter.Name() != "todo.complete" {
		t.Fatalf("unexpected todo complete name %q", todoCompleter.Name())
	}
	if _, err := todoCompleter.Execute(context.Background(), map[string]string{"item_id": "todo_123"}); err == nil {
		t.Fatal("expected todo.complete to require loopgate execution")
	}

	todoLister := &TodoList{}
	if todoLister.Name() != "todo.list" {
		t.Fatalf("unexpected todo list name %q", todoLister.Name())
	}
	if todoLister.Operation() != OpRead {
		t.Fatalf("unexpected todo list operation %q", todoLister.Operation())
	}
	if _, err := todoLister.Execute(context.Background(), nil); err == nil {
		t.Fatal("expected todo.list to require loopgate execution")
	}
}

// TestFSWrite_WritesToExistingParent confirms that writing a new file inside an
// already-existing parent directory is allowed. SafePath requires the parent to
// exist and resolve before the write is permitted.
func TestFSWrite_WritesToExistingParent(t *testing.T) {
	tmpDir := t.TempDir()

	// Parent directory must exist before the write.
	if err := os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	tool := &FSWrite{
		RepoRoot:     tmpDir,
		AllowedRoots: []string{"."},
		DeniedPaths:  []string{},
	}

	_, err := tool.Execute(context.Background(), map[string]string{
		"path":    "subdir/file.txt",
		"content": "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, "subdir", "file.txt")); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

// TestFSWrite_DeniesWhenParentMissing confirms that writing to a path whose
// parent directory does not exist is denied. We cannot resolve the parent, so
// we cannot prove the path is within the allowed root.
func TestFSWrite_DeniesWhenParentMissing(t *testing.T) {
	tmpDir := t.TempDir()

	tool := &FSWrite{
		RepoRoot:     tmpDir,
		AllowedRoots: []string{"."},
		DeniedPaths:  []string{},
	}

	_, err := tool.Execute(context.Background(), map[string]string{
		"path":    "ghost/subdir/file.txt",
		"content": "should not be written",
	})
	if err == nil {
		t.Fatal("expected deny when parent directory does not exist, got allow")
	}
}

func TestFSWrite_DeniedPath(t *testing.T) {
	tmpDir := t.TempDir()

	tool := &FSWrite{
		RepoRoot:     tmpDir,
		AllowedRoots: []string{"."},
		DeniedPaths:  []string{"protected"},
	}

	_, err := tool.Execute(context.Background(), map[string]string{
		"path":    "protected/secret.txt",
		"content": "should not write",
	})
	if err == nil {
		t.Error("expected error for denied path")
	}
}

func TestOpenFileNoFollowForWrite_DeniesSymlinkTarget(t *testing.T) {
	tmpDir := t.TempDir()
	realPath := filepath.Join(tmpDir, "real.txt")
	linkPath := filepath.Join(tmpDir, "link.txt")

	if err := os.WriteFile(realPath, []byte("real"), 0o600); err != nil {
		t.Fatalf("write real path: %v", err)
	}
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	validatedPath, err := resolveValidatedPath(tmpDir, []string{"."}, nil, "link.txt")
	if err != nil {
		t.Fatalf("resolve validated path: %v", err)
	}
	fileHandle, err := openFileNoFollowForWrite(validatedPath)
	if err == nil {
		_ = fileHandle.Close()
		t.Fatal("expected symlink target write-open to be denied")
	}
}

func TestFSList_Success(t *testing.T) {
	tmpDir := t.TempDir()

	// Create some files and a directory
	if err := os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}

	tool := &FSList{
		RepoRoot:     tmpDir,
		AllowedRoots: []string{"."},
		DeniedPaths:  []string{},
	}

	result, err := tool.Execute(context.Background(), map[string]string{"path": "."})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should contain files and directory (with trailing slash)
	if !contains(result, "file1.txt") {
		t.Error("expected file1.txt in output")
	}
	if !contains(result, "file2.txt") {
		t.Error("expected file2.txt in output")
	}
	if !contains(result, "subdir/") {
		t.Error("expected subdir/ in output")
	}
}

func TestRegistryTryRegisterRejectsDuplicate(t *testing.T) {
	registry := NewRegistry()
	firstTool := &FSRead{RepoRoot: t.TempDir()}
	if err := registry.TryRegister(firstTool); err != nil {
		t.Fatalf("register first tool: %v", err)
	}
	if err := registry.TryRegister(firstTool); err == nil {
		t.Fatal("expected duplicate registry entry to be rejected")
	}
}

func TestFSList_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	tool := &FSList{
		RepoRoot:     tmpDir,
		AllowedRoots: []string{"."},
		DeniedPaths:  []string{},
	}

	result, err := tool.Execute(context.Background(), map[string]string{"path": "."})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "(empty directory)" {
		t.Errorf("expected empty directory message, got %q", result)
	}
}

func TestFSList_NotDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "file.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &FSList{
		RepoRoot:     tmpDir,
		AllowedRoots: []string{"."},
		DeniedPaths:  []string{},
	}

	_, err := tool.Execute(context.Background(), map[string]string{"path": "file.txt"})
	if err == nil {
		t.Error("expected error when listing a file")
	}
}

func TestFSList_DeniedPath(t *testing.T) {
	tmpDir := t.TempDir()
	secretDir := filepath.Join(tmpDir, "secret")
	if err := os.MkdirAll(secretDir, 0755); err != nil {
		t.Fatal(err)
	}

	tool := &FSList{
		RepoRoot:     tmpDir,
		AllowedRoots: []string{"."},
		DeniedPaths:  []string{"secret"},
	}

	_, err := tool.Execute(context.Background(), map[string]string{"path": "secret"})
	if err == nil {
		t.Error("expected error for denied path")
	}
}

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry()

	tool := &FSRead{RepoRoot: "/tmp"}
	r.Register(tool)

	if !r.Has("fs_read") {
		t.Error("expected fs_read to be registered")
	}
	if r.Get("fs_read") != tool {
		t.Error("expected to get same tool back")
	}
}

func TestRegistry_List(t *testing.T) {
	r := NewRegistry()

	r.Register(&FSRead{RepoRoot: "/tmp"})
	r.Register(&FSWrite{RepoRoot: "/tmp"})
	r.Register(&FSList{RepoRoot: "/tmp"})

	names := r.List()
	if len(names) != 3 {
		t.Errorf("expected 3 tools, got %d", len(names))
	}

	// Should be sorted
	if names[0] != "fs_list" || names[1] != "fs_read" || names[2] != "fs_write" {
		t.Errorf("unexpected order: %v", names)
	}
}

func TestSchema_Validate(t *testing.T) {
	schema := Schema{
		Args: []ArgDef{
			{Name: "required_arg", Required: true},
			{Name: "optional_arg", Required: false},
		},
	}

	// Should fail with missing required arg
	err := schema.Validate(map[string]string{})
	if err == nil {
		t.Error("expected error for missing required arg")
	}

	// Should pass with required arg present
	err = schema.Validate(map[string]string{"required_arg": "value"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Should pass with both args
	err = schema.Validate(map[string]string{"required_arg": "v1", "optional_arg": "v2"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
