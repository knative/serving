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
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/trace"
	"go.uber.org/zap"

	"github.com/knative/serving/pkg/activator"
	activatornetwork "github.com/knative/serving/pkg/activator/network"
	"github.com/knative/serving/pkg/activator/util"
	"github.com/knative/serving/pkg/apis/serving"
	netlisters "github.com/knative/serving/pkg/client/listers/networking/v1alpha1"
	servinglisters "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	pkghttp "github.com/knative/serving/pkg/http"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/network/prober"
	"knative.dev/pkg/logging/logkey"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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

	host, err := activatornetwork.PrivateEndpointForRevision(namespace, name, revision.GetProtocol(), a.sksLister, a.serviceLister)
	if err != nil {
		logger.Errorw("Failed to get endpoint for private service", zap.Error(err))
		sendError(err, w)
		return
	}

	target := &url.URL{
		Scheme: "http",
		Host:   host,
	}

	_, ttSpan := trace.StartSpan(r.Context(), "throttler_try")
	err = a.throttler.Try(a.endpointTimeout, revID, func() {
		var (
			httpStatus int
		)

		// Send request
		reqCtx, proxySpan := trace.StartSpan(r.Context(), "proxy")
		httpStatus = a.proxyRequest(w, r.WithContext(reqCtx), target)
		proxySpan.End()

		// Report the metrics
		duration := time.Since(start)

		var configurationName string
		var serviceName string
		if revision.Labels != nil {
			configurationName = revision.Labels[serving.ConfigurationLabelKey]
			serviceName = revision.Labels[serving.ServiceLabelKey]
		}

		a.reporter.ReportRequestCount(namespace, serviceName, configurationName, name, httpStatus, 1, 1.0)
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

func sendError(err error, w http.ResponseWriter) {
	msg := fmt.Sprintf("Error getting active endpoint: %v", err)
	if k8serrors.IsNotFound(err) {
		http.Error(w, msg, http.StatusNotFound)
		return
	}
	http.Error(w, msg, http.StatusInternalServerError)
}
