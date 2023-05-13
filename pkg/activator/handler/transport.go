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
	pkgnet "knative.dev/pkg/network"
)

type HibrydTransport struct {
	HTTP1 *http.Transport
	HTTP2 *http2.Transport
	San   string
}

func (ht *HibrydTransport) HTTP1DialTLSContext(ctx context.Context, network, addr string) (net.Conn, error) {
	fmt.Println("\t Using Http1DialTLSContext")
	conf := ht.HTTP1.TLSClientConfig.Clone()
	conf.VerifyConnection = ht.VerifyConnection
	return pkgnet.DialTLSWithBackOff(ctx, network, addr, conf)
}

func (ht *HibrydTransport) HTTP2DialTLSContext(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
	fmt.Println("\t Using Http2DialTLSContext")
	conf := cfg.Clone()
	conf.VerifyConnection = ht.VerifyConnection
	return pkgnet.DialTLSWithBackOff(ctx, network, addr, conf)
}

func (ht *HibrydTransport) VerifyConnection(cs tls.ConnectionState) error {
	fmt.Printf("\t Using VerifyConnection with san %s\n", ht.San)
	if cs.PeerCertificates == nil {
		return errors.New("server certificate not verified during VerifyConnection")
	}
	for _, name := range cs.PeerCertificates[0].DNSNames {
		if name == ht.San {
			fmt.Printf("\tFound QP for san %s\n", ht.San)
			return nil
		}
	}

	return fmt.Errorf("service with san %s does not have a matching name in certificate - names provided: %s", ht.San, cs.PeerCertificates[0].DNSNames)
}

// NewProxyAutoTLSTransport is same with NewProxyAutoTransport but it has tls.Config to create HTTPS request.
// func NewProxyAutoTransport(maxIdle, maxIdlePerHost int) *activatorhandler.HibrydTransport
func NewProxyAutoTLSTransport(maxIdle, maxIdlePerHost int, tlsConf *tls.Config) *HibrydTransport {
	http1 := http.DefaultTransport.(*http.Transport).Clone()
	http1.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		fmt.Println("\t Http1 Dial without VerifyConnection")
		return pkgnet.DialTLSWithBackOff(ctx, network, addr, tlsConf)
	}

	http1.DisableKeepAlives = false
	http1.MaxIdleConns = maxIdle
	http1.MaxIdleConnsPerHost = maxIdlePerHost
	http1.ForceAttemptHTTP2 = false
	http1.DisableCompression = true
	http1.TLSClientConfig = tlsConf

	http2 := &http2.Transport{
		DisableCompression: true,
		TLSClientConfig:    tlsConf,
	}

	ht := &HibrydTransport{
		HTTP1: http1,
		HTTP2: http2,
		San:   certificates.LegacyFakeDnsName,
	}
	ht.HTTP1.DialTLSContext = ht.HTTP1DialTLSContext
	ht.HTTP2.DialTLSContext = ht.HTTP2DialTLSContext

	return ht
}

// HTTP
func NewProxyAutoTransport(maxIdle, maxIdlePerHost int) *HibrydTransport {
	http1 := http.DefaultTransport.(*http.Transport).Clone()
	http1.DialContext = pkgnet.DialWithBackOff
	http1.DisableKeepAlives = true
	http1.MaxIdleConns = maxIdle
	http1.MaxIdleConnsPerHost = maxIdlePerHost
	http1.ForceAttemptHTTP2 = false
	http1.DisableCompression = false

	http2 := &http2.Transport{
		AllowHTTP:          true,
		DisableCompression: true,
		DialTLSContext: func(ctx context.Context, netw, addr string, _ *tls.Config) (net.Conn, error) {
			fmt.Printf("\t Http2 Dial without VerifyConnection\n")
			return pkgnet.DialWithBackOff(ctx, netw, addr)
		},
	}
	ht := &HibrydTransport{
		HTTP1: http1,
		HTTP2: http2,
	}
	return ht
}
