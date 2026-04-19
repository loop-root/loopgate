//go:build !darwin

package secrets

import (
	"context"
	"fmt"
)

func storeSecretInMacOSKeychain(_ context.Context, _ SecretRef, _ string, _ []byte) error {
	return fmt.Errorf("%w: macos keychain backend requires darwin", ErrSecretBackendUnavailable)
}

func readSecretFromMacOSKeychain(_ context.Context, _ SecretRef, _ string) ([]byte, error) {
	return nil, fmt.Errorf("%w: macos keychain backend requires darwin", ErrSecretBackendUnavailable)
}

func deleteSecretFromMacOSKeychain(_ context.Context, _ SecretRef, _ string) error {
	return fmt.Errorf("%w: macos keychain backend requires darwin", ErrSecretBackendUnavailable)
}

func metadataForMacOSKeychainSecret(_ context.Context, _ SecretRef, _ string) (SecretMetadata, error) {
	return SecretMetadata{}, fmt.Errorf("%w: macos keychain backend requires darwin", ErrSecretBackendUnavailable)
}
