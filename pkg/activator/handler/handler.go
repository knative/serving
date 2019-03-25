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
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/knative/pkg/logging/logkey"
	"github.com/knative/pkg/websocket"
	"github.com/knative/serving/pkg/activator"
	"github.com/knative/serving/pkg/activator/util"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	pkghttp "github.com/knative/serving/pkg/http"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/queue"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources"
	resourcenames "github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources/names"
	"github.com/knative/serving/pkg/tracing"
	zipkinhttp "github.com/openzipkin/zipkin-go/middleware/http"
	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

// ActivationHandler will wait for an active endpoint for a revision
// to be available before proxing the request
type ActivationHandler struct {
	Logger     *zap.SugaredLogger
	Transport  http.RoundTripper
	Reporter   activator.StatsReporter
	Throttler  *activator.Throttler
	TRGetter   tracing.TracerRefGetter
	KubeClient kubernetes.Interface
	EndpointL  *activator.EndpointListener

	// GetProbeCount is the number of attempts we should
	// make to network probe the queue-proxy after the revision becomes
	// ready before forwarding the payload.  If zero, a network probe
	// is not required.
	GetProbeCount int

	GetRevision func(revID activator.RevisionID) (*v1alpha1.Revision, error)
	GetService  func(namespace, name string) (*v1.Service, error)
}

func (a *ActivationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	namespace := pkghttp.LastHeaderValue(r.Header, serving.ActivatorRevisionHeaderNamespace)
	name := pkghttp.LastHeaderValue(r.Header, serving.ActivatorRevisionHeaderName)
	start := time.Now()
	revID := activator.RevisionID{Namespace: namespace, Name: name}

	tracerRef, err := a.TRGetter(r.Context())
	defer tracerRef.Done()
	tracer := tracerRef.Tracer

	activateSpan, activeCtx := tracer.StartSpanFromContext(r.Context(), "activation_handler")
	// Only setup our pod trace if this span has a chance to be sampled
	sampled := activateSpan.Context().Sampled
	if sampled == nil || *sampled {
		lo := metav1.ListOptions{LabelSelector: fmt.Sprintf("app=%s", name)}
		go tracing.TracePodStartup(activeCtx, tracer, a.KubeClient, namespace, &lo)
	}

	logger := a.Logger.With(zap.String(logkey.Key, revID.String()))

	revision, err := a.GetRevision(revID)
	if err != nil {
		logger.Errorw("Error while getting revision", zap.Error(err))
		sendError(err, w)
		return
	}

	host, err := a.serviceHostName(revision)
	if err != nil {
		logger.Errorw("Error while getting hostname", zap.Error(err))
		sendError(err, w)
		return
	}

	target := &url.URL{
		Scheme: "http",
		Host:   host,
	}

	// Setup tracing transport if configured
	tr := a.Transport
	traceTr, err := zipkinhttp.NewTransport(tracer, zipkinhttp.RoundTripper(tr))
	if err != nil {
		a.Logger.Errorw("Failed to create zipkin http transport", zap.Error(err))
	} else {
		tr = traceTr
	}

	// Watch for endpoint in EndpointListener
	gotEpCh := make(chan *activator.Endpoint)
	closeCh := make(chan struct{})
	defer close(closeCh)
	go func() {
		ep := a.EndpointL.WaitForEndpoint(namespace, name, closeCh)
		if ep != nil {
			gotEpCh <- ep
		}
	}()

	err = a.Throttler.Try(revID, func() {
		var (
			httpStatus int
			attempts   int
		)

		go func() {
			probeSpan, reqCtx := tracer.StartSpanFromContext(r.Context(), "probe")

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
						http.CanonicalHeaderKey(network.ProbeHeaderName): {queue.Name},
					},
				}
				probeReq = probeReq.WithContext(reqCtx)
				settings := wait.Backoff{
					Duration: 100 * time.Millisecond,
					Factor:   1.3,
					Steps:    a.GetProbeCount,
				}
				err := wait.ExponentialBackoff(settings, func() (bool, error) {
					attempts++
					probeResp, err := tr.RoundTrip(probeReq)
					if err != nil {
						logger.Warnw("Pod probe failed", zap.Error(err))
						return false, nil
					}
					defer probeResp.Body.Close()
					httpStatus = probeResp.StatusCode
					if httpStatus == http.StatusServiceUnavailable {
						logger.Warnf("Pod probe sent status: %d", httpStatus)
						return false, nil
					}
					if body, err := ioutil.ReadAll(probeResp.Body); err != nil {
						logger.Errorw("Pod probe returns an invalid response body", zap.Error(err))
						return false, nil
					} else if queue.Name != string(body) {
						logger.Infof("Pod probe did not reach the target queue proxy. Reached: %s", body)
						return false, nil
					}
					return true, nil
				})
				success = (err == nil) && httpStatus == http.StatusOK
			}

			if success {
				// Once we see a successful probe, send traffic.
				attempts++
				select {
				case gotEpCh <- &activator.Endpoint{}:
				case <-closeCh:
					break
				}
			} else {
				gotEpCh <- nil
			}

			probeSpan.Finish()
		}()

		select {
		case ep := <-gotEpCh:
			if ep != nil {
				proxySpan, reqCtx := tracer.StartSpanFromContext(r.Context(), "proxy")
				httpStatus = a.proxyRequest(w, r.WithContext(reqCtx), target, tr)
				proxySpan.Finish()
			} else {
				httpStatus = http.StatusInternalServerError
				w.WriteHeader(httpStatus)
			}
		}

		// Report the metrics
		duration := time.Since(start)

		var configurationName string
		var serviceName string
		if revision.Labels != nil {
			configurationName = revision.Labels[serving.ConfigurationLabelKey]
			serviceName = revision.Labels[serving.ServiceLabelKey]
		}

		a.Reporter.ReportRequestCount(namespace, serviceName, configurationName, name, httpStatus, attempts, 1.0)
		a.Reporter.ReportResponseTime(namespace, serviceName, configurationName, name, httpStatus, duration)
	})
	if err != nil {
		if err == activator.ErrActivatorOverload {
			http.Error(w, activator.ErrActivatorOverload.Error(), http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			logger.Errorw("Error processing request in the activator", zap.Error(err))
		}
	}
}

func (a *ActivationHandler) proxyRequest(w http.ResponseWriter, r *http.Request, target *url.URL, tr http.RoundTripper) int {
	capture := &statusCapture{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = tr
	proxy.FlushInterval = -1

	util.SetupHeaderPruning(proxy)

	proxy.ServeHTTP(capture, r)
	return capture.statusCode
}

// serviceHostName obtains the hostname of the underlying service and the correct
// port to send requests to.
func (a *ActivationHandler) serviceHostName(rev *v1alpha1.Revision) (string, error) {
	serviceName := resourcenames.K8sService(rev)
	svc, err := a.GetService(rev.Namespace, serviceName)
	if err != nil {
		return "", err
	}

	// Search for the appropriate port
	port := int32(-1)
	for _, p := range svc.Spec.Ports {
		if p.Name == resources.ServicePortName(rev) {
			port = p.Port
			break
		}
	}
	if port == -1 {
		return "", errors.New("revision needs external HTTP port")
	}

	serviceFQDN := reconciler.GetK8sServiceFullname(serviceName, rev.Namespace)

	return fmt.Sprintf("%s:%d", serviceFQDN, port), nil
}

func sendError(err error, w http.ResponseWriter) {
	msg := fmt.Sprintf("Error getting active endpoint: %v", err)
	if k8serrors.IsNotFound(err) {
		http.Error(w, msg, http.StatusNotFound)
		return
	}
	http.Error(w, msg, http.StatusInternalServerError)
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
