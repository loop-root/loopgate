package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// FSList lists directory contents.
type FSList struct {
	RepoRoot     string
	AllowedRoots []string
	DeniedPaths  []string
}

func (t *FSList) Name() string      { return "fs_list" }
func (t *FSList) Category() string  { return "filesystem" }
func (t *FSList) Operation() string { return OpRead }

func (t *FSList) Schema() Schema {
	return Schema{
		Description: "List contents of a directory",
		Args: []ArgDef{
			{
				Name:        "path",
				Description: "Path to the directory to list (relative to repo root or absolute)",
				Required:    true,
				Type:        "path",
			},
		},
	}
}

func (t *FSList) Execute(ctx context.Context, args map[string]string) (string, error) {
	path := args["path"]
	validatedPath, err := resolveValidatedPath(t.RepoRoot, t.AllowedRoots, t.DeniedPaths, path)
	if err != nil {
		return "", fmt.Errorf("path denied: %w", err)
	}

	directoryHandle, err := openResolvedPathReadOnly(validatedPath, true)
	if err != nil {
		return "", fmt.Errorf("cannot access: %w", err)
	}
	defer directoryHandle.Close()

	entries, err := directoryHandle.ReadDir(-1)
	if err != nil {
		return "", fmt.Errorf("cannot read directory: %w", err)
	}

	// Format output
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Strings(names)

	if len(names) == 0 {
		return "(empty directory)", nil
	}

	return strings.Join(names, "\n"), nil
}
