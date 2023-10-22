/*
Copyright 2023 The Knative Authors

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

package certificate

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"testing"
)

// TestVerifySAN tests verifySAN.
func TestVerifySAN(t *testing.T) {
	tests := []struct {
		name   string
		san    string
		expErr bool
	}{{
		name:   "first SAN",
		san:    "knative-knative-serving",
		expErr: false,
	}, {
		name:   "second SAN",
		san:    "data-plane.knative.dev",
		expErr: false,
	}, {
		name:   "non existent SAN",
		san:    "foo",
		expErr: true,
	}}

	// tlsCrt contains two SANs knative-knative-serving and data-plane.knative.dev.
	block, _ := pem.Decode(tlsCrt)
	if block == nil {
		t.Fatal("failed to parse certificate PEM")
	}

	serverCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}

	tlsConnectionState := tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{serverCert},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := verifySAN(test.san)(tlsConnectionState)
			if test.expErr && err == nil {
				t.Fatalf("failed to verify SAN")
			}
			if !test.expErr && err != nil {
				t.Fatalf("failed to verify SAN: %v", err)
			}
		})
	}
}
