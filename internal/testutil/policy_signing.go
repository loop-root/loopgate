package testutil

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"morph/internal/config"
)

const TestPolicySigningKeyID = "loopgate-test-policy-root"

type PolicyTestSigner struct {
	KeyID      string
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
}

func NewPolicyTestSigner() (*PolicyTestSigner, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate test policy signing key: %w", err)
	}
	return &PolicyTestSigner{
		KeyID:      TestPolicySigningKeyID,
		PublicKey:  append(ed25519.PublicKey(nil), publicKey...),
		PrivateKey: append(ed25519.PrivateKey(nil), privateKey...),
	}, nil
}

func (signer *PolicyTestSigner) ConfigureEnv(setenv func(string, string)) {
	setenv("LOOPGATE_TEST_POLICY_SIGNING_KEY_ID", signer.KeyID)
	setenv("LOOPGATE_TEST_POLICY_SIGNING_PUBLIC_KEY", base64.StdEncoding.EncodeToString(signer.PublicKey))
}

func (signer *PolicyTestSigner) WriteSignedPolicyYAML(repoRoot string, policyYAML string) error {
	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		return fmt.Errorf("create policy dir: %w", err)
	}
	rawPolicyBytes := []byte(policyYAML)
	if err := os.WriteFile(policyPath, rawPolicyBytes, 0o600); err != nil {
		return fmt.Errorf("write policy yaml: %w", err)
	}
	signatureFile, err := config.SignPolicyDocument(rawPolicyBytes, signer.KeyID, signer.PrivateKey)
	if err != nil {
		return fmt.Errorf("sign policy yaml: %w", err)
	}
	if err := config.WritePolicySignatureYAML(repoRoot, signatureFile); err != nil {
		return fmt.Errorf("write policy signature yaml: %w", err)
	}
	return nil
}
