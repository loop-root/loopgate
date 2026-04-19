package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"loopgate/internal/config"
)

const policySigningPrivateKeyFileEnv = "LOOPGATE_POLICY_SIGNING_PRIVATE_KEY_FILE"

func main() {
	os.Exit(runPolicySignCLI(os.Args[1:], os.Stdout, os.Stderr, os.Getwd, os.Getenv))
}

func runPolicySignCLI(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	getwd func() (string, error),
	getenv func(string) string,
) int {
	signFlags := flag.NewFlagSet("loopgate-policy-sign", flag.ContinueOnError)
	signFlags.SetOutput(stderr)

	repoRootFlag := signFlags.String("repo-root", "", "repository root containing core/policy/policy.yaml")
	policyPathFlag := signFlags.String("policy-file", "", "path to the policy YAML file to sign")
	privateKeyPathFlag := signFlags.String("private-key-file", "", "path to a PKCS#8 PEM-encoded Ed25519 private key (overrides "+policySigningPrivateKeyFileEnv+" and the default operator path)")
	keyIDFlag := signFlags.String("key-id", "", "trusted signing key identifier (defaults to the current signed policy key_id for -verify-setup, otherwise the built-in Loopgate trust anchor key_id)")
	verifySetupFlag := signFlags.Bool("verify-setup", false, "verify that the trusted public key, current policy signature, and operator signer key all line up")
	if err := signFlags.Parse(args); err != nil {
		return 2
	}

	repoRoot := strings.TrimSpace(*repoRootFlag)
	if repoRoot == "" {
		var err error
		repoRoot, err = getwd()
		if err != nil {
			fmt.Fprintln(stderr, "ERROR: determine repo root:", err)
			return 1
		}
	}
	repoRoot = filepath.Clean(repoRoot)

	effectiveKeyID, err := resolvePolicySigningKeyID(repoRoot, strings.TrimSpace(*keyIDFlag), *verifySetupFlag)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 2
	}

	privateKeyPath, privateKeyPathSource, err := resolvePolicySigningPrivateKeyPath(strings.TrimSpace(*privateKeyPathFlag), strings.TrimSpace(getenv(policySigningPrivateKeyFileEnv)), effectiveKeyID)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 2
	}

	policyPath := strings.TrimSpace(*policyPathFlag)
	if policyPath == "" {
		policyPath = filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	} else if !filepath.IsAbs(policyPath) {
		policyPath = filepath.Join(repoRoot, policyPath)
	}

	if *verifySetupFlag {
		verificationResult, err := verifyPolicySigningSetup(repoRoot, privateKeyPath, privateKeyPathSource, effectiveKeyID)
		if err != nil {
			fmt.Fprintln(stderr, "ERROR:", err)
			return 1
		}
		printPolicySigningSetupVerification(stdout, verificationResult)
		return 0
	}

	rawPolicyBytes, err := os.ReadFile(policyPath)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: read policy file:", err)
		return 1
	}

	rawPrivateKeyBytes, err := os.ReadFile(privateKeyPath)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: read private key:", err)
		if os.IsNotExist(err) && privateKeyPathSource == "default operator path" {
			fmt.Fprintf(stderr, "Create or move the signer key to %s, or override with -private-key-file or %s.\n", privateKeyPath, policySigningPrivateKeyFileEnv)
		}
		return 1
	}

	privateKey, err := config.ParsePolicySigningPrivateKeyPEM(rawPrivateKeyBytes)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: parse private key:", err)
		return 1
	}

	signatureFile, err := config.SignPolicyDocument(rawPolicyBytes, effectiveKeyID, privateKey)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: sign policy:", err)
		return 1
	}
	if err := config.WritePolicySignatureYAML(repoRoot, signatureFile); err != nil {
		fmt.Fprintln(stderr, "ERROR: write policy signature:", err)
		return 1
	}

	fmt.Fprintf(stdout, "Wrote %s\n", filepath.Join(repoRoot, "core", "policy", "policy.yaml.sig"))
	return 0
}

func resolvePolicySigningKeyID(repoRoot string, requestedKeyID string, verifySetup bool) (string, error) {
	trimmedRequestedKeyID := strings.TrimSpace(requestedKeyID)
	if trimmedRequestedKeyID != "" {
		return trimmedRequestedKeyID, nil
	}
	if !verifySetup {
		return config.PolicySigningTrustAnchorKeyID, nil
	}
	signatureFile, err := config.LoadPolicySignatureFile(repoRoot)
	if err != nil {
		return "", fmt.Errorf("load policy signature for verify-setup key_id default: %w", err)
	}
	return strings.TrimSpace(signatureFile.KeyID), nil
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

func printPolicySigningSetupVerification(output io.Writer, verificationResult policySigningSetupVerification) {
	fmt.Fprintln(output, "Policy signing setup OK")
	fmt.Fprintf(output, "key_id: %s\n", verificationResult.ExpectedKeyID)
	fmt.Fprintf(output, "policy_signature: %s\n", verificationResult.SignaturePath)
	fmt.Fprintf(output, "policy_signature_key_id: %s\n", verificationResult.SignatureKeyID)
	fmt.Fprintf(output, "signer_key_path: %s\n", verificationResult.SignerKeyPath)
	fmt.Fprintf(output, "signer_key_source: %s\n", verificationResult.SignerKeyPathSource)
	fmt.Fprintf(output, "signer_key_permissions: %04o\n", verificationResult.SignerKeyPermissions)
}
