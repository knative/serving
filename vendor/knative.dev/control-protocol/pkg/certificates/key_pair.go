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
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
)

type KeyPair struct {
	privateKeyBlock    *pem.Block
	privateKeyPemBytes []byte

	certBlock    *pem.Block
	certPemBytes []byte
}

func NewKeyPair(privateKey *pem.Block, cert *pem.Block) *KeyPair {
	return &KeyPair{
		privateKeyBlock:    privateKey,
		privateKeyPemBytes: pem.EncodeToMemory(privateKey),
		certBlock:          cert,
		certPemBytes:       pem.EncodeToMemory(cert),
	}
}

func (kh *KeyPair) PrivateKey() *pem.Block {
	return kh.privateKeyBlock
}

func (kh *KeyPair) PrivateKeyBytes() []byte {
	return kh.privateKeyPemBytes
}

func (kh *KeyPair) Cert() *pem.Block {
	return kh.certBlock
}

func (kh *KeyPair) CertBytes() []byte {
	return kh.certPemBytes
}

func (kh *KeyPair) Parse() (*x509.Certificate, *rsa.PrivateKey, error) {
	return ParseCert(kh.certPemBytes, kh.privateKeyPemBytes)
}
