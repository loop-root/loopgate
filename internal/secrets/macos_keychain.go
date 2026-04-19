package secrets

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const loopgateKeychainServicePrefix = "loopgate."

type MacOSKeychainStore struct {
	now               func() time.Time
	allowedBackends   []string
	putSecret         func(context.Context, SecretRef, string, []byte) error
	getSecret         func(context.Context, SecretRef, string) ([]byte, error)
	deleteSecret      func(context.Context, SecretRef, string) error
	metadataForSecret func(context.Context, SecretRef, string) (SecretMetadata, error)
}

func NewMacOSKeychainStore(allowedBackends ...string) *MacOSKeychainStore {
	normalizedBackends := append([]string(nil), allowedBackends...)
	if len(normalizedBackends) == 0 {
		normalizedBackends = []string{BackendMacOSKeychain}
	}
	return &MacOSKeychainStore{
		now:               time.Now,
		allowedBackends:   normalizedBackends,
		putSecret:         storeSecretInMacOSKeychain,
		getSecret:         readSecretFromMacOSKeychain,
		deleteSecret:      deleteSecretFromMacOSKeychain,
		metadataForSecret: metadataForMacOSKeychainSecret,
	}
}

func (store *MacOSKeychainStore) Put(ctx context.Context, validatedRef SecretRef, rawSecret []byte) (SecretMetadata, error) {
	if currentRuntimeGOOS != "darwin" {
		return SecretMetadata{}, fmt.Errorf("%w: macos keychain backend requires darwin", ErrSecretBackendUnavailable)
	}
	if err := validateRefForBackends(validatedRef, store.allowedBackends...); err != nil {
		return SecretMetadata{}, err
	}
	if len(rawSecret) == 0 {
		return SecretMetadata{}, fmt.Errorf("%w: secret value is empty", ErrSecretValidation)
	}

	if err := store.putSecret(ctx, validatedRef, keychainServiceName(validatedRef), rawSecret); err != nil {
		return SecretMetadata{}, err
	}

	nowUTC := store.now().UTC()
	return SecretMetadata{
		CreatedAt:     nowUTC,
		LastRotatedAt: nowUTC,
		Status:        "stored",
		Scope:         validatedRef.Scope,
		Fingerprint:   fingerprintFromSecret(rawSecret),
	}, nil
}

func (store *MacOSKeychainStore) Get(ctx context.Context, validatedRef SecretRef) ([]byte, SecretMetadata, error) {
	if currentRuntimeGOOS != "darwin" {
		return nil, SecretMetadata{}, fmt.Errorf("%w: macos keychain backend requires darwin", ErrSecretBackendUnavailable)
	}
	if err := validateRefForBackends(validatedRef, store.allowedBackends...); err != nil {
		return nil, SecretMetadata{}, err
	}

	secretBytes, err := store.getSecret(ctx, validatedRef, keychainServiceName(validatedRef))
	if err != nil {
		return nil, SecretMetadata{}, err
	}

	nowUTC := store.now().UTC()
	return secretBytes, SecretMetadata{
		LastUsedAt:  nowUTC,
		Status:      "stored",
		Scope:       validatedRef.Scope,
		Fingerprint: fingerprintFromSecret(secretBytes),
	}, nil
}

func (store *MacOSKeychainStore) Delete(ctx context.Context, validatedRef SecretRef) error {
	if currentRuntimeGOOS != "darwin" {
		return fmt.Errorf("%w: macos keychain backend requires darwin", ErrSecretBackendUnavailable)
	}
	if err := validateRefForBackends(validatedRef, store.allowedBackends...); err != nil {
		return err
	}

	return store.deleteSecret(ctx, validatedRef, keychainServiceName(validatedRef))
}

func (store *MacOSKeychainStore) Metadata(ctx context.Context, validatedRef SecretRef) (SecretMetadata, error) {
	if currentRuntimeGOOS != "darwin" {
		return SecretMetadata{}, fmt.Errorf("%w: macos keychain backend requires darwin", ErrSecretBackendUnavailable)
	}
	if err := validateRefForBackends(validatedRef, store.allowedBackends...); err != nil {
		return SecretMetadata{}, err
	}

	return store.metadataForSecret(ctx, validatedRef, keychainServiceName(validatedRef))
}

func keychainServiceName(validatedRef SecretRef) string {
	return loopgateKeychainServicePrefix + strings.TrimSpace(validatedRef.Scope)
}

func formatKeychainStatusError(operation string, validatedRef SecretRef, statusCode int, errorMessageText string) error {
	trimmedErrorMessageText := strings.TrimSpace(errorMessageText)
	if trimmedErrorMessageText == "" {
		return fmt.Errorf("%w: macos keychain %s failed for secret ref %q (status %d)", ErrSecretBackendUnavailable, operation, validatedRef.ID, statusCode)
	}
	return fmt.Errorf("%w: macos keychain %s failed for secret ref %q (%s; status %d)", ErrSecretBackendUnavailable, operation, validatedRef.ID, trimmedErrorMessageText, statusCode)
}
