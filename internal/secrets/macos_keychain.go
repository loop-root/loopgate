package secrets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type commandExecutor interface {
	Run(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, []byte, error)
}

type execCommandExecutor struct{}

func (execCommandExecutor) Run(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, []byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	if len(stdin) > 0 {
		command.Stdin = bytes.NewReader(stdin)
	}

	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	command.Stdout = &stdoutBuffer
	command.Stderr = &stderrBuffer

	err := command.Run()
	return stdoutBuffer.Bytes(), stderrBuffer.Bytes(), err
}

type MacOSKeychainStore struct {
	executor        commandExecutor
	now             func() time.Time
	allowedBackends []string
	putSecret       func(context.Context, SecretRef, []byte) error
}

func NewMacOSKeychainStore(allowedBackends ...string) *MacOSKeychainStore {
	normalizedBackends := append([]string(nil), allowedBackends...)
	if len(normalizedBackends) == 0 {
		normalizedBackends = []string{BackendMacOSKeychain}
	}
	return &MacOSKeychainStore{
		executor:        execCommandExecutor{},
		now:             time.Now,
		allowedBackends: normalizedBackends,
		putSecret:       storeSecretInMacOSKeychain,
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

	if err := store.putSecret(ctx, validatedRef, rawSecret); err != nil {
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

	stdoutBytes, stderrBytes, err := store.executor.Run(
		ctx,
		nil,
		"security",
		"find-generic-password",
		"-a", validatedRef.AccountName,
		"-s", keychainServiceName(validatedRef),
		"-w",
	)
	if err != nil {
		return nil, SecretMetadata{}, mapKeychainError("read secret", validatedRef, stderrBytes, err)
	}

	secretBytes := trimSingleTrailingNewline(stdoutBytes)
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

	_, stderrBytes, err := store.executor.Run(
		ctx,
		nil,
		"security",
		"delete-generic-password",
		"-a", validatedRef.AccountName,
		"-s", keychainServiceName(validatedRef),
	)
	if err != nil {
		return mapKeychainError("delete secret", validatedRef, stderrBytes, err)
	}
	return nil
}

func (store *MacOSKeychainStore) Metadata(ctx context.Context, validatedRef SecretRef) (SecretMetadata, error) {
	if currentRuntimeGOOS != "darwin" {
		return SecretMetadata{}, fmt.Errorf("%w: macos keychain backend requires darwin", ErrSecretBackendUnavailable)
	}
	if err := validateRefForBackends(validatedRef, store.allowedBackends...); err != nil {
		return SecretMetadata{}, err
	}

	_, stderrBytes, err := store.executor.Run(
		ctx,
		nil,
		"security",
		"find-generic-password",
		"-a", validatedRef.AccountName,
		"-s", keychainServiceName(validatedRef),
	)
	if err != nil {
		return SecretMetadata{}, mapKeychainError("read secret metadata", validatedRef, stderrBytes, err)
	}

	return SecretMetadata{
		Status: "stored",
		Scope:  validatedRef.Scope,
	}, nil
}

func keychainServiceName(validatedRef SecretRef) string {
	return "morph.loopgate." + validatedRef.Scope
}

func trimSingleTrailingNewline(rawValue []byte) []byte {
	trimmedValue := rawValue
	trimmedValue = bytes.TrimSuffix(trimmedValue, []byte("\r\n"))
	trimmedValue = bytes.TrimSuffix(trimmedValue, []byte("\n"))
	return trimmedValue
}

func mapKeychainError(operation string, validatedRef SecretRef, stderrBytes []byte, err error) error {
	if err == nil {
		return nil
	}

	stderrText := strings.ToLower(strings.TrimSpace(string(stderrBytes)))
	if strings.Contains(stderrText, "could not be found") || strings.Contains(stderrText, "item not found") {
		return fmt.Errorf("%w: keychain item for secret ref %q", ErrSecretNotFound, validatedRef.ID)
	}

	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("%w: macos security tool unavailable during %s", ErrSecretBackendUnavailable, operation)
	}

	if stderrText == "" {
		return fmt.Errorf("%w: macos keychain %s failed for secret ref %q", ErrSecretBackendUnavailable, operation, validatedRef.ID)
	}

	return fmt.Errorf("%w: macos keychain %s failed for secret ref %q (%s)", ErrSecretBackendUnavailable, operation, validatedRef.ID, stderrText)
}

func formatKeychainStatusError(operation string, validatedRef SecretRef, statusCode int, errorMessageText string) error {
	trimmedErrorMessageText := strings.TrimSpace(errorMessageText)
	if trimmedErrorMessageText == "" {
		return fmt.Errorf("%w: macos keychain %s failed for secret ref %q (status %d)", ErrSecretBackendUnavailable, operation, validatedRef.ID, statusCode)
	}
	return fmt.Errorf("%w: macos keychain %s failed for secret ref %q (%s; status %d)", ErrSecretBackendUnavailable, operation, validatedRef.ID, trimmedErrorMessageText, statusCode)
}
