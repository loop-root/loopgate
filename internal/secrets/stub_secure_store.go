package secrets

import (
	"context"
	"fmt"
)

// StubSecureStore explicitly fails closed until OS secure-store integration exists.
type StubSecureStore struct {
	BackendName string
}

func NewStubSecureStore() *StubSecureStore {
	return &StubSecureStore{BackendName: BackendSecure}
}

func (store *StubSecureStore) backendName() string {
	if store == nil || store.BackendName == "" {
		return BackendSecure
	}
	return store.BackendName
}

func (store *StubSecureStore) Put(ctx context.Context, validatedRef SecretRef, rawSecret []byte) (SecretMetadata, error) {
	_ = ctx
	_ = rawSecret
	if err := validateRefForBackend(validatedRef, store.backendName()); err != nil {
		return SecretMetadata{}, err
	}
	return SecretMetadata{}, fmt.Errorf("%w: secure backend %q not implemented", ErrSecretBackendUnavailable, store.backendName())
}

func (store *StubSecureStore) Get(ctx context.Context, validatedRef SecretRef) ([]byte, SecretMetadata, error) {
	_ = ctx
	if err := validateRefForBackend(validatedRef, store.backendName()); err != nil {
		return nil, SecretMetadata{}, err
	}
	return nil, SecretMetadata{}, fmt.Errorf("%w: secure backend %q not implemented", ErrSecretBackendUnavailable, store.backendName())
}

func (store *StubSecureStore) Delete(ctx context.Context, validatedRef SecretRef) error {
	_ = ctx
	if err := validateRefForBackend(validatedRef, store.backendName()); err != nil {
		return err
	}
	return fmt.Errorf("%w: secure backend %q not implemented", ErrSecretBackendUnavailable, store.backendName())
}

func (store *StubSecureStore) Metadata(ctx context.Context, validatedRef SecretRef) (SecretMetadata, error) {
	_ = ctx
	if err := validateRefForBackend(validatedRef, store.backendName()); err != nil {
		return SecretMetadata{}, err
	}
	return SecretMetadata{}, fmt.Errorf("%w: secure backend %q not implemented", ErrSecretBackendUnavailable, store.backendName())
}
