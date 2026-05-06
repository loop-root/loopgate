package hostaccess

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/sys/unix"
)

type RelativePath struct {
	Display string
	Parts   []string
}

var (
	pathExpressionPattern = regexp.MustCompile(`^[^\x00]+$`)
	pathComponentPattern  = regexp.MustCompile(`^[^/\\\x00]+$`)
)

type PathPolicyError struct {
	message string
}

func (err *PathPolicyError) Error() string {
	return err.message
}

func newPathPolicyError(message string) error {
	return &PathPolicyError{message: message}
}

func IsPathPolicyError(err error) bool {
	var target *PathPolicyError
	return errors.As(err, &target)
}

func ValidPathExpression(path string) bool {
	return pathExpressionPattern.MatchString(path)
}

func NormalizeRelativePath(raw string) (RelativePath, error) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimLeft(trimmed, `/\`)
	if trimmed == "" {
		return RelativePath{}, nil
	}

	cleaned := filepath.Clean(filepath.FromSlash(trimmed))
	if cleaned == "." {
		return RelativePath{}, nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
		return RelativePath{}, newPathPolicyError("path escapes granted folder root")
	}

	rawParts := strings.Split(cleaned, string(os.PathSeparator))
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		if part == "" || part == "." || part == ".." {
			return RelativePath{}, newPathPolicyError("path must not contain parent segments")
		}
		parts = append(parts, part)
	}
	return RelativePath{
		Display: strings.Join(parts, "/"),
		Parts:   parts,
	}, nil
}

func wrapHostAccessOpenError(pathPart string, err error) error {
	switch {
	case errors.Is(err, unix.ELOOP):
		return newPathPolicyError("path traverses a symlink, which is not allowed")
	case errors.Is(err, unix.ENOTDIR):
		return newPathPolicyError("path component is not a directory")
	default:
		return fmt.Errorf("open host path component %q: %w", pathPart, err)
	}
}

func openHostRootDirectoryReadOnly(rootPath string) (int, error) {
	if !pathExpressionPattern.MatchString(rootPath) {
		return -1, newPathPolicyError("granted folder root contains unsupported characters")
	}
	rootFD, err := unix.Open(rootPath, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY, 0)
	if err != nil {
		return -1, fmt.Errorf("open granted folder root: %w", err)
	}
	return rootFD, nil
}

func OpenPathReadOnly(rootPath string, rawRelativePath string, expectDirectory bool) (*os.File, RelativePath, error) {
	normalizedPath, err := NormalizeRelativePath(rawRelativePath)
	if err != nil {
		return nil, RelativePath{}, err
	}

	rootFD, err := openHostRootDirectoryReadOnly(rootPath)
	if err != nil {
		return nil, normalizedPath, err
	}

	currentFD := rootFD
	currentLabel := rootPath
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

	if len(normalizedPath.Parts) == 0 {
		if !expectDirectory {
			return nil, normalizedPath, newPathPolicyError("path is a directory, not a file")
		}
		if !pathExpressionPattern.MatchString(currentLabel) {
			return nil, normalizedPath, newPathPolicyError("opened path contains unsupported characters")
		}
		currentFD = -1
		return os.NewFile(uintptr(rootFD), currentLabel), normalizedPath, nil
	}

	for pathPartIndex, pathPart := range normalizedPath.Parts {
		if !pathComponentPattern.MatchString(pathPart) {
			return nil, normalizedPath, newPathPolicyError("path component contains unsupported characters")
		}
		openFlags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
		isLastPart := pathPartIndex == len(normalizedPath.Parts)-1
		if !isLastPart || expectDirectory {
			openFlags |= unix.O_DIRECTORY
		}

		nextFD, openErr := unix.Openat(currentFD, pathPart, openFlags, 0)
		if openErr != nil {
			return nil, normalizedPath, wrapHostAccessOpenError(pathPart, openErr)
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
	if !pathExpressionPattern.MatchString(currentLabel) {
		_ = unix.Close(currentFDForFile)
		return nil, normalizedPath, newPathPolicyError("opened path contains unsupported characters")
	}
	fileHandle := os.NewFile(uintptr(currentFDForFile), currentLabel)
	if !expectDirectory {
		fileInfo, statErr := fileHandle.Stat()
		if statErr != nil {
			_ = fileHandle.Close()
			return nil, normalizedPath, fmt.Errorf("stat opened host file: %w", statErr)
		}
		if fileInfo.IsDir() {
			_ = fileHandle.Close()
			return nil, normalizedPath, newPathPolicyError("path is a directory, not a file")
		}
	}
	return fileHandle, normalizedPath, nil
}

func OpenParentDirectory(rootPath string, rawRelativePath string) (*os.File, string, RelativePath, error) {
	normalizedPath, err := NormalizeRelativePath(rawRelativePath)
	if err != nil {
		return nil, "", RelativePath{}, err
	}
	if len(normalizedPath.Parts) == 0 {
		return nil, "", normalizedPath, newPathPolicyError("path must refer to an entry beneath the granted folder")
	}

	rootFD, err := openHostRootDirectoryReadOnly(rootPath)
	if err != nil {
		return nil, "", normalizedPath, err
	}

	currentFD := rootFD
	currentLabel := rootPath
	parentParts := normalizedPath.Parts[:len(normalizedPath.Parts)-1]
	defer func() {
		if currentFD >= 0 {
			_ = unix.Close(currentFD)
		}
	}()

	for _, pathPart := range parentParts {
		nextFD, openErr := unix.Openat(currentFD, pathPart, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		if openErr != nil {
			return nil, "", normalizedPath, wrapHostAccessOpenError(pathPart, openErr)
		}
		_ = unix.Close(currentFD)
		currentFD = nextFD
		currentLabel = filepath.Join(currentLabel, pathPart)
	}

	parentFD := currentFD
	currentFD = -1
	return os.NewFile(uintptr(parentFD), currentLabel), normalizedPath.Parts[len(normalizedPath.Parts)-1], normalizedPath, nil
}

func LstatPathUnderRoot(rootPath string, rawRelativePath string) (RelativePath, unix.Stat_t, bool, error) {
	normalizedPath, err := NormalizeRelativePath(rawRelativePath)
	if err != nil {
		return RelativePath{}, unix.Stat_t{}, false, err
	}

	if len(normalizedPath.Parts) == 0 {
		rootFD, openErr := openHostRootDirectoryReadOnly(rootPath)
		if openErr != nil {
			return normalizedPath, unix.Stat_t{}, false, openErr
		}
		defer func() { _ = unix.Close(rootFD) }()

		var rootStat unix.Stat_t
		if statErr := unix.Fstat(rootFD, &rootStat); statErr != nil {
			return normalizedPath, unix.Stat_t{}, false, fmt.Errorf("stat granted folder root: %w", statErr)
		}
		return normalizedPath, rootStat, true, nil
	}

	parentHandle, baseName, normalizedPath, err := OpenParentDirectory(rootPath, rawRelativePath)
	if err != nil {
		return normalizedPath, unix.Stat_t{}, false, err
	}
	defer parentHandle.Close()

	var statResult unix.Stat_t
	if statErr := unix.Fstatat(int(parentHandle.Fd()), baseName, &statResult, unix.AT_SYMLINK_NOFOLLOW); statErr != nil {
		if errors.Is(statErr, unix.ENOENT) {
			return normalizedPath, unix.Stat_t{}, false, nil
		}
		return normalizedPath, unix.Stat_t{}, false, wrapHostAccessOpenError(baseName, statErr)
	}
	return normalizedPath, statResult, true, nil
}

func EnsureDirectoryUnderRoot(rootPath string, rawRelativePath string, permissions uint32) (RelativePath, error) {
	normalizedPath, err := NormalizeRelativePath(rawRelativePath)
	if err != nil {
		return RelativePath{}, err
	}

	rootFD, err := openHostRootDirectoryReadOnly(rootPath)
	if err != nil {
		return normalizedPath, err
	}

	currentFD := rootFD
	defer func() {
		if currentFD >= 0 {
			_ = unix.Close(currentFD)
		}
	}()

	for _, pathPart := range normalizedPath.Parts {
		nextFD, openErr := unix.Openat(currentFD, pathPart, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		if openErr == nil {
			if currentFD != rootFD {
				_ = unix.Close(currentFD)
			}
			if currentFD == rootFD {
				_ = unix.Close(rootFD)
			}
			currentFD = nextFD
			continue
		}
		if !errors.Is(openErr, unix.ENOENT) {
			return normalizedPath, wrapHostAccessOpenError(pathPart, openErr)
		}
		if mkdirErr := unix.Mkdirat(currentFD, pathPart, permissions); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
			return normalizedPath, fmt.Errorf("mkdir host path component %q: %w", pathPart, mkdirErr)
		}
		nextFD, openErr = unix.Openat(currentFD, pathPart, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		if openErr != nil {
			return normalizedPath, wrapHostAccessOpenError(pathPart, openErr)
		}
		if currentFD != rootFD {
			_ = unix.Close(currentFD)
		}
		if currentFD == rootFD {
			_ = unix.Close(rootFD)
		}
		currentFD = nextFD
	}

	return normalizedPath, nil
}

func PathModeIsDirectory(mode uint32) bool {
	return mode&unix.S_IFMT == unix.S_IFDIR
}

func PathModeIsSymlink(mode uint32) bool {
	return mode&unix.S_IFMT == unix.S_IFLNK
}
