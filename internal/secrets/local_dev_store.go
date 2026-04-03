package secrets

import "runtime"

var currentRuntimeGOOS = runtime.GOOS

// NewLocalDevSecretStore returns the development secure-store backend.
// It selects the current OS-native backend when implemented and otherwise
// fails closed with an explicit unavailable backend error.
func NewLocalDevSecretStore() SecretStore {
	switch currentRuntimeGOOS {
	case "darwin":
		return NewMacOSKeychainStore(BackendSecure, BackendMacOSKeychain)
	case "windows":
		return &StubSecureStore{BackendName: BackendWindowsCreds}
	case "linux":
		return &StubSecureStore{BackendName: BackendLinuxSecretSvc}
	default:
		return NewStubSecureStore()
	}
}
