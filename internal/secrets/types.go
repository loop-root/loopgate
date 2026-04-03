package secrets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"morph/internal/identifiers"
)

var (
	// ErrSecretNotFound indicates the requested secret ref cannot be resolved.
	ErrSecretNotFound = errors.New("secret not found")
	// ErrSecretBackendUnavailable indicates the configured backend is unavailable.
	ErrSecretBackendUnavailable = errors.New("secret backend unavailable")
	// ErrSecretValidation indicates a secret reference or payload failed validation.
	ErrSecretValidation = errors.New("secret validation failed")
)

const (
	BackendEnv            = "env"
	BackendSecure         = "secure"
	BackendMacOSKeychain  = "macos_keychain"
	BackendWindowsCreds   = "windows_credential_manager"
	BackendLinuxSecretSvc = "linux_secret_service"
)

// SecretRef is a stable, non-secret reference to secret material.
type SecretRef struct {
	ID          string `json:"id" yaml:"id"`
	Backend     string `json:"backend" yaml:"backend"`
	AccountName string `json:"account_name" yaml:"account_name"`
	Scope       string `json:"scope" yaml:"scope"`
}

// Validate ensures the reference contains required fields.
func (ref SecretRef) Validate() error {
	if err := identifiers.ValidateSafeIdentifier("secret ref id", ref.ID); err != nil {
		return fmt.Errorf("%w: %v", ErrSecretValidation, err)
	}
	if err := identifiers.ValidateSafeIdentifier("secret backend", ref.Backend); err != nil {
		return fmt.Errorf("%w: %v", ErrSecretValidation, err)
	}
	if err := identifiers.ValidateSafeIdentifier("secret account name", ref.AccountName); err != nil {
		return fmt.Errorf("%w: %v", ErrSecretValidation, err)
	}
	if err := identifiers.ValidateSafeIdentifier("secret scope", ref.Scope); err != nil {
		return fmt.Errorf("%w: %v", ErrSecretValidation, err)
	}
	return nil
}

// SecretMetadata carries lifecycle metadata safe for audit contexts.
type SecretMetadata struct {
	CreatedAt     time.Time
	LastUsedAt    time.Time
	LastRotatedAt time.Time
	ExpiresAt     *time.Time
	Status        string
	Scope         string
	Fingerprint   string // truncated, non-reversible correlation value only
}

// SecretStore is the boundary for retrieving secret values from backends.
type SecretStore interface {
	Put(ctx context.Context, ref SecretRef, value []byte) (SecretMetadata, error)
	Get(ctx context.Context, ref SecretRef) ([]byte, SecretMetadata, error)
	Delete(ctx context.Context, ref SecretRef) error
	Metadata(ctx context.Context, ref SecretRef) (SecretMetadata, error)
}

func validateRefForBackend(validatedRef SecretRef, backendName string) error {
	return validateRefForBackends(validatedRef, backendName)
}

func validateRefForBackends(validatedRef SecretRef, backendNames ...string) error {
	if err := validatedRef.Validate(); err != nil {
		return err
	}
	for _, backendName := range backendNames {
		if validatedRef.Backend == backendName {
			return nil
		}
	}
	return fmt.Errorf("%w: backend mismatch for ref %q (got=%q want=%v)", ErrSecretValidation, validatedRef.ID, validatedRef.Backend, backendNames)
}

func fingerprintFromSecret(rawSecret []byte) string {
	sum := sha256.Sum256(rawSecret)
	// Keep fingerprint short and non-reversible while still useful for correlation.
	return hex.EncodeToString(sum[:])[:16]
}
