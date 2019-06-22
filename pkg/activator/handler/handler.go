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
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/trace"
	"go.uber.org/zap"

	"github.com/knative/pkg/logging/logkey"
	"github.com/knative/serving/pkg/activator"
	"github.com/knative/serving/pkg/activator/util"
	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	netlisters "github.com/knative/serving/pkg/client/listers/networking/v1alpha1"
	servinglisters "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	pkghttp "github.com/knative/serving/pkg/http"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/network/prober"
	"github.com/knative/serving/pkg/queue"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

// activationHandler will wait for an active endpoint for a revision
// to be available before proxing the request
type activationHandler struct {
	logger    *zap.SugaredLogger
	transport http.RoundTripper
	reporter  activator.StatsReporter
	throttler *activator.Throttler

	probeTimeout          time.Duration
	probeTransportFactory prober.TransportFactory
	endpointTimeout       time.Duration

	revisionLister servinglisters.RevisionLister
	serviceLister  corev1listers.ServiceLister
	sksLister      netlisters.ServerlessServiceLister
}

// The default time we'll try to probe the revision for activation.
const defaulTimeout = 2 * time.Minute

// New constructs a new http.Handler that deals with revision activation.
func New(l *zap.SugaredLogger, r activator.StatsReporter, t *activator.Throttler,
	rl servinglisters.RevisionLister, sl corev1listers.ServiceLister,
	sksL netlisters.ServerlessServiceLister) http.Handler {

	return &activationHandler{
		logger:         l,
		transport:      network.AutoTransport,
		reporter:       r,
		throttler:      t,
		revisionLister: rl,
		sksLister:      sksL,
		serviceLister:  sl,
		probeTimeout:   defaulTimeout,
		// In activator we collect metrics, so we're wrapping
		// the Roundtripper the prober would use inside annotating transport.
		probeTransportFactory: func() http.RoundTripper {
			return &ochttp.Transport{
				Base: network.NewAutoTransport(),
			}
		},
		endpointTimeout: defaulTimeout,
	}
}

func withOrigProto(or *http.Request) prober.ProbeOption {
	return func(r *http.Request) *http.Request {
		r.Proto = or.Proto
		r.ProtoMajor = or.ProtoMajor
		r.ProtoMinor = or.ProtoMinor
		return r
	}
}

func (a *activationHandler) probeEndpoint(logger *zap.SugaredLogger, r *http.Request, target *url.URL) (bool, int) {
	var (
		attempts int
		st       = time.Now()
	)

	reqCtx, probeSpan := trace.StartSpan(r.Context(), "probe")
	defer func() {
		probeSpan.End()
		a.logger.Debugf("Probing %s took %d attempts and %v time", target.String(), attempts, time.Since(st))
	}()

	err := wait.PollImmediate(100*time.Millisecond, a.probeTimeout, func() (bool, error) {
		attempts++
		ret, err := prober.Do(reqCtx, a.probeTransportFactory(), target.String(), queue.Name, withOrigProto(r))
		if err != nil {
			logger.Warnw("Pod probe failed", zap.Error(err))
			return false, nil
		}
		if !ret {
			logger.Warn("Pod probe unsuccessful")
			return false, nil
		}
		return true, nil
	})
	return (err == nil), attempts
}

func (a *activationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	namespace := pkghttp.LastHeaderValue(r.Header, activator.RevisionHeaderNamespace)
	name := pkghttp.LastHeaderValue(r.Header, activator.RevisionHeaderName)
	start := time.Now()
	revID := activator.RevisionID{Namespace: namespace, Name: name}

	logger := a.logger.With(zap.String(logkey.Key, revID.String()))

	revision, err := a.revisionLister.Revisions(namespace).Get(name)
	if err != nil {
		logger.Errorw("Error while getting revision", zap.Error(err))
		sendError(err, w)
		return
	}

	// SKS name matches that of revision.
	sks, err := a.sksLister.ServerlessServices(namespace).Get(name)
	if err != nil {
		logger.Errorw("Error while getting SKS", zap.Error(err))
		sendError(err, w)
		return
	}
	host, err := a.serviceHostName(revision, sks.Status.PrivateServiceName)
	if err != nil {
		logger.Errorw("Error while getting hostname", zap.Error(err))
		sendError(err, w)
		return
	}

	target := &url.URL{
		Scheme: "http",
		Host:   host,
	}

	_, ttSpan := trace.StartSpan(r.Context(), "throttler_try")
	ttStart := time.Now()
	err = a.throttler.Try(a.endpointTimeout, revID, func() {
		var (
			httpStatus int
		)

		ttSpan.End()
		a.logger.Debugf("Waiting for throttler took %v time", time.Since(ttStart))

		success, attempts := a.probeEndpoint(logger, r, target)
		if success {
			// Once we see a successful probe, send traffic.
			attempts++
			reqCtx, proxySpan := trace.StartSpan(r.Context(), "proxy")
			httpStatus = a.proxyRequest(w, r.WithContext(reqCtx), target)
			proxySpan.End()
		} else {
			httpStatus = http.StatusInternalServerError
			w.WriteHeader(httpStatus)
		}

		// Report the metrics
		duration := time.Since(start)

		var configurationName string
		var serviceName string
		if revision.Labels != nil {
			configurationName = revision.Labels[serving.ConfigurationLabelKey]
			serviceName = revision.Labels[serving.ServiceLabelKey]
		}

		a.reporter.ReportRequestCount(namespace, serviceName, configurationName, name, httpStatus, attempts, 1.0)
		a.reporter.ReportResponseTime(namespace, serviceName, configurationName, name, httpStatus, duration)
	})
	if err != nil {
		// Set error on our capacity waiting span and end it
		ttSpan.Annotate([]trace.Attribute{
			trace.StringAttribute("activator.throttler.error", err.Error()),
		}, "ThrottlerTry")
		ttSpan.End()

		if err == activator.ErrActivatorOverload {
			http.Error(w, activator.ErrActivatorOverload.Error(), http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			logger.Errorw("Error processing request in the activator", zap.Error(err))
		}
	}
}

func (a *activationHandler) proxyRequest(w http.ResponseWriter, r *http.Request, target *url.URL) int {
	network.RewriteHostIn(r)
	recorder := pkghttp.NewResponseRecorder(w, http.StatusOK)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &ochttp.Transport{
		Base: a.transport,
	}
	proxy.FlushInterval = -1

	r.Header.Set(network.ProxyHeaderName, activator.Name)

	util.SetupHeaderPruning(proxy)

	proxy.ServeHTTP(recorder, r)
	return recorder.ResponseCode
}

// serviceHostName obtains the hostname of the underlying service and the correct
// port to send requests to.
func (a *activationHandler) serviceHostName(rev *v1alpha1.Revision, serviceName string) (string, error) {
	svc, err := a.serviceLister.Services(rev.Namespace).Get(serviceName)
	if err != nil {
		return "", err
	}

	// Search for the appropriate port
	port := -1
	for _, p := range svc.Spec.Ports {
		if p.Name == networking.ServicePortName(rev.GetProtocol()) {
			port = int(p.Port)
			break
		}
	}
	if port == -1 {
		return "", errors.New("revision needs external HTTP port")
	}

	serviceFQDN := network.GetServiceHostname(serviceName, rev.Namespace)

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
