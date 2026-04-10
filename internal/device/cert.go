package device

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"
)

// ProvisioningBundle contains everything a device needs to connect via mTLS
type ProvisioningBundle struct {
	DeviceID   string
	CACert     []byte
	ClientCert []byte
	ClientKey  []byte
}

// GenerateDeviceBundle creates a new identity (key + signed cert) for a device
func GenerateDeviceBundle(deviceID string, caCertRaw []byte, caKeyRaw []byte) (*ProvisioningBundle, error) {
	// 1. Decode and Parse CA Cert
	caCertBlock, _ := pem.Decode(caCertRaw)
	if caCertBlock == nil {
		return nil, fmt.Errorf("failed to decode CA cert PEM")
	}
	caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("invalid CA cert: %w", err)
	}

	// 2. Decode and Parse CA Key
	caKeyBlock, _ := pem.Decode(caKeyRaw)
	if caKeyBlock == nil {
		return nil, fmt.Errorf("failed to decode CA key PEM")
	}
	caKey, err := x509.ParseECPrivateKey(caKeyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("invalid CA key: %w", err)
	}

	// 2. Generate Device Keypair (ECDSA P-256 as decided in DECISIONS.md)
	deviceKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate device key: %w", err)
	}

	// 3. Create Certificate Template
	serialNumber, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: deviceID, // Important: This is used for 'use_identity_as_username'
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(1, 0, 0), // 1 Year validity as decided
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	// 4. Sign the Certificate
	deviceCertDER, err := x509.CreateCertificate(rand.Reader, &template, caCert, &deviceKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign device cert: %w", err)
	}

	// 5. Encode to PEM
	deviceKeyBytes, _ := x509.MarshalECPrivateKey(deviceKey)
	deviceKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: deviceKeyBytes})
	deviceCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: deviceCertDER})

	// Re-encode CA Cert to PEM for the bundle
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertRaw})

	return &ProvisioningBundle{
		DeviceID:   deviceID,
		CACert:     caPEM,
		ClientCert: deviceCertPEM,
		ClientKey:  deviceKeyPEM,
	}, nil
}
