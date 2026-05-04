package loopgate

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"
)

type auditExportTestCertificates struct {
	RootCAPEM            string
	ServerCertificatePEM string
	ServerPrivateKeyPEM  string
	ClientCertificatePEM string
	ClientPrivateKeyPEM  string
}

func generateAuditExportTestCertificates(t *testing.T) auditExportTestCertificates {
	t.Helper()

	rootPublicKey, rootPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate root key: %v", err)
	}
	rootTemplate := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "loopgate-test-root",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(30 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, &rootTemplate, &rootTemplate, rootPublicKey, rootPrivateKey)
	if err != nil {
		t.Fatalf("create root certificate: %v", err)
	}

	serverPublicKey, serverPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	serverTemplate := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			CommonName: "127.0.0.1",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		DNSNames:              []string{"localhost"},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, &serverTemplate, &rootTemplate, serverPublicKey, rootPrivateKey)
	if err != nil {
		t.Fatalf("create server certificate: %v", err)
	}

	clientPublicKey, clientPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate client key: %v", err)
	}
	clientTemplate := x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject: pkix.Name{
			CommonName: "loopgate-test-client",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	clientDER, err := x509.CreateCertificate(rand.Reader, &clientTemplate, &rootTemplate, clientPublicKey, rootPrivateKey)
	if err != nil {
		t.Fatalf("create client certificate: %v", err)
	}

	return auditExportTestCertificates{
		RootCAPEM:            encodeCertificatePEM(rootDER),
		ServerCertificatePEM: encodeCertificatePEM(serverDER),
		ServerPrivateKeyPEM:  encodePrivateKeyPEM(t, serverPrivateKey),
		ClientCertificatePEM: encodeCertificatePEM(clientDER),
		ClientPrivateKeyPEM:  encodePrivateKeyPEM(t, clientPrivateKey),
	}
}

func encodeCertificatePEM(derBytes []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes}))
}

func encodePrivateKeyPEM(t *testing.T, privateKey ed25519.PrivateKey) string {
	t.Helper()

	privateKeyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal pkcs8 private key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyBytes}))
}

func certificatePublicKeyPinSHA256(t *testing.T, certificatePEM string) string {
	t.Helper()

	decodedBlock, _ := pem.Decode([]byte(certificatePEM))
	if decodedBlock == nil {
		t.Fatal("decode certificate pem")
		return ""
	}
	parsedCertificate, err := x509.ParseCertificate(decodedBlock.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(parsedCertificate.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	publicKeyDigest := sha256.Sum256(publicKeyBytes)
	return hex.EncodeToString(publicKeyDigest[:])
}
