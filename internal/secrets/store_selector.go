package secrets

import (
	"fmt"
	"strings"
)

// NewStoreForRef returns the concrete secret-store backend for a validated ref.
// It never falls back silently across backend classes.
func NewStoreForRef(validatedRef SecretRef) (SecretStore, error) {
	if err := validatedRef.Validate(); err != nil {
		return nil, err
	}

	switch strings.TrimSpace(validatedRef.Backend) {
	case BackendEnv:
		return NewEnvSecretStore(), nil
	case BackendSecure:
		return NewLocalDevSecretStore(), nil
	case BackendMacOSKeychain:
		if currentRuntimeGOOS == "darwin" {
			return NewMacOSKeychainStore(BackendMacOSKeychain), nil
		}
		return &StubSecureStore{BackendName: BackendMacOSKeychain}, nil
	case BackendWindowsCreds:
		return &StubSecureStore{BackendName: BackendWindowsCreds}, nil
	case BackendLinuxSecretSvc:
		return &StubSecureStore{BackendName: BackendLinuxSecretSvc}, nil
	default:
		return nil, fmt.Errorf("%w: unsupported secret backend %q", ErrSecretValidation, validatedRef.Backend)
	}
}
