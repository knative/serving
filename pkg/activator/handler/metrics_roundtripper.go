package handler

import (
	"context"
	"net"
	"net/http"
	"time"

	pkgmetrics "knative.dev/pkg/metrics"
)

// metricsRoundTripper wraps a base RoundTripper and records connection/transport metrics.
type metricsRoundTripper struct {
	Base http.RoundTripper
}

func NewMetricsRoundTripper(base http.RoundTripper) http.RoundTripper {
	return &metricsRoundTripper{Base: base}
}

func (m *metricsRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := m.Base.RoundTrip(req)

	reporterCtx := ReporterContextFrom(req.Context())
	healthy := IsHealthyTarget(req.Context())

	if err != nil {
		// Always record a transport failure when RoundTrip errors.
		if reporterCtx != nil {
			pkgmetrics.RecordBatch(reporterCtx, transportFailuresM.M(1))
		}
		if healthy && reporterCtx != nil {
			// Timeouts
			if isTimeoutErr(err) || errorsIsDeadlineExceeded(err) {
				pkgmetrics.RecordBatch(reporterCtx, healthyTargetTimeoutsM.M(1))
			}
			// In our stack, transport errors produce a 502 to the client.
			pkgmetrics.RecordBatch(reporterCtx, healthyTarget502sM.M(1))
		}
		return nil, err
	}

	if healthy && reporterCtx != nil {
		elapsed := time.Since(start)
		pkgmetrics.RecordBatch(reporterCtx, healthyConnLatencyM.M(float64(elapsed.Milliseconds())))
	}
	return resp, nil
}

func isTimeoutErr(err error) bool {
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return true
	}
	return false
}

func errorsIsDeadlineExceeded(err error) bool {
	return err == context.DeadlineExceeded
}
