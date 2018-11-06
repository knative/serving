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
	"fmt"
	"strings"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
)

const (
	backendDestinationKey   = "metrics.backend-destination"
	stackdriverProjectIDKey = "metrics.stackdriver-project-id"
)

type MetricsBackend string

const (
	// The metrics backend is stackdriver
	Stackdriver MetricsBackend = "stackdriver"
	// The metrics backend is prometheus
	Prometheus MetricsBackend = "prometheus"
)

type metricsConfig struct {
	// The metrics domain. e.g. "serving.knative.dev" or "build.knative.dev".
	domain string
	// The component that emits the metrics. e.g. "activator", "autoscaler".
	component string
	// The metrics backend destination.
	backendDestination MetricsBackend
	// The stackdriver project ID where the stats data are uploaded to. This is
	// not the GCP project ID.
	stackdriverProjectID string
}

func getMetricsConfig(m map[string]string, domain string, component string, logger *zap.SugaredLogger) (*metricsConfig, error) {
	var mc metricsConfig
	backend, ok := m[backendDestinationKey]
	if !ok {
		return nil, errors.New("metrics.backend-destination key is missing")
	}
	lb := MetricsBackend(strings.ToLower(backend))
	switch lb {
	case Stackdriver, Prometheus:
		mc.backendDestination = lb
	default:
		return nil, fmt.Errorf("Unsupported metrics backend value \"%s\"", backend)
	}

	// If stackdriverProjectIDKey is not provided for stackdriver backend destination, OpenCensus will try to
	// use the application default credentials. If that is not available, Opencensus would fail to create the
	// metrics exporter.
	if mc.backendDestination == Stackdriver {
		mc.stackdriverProjectID = m[stackdriverProjectIDKey]
	}

	if domain == "" {
		return nil, errors.New("Metrics domain cannot be empty")
	}
	mc.domain = domain

	if component == "" {
		return nil, errors.New("Metrics component name cannot be empty")
	}
	mc.component = component
	return &mc, nil
}

// UpdateExporterFromConfigMap returns a helper func that can be used to update the exporter
// when a config map is updated
func UpdateExporterFromConfigMap(domain string, component string, logger *zap.SugaredLogger) func(configMap *corev1.ConfigMap) {
	return func(configMap *corev1.ConfigMap) {
		newConfig, err := getMetricsConfig(configMap.Data, domain, component, logger)
		if err != nil {
			ce := getCurMetricsExporter()
			if ce == nil {
				// Fail the process if there doesn't exist an exporter.
				logger.Fatal("Failed to get a valid metrics config")
			} else {
				logger.Error("Failed to get a valid metrics config; Skip updating the metrics exporter", zap.Error(err))
				return
			}
		}

		if isMetricsConfigChanged(newConfig) {
			if err := newMetricsExporter(newConfig, logger); err != nil {
				logger.Errorf("Failed to update a new metrics exporter based on metric config %v. error: %v", newConfig, err)
				return
			}
		}
	}
}

// isMetricsConfigChanged compares the non-nil newConfig against curMetricsConfig. When backend changes,
// or stackdriver project ID changes for stackdriver backend, we need to update the metrics exporter.
func isMetricsConfigChanged(newConfig *metricsConfig) bool {
	cc := getCurMetricsConfig()
	if cc == nil || newConfig.backendDestination != cc.backendDestination {
		return true
	} else if newConfig.backendDestination == Stackdriver && newConfig.stackdriverProjectID != cc.stackdriverProjectID {
		return true
	}
	return false
}
