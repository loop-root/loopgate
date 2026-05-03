package loopgate

import (
	"context"
	"errors"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"testing"
	"time"

	"loopgate/internal/ledger"
	"loopgate/internal/secrets"
)

func TestRotateConnectionCredential_OverwritesExistingSecretSafely(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	fakeStore := &fakeConnectionSecretStore{}
	server.resolveSecretStore = func(validatedRef secrets.SecretRef) (secrets.SecretStore, error) {
		return fakeStore, nil
	}

	registration := connectionRegistration{
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
		GrantType: controlapipkg.GrantTypeClientCredentials,
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
		GrantType: controlapipkg.GrantTypeClientCredentials,
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
