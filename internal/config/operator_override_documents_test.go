package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"
)

func TestLoadOperatorOverrideDocumentWithHash_MissingReturnsEmptyDocument(t *testing.T) {
	loadResult, err := LoadOperatorOverrideDocumentWithHash(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOperatorOverrideDocumentWithHash: %v", err)
	}
	if loadResult.Present {
		t.Fatal("expected missing operator override document to report Present=false")
	}
	if loadResult.Document.Version != "1" {
		t.Fatalf("expected default document version 1, got %q", loadResult.Document.Version)
	}
	if len(loadResult.Document.Grants) != 0 {
		t.Fatalf("expected no grants, got %#v", loadResult.Document.Grants)
	}
}

func TestLoadOperatorOverrideDocumentWithHash_RoundTripsSignedDocument(t *testing.T) {
	repoRoot := t.TempDir()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	t.Setenv("LOOPGATE_TEST_POLICY_SIGNING_KEY_ID", "loopgate-test-override-root")
	t.Setenv("LOOPGATE_TEST_POLICY_SIGNING_PUBLIC_KEY", base64.StdEncoding.EncodeToString(publicKey))

	document := OperatorOverrideDocument{
		Version: "1",
		Grants: []OperatorOverrideGrant{
			{
				ID:           "override-20260421010101-a1b2c3d4e5f6",
				Class:        OperatorOverrideClassRepoEditSafe,
				State:        "active",
				PathPrefixes: []string{"docs"},
				CreatedAtUTC: time.Now().UTC().Format(time.RFC3339),
			},
		},
	}
	documentBytes, err := MarshalOperatorOverrideDocumentYAML(document)
	if err != nil {
		t.Fatalf("MarshalOperatorOverrideDocumentYAML: %v", err)
	}
	signatureFile, err := SignOperatorOverrideDocument(documentBytes, "loopgate-test-override-root", privateKey)
	if err != nil {
		t.Fatalf("SignOperatorOverrideDocument: %v", err)
	}
	if err := WriteOperatorOverrideDocumentYAML(repoRoot, document); err != nil {
		t.Fatalf("WriteOperatorOverrideDocumentYAML: %v", err)
	}
	if err := WriteOperatorOverrideSignatureYAML(repoRoot, signatureFile); err != nil {
		t.Fatalf("WriteOperatorOverrideSignatureYAML: %v", err)
	}

	loadResult, err := LoadOperatorOverrideDocumentWithHash(repoRoot)
	if err != nil {
		t.Fatalf("LoadOperatorOverrideDocumentWithHash: %v", err)
	}
	if !loadResult.Present {
		t.Fatal("expected signed operator override document to be present")
	}
	if loadResult.SignatureKeyID != "loopgate-test-override-root" {
		t.Fatalf("expected signature key id loopgate-test-override-root, got %q", loadResult.SignatureKeyID)
	}
	if len(loadResult.Document.Grants) != 1 || loadResult.Document.Grants[0].PathPrefixes[0] != "docs" {
		t.Fatalf("expected round-tripped document grant, got %#v", loadResult.Document.Grants)
	}
}
