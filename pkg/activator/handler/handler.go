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
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"

	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/trace"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/types"

	"knative.dev/control-protocol/pkg/certificates"
	netcfg "knative.dev/networking/pkg/config"
	netheader "knative.dev/networking/pkg/http/header"
	netproxy "knative.dev/networking/pkg/http/proxy"
	"knative.dev/pkg/logging/logkey"
	pkghandler "knative.dev/pkg/network/handlers"
	tracingconfig "knative.dev/pkg/tracing/config"
	"knative.dev/pkg/tracing/propagation/tracecontextb3"
	"knative.dev/serving/pkg/activator"
	activatorconfig "knative.dev/serving/pkg/activator/config"
	pkghttp "knative.dev/serving/pkg/http"
	"knative.dev/serving/pkg/networking"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/reconciler/serverlessservice/resources/names"
)

// Throttler is the interface that Handler calls to Try to proxy the user request.
type Throttler interface {
	Try(ctx context.Context, revID types.NamespacedName, fn func(string) error) error
}

// activationHandler will wait for an active endpoint for a revision
// to be available before proxying the request
type activationHandler struct {
	transport        *http.Transport
	usePassthroughLb bool
	throttler        Throttler
	bufferPool       httputil.BufferPool
	logger           *zap.SugaredLogger
	trust            netcfg.Trust
}

type verifiedName string

func (vn verifiedName) VerifyConnection(cs tls.ConnectionState) error {
	fmt.Println("\t VerifyConnection")
	if cs.PeerCertificates == nil {
		return errors.New("server certificate not verified during VerifyConnection")
	}
	for _, name := range cs.PeerCertificates[0].DNSNames {
		if name == string(vn) {
			fmt.Printf("\tFound QP for namespace %s\n", vn)
			return nil
		}
	}

	return fmt.Errorf("service in namespace %s does not have a matching name in certificate- names provided: %s", vn, cs.PeerCertificates[0].DNSNames)
}

// New constructs a new http.Handler that deals with revision activation.
func New(_ context.Context, t Throttler, transport http.RoundTripper, usePassthroughLb bool, logger *zap.SugaredLogger, trust netcfg.Trust) http.Handler {
	return &activationHandler{
		transport:        transport.(*http.Transport), // WIP - replace function signature to `transport *http.Transport`
		usePassthroughLb: usePassthroughLb,
		throttler:        t,
		bufferPool:       netproxy.NewBufferPool(),
		logger:           logger,
		trust:            trust,
	}
}

func (a *activationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	config := activatorconfig.FromContext(r.Context())
	tracingEnabled := config.Tracing.Backend != tracingconfig.None

	tryContext, trySpan := r.Context(), (*trace.Span)(nil)
	if tracingEnabled {
		tryContext, trySpan = trace.StartSpan(r.Context(), "throttler_try")
	}

	revID := RevIDFrom(r.Context())
	if err := a.throttler.Try(tryContext, revID, func(dest string) error {
		trySpan.End()

		proxyCtx, proxySpan := r.Context(), (*trace.Span)(nil)
		if tracingEnabled {
			proxyCtx, proxySpan = trace.StartSpan(r.Context(), "activator_proxy")
		}
		a.proxyRequest(revID, w, r.WithContext(proxyCtx), dest, tracingEnabled, a.usePassthroughLb)
		proxySpan.End()

		return nil
	}); err != nil {
		// Set error on our capacity waiting span and end it.
		trySpan.Annotate([]trace.Attribute{trace.StringAttribute("activator.throttler.error", err.Error())}, "ThrottlerTry")
		trySpan.End()

		a.logger.Errorw("Throttler try error", zap.String(logkey.Key, revID.String()), zap.Error(err))

		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, queue.ErrRequestQueueFull) {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
}

func (a *activationHandler) proxyRequest(revID types.NamespacedName, w http.ResponseWriter,
	r *http.Request, target string, tracingEnabled bool, usePassthroughLb bool) {
	netheader.RewriteHostIn(r)
	r.Header.Set(netheader.ProxyKey, activator.Name)

	// Set up the reverse proxy.
	hostOverride := pkghttp.NoHostOverride
	if usePassthroughLb {
		hostOverride = names.PrivateService(revID.Name) + "." + revID.Namespace
	}

	var proxy *httputil.ReverseProxy
	transport := a.transport
	if a.trust != netcfg.TrustDisabled {
		proxy = pkghttp.NewHeaderPruningReverseProxy(useSecurePort(target), hostOverride, activator.RevisionHeaders, true /* uss HTTPS */)

		if a.trust != netcfg.TrustMinimal {
			vn := verifiedName(certificates.LegacyFakeDnsName)
			transport = transport.Clone()
			transport.TLSClientConfig.VerifyConnection = vn.VerifyConnection
		}
	} else {
		proxy = pkghttp.NewHeaderPruningReverseProxy(target, hostOverride, activator.RevisionHeaders, false /* use HTTPS */)
	}

	proxy.BufferPool = a.bufferPool
	proxy.Transport = transport
	if tracingEnabled {
		proxy.Transport = &ochttp.Transport{
			Base:        proxy.Transport,
			Propagation: tracecontextb3.TraceContextB3Egress,
		}
	}

	proxy.FlushInterval = netproxy.FlushInterval
	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		pkghandler.Error(a.logger.With(zap.String(logkey.Key, revID.String())))(w, req, err)
	}

	proxy.ServeHTTP(w, r)
}

// useSecurePort replaces the default port with HTTPS port (8112).
// TODO: endpointsToDests() should support HTTPS instead of this overwrite but it needs metadata request to be encrypted.
// This code should be removed when https://github.com/knative/serving/issues/12821 was solved.
func useSecurePort(target string) string {
	target = strings.Split(target, ":")[0]
	return target + ":" + strconv.Itoa(networking.BackendHTTPSPort)
}
