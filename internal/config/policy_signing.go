package config

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loopgate/internal/identifiers"

	"gopkg.in/yaml.v3"
)

const (
	policySignatureSchemaVersion      = "1"
	policySignatureAlgorithmEd25519   = "ed25519"
	policySignatureMessagePrefix      = "loopgate-policy-signature-v1\n"
	PolicySigningTrustAnchorKeyID     = "loopgate-policy-root-2026-04"
	policySigningTrustAnchorDERBase64 = "MCowBQYDK2VwAyEAEv/fxKaSKQMrZ8brWkB4ZbefF5q5G7RstOHOhqJzEbE="
	policySigningTrustDirEnv          = "LOOPGATE_POLICY_SIGNING_TRUST_DIR"
	policySigningPublicKeySuffix      = ".pub.pem"

	testPolicySigningKeyIDEnv     = "LOOPGATE_TEST_POLICY_SIGNING_KEY_ID"
	testPolicySigningPublicKeyEnv = "LOOPGATE_TEST_POLICY_SIGNING_PUBLIC_KEY"
	defaultTestPolicySigningKeyID = "loopgate-test-policy-root"
)

// PolicySignatureFile is the detached signature metadata for core/policy/policy.yaml.
type PolicySignatureFile struct {
	Version   string `yaml:"version" json:"version"`
	Algorithm string `yaml:"algorithm" json:"algorithm"`
	KeyID     string `yaml:"key_id" json:"key_id"`
	Signature string `yaml:"signature" json:"signature"`
}

func PolicySignaturePath(repoRoot string) string {
	return filepath.Join(repoRoot, "core", "policy", "policy.yaml.sig")
}

func verifyPolicySignature(repoRoot string, rawPolicyBytes []byte) error {
	signatureFile, err := LoadPolicySignatureFile(repoRoot)
	if err != nil {
		return err
	}
	if err := VerifyPolicyDocumentSignature(rawPolicyBytes, signatureFile); err != nil {
		return err
	}
	return nil
}

func LoadPolicySignatureFile(repoRoot string) (PolicySignatureFile, error) {
	signaturePath := PolicySignaturePath(repoRoot)
	return LoadPolicySignatureFromPath(signaturePath)
}

// LoadPolicySignatureFromPath strictly loads a detached policy signature file.
func LoadPolicySignatureFromPath(signaturePath string) (PolicySignatureFile, error) {
	resolvedSignaturePath, err := resolveRequiredLoadPath(signaturePath, "policy signature file")
	if err != nil {
		return PolicySignatureFile{}, err
	}

	rawSignatureBytes, err := os.ReadFile(resolvedSignaturePath)
	if err != nil {
		return PolicySignatureFile{}, fmt.Errorf("read policy signature: %w", err)
	}

	var signatureFile PolicySignatureFile
	decoder := yaml.NewDecoder(bytes.NewReader(rawSignatureBytes))
	decoder.KnownFields(true)
	if err := decoder.Decode(&signatureFile); err != nil {
		return PolicySignatureFile{}, fmt.Errorf("decode policy signature: %w", err)
	}
	if err := validatePolicySignatureFile(signatureFile); err != nil {
		return PolicySignatureFile{}, err
	}
	return signatureFile, nil
}

// TrustedPolicySigningPublicKey returns the public key this binary trusts for the
// provided key identifier. Tests may extend the trust set via the documented
// LOOPGATE_TEST_POLICY_SIGNING_* environment variables.
func TrustedPolicySigningPublicKey(keyID string) (ed25519.PublicKey, error) {
	trustedKeys, err := trustedPolicySigningKeys()
	if err != nil {
		return nil, err
	}
	trustedPublicKey, ok := trustedKeys[strings.TrimSpace(keyID)]
	if !ok {
		return nil, fmt.Errorf("policy signature key %q is not trusted by this binary", keyID)
	}
	return append(ed25519.PublicKey(nil), trustedPublicKey...), nil
}

func validatePolicySignatureFile(signatureFile PolicySignatureFile) error {
	if strings.TrimSpace(signatureFile.Version) != policySignatureSchemaVersion {
		return fmt.Errorf("policy signature version must be %q", policySignatureSchemaVersion)
	}
	if strings.TrimSpace(signatureFile.Algorithm) != policySignatureAlgorithmEd25519 {
		return fmt.Errorf("policy signature algorithm must be %q", policySignatureAlgorithmEd25519)
	}
	if err := identifiers.ValidateSafeIdentifier("policy signature key_id", signatureFile.KeyID); err != nil {
		return fmt.Errorf("invalid policy signature key_id: %w", err)
	}
	if strings.TrimSpace(signatureFile.Signature) == "" {
		return fmt.Errorf("policy signature is required")
	}
	return nil
}

// VerifyPolicyDocumentSignature verifies a detached policy signature file
// against the provided raw policy YAML bytes.
func VerifyPolicyDocumentSignature(rawPolicyBytes []byte, signatureFile PolicySignatureFile) error {
	return verifyDetachedDocumentSignature(rawPolicyBytes, policySignatureMessagePrefix, signatureFile)
}

func verifyDetachedDocumentSignature(rawDocumentBytes []byte, messagePrefix string, signatureFile PolicySignatureFile) error {
	trustedKeys, err := trustedPolicySigningKeys()
	if err != nil {
		return err
	}
	trustedPublicKey, ok := trustedKeys[signatureFile.KeyID]
	if !ok {
		return fmt.Errorf("policy signature key %q is not trusted by this binary", signatureFile.KeyID)
	}

	signatureBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(signatureFile.Signature))
	if err != nil {
		return fmt.Errorf("decode policy signature: %w", err)
	}
	if len(signatureBytes) != ed25519.SignatureSize {
		return fmt.Errorf("policy signature must be %d bytes", ed25519.SignatureSize)
	}
	if !ed25519.Verify(trustedPublicKey, detachedSignatureMessage(messagePrefix, rawDocumentBytes), signatureBytes) {
		return fmt.Errorf("policy signature verification failed for key %q", signatureFile.KeyID)
	}
	return nil
}

func trustedPolicySigningKeys() (map[string]ed25519.PublicKey, error) {
	builtinPublicKey, err := parseTrustedPolicySigningPublicKey(policySigningTrustAnchorDERBase64)
	if err != nil {
		return nil, err
	}

	trustedKeys := map[string]ed25519.PublicKey{
		PolicySigningTrustAnchorKeyID: builtinPublicKey,
	}
	operatorTrustedKeys, err := loadOperatorTrustedPolicySigningKeys()
	if err != nil {
		return nil, err
	}
	for keyID, publicKey := range operatorTrustedKeys {
		existingKey, exists := trustedKeys[keyID]
		switch {
		case !exists:
			trustedKeys[keyID] = publicKey
		case !publicKeysEqual(existingKey, publicKey):
			return nil, fmt.Errorf("operator trust anchor for key_id %q conflicts with an already trusted public key", keyID)
		}
	}
	if !testing.Testing() {
		return trustedKeys, nil
	}

	testPublicKeyBase64 := strings.TrimSpace(os.Getenv(testPolicySigningPublicKeyEnv))
	if testPublicKeyBase64 == "" {
		return trustedKeys, nil
	}

	testPublicKeyBytes, err := base64.StdEncoding.DecodeString(testPublicKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", testPolicySigningPublicKeyEnv, err)
	}
	if len(testPublicKeyBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%s must decode to %d bytes", testPolicySigningPublicKeyEnv, ed25519.PublicKeySize)
	}

	testKeyID := strings.TrimSpace(os.Getenv(testPolicySigningKeyIDEnv))
	if testKeyID == "" {
		testKeyID = defaultTestPolicySigningKeyID
	}
	if err := identifiers.ValidateSafeIdentifier("test policy signing key_id", testKeyID); err != nil {
		return nil, fmt.Errorf("invalid test policy signing key_id: %w", err)
	}
	trustedKeys[testKeyID] = ed25519.PublicKey(append([]byte(nil), testPublicKeyBytes...))
	return trustedKeys, nil
}

func loadOperatorTrustedPolicySigningKeys() (map[string]ed25519.PublicKey, error) {
	trustDir, err := resolvePolicySigningTrustDir()
	if err != nil {
		return nil, err
	}

	dirEntries, err := os.ReadDir(trustDir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]ed25519.PublicKey{}, nil
		}
		return nil, fmt.Errorf("read policy signing trust dir %s: %w", trustDir, err)
	}

	trustedKeys := make(map[string]ed25519.PublicKey)
	for _, dirEntry := range dirEntries {
		if dirEntry.IsDir() {
			continue
		}
		entryName := strings.TrimSpace(dirEntry.Name())
		if !strings.HasSuffix(entryName, policySigningPublicKeySuffix) {
			continue
		}

		keyID := strings.TrimSuffix(entryName, policySigningPublicKeySuffix)
		if err := identifiers.ValidateSafeIdentifier("policy signing trust anchor key_id", keyID); err != nil {
			return nil, fmt.Errorf("invalid policy signing trust anchor filename %q: %w", entryName, err)
		}

		publicKeyPath := filepath.Join(trustDir, entryName)
		rawPublicKeyBytes, err := os.ReadFile(publicKeyPath)
		if err != nil {
			return nil, fmt.Errorf("read policy signing trust anchor %s: %w", publicKeyPath, err)
		}
		publicKey, err := ParsePolicySigningPublicKeyPEM(rawPublicKeyBytes)
		if err != nil {
			return nil, fmt.Errorf("parse policy signing trust anchor %s: %w", publicKeyPath, err)
		}

		existingKey, exists := trustedKeys[keyID]
		switch {
		case !exists:
			trustedKeys[keyID] = publicKey
		case !publicKeysEqual(existingKey, publicKey):
			return nil, fmt.Errorf("policy signing trust dir contains multiple different public keys for key_id %q", keyID)
		}
	}

	return trustedKeys, nil
}

func resolvePolicySigningTrustDir() (string, error) {
	if trustDir := strings.TrimSpace(os.Getenv(policySigningTrustDirEnv)); trustDir != "" {
		return filepath.Clean(trustDir), nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("determine default policy signing trust dir: %w", err)
	}
	return defaultPolicySigningTrustDirForConfigDir(configDir), nil
}

func defaultPolicySigningTrustDirForConfigDir(configDir string) string {
	return filepath.Join(filepath.Clean(configDir), "Loopgate", "policy-signing", "trusted")
}

func parseTrustedPolicySigningPublicKey(derBase64 string) (ed25519.PublicKey, error) {
	derBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(derBase64))
	if err != nil {
		return nil, fmt.Errorf("decode trusted policy signing public key: %w", err)
	}
	publicKeyAny, err := x509.ParsePKIXPublicKey(derBytes)
	if err != nil {
		return nil, fmt.Errorf("parse trusted policy signing public key: %w", err)
	}
	publicKey, ok := publicKeyAny.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("trusted policy signing public key is not ed25519")
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("trusted policy signing public key must be %d bytes", ed25519.PublicKeySize)
	}
	return append(ed25519.PublicKey(nil), publicKey...), nil
}

func ParsePolicySigningPublicKeyPEM(rawPublicKeyBytes []byte) (ed25519.PublicKey, error) {
	publicKeyBlock, remainingBytes := pem.Decode(rawPublicKeyBytes)
	if publicKeyBlock == nil {
		return nil, fmt.Errorf("policy signing public key PEM block not found")
	}
	if strings.TrimSpace(string(remainingBytes)) != "" {
		return nil, fmt.Errorf("policy signing public key PEM contains trailing data")
	}
	if strings.TrimSpace(publicKeyBlock.Type) != "PUBLIC KEY" {
		return nil, fmt.Errorf("policy signing public key PEM type must be PUBLIC KEY")
	}
	publicKeyAny, err := x509.ParsePKIXPublicKey(publicKeyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse policy signing public key PEM: %w", err)
	}
	publicKey, ok := publicKeyAny.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("policy signing public key is not ed25519")
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("policy signing public key must be %d bytes", ed25519.PublicKeySize)
	}
	return append(ed25519.PublicKey(nil), publicKey...), nil
}

func publicKeysEqual(left ed25519.PublicKey, right ed25519.PublicKey) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx] != right[idx] {
			return false
		}
	}
	return true
}

func detachedSignatureMessage(messagePrefix string, rawDocumentBytes []byte) []byte {
	messageBytes := make([]byte, 0, len(messagePrefix)+len(rawDocumentBytes))
	messageBytes = append(messageBytes, messagePrefix...)
	messageBytes = append(messageBytes, rawDocumentBytes...)
	return messageBytes
}

// SignPolicyDocument builds a detached signature file for raw policy YAML bytes.
func SignPolicyDocument(rawPolicyBytes []byte, keyID string, privateKey ed25519.PrivateKey) (PolicySignatureFile, error) {
	return signDetachedDocument(rawPolicyBytes, policySignatureMessagePrefix, keyID, privateKey)
}

func signDetachedDocument(rawDocumentBytes []byte, messagePrefix string, keyID string, privateKey ed25519.PrivateKey) (PolicySignatureFile, error) {
	trimmedKeyID := strings.TrimSpace(keyID)
	if err := identifiers.ValidateSafeIdentifier("policy signing key_id", trimmedKeyID); err != nil {
		return PolicySignatureFile{}, fmt.Errorf("invalid policy signing key_id: %w", err)
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return PolicySignatureFile{}, fmt.Errorf("policy signing private key must be %d bytes", ed25519.PrivateKeySize)
	}
	signatureBytes := ed25519.Sign(privateKey, detachedSignatureMessage(messagePrefix, rawDocumentBytes))
	return PolicySignatureFile{
		Version:   policySignatureSchemaVersion,
		Algorithm: policySignatureAlgorithmEd25519,
		KeyID:     trimmedKeyID,
		Signature: base64.StdEncoding.EncodeToString(signatureBytes),
	}, nil
}

// MarshalPolicySignatureYAML encodes a detached signature file in canonical YAML.
func MarshalPolicySignatureYAML(signatureFile PolicySignatureFile) ([]byte, error) {
	if err := validatePolicySignatureFile(signatureFile); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(&signatureFile); err != nil {
		return nil, fmt.Errorf("marshal policy signature yaml: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("close policy signature yaml encoder: %w", err)
	}
	return buf.Bytes(), nil
}

// WritePolicySignatureYAML writes core/policy/policy.yaml.sig atomically.
func WritePolicySignatureYAML(repoRoot string, signatureFile PolicySignatureFile) error {
	signatureBytes, err := MarshalPolicySignatureYAML(signatureFile)
	if err != nil {
		return err
	}
	signaturePath := PolicySignaturePath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(signaturePath), 0o755); err != nil {
		return fmt.Errorf("create policy signature dir: %w", err)
	}
	if err := atomicWriteFile(signaturePath, signatureBytes, 0o600); err != nil {
		return err
	}
	return nil
}

// ParsePolicySigningPrivateKeyPEM parses a PKCS#8 PEM-encoded Ed25519 private key.
func ParsePolicySigningPrivateKeyPEM(rawPEMBytes []byte) (ed25519.PrivateKey, error) {
	pemBlock, _ := pem.Decode(rawPEMBytes)
	if pemBlock == nil {
		return nil, fmt.Errorf("policy signing private key must be PEM encoded")
	}
	privateKeyAny, err := x509.ParsePKCS8PrivateKey(pemBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse policy signing private key: %w", err)
	}
	privateKey, ok := privateKeyAny.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("policy signing private key is not ed25519")
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("policy signing private key must be %d bytes", ed25519.PrivateKeySize)
	}
	return append(ed25519.PrivateKey(nil), privateKey...), nil
}
