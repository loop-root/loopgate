package safety

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestSafePath_AllowsNormalFileUnderRepo(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "body", "real.txt"), "hello")

	abs, err := SafePath(repo, []string{"."}, []string{"core/memory/ledger"}, "body/real.txt")
	if err != nil {
		t.Fatalf("expected allow, got err: %v", err)
	}
	// On macOS, TempDir paths may be under /var which resolves to /private/var.
	// SafePath returns the resolved target, so canonicalize repo before comparing.
	repoCanon := repo
	if rp, err := filepath.EvalSymlinks(repo); err == nil {
		repoCanon = rp
	}
	if !strings.HasPrefix(abs, repoCanon) {
		t.Fatalf("expected resolved path under repo. got=%q repo=%q repoCanon=%q", abs, repo, repoCanon)
	}
}

func TestSafePath_DeniesTraversal(t *testing.T) {
	repo := t.TempDir()

	_, err := SafePath(repo, []string{"."}, nil, "../etc/passwd")
	if err == nil {
		t.Fatalf("expected deny for traversal, got allow")
	}
}

func TestSafePath_DeniesDeniedPath(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "core", "memory", "ledger", "ledger.jsonl"), "x")

	_, err := SafePath(repo, []string{"."}, []string{"core/memory/ledger"}, "core/memory/ledger/ledger.jsonl")
	if err == nil {
		t.Fatalf("expected deny for denied path, got allow")
	}
}

func TestSafePath_DeniesSymlinkEscape(t *testing.T) {
	// Skip on Windows (symlink creation requires elevated privileges on some setups).
	if runtime.GOOS == "windows" {
		t.Skip("symlink test skipped on windows")
	}

	repo := t.TempDir()

	// Create an external file OUTSIDE the repo.
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	writeFile(t, outsideFile, "nope")

	// Create an in-repo symlink pointing OUTSIDE.
	bodyDir := filepath.Join(repo, "body")
	if err := os.MkdirAll(bodyDir, 0755); err != nil {
		t.Fatalf("mkdir body: %v", err)
	}
	linkPath := filepath.Join(bodyDir, "link")
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Now SafePath should deny because resolved target is outside allowed roots.
	_, err := SafePath(repo, []string{"."}, nil, "body/link")
	if err == nil {
		t.Fatalf("expected deny for symlink escape, got allow")
	}
}

// Additional edge case tests per roadmap

func TestSafePath_DeniesMultipleTraversals(t *testing.T) {
	repo := t.TempDir()

	cases := []string{
		"../../etc/passwd",
		"foo/../../../etc/passwd",
		"./foo/../../bar/../../../etc/passwd",
		"body/tools/../../..",
	}

	for _, path := range cases {
		_, err := SafePath(repo, []string{"."}, nil, path)
		if err == nil {
			t.Errorf("expected deny for %q, got allow", path)
		}
	}
}

func TestSafePath_DeniesEncodedTraversal(t *testing.T) {
	repo := t.TempDir()

	// These are literal strings that could be attempted - Go's filepath.Clean
	// should handle them, but we test to be sure
	cases := []string{
		"..%2f..%2fetc/passwd", // URL-encoded (literal, not decoded)
		"..%252f..%252fetc",    // Double-encoded
		"....//....//etc",      // Extra dots
		"..\\..\\etc\\passwd",  // Windows-style (on Unix, should be treated as literal)
	}

	for _, path := range cases {
		// Most of these will be treated as literal filenames or denied
		// The key is they shouldn't escape
		result, err := SafePath(repo, []string{"."}, nil, path)
		if err == nil {
			// If allowed, verify it's still under repo
			repoCanon := repo
			if rp, rerr := filepath.EvalSymlinks(repo); rerr == nil {
				repoCanon = rp
			}
			if !strings.HasPrefix(result, repoCanon) {
				t.Errorf("path %q resolved outside repo: %q", path, result)
			}
		}
		// If denied, that's also fine
	}
}

func TestSafePath_DeniesEmptyPath(t *testing.T) {
	repo := t.TempDir()

	_, err := SafePath(repo, []string{"."}, nil, "")
	if err == nil {
		t.Fatal("expected deny for empty path")
	}

	_, err = SafePath(repo, []string{"."}, nil, "   ")
	if err == nil {
		t.Fatal("expected deny for whitespace-only path")
	}
}

func TestSafePath_DeniesSymlinkToDeniedPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test skipped on windows")
	}

	repo := t.TempDir()

	// Create a denied directory with a file
	deniedDir := filepath.Join(repo, "core", "memory", "ledger")
	writeFile(t, filepath.Join(deniedDir, "secret.jsonl"), "sensitive")

	// Create a symlink from an allowed location to the denied location
	bodyDir := filepath.Join(repo, "body")
	if err := os.MkdirAll(bodyDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(deniedDir, filepath.Join(bodyDir, "sneaky")); err != nil {
		t.Fatal(err)
	}

	// Try to access via symlink - should be denied
	_, err := SafePath(repo, []string{"."}, []string{"core/memory/ledger"}, "body/sneaky/secret.jsonl")
	if err == nil {
		t.Fatal("expected deny for symlink to denied path")
	}
}

func TestSafePath_DeniesSymlinkChain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test skipped on windows")
	}

	repo := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "secret.txt"), "sensitive")

	// Create a chain: link1 -> link2 -> outside
	bodyDir := filepath.Join(repo, "body")
	if err := os.MkdirAll(bodyDir, 0755); err != nil {
		t.Fatal(err)
	}

	// link2 points outside
	link2 := filepath.Join(bodyDir, "link2")
	if err := os.Symlink(outside, link2); err != nil {
		t.Fatal(err)
	}

	// link1 points to link2
	link1 := filepath.Join(bodyDir, "link1")
	if err := os.Symlink(link2, link1); err != nil {
		t.Fatal(err)
	}

	// Accessing via link1 should still be denied
	_, err := SafePath(repo, []string{"."}, nil, "body/link1/secret.txt")
	if err == nil {
		t.Fatal("expected deny for symlink chain escape")
	}
}

func TestSafePath_AllowsAbsolutePathUnderRoot(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "allowed.txt"), "ok")

	// Resolve repo to canonical form
	repoCanon := repo
	if rp, err := filepath.EvalSymlinks(repo); err == nil {
		repoCanon = rp
	}

	absPath := filepath.Join(repoCanon, "allowed.txt")
	result, err := SafePath(repo, []string{"."}, nil, absPath)
	if err != nil {
		t.Fatalf("expected allow for absolute path under root, got: %v", err)
	}
	if result != absPath {
		t.Errorf("expected %q, got %q", absPath, result)
	}
}

func TestSafePath_DeniesAbsolutePathOutsideRoot(t *testing.T) {
	repo := t.TempDir()

	_, err := SafePath(repo, []string{"."}, nil, "/etc/passwd")
	if err == nil {
		t.Fatal("expected deny for absolute path outside root")
	}
}

func TestSafePath_CaseSensitivity(t *testing.T) {
	repo := t.TempDir()

	// Create a denied path
	deniedDir := filepath.Join(repo, "Secret")
	writeFile(t, filepath.Join(deniedDir, "data.txt"), "sensitive")

	// On case-insensitive filesystems (macOS), these should all be denied
	// On case-sensitive filesystems (Linux), behavior may differ
	cases := []string{
		"Secret/data.txt",
		"secret/data.txt",
		"SECRET/data.txt",
		"sEcReT/data.txt",
	}

	for _, path := range cases {
		_, err := SafePath(repo, []string{"."}, []string{"Secret"}, path)
		// On macOS (case-insensitive), all should be denied
		// On Linux (case-sensitive), only exact match is denied
		if runtime.GOOS == "darwin" && err == nil {
			t.Errorf("expected deny for %q on case-insensitive filesystem", path)
		}
	}
}

func TestSafePath_MultipleAllowedRoots(t *testing.T) {
	repo := t.TempDir()

	// Create multiple allowed directories
	writeFile(t, filepath.Join(repo, "src", "main.go"), "package main")
	writeFile(t, filepath.Join(repo, "docs", "readme.txt"), "readme")
	writeFile(t, filepath.Join(repo, "private", "secret.txt"), "secret")

	// Allow src and docs, deny private
	allowedRoots := []string{"src", "docs"}

	// Should allow src
	_, err := SafePath(repo, allowedRoots, nil, "src/main.go")
	if err != nil {
		t.Errorf("expected allow for src/main.go: %v", err)
	}

	// Should allow docs
	_, err = SafePath(repo, allowedRoots, nil, "docs/readme.txt")
	if err != nil {
		t.Errorf("expected allow for docs/readme.txt: %v", err)
	}

	// Should deny private (not in allowed roots)
	_, err = SafePath(repo, allowedRoots, nil, "private/secret.txt")
	if err == nil {
		t.Error("expected deny for private/secret.txt")
	}
}

func TestSafePath_DenyOverridesAllow(t *testing.T) {
	repo := t.TempDir()

	// Create nested structure
	writeFile(t, filepath.Join(repo, "src", "config", "secrets.txt"), "password123")
	writeFile(t, filepath.Join(repo, "src", "main.go"), "package main")

	// Allow all of src, but deny src/config
	_, err := SafePath(repo, []string{"src"}, []string{"src/config"}, "src/config/secrets.txt")
	if err == nil {
		t.Fatal("expected deny to override allow for nested path")
	}

	// But src/main.go should still be allowed
	_, err = SafePath(repo, []string{"src"}, []string{"src/config"}, "src/main.go")
	if err != nil {
		t.Errorf("expected allow for src/main.go: %v", err)
	}
}

// --- Fail-closed resolution tests ---

// TestSafePath_AllowsWriteToNewFileInExistingDir confirms that a path to a
// not-yet-existing file inside a real, resolved parent directory is permitted.
func TestSafePath_AllowsWriteToNewFileInExistingDir(t *testing.T) {
	repo := t.TempDir()

	// The parent directory exists; the file itself does not yet.
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	result, err := SafePath(repo, []string{"."}, nil, "src/newfile.txt")
	if err != nil {
		t.Fatalf("expected allow for new file in existing dir, got: %v", err)
	}
	repoCanon := repo
	if rp, rerr := filepath.EvalSymlinks(repo); rerr == nil {
		repoCanon = rp
	}
	if !strings.HasPrefix(result, repoCanon) {
		t.Errorf("resolved path %q is outside repo %q", result, repoCanon)
	}
}

// TestSafePath_DeniesWriteWhenParentMissing confirms that a path whose parent
// directory does not exist is denied. We cannot resolve the parent, so we
// cannot prove the path is safe.
func TestSafePath_DeniesWriteWhenParentMissing(t *testing.T) {
	repo := t.TempDir()

	// "ghost" does not exist inside repo.
	_, err := SafePath(repo, []string{"."}, nil, "ghost/subdir/newfile.txt")
	if err == nil {
		t.Fatal("expected deny when parent directory does not exist, got allow")
	}
	if !strings.Contains(err.Error(), "parent_directory_not_resolved") {
		t.Errorf("expected error to mention parent_directory_not_resolved, got: %v", err)
	}
}

// TestSafePath_DeniesDirectorySymlinkEscape confirms that a symlink pointing
// to a directory outside the allowed root cannot be used to access files under
// that directory. The canonical target resolves outside the allowed root.
func TestSafePath_DeniesDirectorySymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test skipped on windows")
	}

	repo := t.TempDir()
	outside := t.TempDir()
	// Simulate /etc/passwd: a file in the outside directory.
	writeFile(t, filepath.Join(outside, "passwd"), "root:x:0:0")

	// Create: repo/allowed/link -> outside
	allowedDir := filepath.Join(repo, "allowed")
	if err := os.MkdirAll(allowedDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(allowedDir, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Accessing allowed/link/passwd should be denied: the resolved path is
	// outside/passwd, which is outside the allowed root.
	_, err := SafePath(repo, []string{"allowed"}, nil, "allowed/link/passwd")
	if err == nil {
		t.Fatal("expected deny for directory symlink escape, got allow")
	}
}

// TestSafePath_DeniesDoubleDotComponent confirms that paths with ".." components
// that would escape the allowed root are denied.
func TestSafePath_DeniesDoubleDotComponent(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cases := []string{
		"src/../..",      // resolves to parent of repo
		"src/../../etc",  // resolves above repo
		"src/../private", // sibling directory outside allowed root "src"
	}
	for _, path := range cases {
		_, err := SafePath(repo, []string{"src"}, nil, path)
		if err == nil {
			t.Errorf("expected deny for %q, got allow", path)
		}
	}
}

// TestValidateBaseName tests the internal basename validator directly.
func TestValidateBaseName(t *testing.T) {
	bad := []string{
		"",
		".",
		"..",
		"a/b",
		"foo/bar",
	}
	for _, name := range bad {
		if err := validateBaseName(name); err == nil {
			t.Errorf("expected error for basename %q, got nil", name)
		}
	}

	good := []string{
		"file.txt",
		"newfile",
		"my-file",
		"my_file",
		"file123",
		".hidden", // a leading dot is a valid Unix filename
	}
	for _, name := range good {
		if err := validateBaseName(name); err != nil {
			t.Errorf("unexpected error for basename %q: %v", name, err)
		}
	}
}

func TestNormalizeForOS_NFCNormalization(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("NFC normalization only applies on darwin")
	}

	// "café" in NFC (precomposed) vs NFD (decomposed: "cafe" + combining acute accent)
	nfc := "caf\u00e9"          // café (single codepoint é)
	nfd := "cafe\u0301"         // café (e + combining acute accent)

	normalizedNFC := normalizeForOS(nfc)
	normalizedNFD := normalizeForOS(nfd)
	if normalizedNFC != normalizedNFD {
		t.Errorf("NFC and NFD forms should normalize to the same string: NFC=%q NFD=%q", normalizedNFC, normalizedNFD)
	}
}

func TestSafePath_UnicodeNFDBypass(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("unicode normalization bypass test only applies on darwin")
	}

	repo := t.TempDir()

	// Create a directory with a precomposed Unicode name
	dirName := "caf\u00e9" // NFC
	dirPath := filepath.Join(repo, dirName)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(dirPath, "data.txt"), "hello")

	// Deny the NFC form, then try to access via NFD form
	nfdDirName := "cafe\u0301" // NFD
	_, err := SafePath(repo, []string{"."}, []string{dirName}, nfdDirName+"/data.txt")
	if err == nil {
		t.Fatal("expected deny for NFD variant of denied NFC path, got allow")
	}
}

func TestSafePath_NullByteHandling(t *testing.T) {
	repo := t.TempDir()

	// Null bytes in paths are a classic attack vector
	cases := []string{
		"file.txt\x00.jpg",
		"\x00/etc/passwd",
		"foo\x00bar",
	}

	for _, path := range cases {
		_, err := SafePath(repo, []string{"."}, nil, path)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "null byte") {
			t.Errorf("expected null-byte denial for %q, got %v", path, err)
		}
	}
}
