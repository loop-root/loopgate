package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"loopgate/internal/config"
)

const policySigningPrivateKeyFileEnv = "LOOPGATE_POLICY_SIGNING_PRIVATE_KEY_FILE"

func main() {
	signFlags := flag.NewFlagSet("loopgate-policy-sign", flag.ContinueOnError)
	signFlags.SetOutput(os.Stderr)

	repoRootFlag := signFlags.String("repo-root", "", "repository root containing core/policy/policy.yaml")
	policyPathFlag := signFlags.String("policy-file", "", "path to the policy YAML file to sign")
	privateKeyPathFlag := signFlags.String("private-key-file", "", "path to a PKCS#8 PEM-encoded Ed25519 private key (overrides "+policySigningPrivateKeyFileEnv+" and the default operator path)")
	keyIDFlag := signFlags.String("key-id", config.PolicySigningTrustAnchorKeyID, "trusted signing key identifier")
	verifySetupFlag := signFlags.Bool("verify-setup", false, "verify that the trusted public key, current policy signature, and operator signer key all line up")
	if err := signFlags.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	privateKeyPath, privateKeyPathSource, err := resolvePolicySigningPrivateKeyPath(strings.TrimSpace(*privateKeyPathFlag), strings.TrimSpace(os.Getenv(policySigningPrivateKeyFileEnv)), strings.TrimSpace(*keyIDFlag))
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(2)
	}

	repoRoot := strings.TrimSpace(*repoRootFlag)
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, "ERROR: determine repo root:", err)
			os.Exit(1)
		}
	}
	repoRoot = filepath.Clean(repoRoot)

	policyPath := strings.TrimSpace(*policyPathFlag)
	if policyPath == "" {
		policyPath = filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	} else if !filepath.IsAbs(policyPath) {
		policyPath = filepath.Join(repoRoot, policyPath)
	}

	if *verifySetupFlag {
		verificationResult, err := verifyPolicySigningSetup(repoRoot, privateKeyPath, privateKeyPathSource, strings.TrimSpace(*keyIDFlag))
		if err != nil {
			fmt.Fprintln(os.Stderr, "ERROR:", err)
			os.Exit(1)
		}
		printPolicySigningSetupVerification(verificationResult)
		return
	}

	rawPolicyBytes, err := os.ReadFile(policyPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: read policy file:", err)
		os.Exit(1)
	}

	rawPrivateKeyBytes, err := os.ReadFile(privateKeyPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: read private key:", err)
		if os.IsNotExist(err) && privateKeyPathSource == "default operator path" {
			fmt.Fprintf(os.Stderr, "Create or move the signer key to %s, or override with -private-key-file or %s.\n", privateKeyPath, policySigningPrivateKeyFileEnv)
		}
		os.Exit(1)
	}

	privateKey, err := config.ParsePolicySigningPrivateKeyPEM(rawPrivateKeyBytes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: parse private key:", err)
		os.Exit(1)
	}

	signatureFile, err := config.SignPolicyDocument(rawPolicyBytes, *keyIDFlag, privateKey)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: sign policy:", err)
		os.Exit(1)
	}
	if err := config.WritePolicySignatureYAML(repoRoot, signatureFile); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: write policy signature:", err)
		os.Exit(1)
	}

	fmt.Printf("Wrote %s\n", filepath.Join(repoRoot, "core", "policy", "policy.yaml.sig"))
}

func resolvePolicySigningPrivateKeyPath(flagValue string, envValue string, keyID string) (string, string, error) {
	if trimmedFlagValue := strings.TrimSpace(flagValue); trimmedFlagValue != "" {
		return filepath.Clean(trimmedFlagValue), "-private-key-file", nil
	}
	if trimmedEnvValue := strings.TrimSpace(envValue); trimmedEnvValue != "" {
		return filepath.Clean(trimmedEnvValue), policySigningPrivateKeyFileEnv, nil
	}
	defaultPath, err := defaultPolicySigningPrivateKeyPath(keyID)
	if err != nil {
		return "", "", err
	}
	return defaultPath, "default operator path", nil
}

func defaultPolicySigningPrivateKeyPath(keyID string) (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("determine default policy signing key directory: %w", err)
	}
	return defaultPolicySigningPrivateKeyPathForConfigDir(configDir, keyID), nil
}

func defaultPolicySigningPrivateKeyPathForConfigDir(configDir string, keyID string) string {
	return filepath.Join(filepath.Clean(configDir), "Loopgate", "policy-signing", strings.TrimSpace(keyID)+".pem")
}

type policySigningSetupVerification struct {
	ExpectedKeyID        string
	SignaturePath        string
	SignatureKeyID       string
	SignerKeyPath        string
	SignerKeyPathSource  string
	SignerKeyPermissions fs.FileMode
}

func verifyPolicySigningSetup(repoRoot string, privateKeyPath string, privateKeyPathSource string, expectedKeyID string) (policySigningSetupVerification, error) {
	verificationResult, err := config.VerifyPolicySigningSetup(repoRoot, privateKeyPath, expectedKeyID)
	if err != nil {
		return policySigningSetupVerification{}, err
	}
	return policySigningSetupVerification{
		ExpectedKeyID:        verificationResult.ExpectedKeyID,
		SignaturePath:        verificationResult.SignaturePath,
		SignatureKeyID:       verificationResult.SignatureKeyID,
		SignerKeyPath:        verificationResult.SignerKeyPath,
		SignerKeyPathSource:  privateKeyPathSource,
		SignerKeyPermissions: verificationResult.SignerKeyPermissions,
	}, nil
}

func printPolicySigningSetupVerification(verificationResult policySigningSetupVerification) {
	fmt.Println("Policy signing setup OK")
	fmt.Printf("key_id: %s\n", verificationResult.ExpectedKeyID)
	fmt.Printf("policy_signature: %s\n", verificationResult.SignaturePath)
	fmt.Printf("policy_signature_key_id: %s\n", verificationResult.SignatureKeyID)
	fmt.Printf("signer_key_path: %s\n", verificationResult.SignerKeyPath)
	fmt.Printf("signer_key_source: %s\n", verificationResult.SignerKeyPathSource)
	fmt.Printf("signer_key_permissions: %04o\n", verificationResult.SignerKeyPermissions)
}
