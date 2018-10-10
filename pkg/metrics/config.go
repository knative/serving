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
package metrics

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"contrib.go.opencensus.io/exporter/stackdriver"
	"go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/stats/view"
	"go.uber.org/zap"
	monitoredrespb "google.golang.org/genproto/googleapis/api/monitoredres"
	corev1 "k8s.io/api/core/v1"
)

const (
	ObservabilityConfigName = "config-observability"
	backendDestinationKey   = "metrics.backend-destination"
	stackdriverProjectIdKey = "metrics.stackdriver-project-id"
	metricsPath             = "/etc/config-observability"
)

var (
	exporter view.Exporter
	mc       metricsConfig
	promSrv  *http.Server
)

type MetricsBackend string

const (
	// The metrics backend is stackdriver
	Stackdriver MetricsBackend = "stackdriver"
	// The metrics backend is prometheus
	Prometheus MetricsBackend = "prometheus"
)

type metricsConfig struct {
	component            string
	backendDestination   MetricsBackend
	stackdriverProjectId string
}

func newMetricsExporter(config metricsConfig, logger *zap.SugaredLogger) (view.Exporter, error) {
	var err error
	if promSrv != nil {
		logger.Info("Stop the server for the previous Prometheus exporter.")
		promSrv.Close()
		promSrv = nil
	}
	if exporter != nil {
		view.UnregisterExporter(exporter)
	}

	if config.backendDestination == Stackdriver {
		exporter, err = newStackdriverExporter(config, logger)
	} else {
		exporter, err = newPrometheusExporter(config, logger)
	}

	if err == nil {
		view.RegisterExporter(exporter)
		view.SetReportingPeriod(10 * time.Second)
		logger.Info("Registered the exporter.")
	}
	return exporter, err
}

func newStackdriverExporter(config metricsConfig, logger *zap.SugaredLogger) (view.Exporter, error) {
	exporter, err := stackdriver.NewExporter(stackdriver.Options{
		ProjectID:    config.stackdriverProjectId,
		MetricPrefix: config.component,
		Resource: &monitoredrespb.MonitoredResource{
			Type: "global",
		},
		DefaultMonitoringLabels: &stackdriver.Labels{},
	})
	if err != nil {
		logger.Error("Failed to create the Stackdriver exporter.", zap.Error(err))
		return nil, err
	}
	logger.Info("Created Opencensus Stackdriver exporter with config:", config)
	return exporter, nil
}

func newPrometheusExporter(config metricsConfig, logger *zap.SugaredLogger) (view.Exporter, error) {
	exporter, err := prometheus.NewExporter(prometheus.Options{Namespace: config.component})
	if err != nil {
		logger.Error("Failed to create the Prometheus exporter.", zap.Error(err))
		return nil, err
	}
	logger.Info("Created Opencensus Prometheus exporter with config:", config)

	logger.Info("Start the endpoint for Prometheus exporter.")
	// Start the endpoint for Prometheus scraping
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", exporter)
		promSrv = &http.Server{
			Addr:    ":9090",
			Handler: mux,
		}
		promSrv.ListenAndServe()
	}()

	return exporter, nil
}

func getMetricsConfig(m map[string]string, component string, logger *zap.SugaredLogger) (metricsConfig, error) {
	var mc metricsConfig
	backend, ok := m[backendDestinationKey]
	if !ok {
		return mc, errors.New("metrics.backend-destination key is missing")
	}

	var mb MetricsBackend
	if strings.EqualFold(backend, "stackdriver") {
		mb = Stackdriver
	} else {
		mb = Prometheus
		if !strings.EqualFold(backend, "prometheus") {
			logger.Infof("Unsupported metrics backend value \"%s\". Use the default metrics backend prometheus.", backend)
		}
	}

	sdProj, ok := m[stackdriverProjectIdKey]
	if strings.EqualFold(backend, "stackdriver") && !ok {
		return mc, errors.New("metrics.stackdriver-project-id key is missing when the backend-destination is set to stackdriver.")
	}
	return metricsConfig{
		component:            component,
		backendDestination:   mb,
		stackdriverProjectId: sdProj,
	}, nil
}

// UpdateExporterFromConfigMap returns a helper func that can be used to update the exporter
// when a config map is updated
func UpdateExporterFromConfigMap(component string, logger *zap.SugaredLogger) func(configMap *corev1.ConfigMap) {
	return func(configMap *corev1.ConfigMap) {
		newConfig, err := getMetricsConfig(configMap.Data, component, logger)
		if err != nil {
			if exporter == nil {
				// Fail the process if there doesn't exist an exporter.
				logger.Fatal("Failed to get a valid metrics config")
			} else {
				logger.Error("Failed to get a valid metrics config; Skip updating the metrics exporter", zap.Error(err))
				return
			}
		}
		if newConfig.backendDestination != mc.backendDestination ||
			newConfig.stackdriverProjectId != mc.stackdriverProjectId {
			_, err = newMetricsExporter(newConfig, logger)
			if err != nil {
				logger.Error("Failed to update a new metrics exporter based on metric config.", zap.Error(err))
				return
			}
			logger.Infof("Successfully updated the metrics exporter; old config: %v; new config %v", mc, newConfig)
			mc = newConfig
		}
	}
}
