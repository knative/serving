/*
Copyright 2018 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/golang/glog"
	zipkin "github.com/openzipkin/zipkin-go"
	zipkinhttp "github.com/openzipkin/zipkin-go/middleware/http"
	httpreporter "github.com/openzipkin/zipkin-go/reporter/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	requestCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "telemetrysample",
			Name:      "requests_total",
			Help:      "Total number of requests.",
		},
		[]string{"status"},
	)
	requestDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "telemetrysample",
		Name:      "request_duration_seconds",
		Help:      "Histogram of the request duration.",
		Buckets:   []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
	})
)

func init() {
	prometheus.MustRegister(requestCount)
	prometheus.MustRegister(requestDuration)
}

func main() {
	flag.Parse()
	glog.Info("Telemetry sample started.")

	// Zipkin setup
	reporter := httpreporter.NewReporter("http://zipkin.istio-system.svc.cluster.local:9411/api/v2/spans")
	defer reporter.Close()
	zipkinEndpoint, err := zipkin.NewEndpoint("TelemetrySample", "localhost:8080")
	if err != nil {
		log.Fatalf("Unable to create zipkin local endpoint: %+v\n", err)
	}
	zipkinTracer, err := zipkin.NewTracer(reporter, zipkin.WithLocalEndpoint(zipkinEndpoint))
	if err != nil {
		log.Fatalf("Unable to create zipkin tracer: %+v\n", err)
	}
	zipkinMiddleware := zipkinhttp.NewServerMiddleware(zipkinTracer, zipkinhttp.TagResponseSize(true))
	zipkinClient, err := zipkinhttp.NewClient(zipkinTracer, zipkinhttp.ClientTrace(true))
	if err != nil {
		log.Fatalf("Unable to create zipkin HTTP client: %+v\n", err)
	}

	mux := http.NewServeMux()

	// Use zipkin middleware to handle traces before receiving calls to our handler
	mux.Handle("/", zipkinMiddleware((rootHandler(zipkinClient))))

	// Setup Prometheus handler for metrics
	mux.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(":8080", mux)
}

var statusCodes = [...]int{
	http.StatusOK,
	http.StatusCreated,
	http.StatusAccepted,
	http.StatusBadRequest,
	http.StatusUnauthorized,
}

func rootHandler(client *zipkinhttp.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var s string
		for k, v := range r.Header {
			s += fmt.Sprintf("[%v: %v], ", k, v)
		}
		glog.Infof("TelemetrySample: Request received. Request headers: %v", s)

		// Pick a random return code
		status := statusCodes[rand.Intn(len(statusCodes))]

		defer func(start time.Time) {
			requestCount.With(prometheus.Labels{"status": fmt.Sprint(status)}).Inc()
			requestDuration.Observe(time.Since(start).Seconds())
		}(time.Now())

		err := callWithNewSpan(
			r.Context(),
			client,
			"http://prometheus-system-np.monitoring.svc.cluster.local:8080",
			"prometheus")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		err = callWithNewSpan(
			r.Context(),
			client,
			"http://grafana.monitoring.svc.cluster.local:30802",
			"grafana")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Ignore this failure!
		_ = callWithNewSpan(
			r.Context(),
			client,
			"http://invalidurl.svc.cluster.local",
			"invalid_url")

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(status)
		w.Write([]byte("Hello world!\n"))
		glog.Infof("Request complete. Status: %v", status)
	}
}

func callWithNewSpan(ctx context.Context, client *zipkinhttp.Client, url string, spanName string) error {
	span := zipkin.SpanFromContext(ctx)
	req, err := http.NewRequest("GET", url, strings.NewReader(""))
	if err != nil {
		glog.Errorf("Request failed: unable to create a new http request: %v", err)
		return err
	}

	req = req.WithContext(zipkin.NewContext(req.Context(), span))
	res, err := client.DoWithAppSpan(req, spanName)
	if err != nil {
		glog.Errorf("Request failed: %v", err)
		return err
	}
	defer res.Body.Close()
	return nil
}
