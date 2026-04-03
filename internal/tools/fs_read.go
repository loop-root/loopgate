package tools

import (
	"context"
	"fmt"
	"io"
)

const defaultMaxReadBytes = 2 * 1024 * 1024 // 2 MB

// FSRead reads file contents.
type FSRead struct {
	RepoRoot     string
	AllowedRoots []string
	DeniedPaths  []string
	MaxReadBytes int64
}

func (t *FSRead) Name() string      { return "fs_read" }
func (t *FSRead) Category() string  { return "filesystem" }
func (t *FSRead) Operation() string { return OpRead }

func (t *FSRead) Schema() Schema {
	return Schema{
		Description: "Read the contents of a file",
		Args: []ArgDef{
			{
				Name:        "path",
				Description: "Path to the file to read (relative to repo root or absolute)",
				Required:    true,
				Type:        "path",
			},
		},
	}
}

func (t *FSRead) Execute(ctx context.Context, args map[string]string) (string, error) {
	path := args["path"]
	validatedPath, err := resolveValidatedPath(t.RepoRoot, t.AllowedRoots, t.DeniedPaths, path)
	if err != nil {
		return "", fmt.Errorf("path denied: %w", err)
	}

	fileHandle, err := openResolvedPathReadOnly(validatedPath, false)
	if err != nil {
		return "", fmt.Errorf("cannot access: %w", err)
	}
	defer fileHandle.Close()

	maxBytes := t.MaxReadBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxReadBytes
	}
	limitedReader := io.LimitReader(fileHandle, maxBytes+1)
	content, err := io.ReadAll(limitedReader)
	if err != nil {
		return "", fmt.Errorf("read error: %w", err)
	}
	if int64(len(content)) > maxBytes {
		return "", fmt.Errorf("file exceeds maximum read size (%d bytes)", maxBytes)
	}

	return string(content), nil
}
