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
	"bufio"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/knative/pkg/websocket"
	"github.com/knative/serving/pkg/activator"
	"github.com/knative/serving/pkg/activator/util"
	pkghttp "github.com/knative/serving/pkg/http"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/queue"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/wait"
)

// ActivationHandler will wait for an active endpoint for a revision
// to be available before proxing the request
type ActivationHandler struct {
	Activator activator.Activator
	Logger    *zap.SugaredLogger
	Transport http.RoundTripper
	Reporter  activator.StatsReporter
	Throttler *activator.Throttler

	// GetProbeCount is the number of attempts we should
	// make to network probe the queue-proxy after the revision becomes
	// ready before forwarding the payload.  If zero, a network probe
	// is not required.
	GetProbeCount int
}

func (a *ActivationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	namespace := pkghttp.LastHeaderValue(r.Header, activator.RevisionHeaderNamespace)
	name := pkghttp.LastHeaderValue(r.Header, activator.RevisionHeaderName)
	start := time.Now()
	revID := activator.RevisionID{Namespace: namespace, Name: name}

	// ActiveEndpoint() will block until the first endpoint is available.
	ar := a.Activator.ActiveEndpoint(namespace, name)

	if ar.Error != nil {
		msg := fmt.Sprintf("Error getting active endpoint: %v", ar.Error)
		a.Logger.Error(msg)
		http.Error(w, msg, ar.Status)
		return
	}
	target := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", ar.Endpoint.FQDN, ar.Endpoint.Port),
	}

	err := a.Throttler.Try(revID, func() {
		var (
			httpStatus int
			attempts   int
		)

		// If a GET probe interval has been configured, then probe
		// the queue-proxy with our network probe header until it
		// returns a 200 status code.
		success := (a.GetProbeCount == 0)
		if !success {
			probeReq := &http.Request{
				Method:     http.MethodGet,
				URL:        target,
				Proto:      r.Proto,
				ProtoMajor: r.ProtoMajor,
				ProtoMinor: r.ProtoMinor,
				Host:       r.Host,
				Header: map[string][]string{
					http.CanonicalHeaderKey(network.ProbeHeaderName): {"true"},
				},
			}
			settings := wait.Backoff{
				Duration: 100 * time.Millisecond,
				Factor:   1.3,
				Steps:    a.GetProbeCount,
			}
			err := wait.ExponentialBackoff(settings, func() (bool, error) {
				attempts++
				probeResp, err := a.Transport.RoundTrip(probeReq)
				if err != nil {
					a.Logger.Warnw("Pod probe failed", zap.Error(err))
					return false, nil
				}
				defer probeResp.Body.Close()
				httpStatus = probeResp.StatusCode
				if httpStatus == http.StatusServiceUnavailable {
					a.Logger.Warnf("Pod probe sent status: %d", httpStatus)
					return false, nil
				}
				if body, err := ioutil.ReadAll(probeResp.Body); err != nil {
					a.Logger.Errorw("Pod probe returns an invalid response body", zap.Error(err))
					return false, nil
				} else if queue.Name != string(body) {
					a.Logger.Infof("Pod probe did not reach the target queue proxy. Reached: %s", body)
					return false, nil
				}
				return true, nil
			})
			success = (err == nil) && httpStatus == http.StatusOK
		}

		if success {
			// Once we see a successful probe, send traffic.
			attempts++
			httpStatus = a.proxyRequest(w, r, target)
		} else {
			httpStatus = http.StatusInternalServerError
			w.WriteHeader(httpStatus)
		}

		// Report the metrics
		duration := time.Since(start)

		a.Reporter.ReportRequestCount(namespace, ar.ServiceName, ar.ConfigurationName, name, httpStatus, attempts, 1.0)
		a.Reporter.ReportResponseTime(namespace, ar.ServiceName, ar.ConfigurationName, name, httpStatus, duration)
	})
	if err != nil {
		if err == activator.ErrActivatorOverload {
			http.Error(w, activator.ErrActivatorOverload.Error(), http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			a.Logger.Errorw("Error processing request in the activator", zap.Error(err))
		}
	}
}

func (a *ActivationHandler) proxyRequest(w http.ResponseWriter, r *http.Request, target *url.URL) int {
	capture := &statusCapture{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = a.Transport
	proxy.FlushInterval = -1

	util.SetupHeaderPruning(proxy)

	proxy.ServeHTTP(capture, r)
	return capture.statusCode
}

type statusCapture struct {
	http.ResponseWriter
	statusCode int
}

var _ http.Flusher = (*statusCapture)(nil)

func (s *statusCapture) WriteHeader(statusCode int) {
	s.statusCode = statusCode
	s.ResponseWriter.WriteHeader(statusCode)
}

// Hijack calls Hijack() on the wrapped http.ResponseWriter if it implements
// http.Hijacker interface, which is required for net/http/httputil/reverseproxy
// to handle connection upgrade/switching protocol.  Otherwise returns an error.
func (s *statusCapture) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return websocket.HijackIfPossible(s.ResponseWriter)
}

func (s *statusCapture) Flush() {
	s.ResponseWriter.(http.Flusher).Flush()
}
