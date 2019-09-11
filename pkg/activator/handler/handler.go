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
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/trace"
	"go.uber.org/zap"
	activatorconfig "knative.dev/serving/pkg/activator/config"
	"knative.dev/serving/pkg/apis/serving"

	"knative.dev/pkg/logging/logkey"
	tracingconfig "knative.dev/pkg/tracing/config"
	"knative.dev/serving/pkg/activator"
	activatornet "knative.dev/serving/pkg/activator/net"
	"knative.dev/serving/pkg/activator/util"
	netlisters "knative.dev/serving/pkg/client/listers/networking/v1alpha1"
	servinglisters "knative.dev/serving/pkg/client/listers/serving/v1alpha1"
	pkghttp "knative.dev/serving/pkg/http"
	"knative.dev/serving/pkg/network"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

// activationHandler will wait for an active endpoint for a revision
// to be available before proxing the request
type activationHandler struct {
	logger    *zap.SugaredLogger
	transport http.RoundTripper
	reporter  activator.StatsReporter
	throttler *activatornet.Throttler

	endpointTimeout time.Duration

	revisionLister servinglisters.RevisionLister
	serviceLister  corev1listers.ServiceLister
	sksLister      netlisters.ServerlessServiceLister
}

// The default time we'll try to probe the revision for activation.
const defaulTimeout = 2 * time.Minute

// New constructs a new http.Handler that deals with revision activation.
func New(l *zap.SugaredLogger, r activator.StatsReporter,
	t *activatornet.Throttler,
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
		endpointTimeout: defaulTimeout,
	}
}

func (a *activationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	namespace := pkghttp.LastHeaderValue(r.Header, activator.RevisionHeaderNamespace)
	name := pkghttp.LastHeaderValue(r.Header, activator.RevisionHeaderName)
	start := time.Now()
	revID := types.NamespacedName{Namespace: namespace, Name: name}
	logger := a.logger.With(zap.String(logkey.Key, revID.String()))

	revision, err := a.revisionLister.Revisions(namespace).Get(name)
	if err != nil {
		logger.Errorw("Error while getting revision", zap.Error(err))
		sendError(err, w)
		return
	}

	tryContext, trySpan := trace.StartSpan(r.Context(), "throttler_try")
	if a.endpointTimeout > 0 {
		var cancel context.CancelFunc
		tryContext, cancel = context.WithTimeout(tryContext, a.endpointTimeout)
		defer cancel()
	}

	err = a.throttler.Try(tryContext, revID, func(dest string) error {
		trySpan.End()

		var httpStatus int
		target := url.URL{
			Scheme: "http",
			Host:   dest,
		}

		proxyCtx, proxySpan := trace.StartSpan(r.Context(), "proxy")
		httpStatus = a.proxyRequest(logger, w, r.WithContext(proxyCtx), &target)
		proxySpan.End()

		configurationName := revision.Labels[serving.ConfigurationLabelKey]
		serviceName := revision.Labels[serving.ServiceLabelKey]
		a.reporter.ReportRequestCount(namespace, serviceName, configurationName, name, httpStatus, 1)
		a.reporter.ReportResponseTime(namespace, serviceName, configurationName, name, httpStatus, time.Since(start))

		return nil
	})
	if err != nil {
		// Set error on our capacity waiting span and end it
		trySpan.Annotate([]trace.Attribute{
			trace.StringAttribute("activator.throttler.error", err.Error()),
		}, "ThrottlerTry")
		trySpan.End()

		if err == activatornet.ErrActivatorOverload {
			http.Error(w, activatornet.ErrActivatorOverload.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		logger.Errorw("Error processing request in the activator", zap.Error(err))
	}
}

func (a *activationHandler) proxyRequest(logger *zap.SugaredLogger, w http.ResponseWriter, r *http.Request, target *url.URL) int {
	network.RewriteHostIn(r)

	// Setup the reverse proxy.
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = a.transport
	if config := activatorconfig.FromContext(r.Context()); config.Tracing.Backend != tracingconfig.None {
		// When we collect metrics, we're wrapping the RoundTripper
		// the proxy would use inside an annotating transport.
		proxy.Transport = &ochttp.Transport{
			Base: a.transport,
		}
	}
	proxy.FlushInterval = -1
	proxy.ErrorHandler = network.ErrorHandler(logger)

	r.Header.Set(network.ProxyHeaderName, activator.Name)

	util.SetupHeaderPruning(proxy)

	recorder := pkghttp.NewResponseRecorder(w, http.StatusOK)
	proxy.ServeHTTP(recorder, r)
	return recorder.ResponseCode
}

func sendError(err error, w http.ResponseWriter) {
	msg := fmt.Sprintf("Error getting active endpoint: %v", err)
	if k8serrors.IsNotFound(err) {
		http.Error(w, msg, http.StatusNotFound)
		return
	}
	http.Error(w, msg, http.StatusInternalServerError)
}
