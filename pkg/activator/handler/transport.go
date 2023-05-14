/*
Copyright 2018 The Knative Authors

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

package handler

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"

	"golang.org/x/net/http2"
	"knative.dev/control-protocol/pkg/certificates"
	netcfg "knative.dev/networking/pkg/config"
	pkgnet "knative.dev/pkg/network"
	activatorconfig "knative.dev/serving/pkg/activator/config"
)

// verify is a SAN verifier and offers verifyConnection that verifies if the san is in the certificate DNSNames
type verify struct {
	san string
}

func (v *verify) verifyConnection(cs tls.ConnectionState) error {
	if cs.PeerCertificates == nil {
		return errors.New("server certificate not verified during VerifyConnection")
	}
	fmt.Printf("\tVerifyConnection looking for SAN %s\n", v.san)
	for _, name := range cs.PeerCertificates[0].DNSNames {
		if name == v.san {
			return nil
		}
	}
	return fmt.Errorf("service with san %s does not have a matching name in certificate - names provided: %s", v.san, cs.PeerCertificates[0].DNSNames)
}

// tlsWrapper is a tls.Config wrapper with a TLS dialer for HTTP1
type tlsWrapper struct {
	tlsConf *tls.Config
}

// dialTLSContextHTTP1 handles HTTPS:HTTP1 dialer
func (tw *tlsWrapper) dialTLSContextHTTP1(ctx context.Context, network, addr string) (net.Conn, error) {
	return dialTLSContext(ctx, network, addr, tw.tlsConf)
}

// dialTLSContext handles HTTPS:HTTP1 and HTTP2 dialers
// Depends on the activator handler's RevIDFrom
func dialTLSContext(ctx context.Context, network, addr string, tlsConf *tls.Config) (net.Conn, error) {
	config := activatorconfig.FromContext(ctx)
	trust := config.Trust
	fmt.Printf("\tdialTLSContext with trust %s\n", trust)
	if trust != netcfg.TrustDisabled {
		tlsConf = tlsConf.Clone()
		san := certificates.LegacyFakeDnsName
		if trust != netcfg.TrustMinimal {
			revID := RevIDFrom(ctx)
			san = "kn-user-" + revID.Namespace
		}
		fmt.Printf("\tverify trust %q of san %s\n", trust, san)
		v := &verify{san: san}
		tlsConf.VerifyConnection = v.verifyConnection
		fmt.Printf("\tdialTLSContext TLS with san %s\n", san)
	}
	return pkgnet.DialTLSWithBackOff(ctx, network, addr, tlsConf)
}

// newAutoTransport is a duplicate of the unexported knative/pkg's network newAutoTransport
func newAutoTransport(v1, v2 http.RoundTripper) http.RoundTripper {
	return pkgnet.RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		t := v1
		if r.ProtoMajor == 2 {
			t = v2
		}
		return t.RoundTrip(r)
	})
}

// newHTTPTransport is a duplicate of the unexported knative/pkg's network newHTTPTransport
func newHTTPTransport(disableKeepAlives, disableCompression bool, maxIdle, maxIdlePerHost int) http.RoundTripper {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = pkgnet.DialWithBackOff
	transport.DisableKeepAlives = disableKeepAlives
	transport.MaxIdleConns = maxIdle
	transport.MaxIdleConnsPerHost = maxIdlePerHost
	transport.ForceAttemptHTTP2 = false
	transport.DisableCompression = disableCompression
	return transport
}

// Depends on the activator handler's RevIDFrom
func newHTTPSTransport(disableKeepAlives, disableCompression bool, maxIdle, maxIdlePerHost int, tlsConf *tls.Config) http.RoundTripper {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = disableKeepAlives
	transport.MaxIdleConns = maxIdle
	transport.MaxIdleConnsPerHost = maxIdlePerHost
	transport.ForceAttemptHTTP2 = false
	transport.DisableCompression = disableCompression

	transport.TLSClientConfig = tlsConf
	tw := tlsWrapper{tlsConf: tlsConf}
	transport.DialTLSContext = tw.dialTLSContextHTTP1

	return transport
}

// Depends on the activator handler's RevIDFrom
func newH2CTransport(disableCompression bool) http.RoundTripper {
	return &http2.Transport{
		AllowHTTP:          true,
		DisableCompression: disableCompression,
		DialTLSContext:     dialTLSContext,
	}
}

// Depends on the activator handler's RevIDFrom
func newH2Transport(disableCompression bool, tlsConf *tls.Config) http.RoundTripper {
	return &http2.Transport{
		DisableCompression: disableCompression,
		DialTLSContext:     dialTLSContext,
		TLSClientConfig:    tlsConf,
	}
}

// Depends on the activator handler's RevIDFrom
func NewProxyAutoTLSTransport(maxIdle, maxIdlePerHost int, tlsConf *tls.Config) http.RoundTripper {
	return newAutoTransport(
		newHTTPSTransport(false, true, maxIdle, maxIdlePerHost, tlsConf),
		newH2Transport(true, tlsConf))
}

// Depends on the activator handler's RevIDFrom
func NewProxyAutoTransport(maxIdle, maxIdlePerHost int) http.RoundTripper {
	return newAutoTransport(
		newHTTPTransport(false, true, maxIdle, maxIdlePerHost),
		newH2CTransport(true))
}
