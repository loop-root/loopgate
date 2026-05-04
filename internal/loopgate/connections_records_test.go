package loopgate

import (
	"context"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loopgate/internal/secrets"
)

func TestRegisterConnection_PersistsSecretRefOnly(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	t.Setenv("LOOPGATE_GITHUB_TOKEN", "ghp-super-secret-value")

	connectionStatus, err := server.RegisterConnection(context.Background(), connectionRegistration{
		Provider:  "github",
		GrantType: controlapipkg.GrantTypeClientCredentials,
		Subject:   "repo-bot",
		Scopes:    []string{"repo.read", "repo.read"},
		Credential: secrets.SecretRef{
			ID:          "github-bot-token",
			Backend:     secrets.BackendEnv,
			AccountName: "LOOPGATE_GITHUB_TOKEN",
			Scope:       "github.repo_read",
		},
	})
	if err != nil {
		t.Fatalf("register connection: %v", err)
	}
	if connectionStatus.Provider != "github" {
		t.Fatalf("unexpected connection status: %#v", connectionStatus)
	}
	if connectionStatus.SecureStoreRefID != "github-bot-token" {
		t.Fatalf("unexpected secure store ref id: %#v", connectionStatus)
	}

	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(status.Connections) != 1 {
		t.Fatalf("expected one connection in status, got %#v", status.Connections)
	}

	connectionPath := filepath.Join(repoRoot, "runtime", "state", "loopgate_connections.json")
	connectionBytes, err := os.ReadFile(connectionPath)
	if err != nil {
		t.Fatalf("read connection file: %v", err)
	}
	connectionText := string(connectionBytes)
	if strings.Contains(connectionText, "ghp-super-secret-value") {
		t.Fatalf("connection state leaked raw secret: %s", connectionText)
	}
	if !strings.Contains(connectionText, `"account_name": "LOOPGATE_GITHUB_TOKEN"`) {
		t.Fatalf("expected connection file to persist only the secret ref metadata: %s", connectionText)
	}
}

func TestConnectionRecordKey_DistinguishesColonContainingIdentifiers(t *testing.T) {
	leftKey := connectionRecordKey("a:b", "c")
	rightKey := connectionRecordKey("a", "b:c")

	if leftKey == rightKey {
		t.Fatalf("expected distinct connection keys for colon-containing identifiers, got %q", leftKey)
	}
}

func TestLoadConnectionRecords_DoesNotCollideColonContainingIdentifiers(t *testing.T) {
	connectionPath := filepath.Join(t.TempDir(), "connections.json")
	connectionJSON := `{
  "connections": [
    {
      "provider": "a:b",
      "grant_type": "public_read",
      "subject": "c",
      "scopes": ["read"],
      "credential": {},
      "status": "ready",
      "created_at_utc": "2026-04-24T00:00:00Z"
    },
    {
      "provider": "a",
      "grant_type": "public_read",
      "subject": "b:c",
      "scopes": ["read"],
      "credential": {},
      "status": "ready",
      "created_at_utc": "2026-04-24T00:00:01Z"
    }
  ]
}`
	if err := os.WriteFile(connectionPath, []byte(connectionJSON), 0o600); err != nil {
		t.Fatalf("write connection records: %v", err)
	}

	connectionRecords, err := loadConnectionRecords(connectionPath)
	if err != nil {
		t.Fatalf("load connection records: %v", err)
	}
	if len(connectionRecords) != 2 {
		t.Fatalf("expected two non-colliding connection records, got %#v", connectionRecords)
	}
}

func TestConnectionStatuses_IncludesConfiguredPublicReadConnections(t *testing.T) {
	repoRoot := t.TempDir()
	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = writer.Write([]byte("<html><head><title>Operational</title></head><body>ok</body></html>"))
	}))
	defer providerServer.Close()

	writeConfiguredPublicHTMLMetaYAML(t, repoRoot, providerServer.URL)
	_, status, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	if len(status.Connections) != 1 {
		t.Fatalf("expected one configured public connection, got %#v", status.Connections)
	}
	if status.Connections[0].GrantType != controlapipkg.GrantTypePublicRead {
		t.Fatalf("unexpected connection summary: %#v", status.Connections[0])
	}
	if status.Connections[0].SecureStoreRefID != "none" {
		t.Fatalf("expected no secret ref for public connection, got %#v", status.Connections[0])
	}
	if status.Connections[0].Status != "public_configured" {
		t.Fatalf("unexpected public connection status: %#v", status.Connections[0])
	}
}

func TestSaveConnectionRecords_UsesUniqueTempPath(t *testing.T) {
	connectionPath := filepath.Join(t.TempDir(), "runtime", "state", "loopgate_connections.json")
	if err := os.MkdirAll(filepath.Dir(connectionPath), 0o700); err != nil {
		t.Fatalf("mkdir connection dir: %v", err)
	}
	fixedTempPath := connectionPath + ".tmp"
	if err := os.MkdirAll(fixedTempPath, 0o700); err != nil {
		t.Fatalf("mkdir fixed temp blocker: %v", err)
	}

	connectionRecords := map[string]connectionRecord{
		connectionRecordKey("github", "repo-bot"): {
			Provider:  "github",
			GrantType: controlapipkg.GrantTypeClientCredentials,
			Subject:   "repo-bot",
			Scopes:    []string{"repo.read"},
			Credential: secrets.SecretRef{
				ID:          "github-bot-token",
				Backend:     secrets.BackendEnv,
				AccountName: "LOOPGATE_GITHUB_TOKEN",
				Scope:       "github.repo_read",
			},
		},
	}

	if err := saveConnectionRecords(connectionPath, connectionRecords); err != nil {
		t.Fatalf("save connection records: %v", err)
	}

	loadedRecords, err := loadConnectionRecords(connectionPath)
	if err != nil {
		t.Fatalf("load connection records: %v", err)
	}
	if len(loadedRecords) != 1 {
		t.Fatalf("expected one saved connection record, got %#v", loadedRecords)
	}
	if _, err := os.Stat(fixedTempPath); err != nil {
		t.Fatalf("expected fixed temp blocker to remain untouched, got %v", err)
	}
}
