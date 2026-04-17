package loopgate

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"loopgate/internal/ledger"
	"loopgate/internal/secrets"
)

type fakeConnectionSecretStore struct {
	storedSecret map[string][]byte
	metadata     map[string]secrets.SecretMetadata
	putErr       error
	getErr       error
	deleteErr    error
	putCalls     int
}

func (fakeStore *fakeConnectionSecretStore) Put(ctx context.Context, validatedRef secrets.SecretRef, rawSecret []byte) (secrets.SecretMetadata, error) {
	_ = ctx
	if fakeStore.putErr != nil {
		return secrets.SecretMetadata{}, fakeStore.putErr
	}
	if fakeStore.storedSecret == nil {
		fakeStore.storedSecret = make(map[string][]byte)
	}
	if fakeStore.metadata == nil {
		fakeStore.metadata = make(map[string]secrets.SecretMetadata)
	}
	fakeStore.putCalls++
	fakeStore.storedSecret[validatedRef.ID] = append([]byte(nil), rawSecret...)
	secretMetadata := secrets.SecretMetadata{
		CreatedAt:     time.Date(2026, 3, 8, 1, 0, 0, 0, time.UTC),
		LastRotatedAt: time.Date(2026, 3, 8, 1, 0, 0, 0, time.UTC),
		Status:        "stored",
		Scope:         validatedRef.Scope,
		Fingerprint:   "fakefingerprint01",
	}
	fakeStore.metadata[validatedRef.ID] = secretMetadata
	return secretMetadata, nil
}

func (fakeStore *fakeConnectionSecretStore) Get(ctx context.Context, validatedRef secrets.SecretRef) ([]byte, secrets.SecretMetadata, error) {
	_ = ctx
	if fakeStore.getErr != nil {
		return nil, secrets.SecretMetadata{}, fakeStore.getErr
	}
	rawSecret, found := fakeStore.storedSecret[validatedRef.ID]
	if !found {
		return nil, secrets.SecretMetadata{}, secrets.ErrSecretNotFound
	}
	secretMetadata := fakeStore.metadata[validatedRef.ID]
	secretMetadata.LastUsedAt = time.Date(2026, 3, 8, 1, 30, 0, 0, time.UTC)
	return append([]byte(nil), rawSecret...), secretMetadata, nil
}

func (fakeStore *fakeConnectionSecretStore) Delete(ctx context.Context, validatedRef secrets.SecretRef) error {
	_ = ctx
	if fakeStore.deleteErr != nil {
		return fakeStore.deleteErr
	}
	delete(fakeStore.storedSecret, validatedRef.ID)
	delete(fakeStore.metadata, validatedRef.ID)
	return nil
}

func (fakeStore *fakeConnectionSecretStore) Metadata(ctx context.Context, validatedRef secrets.SecretRef) (secrets.SecretMetadata, error) {
	_ = ctx
	secretMetadata, found := fakeStore.metadata[validatedRef.ID]
	if !found {
		return secrets.SecretMetadata{}, secrets.ErrSecretNotFound
	}
	return secretMetadata, nil
}

func TestRegisterConnection_PersistsSecretRefOnly(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	t.Setenv("LOOPGATE_GITHUB_TOKEN", "ghp-super-secret-value")

	connectionStatus, err := server.RegisterConnection(context.Background(), connectionRegistration{
		Provider:  "github",
		GrantType: GrantTypeClientCredentials,
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

func TestResolveConnectionSecret_UsesLoopgateOwnedReference(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	t.Setenv("LOOPGATE_SLACK_TOKEN", "xoxb-loopgate-secret")

	_, err := server.RegisterConnection(context.Background(), connectionRegistration{
		Provider:  "slack",
		GrantType: GrantTypeClientCredentials,
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
	if string(rawSecret) != "xoxb-loopgate-secret" {
		t.Fatalf("unexpected resolved secret: %q", string(rawSecret))
	}
	if connectionStatus.Provider != "slack" || connectionStatus.Subject != "workspace-bot" {
		t.Fatalf("unexpected connection status after resolve: %#v", connectionStatus)
	}
	if secretMetadata.Fingerprint == "" || strings.Contains(secretMetadata.Fingerprint, "xoxb-loopgate-secret") {
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
		GrantType: GrantTypeAuthorizationCode,
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
	if status.Connections[0].GrantType != GrantTypePublicRead {
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
			GrantType: GrantTypeClientCredentials,
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

func TestUpsertConnectionCredential_PersistsOnlySecretRefMetadata(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	fakeStore := &fakeConnectionSecretStore{}
	server.resolveSecretStore = func(validatedRef secrets.SecretRef) (secrets.SecretStore, error) {
		return fakeStore, nil
	}

	connectionStatus, err := server.UpsertConnectionCredential(context.Background(), connectionRegistration{
		Provider:  "github",
		GrantType: GrantTypeClientCredentials,
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
		GrantType: GrantTypeClientCredentials,
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
			"slack-bot-token": []byte("xoxb-loopgate-secret"),
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
		GrantType: GrantTypeClientCredentials,
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

func TestRotateConnectionCredential_OverwritesExistingSecretSafely(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	fakeStore := &fakeConnectionSecretStore{}
	server.resolveSecretStore = func(validatedRef secrets.SecretRef) (secrets.SecretStore, error) {
		return fakeStore, nil
	}

	registration := connectionRegistration{
		Provider:  "github",
		GrantType: GrantTypeClientCredentials,
		Subject:   "repo-bot",
		Scopes:    []string{"repo.read"},
		Credential: secrets.SecretRef{
			ID:          "github-bot-token",
			Backend:     secrets.BackendSecure,
			AccountName: "loopgate.github.repo-bot",
			Scope:       "github.repo_read",
		},
	}

	if _, err := server.UpsertConnectionCredential(context.Background(), registration, []byte("ghp-initial-secret")); err != nil {
		t.Fatalf("first upsert connection credential: %v", err)
	}

	connectionStatus, err := server.RotateConnectionCredential(context.Background(), registration, []byte("ghp-rotated-secret"))
	if err != nil {
		t.Fatalf("rotate connection credential: %v", err)
	}
	if connectionStatus.Status != "stored" {
		t.Fatalf("unexpected rotated connection status: %#v", connectionStatus)
	}
	if storedSecret := string(fakeStore.storedSecret["github-bot-token"]); storedSecret != "ghp-rotated-secret" {
		t.Fatalf("expected rotated secret to be stored, got %q", storedSecret)
	}
	if fakeStore.putCalls < 2 {
		t.Fatalf("expected rotation to perform a second backend write, got %d put calls", fakeStore.putCalls)
	}
}

func TestRotateConnectionCredential_InvalidatesCachedProviderToken(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	fakeStore := &fakeConnectionSecretStore{}
	server.resolveSecretStore = func(validatedRef secrets.SecretRef) (secrets.SecretStore, error) {
		return fakeStore, nil
	}

	registration := connectionRegistration{
		Provider:  "github",
		GrantType: GrantTypeClientCredentials,
		Subject:   "repo-bot",
		Scopes:    []string{"repo.read"},
		Credential: secrets.SecretRef{
			ID:          "github-bot-token",
			Backend:     secrets.BackendSecure,
			AccountName: "loopgate.github.repo-bot",
			Scope:       "github.repo_read",
		},
	}

	if _, err := server.UpsertConnectionCredential(context.Background(), registration, []byte("ghp-initial-secret")); err != nil {
		t.Fatalf("upsert connection credential: %v", err)
	}

	connectionKey := connectionRecordKey("github", "repo-bot")
	server.providerRuntime.mu.Lock()
	server.providerRuntime.tokens[connectionKey] = providerAccessToken{
		ConnectionKey: connectionKey,
		AccessToken:   "cached-access-token",
		TokenType:     "Bearer",
		ExpiresAt:     time.Now().UTC().Add(5 * time.Minute),
	}
	server.providerRuntime.mu.Unlock()

	if _, err := server.RotateConnectionCredential(context.Background(), registration, []byte("ghp-rotated-secret")); err != nil {
		t.Fatalf("rotate connection credential: %v", err)
	}

	server.providerRuntime.mu.Lock()
	_, foundCachedToken := server.providerRuntime.tokens[connectionKey]
	server.providerRuntime.mu.Unlock()
	if foundCachedToken {
		t.Fatal("expected cached provider token to be invalidated after credential rotation")
	}
}

func TestRotateConnectionCredential_RestoresPreviousSecretOnAuditFailure(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	fakeStore := &fakeConnectionSecretStore{}
	server.resolveSecretStore = func(validatedRef secrets.SecretRef) (secrets.SecretStore, error) {
		return fakeStore, nil
	}

	registration := connectionRegistration{
		Provider:  "github",
		GrantType: GrantTypeClientCredentials,
		Subject:   "repo-bot",
		Scopes:    []string{"repo.read"},
		Credential: secrets.SecretRef{
			ID:          "github-bot-token",
			Backend:     secrets.BackendSecure,
			AccountName: "loopgate.github.repo-bot",
			Scope:       "github.repo_read",
		},
	}

	if _, err := server.UpsertConnectionCredential(context.Background(), registration, []byte("ghp-initial-secret")); err != nil {
		t.Fatalf("first upsert connection credential: %v", err)
	}

	appendAuditEvent := server.appendAuditEvent
	server.appendAuditEvent = func(path string, ledgerEvent ledger.Event) error {
		if ledgerEvent.Type == "connection.credential_rotated" {
			return errors.New("audit unavailable")
		}
		return appendAuditEvent(path, ledgerEvent)
	}

	if _, err := server.RotateConnectionCredential(context.Background(), registration, []byte("ghp-rotated-secret")); err == nil {
		t.Fatal("expected rotation to fail closed when audit is unavailable")
	}

	if storedSecret := string(fakeStore.storedSecret["github-bot-token"]); storedSecret != "ghp-initial-secret" {
		t.Fatalf("expected original secret to be restored after failed rotation, got %q", storedSecret)
	}

	server.connectionRuntime.mu.Lock()
	rolledBackRecord := server.connectionRuntime.records[connectionRecordKey("github", "repo-bot")]
	server.connectionRuntime.mu.Unlock()
	if rolledBackRecord.LastRotatedAtUTC == "" {
		t.Fatalf("expected original connection record to remain intact, got %#v", rolledBackRecord)
	}
}
