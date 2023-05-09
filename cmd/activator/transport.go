package main

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

// NewProxyAutoTLSTransport is same with NewProxyAutoTransport but it has tls.Config to create HTTPS request.
func NewProxyAutoTLSTransport(maxIdle, maxIdlePerHost int, tlsConf *tls.Config) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	//transport.DialContext = DialWithBackOff
	transport.DisableKeepAlives = false
	transport.MaxIdleConns = maxIdle
	transport.MaxIdleConnsPerHost = maxIdlePerHost
	transport.ForceAttemptHTTP2 = false
	transport.DisableCompression = true

	transport.TLSClientConfig = tlsConf

	return transport
}

// NewProxyAutoTransport creates a RoundTripper suitable for use by a reverse
// proxy.  The returned transport uses HTTP or H2C based on the request's HTTP
// version. The transport has DisableCompression set to true.
func NewProxyAutoTransport(maxIdle, maxIdlePerHost int) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = DialWithBackOff
	transport.DisableKeepAlives = false
	transport.MaxIdleConns = maxIdle
	transport.MaxIdleConnsPerHost = maxIdlePerHost
	transport.ForceAttemptHTTP2 = false
	transport.DisableCompression = true
	return transport
}

// RoundTripperFunc implementation roundtrips a request.
type RoundTripperFunc func(*http.Request) (*http.Response, error)

// RoundTrip implements http.RoundTripper.
func (rt RoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return rt(r)
}

/*
	func newAutoTransport(v1, v2 http.RoundTripper) http.RoundTripper {
		return RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
			t := v1
			if r.ProtoMajor == 2 {
				t = v2
			}
			return t.RoundTrip(r)
		})
	}

	func newHTTPTransport(disableKeepAlives, disableCompression bool, maxIdle, maxIdlePerHost int) *http.Transport {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.DialContext = DialWithBackOff
		transport.DisableKeepAlives = disableKeepAlives
		transport.MaxIdleConns = maxIdle
		transport.MaxIdleConnsPerHost = maxIdlePerHost
		transport.ForceAttemptHTTP2 = false
		transport.DisableCompression = disableCompression
		return transport
	}

	func newHTTPSTransport(disableKeepAlives, disableCompression bool, maxIdle, maxIdlePerHost int, tlsConf *tls.Config) *http.Transport {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.DialContext = DialWithBackOff
		transport.DisableKeepAlives = disableKeepAlives
		transport.MaxIdleConns = maxIdle
		transport.MaxIdleConnsPerHost = maxIdlePerHost
		transport.ForceAttemptHTTP2 = false
		transport.DisableCompression = disableCompression

		transport.TLSClientConfig = tlsConf
		return transport
	}

	func newH2CTransport(disableCompression bool) *http2.Transport {
		return &http2.Transport{
			AllowHTTP:          true,
			DisableCompression: disableCompression,
			DialTLS: func(netw, addr string, _ *tls.Config) (net.Conn, error) {
				return DialWithBackOff(context.Background(),
					netw, addr)
			},
		}
	}

// newH2Transport constructs a neew H2 transport. That transport will handles HTTPS traffic
// with TLS config.

	func newH2Transport(disableCompression bool, tlsConf *tls.Config) *http2.Transport {
		return &http2.Transport{
			DisableCompression: disableCompression,
			DialTLS: func(netw, addr string, tlsConf *tls.Config) (net.Conn, error) {
				return DialTLSWithBackOff(context.Background(),
					netw, addr, tlsConf)
			},
			TLSClientConfig: tlsConf,
		}
	}
*/
const sleep = 30 * time.Millisecond

var backOffTemplate = wait.Backoff{
	Duration: 50 * time.Millisecond,
	Factor:   1.4,
	Jitter:   0.1, // At most 10% jitter.
	Steps:    15,
}

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
var DialTLSWithBackOff = NewTLSBackoffDialer(backOffTemplate)

// NewTLSBackoffDialer is same with NewBackoffDialer but takes tls config.
func NewTLSBackoffDialer(backoffConfig wait.Backoff) func(context.Context, string, string, *tls.Config) (net.Conn, error) {
	return func(ctx context.Context, network, address string, tlsConf *tls.Config) (net.Conn, error) {
		return dialBackOffHelper(ctx, network, address, backoffConfig, tlsConf)
	}
}

// ErrTimeoutDialing when the timeout is reached after set amount of time.
var ErrTimeoutDialing = errors.New("timed out dialing")

func dialBackOffHelper(ctx context.Context, network, address string, bo wait.Backoff, tlsConf *tls.Config) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:   bo.Duration, // Initial duration.
		KeepAlive: 5 * time.Second,
		DualStack: true,
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
			c, err = tls.DialWithDialer(dialer, network, address, tlsConf)
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
