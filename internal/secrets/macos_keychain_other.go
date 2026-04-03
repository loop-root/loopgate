//go:build !darwin

package secrets

import (
	"context"
	"fmt"
)

func storeSecretInMacOSKeychain(_ context.Context, _ SecretRef, _ []byte) error {
	return fmt.Errorf("%w: macos keychain backend requires darwin", ErrSecretBackendUnavailable)
}
