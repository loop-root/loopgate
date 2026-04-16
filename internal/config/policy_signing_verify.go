package config

import (
	"bytes"
	"crypto/ed25519"
	"fmt"
	"os"
	"strings"
)

type PolicySigningSetupVerification struct {
	ExpectedKeyID        string
	SignaturePath        string
	SignatureKeyID       string
	SignerKeyPath        string
	SignerKeyPermissions os.FileMode
}

func VerifyPolicySigningSetup(repoRoot string, privateKeyPath string, expectedKeyID string) (PolicySigningSetupVerification, error) {
	trimmedExpectedKeyID := strings.TrimSpace(expectedKeyID)
	if trimmedExpectedKeyID == "" {
		return PolicySigningSetupVerification{}, fmt.Errorf("expected policy signing key_id is required")
	}
	trimmedPrivateKeyPath := strings.TrimSpace(privateKeyPath)
	if trimmedPrivateKeyPath == "" {
		return PolicySigningSetupVerification{}, fmt.Errorf("policy signing private key path is required")
	}

	if _, err := LoadPolicyWithHash(repoRoot); err != nil {
		return PolicySigningSetupVerification{}, fmt.Errorf("verify policy signature against trusted public keys: %w", err)
	}

	signatureFile, err := LoadPolicySignatureFile(repoRoot)
	if err != nil {
		return PolicySigningSetupVerification{}, err
	}
	if signatureFile.KeyID != trimmedExpectedKeyID {
		return PolicySigningSetupVerification{}, fmt.Errorf("policy signature key_id %q does not match expected key_id %q", signatureFile.KeyID, trimmedExpectedKeyID)
	}

	trustedPublicKey, err := TrustedPolicySigningPublicKey(trimmedExpectedKeyID)
	if err != nil {
		return PolicySigningSetupVerification{}, err
	}

	privateKeyFileInfo, err := os.Stat(trimmedPrivateKeyPath)
	if err != nil {
		return PolicySigningSetupVerification{}, fmt.Errorf("stat private key %s: %w", trimmedPrivateKeyPath, err)
	}
	privateKeyPermissions := privateKeyFileInfo.Mode().Perm()
	if privateKeyPermissions&0o077 != 0 {
		return PolicySigningSetupVerification{}, fmt.Errorf("private key %s permissions %04o are too broad; require 0600 or stricter", trimmedPrivateKeyPath, privateKeyPermissions)
	}

	rawPrivateKeyBytes, err := os.ReadFile(trimmedPrivateKeyPath)
	if err != nil {
		return PolicySigningSetupVerification{}, fmt.Errorf("read private key %s: %w", trimmedPrivateKeyPath, err)
	}
	privateKey, err := ParsePolicySigningPrivateKeyPEM(rawPrivateKeyBytes)
	if err != nil {
		return PolicySigningSetupVerification{}, fmt.Errorf("parse private key %s: %w", trimmedPrivateKeyPath, err)
	}
	derivedPublicKey, ok := privateKey.Public().(ed25519.PublicKey)
	if !ok {
		return PolicySigningSetupVerification{}, fmt.Errorf("private key %s did not yield an Ed25519 public key", trimmedPrivateKeyPath)
	}
	if !bytes.Equal(derivedPublicKey, trustedPublicKey) {
		return PolicySigningSetupVerification{}, fmt.Errorf("private key %s does not match trusted public key for key_id %q", trimmedPrivateKeyPath, trimmedExpectedKeyID)
	}

	return PolicySigningSetupVerification{
		ExpectedKeyID:        trimmedExpectedKeyID,
		SignaturePath:        PolicySignaturePath(repoRoot),
		SignatureKeyID:       signatureFile.KeyID,
		SignerKeyPath:        trimmedPrivateKeyPath,
		SignerKeyPermissions: privateKeyPermissions,
	}, nil
}
