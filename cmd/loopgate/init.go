package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"loopgate/internal/config"
	"loopgate/internal/identifiers"
)

const policySigningTrustDirEnv = "LOOPGATE_POLICY_SIGNING_TRUST_DIR"

const loopgateRepoRootEnv = "LOOPGATE_REPO_ROOT"

const localPolicySignerKeyIDMarkerName = "local_policy_signer_key_id"

type loopgateInitResult struct {
	KeyID              string
	SocketPath         string
	AlreadyInitialized bool
}

func runInit(args []string, stdout io.Writer, stderr io.Writer) error {
	initFlags := flag.NewFlagSet("init", flag.ContinueOnError)
	initFlags.SetOutput(stderr)

	repoRootFlag := initFlags.String("repo-root", "", "repository root containing core/policy/policy.yaml")
	keyIDFlag := initFlags.String("key-id", "", "policy signing key identifier (default: local-operator-<short-hostname>)")
	forceFlag := initFlags.Bool("force", false, "rotate the current local signer after moving existing key material to .bak")
	if err := initFlags.Parse(args); err != nil {
		return err
	}
	if initFlags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %s", strings.Join(initFlags.Args(), " "))
	}

	repoRoot, err := resolveLoopgateRepoRoot(strings.TrimSpace(*repoRootFlag))
	if err != nil {
		return err
	}
	keyID, err := resolveLoopgateInitKeyID(strings.TrimSpace(*keyIDFlag))
	if err != nil {
		return err
	}

	result, err := initializeLoopgatePolicySigning(repoRoot, keyID, *forceFlag)
	if err != nil {
		return err
	}
	if result.AlreadyInitialized {
		fmt.Fprintln(stdout, "already initialized")
		return nil
	}

	fmt.Fprintf(stdout, "key_id: %s\n", result.KeyID)
	fmt.Fprintf(stdout, "socket_path: %s\n", result.SocketPath)
	fmt.Fprintln(stdout, "next_command: ./bin/loopgate")
	return nil
}

func initializeLoopgatePolicySigning(repoRoot string, keyID string, force bool) (loopgateInitResult, error) {
	runtimeStateDir := filepath.Join(repoRoot, "runtime", "state")
	if err := os.MkdirAll(runtimeStateDir, 0o700); err != nil {
		return loopgateInitResult{}, fmt.Errorf("create runtime state directory: %w", err)
	}
	if err := os.Chmod(runtimeStateDir, 0o700); err != nil {
		return loopgateInitResult{}, fmt.Errorf("chmod runtime state directory: %w", err)
	}

	privateKeyPath, err := defaultOperatorPolicySigningPrivateKeyPath(keyID)
	if err != nil {
		return loopgateInitResult{}, err
	}
	publicKeyPath, err := defaultOperatorPolicySigningPublicKeyPath(keyID)
	if err != nil {
		return loopgateInitResult{}, err
	}

	if !force {
		if _, err := config.VerifyPolicySigningSetup(repoRoot, privateKeyPath, keyID); err == nil {
			if err := writeLocalPolicySignerKeyIDMarker(repoRoot, keyID); err != nil {
				return loopgateInitResult{}, err
			}
			return loopgateInitResult{
				KeyID:              keyID,
				SocketPath:         filepath.Join(runtimeStateDir, "loopgate.sock"),
				AlreadyInitialized: true,
			}, nil
		}
	}

	existingPrivateKey, existingPrivateKeyPresent, err := loadExistingOperatorPolicySigningPrivateKey(privateKeyPath)
	if err != nil {
		return loopgateInitResult{}, err
	}

	var signerPublicKey ed25519.PublicKey
	var signerPrivateKey ed25519.PrivateKey
	switch {
	case force:
		if err := backupExistingFileForForce(privateKeyPath); err != nil {
			return loopgateInitResult{}, err
		}
		if err := backupExistingFileForForce(publicKeyPath); err != nil {
			return loopgateInitResult{}, err
		}
		signerPublicKey, signerPrivateKey, err = ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return loopgateInitResult{}, fmt.Errorf("generate policy signing keypair: %w", err)
		}
	case existingPrivateKeyPresent:
		signerPrivateKey = existingPrivateKey
		derivedPublicKey, ok := signerPrivateKey.Public().(ed25519.PublicKey)
		if !ok {
			return loopgateInitResult{}, fmt.Errorf("existing private key %s did not yield an Ed25519 public key", privateKeyPath)
		}
		signerPublicKey = append(ed25519.PublicKey(nil), derivedPublicKey...)
	default:
		signerPublicKey, signerPrivateKey, err = ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return loopgateInitResult{}, fmt.Errorf("generate policy signing keypair: %w", err)
		}
	}

	if err := ensureOperatorPolicySigningPublicKeyPath(publicKeyPath, signerPublicKey, force); err != nil {
		return loopgateInitResult{}, err
	}
	if err := writeOperatorPolicySigningPrivateKey(privateKeyPath, signerPrivateKey); err != nil {
		return loopgateInitResult{}, err
	}
	if err := signRepoPolicyWithLocalOperatorKey(repoRoot, keyID); err != nil {
		return loopgateInitResult{}, err
	}

	if _, err := config.VerifyPolicySigningSetup(repoRoot, privateKeyPath, keyID); err != nil {
		return loopgateInitResult{}, fmt.Errorf("verify initialized policy signing setup: %w", err)
	}
	if err := writeLocalPolicySignerKeyIDMarker(repoRoot, keyID); err != nil {
		return loopgateInitResult{}, err
	}

	return loopgateInitResult{
		KeyID:      keyID,
		SocketPath: filepath.Join(runtimeStateDir, "loopgate.sock"),
	}, nil
}

func signRepoPolicyWithLocalOperatorKey(repoRoot string, keyID string) error {
	privateKeyPath, err := defaultOperatorPolicySigningPrivateKeyPath(keyID)
	if err != nil {
		return err
	}
	signerPrivateKey, signerPrivateKeyPresent, err := loadExistingOperatorPolicySigningPrivateKey(privateKeyPath)
	if err != nil {
		return err
	}
	if !signerPrivateKeyPresent {
		return fmt.Errorf("operator private key %s not found after initialization", privateKeyPath)
	}

	rawPolicyBytes, err := os.ReadFile(filepath.Join(repoRoot, "core", "policy", "policy.yaml"))
	if err != nil {
		return fmt.Errorf("read policy yaml: %w", err)
	}
	signatureFile, err := config.SignPolicyDocument(rawPolicyBytes, keyID, signerPrivateKey)
	if err != nil {
		return fmt.Errorf("sign policy yaml: %w", err)
	}
	if err := config.WritePolicySignatureYAML(repoRoot, signatureFile); err != nil {
		return fmt.Errorf("write policy signature yaml: %w", err)
	}
	return nil
}

func localPolicySignerKeyIDMarkerPath(repoRoot string) string {
	return filepath.Join(repoRoot, "runtime", "state", localPolicySignerKeyIDMarkerName)
}

func writeLocalPolicySignerKeyIDMarker(repoRoot string, keyID string) error {
	markerPath := localPolicySignerKeyIDMarkerPath(repoRoot)
	if err := os.WriteFile(markerPath, []byte(strings.TrimSpace(keyID)+"\n"), 0o600); err != nil {
		return fmt.Errorf("write local policy signer key marker: %w", err)
	}
	return nil
}

func loadLocalPolicySignerKeyIDMarker(repoRoot string) (string, bool, error) {
	rawMarkerBytes, err := os.ReadFile(localPolicySignerKeyIDMarkerPath(repoRoot))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read local policy signer key marker: %w", err)
	}
	keyID := strings.TrimSpace(string(rawMarkerBytes))
	if keyID == "" {
		return "", false, nil
	}
	return keyID, true, nil
}

func resolveLoopgateRepoRoot(flagValue string) (string, error) {
	if trimmedFlagValue := strings.TrimSpace(flagValue); trimmedFlagValue != "" {
		return filepath.Clean(trimmedFlagValue), nil
	}
	if repoRoot := resolveLoopgateRepoRootEnv(); repoRoot != "" {
		return repoRoot, nil
	}
	repoRoot, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("determine repo root: %w", err)
	}
	return filepath.Clean(repoRoot), nil
}

func resolveLoopgateRepoRootEnv() string {
	if repoRoot := strings.TrimSpace(os.Getenv(loopgateRepoRootEnv)); repoRoot != "" {
		return filepath.Clean(repoRoot)
	}
	return ""
}

func resolveLoopgateInitKeyID(flagValue string) (string, error) {
	keyID := strings.TrimSpace(flagValue)
	if keyID == "" {
		shortHostname, err := os.Hostname()
		if err != nil {
			return "", fmt.Errorf("determine hostname for default key_id: %w", err)
		}
		shortHostname = strings.TrimSpace(shortHostname)
		if idx := strings.Index(shortHostname, "."); idx >= 0 {
			shortHostname = shortHostname[:idx]
		}
		shortHostname = strings.ToLower(shortHostname)
		shortHostname = strings.ReplaceAll(shortHostname, "_", "-")
		shortHostname = strings.ReplaceAll(shortHostname, " ", "-")
		shortHostname = strings.Trim(shortHostname, "-")
		if shortHostname == "" {
			shortHostname = "local"
		}
		keyID = "local-operator-" + shortHostname
	}
	if err := identifiers.ValidateSafeIdentifier("loopgate init key_id", keyID); err != nil {
		return "", err
	}
	return keyID, nil
}

func defaultOperatorPolicySigningPrivateKeyPath(keyID string) (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("determine operator config directory: %w", err)
	}
	return filepath.Join(filepath.Clean(configDir), "Loopgate", "policy-signing", keyID+".pem"), nil
}

func defaultOperatorPolicySigningPublicKeyPath(keyID string) (string, error) {
	trustDir, err := resolveOperatorPolicySigningTrustDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(trustDir, keyID+".pub.pem"), nil
}

func resolveOperatorPolicySigningTrustDir() (string, error) {
	if trustDir := strings.TrimSpace(os.Getenv(policySigningTrustDirEnv)); trustDir != "" {
		return filepath.Clean(trustDir), nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("determine operator trust directory: %w", err)
	}
	return filepath.Join(filepath.Clean(configDir), "Loopgate", "policy-signing", "trusted"), nil
}

func loadExistingOperatorPolicySigningPrivateKey(privateKeyPath string) (ed25519.PrivateKey, bool, error) {
	rawPrivateKeyBytes, err := os.ReadFile(privateKeyPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read private key %s: %w", privateKeyPath, err)
	}
	privateKey, err := config.ParsePolicySigningPrivateKeyPEM(rawPrivateKeyBytes)
	if err != nil {
		return nil, false, fmt.Errorf("parse private key %s: %w", privateKeyPath, err)
	}
	return privateKey, true, nil
}

func ensureOperatorPolicySigningPublicKeyPath(publicKeyPath string, publicKey ed25519.PublicKey, force bool) error {
	if err := os.MkdirAll(filepath.Dir(publicKeyPath), 0o700); err != nil {
		return fmt.Errorf("create operator trust directory: %w", err)
	}
	if rawExistingPublicKeyBytes, err := os.ReadFile(publicKeyPath); err == nil {
		existingPublicKey, parseErr := config.ParsePolicySigningPublicKeyPEM(rawExistingPublicKeyBytes)
		if parseErr != nil {
			if !force {
				return fmt.Errorf("parse existing trust anchor %s: %w", publicKeyPath, parseErr)
			}
		} else if !publicKeysEqual(existingPublicKey, publicKey) && !force {
			return fmt.Errorf("existing trust anchor %s conflicts with the local signer; rerun with --force to rotate it", publicKeyPath)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read trust anchor %s: %w", publicKeyPath, err)
	}

	rawPublicKeyPEM, err := marshalPolicySigningPublicKeyPEM(publicKey)
	if err != nil {
		return err
	}
	if err := os.WriteFile(publicKeyPath, rawPublicKeyPEM, 0o644); err != nil {
		return fmt.Errorf("write trust anchor %s: %w", publicKeyPath, err)
	}
	return nil
}

func writeOperatorPolicySigningPrivateKey(privateKeyPath string, privateKey ed25519.PrivateKey) error {
	if err := os.MkdirAll(filepath.Dir(privateKeyPath), 0o700); err != nil {
		return fmt.Errorf("create operator key directory: %w", err)
	}
	rawPrivateKeyPEM, err := marshalPolicySigningPrivateKeyPEM(privateKey)
	if err != nil {
		return err
	}
	if err := os.WriteFile(privateKeyPath, rawPrivateKeyPEM, 0o600); err != nil {
		return fmt.Errorf("write private key %s: %w", privateKeyPath, err)
	}
	return nil
}

func marshalPolicySigningPrivateKeyPEM(privateKey ed25519.PrivateKey) ([]byte, error) {
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("marshal policy signing private key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privateKeyDER,
	}), nil
}

func marshalPolicySigningPublicKeyPEM(publicKey ed25519.PublicKey) ([]byte, error) {
	publicKeyDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal policy signing public key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyDER,
	}), nil
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

func backupExistingFileForForce(path string) error {
	fileInfo, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat %s before force rotation: %w", path, err)
	}
	if !fileInfo.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}

	backupPath := path + ".bak"
	for suffix := 1; ; suffix++ {
		if _, err := os.Stat(backupPath); errors.Is(err, os.ErrNotExist) {
			break
		} else if err != nil {
			return fmt.Errorf("stat backup path %s: %w", backupPath, err)
		}
		backupPath = fmt.Sprintf("%s.bak.%d", path, suffix)
	}
	if err := os.Rename(path, backupPath); err != nil {
		return fmt.Errorf("backup %s to %s: %w", path, backupPath, err)
	}
	return nil
}
