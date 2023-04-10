/*
Copyright 2021 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package certificates

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"

	"go.uber.org/zap"
	"knative.dev/pkg/logging"
)

var randReader = rand.Reader
var serialNumberLimit = new(big.Int).Lsh(big.NewInt(1), 128)

// Create template common to all certificates
func createCertTemplate(expirationInterval time.Duration, sans []string) (*x509.Certificate, error) {
	serialNumber, err := rand.Int(randReader, serialNumberLimit)
	if err != nil {
		return nil, errors.New("failed to generate serial number: " + err.Error())
	}

	tmpl := x509.Certificate{
		SerialNumber:          serialNumber,
		SignatureAlgorithm:    x509.SHA256WithRSA,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(expirationInterval),
		BasicConstraintsValid: true,
		DNSNames:              sans,
	}
	return &tmpl, nil
}

// Create cert template suitable for CA and hence signing
func createCACertTemplate(expirationInterval time.Duration) (*x509.Certificate, error) {
	rootCert, err := createCertTemplate(expirationInterval, []string{})
	if err != nil {
		return nil, err
	}
	// Make it into a CA cert and change it so we can use it to sign certs
	rootCert.IsCA = true
	rootCert.KeyUsage = x509.KeyUsageCertSign
	rootCert.Subject = pkix.Name{
		Organization: []string{Organization},
	}
	return rootCert, nil
}

// Create cert template that we can use on the client/server for TLS
func createTransportCertTemplate(expirationInterval time.Duration, sans []string) (*x509.Certificate, error) {
	cert, err := createCertTemplate(expirationInterval, sans)
	if err != nil {
		return nil, err
	}
	cert.KeyUsage = x509.KeyUsageDigitalSignature
	cert.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}
	cert.Subject = pkix.Name{
		Organization: []string{Organization},
		CommonName:   "control-protocol-certificate",
	}
	return cert, err
}

func createCert(template, parent *x509.Certificate, pub, parentPriv interface{}) (cert *x509.Certificate, certPEM *pem.Block, err error) {
	certDER, err := x509.CreateCertificate(rand.Reader, template, parent, pub, parentPriv)
	if err != nil {
		return
	}
	cert, err = x509.ParseCertificate(certDER)
	if err != nil {
		return
	}
	certPEM = &pem.Block{Type: "CERTIFICATE", Bytes: certDER}
	return
}

// CreateCACerts generates the root CA cert
func CreateCACerts(ctx context.Context, expirationInterval time.Duration) (*KeyPair, error) {
	logger := logging.FromContext(ctx)
	caKeyPair, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		logger.Errorw("error generating random key", zap.Error(err))
		return nil, err
	}

	rootCertTmpl, err := createCACertTemplate(expirationInterval)
	if err != nil {
		logger.Errorw("error generating CA cert", zap.Error(err))
		return nil, err
	}

	_, caCertPem, err := createCert(rootCertTmpl, rootCertTmpl, &caKeyPair.PublicKey, caKeyPair)
	if err != nil {
		logger.Errorw("error signing the CA cert", zap.Error(err))
		return nil, err
	}
	caPrivateKeyPem := &pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(caKeyPair),
	}
	return NewKeyPair(caPrivateKeyPem, caCertPem), nil
}

// Deprecated: CreateControlPlaneCert generates the certificate for the client
func CreateControlPlaneCert(ctx context.Context, caKey *rsa.PrivateKey, caCertificate *x509.Certificate, expirationInterval time.Duration) (*KeyPair, error) {
	return CreateCert(ctx, caKey, caCertificate, expirationInterval, LegacyFakeDnsName)
}

// Deprecated: CreateDataPlaneCert generates the certificate for the server
func CreateDataPlaneCert(ctx context.Context, caKey *rsa.PrivateKey, caCertificate *x509.Certificate, expirationInterval time.Duration) (*KeyPair, error) {
	return CreateCert(ctx, caKey, caCertificate, expirationInterval, LegacyFakeDnsName)
}

// CreateCert generates the certificate for use by client and server
func CreateCert(ctx context.Context, caKey *rsa.PrivateKey, caCertificate *x509.Certificate, expirationInterval time.Duration, sans ...string) (*KeyPair, error) {
	logger := logging.FromContext(ctx)

	// Then create the private key for the serving cert
	keyPair, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		logger.Errorw("error generating random key", zap.Error(err))
		return nil, err
	}

	certTemplate, err := createTransportCertTemplate(expirationInterval, sans)
	if err != nil {
		logger.Errorw("failed to create the certificate template", zap.Error(err))
		return nil, err
	}

	// create a certificate which wraps the public key, sign it with the CA private key
	_, certPEM, err := createCert(certTemplate, caCertificate, &keyPair.PublicKey, caKey)
	if err != nil {
		logger.Errorw("error signing certificate template", zap.Error(err))
		return nil, err
	}

	privateKeyPEM := &pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(keyPair),
	}
	return NewKeyPair(privateKeyPEM, certPEM), nil
}

// ParseCert parses a certificate/private key pair from serialized pem blocks
func ParseCert(certPemBytes []byte, privateKeyPemBytes []byte) (*x509.Certificate, *rsa.PrivateKey, error) {
	certBlock, _ := pem.Decode(certPemBytes)
	if certBlock == nil {
		return nil, nil, fmt.Errorf("decoding the cert block returned nil")
	}
	if certBlock.Type != "CERTIFICATE" {
		return nil, nil, fmt.Errorf("bad pem block, expecting type 'CERTIFICATE', found %q", certBlock.Type)
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}

	pkBlock, _ := pem.Decode(privateKeyPemBytes)
	if pkBlock == nil {
		return nil, nil, fmt.Errorf("decoding the pk block returned nil")
	}
	if pkBlock.Type != "RSA PRIVATE KEY" {
		return nil, nil, fmt.Errorf("bad pem block, expecting type 'RSA PRIVATE KEY', found %q", pkBlock.Type)
	}
	pk, err := x509.ParsePKCS1PrivateKey(pkBlock.Bytes)
	return cert, pk, err
}

// ValidateCert checks the expiration of the certificate
func ValidateCert(cert *x509.Certificate, rotationThreshold time.Duration) error {
	if !cert.NotAfter.After(time.Now().Add(rotationThreshold)) {
		return fmt.Errorf("certificate is going to expire %v", cert.NotAfter)
	}
	return nil
}
