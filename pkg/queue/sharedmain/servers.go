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

	"go.uber.org/zap"
	pkgnet "knative.dev/pkg/network"
	"knative.dev/pkg/profiling"
	"knative.dev/serving/pkg/networking"
	"knative.dev/serving/pkg/queue"
)

func buildMainHTTPServer(composedHandler http.Handler, env config, enableTLS bool) httpServer {
	var port, name string
	if enableTLS {
		name = "mainTls"
		port = env.QueueServingTLSPort
	} else {
		name = "main"
		port = env.QueueServingPort
	}

	return httpServer{
		name: name,
		tls:  enableTLS,
		srv:  pkgnet.NewServer(":"+port, composedHandler),
	}
}

func buildAdminHTTPServer(handler http.Handler, enableTLS bool) httpServer {
	return httpServer{
		name: "admin",
		tls:  enableTLS,
		srv: &http.Server{
			Addr:    ":" + strconv.Itoa(networking.QueueAdminPort),
			Handler: handler,
			//https://medium.com/a-journey-with-go/go-understand-and-mitigate-slowloris-attack-711c1b1403f6
			ReadHeaderTimeout: time.Minute,
		},
	}
}

func buildMetricsHTTPServer(protobufStatReporter *queue.ProtobufStatsReporter) httpServer {
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", queue.NewStatsHandler(protobufStatReporter))

	return httpServer{
		name: "metrics",
		tls:  false,
		srv: &http.Server{
			Addr:    ":" + strconv.Itoa(networking.AutoscalingQueueMetricsPort),
			Handler: metricsMux,
			//https://medium.com/a-journey-with-go/go-understand-and-mitigate-slowloris-attack-711c1b1403f6
			ReadHeaderTimeout: time.Minute,
		},
	}
}

func buildProfilingHTTPServer(logger *zap.SugaredLogger) httpServer {
	return httpServer{
		name: "profile",
		tls:  false,
		srv:  profiling.NewServer(profiling.NewHandler(logger, true)),
	}
}
