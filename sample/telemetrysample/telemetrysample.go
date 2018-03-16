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
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/golang/glog"
	zipkin "github.com/openzipkin/zipkin-go"
	zipkinhttp "github.com/openzipkin/zipkin-go/middleware/http"
	httpreporter "github.com/openzipkin/zipkin-go/reporter/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// Create a counter to keep track of count of incoming requests.
	// For more information on counters, see https://prometheus.io/docs/concepts/metric_types/
	requestCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "telemetrysample",
			Name:      "requests_total",
			Help:      "Total number of requests.",
		},
		// Capture the HTTP response code in a label so that we
		// can aggregate and visualize this metric based on different
		// response codes (see count of all 400 vs 200 for example).
		[]string{"status"},
	)

	// Create a histogram to observe the request duration in buckets.
	// For more information on histograms, see https://prometheus.io/docs/concepts/metric_types/
	requestDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "telemetrysample",
		Name:      "request_duration_seconds",
		Help:      "Histogram of the request duration.",
		Buckets:   []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
	})
)

func init() {
	// Register metrics defined above with the client library.
	// prometheus.MustRegister doesn't make any external calls to Prometheus.
	// Instead, it simply validates the correctness of metric definitions and
	// tells client library to report their values to Promethus during scraping.
	// If registration fails, MustRegister panics. To prevent a panic, call
	// Register function instead. However; not failing due to invalid metric
	// definitions is generally not a good idea as your service will be running
	// blind without metrics coverage.
	prometheus.MustRegister(requestCount)
	prometheus.MustRegister(requestDuration)
}

func main() {
	flag.Parse()
	glog.Info("Telemetry sample started.")

	// Zipkin setup
	// If your service only calls other Elafros revisions, then Zipkin setup below
	// is not needed. All that you need is to extract the following headers from incoming
	// requests and copy them to your outgoing requests.
	// x-request-id, x-b3-traceid, x-b3-spanid, x-b3-parentspanid, x-b3-sampled, x-b3-flags, x-ot-span-context
	//
	// For richer instrumentation, or to instrument calls that are made to external services,
	// you can instrument the code using Zipkin's Go client library as shown below.
	//
	// Zipkin is installed in istio-system namespace because istio assumes that zipkin is installed there.
	// Ideally this value should be config driven, but for demo purposes, we will hardcode it here.
	// For unit tests, reporter.noopReporter can be used instead of the httpreporter below.
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

	// Zipkin middleware implements an http.Handler that automatically extracts
	// traces from HTTP headers and intercepts http.ResponseWriter to
	// add the extracted tracing information to the response.
	zipkinMiddleware := zipkinhttp.NewServerMiddleware(zipkinTracer, zipkinhttp.TagResponseSize(true))

	// Zipkin transport implements an http.RoundTripper and injects open tracing
	// headers into outgoing requests. If an outgoing call is made as part of responding
	// to an incoming call, outgoing request must be created with the incoming request's
	// context. Otherwise, the transporter will start a new trace id (i.e. a new root span)
	// rather than attaching to the incoming request's trace id.
	zipkinTransport, err := zipkinhttp.NewTransport(zipkinTracer)
	if err != nil {
		log.Fatalf("Unable to create Zipkin HTTP transport: %+v\n", err)
	}
	client := &http.Client{Transport: zipkinTransport}

	mux := http.NewServeMux()

	// Use zipkin middleware to handle traces before receiving calls to our handler.
	mux.Handle("/", zipkinMiddleware((rootHandler(client))))

	// Setup Prometheus handler for metrics.
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

func rootHandler(client *http.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Write the http request headers to the log for demonstration purposes.
		glog.Infof("TelemetrySample: Request received. Request headers: %v", r.Header)

		// Pick a random return code - this is used for demonstrating metrics & logs
		// with different responses.
		status := statusCodes[rand.Intn(len(statusCodes))]

		// Before returning from this function, update requestCount and requestDuration metrics.
		defer func(start time.Time) {
			// Counters only support incrementing. Increment the count by one
			// to capture the single call.
			requestCount.With(prometheus.Labels{"status": fmt.Sprint(status)}).Inc()

			// Capture the duration of the call using our histogram metric. Observe will
			// put the correct values in the correct bucket as configured above.
			requestDuration.Observe(time.Since(start).Seconds())
		}(time.Now())

		getWithContext := func(url string) (*http.Response, error) {
			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				glog.Errorf("Failed to create a new request: %v", err)
				return nil, err
			}
			// If we don't attach the incoming request's context, we will end up creating
			// a new trace id (i.e. a new root span) rather than attaching to the incoming
			// request's trace id. To ensure the continuity of the trace, use incoming
			// request's context for the outgoing request.
			req = req.WithContext(r.Context())
			return client.Do(req)
		}

		// Simulate a few extra calls to other services to demostrate the distributed tracing capabilities.
		// In sequence, call three different other services. For each call, we will create a new span
		// to track that call in the call graph. See http://opentracing.io/documentation/ for more information
		// on these concepts.
		res, err := getWithContext("http://prometheus-system-np.monitoring.svc.cluster.local:8080")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer res.Body.Close()

		res, err = getWithContext("http://grafana.monitoring.svc.cluster.local:30802")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer res.Body.Close()

		// Let's call a non-existent URL to demonstrate the failure scenarios in distributed tracing.
		res, err = getWithContext("http://invalidurl.svc.cluster.local")
		if err != nil {
			glog.Errorf("Request failed: %v", err)
		} else {
			defer res.Body.Close()
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(status)
		w.Write([]byte("Hello world!\n"))
		glog.Infof("Request complete. Status: %v", status)
	}
}
