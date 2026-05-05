package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"loopgate/internal/safety"

	"golang.org/x/sys/unix"
)

type validatedPath struct {
	ResolvedPath       string
	MatchedRoot        string
	RelativeToRoot     string
	RelativeParts      []string
	InputPathIsSymlink bool
}

var toolsPathExpressionPattern = regexp.MustCompile(`^[^\x00]+$`)

func resolveValidatedPath(repoRoot string, allowedRoots []string, deniedPaths []string, userPath string) (validatedPath, error) {
	candidateInputPath, err := rawCandidatePath(repoRoot, userPath)
	if err != nil {
		return validatedPath{}, err
	}

	explanation, err := safety.ExplainSafePath(repoRoot, allowedRoots, deniedPaths, userPath)
	if err != nil {
		return validatedPath{}, err
	}
	if strings.TrimSpace(explanation.AllowedMatch) == "" {
		return validatedPath{}, fmt.Errorf("allowed root match not found")
	}

	relativePath, err := filepath.Rel(explanation.AllowedMatch, explanation.ResolvedAbs)
	if err != nil {
		return validatedPath{}, fmt.Errorf("build relative path: %w", err)
	}
	relativePath = filepath.Clean(relativePath)
	if strings.HasPrefix(relativePath, "..") {
		return validatedPath{}, fmt.Errorf("resolved path escaped matched root")
	}

	relativeParts := []string{}
	if relativePath != "." {
		for _, relativePart := range strings.Split(relativePath, string(os.PathSeparator)) {
			if relativePart == "" || relativePart == "." || relativePart == ".." {
				return validatedPath{}, fmt.Errorf("invalid resolved path segment %q", relativePart)
			}
			relativeParts = append(relativeParts, relativePart)
		}
	}

	return validatedPath{
		ResolvedPath:       explanation.ResolvedAbs,
		MatchedRoot:        explanation.AllowedMatch,
		RelativeToRoot:     relativePath,
		RelativeParts:      relativeParts,
		InputPathIsSymlink: isSymlinkPath(candidateInputPath),
	}, nil
}

func openResolvedPathReadOnly(validated validatedPath, expectDirectory bool) (*os.File, error) {
	rootFD, err := unix.Open(validated.MatchedRoot, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, fmt.Errorf("open matched root: %w", err)
	}

	currentFD := rootFD
	currentLabel := validated.MatchedRoot
	openedRoot := true
	closeCurrent := func() {
		if currentFD >= 0 {
			_ = unix.Close(currentFD)
			currentFD = -1
		}
	}
	defer func() {
		if currentFD >= 0 {
			closeCurrent()
		}
	}()

	if len(validated.RelativeParts) == 0 {
		if !expectDirectory {
			return nil, fmt.Errorf("path is a directory, use fs_list instead")
		}
		currentFD = -1
		return os.NewFile(uintptr(rootFD), currentLabel), nil
	}

	for partIndex, pathPart := range validated.RelativeParts {
		openFlags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
		isLastPart := partIndex == len(validated.RelativeParts)-1
		if !isLastPart || expectDirectory {
			openFlags |= unix.O_DIRECTORY
		}

		nextFD, err := unix.Openat(currentFD, pathPart, openFlags, 0)
		if err != nil {
			return nil, fmt.Errorf("open resolved component %q: %w", pathPart, err)
		}
		if !openedRoot || currentFD != rootFD {
			_ = unix.Close(currentFD)
		}
		if openedRoot && currentFD == rootFD {
			_ = unix.Close(rootFD)
			openedRoot = false
		}
		currentFD = nextFD
		currentLabel = filepath.Join(currentLabel, pathPart)
	}

	currentFDForFile := currentFD
	currentFD = -1
	fileHandle := os.NewFile(uintptr(currentFDForFile), currentLabel)
	if !expectDirectory {
		fileInfo, err := fileHandle.Stat()
		if err != nil {
			_ = fileHandle.Close()
			return nil, fmt.Errorf("stat opened file: %w", err)
		}
		if fileInfo.IsDir() {
			_ = fileHandle.Close()
			return nil, fmt.Errorf("path is a directory, use fs_list instead")
		}
	}
	return fileHandle, nil
}

func openParentDirectoryNoFollowForWrite(validated validatedPath) (*os.File, string, error) {
	if validated.InputPathIsSymlink {
		return nil, "", fmt.Errorf("writing through a symlink path is denied")
	}

	baseName := filepath.Base(validated.ResolvedPath)
	if baseName == "" || baseName == "." || baseName == ".." || strings.ContainsRune(baseName, os.PathSeparator) {
		return nil, "", fmt.Errorf("invalid target name")
	}

	rootFD, err := unix.Open(validated.MatchedRoot, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, "", fmt.Errorf("open matched root: %w", err)
	}

	currentFD := rootFD
	currentLabel := validated.MatchedRoot
	parentParts := validated.RelativeParts
	if len(parentParts) > 0 {
		parentParts = parentParts[:len(parentParts)-1]
	}
	defer func() {
		if currentFD >= 0 {
			_ = unix.Close(currentFD)
		}
	}()

	for _, pathPart := range parentParts {
		nextFD, err := unix.Openat(currentFD, pathPart, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		if err != nil {
			return nil, "", fmt.Errorf("open resolved parent component %q: %w", pathPart, err)
		}
		_ = unix.Close(currentFD)
		currentFD = nextFD
		currentLabel = filepath.Join(currentLabel, pathPart)
	}

	parentFD := currentFD
	currentFD = -1
	return os.NewFile(uintptr(parentFD), currentLabel), baseName, nil
}

func isSymlinkPath(candidatePath string) bool {
	if !toolsPathExpressionPattern.MatchString(candidatePath) {
		return false
	}
	fileInfo, err := os.Lstat(candidatePath)
	if err != nil {
		return false
	}
	return fileInfo.Mode()&os.ModeSymlink != 0
}

func rawCandidatePath(repoRoot string, userPath string) (string, error) {
	if filepath.IsAbs(userPath) {
		return filepath.Abs(filepath.Clean(userPath))
	}
	return filepath.Abs(filepath.Join(repoRoot, userPath))
}
