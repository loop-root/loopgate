package tools

import (
	"context"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// FSMkdir creates a directory inside the sandbox.
type FSMkdir struct {
	RepoRoot     string
	AllowedRoots []string
	DeniedPaths  []string
}

func (t *FSMkdir) Name() string      { return "fs_mkdir" }
func (t *FSMkdir) Category() string  { return "filesystem" }
func (t *FSMkdir) Operation() string { return OpWrite }

func (t *FSMkdir) Schema() Schema {
	return Schema{
		Description: "Create a directory (parent directory must already exist)",
		Args: []ArgDef{
			{
				Name:        "path",
				Description: "Path to the directory to create (relative to home or absolute)",
				Required:    true,
				Type:        "path",
			},
		},
	}
}

func (t *FSMkdir) Execute(ctx context.Context, args map[string]string) (string, error) {
	path := args["path"]

	validated, err := resolveValidatedPath(t.RepoRoot, t.AllowedRoots, t.DeniedPaths, path)
	if err != nil {
		return "", fmt.Errorf("path denied: %w", err)
	}

	if validated.InputPathIsSymlink {
		return "", fmt.Errorf("creating directories through a symlink path is denied")
	}

	// If directory already exists, succeed silently.
	if info, statErr := os.Stat(validated.ResolvedPath); statErr == nil {
		if info.IsDir() {
			return fmt.Sprintf("directory already exists: %s", path), nil
		}
		return "", fmt.Errorf("path exists and is not a directory")
	}

	// Open the parent directory with no-follow semantics and create the
	// target directory via mkdirat to prevent symlink races.
	parentHandle, baseName, err := openParentDirectoryNoFollowForWrite(validated)
	if err != nil {
		return "", fmt.Errorf("mkdir open parent: %w", err)
	}
	defer parentHandle.Close()

	if err := unix.Mkdirat(int(parentHandle.Fd()), baseName, 0o700); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}

	return fmt.Sprintf("created directory: %s", path), nil
}
