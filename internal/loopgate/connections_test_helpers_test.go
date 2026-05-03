package loopgate

import (
	"context"
	"sync"
	"time"

	"loopgate/internal/secrets"
)

type fakeConnectionSecretStore struct {
	mu           sync.Mutex
	storedSecret map[string][]byte
	metadata     map[string]secrets.SecretMetadata
	putErr       error
	getErr       error
	deleteErr    error
	putCalls     int
}

func (fakeStore *fakeConnectionSecretStore) Put(ctx context.Context, validatedRef secrets.SecretRef, rawSecret []byte) (secrets.SecretMetadata, error) {
	_ = ctx
	fakeStore.mu.Lock()
	defer fakeStore.mu.Unlock()
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
	fakeStore.mu.Lock()
	defer fakeStore.mu.Unlock()
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
	fakeStore.mu.Lock()
	defer fakeStore.mu.Unlock()
	if fakeStore.deleteErr != nil {
		return fakeStore.deleteErr
	}
	delete(fakeStore.storedSecret, validatedRef.ID)
	delete(fakeStore.metadata, validatedRef.ID)
	return nil
}

func (fakeStore *fakeConnectionSecretStore) Metadata(ctx context.Context, validatedRef secrets.SecretRef) (secrets.SecretMetadata, error) {
	_ = ctx
	fakeStore.mu.Lock()
	defer fakeStore.mu.Unlock()
	secretMetadata, found := fakeStore.metadata[validatedRef.ID]
	if !found {
		return secrets.SecretMetadata{}, secrets.ErrSecretNotFound
	}
	return secretMetadata, nil
}
