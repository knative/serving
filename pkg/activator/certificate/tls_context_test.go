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
