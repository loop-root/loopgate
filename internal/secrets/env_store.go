package secrets

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// EnvSecretStore resolves secrets from runtime environment variables.
// This backend is read-only and intended for runtime injection.
type EnvSecretStore struct {
	lookupEnv func(string) (string, bool)
	now       func() time.Time
}

func NewEnvSecretStore() *EnvSecretStore {
	return &EnvSecretStore{
		lookupEnv: os.LookupEnv,
		now:       time.Now,
	}
}

func (store *EnvSecretStore) Put(ctx context.Context, validatedRef SecretRef, rawSecret []byte) (SecretMetadata, error) {
	_ = ctx
	_ = rawSecret
	if err := validateRefForBackend(validatedRef, BackendEnv); err != nil {
		return SecretMetadata{}, err
	}
	return SecretMetadata{}, fmt.Errorf("%w: env backend is read-only (runtime injection only)", ErrSecretValidation)
}

func (store *EnvSecretStore) Get(ctx context.Context, validatedRef SecretRef) ([]byte, SecretMetadata, error) {
	_ = ctx
	if err := validateRefForBackend(validatedRef, BackendEnv); err != nil {
		return nil, SecretMetadata{}, err
	}

	if store.lookupEnv == nil {
		return nil, SecretMetadata{}, fmt.Errorf("%w: env lookup function not configured", ErrSecretBackendUnavailable)
	}
	rawSecretValue, found := store.lookupEnv(validatedRef.AccountName)
	if !found || strings.TrimSpace(rawSecretValue) == "" {
		return nil, SecretMetadata{}, fmt.Errorf("%w: missing env var %q", ErrSecretNotFound, validatedRef.AccountName)
	}

	nowUTC := store.now().UTC()
	secretMetadata := SecretMetadata{
		LastUsedAt:  nowUTC,
		Status:      "runtime_injected",
		Scope:       validatedRef.Scope,
		Fingerprint: fingerprintFromSecret([]byte(rawSecretValue)),
	}
	return []byte(rawSecretValue), secretMetadata, nil
}

func (store *EnvSecretStore) Delete(ctx context.Context, validatedRef SecretRef) error {
	_ = ctx
	if err := validateRefForBackend(validatedRef, BackendEnv); err != nil {
		return err
	}
	return fmt.Errorf("%w: env backend delete is not supported", ErrSecretValidation)
}

func (store *EnvSecretStore) Metadata(ctx context.Context, validatedRef SecretRef) (SecretMetadata, error) {
	_, secretMetadata, err := store.Get(ctx, validatedRef)
	if err != nil {
		return SecretMetadata{}, err
	}
	return secretMetadata, nil
}
