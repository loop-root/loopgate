package hostaccess

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeRelativePathRejectsEscape(t *testing.T) {
	_, err := NormalizeRelativePath("../secret.txt")
	if err == nil {
		t.Fatal("expected escaped relative path to fail")
	}
	if !IsPathPolicyError(err) {
		t.Fatalf("expected path policy error, got %T %v", err, err)
	}
}

func TestOpenPathReadOnlyRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	targetDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(targetDir, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("write target file: %v", err)
	}
	if err := os.Symlink(filepath.Join(targetDir, "secret.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	fileHandle, _, err := OpenPathReadOnly(root, "link.txt", false)
	if fileHandle != nil {
		_ = fileHandle.Close()
	}
	if err == nil {
		t.Fatal("expected symlink open to fail")
	}
	if !IsPathPolicyError(err) {
		t.Fatalf("expected path policy error, got %T %v", err, err)
	}
}

func TestEnsureDirectoryUnderRootCreatesNestedDirectory(t *testing.T) {
	root := t.TempDir()
	normalizedPath, err := EnsureDirectoryUnderRoot(root, "a/b/c", 0o755)
	if err != nil {
		t.Fatalf("ensure directory: %v", err)
	}
	if normalizedPath.Display != "a/b/c" {
		t.Fatalf("expected normalized display path a/b/c, got %q", normalizedPath.Display)
	}
	fileInfo, err := os.Stat(filepath.Join(root, "a", "b", "c"))
	if err != nil {
		t.Fatalf("stat created directory: %v", err)
	}
	if !fileInfo.IsDir() {
		t.Fatal("expected created path to be a directory")
	}
}

func TestLstatPathUnderRootReportsMissingLeaf(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "parent"), 0o755); err != nil {
		t.Fatalf("create parent: %v", err)
	}

	normalizedPath, _, exists, err := LstatPathUnderRoot(root, "parent/missing.txt")
	if err != nil {
		t.Fatalf("lstat missing leaf: %v", err)
	}
	if exists {
		t.Fatal("expected missing leaf to report exists=false")
	}
	if normalizedPath.Display != "parent/missing.txt" {
		t.Fatalf("unexpected normalized path %q", normalizedPath.Display)
	}
}

func TestOpenPathReadOnlyMissingRootReturnsPlainError(t *testing.T) {
	_, _, err := OpenPathReadOnly(filepath.Join(t.TempDir(), "missing"), "file.txt", false)
	if err == nil {
		t.Fatal("expected missing root to fail")
	}
	if IsPathPolicyError(err) {
		t.Fatalf("expected missing root to remain an execution error, got %v", err)
	}
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	t.Fatalf("expected missing-root error to wrap os.ErrNotExist, got %v", err)
}
