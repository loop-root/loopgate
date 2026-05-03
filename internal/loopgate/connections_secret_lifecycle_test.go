package loopgate

import (
	"context"
	"errors"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"loopgate/internal/ledger"
	"loopgate/internal/secrets"
)

func TestResolveConnectionSecret_UsesLoopgateOwnedReference(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	t.Setenv("LOOPGATE_SLACK_TOKEN", "slack-test-credential")

	_, err := server.RegisterConnection(context.Background(), connectionRegistration{
		Provider:  "slack",
		GrantType: controlapipkg.GrantTypeClientCredentials,
		Subject:   "workspace-bot",
		Scopes:    []string{"chat.write"},
		Credential: secrets.SecretRef{
			ID:          "slack-bot-token",
			Backend:     secrets.BackendEnv,
			AccountName: "LOOPGATE_SLACK_TOKEN",
			Scope:       "slack.chat_write",
		},
	})
	if err != nil {
		t.Fatalf("register connection: %v", err)
	}

	rawSecret, secretMetadata, connectionStatus, err := server.ResolveConnectionSecret(context.Background(), "slack", "workspace-bot")
	if err != nil {
		t.Fatalf("resolve connection secret: %v", err)
	}
	if string(rawSecret) != "slack-test-credential" {
		t.Fatalf("unexpected resolved secret: %q", string(rawSecret))
	}
	if connectionStatus.Provider != "slack" || connectionStatus.Subject != "workspace-bot" {
		t.Fatalf("unexpected connection status after resolve: %#v", connectionStatus)
	}
	if secretMetadata.Fingerprint == "" || strings.Contains(secretMetadata.Fingerprint, "slack-test-credential") {
		t.Fatalf("unexpected secret metadata fingerprint: %#v", secretMetadata)
	}
	if strings.TrimSpace(connectionStatus.LastUsedAtUTC) == "" {
		t.Fatalf("expected last_used_at_utc to be recorded after resolution: %#v", connectionStatus)
	}
}

func TestRegisterConnection_FailsClosedOnUnavailableSecureBackend(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	_, err := server.RegisterConnection(context.Background(), connectionRegistration{
		Provider:  "notion",
		GrantType: controlapipkg.GrantTypeAuthorizationCode,
		Subject:   "workspace-1",
		Scopes:    []string{"pages.read"},
		Credential: secrets.SecretRef{
			ID:          "notion-refresh",
			Backend:     secrets.BackendMacOSKeychain,
			AccountName: "loopgate.notion.workspace-1",
			Scope:       "notion.pages_read",
		},
	})
	if runtime.GOOS == "darwin" {
		if !errors.Is(err, secrets.ErrSecretNotFound) {
			t.Fatalf("expected missing keychain item error on darwin, got %v", err)
		}
	} else if !errors.Is(err, secrets.ErrSecretBackendUnavailable) {
		t.Fatalf("expected secure backend unavailable error, got %v", err)
	}

	if len(server.connectionStatuses()) != 0 {
		t.Fatalf("expected no connection record on failed registration, got %#v", server.connectionStatuses())
	}
}

func TestUpsertConnectionCredential_PersistsOnlySecretRefMetadata(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	fakeStore := &fakeConnectionSecretStore{}
	server.resolveSecretStore = func(validatedRef secrets.SecretRef) (secrets.SecretStore, error) {
		return fakeStore, nil
	}

	connectionStatus, err := server.UpsertConnectionCredential(context.Background(), connectionRegistration{
		Provider:  "github",
		GrantType: controlapipkg.GrantTypeClientCredentials,
		Subject:   "repo-bot",
		Scopes:    []string{"repo.read"},
		Credential: secrets.SecretRef{
			ID:          "github-bot-token",
			Backend:     secrets.BackendSecure,
			AccountName: "loopgate.github.repo-bot",
			Scope:       "github.repo_read",
		},
	}, []byte("ghp-super-secret-value"))
	if err != nil {
		t.Fatalf("upsert connection credential: %v", err)
	}
	if connectionStatus.Status != "stored" {
		t.Fatalf("unexpected connection status: %#v", connectionStatus)
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
	if !strings.Contains(connectionText, `"id": "github-bot-token"`) {
		t.Fatalf("expected persisted secure-store ref metadata only: %s", connectionText)
	}
	if storedSecret := string(fakeStore.storedSecret["github-bot-token"]); storedSecret != "ghp-super-secret-value" {
		t.Fatalf("expected secret to be stored in backend only, got %q", storedSecret)
	}
}

func TestUpsertConnectionCredential_AuditFailureSurfacesSecretCleanupError(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	fakeStore := &fakeConnectionSecretStore{deleteErr: errors.New("delete failed")}
	server.resolveSecretStore = func(validatedRef secrets.SecretRef) (secrets.SecretStore, error) {
		return fakeStore, nil
	}

	appendAuditEvent := server.appendAuditEvent
	server.appendAuditEvent = func(path string, ledgerEvent ledger.Event) error {
		if ledgerEvent.Type == "connection.credential_upserted" {
			return errors.New("audit unavailable")
		}
		return appendAuditEvent(path, ledgerEvent)
	}

	_, err := server.UpsertConnectionCredential(context.Background(), connectionRegistration{
		Provider:  "github",
		GrantType: controlapipkg.GrantTypeClientCredentials,
		Subject:   "repo-bot",
		Scopes:    []string{"repo.read"},
		Credential: secrets.SecretRef{
			ID:          "github-bot-token",
			Backend:     secrets.BackendSecure,
			AccountName: "loopgate.github.repo-bot",
			Scope:       "github.repo_read",
		},
	}, []byte("ghp-super-secret-value"))
	if err == nil {
		t.Fatal("expected upsert to fail closed when audit is unavailable")
	}
	if !strings.Contains(err.Error(), "audit unavailable") {
		t.Fatalf("expected audit failure in error, got %v", err)
	}
	if !strings.Contains(err.Error(), "delete failed") {
		t.Fatalf("expected secret cleanup failure in error, got %v", err)
	}
}

func TestValidateConnection_UpdatesStatusAndValidationTimestamp(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	fakeStore := &fakeConnectionSecretStore{
		storedSecret: map[string][]byte{
			"slack-bot-token": []byte("slack-test-credential"),
		},
		metadata: map[string]secrets.SecretMetadata{
			"slack-bot-token": {
				Status:        "validated",
				Scope:         "slack.chat_write",
				LastRotatedAt: time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC),
			},
		},
	}
	server.resolveSecretStore = func(validatedRef secrets.SecretRef) (secrets.SecretStore, error) {
		return fakeStore, nil
	}

	_, err := server.RegisterConnection(context.Background(), connectionRegistration{
		Provider:  "slack",
		GrantType: controlapipkg.GrantTypeClientCredentials,
		Subject:   "workspace-bot",
		Scopes:    []string{"chat.write"},
		Credential: secrets.SecretRef{
			ID:          "slack-bot-token",
			Backend:     secrets.BackendSecure,
			AccountName: "loopgate.slack.workspace-bot",
			Scope:       "slack.chat_write",
		},
	})
	if err != nil {
		t.Fatalf("register connection: %v", err)
	}

	connectionStatus, err := server.ValidateConnection(context.Background(), "slack", "workspace-bot")
	if err != nil {
		t.Fatalf("validate connection: %v", err)
	}
	if connectionStatus.Status != "validated" {
		t.Fatalf("expected validated status, got %#v", connectionStatus)
	}
	if strings.TrimSpace(connectionStatus.LastValidatedAtUTC) == "" {
		t.Fatalf("expected validation timestamp in status summary, got %#v", connectionStatus)
	}
	if strings.TrimSpace(connectionStatus.LastRotatedAtUTC) == "" {
		t.Fatalf("expected rotation timestamp in status summary, got %#v", connectionStatus)
	}

	server.connectionRuntime.mu.Lock()
	updatedRecord := server.connectionRuntime.records[connectionRecordKey("slack", "workspace-bot")]
	server.connectionRuntime.mu.Unlock()
	if strings.TrimSpace(updatedRecord.LastValidatedAtUTC) == "" {
		t.Fatalf("expected validation timestamp to be recorded, got %#v", updatedRecord)
	}
	if updatedRecord.LastRotatedAtUTC == "" {
		t.Fatalf("expected rotated timestamp to be retained, got %#v", updatedRecord)
	}
}
