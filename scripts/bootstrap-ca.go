package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

const certDir = "certs"

func main() {
	if err := os.MkdirAll(certDir, 0755); err != nil {
		log.Fatalf("failed to create certs directory: %v", err)
	}

	// 1. Generate Root CA
	caPrivKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatalf("failed to generate CA key: %v", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"FieldPulse IoT"},
			CommonName:   "FieldPulse Root CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	caBytes, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caPrivKey.PublicKey, caPrivKey)
	if err != nil {
		log.Fatalf("failed to create CA certificate: %v", err)
	}

	savePEM(filepath.Join(certDir, "ca.crt"), "CERTIFICATE", caBytes)
	saveKey(filepath.Join(certDir, "ca.key"), caPrivKey)

	// 2. Generate Broker Cert
	brokerPrivKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatalf("failed to generate broker key: %v", err)
	}

	brokerTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization: []string{"FieldPulse IoT"},
			CommonName:   "localhost", // Or specific broker hostname
		},
		DNSNames:  []string{"localhost", "mosquitto"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore: time.Now(),
		NotAfter:  time.Now().AddDate(1, 0, 0),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}

	brokerBytes, err := x509.CreateCertificate(rand.Reader, brokerTemplate, caTemplate, &brokerPrivKey.PublicKey, caPrivKey)
	if err != nil {
		log.Fatalf("failed to create broker certificate: %v", err)
	}

	savePEM(filepath.Join(certDir, "broker.crt"), "CERTIFICATE", brokerBytes)
	saveKey(filepath.Join(certDir, "broker.key"), brokerPrivKey)

	// 3. Generate Test Client Cert
	clientPrivKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatalf("failed to generate client key: %v", err)
	}

	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject: pkix.Name{
			Organization: []string{"FieldPulse IoT"},
			CommonName:   "test-client",
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().AddDate(1, 0, 0),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature,
	}

	clientBytes, err := x509.CreateCertificate(rand.Reader, clientTemplate, caTemplate, &clientPrivKey.PublicKey, caPrivKey)
	if err != nil {
		log.Fatalf("failed to create client certificate: %v", err)
	}

	savePEM(filepath.Join(certDir, "client.crt"), "CERTIFICATE", clientBytes)
	saveKey(filepath.Join(certDir, "client.key"), clientPrivKey)

	log.Println("Root CA, Broker, and Client certificates generated successfully in ./certs")
}

func savePEM(filename, typeLabel string, data []byte) {
	f, err := os.Create(filename)
	if err != nil {
		log.Fatalf("failed to create file %s: %v", filename, err)
	}
	defer f.Close()
	pem.Encode(f, &pem.Block{Type: typeLabel, Bytes: data})
}

func saveKey(filename string, key *ecdsa.PrivateKey) {
	f, err := os.Create(filename)
	if err != nil {
		log.Fatalf("failed to create file %s: %v", filename, err)
	}
	defer f.Close()
	b, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		log.Fatalf("failed to marshal private key: %v", err)
	}
	pem.Encode(f, &pem.Block{Type: "EC PRIVATE KEY", Bytes: b})
}
