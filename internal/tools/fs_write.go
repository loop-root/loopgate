package tools

import (
	"context"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// FSWrite writes content to a file.
type FSWrite struct {
	RepoRoot     string
	AllowedRoots []string
	DeniedPaths  []string
}

func (t *FSWrite) Name() string      { return "fs_write" }
func (t *FSWrite) Category() string  { return "filesystem" }
func (t *FSWrite) Operation() string { return OpWrite }

func (t *FSWrite) Schema() Schema {
	return Schema{
		Description: "Write content to a file (parent directory must already exist)",
		Args: []ArgDef{
			{
				Name:        "path",
				Description: "Path to the file to write (relative to repo root or absolute)",
				Required:    true,
				Type:        "path",
			},
			{
				Name:        "content",
				Description: "Content to write to the file",
				Required:    true,
				Type:        "string",
			},
		},
	}
}

func (t *FSWrite) Execute(ctx context.Context, args map[string]string) (string, error) {
	path := args["path"]
	content := args["content"]

	validatedPath, err := resolveValidatedPath(t.RepoRoot, t.AllowedRoots, t.DeniedPaths, path)
	if err != nil {
		return "", fmt.Errorf("path denied: %w", err)
	}

	// Check if target is an existing directory
	if fileInfo, err := os.Stat(validatedPath.ResolvedPath); err == nil && fileInfo.IsDir() {
		return "", fmt.Errorf("path is a directory")
	}

	fileHandle, err := openFileNoFollowForWrite(validatedPath)
	if err != nil {
		return "", fmt.Errorf("write open error: %w", err)
	}
	defer fileHandle.Close()

	if _, err := io.WriteString(fileHandle, content); err != nil {
		return "", fmt.Errorf("write error: %w", err)
	}
	if err := fileHandle.Sync(); err != nil {
		return "", fmt.Errorf("write sync error: %w", err)
	}

	return fmt.Sprintf("wrote %d bytes to %s", len(content), path), nil
}

func openFileNoFollowForWrite(validatedPath validatedPath) (*os.File, error) {
	parentDirectoryHandle, baseName, err := openParentDirectoryNoFollowForWrite(validatedPath)
	if err != nil {
		return nil, err
	}
	defer parentDirectoryHandle.Close()

	targetFD, err := unix.Openat(
		int(parentDirectoryHandle.Fd()),
		baseName,
		unix.O_WRONLY|unix.O_CREAT|unix.O_TRUNC|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0o600,
	)
	if err != nil {
		return nil, fmt.Errorf("open target with no-follow: %w", err)
	}

	return os.NewFile(uintptr(targetFD), validatedPath.ResolvedPath), nil
}
