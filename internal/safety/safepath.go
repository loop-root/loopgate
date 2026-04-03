package safety

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/text/unicode/norm"
)

func rejectNullByte(rawPath string) error {
	if strings.ContainsRune(rawPath, 0) {
		return fmt.Errorf("path contains null byte")
	}
	return nil
}

// resolvePathStrict resolves a path's real filesystem location, fail-closed.
//
// For paths that exist: uses filepath.EvalSymlinks to get the canonical target.
// For paths that do not yet exist (e.g. a new file being written): the immediate
// parent directory must exist and resolve cleanly, and the basename must be a
// single path element with no separators or "..".
//
// This function never silently falls back to an unresolved path. If resolution
// cannot be proven, an error is returned and the caller must deny the operation.
func resolvePathStrict(path string) (string, error) {
	if err := rejectNullByte(path); err != nil {
		return "", err
	}

	// Fast path: the full path already exists — resolve it directly.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved, nil
	}

	// Path does not exist yet. Resolve only the immediate parent directory.
	// Walking further up the tree is not permitted: if the parent does not
	// exist, we cannot prove the path is safe and must deny.
	parentDir := filepath.Dir(path)
	baseName := filepath.Base(path)

	if err := validateBaseName(baseName); err != nil {
		return "", fmt.Errorf("invalid_basename: %w", err)
	}

	resolvedParent, err := filepath.EvalSymlinks(parentDir)
	if err != nil {
		return "", fmt.Errorf("parent_directory_not_resolved: %w", err)
	}

	return filepath.Join(resolvedParent, baseName), nil
}

// validateBaseName ensures name is a single clean path element.
// It must not be empty, ".", "..", or contain path separators.
// filepath.Base after filepath.Clean should never produce a name that fails
// this check, but we validate explicitly for defense-in-depth.
func validateBaseName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("%q is not a valid path element", name)
	}
	if strings.ContainsRune(name, '/') {
		return fmt.Errorf("%q contains path separators", name)
	}
	if os.PathSeparator != '/' && strings.ContainsRune(name, os.PathSeparator) {
		return fmt.Errorf("%q contains path separators", name)
	}
	return nil
}

func cleanAbs(path string) (string, error) {
	if path == "" {
		return "", errors.New("empty path")
	}
	if err := rejectNullByte(path); err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	absPath = filepath.Clean(absPath)

	// Canonicalize symlinked prefixes when possible (macOS often returns /var paths)
	// so allow/deny comparisons match resolved targets (e.g., /var -> /private/var).
	if symResolved, symErr := filepath.EvalSymlinks(absPath); symErr == nil {
		if resolvedAbs, absErr := filepath.Abs(symResolved); absErr == nil {
			absPath = filepath.Clean(resolvedAbs)
		}
	}

	return absPath, nil
}

func normalizeForOS(path string) string {
	// macOS commonly uses a case-insensitive filesystem; comparing in lower-case
	// blocks trivial case-bypass attempts. APFS stores filenames in NFD form, so
	// NFC-normalize to prevent combining-character mismatches in isWithin.
	if runtime.GOOS == "darwin" {
		return strings.ToLower(norm.NFC.String(path))
	}
	return path
}

func isWithin(rootAbs, targetAbs string) bool {
	normalizedRoot := normalizeForOS(filepath.Clean(rootAbs))
	normalizedTarget := normalizeForOS(filepath.Clean(targetAbs))

	if normalizedRoot == normalizedTarget {
		return true
	}
	// Ensure whole-path-segment prefix match (avoid /rootX prefix tricks).
	sep := string(os.PathSeparator)
	if !strings.HasSuffix(normalizedRoot, sep) {
		normalizedRoot += sep
	}
	return strings.HasPrefix(normalizedTarget, normalizedRoot)
}

// SafePath resolves a user-supplied path into an absolute path under an allowed root,
// enforcing an explicit deny list. Symlinks are resolved before checks so a symlink
// inside an allowed root cannot escape to arbitrary locations.
func SafePath(repoRoot string, allowedRoots []string, deniedPaths []string, userPath string) (string, error) {
	if strings.TrimSpace(userPath) == "" {
		return "", fmt.Errorf("empty path")
	}
	if err := rejectNullByte(userPath); err != nil {
		return "", err
	}

	repoAbs, err := cleanAbs(repoRoot)
	if err != nil {
		return "", err
	}

	// Build absolute path from userPath. If relative, treat as repo-relative.
	var abs string
	if filepath.IsAbs(userPath) {
		abs = userPath
	} else {
		abs = filepath.Join(repoAbs, userPath)
	}

	abs, err = cleanAbs(abs)
	if err != nil {
		return "", err
	}

	// Resolve symlinks so checks apply to the real target. Fail closed:
	// if we cannot prove where this path points on disk, deny the operation.
	resolvedRaw, resolveErr := resolvePathStrict(abs)
	if resolveErr != nil {
		return "", fmt.Errorf("symlink_resolution_failed: %w", resolveErr)
	}
	var resolvedAbs string
	resolvedAbs, err = cleanAbs(resolvedRaw)
	if err != nil {
		return "", fmt.Errorf("symlink_resolution_failed: %w", err)
	}

	// Allowed roots: interpret relative roots as repo-relative.
	allowed := false
	for _, root := range allowedRoots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		var rootAbs string
		if filepath.IsAbs(root) {
			rootAbs = root
		} else {
			rootAbs = filepath.Join(repoAbs, root)
		}
		rootAbs, err = cleanAbs(rootAbs)
		if err != nil {
			continue
		}
		if isWithin(rootAbs, resolvedAbs) {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("path is outside allowed roots")
	}

	// Deny list: interpret relative denies as repo-relative.
	for _, deny := range deniedPaths {
		deny = strings.TrimSpace(deny)
		if deny == "" {
			continue
		}
		var denyAbs string
		if filepath.IsAbs(deny) {
			denyAbs = deny
		} else {
			denyAbs = filepath.Join(repoAbs, deny)
		}
		denyAbs, err = cleanAbs(denyAbs)
		if err != nil {
			// Fail closed: an unresolvable deny rule must not be skipped (would widen access).
			return "", fmt.Errorf("deny path resolution failed: %w", err)
		}
		if isWithin(denyAbs, resolvedAbs) {
			return "", fmt.Errorf("path is denied by policy")
		}
	}

	return resolvedAbs, nil
}

// SafePathExplanation captures intermediate values used by SafePath.
// This is intended for debugging and tests.
type SafePathExplanation struct {
	RepoAbs      string
	Input        string
	CandidateAbs string
	ResolvedAbs  string
	AllowedRoots []string
	AllowedMatch string
	DeniedPaths  []string
	DeniedMatch  string
	Decision     string
}

// ExplainSafePath returns the same decision as SafePath, along with intermediate values.
func ExplainSafePath(repoRoot string, allowedRoots []string, deniedPaths []string, userPath string) (SafePathExplanation, error) {
	explanation := SafePathExplanation{Input: userPath}

	if err := rejectNullByte(userPath); err != nil {
		explanation.Decision = "deny:invalid_path"
		return explanation, err
	}

	repoAbs, err := cleanAbs(repoRoot)
	if err != nil {
		explanation.Decision = "error"
		return explanation, err
	}
	explanation.RepoAbs = repoAbs

	var abs string
	if filepath.IsAbs(userPath) {
		abs = userPath
	} else {
		abs = filepath.Join(repoAbs, userPath)
	}
	abs, err = cleanAbs(abs)
	if err != nil {
		explanation.Decision = "error"
		return explanation, err
	}
	explanation.CandidateAbs = abs

	resolvedRaw, resolveErr := resolvePathStrict(abs)
	if resolveErr != nil {
		explanation.Decision = "deny:symlink_resolution_failed"
		return explanation, fmt.Errorf("symlink_resolution_failed: %w", resolveErr)
	}
	var resolvedAbs string
	resolvedAbs, err = cleanAbs(resolvedRaw)
	if err != nil {
		explanation.Decision = "deny:symlink_resolution_failed"
		return explanation, fmt.Errorf("symlink_resolution_failed: %w", err)
	}
	explanation.ResolvedAbs = resolvedAbs

	// Allowed roots
	for _, root := range allowedRoots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		var rootAbs string
		if filepath.IsAbs(root) {
			rootAbs = root
		} else {
			rootAbs = filepath.Join(repoAbs, root)
		}
		rootAbs, err = cleanAbs(rootAbs)
		if err != nil {
			continue
		}
		explanation.AllowedRoots = append(explanation.AllowedRoots, rootAbs)
		if isWithin(rootAbs, resolvedAbs) {
			explanation.AllowedMatch = rootAbs
			break
		}
	}
	if explanation.AllowedMatch == "" {
		explanation.Decision = "deny:outside_allowed_roots"
		return explanation, fmt.Errorf("path is outside allowed roots")
	}

	// Deny list
	for _, deny := range deniedPaths {
		deny = strings.TrimSpace(deny)
		if deny == "" {
			continue
		}
		var denyAbs string
		if filepath.IsAbs(deny) {
			denyAbs = deny
		} else {
			denyAbs = filepath.Join(repoAbs, deny)
		}
		denyAbs, err = cleanAbs(denyAbs)
		if err != nil {
			explanation.Decision = "deny:deny_resolution_failed"
			return explanation, fmt.Errorf("deny path resolution failed: %w", err)
		}
		explanation.DeniedPaths = append(explanation.DeniedPaths, denyAbs)
		if isWithin(denyAbs, resolvedAbs) {
			explanation.DeniedMatch = denyAbs
			explanation.Decision = "deny:denied_by_policy"
			return explanation, fmt.Errorf("path is denied by policy")
		}
	}

	explanation.Decision = "allow"
	return explanation, nil
}
