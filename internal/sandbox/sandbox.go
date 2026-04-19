package sandbox

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"

	"loopgate/internal/safety"

	"golang.org/x/sys/unix"
	"golang.org/x/text/unicode/norm"
)

var (
	ErrSandboxPathInvalid       = errors.New("sandbox path is invalid")
	ErrSandboxPathOutsideRoot   = errors.New("sandbox path is outside sandbox home")
	ErrSandboxSourceUnavailable = errors.New("sandbox source is unavailable")
	ErrSandboxDestinationExists = errors.New("sandbox destination already exists")
	ErrSymlinkNotAllowed        = errors.New("symlink entries are not allowed")
)

const (
	VirtualRoot      = "/loopgate"
	VirtualHome      = "/loopgate/home"
	VirtualWorkspace = "/loopgate/home/workspace"
	VirtualImports   = "/loopgate/home/imports"
	VirtualOutputs   = "/loopgate/home/outputs"
	VirtualScratch   = "/loopgate/home/scratch"
	VirtualAgents    = "/loopgate/home/agents"
	VirtualTmp       = "/loopgate/home/tmp"
	VirtualLogs      = "/loopgate/home/logs"
)

type Paths struct {
	Root      string
	Home      string
	Workspace string
	Imports   string
	Outputs   string
	Scratch   string
	Agents    string
	Tmp       string
	Logs      string
}

func PathsForRepo(repoRoot string) Paths {
	rootPath := filepath.Join(repoRoot, "runtime", "sandbox", "root")
	homePath := filepath.Join(rootPath, "home")
	return Paths{
		Root:      rootPath,
		Home:      homePath,
		Workspace: filepath.Join(homePath, "workspace"),
		Imports:   filepath.Join(homePath, "imports"),
		Outputs:   filepath.Join(homePath, "outputs"),
		Scratch:   filepath.Join(homePath, "scratch"),
		Agents:    filepath.Join(homePath, "agents"),
		Tmp:       filepath.Join(homePath, "tmp"),
		Logs:      filepath.Join(homePath, "logs"),
	}
}

func (paths Paths) Ensure() error {
	for _, targetPath := range []string{
		paths.Root,
		paths.Home,
		paths.Workspace,
		paths.Imports,
		paths.Outputs,
		paths.Scratch,
		paths.Agents,
		paths.Tmp,
		paths.Logs,
	} {
		if err := os.MkdirAll(targetPath, 0o700); err != nil {
			return fmt.Errorf("create sandbox path %s: %w", targetPath, err)
		}
	}
	return nil
}

func NormalizeRelativePath(rawPath string) (string, error) {
	trimmedPath := strings.TrimSpace(rawPath)
	if trimmedPath == "" {
		return "", fmt.Errorf("%w: path is required", ErrSandboxPathInvalid)
	}
	trimmedPath = norm.NFC.String(trimmedPath)
	if strings.HasPrefix(trimmedPath, "/") || filepath.IsAbs(trimmedPath) {
		return "", fmt.Errorf("%w: absolute paths are not allowed", ErrSandboxPathInvalid)
	}
	normalizedPath := path.Clean(strings.ReplaceAll(trimmedPath, "\\", "/"))
	if normalizedPath == "." || normalizedPath == "" {
		return "", fmt.Errorf("%w: empty sandbox path", ErrSandboxPathInvalid)
	}
	if strings.HasPrefix(normalizedPath, "../") || normalizedPath == ".." {
		return "", fmt.Errorf("%w: traversal is not allowed", ErrSandboxPathInvalid)
	}
	return normalizedPath, nil
}

func NormalizeHomePath(rawPath string) (string, error) {
	trimmedPath := strings.TrimSpace(rawPath)
	if trimmedPath == "" {
		return "", fmt.Errorf("%w: path is required", ErrSandboxPathInvalid)
	}
	trimmedPath = norm.NFC.String(trimmedPath)
	if strings.HasPrefix(trimmedPath, "/") || filepath.IsAbs(trimmedPath) {
		normalizedPath := path.Clean(strings.ReplaceAll(trimmedPath, "\\", "/"))
		if normalizedPath == VirtualHome {
			return ".", nil
		}
		if !strings.HasPrefix(normalizedPath, VirtualHome+"/") {
			return "", fmt.Errorf("%w: absolute sandbox paths must stay inside %s", ErrSandboxPathInvalid, VirtualHome)
		}
		return NormalizeRelativePath(strings.TrimPrefix(normalizedPath, VirtualHome+"/"))
	}
	return NormalizeRelativePath(trimmedPath)
}

func VirtualizeRelativeHomePath(sandboxRelativePath string) string {
	normalizedPath := path.Clean(strings.ReplaceAll(strings.TrimSpace(sandboxRelativePath), "\\", "/"))
	if normalizedPath == "" || normalizedPath == "." {
		return VirtualHome
	}
	return path.Join(VirtualHome, normalizedPath)
}

func (paths Paths) ResolveHomePath(rawRelativePath string) (string, string, error) {
	normalizedRelativePath, err := NormalizeHomePath(rawRelativePath)
	if err != nil {
		return "", "", err
	}
	candidatePath := paths.Home
	if normalizedRelativePath != "." {
		candidatePath = filepath.Join(paths.Home, filepath.FromSlash(normalizedRelativePath))
	}
	resolvedPath, err := resolveExistingPathWithin(paths.Home, candidatePath)
	if err != nil {
		return "", "", err
	}
	return resolvedPath, normalizedRelativePath, nil
}

func (paths Paths) ResolveOutputsPath(rawRelativePath string) (string, string, error) {
	resolvedPath, normalizedRelativePath, err := paths.ResolveHomePath(rawRelativePath)
	if err != nil {
		return "", "", err
	}
	if normalizedRelativePath != "outputs" && !strings.HasPrefix(normalizedRelativePath, "outputs/") {
		return "", "", fmt.Errorf("%w: path must stay inside %s/", ErrSandboxPathOutsideRoot, VirtualOutputs)
	}
	return resolvedPath, normalizedRelativePath, nil
}

func (paths Paths) BuildImportDestination(rawName string) (string, string, error) {
	destinationName := strings.TrimSpace(rawName)
	if destinationName == "" {
		return "", "", fmt.Errorf("%w: destination name is required", ErrSandboxPathInvalid)
	}
	sanitizedName := sanitizeEntryName(destinationName)
	if sanitizedName == "" {
		return "", "", fmt.Errorf("%w: destination name is invalid", ErrSandboxPathInvalid)
	}
	relativePath := path.Join("imports", sanitizedName)
	absolutePath, err := resolveCreatePathWithin(paths.Home, filepath.Join(paths.Imports, sanitizedName))
	if err != nil {
		return "", "", err
	}
	return absolutePath, relativePath, nil
}

func (paths Paths) BuildStagedOutput(rawName string) (string, string, error) {
	destinationName := strings.TrimSpace(rawName)
	if destinationName == "" {
		return "", "", fmt.Errorf("%w: output name is required", ErrSandboxPathInvalid)
	}
	sanitizedName := sanitizeEntryName(destinationName)
	if sanitizedName == "" {
		return "", "", fmt.Errorf("%w: output name is invalid", ErrSandboxPathInvalid)
	}
	relativePath := path.Join("outputs", sanitizedName)
	absolutePath, err := resolveCreatePathWithin(paths.Home, filepath.Join(paths.Outputs, sanitizedName))
	if err != nil {
		return "", "", err
	}
	return absolutePath, relativePath, nil
}

func (paths Paths) BuildAgentWorkingDirectory(rawName string) (string, string, error) {
	directoryName := strings.TrimSpace(rawName)
	if directoryName == "" {
		return "", "", fmt.Errorf("%w: agent directory name is required", ErrSandboxPathInvalid)
	}
	sanitizedName := sanitizeEntryName(directoryName)
	if sanitizedName == "" {
		return "", "", fmt.Errorf("%w: agent directory name is invalid", ErrSandboxPathInvalid)
	}
	relativePath := path.Join("agents", sanitizedName)
	absolutePath, err := resolveCreatePathWithin(paths.Home, filepath.Join(paths.Agents, sanitizedName))
	if err != nil {
		return "", "", err
	}
	return absolutePath, relativePath, nil
}

func ResolveHostSource(rawHostPath string) (string, fs.FileInfo, error) {
	trimmedPath := strings.TrimSpace(rawHostPath)
	if trimmedPath == "" {
		return "", nil, fmt.Errorf("%w: host source path is required", ErrSandboxSourceUnavailable)
	}
	absolutePath, err := filepath.Abs(trimmedPath)
	if err != nil {
		return "", nil, fmt.Errorf("%w: resolve source path: %v", ErrSandboxSourceUnavailable, err)
	}
	resolvedPath, err := filepath.EvalSymlinks(absolutePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, fmt.Errorf("%w: source path not found", ErrSandboxSourceUnavailable)
		}
		return "", nil, fmt.Errorf("%w: evaluate source symlinks: %v", ErrSandboxSourceUnavailable, err)
	}
	fileInfo, err := os.Stat(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, fmt.Errorf("%w: source path not found", ErrSandboxSourceUnavailable)
		}
		return "", nil, fmt.Errorf("%w: stat source path: %v", ErrSandboxSourceUnavailable, err)
	}
	return resolvedPath, fileInfo, nil
}

func ResolveHostDestination(rawHostPath string) (string, error) {
	trimmedPath := strings.TrimSpace(rawHostPath)
	if trimmedPath == "" {
		return "", fmt.Errorf("%w: host destination path is required", ErrSandboxPathInvalid)
	}
	absolutePath, err := filepath.Abs(trimmedPath)
	if err != nil {
		return "", fmt.Errorf("%w: resolve destination path: %v", ErrSandboxPathInvalid, err)
	}
	parentPath := filepath.Dir(absolutePath)
	resolvedParentPath, err := filepath.EvalSymlinks(parentPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: destination parent does not exist", ErrSandboxPathInvalid)
		}
		return "", fmt.Errorf("%w: resolve destination parent: %v", ErrSandboxPathInvalid, err)
	}
	parentInfo, err := os.Stat(resolvedParentPath)
	if err != nil {
		return "", fmt.Errorf("%w: stat destination parent: %v", ErrSandboxPathInvalid, err)
	}
	if !parentInfo.IsDir() {
		return "", fmt.Errorf("%w: destination parent is not a directory", ErrSandboxPathInvalid)
	}
	return filepath.Join(resolvedParentPath, filepath.Base(absolutePath)), nil
}

func CopyPathAtomic(sourcePath string, destinationPath string) (string, error) {
	if _, err := os.Lstat(destinationPath); err == nil {
		return "", fmt.Errorf("%w: %s", ErrSandboxDestinationExists, destinationPath)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat destination path: %w", err)
	}

	return copyPathIntoDestination(sourcePath, destinationPath, false, nil)
}

// MirrorPathAtomic replaces the destination with a fresh copy of the source.
// The replacement happens via sibling temp and backup paths so the destination
// is either the old content or the new content, never a half-written tree.
func MirrorPathAtomic(sourcePath string, destinationPath string) (string, error) {
	return MirrorPathAtomicWithFinalize(sourcePath, destinationPath, nil)
}

// MirrorPathAtomicWithFinalize replaces the destination with a fresh copy of
// the source and runs finalize before deleting the backup of the prior
// destination. If finalize fails, the new destination is rolled back and the
// previous destination is restored.
func MirrorPathAtomicWithFinalize(sourcePath string, destinationPath string, finalize func(entryType string) error) (string, error) {
	return copyPathIntoDestination(sourcePath, destinationPath, true, finalize)
}

func copyPathIntoDestination(sourcePath string, destinationPath string, allowReplace bool, finalize func(entryType string) error) (string, error) {
	if !allowReplace {
		if _, err := os.Lstat(destinationPath); err == nil {
			return "", fmt.Errorf("%w: %s", ErrSandboxDestinationExists, destinationPath)
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("stat destination path: %w", err)
		}
	}

	sourceFileHandle, sourceInfo, err := openSourcePathReadOnlyNoFollow(sourcePath)
	if err != nil {
		return "", err
	}
	defer sourceFileHandle.Close()

	temporaryPath := destinationPath + ".tmp"
	_ = os.RemoveAll(temporaryPath)

	entryType := "file"
	if sourceInfo.IsDir() {
		entryType = "directory"
		if err := copyDirectoryNoSymlinksFromHandle(sourceFileHandle, temporaryPath); err != nil {
			_ = os.RemoveAll(temporaryPath)
			return "", err
		}
	} else {
		if err := copyFileNoSymlinksFromHandle(sourceFileHandle, sourceInfo, temporaryPath); err != nil {
			_ = os.RemoveAll(temporaryPath)
			return "", err
		}
	}

	backupPath := destinationPath + ".bak"
	_ = os.RemoveAll(backupPath)
	destinationExists := false
	if _, err := os.Lstat(destinationPath); err == nil {
		destinationExists = true
		if err := os.Rename(destinationPath, backupPath); err != nil {
			_ = os.RemoveAll(temporaryPath)
			return "", fmt.Errorf("move existing destination aside: %w", err)
		}
	} else if !os.IsNotExist(err) {
		_ = os.RemoveAll(temporaryPath)
		return "", fmt.Errorf("stat destination path: %w", err)
	}

	if err := os.Rename(temporaryPath, destinationPath); err != nil {
		_ = os.RemoveAll(temporaryPath)
		if destinationExists {
			_ = os.Rename(backupPath, destinationPath)
		}
		return "", fmt.Errorf("rename copied path into place: %w", err)
	}

	if finalize != nil {
		if err := finalize(entryType); err != nil {
			if removeErr := os.RemoveAll(destinationPath); removeErr != nil {
				return "", fmt.Errorf("finalize replaced destination: %w (remove failed: %v)", err, removeErr)
			}
			if destinationExists {
				if restoreErr := os.Rename(backupPath, destinationPath); restoreErr != nil {
					return "", fmt.Errorf("finalize replaced destination: %w (restore failed: %v)", err, restoreErr)
				}
			}
			if syncErr := syncParentDirectory(destinationPath); syncErr != nil {
				return "", fmt.Errorf("finalize replaced destination: %w (sync failed: %v)", err, syncErr)
			}
			return "", err
		}
	}

	if destinationExists {
		if err := os.RemoveAll(backupPath); err != nil {
			return "", fmt.Errorf("remove replaced destination backup: %w", err)
		}
	}
	if err := syncParentDirectory(destinationPath); err != nil {
		return "", err
	}
	return entryType, nil
}

func syncParentDirectory(targetPath string) error {
	destinationDir, err := os.Open(filepath.Dir(targetPath))
	if err != nil {
		return fmt.Errorf("open destination parent: %w", err)
	}
	defer destinationDir.Close()
	if err := destinationDir.Sync(); err != nil {
		return fmt.Errorf("sync destination parent: %w", err)
	}
	return nil
}

func resolveExistingPathWithin(rootPath string, candidatePath string) (string, error) {
	resolvedPath, err := filepath.EvalSymlinks(candidatePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: source path not found", ErrSandboxSourceUnavailable)
		}
		return "", fmt.Errorf("%w: evaluate path: %v", ErrSandboxSourceUnavailable, err)
	}
	if err := ensureWithinRoot(rootPath, resolvedPath); err != nil {
		return "", err
	}
	return resolvedPath, nil
}

func resolveCreatePathWithin(rootPath string, candidatePath string) (string, error) {
	absoluteCandidatePath := candidatePath
	if !filepath.IsAbs(absoluteCandidatePath) {
		absoluteCandidatePath = filepath.Clean(filepath.Join(rootPath, candidatePath))
	}
	parentPath := filepath.Dir(absoluteCandidatePath)
	resolvedParentPath, err := filepath.EvalSymlinks(parentPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: destination parent does not exist", ErrSandboxPathInvalid)
		}
		return "", fmt.Errorf("%w: resolve destination parent: %v", ErrSandboxPathInvalid, err)
	}
	if err := ensureWithinRoot(rootPath, resolvedParentPath); err != nil {
		return "", err
	}
	return filepath.Join(resolvedParentPath, filepath.Base(absoluteCandidatePath)), nil
}

func ensureWithinRoot(rootPath string, targetPath string) error {
	rootAbsolutePath, err := filepath.Abs(rootPath)
	if err != nil {
		return fmt.Errorf("resolve sandbox root: %w", err)
	}
	resolvedRootPath, evalErr := filepath.EvalSymlinks(rootAbsolutePath)
	if evalErr != nil {
		return fmt.Errorf("%w: cannot resolve sandbox root path: %v", ErrSandboxPathInvalid, evalErr)
	}
	rootAbsolutePath = resolvedRootPath
	targetAbsolutePath, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("resolve target path: %w", err)
	}
	resolvedTargetPath, evalErr := filepath.EvalSymlinks(targetAbsolutePath)
	if evalErr != nil {
		return fmt.Errorf("%w: cannot resolve target path: %v", ErrSandboxPathInvalid, evalErr)
	}
	targetAbsolutePath = resolvedTargetPath
	relativePath, err := filepath.Rel(rootAbsolutePath, targetAbsolutePath)
	if err != nil {
		return fmt.Errorf("%w: compute relative path: %v", ErrSandboxPathOutsideRoot, err)
	}
	normalizedRelativePath := safety.NormalizePathForOSComparison(filepath.Clean(relativePath))
	if normalizedRelativePath == ".." || strings.HasPrefix(normalizedRelativePath, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: %s", ErrSandboxPathOutsideRoot, targetAbsolutePath)
	}
	return nil
}

func copyDirectoryNoSymlinksFromHandle(sourceDirectoryHandle *os.File, destinationDirectoryPath string) error {
	sourceDirectoryInfo, err := sourceDirectoryHandle.Stat()
	if err != nil {
		return fmt.Errorf("stat opened source directory: %w", err)
	}
	if !sourceDirectoryInfo.IsDir() {
		return fmt.Errorf("%w: source entry %s is not a directory", ErrSandboxPathInvalid, sourceDirectoryHandle.Name())
	}

	if err := os.MkdirAll(destinationDirectoryPath, 0o700); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	sourceDirectoryEntries, err := sourceDirectoryHandle.ReadDir(-1)
	if err != nil {
		return fmt.Errorf("read source directory: %w", err)
	}

	for _, sourceDirectoryEntry := range sourceDirectoryEntries {
		childName := sourceDirectoryEntry.Name()
		childSourceHandle, childSourceInfo, err := openChildPathReadOnlyNoFollow(sourceDirectoryHandle, childName, false)
		if err != nil {
			return wrapSandboxSourceOpenError(filepath.Join(sourceDirectoryHandle.Name(), childName), err)
		}
		childDestinationPath := filepath.Join(destinationDirectoryPath, childName)
		if childSourceInfo.IsDir() {
			if err := copyDirectoryNoSymlinksFromHandle(childSourceHandle, childDestinationPath); err != nil {
				_ = childSourceHandle.Close()
				return err
			}
			if err := childSourceHandle.Close(); err != nil {
				return fmt.Errorf("close copied source directory: %w", err)
			}
			continue
		}
		if err := copyFileNoSymlinksFromHandle(childSourceHandle, childSourceInfo, childDestinationPath); err != nil {
			_ = childSourceHandle.Close()
			return err
		}
		if err := childSourceHandle.Close(); err != nil {
			return fmt.Errorf("close copied source file: %w", err)
		}
	}

	return nil
}

func copyFileNoSymlinksFromHandle(sourceFileHandle *os.File, sourceFileInfo fs.FileInfo, destinationFilePath string) error {
	if sourceFileInfo.IsDir() {
		return fmt.Errorf("%w: source entry %s is a directory", ErrSandboxPathInvalid, sourceFileHandle.Name())
	}
	if !sourceFileInfo.Mode().IsRegular() {
		return fmt.Errorf("%w: source entry %s must be a regular file or directory", ErrSandboxPathInvalid, sourceFileHandle.Name())
	}
	if _, err := sourceFileHandle.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind source file: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destinationFilePath), 0o700); err != nil {
		return fmt.Errorf("create destination parent: %w", err)
	}
	destinationFile, err := os.OpenFile(destinationFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open destination file: %w", err)
	}
	defer func() { _ = destinationFile.Close() }()

	if _, err := io.Copy(destinationFile, sourceFileHandle); err != nil {
		return fmt.Errorf("copy file contents: %w", err)
	}
	if err := destinationFile.Sync(); err != nil {
		return fmt.Errorf("sync destination file: %w", err)
	}
	if err := destinationFile.Close(); err != nil {
		return fmt.Errorf("close destination file: %w", err)
	}
	return nil
}

func openSourcePathReadOnlyNoFollow(sourcePath string) (*os.File, fs.FileInfo, error) {
	canonicalSourcePath, err := filepath.Abs(filepath.Clean(sourcePath))
	if err != nil {
		return nil, nil, fmt.Errorf("resolve source path: %w", err)
	}
	resolvedSourceParentPath, err := filepath.EvalSymlinks(filepath.Dir(canonicalSourcePath))
	if err != nil {
		return nil, nil, wrapSandboxSourceOpenError(canonicalSourcePath, err)
	}
	resolvedSourcePath := filepath.Join(resolvedSourceParentPath, filepath.Base(canonicalSourcePath))
	sourceFileHandle, err := openAbsolutePathReadOnlyNoFollow(resolvedSourcePath, false)
	if err != nil {
		return nil, nil, wrapSandboxSourceOpenError(resolvedSourcePath, err)
	}
	sourceFileInfo, err := sourceFileHandle.Stat()
	if err != nil {
		_ = sourceFileHandle.Close()
		return nil, nil, fmt.Errorf("stat opened source path: %w", err)
	}
	return sourceFileHandle, sourceFileInfo, nil
}

func openAbsolutePathReadOnlyNoFollow(absolutePath string, requireDirectory bool) (*os.File, error) {
	cleanedAbsolutePath := filepath.Clean(absolutePath)
	if !filepath.IsAbs(cleanedAbsolutePath) {
		return nil, fmt.Errorf("path must be absolute")
	}

	rootDirectoryHandle, err := os.Open(string(filepath.Separator))
	if err != nil {
		return nil, fmt.Errorf("open filesystem root: %w", err)
	}

	pathParts := splitAbsolutePath(cleanedAbsolutePath)
	if len(pathParts) == 0 {
		return rootDirectoryHandle, nil
	}

	currentDirectoryHandle := rootDirectoryHandle

	for pathPartIndex, pathPart := range pathParts {
		openFlags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		isLastPart := pathPartIndex == len(pathParts)-1
		if !isLastPart || requireDirectory {
			openFlags |= unix.O_DIRECTORY
		}
		nextFD, err := unix.Openat(int(currentDirectoryHandle.Fd()), pathPart, openFlags, 0)
		if err != nil {
			_ = currentDirectoryHandle.Close()
			return nil, err
		}
		nextHandle := os.NewFile(uintptr(nextFD), filepath.Join(currentDirectoryHandle.Name(), pathPart))
		if err := currentDirectoryHandle.Close(); err != nil {
			_ = nextHandle.Close()
			return nil, fmt.Errorf("close traversed source directory: %w", err)
		}
		currentDirectoryHandle = nextHandle
	}

	return currentDirectoryHandle, nil
}

func openChildPathReadOnlyNoFollow(parentDirectoryHandle *os.File, childName string, requireDirectory bool) (*os.File, fs.FileInfo, error) {
	if childName == "" || childName == "." || childName == ".." || strings.ContainsRune(childName, os.PathSeparator) {
		return nil, nil, fmt.Errorf("invalid child entry name %q", childName)
	}

	openFlags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	if requireDirectory {
		openFlags |= unix.O_DIRECTORY
	}
	childFD, err := unix.Openat(int(parentDirectoryHandle.Fd()), childName, openFlags, 0)
	if err != nil {
		return nil, nil, err
	}
	childHandle := os.NewFile(uintptr(childFD), filepath.Join(parentDirectoryHandle.Name(), childName))
	childInfo, err := childHandle.Stat()
	if err != nil {
		_ = childHandle.Close()
		return nil, nil, fmt.Errorf("stat opened child entry: %w", err)
	}
	return childHandle, childInfo, nil
}

func wrapSandboxSourceOpenError(sourcePath string, err error) error {
	switch {
	case errors.Is(err, unix.ENOENT):
		return fmt.Errorf("%w: source path not found", ErrSandboxSourceUnavailable)
	case errors.Is(err, unix.ELOOP):
		return fmt.Errorf("%w: %s", ErrSymlinkNotAllowed, sourcePath)
	default:
		return fmt.Errorf("open source path: %w", err)
	}
}

func splitAbsolutePath(absolutePath string) []string {
	trimmedPath := strings.TrimPrefix(filepath.Clean(absolutePath), string(filepath.Separator))
	if trimmedPath == "" {
		return nil
	}
	return strings.Split(trimmedPath, string(filepath.Separator))
}

func sanitizeEntryName(rawName string) string {
	trimmedName := strings.TrimSpace(rawName)
	if trimmedName == "" {
		return ""
	}
	lowerName := strings.ToLower(filepath.Base(trimmedName))
	var builder strings.Builder
	lastWasSeparator := false
	for _, character := range lowerName {
		switch {
		case unicode.IsLetter(character), unicode.IsDigit(character):
			builder.WriteRune(character)
			lastWasSeparator = false
		case character == '.', character == '_', character == '-':
			builder.WriteRune(character)
			lastWasSeparator = false
		default:
			if !lastWasSeparator {
				builder.WriteRune('-')
				lastWasSeparator = true
			}
		}
		if builder.Len() >= 64 {
			break
		}
	}
	sanitizedName := strings.Trim(builder.String(), "-.")
	if sanitizedName == "" {
		return ""
	}
	return sanitizedName
}
