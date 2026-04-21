package config

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"loopgate/internal/identifiers"

	"gopkg.in/yaml.v3"
)

const (
	operatorOverrideDocumentSchemaVersion  = "1"
	operatorOverrideStateActive            = "active"
	operatorOverrideStateRevoked           = "revoked"
	operatorOverrideSignatureMessagePrefix = "loopgate-operator-overrides-v1\n"
)

type OperatorOverrideDocument struct {
	Version string                  `yaml:"version" json:"version"`
	Grants  []OperatorOverrideGrant `yaml:"grants" json:"grants"`
}

type OperatorOverrideGrant struct {
	ID           string   `yaml:"id" json:"id"`
	Class        string   `yaml:"class" json:"class"`
	State        string   `yaml:"state" json:"state"`
	PathPrefixes []string `yaml:"path_prefixes" json:"path_prefixes"`
	CreatedAtUTC string   `yaml:"created_at_utc" json:"created_at_utc"`
	RevokedAtUTC string   `yaml:"revoked_at_utc,omitempty" json:"revoked_at_utc,omitempty"`
}

type OperatorOverrideLoadResult struct {
	Document       OperatorOverrideDocument
	ContentSHA256  string
	SignatureKeyID string
	Present        bool
}

func OperatorOverrideDocumentPath(repoRoot string) string {
	return filepath.Join(repoRoot, "runtime", "state", "operator_overrides.yaml")
}

func OperatorOverrideSignaturePath(repoRoot string) string {
	return filepath.Join(repoRoot, "runtime", "state", "operator_overrides.yaml.sig")
}

func LoadOperatorOverrideDocumentWithHash(repoRoot string) (OperatorOverrideLoadResult, error) {
	documentPath := OperatorOverrideDocumentPath(repoRoot)
	rawDocumentBytes, err := os.ReadFile(documentPath)
	if err != nil {
		if os.IsNotExist(err) {
			return OperatorOverrideLoadResult{
				Document: OperatorOverrideDocument{
					Version: operatorOverrideDocumentSchemaVersion,
					Grants:  []OperatorOverrideGrant{},
				},
				Present: false,
			}, nil
		}
		return OperatorOverrideLoadResult{}, fmt.Errorf("read operator override document: %w", err)
	}

	signatureFile, err := LoadPolicySignatureFromPath(OperatorOverrideSignaturePath(repoRoot))
	if err != nil {
		return OperatorOverrideLoadResult{}, err
	}
	if err := VerifyOperatorOverrideDocumentSignature(rawDocumentBytes, signatureFile); err != nil {
		return OperatorOverrideLoadResult{}, err
	}

	document, err := ParseOperatorOverrideDocument(rawDocumentBytes)
	if err != nil {
		return OperatorOverrideLoadResult{}, err
	}
	contentHash := sha256.Sum256(rawDocumentBytes)
	return OperatorOverrideLoadResult{
		Document:       document,
		ContentSHA256:  hex.EncodeToString(contentHash[:]),
		SignatureKeyID: strings.TrimSpace(signatureFile.KeyID),
		Present:        true,
	}, nil
}

func ParseOperatorOverrideDocument(rawDocumentBytes []byte) (OperatorOverrideDocument, error) {
	var document OperatorOverrideDocument
	decoder := yaml.NewDecoder(bytes.NewReader(rawDocumentBytes))
	decoder.KnownFields(true)
	if err := decoder.Decode(&document); err != nil {
		return OperatorOverrideDocument{}, err
	}
	if err := applyOperatorOverrideDocumentDefaults(&document); err != nil {
		return OperatorOverrideDocument{}, err
	}
	return document, nil
}

func MarshalOperatorOverrideDocumentYAML(document OperatorOverrideDocument) ([]byte, error) {
	if err := applyOperatorOverrideDocumentDefaults(&document); err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(&document); err != nil {
		return nil, fmt.Errorf("marshal operator override yaml: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("close operator override yaml encoder: %w", err)
	}
	return buffer.Bytes(), nil
}

func WriteOperatorOverrideDocumentYAML(repoRoot string, document OperatorOverrideDocument) error {
	documentBytes, err := MarshalOperatorOverrideDocumentYAML(document)
	if err != nil {
		return err
	}
	documentPath := OperatorOverrideDocumentPath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(documentPath), 0o700); err != nil {
		return fmt.Errorf("create operator override directory: %w", err)
	}
	return atomicWriteFile(documentPath, documentBytes, 0o600)
}

func WriteOperatorOverrideSignatureYAML(repoRoot string, signatureFile PolicySignatureFile) error {
	signatureBytes, err := MarshalPolicySignatureYAML(signatureFile)
	if err != nil {
		return err
	}
	signaturePath := OperatorOverrideSignaturePath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(signaturePath), 0o700); err != nil {
		return fmt.Errorf("create operator override signature directory: %w", err)
	}
	return atomicWriteFile(signaturePath, signatureBytes, 0o600)
}

func SignOperatorOverrideDocument(rawDocumentBytes []byte, keyID string, privateKey ed25519.PrivateKey) (PolicySignatureFile, error) {
	return signDetachedDocument(rawDocumentBytes, operatorOverrideSignatureMessagePrefix, keyID, privateKey)
}

func VerifyOperatorOverrideDocumentSignature(rawDocumentBytes []byte, signatureFile PolicySignatureFile) error {
	return verifyDetachedDocumentSignature(rawDocumentBytes, operatorOverrideSignatureMessagePrefix, signatureFile)
}

func applyOperatorOverrideDocumentDefaults(document *OperatorOverrideDocument) error {
	if strings.TrimSpace(document.Version) == "" {
		document.Version = operatorOverrideDocumentSchemaVersion
	}
	if strings.TrimSpace(document.Version) != operatorOverrideDocumentSchemaVersion {
		return fmt.Errorf("operator override version must be %q", operatorOverrideDocumentSchemaVersion)
	}
	if len(document.Grants) == 0 {
		document.Grants = []OperatorOverrideGrant{}
		return nil
	}

	validatedGrantIDs := make(map[string]struct{}, len(document.Grants))
	validatedGrants := make([]OperatorOverrideGrant, 0, len(document.Grants))
	for _, grant := range document.Grants {
		grant.ID = strings.TrimSpace(grant.ID)
		if err := identifiers.ValidateSafeIdentifier("operator override grant id", grant.ID); err != nil {
			return fmt.Errorf("operator override grant id: %w", err)
		}
		if _, exists := validatedGrantIDs[grant.ID]; exists {
			return fmt.Errorf("operator override grant id %q is duplicated", grant.ID)
		}
		validatedGrantIDs[grant.ID] = struct{}{}

		grant.Class = strings.TrimSpace(grant.Class)
		if grant.Class != OperatorOverrideClassRepoEditSafe {
			return fmt.Errorf("operator override grant class %q is unsupported in this document version", grant.Class)
		}

		grant.State = strings.TrimSpace(grant.State)
		switch grant.State {
		case operatorOverrideStateActive, operatorOverrideStateRevoked:
		default:
			return fmt.Errorf("operator override grant %q state must be one of: %s, %s", grant.ID, operatorOverrideStateActive, operatorOverrideStateRevoked)
		}

		grant.PathPrefixes = normalizeConfiguredPaths(grant.PathPrefixes)
		if len(grant.PathPrefixes) == 0 {
			return fmt.Errorf("operator override grant %q path_prefixes must be explicitly configured", grant.ID)
		}
		if strings.TrimSpace(grant.CreatedAtUTC) == "" {
			return fmt.Errorf("operator override grant %q created_at_utc is required", grant.ID)
		}
		if grant.State == operatorOverrideStateRevoked && strings.TrimSpace(grant.RevokedAtUTC) == "" {
			return fmt.Errorf("operator override grant %q revoked_at_utc is required when state is revoked", grant.ID)
		}
		validatedGrants = append(validatedGrants, grant)
	}
	document.Grants = validatedGrants
	return nil
}

func ActiveOperatorOverrideGrants(document OperatorOverrideDocument, className string) []OperatorOverrideGrant {
	activeGrants := make([]OperatorOverrideGrant, 0, len(document.Grants))
	for _, grant := range document.Grants {
		if strings.TrimSpace(grant.Class) != strings.TrimSpace(className) {
			continue
		}
		if strings.TrimSpace(grant.State) != operatorOverrideStateActive {
			continue
		}
		activeGrants = append(activeGrants, grant)
	}
	return activeGrants
}

func operatorOverrideConfiguredPathRoot(configuredPath string, repoRoot string) string {
	trimmedConfiguredPath := strings.TrimSpace(configuredPath)
	if trimmedConfiguredPath == "" {
		return ""
	}
	if filepath.IsAbs(trimmedConfiguredPath) {
		return filepath.Clean(trimmedConfiguredPath)
	}
	return filepath.Clean(filepath.Join(repoRoot, trimmedConfiguredPath))
}

func OperatorOverrideGrantMatchesPath(grant OperatorOverrideGrant, targetPath string, repoRoot string) bool {
	for _, configuredPath := range grant.PathPrefixes {
		resolvedRoot := operatorOverrideConfiguredPathRoot(configuredPath, repoRoot)
		if resolvedRoot == "" {
			continue
		}
		if operatorOverridePathWithinRoot(targetPath, resolvedRoot) {
			return true
		}
	}
	return false
}

func operatorOverridePathWithinRoot(targetPath string, rootPath string) bool {
	relativePath, err := filepath.Rel(rootPath, targetPath)
	if err != nil {
		return false
	}
	if relativePath == "." {
		return true
	}
	return relativePath != ".." && !strings.HasPrefix(relativePath, ".."+string(filepath.Separator))
}
