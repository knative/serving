package activator

import (
	"net"
	"net/http"
	"time"

	"github.com/golang/glog"
)

// RetryingRoundTripperInterface has RoundTrip method to send request with retries.
type RetryingRoundTripperInterface interface {
	RoundTrip(req *http.Request) (*http.Response, error)
}

// RetryingRoundTripper is a layer on top of http.DefaultTransport, with retries.
// Forked from https://github.com/fission/fission/blob/746c51901da590cff09317dbe59aa19241211812/router/functionHandler.go#L53
type RetryingRoundTripper struct {
	MaxRetries     uint
	InitialTimeout time.Duration
}

// RoundTrip sends the request with retries.
func (rrt RetryingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	timeout := rrt.InitialTimeout
	transport := http.DefaultTransport.(*http.Transport)
	// Do max-1 retries; the last one uses default transport timeouts
	for i := rrt.MaxRetries - 1; i > 0; i-- {
		// update timeout in transport
		transport.DialContext = (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: 30 * time.Second,
		}).DialContext
		resp, err := transport.RoundTrip(req)
		if err == nil {
			return resp, nil
		}
		timeout = timeout + timeout
		glog.Infof("Retrying request to %v in %v", req.URL.Host, timeout)
		time.Sleep(timeout)
	}
	// finally, one more retry with the default timeout
	return http.DefaultTransport.RoundTrip(req)
}
