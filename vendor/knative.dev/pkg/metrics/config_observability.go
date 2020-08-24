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

package metrics

import (
	"os"
	"strings"
	texttemplate "text/template"

	corev1 "k8s.io/api/core/v1"
	cm "knative.dev/pkg/configmap"
)

const (
	// The following is used to set the default log url template
	DefaultLogURLTemplate = "http://localhost:8001/api/v1/namespaces/knative-monitoring/services/kibana-logging/proxy/app/kibana#/discover?_a=(query:(match:(kubernetes.labels.knative-dev%2FrevisionUID:(query:'${REVISION_UID}',type:phrase))))"

	// The following is used to set the default metrics backend
	defaultRequestMetricsBackend = "prometheus"

	// The env var name for config-observability
	configMapNameEnv = "CONFIG_OBSERVABILITY_NAME"
)

// ObservabilityConfig contains the configuration defined in the observability ConfigMap.
// +k8s:deepcopy-gen=true
type ObservabilityConfig struct {
	// EnableVarLogCollection specifies whether the logs under /var/log/ should be available
	// for collection on the host node by the fluentd daemon set.
	EnableVarLogCollection bool

	// LoggingURLTemplate is a string containing the logging url template where
	// the variable REVISION_UID will be replaced with the created revision's UID.
	LoggingURLTemplate string

	// RequestLogTemplate is the go template to use to shape the request logs.
	RequestLogTemplate string

	// EnableProbeRequestLog enables queue-proxy to write health check probe request logs.
	EnableProbeRequestLog bool

	// RequestMetricsBackend specifies the request metrics destination, e.g. Prometheus,
	// Stackdriver. "None" disables all backends.
	RequestMetricsBackend string

	// EnableProfiling indicates whether it is allowed to retrieve runtime profiling data from
	// the pods via an HTTP server in the format expected by the pprof visualization tool.
	EnableProfiling bool

	// EnableRequestLog enables activator/queue-proxy to write request logs.
	EnableRequestLog bool

	// MetricsCollectorAddress specifies the metrics collector address. This is only used
	// when the metrics backend is opencensus.
	MetricsCollectorAddress string
}

func defaultConfig() *ObservabilityConfig {
	return &ObservabilityConfig{
		LoggingURLTemplate:    DefaultLogURLTemplate,
		RequestMetricsBackend: defaultRequestMetricsBackend,
	}
}

// NewObservabilityConfigFromConfigMap creates a ObservabilityConfig from the supplied ConfigMap
func NewObservabilityConfigFromConfigMap(configMap *corev1.ConfigMap) (*ObservabilityConfig, error) {
	oc := defaultConfig()

	if err := cm.Parse(configMap.Data,
		cm.AsBool("logging.enable-var-log-collection", &oc.EnableVarLogCollection),
		cm.AsString("logging.revision-url-template", &oc.LoggingURLTemplate),
		cm.AsString("logging.request-log-template", &oc.RequestLogTemplate),
		cm.AsBool("logging.enable-probe-request-log", &oc.EnableProbeRequestLog),
		cm.AsString("metrics.request-metrics-backend-destination", &oc.RequestMetricsBackend),
		cm.AsBool("profiling.enable", &oc.EnableProfiling),
		cm.AsString("metrics.opencensus-address", &oc.MetricsCollectorAddress),
	); err != nil {
		return nil, err
	}

	if raw, ok := configMap.Data["logging.enable-request-log"]; ok {
		if strings.EqualFold(raw, "true") && oc.RequestLogTemplate != "" {
			oc.EnableRequestLog = true
		}
	} else if oc.RequestLogTemplate != "" {
		// TODO: remove this after 0.17 cuts, this is meant only for smooth transition to the new flag.
		// Once 0.17 cuts we should set a proper default value and users will need to set the flag explicitly
		// to enable request logging.
		oc.EnableRequestLog = true
	}

	if oc.RequestLogTemplate != "" {
		// Verify that we get valid templates.
		if _, err := texttemplate.New("requestLog").Parse(oc.RequestLogTemplate); err != nil {
			return nil, err
		}
	}

	return oc, nil
}

// ConfigMapName gets the name of the metrics ConfigMap
func ConfigMapName() string {
	cm := os.Getenv(configMapNameEnv)
	if cm == "" {
		return "config-observability"
	}
	return cm
}
