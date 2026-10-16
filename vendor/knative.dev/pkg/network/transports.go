/*
Copyright 2019 The Knative Authors

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

package network

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

// RoundTripperFunc implementation roundtrips a request.
type RoundTripperFunc func(*http.Request) (*http.Response, error)

// RoundTrip implements http.RoundTripper.
func (rt RoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return rt(r)
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

const sleep = 30 * time.Millisecond

var backOffTemplate = wait.Backoff{
	Duration: 50 * time.Millisecond,
	Factor:   1.4,
	Jitter:   0.1, // At most 10% jitter.
	Steps:    15,
}

// ErrTimeoutDialing when the timeout is reached after set amount of time.
var ErrTimeoutDialing = errors.New("timed out dialing")

// DialWithBackOff executes `net.Dialer.DialContext()` with exponentially increasing
// dial timeouts. In addition it sleeps with random jitter between tries.
var DialWithBackOff = NewBackoffDialer(backOffTemplate)

// NewBackoffDialer returns a dialer that executes `net.Dialer.DialContext()` with
// exponentially increasing dial timeouts. In addition it sleeps with random jitter
// between tries.
func NewBackoffDialer(backoffConfig wait.Backoff) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		return dialBackOffHelper(ctx, network, address, backoffConfig, nil)
	}
}

// DialTLSWithBackOff is same with DialWithBackOff but takes tls config.
//
// ALPN contract: when tlsConf.NextProtos is empty, the dialer applies the
// protocols requested via the dial context (see withH2ALPN), so the h2
// transport negotiates h2. A caller that sets NextProtos itself is untouched.
var DialTLSWithBackOff = NewTLSBackoffDialer(backOffTemplate)

// NewTLSBackoffDialer is same with NewBackoffDialer but takes tls config.
func NewTLSBackoffDialer(backoffConfig wait.Backoff) func(context.Context, string, string, *tls.Config) (net.Conn, error) {
	return func(ctx context.Context, network, address string, tlsConf *tls.Config) (net.Conn, error) {
		return dialBackOffHelper(ctx, network, address, backoffConfig, tlsConf)
	}
}

// alpnContextKey carries requested ALPN protocols from the transport to the
// dialer. ALPN intent (h2 vs HTTP/1.1) is known at the transport, but the
// tls.Config is built by the caller and reaches only the shared dialer, and
// net/http's custom DialTLSContext hook carries no *tls.Config. So the protocols
// are passed out-of-band through the dial context, keeping DialTLSContextFunc
// unchanged (the alternative is a breaking API change to pass them explicitly).
type alpnContextKey struct{}

// withH2ALPN requests h2 ALPN for dials on ctx.
func withH2ALPN(ctx context.Context) context.Context {
	return context.WithValue(ctx, alpnContextKey{}, []string{"h2"})
}

func dialBackOffHelper(ctx context.Context, network, address string, bo wait.Backoff, tlsConf *tls.Config) (net.Conn, error) {
	// net/http sets no ALPN for custom TLS dialers; apply requested protocols
	// when the caller set none.
	if tlsConf != nil && len(tlsConf.NextProtos) == 0 {
		if protos, ok := ctx.Value(alpnContextKey{}).([]string); ok {
			tlsConf = tlsConf.Clone()
			tlsConf.NextProtos = protos
		}
	}
	dialer := &net.Dialer{
		Timeout:   bo.Duration, // Initial duration.
		KeepAlive: 5 * time.Second,
	}
	start := time.Now()
	for {
		var (
			c   net.Conn
			err error
		)
		if tlsConf == nil {
			c, err = dialer.DialContext(ctx, network, address)
		} else {
			d := tls.Dialer{NetDialer: dialer, Config: tlsConf}
			c, err = d.DialContext(ctx, network, address)
		}
		if err != nil {
			var errNet net.Error
			if errors.As(err, &errNet) && errNet.Timeout() {
				if bo.Steps < 1 {
					break
				}
				dialer.Timeout = bo.Step()
				time.Sleep(wait.Jitter(sleep, 1.0)) // Sleep with jitter.
				continue
			}
			return nil, err
		}
		return c, nil
	}
	elapsed := time.Since(start)
	return nil, fmt.Errorf("%w %s after %.2fs", ErrTimeoutDialing, address, elapsed.Seconds())
}

func newHTTPTransport(disableKeepAlives, disableCompression bool, maxIdle, maxIdlePerHost int) http.RoundTripper {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = DialWithBackOff
	transport.DisableKeepAlives = disableKeepAlives
	transport.MaxIdleConns = maxIdle
	transport.MaxIdleConnsPerHost = maxIdlePerHost
	transport.ForceAttemptHTTP2 = false
	transport.DisableCompression = disableCompression
	return transport
}

type DialTLSContextFunc func(ctx context.Context, network, addr string) (net.Conn, error)

func newHTTPSTransport(disableKeepAlives, disableCompression bool, maxIdle, maxIdlePerHost int, tlsContext DialTLSContextFunc) http.RoundTripper {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = disableKeepAlives
	transport.MaxIdleConns = maxIdle
	transport.MaxIdleConnsPerHost = maxIdlePerHost
	transport.ForceAttemptHTTP2 = false
	transport.DisableCompression = disableCompression
	transport.DialTLSContext = tlsContext

	return transport
}

// NewProberTransport creates a RoundTripper that is useful for probing,
// since it will not cache connections.
func NewProberTransport() http.RoundTripper {
	return newAutoTransport(
		newHTTPTransport(true /*disable keep-alives*/, false /*disable auto-compression*/, 0, 0 /*no caching*/),
		NewH2CTransport())
}

// NewProxyAutoTLSTransport is same with NewProxyAutoTransport but it has DialTLSContextFunc to create HTTPS request.
func NewProxyAutoTLSTransport(maxIdle, maxIdlePerHost int, tlsContext DialTLSContextFunc) http.RoundTripper {
	return newAutoTransport(
		newHTTPSTransport(false /*disable keep-alives*/, true /*disable auto-compression*/, maxIdle, maxIdlePerHost, tlsContext),
		newH2Transport(true /*disable auto-compression*/, tlsContext))
}

// NewAutoTransport creates a RoundTripper that can use appropriate transport
// based on the request's HTTP version.
func NewAutoTransport(maxIdle, maxIdlePerHost int) http.RoundTripper {
	return newAutoTransport(
		newHTTPTransport(false /*disable keep-alives*/, false /*disable auto-compression*/, maxIdle, maxIdlePerHost),
		newH2CTransport(false /*disable auto-compression*/))
}

// NewProxyAutoTransport creates a RoundTripper suitable for use by a reverse
// proxy.  The returned transport uses HTTP or H2C based on the request's HTTP
// version. The transport has DisableCompression set to true.
func NewProxyAutoTransport(maxIdle, maxIdlePerHost int) http.RoundTripper {
	return newAutoTransport(
		newHTTPTransport(false /*disable keep-alives*/, true /*disable auto-compression*/, maxIdle, maxIdlePerHost),
		newH2CTransport(true /*disable auto-compression*/))
}

// AutoTransport uses h2c for HTTP2 requests and falls back to `http.DefaultTransport` for all others
var AutoTransport = NewAutoTransport(1000, 100)
