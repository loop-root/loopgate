package agentdocs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type surfaceManifest struct {
	Version  string         `yaml:"version"`
	Updated  string         `yaml:"updated"`
	Surfaces []agentSurface `yaml:"surfaces"`
}

type agentSurface struct {
	ID        string           `yaml:"id"`
	Kind      string           `yaml:"kind"`
	Command   string           `yaml:"command"`
	Source    string           `yaml:"source"`
	Stability string           `yaml:"stability"`
	Audience  []string         `yaml:"audience"`
	Authority surfaceAuthority `yaml:"authority"`
	Skill     string           `yaml:"skill"`
	Docs      []string         `yaml:"docs"`
}

type surfaceAuthority struct {
	Mode                    string   `yaml:"mode"`
	RequiresRunningLoopgate bool     `yaml:"requires_running_loopgate"`
	WritesState             bool     `yaml:"writes_state"`
	ControlCapabilities     []string `yaml:"control_capabilities"`
}

func TestAgentSurfacesManifestIsUsable(t *testing.T) {
	root := findRepoRoot(t)
	manifestPath := filepath.Join(root, "docs", "agent", "agent_surfaces.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read agent surfaces manifest: %v", err)
	}

	var manifest surfaceManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse agent surfaces manifest: %v", err)
	}

	if manifest.Version == "" {
		t.Fatal("manifest version is required")
	}
	if manifest.Updated == "" {
		t.Fatal("manifest updated date is required")
	}
	if len(manifest.Surfaces) == 0 {
		t.Fatal("manifest must describe at least one agent surface")
	}

	seenIDs := map[string]bool{}
	for _, surface := range manifest.Surfaces {
		if surface.ID == "" {
			t.Fatal("surface id is required")
		}
		if seenIDs[surface.ID] {
			t.Fatalf("duplicate surface id %q", surface.ID)
		}
		seenIDs[surface.ID] = true

		requireField(t, surface.ID, "kind", surface.Kind)
		requireField(t, surface.ID, "command", surface.Command)
		requireField(t, surface.ID, "source", surface.Source)
		requireField(t, surface.ID, "stability", surface.Stability)
		requireField(t, surface.ID, "authority.mode", surface.Authority.Mode)
		if len(surface.Audience) == 0 {
			t.Fatalf("%s audience is required", surface.ID)
		}
		if surface.Skill == "" {
			t.Fatalf("%s skill path is required", surface.ID)
		}
		if len(surface.Docs) == 0 {
			t.Fatalf("%s docs list is required", surface.ID)
		}

		requireRepoPathExists(t, root, surface.ID, "source", surface.Source)
		requireRepoPathExists(t, root, surface.ID, "skill", surface.Skill)
		for _, docPath := range surface.Docs {
			requireRepoPathExists(t, root, surface.ID, "doc", docPath)
		}
	}
}

func requireField(t *testing.T, surfaceID, name, value string) {
	t.Helper()
	if strings.TrimSpace(value) == "" {
		t.Fatalf("%s %s is required", surfaceID, name)
	}
}

func requireRepoPathExists(t *testing.T, root, surfaceID, fieldName, relPath string) {
	t.Helper()
	if filepath.IsAbs(relPath) {
		t.Fatalf("%s %s path must be relative: %s", surfaceID, fieldName, relPath)
	}
	clean := filepath.Clean(relPath)
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		t.Fatalf("%s %s path escapes repo: %s", surfaceID, fieldName, relPath)
	}
	fullPath := filepath.Join(root, clean)
	if _, err := os.Stat(fullPath); err != nil {
		t.Fatalf("%s %s path %s is not readable: %v", surfaceID, fieldName, relPath, err)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			t.Fatal("could not find repo root containing go.mod")
		}
		wd = parent
	}
}
