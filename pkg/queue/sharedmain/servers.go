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

package sharedmain

import (
	"net/http"
	"strconv"
	"time"

	pkgnet "knative.dev/pkg/network"
	"knative.dev/serving/pkg/networking"
	"knative.dev/serving/pkg/queue"
)

func mainServer(addr string, handler http.Handler) *http.Server {
	return pkgnet.NewServer(addr, handler)
}

func adminServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:    addr,
		Handler: handler,
		// https://medium.com/a-journey-with-go/go-understand-and-mitigate-slowloris-attack-711c1b1403f6
		ReadHeaderTimeout: time.Minute,
	}
}

func metricsServer(reporter *queue.ProtobufStatsReporter) *http.Server {
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", queue.NewStatsHandler(reporter))

	return &http.Server{
		Addr:              ":" + strconv.Itoa(networking.AutoscalingQueueMetricsPort),
		Handler:           metricsMux,
		ReadHeaderTimeout: time.Minute, //https://medium.com/a-journey-with-go/go-understand-and-mitigate-slowloris-attack-711c1b1403f6
	}
}
