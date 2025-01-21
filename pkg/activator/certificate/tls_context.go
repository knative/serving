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
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"

	"knative.dev/networking/pkg/certificates"
	pkgnet "knative.dev/pkg/network"
	"knative.dev/serving/pkg/activator/handler"
)

// TLSContext returns DialTLSContextFunc.
func (cr *CertCache) TLSContext() pkgnet.DialTLSContextFunc {
	return cr.dialTLSContext
}

// dialTLSContext handles TLS dialer
func (cr *CertCache) dialTLSContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return dialTLSContext(ctx, network, addr, cr)
}

// dialTLSContext handles verify SAN before calling DialTLSWithBackOff.
func dialTLSContext(ctx context.Context, network, addr string, cr *CertCache) (net.Conn, error) {
	cr.certificatesMux.RLock()
	// Clone the certificate Pool such that the one used by the client will be different from the one that will get updated is CA is replaced.
	tlsConf := cr.TLSConf.Clone()
	tlsConf.RootCAs = tlsConf.RootCAs.Clone()
	cr.certificatesMux.RUnlock()

	revID := handler.RevIDFrom(ctx)
	san := certificates.DataPlaneUserSAN(revID.Namespace)

	tlsConf.VerifyConnection = verifySAN(san, revID.Name)
	tlsConf.InsecureSkipVerify = true
	return pkgnet.DialTLSWithBackOff(ctx, network, addr, tlsConf)
}

func verifySAN(san, rev string) func(tls.ConnectionState) error {
	return func(cs tls.ConnectionState) error {
		log.Printf("In verifySAN1: %s-%s", san, rev)
		if len(cs.PeerCertificates) == 0 {
			return errors.New("no PeerCertificates provided")
		}
		log.Printf("In verifySAN2: %s-%s", san, rev)
		for _, name := range cs.PeerCertificates[0].DNSNames {
			if name == san {
				log.Printf("In verifySAN3 %s-%s\n", name, rev)
				return nil
			}
		}
		return fmt.Errorf("san %q does not have a matching name in %v", san, cs.PeerCertificates[0].DNSNames)
	}
}
