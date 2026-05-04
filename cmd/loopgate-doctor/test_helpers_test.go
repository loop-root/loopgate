package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"loopgate/internal/ledger"
)

type auditExportTestCertificates struct {
	RootCAPEM            string
	ClientCertificatePEM string
	ClientPrivateKeyPEM  string
}

func generateAuditExportTestCertificates(t *testing.T) auditExportTestCertificates {
	t.Helper()

	rootPrivateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate root private key: %v", err)
	}
	rootTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Loopgate Test Root CA"},
		NotBefore:             time.Now().UTC().Add(-1 * time.Hour),
		NotAfter:              time.Now().UTC().Add(7 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}
	rootCertificateDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &rootPrivateKey.PublicKey, rootPrivateKey)
	if err != nil {
		t.Fatalf("create root certificate: %v", err)
	}
	rootCertificate, err := x509.ParseCertificate(rootCertificateDER)
	if err != nil {
		t.Fatalf("parse root certificate: %v", err)
	}
	rootCAPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootCertificateDER}))

	clientPrivateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate client private key: %v", err)
	}
	clientTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "Loopgate Test Client"},
		NotBefore:             time.Now().UTC().Add(-1 * time.Hour),
		NotAfter:              time.Now().UTC().Add(48 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	clientCertificateDER, err := x509.CreateCertificate(rand.Reader, clientTemplate, rootCertificate, &clientPrivateKey.PublicKey, rootPrivateKey)
	if err != nil {
		t.Fatalf("create client certificate: %v", err)
	}
	clientCertificatePEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientCertificateDER}))
	clientPrivateKeyDER, err := x509.MarshalPKCS8PrivateKey(clientPrivateKey)
	if err != nil {
		t.Fatalf("marshal client private key: %v", err)
	}
	clientPrivateKeyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: clientPrivateKeyDER}))

	return auditExportTestCertificates{
		RootCAPEM:            rootCAPEM,
		ClientCertificatePEM: clientCertificatePEM,
		ClientPrivateKeyPEM:  clientPrivateKeyPEM,
	}
}

func appendDoctorAuditEventForTest(t *testing.T, activeAuditPath string, timestamp string, eventType string, auditSequence int64, data map[string]interface{}) {
	appendDoctorAuditEventForSessionTest(t, activeAuditPath, "session-1", timestamp, eventType, auditSequence, data)
}

func appendDoctorAuditEventForSessionTest(t *testing.T, activeAuditPath string, sessionID string, timestamp string, eventType string, auditSequence int64, data map[string]interface{}) {
	t.Helper()

	copied := map[string]interface{}{}
	for key, value := range data {
		copied[key] = value
	}
	copied["audit_sequence"] = auditSequence
	if err := ledger.Append(activeAuditPath, ledger.NewEvent(timestamp, eventType, sessionID, copied)); err != nil {
		t.Fatalf("append audit event: %v", err)
	}
}
