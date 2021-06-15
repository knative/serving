/*
Copyright 2019 The Knative Authors

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

package queue

import (
	"context"
	"net/http"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	network "knative.dev/networking/pkg"
	pkgmetrics "knative.dev/pkg/metrics"
	pkghttp "knative.dev/serving/pkg/http"
	"knative.dev/serving/pkg/metrics"
)

var (
	// NOTE: 0 should not be used as boundary. See
	// https://github.com/census-ecosystem/opencensus-go-exporter-stackdriver/issues/98
	defaultLatencyDistribution = view.Distribution(
		5, 10, 20, 40, 60, 80, 100, 150, 200, 250, 300, 350, 400, 450, 500, 600,
		700, 800, 900, 1000, 2000, 5000, 10000, 20000, 50000, 100000)

	// Metric counters.
	requestCountM = stats.Int64(
		"request_count",
		"The number of requests that are routed to queue-proxy",
		stats.UnitDimensionless)
	responseTimeInMsecM = stats.Float64(
		"request_latencies",
		"The response time in millisecond",
		stats.UnitMilliseconds)
	appRequestCountM = stats.Int64(
		"app_request_count",
		"The number of requests that are routed to user-container",
		stats.UnitDimensionless)
	appResponseTimeInMsecM = stats.Float64(
		"app_request_latencies",
		"The response time in millisecond",
		stats.UnitMilliseconds)
	queueDepthM = stats.Int64(
		"queue_depth",
		"The current number of items in the serving and waiting queue, or not reported if unlimited concurrency.",
		stats.UnitDimensionless)
)

type requestMetricsHandler struct {
	next     http.Handler
	statsCtx context.Context
}

type appRequestMetricsHandler struct {
	next     http.Handler
	statsCtx context.Context
	breaker  *Breaker
}

// NewRequestMetricsHandler creates an http.Handler that emits request metrics.
func NewRequestMetricsHandler(next http.Handler,
	ns, service, config, rev, pod string) (http.Handler, error) {
	keys := []tag.Key{metrics.PodTagKey, metrics.ContainerTagKey, metrics.ResponseCodeKey, metrics.ResponseCodeClassKey, metrics.RouteTagKey}
	if err := pkgmetrics.RegisterResourceView(
		&view.View{
			Description: "The number of requests that are routed to queue-proxy",
			Measure:     requestCountM,
			Aggregation: view.Count(),
			TagKeys:     keys,
		},
		&view.View{
			Description: "The response time in millisecond",
			Measure:     responseTimeInMsecM,
			Aggregation: defaultLatencyDistribution,
			TagKeys:     keys,
		},
	); err != nil {
		return nil, err
	}

	ctx, err := metrics.PodRevisionContext(pod, "queue-proxy", ns, service, config, rev)
	if err != nil {
		return nil, err
	}

	return &requestMetricsHandler{
		next:     next,
		statsCtx: ctx,
	}, nil
}

func (h *requestMetricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rr := pkghttp.NewResponseRecorder(w, http.StatusOK)
	startTime := time.Now()

	defer func() {
		// Filter probe requests for revision metrics.
		if network.IsProbe(r) {
			return
		}

		// If ServeHTTP panics, recover, record the failure and panic again.
		err := recover()
		latency := time.Since(startTime)
		routeTag := GetRouteTagNameFromRequest(r)
		if err != nil {
			ctx := metrics.AugmentWithResponseAndRouteTag(h.statsCtx,
				http.StatusInternalServerError, routeTag)
			pkgmetrics.RecordBatch(ctx, requestCountM.M(1),
				responseTimeInMsecM.M(float64(latency.Milliseconds())))
			panic(err)
		}
		ctx := metrics.AugmentWithResponseAndRouteTag(h.statsCtx,
			rr.ResponseCode, routeTag)
		pkgmetrics.RecordBatch(ctx, requestCountM.M(1),
			responseTimeInMsecM.M(float64(latency.Milliseconds())))
	}()

	h.next.ServeHTTP(rr, r)
}

// NewAppRequestMetricsHandler creates an http.Handler that emits request metrics.
func NewAppRequestMetricsHandler(next http.Handler, b *Breaker,
	ns, service, config, rev, pod string) (http.Handler, error) {
	keys := []tag.Key{metrics.PodTagKey, metrics.ContainerTagKey, metrics.ResponseCodeKey, metrics.ResponseCodeClassKey}
	if err := pkgmetrics.RegisterResourceView(&view.View{
		Description: "The number of requests that are routed to user-container",
		Measure:     appRequestCountM,
		Aggregation: view.Count(),
		TagKeys:     keys,
	}, &view.View{
		Description: "The response time in millisecond",
		Measure:     appResponseTimeInMsecM,
		Aggregation: defaultLatencyDistribution,
		TagKeys:     keys,
	}, &view.View{
		Description: "The number of items queued at this queue proxy.",
		Measure:     queueDepthM,
		Aggregation: view.LastValue(),
		TagKeys:     keys,
	}); err != nil {
		return nil, err
	}

	ctx, err := metrics.PodRevisionContext(pod, "queue-proxy", ns, service, config, rev)
	if err != nil {
		return nil, err
	}

	return &appRequestMetricsHandler{
		next:     next,
		statsCtx: ctx,
		breaker:  b,
	}, nil
}

func (h *appRequestMetricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rr := pkghttp.NewResponseRecorder(w, http.StatusOK)
	startTime := time.Now()

	if h.breaker != nil {
		pkgmetrics.Record(h.statsCtx, queueDepthM.M(int64(h.breaker.InFlight())))
	}
	defer func() {
		// Filter probe requests for revision metrics.
		if network.IsProbe(r) {
			return
		}

		// If ServeHTTP panics, recover, record the failure and panic again.
		err := recover()
		latency := time.Since(startTime)
		if err != nil {
			ctx := metrics.AugmentWithResponse(h.statsCtx, http.StatusInternalServerError)
			pkgmetrics.RecordBatch(ctx, appRequestCountM.M(1),
				appResponseTimeInMsecM.M(float64(latency.Milliseconds())))
			panic(err)
		}

		ctx := metrics.AugmentWithResponse(h.statsCtx, rr.ResponseCode)
		pkgmetrics.RecordBatch(ctx, appRequestCountM.M(1),
			appResponseTimeInMsecM.M(float64(latency.Milliseconds())))
	}()
	h.next.ServeHTTP(rr, r)
}

const (
	defaultTagName   = "DEFAULT"
	undefinedTagName = "UNDEFINED"
	disabledTagName  = "DISABLED"
)

// GetRouteTagNameFromRequest extracts the value of the tag header from http.Request
func GetRouteTagNameFromRequest(r *http.Request) string {
	name := r.Header.Get(network.TagHeaderName)
	isDefaultRoute := r.Header.Get(network.DefaultRouteHeaderName)

	if name == "" {
		if isDefaultRoute == "" {
			// If there are no tag header and no `Knative-Serving-Default-Route` header,
			// it means that the tag header based routing is disabled, so the tag value is set to `disabled`.
			return disabledTagName
		}
		// If there is no tag header, just returns "default".
		return defaultTagName
	} else if isDefaultRoute == "true" {
		// If there is a tag header with not-empty string and the request is routed via the default route,
		// returns "undefined".
		return undefinedTagName
	}
	// Otherwise, returns the value of the tag header.
	return name
}
