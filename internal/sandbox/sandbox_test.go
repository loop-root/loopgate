package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestResolveHomePathAcceptsVirtualPath(t *testing.T) {
	repoRoot := t.TempDir()
	paths := PathsForRepo(repoRoot)
	if err := paths.Ensure(); err != nil {
		t.Fatalf("ensure sandbox paths: %v", err)
	}

	importedPath := filepath.Join(paths.Imports, "notes.txt")
	if err := os.WriteFile(importedPath, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write imported path: %v", err)
	}
	canonicalImportedPath, err := filepath.EvalSymlinks(importedPath)
	if err != nil {
		t.Fatalf("eval imported path symlinks: %v", err)
	}

	resolvedPath, sandboxRelativePath, err := paths.ResolveHomePath("/loopgate/home/imports/notes.txt")
	if err != nil {
		t.Fatalf("resolve virtual sandbox path: %v", err)
	}
	if sandboxRelativePath != "imports/notes.txt" {
		t.Fatalf("expected relative sandbox path imports/notes.txt, got %q", sandboxRelativePath)
	}
	if resolvedPath != canonicalImportedPath {
		t.Fatalf("expected resolved path %q, got %q", canonicalImportedPath, resolvedPath)
	}
}

func TestResolveHomePathRejectsVirtualPathOutsideHome(t *testing.T) {
	repoRoot := t.TempDir()
	paths := PathsForRepo(repoRoot)
	if err := paths.Ensure(); err != nil {
		t.Fatalf("ensure sandbox paths: %v", err)
	}

	if _, _, err := paths.ResolveHomePath("/loopgate/state/secrets.json"); err == nil {
		t.Fatal("expected virtual path outside /loopgate/home to be rejected")
	}
}

func TestNormalizeHomePathNormalizesUnicodeBeforeTraversalChecks(t *testing.T) {
	normalizedPath, err := NormalizeHomePath("imports/e\u0301vidence.txt")
	if err != nil {
		t.Fatalf("normalize unicode path: %v", err)
	}
	if normalizedPath != "imports/\u00e9vidence.txt" {
		t.Fatalf("expected NFC-normalized path, got %q", normalizedPath)
	}
}

func TestVirtualizeRelativeHomePath(t *testing.T) {
	if virtualPath := VirtualizeRelativeHomePath("outputs/staged.txt"); virtualPath != "/loopgate/home/outputs/staged.txt" {
		t.Fatalf("unexpected virtualized path: %q", virtualPath)
	}
}

func TestCopyPathAtomicCopiesRegularFile(t *testing.T) {
	sourceDirectory := t.TempDir()
	destinationDirectory := t.TempDir()

	sourcePath := filepath.Join(sourceDirectory, "notes.txt")
	if err := os.WriteFile(sourcePath, []byte("sandbox copy"), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	destinationPath := filepath.Join(destinationDirectory, "notes-copy.txt")
	entryType, err := CopyPathAtomic(sourcePath, destinationPath)
	if err != nil {
		t.Fatalf("copy regular file: %v", err)
	}
	if entryType != "file" {
		t.Fatalf("expected file entry type, got %q", entryType)
	}

	copiedBytes, err := os.ReadFile(destinationPath)
	if err != nil {
		t.Fatalf("read copied file: %v", err)
	}
	if string(copiedBytes) != "sandbox copy" {
		t.Fatalf("expected copied content %q, got %q", "sandbox copy", string(copiedBytes))
	}
}

func TestCopyPathAtomicCopiesDirectoryTree(t *testing.T) {
	sourceDirectory := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(filepath.Join(sourceDirectory, "nested"), 0o700); err != nil {
		t.Fatalf("create source directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDirectory, "root.txt"), []byte("root"), 0o600); err != nil {
		t.Fatalf("write root file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDirectory, "nested", "child.txt"), []byte("child"), 0o600); err != nil {
		t.Fatalf("write nested file: %v", err)
	}

	destinationPath := filepath.Join(t.TempDir(), "copied")
	entryType, err := CopyPathAtomic(sourceDirectory, destinationPath)
	if err != nil {
		t.Fatalf("copy directory: %v", err)
	}
	if entryType != "directory" {
		t.Fatalf("expected directory entry type, got %q", entryType)
	}

	rootBytes, err := os.ReadFile(filepath.Join(destinationPath, "root.txt"))
	if err != nil {
		t.Fatalf("read copied root file: %v", err)
	}
	if string(rootBytes) != "root" {
		t.Fatalf("expected root file content %q, got %q", "root", string(rootBytes))
	}

	childBytes, err := os.ReadFile(filepath.Join(destinationPath, "nested", "child.txt"))
	if err != nil {
		t.Fatalf("read copied child file: %v", err)
	}
	if string(childBytes) != "child" {
		t.Fatalf("expected child file content %q, got %q", "child", string(childBytes))
	}
}

func TestCopyPathAtomicRejectsNestedSymlink(t *testing.T) {
	sourceDirectory := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(sourceDirectory, 0o700); err != nil {
		t.Fatalf("create source directory: %v", err)
	}

	outsidePath := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outsidePath, []byte("outside"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outsidePath, filepath.Join(sourceDirectory, "escape.txt")); err != nil {
		t.Fatalf("create nested symlink: %v", err)
	}

	destinationPath := filepath.Join(t.TempDir(), "copied")
	_, err := CopyPathAtomic(sourceDirectory, destinationPath)
	if !errors.Is(err, ErrSymlinkNotAllowed) {
		t.Fatalf("expected nested symlink denial, got %v", err)
	}
	if _, statErr := os.Stat(destinationPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected denied copy to leave destination absent, got err=%v", statErr)
	}
}

func TestCopyPathAtomicRejectsUnsupportedSourceEntryType(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "named-pipe")
	if err := unix.Mkfifo(sourcePath, 0o600); err != nil {
		t.Fatalf("mkfifo source path: %v", err)
	}

	destinationPath := filepath.Join(t.TempDir(), "copied")
	_, err := CopyPathAtomic(sourcePath, destinationPath)
	if !errors.Is(err, ErrSandboxPathInvalid) {
		t.Fatalf("expected unsupported entry denial, got %v", err)
	}
	if _, statErr := os.Stat(destinationPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected denied copy to leave destination absent, got err=%v", statErr)
	}
}

func TestMirrorPathAtomicReplacesExistingDirectoryTree(t *testing.T) {
	sourceDirectory := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(filepath.Join(sourceDirectory, "nested"), 0o700); err != nil {
		t.Fatalf("create source directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDirectory, "nested", "child.txt"), []byte("new child"), 0o600); err != nil {
		t.Fatalf("write source child: %v", err)
	}

	destinationDirectory := filepath.Join(t.TempDir(), "destination")
	if err := os.MkdirAll(filepath.Join(destinationDirectory, "old"), 0o700); err != nil {
		t.Fatalf("create destination directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(destinationDirectory, "old", "stale.txt"), []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale destination file: %v", err)
	}

	entryType, err := MirrorPathAtomic(sourceDirectory, destinationDirectory)
	if err != nil {
		t.Fatalf("mirror directory: %v", err)
	}
	if entryType != "directory" {
		t.Fatalf("expected directory entry type, got %q", entryType)
	}

	if _, err := os.Stat(filepath.Join(destinationDirectory, "old", "stale.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected stale destination content to be removed, got %v", err)
	}

	childBytes, err := os.ReadFile(filepath.Join(destinationDirectory, "nested", "child.txt"))
	if err != nil {
		t.Fatalf("read mirrored child file: %v", err)
	}
	if string(childBytes) != "new child" {
		t.Fatalf("expected mirrored child content %q, got %q", "new child", string(childBytes))
	}
}

func TestMirrorPathAtomicWithFinalizeRollsBackOnFinalizeFailure(t *testing.T) {
	sourceDirectory := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(filepath.Join(sourceDirectory, "nested"), 0o700); err != nil {
		t.Fatalf("create source directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDirectory, "nested", "child.txt"), []byte("new child"), 0o600); err != nil {
		t.Fatalf("write source child: %v", err)
	}

	destinationDirectory := filepath.Join(t.TempDir(), "destination")
	if err := os.MkdirAll(filepath.Join(destinationDirectory, "old"), 0o700); err != nil {
		t.Fatalf("create destination directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(destinationDirectory, "old", "stale.txt"), []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale destination file: %v", err)
	}

	finalizeErr := errors.New("audit unavailable")
	_, err := MirrorPathAtomicWithFinalize(sourceDirectory, destinationDirectory, func(string) error {
		return finalizeErr
	})
	if !errors.Is(err, finalizeErr) {
		t.Fatalf("expected finalize error %v, got %v", finalizeErr, err)
	}

	staleBytes, err := os.ReadFile(filepath.Join(destinationDirectory, "old", "stale.txt"))
	if err != nil {
		t.Fatalf("read restored stale file: %v", err)
	}
	if string(staleBytes) != "stale" {
		t.Fatalf("expected restored stale content %q, got %q", "stale", string(staleBytes))
	}

	if _, err := os.Stat(filepath.Join(destinationDirectory, "nested", "child.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected new mirrored content to be rolled back, got %v", err)
	}
}

func TestEnsureWithinRootHonorsDarwinCaseFoldAndUnicodeNormalization(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-specific case-fold and normalization behavior")
	}

	repoRoot := t.TempDir()
	paths := PathsForRepo(repoRoot)
	if err := paths.Ensure(); err != nil {
		t.Fatalf("ensure sandbox paths: %v", err)
	}

	normalizedRoot, err := filepath.EvalSymlinks(paths.Home)
	if err != nil {
		t.Fatalf("resolve sandbox root: %v", err)
	}
	targetPath := filepath.Join(normalizedRoot, "imports", "\u00e9vidence.txt")
	if err := os.WriteFile(targetPath, []byte("ok"), 0o600); err != nil {
		t.Fatalf("write normalized target: %v", err)
	}

	aliasRoot := strings.ToUpper(normalizedRoot)
	aliasTarget := filepath.Join(aliasRoot, "IMPORTS", "e\u0301vidence.txt")
	if err := ensureWithinRoot(aliasRoot, aliasTarget); err != nil {
		t.Fatalf("expected normalized/case-folded path comparison to allow canonical target, got %v", err)
	}
}
