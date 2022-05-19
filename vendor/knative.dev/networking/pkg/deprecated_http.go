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

package pkg

import (
	"knative.dev/networking/pkg/http"
	"knative.dev/networking/pkg/http/probe"
	"knative.dev/networking/pkg/http/proxy"
	"knative.dev/networking/pkg/http/stats"
)

const (
	// ProbePath is the name of a path that activator, autoscaler and
	// prober(used by KIngress generally) use for health check.
	//
	// Deprecated: use knative.dev/networking/pkg/http.HealthCheckPath
	ProbePath = http.HealthCheckPath

	// FlushInterval controls the time when we flush the connection in the
	// reverse proxies (Activator, QP).
	// As of go1.16, a FlushInterval of 0 (the default) still flushes immediately
	// when Content-Length is -1, which means the default works properly for
	// streaming/websockets, without flushing more often than necessary for
	// non-streaming requests.
	//
	// Deprecated: use knative.dev/networking/pkg/http/proxy.FlushInterval
	FlushInterval = proxy.FlushInterval
)

type (
	// ReqEvent represents either an incoming or closed request.
	//
	// Deprecated: use knative.dev/networking/pkg/http/stats.ReqEvent
	ReqEvent = stats.ReqEvent

	// ReqEventType denotes the type (incoming/closed) of a ReqEvent.
	//
	// Deprecated: use knative.dev/networking/pkg/http/stats.ReqEventType
	ReqEventType = stats.ReqEventType

	// RequestStats collects statistics about requests as they flow in and out of the system.
	//
	// Deprecated: use knative.dev/networking/pkg/http/stats.RequestStats
	RequestStats = stats.RequestStats

	// RequestStatsReport are the metrics reported from the the request stats collector
	// at a given time.
	//
	// Deprecated: use knative.dev/networking/pkg/http/stats.RequestStatsReport
	RequestStatsReport = stats.RequestStatsReport
)

const (
	// ReqIn represents an incoming request
	//
	// Deprecated: use knative.dev/networking/pkg/http/stats.ReqIn
	ReqIn = stats.ReqIn

	// ReqOut represents a finished request
	//
	// Deprecated: use knative.dev/networking/pkg/http/stats.ReqOut
	ReqOut = stats.ReqOut

	// ProxiedIn represents an incoming request through a proxy.
	//
	// Deprecated: use knative.dev/networking/pkg/http/stats.ProxiedIn
	ProxiedIn = stats.ProxiedIn

	// ProxiedOut represents a finished proxied request.
	//
	// Deprecated: use knative.dev/networking/pkg/http/stats.ProxiedOut
	ProxiedOut = stats.ProxiedOut
)

var (
	// NewRequestStats builds a RequestStats instance, started at the given time.
	//
	// Deprecated: use knative.dev/networking/pkg/http/stats.NewRequestStats
	NewRequestStats = stats.NewRequestStats

	// NewBufferPool creates a new BufferPool. This is only safe to use in the context
	// of a httputil.ReverseProxy, as the buffers returned via Put are not cleaned
	// explicitly.
	//
	// Deprecated: use knative.dev/networking/pkg/http/proxy.NewBufferPool
	NewBufferPool = proxy.NewBufferPool

	// NewProbeHandler wraps a HTTP handler handling probing requests around the provided HTTP handler
	//
	// Deprecated: use knative.dev/networking/pkg/http/probe.NewHandler
	NewProbeHandler = probe.NewHandler

	// IsPotentialMeshErrorResponse returns whether the HTTP response is compatible
	// with having been caused by attempting direct connection when mesh was
	// enabled. For example if we get a HTTP 404 status code it's safe to assume
	// mesh is not enabled even if a probe was otherwise unsuccessful. This is
	// useful to avoid falling back to ClusterIP when we see errors which are
	// unrelated to mesh being enabled.
	//
	// Deprecated: use knative.dev/networking/pkg/http.IsPotentialMeshErrorResponse
	IsPotentialMeshErrorResponse = http.IsPotentialMeshErrorResponse
)
