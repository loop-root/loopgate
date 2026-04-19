package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEnvSecretStore_GetSuccess(t *testing.T) {
	fixedNow := time.Date(2026, 3, 7, 18, 30, 0, 0, time.UTC)
	store := &EnvSecretStore{
		lookupEnv: func(key string) (string, bool) {
			if key == "MORPH_API_TOKEN" {
				return "token-123456", true
			}
			return "", false
		},
		now: func() time.Time { return fixedNow },
	}

	validatedRef := SecretRef{
		ID:          "sec-ref-1",
		Backend:     BackendEnv,
		AccountName: "MORPH_API_TOKEN",
		Scope:       "llm.provider",
	}

	rawSecret, secretMetadata, err := store.Get(context.Background(), validatedRef)
	if err != nil {
		t.Fatalf("env get failed: %v", err)
	}
	if string(rawSecret) != "token-123456" {
		t.Fatalf("unexpected secret value: %q", string(rawSecret))
	}
	if secretMetadata.Status != "runtime_injected" {
		t.Fatalf("unexpected status: %q", secretMetadata.Status)
	}
	if !secretMetadata.LastUsedAt.Equal(fixedNow) {
		t.Fatalf("unexpected last used timestamp: %v", secretMetadata.LastUsedAt)
	}
	if len(secretMetadata.Fingerprint) != 16 {
		t.Fatalf("expected truncated fingerprint, got %q", secretMetadata.Fingerprint)
	}
}

func TestEnvSecretStore_GetNotFound(t *testing.T) {
	store := &EnvSecretStore{
		lookupEnv: func(string) (string, bool) { return "", false },
		now:       time.Now,
	}

	validatedRef := SecretRef{
		ID:          "sec-ref-2",
		Backend:     BackendEnv,
		AccountName: "MISSING_ENV",
		Scope:       "llm.provider",
	}
	_, _, err := store.Get(context.Background(), validatedRef)
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("expected ErrSecretNotFound, got %v", err)
	}
}

func TestEnvSecretStore_PutFailsClosed(t *testing.T) {
	store := NewEnvSecretStore()
	validatedRef := SecretRef{
		ID:          "sec-ref-3",
		Backend:     BackendEnv,
		AccountName: "MORPH_API_TOKEN",
		Scope:       "llm.provider",
	}
	_, err := store.Put(context.Background(), validatedRef, []byte("new-token"))
	if !errors.Is(err, ErrSecretValidation) {
		t.Fatalf("expected ErrSecretValidation, got %v", err)
	}
}

func TestStubSecureStore_BackendUnavailable(t *testing.T) {
	store := NewStubSecureStore()
	validatedRef := SecretRef{
		ID:          "sec-ref-4",
		Backend:     BackendSecure,
		AccountName: "morph.dev",
		Scope:       "llm.provider",
	}

	_, _, err := store.Get(context.Background(), validatedRef)
	if !errors.Is(err, ErrSecretBackendUnavailable) {
		t.Fatalf("expected ErrSecretBackendUnavailable, got %v", err)
	}
}

func TestNoPermissiveFallbackBehavior(t *testing.T) {
	t.Setenv("MORPH_TOKEN_FALLBACK_TEST", "fallback-secret")

	envStore := NewEnvSecretStore()
	secureStore := NewStubSecureStore()

	secureRef := SecretRef{
		ID:          "sec-ref-5",
		Backend:     BackendSecure,
		AccountName: "MORPH_TOKEN_FALLBACK_TEST",
		Scope:       "llm.provider",
	}
	_, _, secureErr := secureStore.Get(context.Background(), secureRef)
	if !errors.Is(secureErr, ErrSecretBackendUnavailable) {
		t.Fatalf("expected secure backend unavailable error, got %v", secureErr)
	}

	_, _, envErr := envStore.Get(context.Background(), secureRef)
	if !errors.Is(envErr, ErrSecretValidation) {
		t.Fatalf("expected backend mismatch validation error, got %v", envErr)
	}
}

func TestRedactText_QuotedValueWithSpaces(t *testing.T) {
	out := RedactText(`password="my secret key"`)
	if strings.Contains(out, "secret key") {
		t.Fatalf("quoted password value leaked: %q", out)
	}
}

func TestRedactText_BasicAuthToken(t *testing.T) {
	const basicCreds = "dXNlcjpwYXNz"
	out := RedactText("Authorization: Basic " + basicCreds)
	if strings.Contains(out, basicCreds) {
		t.Fatalf("basic auth credentials leaked: %q", out)
	}
}

func TestRedactTextAndStructuredFields(t *testing.T) {
	rawJWT := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.c2lnbmF0dXJl"
	truncatedJWT := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0"
	rawText := "Authorization: Bearer super-secret-token\napi_key=my-key\nurl=https://example.com/callback?token=url-secret\njwt=" + rawJWT + "\nnormal=ok"
	redactedText := RedactText(rawText)
	if strings.Contains(redactedText, "super-secret-token") {
		t.Fatalf("redacted text leaked bearer token: %q", redactedText)
	}
	if strings.Contains(redactedText, "my-key") {
		t.Fatalf("redacted text leaked api key: %q", redactedText)
	}
	if strings.Contains(redactedText, "url-secret") {
		t.Fatalf("redacted text leaked query param token: %q", redactedText)
	}
	if strings.Contains(redactedText, rawJWT) {
		t.Fatalf("redacted text leaked JWT-like token: %q", redactedText)
	}
	if leakedTruncatedJWT := RedactText("jwt_truncated=" + truncatedJWT); strings.Contains(leakedTruncatedJWT, truncatedJWT) {
		t.Fatalf("redacted text leaked truncated JWT-like token: %q", leakedTruncatedJWT)
	}

	rawFields := map[string]interface{}{
		"authorization": "Bearer another-secret",
		"token":         "abc123",
		"note":          "safe text",
		"raw_bytes":     []byte("byte-secret"),
		"raw_message":   json.RawMessage(`{"refresh_token":"raw-message-secret"}`),
		"nested": map[string]interface{}{
			"client_secret": "nested-secret",
			"url":           "https://example.com/profile?refresh_token=nested-refresh",
		},
	}
	redactedFields := RedactStructuredFields(rawFields)
	encodedFields, err := json.Marshal(redactedFields)
	if err != nil {
		t.Fatalf("marshal redacted fields: %v", err)
	}
	encodedString := string(encodedFields)
	for _, leaked := range []string{"another-secret", "abc123", "nested-secret", "nested-refresh", "byte-secret", "raw-message-secret"} {
		if strings.Contains(encodedString, leaked) {
			t.Fatalf("redacted fields leaked secret %q: %s", leaked, encodedString)
		}
	}
	if rawBytesValue, ok := redactedFields["raw_bytes"].(string); !ok || rawBytesValue != redactedPlaceholder {
		t.Fatalf("expected raw byte field to be redacted, got %#v", redactedFields["raw_bytes"])
	}
	if rawMessageValue, ok := redactedFields["raw_message"].(string); !ok || rawMessageValue != redactedPlaceholder {
		t.Fatalf("expected raw json message field to be redacted, got %#v", redactedFields["raw_message"])
	}
}

func TestAppendSecretMetadataEvent_NoSecretValueWritten(t *testing.T) {
	ledgerPath := filepath.Join(t.TempDir(), "ledger.jsonl")
	rawSecret := "ultra-sensitive-secret-token"

	validatedRef := SecretRef{
		ID:          "sec-ref-6",
		Backend:     BackendEnv,
		AccountName: "MORPH_TOKEN",
		Scope:       "llm.provider",
	}
	secretMetadata := SecretMetadata{
		CreatedAt:   time.Date(2026, 3, 7, 19, 0, 0, 0, time.UTC),
		Status:      "active",
		Scope:       validatedRef.Scope,
		Fingerprint: fingerprintFromSecret([]byte(rawSecret)),
	}
	rawDetails := map[string]interface{}{
		"value":         rawSecret,
		"authorization": "Bearer " + rawSecret,
		"safe_note":     "rotation requested",
	}

	err := AppendSecretMetadataEvent(
		ledgerPath,
		"session-1",
		"secret.metadata.recorded",
		validatedRef,
		secretMetadata,
		rawDetails,
	)
	if err != nil {
		t.Fatalf("append secret metadata event: %v", err)
	}

	ledgerBytes, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	ledgerText := string(ledgerBytes)
	if strings.Contains(ledgerText, rawSecret) {
		t.Fatalf("ledger leaked raw secret: %s", ledgerText)
	}
	if strings.Contains(strings.ToLower(ledgerText), "bearer "+strings.ToLower(rawSecret)) {
		t.Fatalf("ledger leaked authorization token: %s", ledgerText)
	}
	if strings.Contains(ledgerText, "rotation requested") {
		t.Fatalf("ledger leaked raw detail value unexpectedly: %s", ledgerText)
	}
	if !strings.Contains(ledgerText, `"redacted":true`) {
		t.Fatalf("expected redacted detail summary marker in ledger: %s", ledgerText)
	}
}

func TestBackendConstantsStrictMatching(t *testing.T) {
	envStore := NewEnvSecretStore()
	t.Setenv("MORPH_BACKEND_CASE_TEST", "case-secret")

	caseMismatchedRef := SecretRef{
		ID:          "sec-ref-7",
		Backend:     "ENV",
		AccountName: "MORPH_BACKEND_CASE_TEST",
		Scope:       "llm.provider",
	}
	_, _, err := envStore.Get(context.Background(), caseMismatchedRef)
	if !errors.Is(err, ErrSecretValidation) {
		t.Fatalf("expected strict backend match failure, got %v", err)
	}

	unknownBackendRef := SecretRef{
		ID:          "sec-ref-8",
		Backend:     BackendMacOSKeychain,
		AccountName: "MORPH_BACKEND_CASE_TEST",
		Scope:       "llm.provider",
	}
	_, _, err = envStore.Get(context.Background(), unknownBackendRef)
	if !errors.Is(err, ErrSecretValidation) {
		t.Fatalf("expected backend mismatch failure, got %v", err)
	}
}

func TestSecretRefValidate_DeniesTraversalLikeAccountNames(t *testing.T) {
	invalidRef := SecretRef{
		ID:          "sec-ref-9",
		Backend:     BackendEnv,
		AccountName: "../../etc/passwd",
		Scope:       "llm.provider",
	}
	if err := invalidRef.Validate(); !errors.Is(err, ErrSecretValidation) {
		t.Fatalf("expected ErrSecretValidation, got %v", err)
	}

	shellLikeRef := SecretRef{
		ID:          "sec-ref-10",
		Backend:     BackendEnv,
		AccountName: "$rm",
		Scope:       "llm.provider",
	}
	if err := shellLikeRef.Validate(); !errors.Is(err, ErrSecretValidation) {
		t.Fatalf("expected ErrSecretValidation for shell-like account name, got %v", err)
	}
}

func TestNewStoreForRef_SelectsExpectedBackends(t *testing.T) {
	envStore, err := NewStoreForRef(SecretRef{
		ID:          "sec-ref-11",
		Backend:     BackendEnv,
		AccountName: "MORPH_TEST_ENV",
		Scope:       "llm.provider",
	})
	if err != nil {
		t.Fatalf("new store for env ref: %v", err)
	}
	if _, ok := envStore.(*EnvSecretStore); !ok {
		t.Fatalf("expected EnvSecretStore, got %T", envStore)
	}

	secureStore, err := NewStoreForRef(SecretRef{
		ID:          "sec-ref-11b",
		Backend:     BackendSecure,
		AccountName: "loopgate.test",
		Scope:       "integration.oauth",
	})
	if err != nil {
		t.Fatalf("new store for secure ref: %v", err)
	}
	if currentRuntimeGOOS == "darwin" {
		if _, ok := secureStore.(*MacOSKeychainStore); !ok {
			t.Fatalf("expected MacOSKeychainStore for secure backend on darwin, got %T", secureStore)
		}
	} else {
		if _, ok := secureStore.(*StubSecureStore); !ok {
			t.Fatalf("expected StubSecureStore for secure backend off darwin, got %T", secureStore)
		}
	}

	macStore, err := NewStoreForRef(SecretRef{
		ID:          "sec-ref-12",
		Backend:     BackendMacOSKeychain,
		AccountName: "loopgate.test",
		Scope:       "integration.oauth",
	})
	if err != nil {
		t.Fatalf("new store for macos ref: %v", err)
	}
	if currentRuntimeGOOS == "darwin" {
		keychainStore, ok := macStore.(*MacOSKeychainStore)
		if !ok {
			t.Fatalf("expected MacOSKeychainStore on darwin, got %T", macStore)
		}
		if !reflect.DeepEqual(keychainStore.allowedBackends, []string{BackendMacOSKeychain}) {
			t.Fatalf("unexpected allowed backends: %#v", keychainStore.allowedBackends)
		}
	} else {
		stubStore, ok := macStore.(*StubSecureStore)
		if !ok {
			t.Fatalf("expected StubSecureStore for macos backend, got %T", macStore)
		}
		if stubStore.backendName() != BackendMacOSKeychain {
			t.Fatalf("unexpected stub backend name: %q", stubStore.backendName())
		}
	}
}

func TestNewLocalDevSecretStore_SelectsPlatformBackend(t *testing.T) {
	originalGOOS := currentRuntimeGOOS
	t.Cleanup(func() {
		currentRuntimeGOOS = originalGOOS
	})

	currentRuntimeGOOS = "darwin"
	localDevStore := NewLocalDevSecretStore()
	keychainStore, ok := localDevStore.(*MacOSKeychainStore)
	if !ok {
		t.Fatalf("expected MacOSKeychainStore on darwin, got %T", localDevStore)
	}
	if !reflect.DeepEqual(keychainStore.allowedBackends, []string{BackendSecure, BackendMacOSKeychain}) {
		t.Fatalf("unexpected local dev allowed backends: %#v", keychainStore.allowedBackends)
	}

	currentRuntimeGOOS = "linux"
	localDevStore = NewLocalDevSecretStore()
	linuxStub, ok := localDevStore.(*StubSecureStore)
	if !ok || linuxStub.backendName() != BackendLinuxSecretSvc {
		t.Fatalf("expected linux secret service stub, got %T %#v", localDevStore, localDevStore)
	}
}

func TestMacOSKeychainStore_PutUsesInjectedWriter(t *testing.T) {
	originalGOOS := currentRuntimeGOOS
	currentRuntimeGOOS = "darwin"
	t.Cleanup(func() {
		currentRuntimeGOOS = originalGOOS
	})

	fixedNow := time.Date(2026, 3, 7, 21, 0, 0, 0, time.UTC)
	var storedRef SecretRef
	var storedServiceName string
	var storedSecret []byte
	store := &MacOSKeychainStore{
		now:             func() time.Time { return fixedNow },
		allowedBackends: []string{BackendMacOSKeychain},
		putSecret: func(_ context.Context, validatedRef SecretRef, serviceName string, rawSecret []byte) error {
			storedRef = validatedRef
			storedServiceName = serviceName
			storedSecret = append([]byte(nil), rawSecret...)
			return nil
		},
	}

	validatedRef := SecretRef{
		ID:          "sec-ref-keychain-put",
		Backend:     BackendMacOSKeychain,
		AccountName: "loopgate.prod",
		Scope:       "integration.oauth",
	}

	secretMetadata, err := store.Put(context.Background(), validatedRef, []byte("super-secret-key"))
	if err != nil {
		t.Fatalf("keychain put failed: %v", err)
	}
	if storedRef != validatedRef {
		t.Fatalf("unexpected validated ref: %#v", storedRef)
	}
	if storedServiceName != loopgateKeychainServicePrefix+validatedRef.Scope {
		t.Fatalf("expected loopgate service name, got %q", storedServiceName)
	}
	if string(storedSecret) != "super-secret-key" {
		t.Fatalf("expected raw secret to reach injected writer, got %q", string(storedSecret))
	}
	if !secretMetadata.CreatedAt.Equal(fixedNow) || !secretMetadata.LastRotatedAt.Equal(fixedNow) {
		t.Fatalf("unexpected keychain metadata timestamps: %#v", secretMetadata)
	}
	if secretMetadata.Status != "stored" || secretMetadata.Scope != validatedRef.Scope {
		t.Fatalf("unexpected keychain metadata: %#v", secretMetadata)
	}
}

func TestMacOSKeychainStore_GetReturnsMetadata(t *testing.T) {
	originalGOOS := currentRuntimeGOOS
	currentRuntimeGOOS = "darwin"
	t.Cleanup(func() {
		currentRuntimeGOOS = originalGOOS
	})

	fixedNow := time.Date(2026, 3, 7, 22, 0, 0, 0, time.UTC)
	store := &MacOSKeychainStore{
		now:             func() time.Time { return fixedNow },
		allowedBackends: []string{BackendMacOSKeychain},
		getSecret: func(_ context.Context, validatedRef SecretRef, serviceName string) ([]byte, error) {
			if serviceName != loopgateKeychainServicePrefix+validatedRef.Scope {
				t.Fatalf("unexpected service name lookup: %q", serviceName)
			}
			return []byte("retrieved-secret"), nil
		},
	}
	validatedRef := SecretRef{
		ID:          "sec-ref-keychain-get",
		Backend:     BackendMacOSKeychain,
		AccountName: "loopgate.prod",
		Scope:       "integration.oauth",
	}

	rawSecret, secretMetadata, err := store.Get(context.Background(), validatedRef)
	if err != nil {
		t.Fatalf("keychain get failed: %v", err)
	}
	if string(rawSecret) != "retrieved-secret" {
		t.Fatalf("unexpected keychain secret value: %q", string(rawSecret))
	}
	if !secretMetadata.LastUsedAt.Equal(fixedNow) {
		t.Fatalf("unexpected keychain last used timestamp: %#v", secretMetadata)
	}
	if secretMetadata.Status != "stored" || secretMetadata.Scope != validatedRef.Scope {
		t.Fatalf("unexpected keychain metadata: %#v", secretMetadata)
	}
}

func TestMacOSKeychainStore_GetFallsBackToLegacyMorphService(t *testing.T) {
	originalGOOS := currentRuntimeGOOS
	currentRuntimeGOOS = "darwin"
	t.Cleanup(func() {
		currentRuntimeGOOS = originalGOOS
	})

	validatedRef := SecretRef{
		ID:          "sec-ref-keychain-legacy-read",
		Backend:     BackendMacOSKeychain,
		AccountName: "loopgate.audit_ledger_hmac",
		Scope:       "local",
	}

	var requestedServiceNames []string
	store := &MacOSKeychainStore{
		now:             time.Now,
		allowedBackends: []string{BackendMacOSKeychain},
		getSecret: func(_ context.Context, validatedRef SecretRef, serviceName string) ([]byte, error) {
			requestedServiceNames = append(requestedServiceNames, serviceName)
			switch serviceName {
			case keychainServiceName(validatedRef):
				return nil, fmt.Errorf("%w: keychain item for secret ref %q", ErrSecretNotFound, validatedRef.ID)
			case legacyKeychainServiceName(validatedRef):
				return []byte("legacy-secret"), nil
			default:
				t.Fatalf("unexpected service lookup: %q", serviceName)
				return nil, nil
			}
		},
	}

	rawSecret, secretMetadata, err := store.Get(context.Background(), validatedRef)
	if err != nil {
		t.Fatalf("keychain get with legacy fallback failed: %v", err)
	}
	if string(rawSecret) != "legacy-secret" {
		t.Fatalf("expected legacy secret value, got %q", string(rawSecret))
	}
	expectedServiceNames := []string{
		keychainServiceName(validatedRef),
		legacyKeychainServiceName(validatedRef),
	}
	if !reflect.DeepEqual(requestedServiceNames, expectedServiceNames) {
		t.Fatalf("unexpected service lookup order: %#v", requestedServiceNames)
	}
	if secretMetadata.Status != "stored" || secretMetadata.Scope != validatedRef.Scope {
		t.Fatalf("unexpected keychain metadata after legacy fallback: %#v", secretMetadata)
	}
}

func TestMacOSKeychainStore_MetadataFallsBackToLegacyMorphService(t *testing.T) {
	originalGOOS := currentRuntimeGOOS
	currentRuntimeGOOS = "darwin"
	t.Cleanup(func() {
		currentRuntimeGOOS = originalGOOS
	})

	validatedRef := SecretRef{
		ID:          "sec-ref-keychain-legacy-metadata",
		Backend:     BackendMacOSKeychain,
		AccountName: "loopgate.audit_ledger_hmac",
		Scope:       "local",
	}

	var requestedServiceNames []string
	store := &MacOSKeychainStore{
		now:             time.Now,
		allowedBackends: []string{BackendMacOSKeychain},
		metadataForSecret: func(_ context.Context, validatedRef SecretRef, serviceName string) (SecretMetadata, error) {
			requestedServiceNames = append(requestedServiceNames, serviceName)
			switch serviceName {
			case keychainServiceName(validatedRef):
				return SecretMetadata{}, fmt.Errorf("%w: keychain item for secret ref %q", ErrSecretNotFound, validatedRef.ID)
			case legacyKeychainServiceName(validatedRef):
				return SecretMetadata{Status: "stored", Scope: validatedRef.Scope}, nil
			default:
				t.Fatalf("unexpected metadata lookup: %q", serviceName)
				return SecretMetadata{}, nil
			}
		},
	}

	secretMetadata, err := store.Metadata(context.Background(), validatedRef)
	if err != nil {
		t.Fatalf("keychain metadata with legacy fallback failed: %v", err)
	}
	expectedServiceNames := []string{
		keychainServiceName(validatedRef),
		legacyKeychainServiceName(validatedRef),
	}
	if !reflect.DeepEqual(requestedServiceNames, expectedServiceNames) {
		t.Fatalf("unexpected metadata lookup order: %#v", requestedServiceNames)
	}
	if secretMetadata.Status != "stored" || secretMetadata.Scope != validatedRef.Scope {
		t.Fatalf("unexpected metadata after legacy fallback: %#v", secretMetadata)
	}
}

func TestMacOSKeychainStore_DeleteFallsBackToLegacyMorphService(t *testing.T) {
	originalGOOS := currentRuntimeGOOS
	currentRuntimeGOOS = "darwin"
	t.Cleanup(func() {
		currentRuntimeGOOS = originalGOOS
	})

	validatedRef := SecretRef{
		ID:          "sec-ref-keychain-legacy-delete",
		Backend:     BackendMacOSKeychain,
		AccountName: "loopgate.audit_ledger_hmac",
		Scope:       "local",
	}

	var deletedServiceNames []string
	store := &MacOSKeychainStore{
		now:             time.Now,
		allowedBackends: []string{BackendMacOSKeychain},
		deleteSecret: func(_ context.Context, validatedRef SecretRef, serviceName string) error {
			deletedServiceNames = append(deletedServiceNames, serviceName)
			switch serviceName {
			case keychainServiceName(validatedRef):
				return fmt.Errorf("%w: keychain item for secret ref %q", ErrSecretNotFound, validatedRef.ID)
			case legacyKeychainServiceName(validatedRef):
				return nil
			default:
				t.Fatalf("unexpected delete service name: %q", serviceName)
				return nil
			}
		},
	}

	if err := store.Delete(context.Background(), validatedRef); err != nil {
		t.Fatalf("keychain delete with legacy fallback failed: %v", err)
	}
	expectedServiceNames := []string{
		keychainServiceName(validatedRef),
		legacyKeychainServiceName(validatedRef),
	}
	if !reflect.DeepEqual(deletedServiceNames, expectedServiceNames) {
		t.Fatalf("unexpected delete order: %#v", deletedServiceNames)
	}
}

func TestMacOSKeychainStore_NotFoundMapsToErrSecretNotFound(t *testing.T) {
	originalGOOS := currentRuntimeGOOS
	currentRuntimeGOOS = "darwin"
	t.Cleanup(func() {
		currentRuntimeGOOS = originalGOOS
	})

	store := &MacOSKeychainStore{
		now:             time.Now,
		allowedBackends: []string{BackendMacOSKeychain},
		getSecret: func(_ context.Context, _ SecretRef, _ string) ([]byte, error) {
			return nil, fmt.Errorf("%w: keychain item for secret ref %q", ErrSecretNotFound, "sec-ref-keychain-missing")
		},
	}
	validatedRef := SecretRef{
		ID:          "sec-ref-keychain-missing",
		Backend:     BackendMacOSKeychain,
		AccountName: "loopgate.prod",
		Scope:       "integration.oauth",
	}

	_, _, err := store.Get(context.Background(), validatedRef)
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("expected ErrSecretNotFound, got %v", err)
	}
}

func TestFormatKeychainStatusError_UsesReadableMessageWhenAvailable(t *testing.T) {
	validatedRef := SecretRef{
		ID:          "audit_ledger_hmac",
		Backend:     BackendMacOSKeychain,
		AccountName: "loopgate.audit_ledger_hmac",
		Scope:       "local",
	}

	err := formatKeychainStatusError("store secret", validatedRef, -60006, "The authorization was canceled by the user.")
	if !errors.Is(err, ErrSecretBackendUnavailable) {
		t.Fatalf("expected ErrSecretBackendUnavailable, got %v", err)
	}
	if !strings.Contains(err.Error(), "authorization was canceled by the user") {
		t.Fatalf("expected readable security message, got %q", err)
	}
	if !strings.Contains(err.Error(), "status -60006") {
		t.Fatalf("expected status code in error, got %q", err)
	}
}
