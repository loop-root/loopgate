package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestShellExec_DoesNotInheritAmbientSecrets(t *testing.T) {
	t.Setenv("MORPH_SECRET_TEST", "super-secret-value")

	tmpDir := t.TempDir()
	tool := &ShellExec{
		WorkDir:         tmpDir,
		AllowedCommands: []string{"env"},
	}
	result, err := tool.Execute(context.Background(), map[string]string{
		"command": "env",
	})
	if err != nil {
		t.Fatalf("execute shell command: %v", err)
	}
	if strings.Contains(result, "MORPH_SECRET_TEST=") || strings.Contains(result, "super-secret-value") {
		t.Fatalf("expected ambient secret env var to be absent, got %q", result)
	}
	if !strings.Contains(result, "HOME="+tmpDir) {
		t.Fatalf("expected HOME to be sandbox workdir %q, got %q", tmpDir, result)
	}
}

func TestShellExec_EmptyWorkDirDoesNotExposeAmbientHome(t *testing.T) {
	t.Setenv("HOME", filepath.Join(t.TempDir(), "real-home"))

	tool := &ShellExec{
		AllowedCommands: []string{"env"},
	}
	result, err := tool.Execute(context.Background(), map[string]string{
		"command": "env",
	})
	if err != nil {
		t.Fatalf("execute shell command: %v", err)
	}
	if strings.Contains(result, "HOME="+os.Getenv("HOME")) {
		t.Fatalf("expected ambient HOME to be absent, got %q", result)
	}
	if !strings.Contains(result, "HOME="+emptyShellHome) {
		t.Fatalf("expected HOME to use empty shell home %q, got %q", emptyShellHome, result)
	}
}

func TestShellExec_RejectsCommandOutsidePolicyAllowlist(t *testing.T) {
	tool := &ShellExec{
		WorkDir:         t.TempDir(),
		AllowedCommands: []string{"git"},
	}
	_, err := tool.Execute(context.Background(), map[string]string{
		"command": "env",
	})
	if err == nil || !strings.Contains(err.Error(), "not allowed by policy") {
		t.Fatalf("expected allowlist denial, got %v", err)
	}
}

func TestShellExec_RejectsShellControlOperators(t *testing.T) {
	tool := &ShellExec{
		WorkDir:         t.TempDir(),
		AllowedCommands: []string{"echo"},
	}
	_, err := tool.Execute(context.Background(), map[string]string{
		"command": "echo hi | cat",
	})
	if err == nil || !strings.Contains(err.Error(), "control operators") {
		t.Fatalf("expected direct-command denial, got %v", err)
	}
}

func TestShellExec_UsesHermeticPathForBareAllowedCommand(t *testing.T) {
	tmpDir := t.TempDir()
	attackerBinDir := filepath.Join(tmpDir, "attacker-bin")
	if err := os.MkdirAll(attackerBinDir, 0o755); err != nil {
		t.Fatalf("mkdir attacker bin: %v", err)
	}
	attackerCommandPath := filepath.Join(attackerBinDir, "env")
	if err := os.WriteFile(attackerCommandPath, []byte("#!/bin/sh\necho attacker-shadowed-env\n"), 0o755); err != nil {
		t.Fatalf("write attacker command: %v", err)
	}
	t.Setenv("PATH", attackerBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	tool := &ShellExec{
		WorkDir:         tmpDir,
		AllowedCommands: []string{"env"},
	}
	result, err := tool.Execute(context.Background(), map[string]string{
		"command": "env",
	})
	if err != nil {
		t.Fatalf("execute shell command: %v", err)
	}
	if strings.Contains(result, "attacker-shadowed-env") {
		t.Fatalf("expected bare command to ignore ambient PATH shadowing, got %q", result)
	}
	if !strings.Contains(result, "HOME="+tmpDir) {
		t.Fatalf("expected HOME to be sandbox workdir %q, got %q", tmpDir, result)
	}
}

func TestShellExec_AllowsExactExecutablePath(t *testing.T) {
	tmpDir := t.TempDir()
	exactCommandPath := filepath.Join(tmpDir, "print-tool")
	if err := os.WriteFile(exactCommandPath, []byte("#!/bin/sh\necho exact-path-ok\n"), 0o755); err != nil {
		t.Fatalf("write exact-path command: %v", err)
	}

	tool := &ShellExec{
		WorkDir:         tmpDir,
		AllowedCommands: []string{exactCommandPath},
	}
	result, err := tool.Execute(context.Background(), map[string]string{
		"command": exactCommandPath,
	})
	if err != nil {
		t.Fatalf("execute exact-path shell command: %v", err)
	}
	if !strings.Contains(result, "exact-path-ok") {
		t.Fatalf("expected exact-path command output, got %q", result)
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
	if err := r.Register(tool); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	if !r.Has("fs_read") {
		t.Error("expected fs_read to be registered")
	}
	if r.Get("fs_read") != tool {
		t.Error("expected to get same tool back")
	}
}

func TestRegistry_List(t *testing.T) {
	r := NewRegistry()

	if err := r.Register(&FSRead{RepoRoot: "/tmp"}); err != nil {
		t.Fatalf("register fs_read: %v", err)
	}
	if err := r.Register(&FSWrite{RepoRoot: "/tmp"}); err != nil {
		t.Fatalf("register fs_write: %v", err)
	}
	if err := r.Register(&FSList{RepoRoot: "/tmp"}); err != nil {
		t.Fatalf("register fs_list: %v", err)
	}

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
