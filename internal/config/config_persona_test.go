package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPersona_StrictRejectsUnknownField(t *testing.T) {
	repoRoot := t.TempDir()
	personaPath := filepath.Join(repoRoot, "persona", "default.yaml")
	if err := os.MkdirAll(filepath.Dir(personaPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rawPersona := `name: Operator
version: 0.1.0
defaults:
  tone: helpful
unknown_field: true
`
	if err := os.WriteFile(personaPath, []byte(rawPersona), 0o600); err != nil {
		t.Fatalf("write persona: %v", err)
	}

	_, err := LoadPersona(repoRoot)
	if err == nil {
		t.Fatal("expected strict decode error for unknown persona field, got nil")
	}
}

func TestLoadPersona_MissingFileGetsSecureDefaults(t *testing.T) {
	repoRoot := t.TempDir()

	persona, err := LoadPersona(repoRoot)
	if err != nil {
		t.Fatalf("load default persona: %v", err)
	}
	if !persona.Trust.TreatModelOutputAsUntrusted {
		t.Fatal("expected default persona to treat model output as untrusted")
	}
	if !persona.HallucinationControls.RefuseToInventFacts {
		t.Fatal("expected default persona to refuse inventing facts")
	}
	if !persona.RiskControls.RequireExplicitApprovalFor.FilesystemWrites {
		t.Fatal("expected default persona to require approval for filesystem writes")
	}
	if persona.Defaults.PreferredResponseFormat == "" {
		t.Fatal("expected default preferred response format")
	}
}
