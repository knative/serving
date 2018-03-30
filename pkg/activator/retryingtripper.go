package activator

import (
	"net"
	"net/http"
	"time"

	"github.com/golang/glog"
)

// A layer on top of http.DefaultTransport, with retries.
// Forked from https://github.com/fission/fission/blob/746c51901da590cff09317dbe59aa19241211812/router/functionHandler.go#L53
type retryingRoundTripper struct {
	MaxRetries     uint
	InitialTimeout time.Duration
}

func (rrt retryingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
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
		glog.Info("Retrying request to %v in %v", req.URL.Host, timeout)
		time.Sleep(timeout)
	}
	// finally, one more retry with the default timeout
	return http.DefaultTransport.RoundTrip(req)
}
