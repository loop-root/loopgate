package loopgate

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHavenSettingsIdleFailsClosedOnCorruptPrefs(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := filepath.Join(repoRoot, "runtime", "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "haven_preferences.json"), []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write corrupt prefs: %v", err)
	}

	client, status, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	havenClient := NewClient(client.socketPath)
	havenClient.ConfigureSession("haven", "idle-settings-test", advertisedSessionCapabilityNames(status))
	token, err := havenClient.ensureCapabilityToken(context.Background())
	if err != nil {
		t.Fatalf("ensure haven token: %v", err)
	}

	var response havenIdleSettingsResponse
	if err := havenClient.doJSON(context.Background(), http.MethodGet, "/v1/settings/idle", token, nil, &response, nil); err == nil || !strings.Contains(err.Error(), DenialCodeExecutionFailed) {
		t.Fatalf("expected corrupt preferences to fail closed, got %v", err)
	}
}

func TestWriteIdleSettingsUses0600PrefsFile(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	if err := server.writeIdleSettings(true, false); err != nil {
		t.Fatalf("write idle settings: %v", err)
	}

	pathInfo, err := os.Stat(filepath.Join(repoRoot, "runtime", "state", "haven_preferences.json"))
	if err != nil {
		t.Fatalf("stat prefs file: %v", err)
	}
	if got, want := pathInfo.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("prefs mode = %v want %v", got, want)
	}
}
