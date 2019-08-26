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
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"time"

	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/trace"
	"go.uber.org/zap"
	activatorconfig "knative.dev/serving/pkg/activator/config"

	"knative.dev/pkg/logging/logkey"
	tracingconfig "knative.dev/pkg/tracing/config"
	"knative.dev/serving/pkg/activator"
	revnet "knative.dev/serving/pkg/activator/net"
	"knative.dev/serving/pkg/activator/util"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	netlisters "knative.dev/serving/pkg/client/listers/networking/v1alpha1"
	servinglisters "knative.dev/serving/pkg/client/listers/serving/v1alpha1"
	pkghttp "knative.dev/serving/pkg/http"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/network/prober"
	"knative.dev/serving/pkg/queue"

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

	probeTimeout    time.Duration
	probeTransport  http.RoundTripper
	endpointTimeout time.Duration

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
		logger:          l,
		transport:       network.AutoTransport,
		reporter:        r,
		throttler:       t,
		revisionLister:  rl,
		sksLister:       sksL,
		serviceLister:   sl,
		probeTimeout:    defaulTimeout,
		probeTransport:  network.NewProberTransport(),
		endpointTimeout: defaulTimeout,
	}
}

func withOrigProto(or *http.Request) prober.Preparer {
	return func(r *http.Request) *http.Request {
		r.Proto = or.Proto
		r.ProtoMajor = or.ProtoMajor
		r.ProtoMinor = or.ProtoMinor
		return r
	}
}

func (a *activationHandler) probeEndpoint(logger *zap.SugaredLogger, r *http.Request, target *url.URL, revID activator.RevisionID) (bool, int) {
	var (
		attempts int
		st       = time.Now()
		url      = target.String()
	)

	logger.Debugf("Actually will be probing %s", url)
	config := activatorconfig.FromContext(r.Context())
	if config.Tracing.Backend != tracingconfig.None {
		// When we collect metrics, we're wrapping the RoundTripper
		// the prober would use inside an annotating transport.
		a.probeTransport = &ochttp.Transport{
			Base: network.NewProberTransport(),
		}
	}

	err := wait.PollImmediate(100*time.Millisecond, a.probeTimeout, func() (bool, error) {
		attempts++
		ret, err := prober.Do(
			r.Context(),
			a.probeTransport,
			url,
			prober.WithHeader(network.ProbeHeaderName, queue.Name),
			prober.ExpectsBody(queue.Name),
			prober.ExpectsStatusCodes([]int{http.StatusOK}),
			withOrigProto(r))
		if err != nil {
			logger.Warnw("Pod probe failed", zap.Error(err))
			return false, nil
		}
		if !ret {
			logger.Warn("Pod probe unsuccessful")
			return false, nil
		}

		// Cache probe success.
		a.throttler.MarkProbe(revID)
		return true, nil
	})

	a.logger.Debugf("Probing %s took %d attempts and %v time", target.String(), attempts, time.Since(st))
	return (err == nil), attempts
}

func (a *activationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	namespace := pkghttp.LastHeaderValue(r.Header, activator.RevisionHeaderNamespace)
	name := pkghttp.LastHeaderValue(r.Header, activator.RevisionHeaderName)
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

	tryContext, trySpan := trace.StartSpan(r.Context(), "throttler_try")
	if a.endpointTimeout > 0 {
		var cancel context.CancelFunc
		tryContext, cancel = context.WithTimeout(tryContext, a.endpointTimeout)
		defer cancel()
	}

	tryStart := time.Now()
	err = a.throttler.Try(tryContext, revID, func() {
		trySpan.End()
		a.logger.Debugf("Waiting for throttler took %v time", time.Since(tryStart))

		// This opportunistically caches the probes, so
		// a few concurrent requests might result in concurrent probes
		// but requests coming after won't. When cached 0 attempts were required.
		success, attempts := true, 0
		if a.throttler.ShouldProbe(revID) {
			probeCtx, probeSpan := trace.StartSpan(r.Context(), "probe")
			success, attempts = a.probeEndpoint(logger, r.WithContext(probeCtx), target, revID)
			probeSpan.End()
		}
		var httpStatus int
		if success {
			// Once we see a successful probe, send traffic.
			attempts++
			proxyCtx, proxySpan := trace.StartSpan(r.Context(), "proxy")
			httpStatus = a.proxyRequest(w, r.WithContext(proxyCtx), target)
			proxySpan.End()
		} else {
			httpStatus = http.StatusInternalServerError
			w.WriteHeader(httpStatus)
		}

		configurationName := revision.Labels[serving.ConfigurationLabelKey]
		serviceName := revision.Labels[serving.ServiceLabelKey]
		a.reporter.ReportRequestCount(namespace, serviceName, configurationName, name, httpStatus, attempts)
	})
	if err != nil {
		// Set error on our capacity waiting span and end it
		trySpan.Annotate([]trace.Attribute{
			trace.StringAttribute("activator.throttler.error", err.Error()),
		}, "ThrottlerTry")
		trySpan.End()

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
	config := activatorconfig.FromContext(r.Context())
	proxy.Transport = a.transport
	if config.Tracing.Backend != tracingconfig.None {
		// When we collect metrics, we're wrapping the RoundTripper
		// the proxy would use inside an annotating transport.
		proxy.Transport = &ochttp.Transport{
			Base: a.transport,
		}
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

	// Search for the appropriate port.
	port, ok := revnet.GetServicePort(rev.GetProtocol(), svc)
	if !ok {
		return "", errors.New("revision needs external HTTP port")
	}

	// Use the ClusterIP directly to elide DNS lookup, which both adds latency
	// and hurts reliability when routing through the activator.
	return net.JoinHostPort(svc.Spec.ClusterIP, strconv.Itoa(port)), nil
}

func sendError(err error, w http.ResponseWriter) {
	msg := fmt.Sprintf("Error getting active endpoint: %v", err)
	if k8serrors.IsNotFound(err) {
		http.Error(w, msg, http.StatusNotFound)
		return
	}
	http.Error(w, msg, http.StatusInternalServerError)
}
