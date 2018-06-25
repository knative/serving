/*
Copyright 2018 The Knative Authors

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
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	openzipkin "github.com/openzipkin/zipkin-go"
	httpreporter "github.com/openzipkin/zipkin-go/reporter/http"

	"go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/exporter/zipkin"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/plugin/ochttp/propagation/b3"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"
)

var (
	// Create a measurement to keep track of incoming request counts.
	// For more information on measurements, see https://godoc.org/go.opencensus.io/stats
	requestCount = stats.Int64("request_count", "Total number of requests.", stats.UnitNone)

	// Create a measurement to keep track of request durations.
	requestDuration = stats.Int64("request_duration", "Histogram of the request duration.", stats.UnitMilliseconds)

	// Capture the HTTP response code in a tag so that we can aggregate and visualize
	// this metric based on different response codes (see count of all 400 vs 200 for example).
	// For more information on tags, see https://godoc.org/go.opencensus.io/tag
	requestStatusTagKey tag.Key
)

func main() {
	flag.Parse()
	log.SetPrefix("TelemetrySample: ")
	log.Print("Telemetry sample started.")

	//
	// Metrics setup
	//

	// We want our metrics in Prometheus. Create the exporter to do that.
	promExporter, err := prometheus.NewExporter(prometheus.Options{Namespace: "telemetrysample"})
	if err != nil {
		log.Fatal(err)
	}
	view.RegisterExporter(promExporter)

	// Create the tag keys that will be used to add tags to our measurements.
	requestStatusTagKey, err = tag.NewKey("status")
	if err != nil {
		log.Fatal(err)
	}

	// Create view to see our measurements cumulatively.
	err = view.Register(
		&view.View{
			Description: "Total number of requests.",
			Measure:     requestCount,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{requestStatusTagKey},
		},
		&view.View{
			Description: "Histogram of the request duration.",
			Measure:     requestDuration,
			Aggregation: view.Distribution(10, 25, 50, 100, 250, 500, 1000, 2500),
		},
	)
	if err != nil {
		log.Fatalf("Cannot subscribe to the view: %v", err)
	}
	view.SetReportingPeriod(1 * time.Second)

	//
	// Tracing setup
	//

	// If your service only calls other Knative Serving revisions, then Zipkin setup below
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
	localEndpoint, err := openzipkin.NewEndpoint("TelemetrySample", "localhost:8080")
	if err != nil {
		log.Fatalf("Unable to create zipkin local endpoint: %+v\n", err)
	}

	// The OpenCensus exporter wraps the Zipkin reporter
	zipkinExporter := zipkin.NewExporter(reporter, localEndpoint)
	trace.RegisterExporter(zipkinExporter)

	// For example purposes, sample every trace.
	trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})

	// Use an http.RoundTripper that instruments all outgoing requests with stats and tracing.
	client := &http.Client{Transport: &ochttp.Transport{Propagation: &b3.HTTPFormat{}}}

	// Implements an http.Handler that automatically extracts traces
	// from the incoming request and writes them back to the response.
	handler := &ochttp.Handler{Propagation: &b3.HTTPFormat{}, Handler: rootHandler(client)}

	mux := http.NewServeMux()
	mux.Handle("/", handler)
	mux.Handle("/log", &ochttp.Handler{Propagation: &b3.HTTPFormat{}, Handler: logHandler(client)})
	// Setup OpenCensus Prometheus exporter to handle scraping for metrics.
	mux.Handle("/metrics", promExporter)
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
		log.Printf("Request received. Request headers: %v", r.Header)

		// Pick a random return code - this is used for demonstrating metrics & logs
		// with different responses.
		status := statusCodes[rand.Intn(len(statusCodes))]

		// Before returning from this function, update requestCount and requestDuration metrics.
		defer func(start time.Time) {
			// Create a new context with the metric tags and their values.
			ctx, err := tag.New(r.Context(), tag.Insert(requestStatusTagKey, fmt.Sprint(status)))
			if err != nil {
				log.Fatal(err)
			}

			// Increment the request count by one.
			stats.Record(ctx, requestCount.M(1))

			// Record the request duration.
			stats.Record(ctx, requestDuration.M(time.Since(start).Nanoseconds()/int64(time.Millisecond)))
		}(time.Now())

		getWithContext := func(url string) (*http.Response, error) {
			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				log.Printf("Failed to create a new request: %v", err)
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
		// Close directly rather than defer to ensure that spans are finished now rather than
		// end of this function. This applies to the remaining res.Body().Close calls below.
		res.Body.Close()

		res, err = getWithContext("http://grafana.monitoring.svc.cluster.local:30802")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		res.Body.Close()

		// Let's call a non-existent URL to demonstrate the failure scenarios in distributed tracing.
		res, err = getWithContext("http://invalidurl.svc.cluster.local")
		if err != nil {
			log.Printf("Request failed: %v", err)
		} else {
			res.Body.Close()
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(status)
		w.Write([]byte("Hello world!\n"))
		log.Printf("Request complete. Status: %v", status)
	}
}

func logHandler(client *http.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		timestamp := time.Now()

		// Send logs to STDOUT
		msg := "A log in plain text format to STDOUT"
		fmt.Fprintln(os.Stdout, msg)

		data := map[string]string{
			"log":  "A log in json format to STDOUT",
			"foo":  "bar",
			"time": timestamp.String(),
			// Cluster operator can configure which field is used as time key and what
			// the format is. For example, in config/monitoring/150-elasticsearch-dev/100-fluentd-configmap.yaml,
			// fluentd-time is the reserved key to tell fluentd the logging time. It
			// must be in the format of RFC3339Nano, i.e. %Y-%m-%dT%H:%M:%S.%NZ.
			// Without this, fluentd uses the time when it collect the log as an
			// event time.
			"fluentd-time": timestamp.Format(time.RFC3339Nano),
		}
		jsonOutput, _ := json.Marshal(data)
		fmt.Fprintln(os.Stdout, string(jsonOutput))

		// Send logs to /var/log
		fileName := "/var/log/sample.log"
		f, err := os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		defer f.Close()

		if err == nil {
			msg = "A log in plain text format to /var/log\n"
			if _, err := f.WriteString(msg); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to write to %s: %v", fileName, err)
			}

			data["log"] = "A log in json format to /var/log"
			jsonOutput, _ := json.Marshal(data)
			if _, err := f.WriteString(string(jsonOutput) + "\n"); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to write to %s: %v", fileName, err)
			}
			f.Sync()
		} else {
			fmt.Fprintf(os.Stderr, "Failed to create %s: %v", fileName, err)
		}

		fmt.Fprint(w, "Sending logs done.\n")
	}
}
