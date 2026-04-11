package loopgate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func validateServerSocketPath(repoRoot string, rawSocketPath string) (string, error) {
	cleanedSocketPath := filepath.Clean(strings.TrimSpace(rawSocketPath))
	if cleanedSocketPath == "." || cleanedSocketPath == "" {
		return "", fmt.Errorf("socket path is required")
	}
	if !filepath.IsAbs(cleanedSocketPath) {
		return "", fmt.Errorf("socket path must be absolute")
	}
	if filepath.Ext(cleanedSocketPath) != ".sock" {
		return "", fmt.Errorf("socket path must end with .sock")
	}

	runtimeRoot := filepath.Join(repoRoot, "runtime")
	if socketPathWithinAllowedRoot(cleanedSocketPath, runtimeRoot) || socketPathIsDirectSystemTempChild(cleanedSocketPath) {
		return cleanedSocketPath, nil
	}
	if socketPathWithinAllowedRoot(cleanedSocketPath, os.TempDir()) && !socketPathWithinAllowedRoot(cleanedSocketPath, repoRoot) {
		return cleanedSocketPath, nil
	}
	return "", fmt.Errorf("socket path %q is outside allowed runtime roots", cleanedSocketPath)
}

func socketPathWithinAllowedRoot(candidatePath string, allowedRoot string) bool {
	cleanedCandidatePath := filepath.Clean(candidatePath)
	cleanedAllowedRoot := filepath.Clean(allowedRoot)
	if cleanedCandidatePath == cleanedAllowedRoot {
		return true
	}
	relativePath, err := filepath.Rel(cleanedAllowedRoot, cleanedCandidatePath)
	if err != nil {
		return false
	}
	return relativePath != ".." && !strings.HasPrefix(relativePath, ".."+string(filepath.Separator))
}

func socketPathIsDirectSystemTempChild(candidatePath string) bool {
	return filepath.Clean(filepath.Dir(candidatePath)) == filepath.Clean(os.TempDir())
}

func removeStaleSocketPath(socketPath string) error {
	pathInfo, err := os.Lstat(socketPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if pathInfo.IsDir() {
		return fmt.Errorf("socket path %q is a directory", socketPath)
	}
	return os.Remove(socketPath)
}
