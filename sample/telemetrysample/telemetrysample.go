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
	"math/rand"
	"net/http"
	"time"

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

	mux := http.NewServeMux()
	mux.HandleFunc("/", rootHandler)
	mux.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(":8080", mux)
}

var statusCodes = [...]int{
	http.StatusOK,
	http.StatusBadRequest,
	http.StatusUnauthorized,
	http.StatusInternalServerError,
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	// Pick a random return code
	status := statusCodes[rand.Intn(len(statusCodes))]

	defer func(start time.Time) {
		requestCount.With(prometheus.Labels{"status": fmt.Sprint(status)}).Inc()
		requestDuration.Observe(time.Since(start).Seconds())
	}(time.Now())

	// Add a random delay to simulate some actual work here
	time.Sleep(time.Duration(rand.Intn(1000)) * time.Millisecond)

	w.WriteHeader(status)
	w.Write([]byte("Hello world!\n"))
}
