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

type verify struct {
	san string
}

type tlsWrapper struct {
	tlsConf *tls.Config
}

func (tw *tlsWrapper) HTTP1DialTLSContext(ctx context.Context, network, addr string) (net.Conn, error) {
	san := certificates.LegacyFakeDnsName
	config := activatorconfig.FromContext(ctx)
	trust := config.Trust
	if trust != netcfg.TrustMinimal {
		revID := RevIDFrom(ctx)
		san = "kn-user-" + revID.Namespace
	}
	v := &verify{san: san}

	fmt.Printf("\tHttp1 mysan %s during trust %s\n", san, trust)

	conf := tw.tlsConf.Clone()
	conf.VerifyConnection = v.VerifyConnection
	return pkgnet.DialTLSWithBackOff(ctx, network, addr, conf)
}

func (v *verify) VerifyConnection(cs tls.ConnectionState) error {
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

func HTTP2DialTLSContext(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
	var tlsConf *tls.Config
	config := activatorconfig.FromContext(ctx)
	trust := config.Trust
	fmt.Printf("\tHttp2  during trust %s\n", trust)
	if trust != netcfg.TrustDisabled {
		tlsConf = cfg.Clone()
		san := certificates.LegacyFakeDnsName
		if trust != netcfg.TrustMinimal {
			revID := RevIDFrom(ctx)
			san = "kn-user-" + revID.Namespace
		}
		v := &verify{san: san}
		tlsConf.VerifyConnection = v.VerifyConnection
		fmt.Printf("\tHttp2 TLS with san %s\n", san)
	}
	return pkgnet.DialTLSWithBackOff(ctx, network, addr, tlsConf)
	//return pkgnet.DialWithBackOff(ctx, network, addr)
}

func NewProxyAutoTLSTransport(maxIdle, maxIdlePerHost int, tlsConf *tls.Config) http.RoundTripper {
	return newAutoTransport(
		newHTTPSTransport(false, true, maxIdle, maxIdlePerHost, tlsConf),
		newH2Transport(true, tlsConf))
}

func NewProxyAutoTransport(maxIdle, maxIdlePerHost int) http.RoundTripper {
	return newAutoTransport(
		newHTTPTransport(false, true, maxIdle, maxIdlePerHost),
		newH2CTransport(true))
}

func newAutoTransport(v1, v2 http.RoundTripper) http.RoundTripper {
	return RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		t := v1
		if r.ProtoMajor == 2 {
			t = v2
		}
		return t.RoundTrip(r)
	})
}

type RoundTripperFunc func(*http.Request) (*http.Response, error)

// RoundTrip implements http.RoundTripper.
func (rt RoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return rt(r)
}

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

func newHTTPSTransport(disableKeepAlives, disableCompression bool, maxIdle, maxIdlePerHost int, tlsConf *tls.Config) http.RoundTripper {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	//transport.DialContext = pkgnet.DialWithBackOff
	transport.DisableKeepAlives = disableKeepAlives
	transport.MaxIdleConns = maxIdle
	transport.MaxIdleConnsPerHost = maxIdlePerHost
	transport.ForceAttemptHTTP2 = false
	transport.DisableCompression = disableCompression

	transport.TLSClientConfig = tlsConf
	tw := tlsWrapper{tlsConf: tlsConf}
	transport.DialTLSContext = tw.HTTP1DialTLSContext

	return transport
}

func newH2CTransport(disableCompression bool) http.RoundTripper {
	return &http2.Transport{
		AllowHTTP:          true,
		DisableCompression: disableCompression,
		DialTLSContext:     HTTP2DialTLSContext,
	}
}

func newH2Transport(disableCompression bool, tlsConf *tls.Config) http.RoundTripper {
	return &http2.Transport{
		DisableCompression: disableCompression,
		DialTLSContext:     HTTP2DialTLSContext,
		TLSClientConfig:    tlsConf,
	}
}
