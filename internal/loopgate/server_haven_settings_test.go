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

	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	pinTestProcessAsExpectedClient(t, server)
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

func TestHavenSettingsIdleReadDoesNotExposePrefsPath(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := filepath.Join(repoRoot, "runtime", "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	prefsPath := filepath.Join(stateDir, "haven_preferences.json")
	if err := os.Mkdir(prefsPath, 0o700); err != nil {
		t.Fatalf("mkdir prefs path as directory: %v", err)
	}

	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	pinTestProcessAsExpectedClient(t, server)
	havenClient := NewClient(client.socketPath)
	havenClient.ConfigureSession("haven", "idle-settings-path-redaction", advertisedSessionCapabilityNames(status))
	token, err := havenClient.ensureCapabilityToken(context.Background())
	if err != nil {
		t.Fatalf("ensure haven token: %v", err)
	}

	var response havenIdleSettingsResponse
	err = havenClient.doJSON(context.Background(), http.MethodGet, "/v1/settings/idle", token, nil, &response, nil)
	if err == nil {
		t.Fatal("expected idle settings read to fail when prefs path is unreadable")
	}
	if !strings.Contains(err.Error(), havenIdleSettingsLoadFailureText) {
		t.Fatalf("expected stable load failure text, got %v", err)
	}
	if strings.Contains(err.Error(), prefsPath) || strings.Contains(err.Error(), "haven_preferences.json") {
		t.Fatalf("expected prefs path to stay redacted, got %v", err)
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
