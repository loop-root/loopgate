package memory

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type UnsupportedRawMemoryArtifact struct {
	AbsolutePath string
	RelativePath string
	Kind         string
	FileCount    int
}

func InspectUnsupportedRawMemoryArtifacts(repoRoot string) ([]UnsupportedRawMemoryArtifact, error) {
	rawMemoryPath := filepath.Join(repoRoot, ".morph", "memory")
	fileInfo, err := os.Lstat(rawMemoryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("inspect unsupported raw memory path: %w", err)
	}

	relativePath, err := filepath.Rel(repoRoot, rawMemoryPath)
	if err != nil {
		relativePath = filepath.Clean(rawMemoryPath)
	}
	rawMemoryArtifact := UnsupportedRawMemoryArtifact{
		AbsolutePath: rawMemoryPath,
		RelativePath: filepath.Clean(relativePath),
		Kind:         unsupportedRawMemoryKind(fileInfo),
	}

	if !fileInfo.IsDir() || fileInfo.Mode()&os.ModeSymlink != 0 {
		rawMemoryArtifact.FileCount = 1
		return []UnsupportedRawMemoryArtifact{rawMemoryArtifact}, nil
	}

	fileCount := 0
	walkErr := filepath.WalkDir(rawMemoryPath, func(currentPath string, directoryEntry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if currentPath == rawMemoryPath {
			return nil
		}
		if !directoryEntry.IsDir() {
			fileCount++
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk unsupported raw memory path: %w", walkErr)
	}
	rawMemoryArtifact.FileCount = fileCount
	return []UnsupportedRawMemoryArtifact{rawMemoryArtifact}, nil
}

func FormatUnsupportedRawMemoryArtifactWarning(rawMemoryArtifact UnsupportedRawMemoryArtifact) string {
	return fmt.Sprintf(
		"unsupported raw memory %s detected at %s (%d file entries). This path is ignored by governed memory commands and should not be treated as authoritative memory state.",
		rawMemoryArtifact.Kind,
		rawMemoryArtifact.RelativePath,
		rawMemoryArtifact.FileCount,
	)
}

func unsupportedRawMemoryKind(fileInfo os.FileInfo) string {
	switch {
	case fileInfo.Mode()&os.ModeSymlink != 0:
		return "symlink"
	case fileInfo.IsDir():
		return "directory"
	default:
		return "file"
	}
}
