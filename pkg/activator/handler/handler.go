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
	"strings"
	"syscall"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/types"

	netheader "knative.dev/networking/pkg/http/header"
	netproxy "knative.dev/networking/pkg/http/proxy"
	"knative.dev/pkg/logging/logkey"
	"knative.dev/pkg/network"
	"knative.dev/serving/pkg/activator"
	apiconfig "knative.dev/serving/pkg/apis/config"
	pkghttp "knative.dev/serving/pkg/http"
	"knative.dev/serving/pkg/networking"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/reconciler/serverlessservice/resources/names"
)

// Throttler is the interface that Handler calls to Try to proxy the user request.
type Throttler interface {
	Try(ctx context.Context, revID types.NamespacedName, fn func(string, bool) error) error
}

// activationHandler will wait for an active endpoint for a revision
// to be available before proxying the request
type activationHandler struct {
	transport        http.RoundTripper
	usePassthroughLb bool
	throttler        Throttler
	bufferPool       httputil.BufferPool
	logger           *zap.SugaredLogger
	tls              bool
	tracer           trace.Tracer
}

// New constructs a new http.Handler that deals with revision activation.
func New(_ context.Context,
	t Throttler,
	transport http.RoundTripper,
	usePassthroughLb bool,
	logger *zap.SugaredLogger,
	tlsEnabled bool,
	tp trace.TracerProvider,
) http.Handler {
	if tp == nil {
		tp = otel.GetTracerProvider()
	}

	transport = otelhttp.NewTransport(
		transport,
		otelhttp.WithTracerProvider(tp),
		otelhttp.WithFilter(func(r *http.Request) bool {
			// Don't trace kubelet probes
			return !network.IsKubeletProbe(r)
		}),
		otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string {
			if r.URL.Path == "" {
				return r.Method + " /"
			}
			return fmt.Sprintf("%s %s", r.Method, r.URL.Path)
		}),
	)

	tracer := tp.Tracer("knative.dev/serving/pkg/activator")

	return &activationHandler{
		tracer:           tracer,
		transport:        transport,
		usePassthroughLb: usePassthroughLb,
		throttler:        t,
		bufferPool:       netproxy.NewBufferPool(),
		logger:           logger,
		tls:              tlsEnabled,
	}
}

func (a *activationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tryContext, trySpan := a.tracer.Start(r.Context(), "throttler_try")

	revID := RevIDFrom(r.Context())
	if err := a.throttler.Try(tryContext, revID, func(dest string, isClusterIP bool) error {
		trySpan.End()

		proxyCtx, proxySpan := a.tracer.Start(r.Context(), "activator_proxy")
		a.proxyRequest(revID, w, r.WithContext(proxyCtx), dest, a.usePassthroughLb, isClusterIP)
		proxySpan.End()

		return nil
	}); err != nil {
		// Set error on our capacity waiting span and end it.
		trySpan.SetStatus(codes.Error, err.Error())
		trySpan.End()

		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, queue.ErrRequestQueueFull) {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
}

func (a *activationHandler) proxyRequest(revID types.NamespacedName, w http.ResponseWriter,
	r *http.Request, target string, usePassthroughLb bool, isClusterIP bool,
) {
	netheader.RewriteHostIn(r)
	r.Header.Set(netheader.ProxyKey, activator.Name)

	// Set up the reverse proxy.
	hostOverride := pkghttp.NoHostOverride
	if usePassthroughLb {
		hostOverride = names.PrivateService(revID.Name) + "." + revID.Namespace
	}

	var proxy *httputil.ReverseProxy
	if a.tls {
		tlsTargetPort := networking.BackendHTTPSPort
		if isClusterIP {
			tlsTargetPort = 443
		}
		proxy = pkghttp.NewHeaderPruningReverseProxy(useSecurePort(target, tlsTargetPort), hostOverride, activator.RevisionHeaders, true /* uss HTTPS */)
	} else {
		proxy = pkghttp.NewHeaderPruningReverseProxy(target, hostOverride, activator.RevisionHeaders, false /* use HTTPS */)
	}

	proxy.BufferPool = a.bufferPool
	proxy.Transport = a.transport
	proxy.FlushInterval = netproxy.FlushInterval

	// Mark this request as targeting a healthy backend so metrics can be scoped appropriately.
	r = r.WithContext(WithHealthyTarget(r.Context(), true))

	// Log proxy attempt
	a.logger.Debugw("Proxy attempt",
		zap.String("x-request-id", r.Header.Get("X-Request-Id")),
		zap.String("target", target),
		zap.Bool("clusterIP", isClusterIP),
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
	)
	proxy.ServeHTTP(w, r)
}

// useSecurePort replaces the default port with HTTPS port (8112).
// TODO: endpointsToDests() should support HTTPS instead of this overwrite but it needs metadata request to be encrypted.
// This code should be removed when https://github.com/knative/serving/issues/12821 was solved.
func useSecurePort(target string, port int) string {
	target = strings.Split(target, ":")[0]
	return target + ":" + strconv.Itoa(port)
}

func WrapActivatorHandlerWithFullDuplex(h http.Handler, logger *zap.SugaredLogger) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		revEnableHTTP1FullDuplex := strings.EqualFold(RevAnnotation(r.Context(), apiconfig.AllowHTTPFullDuplexFeatureKey), "Enabled")
		if revEnableHTTP1FullDuplex {
			rc := http.NewResponseController(w)
			if err := rc.EnableFullDuplex(); err != nil {
				logger.Errorw("Unable to enable full duplex", zap.Error(err))
			}
		}
		h.ServeHTTP(w, r)
	})
}

// logDetailedProxyError provides detailed logging about proxy failures to help with 502 debugging
func (a *activationHandler) logDetailedProxyError(revID types.NamespacedName, target string, req *http.Request, err error) {
	logger := a.logger.With(zap.String(logkey.Key, revID.String()))

	// Extract request ID for correlation
	xRequestId := req.Header.Get("X-Request-Id")

	// Classify the error type
	errorType := "unknown"
	errorDetails := make(map[string]interface{})

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		errorType = "timeout"
		errorDetails["reason"] = "request deadline exceeded"
	case errors.Is(err, context.Canceled):
		errorType = "canceled"
		errorDetails["reason"] = "request context canceled"
	default:
		if netErr, ok := err.(net.Error); ok {
			if netErr.Timeout() {
				errorType = "network_timeout"
				errorDetails["reason"] = "network timeout"
			} else {
				errorType = "network_error"
				errorDetails["reason"] = netErr.Error()
			}
		} else if urlErr, ok := err.(*url.Error); ok {
			errorType = "url_error"
			errorDetails["op"] = urlErr.Op
			errorDetails["url"] = urlErr.URL
			if urlErr.Err != nil {
				// Check for specific connection errors
				if errors.Is(urlErr.Err, syscall.ECONNREFUSED) {
					errorDetails["reason"] = "connection refused"
				} else if errors.Is(urlErr.Err, syscall.ECONNRESET) {
					errorDetails["reason"] = "connection reset by peer"
				} else if errors.Is(urlErr.Err, syscall.EHOSTUNREACH) {
					errorDetails["reason"] = "host unreachable"
				} else if errors.Is(urlErr.Err, syscall.ENETUNREACH) {
					errorDetails["reason"] = "network unreachable"
				} else {
					errorDetails["reason"] = urlErr.Err.Error()
				}
			}
		} else {
			errorType = "other"
			errorDetails["reason"] = err.Error()
		}
	}

	logger.Errorw("Proxy request failed - returning 502 Bad Gateway",
		zap.String("x-request-id", xRequestId),
		zap.String("target", target),
		zap.String("method", req.Method),
		zap.String("path", req.URL.Path),
		zap.String("error_type", errorType),
		zap.Any("error_details", errorDetails),
		zap.Error(err),
	)
}
